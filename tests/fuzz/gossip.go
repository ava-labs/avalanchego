// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"

	txmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	// errTxPullLost means that a node answered a tx pull, and the node that
	// pulled did not process every item of the answer.
	errTxPullLost = errors.New("tx pull items lost")
	// errNoTxPush means that a node took txs through its API that other nodes
	// built into blocks, and did not push any of them.
	errNoTxPush = errors.New("no tx push gossip")
	// errTxPullDuplicates means that tx pulls brought a node too many txs that
	// it already held.
	errTxPullDuplicates = errors.New("too many duplicate tx pull items")
)

// checkGossip adds the gossip events in lines, which are lines of nodes'
// events.log, to r.gossip, and fails t if the gossip so far breaks a gossip
// check.
func (r *runner) checkGossip(t *rapid.T, lines []byte) {
	require.NoError(t, r.gossip.add(lines, r.faults.txPullExcused))

	if err := r.gossip.checkTxPullConservation(); err != nil {
		r.fail(t, "tx pull conservation", err)
	}
	if err := r.gossip.checkTxPush(r.apiTxs); err != nil {
		r.fail(t, "tx push", err)
	}
	if err := r.gossip.checkTxPullDuplicates(); err != nil {
		r.fail(t, "tx pull duplicates", err)
	}
}

// A gossipLog follows the tx gossip between honest nodes, across checks, from
// the lines of their events.log (Stateful Fuzzing.md 5.1). The gossip checks
// read it: checkTxPullConservation, checkTxPush, and checkTxPullDuplicates.
type gossipLog struct {
	honest set.Set[ids.NodeID]

	// served and txPulled hold each side of a tx pull until the other side's
	// line arrives. Each side writes its line after it sends or handles the
	// reply, so either can come first.
	served   map[txPullKey][]ids.ID
	txPulled map[txPullKey]txPullReply

	// txPushers holds the nodes that an honest node logged a P-Chain tx push
	// from.
	txPushers set.Set[ids.NodeID]

	// builders holds, by P-Chain block ID, the honest nodes that built the
	// block. accepted holds the accepted block at each height, and
	// acceptedAt the height that accepted each tx.
	builders   map[ids.ID]set.Set[ids.NodeID]
	accepted   map[uint64]ids.ID
	acceptedAt map[ids.ID]uint64

	// pulledTxs holds, by node, the P-Chain txs that honest nodes sent in
	// reply to its tx pulls, and duplicateTxs the ones it already held.
	pulledTxs    map[ids.NodeID]set.Set[ids.ID]
	duplicateTxs map[ids.NodeID]set.Set[ids.ID]
}

// newGossipLog returns a gossipLog that follows the gossip between the honest
// nodes.
func newGossipLog(honest set.Set[ids.NodeID]) *gossipLog {
	return &gossipLog{
		honest:       honest,
		served:       make(map[txPullKey][]ids.ID),
		txPulled:     make(map[txPullKey]txPullReply),
		builders:     make(map[ids.ID]set.Set[ids.NodeID]),
		accepted:     make(map[uint64]ids.ID),
		acceptedAt:   make(map[ids.ID]uint64),
		pulledTxs:    make(map[ids.NodeID]set.Set[ids.ID]),
		duplicateTxs: make(map[ids.NodeID]set.Set[ids.ID]),
	}
}

// A txPullKey names one tx pull. A request and its reply share it. Request
// IDs count per node, so they are not unique alone.
type txPullKey struct {
	chain     string
	requester ids.NodeID
	responder ids.NodeID
	requestID uint32
}

// A txPullReply is what the node that pulled txs did with the reply.
type txPullReply struct {
	failed bool // the request failed, so no items came
	items  []gossipItem
}

// A gossipItem is one item of a tx push or a tx pull reply, as the node that
// got it logs it. ID is nil when the item does not parse. Err is empty when
// the node added the item.
type gossipItem struct {
	ID  *ids.ID `json:"id,omitempty"`
	Err string  `json:"err,omitempty"`
}

