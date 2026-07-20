// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"errors"
	"slices"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

func (nm *networkedMachine) actions() map[string]func(*rapid.T) {
	return map[string]func(*rapid.T){
		// Duplicate keys weight the common actions up, mirroring how the
		// generators weight sample sets.
		"issueTx":             nm.issueTx,
		"issueTx2":            nm.issueTx,
		"issueTx3":            nm.issueTx,
		"buildAndDistribute":  nm.buildAndDistribute,
		"buildAndDistribute2": nm.buildAndDistribute,
		"advanceClock":        nm.advanceClock,
		"settle":              nm.settle,
		"":                    nm.check,
	}
}

// issueTx issues one randomized eth tx (transfer or contract op) from a drawn
// account to a drawn node. An account with txs already in flight is pinned to
// the node that received them (see networkedMachine.pins).
func (nm *networkedMachine) issueTx(rt *rapid.T) {
	fromIdx := rapid.IntRange(0, len(nm.addrs)-1).Draw(rt, "from")
	from := nm.addrs[fromIdx]
	nodeIdx, pinned := nm.pins[from]
	if !pinned {
		nodeIdx = rapid.IntRange(0, len(nm.nodes)-1).Draw(rt, "node")
	}
	n := nm.nodes[nodeIdx]

	before := len(nm.m.pendingEth)
	kind := rapid.SampledFrom([]txKind{
		kindTransfer, kindTransfer, kindTransfer, kindDeploy, kindStore, kindRevert,
	}).Draw(rt, "kind")
	switch kind {
	case kindTransfer:
		nm.modelCore.issueTransfer(rt, n.ctx, n.sut, fromIdx)
	case kindDeploy:
		nm.modelCore.issueDeploy(rt, n.ctx, n.sut, fromIdx)
	case kindStore:
		nm.modelCore.issueStore(rt, n.ctx, n.sut, fromIdx)
	case kindRevert:
		nm.modelCore.issueRevert(rt, n.ctx, n.sut, fromIdx)
	}
	if len(nm.m.pendingEth) == before {
		return // rejected negative or capacity no-op: nothing entered the pool
	}
	nm.pins[from] = nodeIdx
	// Admission sync (subscription-based, not timed): the tx must become
	// pending on the node it was issued to before the machine moves on.
	n.sut.waitForPendingEthTxs(n.ctx, nm.tb, nm.pendingEthTxs[len(nm.pendingEthTxs)-1])
}

// advanceToBuildable moves the shared mock clock to n's preference's earliest
// buildable time. MUST run before anything that reaches WaitForEvent (see the
// single-node machine's advanceToBuildable).
func (nm *networkedMachine) advanceToBuildable(n *modelNode) {
	earliest := earliestBuildTime(n.sut.VM.VM.GetPreference())
	if nm.clock.Now().Before(earliest) {
		nm.clock.Set(earliest)
	}
}

// buildOn builds and verifies (but does not accept) a block on n atop
// parentID, with the same bounded errEmptyBlock / ErrExecutionLagging
// recovery as the single-node machine. The pre-build sync waits until n's
// pool holds every model-tracked in-flight tx, which is what makes the block
// contents a function of model state: txs reach n via real push/pull gossip.
func (nm *networkedMachine) buildOn(rt *rapid.T, n *modelNode, parentID ids.ID) *blocks.Block {
	blockCtx := &block.Context{}
	require.NoErrorf(rt, n.sut.SetPreference(n.ctx, parentID, blockCtx), "%T.SetPreference() on builder %d", n.sut.VM, n.idx)

	nm.advanceToBuildable(n)
	n.sut.waitForPendingEthTxs(n.ctx, nm.tb, nm.pendingEthTxs...)
	n.sut.waitForPendingTxs(n.ctx, nm.tb)

	const maxBuildAttempts = 5
	var blk *blocks.Block
	for attempt := 1; ; attempt++ {
		var err error
		blk, err = n.sut.BuildBlock(n.ctx, blockCtx)
		if err == nil {
			break
		}
		require.Lessf(rt, attempt, maxBuildAttempts, "BuildBlock on node %d never recovered after %d attempts: %v", n.idx, attempt, err)
		switch {
		case errors.Is(err, errEmptyBlock):
			// Worst-case building validates spendability against the last-
			// SETTLED state (ACP-194); settle the tip to unlock unsettled
			// credits. See the single-node machine for the full analysis.
			require.NotNilf(rt, nm.m.lastAccepted, "%T.BuildBlock() returned errEmptyBlock with nothing accepted to settle", n.sut.VM)
			nm.settle(rt)
		case errors.Is(err, sae.ErrExecutionLagging):
			if nm.m.lastAccepted != nil {
				require.NoErrorf(rt, nm.m.lastAccepted.WaitUntilExecuted(n.ctx), "%T.WaitUntilExecuted() during lag recovery", nm.m.lastAccepted)
			}
		default:
			require.NoErrorf(rt, err, "%T.BuildBlock() attempt %d on node %d: want errors.Is(err, errEmptyBlock) or errors.Is(err, sae.ErrExecutionLagging)", n.sut.VM, attempt, n.idx)
		}
		nm.advanceToBuildable(n)
	}
	require.NoErrorf(rt, n.sut.VerifyBlock(n.ctx, blockCtx, blk), "%T.VerifyBlock() on builder %d", n.sut.VM, n.idx)
	return blk
}

