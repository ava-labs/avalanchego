// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

// pathBlock is the path of a tx in a block that the Byzantine proposer sent,
// beside pathAPI and pathGossip.
const pathBlock = "block"

// errPathsDisagree means a path refused a tx that a path with an earlier
// chain time took on the same state, for a reason that only a bug explains.
var errPathsDisagree = errors.New("two paths gave one tx different verdicts on the same state")

// A verdict is what one path did with a tx on one node: it took the tx, or
// refused it.
type verdict struct {
	Path    string     `json:"path"`
	Node    ids.NodeID `json:"node"`
	Refusal string     `json:"refusal,omitempty"` // empty if the path took the tx
}

// A pathCase is a tx that a propose op sent on the mempool paths and as the
// first tx of its block, while it waits for the block's verdict.
type pathCase struct {
	tx      ids.ID
	mempool []verdict // the API's, then gossip's, in the order they ran
}

// judgePaths is the path agreement oracle (Stateful Fuzzing.md 3). verdicts
// are the verdicts of paths that judged one tx on one state, in the order of
// the chain time each ran at. It returns [errPathsDisagree] if a path refused
// the tx after an earlier one took it, and no allowed rule explains the
// refusal (explained).
//
// Guarantee: the mempool and block verification run the same executor
// (block/executor/manager.go VerifyTx and verifier.go processStandardTxs both
// call executor.StandardTx). On one state, its verdict changes only with the
// chain time and with shared memory. A later chain time lowers the gas price
// and deactivates L1 validators, so it can make a tx valid, and an earlier
// refusal says nothing. The other direction is judged.
func judgePaths(verdicts []verdict) error {
	for i, earlier := range verdicts {
		if earlier.Refusal != "" {
			continue
		}
		for _, later := range verdicts[i+1:] {
			if later.Refusal != "" && !explained(later) {
				return fmt.Errorf("%w: the %s path on node %s took it, and the %s path on node %s refused it: %s", errPathsDisagree, earlier.Path, earlier.Node, later.Path, later.Node, later.Refusal)
			}
		}
	}

	return nil
}

// explained reports whether a rule other than a bug explains v, a refusal
// of a tx that a path with an earlier chain time took on the same state.
//
// Only a fresh verdict of the executor is judged. Every other refusal comes
// from a rule that only the mempool has:
//
//   - a duplicate of a mempool tx (vms/platformvm/network/gossip.go:71)
//   - the dropped-tx cache, which returns an older verdict without "failed
//     verification" (gossip.go:75, txs/mempool/mempool.go:97)
//   - conflicts with mempool txs (mempool.go:112)
//   - the meter: the AVAX-minted check and no gas used (mempool.go:116,
//     208, 222)
//   - the mempool's gas limit and its eviction by gas price (mempool.go:123,
//     141)
//   - the chain not bootstrapped, imports while partially synced, and the
//     pre-Helicon size limit (block/executor/manager.go:128, 135, 141)
//   - warp messages, verified at the preferred block's P-Chain height
//     (manager.go:157)
//   - choosing and advancing to the next block time (manager.go:176, 185)
//   - a tx gas above the current gas capacity (manager.go:203); a block
//     checks its whole gas instead (block/executor/verifier.go
//     processStandardTxs)
//
// Of the executor's rules, those that a later time can trip are allowed too:
// a staking period too short from a later chain time
// (txs/executor/staker_tx_verification.go:29), a warp message that has
// expired by it (standard_tx_executor.go:947), and shared memory, which
// changes with wall time, not with the state (standard_tx_executor.go:344).
func explained(v verdict) bool {
	const (
		fresh    = "failed verification: " // vms/platformvm/network/gossip.go:90
		executor = "failed execution: "    // txs/executor/standard_tx_executor.go:92, block/executor/manager.go:216
	)
	if !strings.Contains(v.Refusal, executor) {
		return true
	}
	// A gossiped tx's drop reason never has "failed verification": the
	// mempool marks VerifyTx's own error (gossip.go:89). It is fresh anyway:
	// mempoolPaths gossips a new tx to a node that has seen it at most from
	// the API node's push, on the same state, and reads the reason before
	// any node holds the block, whose verification would mark its own
	// (block/executor/verifier.go:585).
	if v.Path == pathAPI && !strings.Contains(v.Refusal, fresh) {
		return true
	}

	return strings.Contains(v.Refusal, "staking period is too short") ||
		strings.Contains(v.Refusal, "warp message expired") ||
		strings.Contains(v.Refusal, "failed to get shared memory")
}