// add reads the gossip events and the P-Chain blocks built and accepted in
// lines, which are lines of honest nodes' events.log. It skips gossip with any
// other peer: a Byzantine peer logs nothing, so its side of a pull is never
// known. It does not count the items of a pull reply that came at a time
// that excused reports, which a fault explains (checkTxPullDuplicates). A nil
// excused excuses nothing.
func (g *gossipLog) add(lines []byte, excused func(time.Time) bool) error {
	for line := range bytes.Lines(lines) {
		// Most lines are neither gossip nor blocks. Decode only those.
		if bytes.Contains(line, []byte(`"gossip":`)) {
			if err := g.addGossip(line, excused); err != nil {
				return err
			}
		}
		if bytes.Contains(line, []byte(`"built":`)) || bytes.Contains(line, []byte(`"accepted_txs":`)) {
			if err := g.addBlocks(line); err != nil {
				return err
			}
		}
	}

	return nil
}

// addGossip reads one gossip event.
func (g *gossipLog) addGossip(line []byte, excused func(time.Time) bool) error {
	var ev struct {
		Timestamp int64        `json:"timestamp"`
		Node      ids.NodeID   `json:"node"`
		Chain     string       `json:"chain"`
		From      ids.NodeID   `json:"from"`
		RequestID uint32       `json:"request_id"`
		Gossip    string       `json:"gossip"`
		Items     []gossipItem `json:"items"`
		Served    []ids.ID     `json:"served"`
	}
	if err := json.Unmarshal(line, &ev); err != nil {
		return fmt.Errorf("decode event %q: %w", line, err)
	}
	if !g.honest.Contains(ev.Node) || !g.honest.Contains(ev.From) {
		return nil
	}

	switch ev.Gossip {
	case "pull_request":
		g.served[txPullKey{chain: ev.Chain, requester: ev.From, responder: ev.Node, requestID: ev.RequestID}] = ev.Served
	case "pull_response":
		g.txPulled[txPullKey{chain: ev.Chain, requester: ev.Node, responder: ev.From, requestID: ev.RequestID}] = txPullReply{items: ev.Items}
		at := time.Unix(0, ev.Timestamp)
		if ev.Chain == "P" && (excused == nil || !excused(at)) {
			g.countTxPulled(ev.Node, ev.Items)
		}
	case "pull_failed":
		g.txPulled[txPullKey{chain: ev.Chain, requester: ev.Node, responder: ev.From, requestID: ev.RequestID}] = txPullReply{failed: true}
	case "push":
		if ev.Chain == "P" {
			g.txPushers.Add(ev.From)
		}
	}

	return nil
}

// countTxPulled adds the txs that node got in reply to one P-Chain tx pull,
// and the ones it already held in its mempool. An item that does not parse
// names no tx, so it is not counted.
func (g *gossipLog) countTxPulled(node ids.NodeID, items []gossipItem) {
	for _, item := range items {
		if item.ID == nil {
			continue
		}

		pulled := g.pulledTxs[node]
		pulled.Add(*item.ID)
		g.pulledTxs[node] = pulled
		if strings.Contains(item.Err, txmempool.ErrDuplicateTx.Error()) {
			dups := g.duplicateTxs[node]
			dups.Add(*item.ID)
			g.duplicateTxs[node] = dups
		}
	}
}

