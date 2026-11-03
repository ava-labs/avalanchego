// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	pvmblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
	psigner "github.com/ava-labs/avalanchego/wallet/chain/p/signer"
	pwallet "github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
)

// TestByzantineGossip checks that a tx that a Byzantine peer gossips to one
// honest node goes through the real p2p stack into the mempool, and that
// every honest node then accepts it.
func TestByzantineGossip(t *testing.T) {
	p := &proxy{}
	t.Cleanup(p.Close)
	nodes, _, key := startNodes(t, keptTempDir(t), 1, p)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	b, err := connectByzantine(ctx, &p.net, nodes[0].StakingAddress())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, b.close(context.WithoutCancel(ctx)))
	})

	wallet, err := primary.MakePWallet(ctx, nodes[0].APIURI(), secp256k1fx.NewKeychain(key), primary.WalletConfig{})
	require.NoError(t, err)

	utx, err := wallet.Builder().NewBaseTx(
		[]*avax.TransferableOutput{{
			Asset: avax.Asset{ID: wallet.Builder().Context().AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: units.Avax,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.Address()},
				},
			},
		}},
		common.WithContext(ctx),
	)
	require.NoError(t, err)

	tx, err := psigner.SignUnsigned(ctx, wallet.Signer(), utx)
	require.NoError(t, err)

	require.NoError(t, b.gossipTx(ctx, tx))
	for _, n := range nodes {
		require.NoError(t, platformvm.NewClient(n.APIURI()).AwaitTxAccepted(ctx, tx.ID(), 100*time.Millisecond), n.ID)
	}
}

// TestByzantineValidatorGetsBlock checks that a peer with a validator's
// staking key connects to an honest node and fetches the preferred proposervm
// block over p2p.
func TestByzantineValidatorGetsBlock(t *testing.T) {
	p := &proxy{}
	t.Cleanup(p.Close)
	nodes, byzVdrs, funded := startNodes(t, keptTempDir(t), 1, p)

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	b, err := connectValidator(ctx, &p.net, nodes[0].StakingAddress(), byzVdrs[0], false)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, b.close(context.WithoutCancel(ctx)))
	})

	// Issue a tx so that the P-Chain builds and accepts a block to fetch.
	wallet, err := primary.MakePWallet(ctx, nodes[0].APIURI(), secp256k1fx.NewKeychain(funded), primary.WalletConfig{})
	require.NoError(t, err)
	tx, err := wallet.IssueBaseTx(
		[]*avax.TransferableOutput{{
			Asset: avax.Asset{ID: wallet.Builder().Context().AVAXAssetID},
			Out:   &secp256k1fx.TransferOutput{Amt: units.Avax, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{funded.Address()}}},
		}},
		common.WithContext(ctx),
	)
	require.NoError(t, err)
	require.NoError(t, platformvm.NewClient(nodes[0].APIURI()).AwaitTxAccepted(ctx, tx.ID(), 100*time.Millisecond))

	// The last block that node 0 accepted on the P-Chain, as a proposervm ID.
	var blkID ids.ID
	require.Eventually(t, func() bool {
		accepted := nodes[0].Accepted(constants.PlatformChainID)
		if len(accepted) == 0 {
			return false
		}
		blkID = accepted[len(accepted)-1]
		return true
	}, 10*time.Second, 100*time.Millisecond)

	blockBytes, err := b.getBlock(ctx, constants.PlatformChainID, blkID)
	require.NoError(t, err)

	blk, err := pvmblock.Parse(blockBytes, constants.PlatformChainID)
	require.NoError(t, err)
	require.Equal(t, blkID, blk.ID())
}