// judgeBlocks judges each case in cases, by block ID, whose block a node in
// lines, lines of the nodes' events.log, verified. The block took the case's
// tx if a node verified it with no error. Its time is the earliest of the
// paths' chain times: the propose op builds it before the mempool paths run,
// at the parent's time or before. It returns the blocks it judged, and the
// first case that fails judgePaths.
func judgeBlocks(lines []byte, cases map[ids.ID]pathCase) ([]ids.ID, error) {
	took := make(map[ids.ID]ids.NodeID)
	var judged []ids.ID
	for line := range bytes.Lines(lines) {
		// Most lines are messages. Decode only the verifications.
		if !bytes.Contains(line, []byte(`"verified":`)) {
			continue
		}

		var ev struct {
			Node     ids.NodeID `json:"node"`
			Chain    string     `json:"chain"`
			Verified []struct {
				Block ids.ID `json:"block"`
				Err   string `json:"err"`
			} `json:"verified"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("decode event %q: %w", line, err)
		}
		if ev.Chain != "P" {
			continue
		}

		for _, v := range ev.Verified {
			if _, ok := cases[v.Block]; !ok {
				continue
			}
			if !slices.Contains(judged, v.Block) {
				judged = append(judged, v.Block)
			}
			if _, ok := took[v.Block]; !ok && v.Err == "" {
				took[v.Block] = ev.Node
			}
		}
	}

	for _, blkID := range judged {
		node, ok := took[blkID]
		if !ok {
			continue
		}

		c := cases[blkID]
		verdicts := append([]verdict{{Path: pathBlock, Node: node}}, c.mempool...)
		if err := judgePaths(verdicts); err != nil {
			return judged, fmt.Errorf("tx %s in block %s: %w", c.tx, blkID, err)
		}
	}

	return judged, nil
}

// checkPaths judges, by judgeBlocks, the cases whose blocks a node in lines
// verified, and forgets them. A block that no node verifies, such as one
// that does not parse, is never judged.
func (r *runner) checkPaths(t *rapid.T, lines []byte) {
	judged, err := judgeBlocks(lines, r.paths)
	if err != nil {
		r.fail(t, "path agreement", err)
	}
	for _, blkID := range judged {
		delete(r.paths, blkID)
	}
}

// mempoolPaths sends tx to the API of one honest node and gossips it to
// another, both drawn by t, and returns the verdict of each path. tx is the
// first tx of a block on tip that is not sent yet, so the mempools judge it
// before any node holds the block. A path's verdict counts only if its node
// was on tip's state before and after it (movedPreference): then the
// mempool ran on the block's parent state, whatever the links did. The op's
// event names each node that moved.
//
// The API's verdict is its error. Gossip has no reply, so its verdict is the
// node's GetTxStatus: Processing took the tx, Dropped refused it with the
// reason (gossipVerdict).
func (r *runner) mempoolPaths(ctx context.Context, t *rapid.T, tip parentBlock, tx *platform.Tx) []verdict {
	api := uniform(len(r.clients)).Draw(t, "apiNode")
	gossip := (api + 1 + uniform(len(r.clients)-1).Draw(t, "gossipNode")) % len(r.clients)
	if other := r.movedPreference(ctx, t, tip, tx.ID(), api, gossip); other != "" {
		r.ev.Append("paths_skipped", "before: "+other)
		return nil
	}

	var verdicts []verdict
	v := verdict{Path: pathAPI, Node: r.nodeIDs[api]}
	if err := r.issueAPI(ctx, t, api, tx); err != nil {
		v.Refusal = err.Error()
	}
	// A node that takes the tx can build a block on tip with it in its slot,
	// and then prefers that block.
	if other := r.movedPreference(ctx, t, tip, tx.ID(), api); other != "" {
		r.ev.Append("paths_skipped", "after the API: "+other)
	} else {
		verdicts = append(verdicts, v)
	}

	require.NoError(t, r.gossipers[gossip].gossipTx(ctx, tx))
	reason, ok := r.gossipVerdict(ctx, t, gossip, tx.ID())
	if !ok {
		return verdicts
	}
	if other := r.movedPreference(ctx, t, tip, tx.ID(), gossip); other != "" {
		r.ev.Append("paths_skipped", "after gossip: "+other)
		return verdicts
	}

	return append(verdicts, verdict{Path: pathGossip, Node: r.nodeIDs[gossip], Refusal: reason})
}

// gossipVerdict waits for node i to judge txID, which a peer gossiped to it,
// and returns the drop reason, or "" if the node took the tx. It returns
// false if the node shows no verdict within gossipWait.
func (r *runner) gossipVerdict(ctx context.Context, t *rapid.T, i int, txID ids.ID) (string, bool) {
	// A node takes a pushed tx in milliseconds on loopback.
	const gossipWait = 2 * time.Second

	// The RPCs take ctx, so the bound ends the wait and never fails an RPC.
	waitCtx, cancel := context.WithTimeout(ctx, gossipWait)
	defer cancel()

	var reason string
	judged := r.sinks[i].wait(waitCtx, func() bool {
		res, err := r.clients[i].GetTxStatus(ctx, txID)
		require.NoError(t, err)

		reason = res.Reason
		return res.Status == status.Processing || res.Status == status.Dropped
	})

	return reason, judged
}

// movedPreference names the first of nodes whose preferred P-Chain block,
// as it tells the proposer's peer, shows that the node may have left tip's
// state, or returns "" if none does. A node that prefers tip is on tip's
// state. So was one that prefers a block on tip that holds txID: it built
// that block from its mempool, on tip, after it took the tx.
func (r *runner) movedPreference(ctx context.Context, t *rapid.T, tip parentBlock, txID ids.ID, nodes ...int) string {
	for _, i := range nodes {
		preferred, err := r.proposer.peers[i].preferred(ctx, constants.PlatformChainID)
		require.NoError(t, err)
		if preferred == tip.id {
			continue
		}

		blk, err := r.proposer.parent(ctx, i, preferred)
		require.NoError(t, err)

		holds := slices.ContainsFunc(blk.inner.Txs(), func(tx *platform.Tx) bool { return tx.ID() == txID })
		if blk.inner.Parent() != tip.inner.ID() || !holds {
			return fmt.Sprintf("node %d prefers %s", i, preferred)
		}
	}

	return ""
}