// addBlocks reads the P-Chain blocks that one event of an honest node built
// or accepted. The built block is the one that consensus decides, which
// accepted names too; accepted_txs names the VM's block inside it, at the
// same height.
func (g *gossipLog) addBlocks(line []byte) error {
	type block struct {
		Block  ids.ID `json:"block"`
		Height uint64 `json:"height"`
	}
	var ev struct {
		Node        ids.NodeID `json:"node"`
		Chain       string     `json:"chain"`
		Built       []block    `json:"built"`
		Accepted    []block    `json:"accepted"`
		AcceptedTxs []struct {
			Height uint64 `json:"height"`
			Txs    []struct {
				ID ids.ID `json:"id"`
			} `json:"txs"`
		} `json:"accepted_txs"`
	}
	if err := json.Unmarshal(line, &ev); err != nil {
		return fmt.Errorf("decode event %q: %w", line, err)
	}
	if ev.Chain != "P" || !g.honest.Contains(ev.Node) {
		return nil
	}

	for _, b := range ev.Built {
		builders := g.builders[b.Block]
		builders.Add(ev.Node)
		g.builders[b.Block] = builders
	}
	for _, b := range ev.Accepted {
		g.accepted[b.Height] = b.Block
	}
	for _, b := range ev.AcceptedTxs {
		for _, tx := range b.Txs {
			g.acceptedAt[tx.ID] = b.Height
		}
	}

	return nil
}

// checkTxPullConservation returns an error that names a tx pull whose reply
// lost items: the node that answered served items that the node that pulled
// did not process, as added, duplicate, or refused. It forgets every pull it
// judged, and keeps a pull with one side still unknown for a later call.
//
// Guarantee: a node processes every item of a pull reply that arrives
// (network/p2p/gossip/gossip.go handleResponse). Source: the responder's
// "served" (gossip handler AppRequest) against the requester's "items" in
// the reply, linked by chain, both nodes, and request ID. A failed pull
// brings no items, so it is not judged. Both nodes are honest, and loopback
// does not change a message, so a valid run processes exactly what was
// served, in order. A delayed link does not change that, so the check judges
// pulls on delayed links too: a reply that comes after the request timeout,
// or never, makes the pull fail (handleResponse gets the error and no
// items), and the router drops the late reply.
func (g *gossipLog) checkTxPullConservation() error {
	for key, reply := range g.txPulled {
		served, ok := g.served[key]
		if !ok {
			continue
		}
		delete(g.txPulled, key)
		delete(g.served, key)

		if reply.failed {
			continue
		}

		got := make([]ids.ID, 0, len(reply.items))
		for _, item := range reply.items {
			if item.ID != nil {
				got = append(got, *item.ID)
			}
		}
		if !slices.Equal(served, got) || len(reply.items) != len(served) {
			return fmt.Errorf("%w: on the %s chain, node %s served %d items %v for request %d, and node %s processed %d items, %d of them parsed: %v", errTxPullLost, key.chain, key.responder, len(served), served, key.requestID, key.requester, len(reply.items), len(got), got)
		}
	}

	return nil
}

// checkTxPush returns an error that names a node that pushed none of its txs
// to an honest node, though at least 10 of its API txs, by apiTxs, committed
// in blocks that other nodes built.
//
// Guarantee: a node push-gossips each tx its API takes
// (vms/platformvm/network/network.go IssueTxFromRPC) on its next push round,
// every 50 ms (nodes.go setTestDefaults), unless the tx left its mempool first
// (network/p2p/gossip/gossip.go PushGossiper.gossip). Source: the "push"
// events that the other honest nodes, all validators, log, and the P-Chain
// blocks that honest nodes log as built and accepted. A node that packs a tx
// into its own block removes it from its mempool at once
// (vms/platformvm/block/builder/builder.go executeTx), so a tx in a block that
// the node built does not count. That was 238 of 740 API txs over 9 baseline
// runs (seeds 1-8 at 1000 steps, 11 at 400). Nor does a tx that no accepted
// block holds: it may still be pushed, or be dropped first. A counted tx
// misses the push only when another node pulls it, builds it, and the first
// node verifies that block, all within one push round: 17 of 502, and at
// worst 6 of 55 on one node. At that rate, 10 counted txs that all miss it
// happen with a chance of 3e-10. A fault that closes links loses the pushes
// they queued, so apiTxs holds no tx that the API took while a fault could
// have lost its push (runner.sendTo, faults.txPushExcused).
func (g *gossipLog) checkTxPush(apiTxs map[ids.NodeID][]ids.ID) error {
	const minTxs = 10

	for node, txs := range apiTxs {
		if g.txPushers.Contains(node) {
			continue
		}

		var n int
		for _, tx := range txs {
			height, ok := g.acceptedAt[tx]
			if !ok {
				continue
			}
			if builders := g.builders[g.accepted[height]]; !builders.Contains(node) {
				n++
			}
		}
		if n >= minTxs {
			return fmt.Errorf("%w: %d txs that node %s took through its API committed in blocks that other nodes built, and no honest node got a P-Chain tx push from it", errNoTxPush, n, node)
		}
	}

	return nil
}