// TestByzantineProposesBlock checks that a valid block that the Byzantine
// validator proposes in its own slot, on a node's preferred tip, is accepted
// by every honest node. Unlike the propose op (propose.go), which proposes
// only in a slot that is already its own, the test waits for its slot, and
// the proxy holds every honest link while it waits, so no honest block
// advances the tip and the block builds on a fresh parent. The block carries
// a valid transfer, so acceptance is the expected outcome.
func TestByzantineProposesBlock(t *testing.T) {
	n := startProposerNetwork(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	n.proxy.holdAll(n.addrs)
	tip, err := n.proposer.tip(ctx, 0)
	require.NoError(t, err)

	inner, err := innerBlock(tip, []*platform.Tx{n.transfer(t, 0, 0)}, time.Now())
	require.NoError(t, err)
	blk, err := n.proposer.propose(ctx, n.client, tip, inner, 30*time.Second)
	require.NoError(t, err)
	n.proxy.clearDelays()

	require.Eventually(t, func() bool {
		for _, node := range n.nodes {
			if !slices.Contains(node.Accepted(constants.PlatformChainID), blk.ID()) {
				return false
			}
		}
		return true
	}, 60*time.Second, 50*time.Millisecond, "the honest nodes did not accept the proposed block")
}

// TestByzantineBlockRefusesMint checks that a proposed block reaches honest
// block verification, which the mempool's own checks cannot shield: every
// honest node verifies a block that holds a BaseTx whose outputs exceed its
// inputs, logs the error in its events.log, and does not accept the block.
// The control is TestByzantineProposesBlock, whose block differs in its tx.
func TestByzantineBlockRefusesMint(t *testing.T) {
	n := startProposerNetwork(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	n.proxy.holdAll(n.addrs)
	tip, err := n.proposer.tip(ctx, 0)
	require.NoError(t, err)

	inner, err := innerBlock(tip, []*platform.Tx{n.transfer(t, 0, units.KiloAvax)}, time.Now())
	require.NoError(t, err)
	blk, err := n.proposer.propose(ctx, n.client, tip, inner, 30*time.Second)
	require.NoError(t, err)
	n.proxy.clearDelays()

	for _, node := range n.nodes {
		var reason string
		require.Eventually(t, func() bool {
			reason = verifyError(t, node, blk.ID())
			return reason != ""
		}, 30*time.Second, 50*time.Millisecond, "node %s did not refuse the block", node.ID)
		t.Logf("node %s refused the block: %s", node.ID, reason)
		require.NotContains(t, node.Accepted(constants.PlatformChainID), blk.ID())
	}
}

// TestByzantinePathsAgree checks path agreement on a live network. A BaseTx
// whose outputs exceed its inputs goes to node 1's API, by gossip to node 2,
// and in a block on the tip that both nodes prefer, while every honest link
// is held. Each path refuses it, and judgeBlocks passes. A block verifier
// that skips the executor's error takes the block, and judgeBlocks fails:
// the mempool kept a rule that block verification dropped.
func TestByzantinePathsAgree(t *testing.T) {
	n := startProposerNetwork(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	g, err := connectByzantine(ctx, &n.proxy.net, n.addrs[2])
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, g.close(context.WithoutCancel(ctx)))
	})

	n.proxy.holdAll(n.addrs)
	tip, err := n.proposer.tip(ctx, 0)
	require.NoError(t, err)

	tx := n.transfer(t, 0, units.KiloAvax)
	inner, err := innerBlock(tip, []*platform.Tx{tx}, time.Now())
	require.NoError(t, err)
	for _, i := range []int{1, 2} {
		preferred, err := n.proposer.peers[i].preferred(ctx, constants.PlatformChainID)
		require.NoError(t, err)
		require.Equal(t, tip.id, preferred, "node %d prefers another block", i)
	}

	_, refusal := platformvm.NewClient(n.nodes[1].APIURI()).IssueTx(ctx, tx.Bytes())
	if refusal == nil {
		t.Fatal("node 1's API took a tx that mints AVAX")
	}
	mempool := []verdict{{Path: pathAPI, Node: n.nodes[1].ID, Refusal: refusal.Error()}}

	require.NoError(t, g.gossipTx(ctx, tx))
	gossiped := platformvm.NewClient(n.nodes[2].APIURI())
	require.Eventually(t, func() bool {
		res, err := gossiped.GetTxStatus(ctx, tx.ID())
		require.NoError(t, err)
		if res.Status != status.Dropped {
			return false
		}
		mempool = append(mempool, verdict{Path: pathGossip, Node: n.nodes[2].ID, Refusal: res.Reason})
		return true
	}, 10*time.Second, 20*time.Millisecond, "node 2 did not drop the gossiped tx")
	t.Logf("mempool verdicts: %+v", mempool)

	blk, err := n.proposer.propose(ctx, n.client, tip, inner, 30*time.Second)
	require.NoError(t, err)
	n.proxy.clearDelays()

	cases := map[ids.ID]pathCase{blk.ID(): {tx: tx.ID(), mempool: mempool}}
	var judgeErr error
	require.Eventually(t, func() bool {
		var judged []ids.ID
		judged, judgeErr = judgeBlocks(nodeEventLines(t, n.nodes), cases)
		return len(judged) > 0
	}, 30*time.Second, 50*time.Millisecond, "no node verified the block")
	require.NoError(t, judgeErr)
}

// nodeEventLines returns the lines of every node's events.log.
func nodeEventLines(t *testing.T, nodes []*testNode) []byte {
	var lines []byte
	for _, node := range nodes {
		lines = append(lines, eventLines(t, node)...)
	}

	return lines
}

// eventLines returns the lines of node's events.log, after it writes the
// lines that node logged since the last flush (testNode.flushEvents).
func eventLines(t *testing.T, node *testNode) []byte {
	_, err := node.flushEvents()
	require.NoError(t, err)

	b, err := os.ReadFile(node.eventsPath)
	require.NoError(t, err)

	return b
}