// deliverBlock plays the consensus engine for one node: parse the canonical
// bytes, verify, accept, and wait for execution.
func (nm *networkedMachine) deliverBlock(rt *rapid.T, n *modelNode, ab acceptedBlock) {
	blk, err := n.sut.ParseBlock(n.ctx, ab.bytes)
	require.NoErrorf(rt, err, "%T.ParseBlock() on node %d", n.sut.VM, n.idx)
	require.Equalf(rt, ab.id, blk.ID(), "parsed block ID on node %d", n.idx)
	require.NoErrorf(rt, n.sut.VerifyBlock(n.ctx, &block.Context{}, blk), "%T.VerifyBlock() on node %d", n.sut.VM, n.idx)
	require.NoErrorf(rt, n.sut.AcceptBlock(n.ctx, blk), "%T.AcceptBlock() on node %d", n.sut.VM, n.idx)
	require.NoErrorf(rt, blk.WaitUntilExecuted(n.ctx), "%T.WaitUntilExecuted() on node %d", blk, n.idx)
	n.acceptedCount++
}

// applyCanonical records blk (built by builder) as the next canonical block
// and updates the shared model. The networked suite issues no cross-chain
// txs, so unlike the single-node machine there is no atomic loop — only an
// assertion that the invariant holds.
func (nm *networkedMachine) applyCanonical(rt *rapid.T, builder *modelNode, blk *blocks.Block, ab acceptedBlock) {
	require.Emptyf(rt, blockTxs(nm.tb, blk), "networked model issues no atomic txs; block %d must contain none", blk.NumberU64())
	nm.modelCore.applyBlock(rt, builder.ctx, builder.sut, blk)
	nm.canonical = append(nm.canonical, ab)
	// Unpin drained accounts so they may migrate to another node.
	for _, addr := range nm.addrs {
		if _, ok := nm.pins[addr]; ok && nm.pendingCount(addr) == 0 {
			delete(nm.pins, addr)
		}
	}
}

// buildAndDistribute drives one canonical consensus round: a drawn eligible
// validator builds, accepts, and executes a block; every other non-delayed
// node then receives it in a drawn order. Delayed nodes receive nothing —
// canonical[acceptedCount:] is their implicit queue.
func (nm *networkedMachine) buildAndDistribute(rt *rapid.T) {
	builders := nm.nonDelayedValidators()
	b := builders[0]
	if len(builders) > 1 {
		b = builders[rapid.IntRange(0, len(builders)-1).Draw(rt, "builder")]
	}
	if len(nm.m.pendingEth) == 0 {
		// The VM refuses empty blocks. No pending txs means no pins (pins are
		// GC'd on drain), so pinning the richest account to the builder is
		// always consistent.
		richestIdx := nm.issueMinimalTransfer(rt, b.ctx, b.sut)
		nm.pins[nm.addrs[richestIdx]] = b.idx
	}

	blk := nm.buildOn(rt, b, nm.tipID())
	require.NoErrorf(rt, b.sut.AcceptBlock(b.ctx, blk), "%T.AcceptBlock() on builder %d", b.sut.VM, b.idx)
	require.NoErrorf(rt, blk.WaitUntilExecuted(b.ctx), "%T.WaitUntilExecuted() on builder %d", blk, b.idx)
	b.acceptedCount++
	ab := acceptedBlock{id: blk.ID(), height: blk.NumberU64(), bytes: blk.Bytes()}
	nm.applyCanonical(rt, b, blk, ab)

	rest := make([]int, 0, len(nm.nodes)-1)
	for _, n := range nm.nodes {
		if n.idx != b.idx {
			rest = append(rest, n.idx)
		}
	}
	for len(rest) > 0 {
		k := 0
		if len(rest) > 1 {
			k = rapid.IntRange(0, len(rest)-1).Draw(rt, "deliverNext")
		}
		n := nm.nodes[rest[k]]
		rest = slices.Delete(rest, k, k+1)
		if n.delayed {
			continue
		}
		nm.deliverBlock(rt, n, ab)
	}
}

func (nm *networkedMachine) advanceClock(rt *rapid.T) {
	var d time.Duration
	if rapid.IntRange(0, 9).Draw(rt, "isStall") == 0 {
		// Rare multi-Tau jump: the "GC stall" / slow-processing scenario.
		d = time.Duration(rapid.Int64Range(int64(saeparams.Tau), int64(10*saeparams.Tau)).Draw(rt, "stall"))
	} else {
		d = time.Duration(rapid.Int64Range(int64(time.Millisecond), int64(2*time.Second)).Draw(rt, "tick"))
	}
	nm.clock.Advance(d)
}

func (nm *networkedMachine) settle(_ *rapid.T) {
	if nm.m.lastAccepted == nil {
		return
	}
	// m.lastAccepted is the builder's handle and is already executed;
	// AdvanceToSettle only reads its gas-time, and the shared clock moves for
	// every node at once.
	nm.clock.AdvanceToSettle(nm.tb.Context(), nm.tb, nm.m.lastAccepted)
}