// checkTxPullDuplicates returns an error that names a node whose tx pulls
// from honest nodes brought more P-Chain txs that it already held than
// maxDuplicates allows for the txs they brought. It counts each tx once per
// node, however many replies carried it.
//
// Guarantee: a pull carries a bloom filter of the requester's mempool, and
// the responder serves only the txs that the filter does not hold
// (network/p2p/gossip/handler.go AppRequest), so a served tx is new to the
// requester unless the filter missed it. Source: the requester's "items",
// whose error names a duplicate tx (vms/txs/mempool ErrDuplicateTx). The
// filter's 1% false positives (vms/platformvm/config/network.go) hide txs;
// they do not repeat them. A valid run gets duplicates only from races: a tx
// that reaches the requester by push while its pull is in flight, or that
// its mempool takes back from a rejected block, which bypasses the filter
// (vms/platformvm/block/executor/rejector.go rejectBlock). Such a tx stays
// outside the filter until the filter resets, so every pull brings it again
// while it waits in the mempools: one node got the same 11 txs 19 times
// each. A repeat says nothing that the first copy did not, so the check
// counts txs, not items. Over 21 baseline runs, no node's share of items
// passed baseRate once it had 10: at worst 1 in 10 (seeds 1-6 at 100 steps,
// 11-16 at 1000), and 1 in 17 (seeds 1-8 at 1000 steps, 11 at 400). The
// check fails only when that rate gives so many duplicates with a chance
// below 1e-9. With the filter missing the node's own txs, the share reached
// 5 in 10. A delayed reply races the pushes of the same txs, so the items of a
// reply that came in a fault span, or before every reply that it delayed
// arrived, are not counted (add, faults.txPullExcused).
func (g *gossipLog) checkTxPullDuplicates() error {
	const baseRate = 0.1

	for node, pulled := range g.pulledTxs {
		if dups, n := g.duplicateTxs[node].Len(), pulled.Len(); dups > maxDuplicates(n, baseRate) {
			return fmt.Errorf("%w: node %s already held %d of the %d P-Chain txs that honest nodes sent in reply to its tx pulls", errTxPullDuplicates, node, dups, n)
		}
	}

	return nil
}

// maxDuplicates returns the least k such that n items, each a duplicate with
// chance rate on its own, hold more than k duplicates with a chance below
// 1e-9. While rate^n is not below 1e-9, k is n and nothing fails: at a rate
// of 0.1, a node needs 10 items before the bound means anything.
func maxDuplicates(n int, rate float64) int {
	const falsePositive = 1e-9

	lgN, _ := math.Lgamma(float64(n + 1))
	var tail float64 // the chance of more than k duplicates
	for k := n; k > 0; k-- {
		lgK, _ := math.Lgamma(float64(k + 1))
		lgRest, _ := math.Lgamma(float64(n - k + 1))
		p := math.Exp(lgN - lgK - lgRest + float64(k)*math.Log(rate) + float64(n-k)*math.Log1p(-rate))
		if tail+p >= falsePositive {
			return k
		}
		tail += p
	}

	return 0
}