// A proposerNetwork is a network of honest nodes and a Byzantine validator
// that proposes blocks to them, with two funded actors.
type proposerNetwork struct {
	nodes    []*testNode
	addrs    []netip.AddrPort
	proxy    *proxy
	proposer *proposer
	client   *platformvm.Client // node 0's API
	actors   []ids.ShortID
	wallets  []pwallet.Wallet // the actors' wallets, through node 0
}

// startProposerNetwork starts the honest nodes and the proposer, and funds
// the actors with a tx that every node accepts, so the chain's tip is a
// proposervm block.
func startProposerNetwork(t *testing.T) *proposerNetwork {
	p := &proxy{}
	t.Cleanup(p.Close)
	nodes, byzVdrs, funded := startNodes(t, keptTempDir(t), 1, p)

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	n := &proposerNetwork{nodes: nodes, proxy: p, client: platformvm.NewClient(nodes[0].APIURI())}
	for _, node := range nodes {
		n.addrs = append(n.addrs, node.StakingAddress())
	}
	prop, err := connectProposer(ctx, &p.net, n.addrs, byzVdrs[0])
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, prop.close(context.WithoutCancel(ctx)))
	})
	n.proposer = prop

	funder, err := primary.MakePWallet(ctx, nodes[0].APIURI(), secp256k1fx.NewKeychain(funded), primary.WalletConfig{})
	require.NoError(t, err)
	var (
		keys []*secp256k1.PrivateKey
		outs []*avax.TransferableOutput
	)
	for range 2 {
		key, err := secp256k1.NewPrivateKey()
		require.NoError(t, err)
		keys = append(keys, key)
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: funder.Builder().Context().AVAXAssetID},
			Out:   &secp256k1fx.TransferOutput{Amt: units.MegaAvax, OutputOwners: *owner(key.Address())},
		})
	}
	avax.SortTransferableOutputs(outs, platform.Codec)
	seed, err := funder.IssueBaseTx(outs, common.WithContext(ctx))
	require.NoError(t, err)
	for _, node := range nodes {
		require.NoError(t, platformvm.NewClient(node.APIURI()).AwaitTxAccepted(ctx, seed.ID(), 20*time.Millisecond))
	}

	for _, key := range keys {
		w, err := primary.MakePWallet(ctx, nodes[0].APIURI(), secp256k1fx.NewKeychain(key), primary.WalletConfig{})
		require.NoError(t, err)
		n.actors = append(n.actors, key.Address())
		n.wallets = append(n.wallets, w)
	}

	return n
}

// transfer returns a signed BaseTx that pays 1 AVAX from actor i to itself,
// with mint more in its outputs than its inputs hold.
func (n *proposerNetwork) transfer(t *testing.T, i int, mint uint64) *platform.Tx {
	t.Helper()

	w := n.wallets[i]
	utx, err := w.Builder().NewBaseTx([]*avax.TransferableOutput{{
		Asset: avax.Asset{ID: w.Builder().Context().AVAXAssetID},
		Out:   &secp256k1fx.TransferOutput{Amt: units.Avax, OutputOwners: *owner(n.actors[i])},
	}}, common.WithContext(t.Context()))
	require.NoError(t, err)

	utx.Outs[0].Out.(*secp256k1fx.TransferOutput).Amt += mint
	avax.SortTransferableOutputs(utx.Outs, platform.Codec)
	tx, err := psigner.SignUnsigned(t.Context(), w.Signer(), utx)
	require.NoError(t, err)

	return tx
}

// verifyError returns the error with which node's events.log shows that it
// failed to verify blkID, or "" if it shows none.
func verifyError(t *testing.T, node *testNode, blkID ids.ID) string {
	for _, ev := range pChainEvents(t, node) {
		for _, v := range ev.Verified {
			if v.Block == blkID && v.Err != "" {
				return v.Err
			}
		}
	}

	return ""
}

// A blockEvent is a block in a node's event: one it built or verified.
type blockEvent struct {
	Block  ids.ID `json:"block"`
	Height uint64 `json:"height"`
	Err    string `json:"err"`
}

// pChainEvents returns the blocks that each P-Chain event in node's
// events.log built and verified.
func pChainEvents(t *testing.T, node *testNode) []struct{ Built, Verified []blockEvent } {
	var evs []struct{ Built, Verified []blockEvent }
	for line := range bytes.Lines(eventLines(t, node)) {
		if !bytes.Contains(line, []byte(`"built":`)) && !bytes.Contains(line, []byte(`"verified":`)) {
			continue
		}

		var ev struct {
			Chain    string       `json:"chain"`
			Built    []blockEvent `json:"built"`
			Verified []blockEvent `json:"verified"`
		}
		require.NoError(t, json.Unmarshal(line, &ev))
		if ev.Chain != "P" {
			continue
		}
		evs = append(evs, struct{ Built, Verified []blockEvent }{ev.Built, ev.Verified})
	}

	return evs
}
