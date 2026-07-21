// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"errors"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

func (nm *networkedMachine) actions() map[string]func(*rapid.T) {
	return map[string]func(*rapid.T){
		// Duplicate keys weight the common actions up, mirroring how the
		// generators weight sample sets. issueTx keeps all 3 aliases: it never
		// waits on cross-node gossip (the tx is submitted directly to, and
		// polled for pending-ness on, the node it targets), so it is cheap
		// regardless of weight. buildAndDistribute drops one alias: each call
		// waits for every model-tracked pending tx to reach the builder via
		// real (unmocked) push/pull gossip (see pushGossipPeriod in
		// sae/vm.go), so its relative weight is a direct real-wall-clock
		// budget knob; one alias holds the CI budget while it still runs far
		// more than the once-only actions below. issueAtomicTx keeps a single
		// alias because import/export add a real-gossip sync to every
		// subsequent buildOn (the same wall-clock knob as buildAndDistribute).
		"issueTx":            nm.issueTx,
		"issueTx2":           nm.issueTx,
		"issueTx3":           nm.issueTx,
		"issueAtomicTx":      nm.issueAtomicTx,
		"buildAndDistribute": nm.buildAndDistribute,
		"advanceClock":       nm.advanceClock,
		"settle":             nm.settle,
		"delayNode":          nm.delayNode,
		"catchUpNode":        nm.catchUpNode,
		"competingSiblings":  nm.competingSiblings,
		"restartNode":        nm.restartNode,
		"":                   nm.check,
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
		// A delayed node has no sync point covering its pool — unlike a
		// pinned node (whose every prior tx was admitted with a
		// waitForPendingEthTxs sync, so its pool holds the account's full
		// contiguous nonce history), a fresh destination's receipt of
		// earlier gossip is unguaranteed. Since a delayed node also skips
		// canonical block delivery, an unpinned account's current
		// (model-consistent) nonce can be gapped there and never promote to
		// pending, hanging waitForPendingEthTxs forever. Restrict fresh
		// draws to non-delayed nodes; delayNode's guard keeps at least one
		// validator (and hence one node) non-delayed, so eligible is never
		// empty.
		eligible := nm.nonDelayedNodes()
		nodeIdx = eligible[rapid.IntRange(0, len(eligible)-1).Draw(rt, "node")].idx
	}
	// A pinned account's node can go delayed after the pin was set (delayNode
	// has no knowledge of pins). Two hazards then apply: (i) nonce — like the
	// fresh-draw case above, a delayed node has no sync point covering its
	// pool, so a new tx can never promote to pending there; (ii) balance —
	// pool admission validates against the node's last-executed state, so
	// model-visible credits from canonical blocks the node hasn't executed
	// (because it's delayed) are invisible to it, and the model can size a
	// value/gas draw from a balance the pinned node's pool cannot yet see,
	// spuriously rejecting with core.ErrInsufficientFunds. No-op rather than
	// issue: the condition is pure model state (pins + delayed), so the draw
	// count stays a function of model state alone, preserving replay
	// determinism.
	if pinned && nm.nodes[nodeIdx].delayed {
		return
	}
	n := nm.nodes[nodeIdx]

	before := len(nm.m.pendingEth)
	kind := rapid.SampledFrom([]txKind{
		kindTransfer, kindTransfer, kindTransfer, kindDeploy, kindStore, kindRevert,
		kindWarpSend, kindWarpReceive,
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
	case kindWarpSend:
		nm.modelCore.issueWarpSend(rt, n.ctx, n.sut, fromIdx)
	case kindWarpReceive:
		nm.modelCore.issueWarpReceive(rt, n.ctx, n.sut, fromIdx)
	}
	if len(nm.m.pendingEth) == before {
		return // rejected negative or capacity no-op: nothing entered the pool
	}
	nm.pins[from] = nodeIdx
	// Admission sync (subscription-based, not timed): the tx must become
	// pending on the node it was issued to before the machine moves on.
	n.sut.waitForPendingEthTxs(n.ctx, nm.tb, nm.pendingEthTxs[len(nm.pendingEthTxs)-1])
}

// issueAtomicTx drives the cross-chain surface. provision writes one drawn
// UTXO into EVERY node's shared memory; import/export issue to a drawn
// non-delayed node, from which the tx must reach the builder via cchain's
// real atomic-tx gossip (the buildOn sync point).
func (nm *networkedMachine) issueAtomicTx(rt *rapid.T) {
	kind := rapid.SampledFrom([]string{"provision", "import", "export"}).Draw(rt, "atomicKind")
	if kind == "provision" {
		nm.modelCore.provisionUTXO(rt, nm.allSUTs()...)
		return
	}
	// Import/export admission validates against the target node's
	// last-executed state (export balance and nonce), which lags the model
	// on a delayed node; issue only to non-delayed nodes (mirrors issueTx's
	// guard). delayNode keeps at least one validator non-delayed, so
	// eligible is never empty.
	eligible := nm.nonDelayedNodes()
	target := eligible[0]
	if len(eligible) > 1 {
		target = eligible[rapid.IntRange(0, len(eligible)-1).Draw(rt, "node")]
	}
	walletFor := func(ownerIdx int) *wallet {
		// Wallets hold a node-specific Client, so they are created lazily
		// per issuance against the target node. The one-in-flight-atomic-tx-
		// per-key rule (enforced by the shared issuance methods) means the
		// model's executed nonce is always current here.
		w := newWallet(nm.atomicKeys[ownerIdx], target.sut.ctx, target.sut.Client)
		w.nonce = nm.m.nonces[nm.atomicAddrs[ownerIdx]]
		return w
	}
	switch kind {
	case "import":
		nm.modelCore.issueImport(rt, target.ctx, target.sut, nm.allSUTs(), walletFor)
	default:
		nm.modelCore.issueExport(rt, target.ctx, target.sut, walletFor)
	}
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
	n.sut.waitForAtomicTxs(n.ctx, nm.tb, nm.pendingAtomicTxs...)
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
// bytes, verify, accept, and wait for execution, then assert ab's per-node
// side effects on n.
func (nm *networkedMachine) deliverBlock(rt *rapid.T, n *modelNode, ab acceptedBlock) {
	blk, err := n.sut.ParseBlock(n.ctx, ab.bytes)
	require.NoErrorf(rt, err, "%T.ParseBlock() on node %d", n.sut.VM, n.idx)
	require.Equalf(rt, ab.id, blk.ID(), "parsed block ID on node %d", n.idx)
	require.Equalf(rt, ab.height, blk.NumberU64(), "parsed block height on node %d", n.idx)
	require.NoErrorf(rt, n.sut.VerifyBlock(n.ctx, &block.Context{}, blk), "%T.VerifyBlock() on node %d", n.sut.VM, n.idx)
	require.NoErrorf(rt, n.sut.AcceptBlock(n.ctx, blk), "%T.AcceptBlock() on node %d", n.sut.VM, n.idx)
	require.NoErrorf(rt, blk.WaitUntilExecuted(n.ctx), "%T.WaitUntilExecuted() on node %d", blk, n.idx)
	nm.assertBlockEffects(n, ab)
	n.acceptedCount++
}

// enrichBlock builds the canonical acceptedBlock for blk, capturing the
// model-tracked side effects (warp sends) the machine must later assert on
// every node that executes it. MUST run before applyCanonical: it reads the
// pending maps that reconciliation clears.
func (nm *networkedMachine) enrichBlock(blk *blocks.Block) acceptedBlock {
	ab := acceptedBlock{id: blk.ID(), height: blk.NumberU64(), bytes: blk.Bytes()}
	for _, ethTx := range blk.Transactions() {
		if it, ok := nm.m.pendingEth[ethTx.Hash()]; ok && it.kind == kindWarpSend {
			ab.warpSends = append(ab.warpSends, warpSend{from: it.from, payload: it.payload})
		}
	}
	atxs := blockTxs(nm.tb, blk)
	if len(atxs) > 0 {
		eff := &atomicBlockEffects{
			txs:      atxs,
			consumed: make(map[ids.ID][]*avax.UTXO),
			exported: make(map[ids.ID][]*avax.UTXO),
		}
		for _, atx := range atxs {
			exp, ok := nm.m.pendingAtomic[atx.ID()]
			if !ok {
				continue // applyAtomicBlockEffects fails the run on unexpected txs
			}
			if exp.isImport {
				eff.consumed[exp.remoteChain] = append(eff.consumed[exp.remoteChain], utxosOf(exp.consumed)...)
			} else {
				eff.exported[exp.remoteChain] = append(eff.exported[exp.remoteChain], exp.exported...)
			}
		}
		ab.atomic = eff
	}
	return ab
}

// assertBlockEffects asserts ab's per-node observable side effects on n,
// which must already have accepted and executed ab: every warp message the
// block sent must be signable by n's own warp backend (each node's storage
// records the message when IT executes the block).
func (nm *networkedMachine) assertBlockEffects(n *modelNode, ab acceptedBlock) {
	if ab.atomic != nil {
		// A node's pool evicts an included cross-chain tx when the node
		// executes its block; wait so a later build on this node can never
		// race a stale pool entry.
		for _, atx := range ab.atomic.txs {
			n.sut.waitForTxPoolStateUpdate(n.ctx, nm.tb, atx)
		}
		// The VM applies shared-memory ops when THIS node accepts the block:
		// consumed UTXOs must be gone from, and exported UTXOs present in,
		// n's own atomic memory. Iterate remoteChains (not the maps) for
		// deterministic assertion order.
		for _, chain := range nm.remoteChains(n.sut) {
			if us := ab.atomic.consumed[chain]; len(us) > 0 {
				n.sut.assertUTXOsMissing(nm.tb, n.sut.ctx.ChainID, chain, us...)
			}
			if us := ab.atomic.exported[chain]; len(us) > 0 {
				n.sut.assertUTXOsExist(nm.tb, chain, n.sut.ctx.ChainID, us...)
			}
		}
	}
	for _, ws := range ab.warpSends {
		msg := n.sut.newAddressedCallMessage(nm.tb, ws.from.Bytes(), ws.payload)
		n.sut.signAndVerifyWarpMessage(n.ctx, nm.tb, msg)
	}
}

// applyCanonical records blk (built by builder) as the next canonical block
// and updates the shared model, including the shared atomic (cross-chain)
// reconciliation aimed at the builder — the analogue of the single-node
// machine's applyBlock pairing.
func (nm *networkedMachine) applyCanonical(rt *rapid.T, builder *modelNode, blk *blocks.Block, ab acceptedBlock) {
	nm.modelCore.applyBlock(rt, builder.ctx, builder.sut, blk)
	nm.modelCore.applyAtomicBlockEffects(rt, builder.ctx, builder.sut, blk)
	nm.canonical = append(nm.canonical, ab)
	nm.warpSent = append(nm.warpSent, ab.warpSends...)
	// Unpin drained accounts so they may migrate to another node.
	for _, addr := range nm.addrs {
		if _, ok := nm.pins[addr]; ok && nm.pendingCount(addr) == 0 {
			delete(nm.pins, addr)
		}
	}
	nm.snapshot()
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
	if len(nm.m.pendingEth) == 0 && len(nm.m.pendingAtomic) == 0 {
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
	ab := nm.enrichBlock(blk)
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

// anyDelayed reports whether any node is currently lagging.
func (nm *networkedMachine) anyDelayed() bool {
	for _, n := range nm.nodes {
		if n.delayed {
			return true
		}
	}
	return false
}

// delayNode marks a drawn node lagging: subsequent canonical blocks are
// withheld from it until catchUpNode. Refuses to delay the last buildable
// validator.
func (nm *networkedMachine) delayNode(rt *rapid.T) {
	idx := rapid.IntRange(0, len(nm.nodes)-1).Draw(rt, "node")
	n := nm.nodes[idx]
	if n.delayed {
		return
	}
	if n.isValidator && len(nm.nonDelayedValidators()) == 1 {
		return // at least one buildable validator must remain
	}
	n.delayed = true
}

// catchUpNode delivers a lagging node's withheld canonical blocks in order
// and clears its lag.
func (nm *networkedMachine) catchUpNode(rt *rapid.T) {
	idx := rapid.IntRange(0, len(nm.nodes)-1).Draw(rt, "node")
	n := nm.nodes[idx]
	if !n.delayed {
		return
	}
	for n.acceptedCount < len(nm.canonical) {
		nm.deliverBlock(rt, n, nm.canonical[n.acceptedCount])
	}
	n.delayed = false
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

// competingSiblings has two validators build sibling blocks on the same
// parent, verifies both on every node, then resolves: a drawn winner is
// accepted and executed everywhere, the loser rejected everywhere, in a drawn
// per-node order. Because tx-priority ties break on per-node pool-admission
// wall time, the siblings may also come out byte-identical; the draw sequence
// is the same on both paths so replays stay deterministic.
func (nm *networkedMachine) competingSiblings(rt *rapid.T) {
	if nm.anyDelayed() {
		return // siblings resolve atomically network-wide; keep queue semantics simple
	}
	jIdx := rapid.IntRange(0, nm.cfg.numValidators-1).Draw(rt, "builderA")
	kIdx := rapid.IntRange(0, nm.cfg.numValidators-2).Draw(rt, "builderB")
	if kIdx >= jIdx {
		kIdx++
	}
	j, k := nm.nodes[jIdx], nm.nodes[kIdx]

	if len(nm.m.pendingEth) == 0 && len(nm.m.pendingAtomic) == 0 {
		richestIdx := nm.issueMinimalTransfer(rt, j.ctx, j.sut)
		nm.pins[nm.addrs[richestIdx]] = j.idx
	}
	parentID := nm.tipID()
	blkA := nm.buildOn(rt, j, parentID)
	blkB := nm.buildOn(rt, k, parentID)

	// Drawn unconditionally so both branches consume the same draw stream.
	winnerA := rapid.Bool().Draw(rt, "winnerA")
	order := make([]int, 0, len(nm.nodes))
	rest := make([]int, len(nm.nodes))
	for i := range rest {
		rest[i] = i
	}
	for len(rest) > 0 {
		p := 0
		if len(rest) > 1 {
			p = rapid.IntRange(0, len(rest)-1).Draw(rt, "resolveNext")
		}
		order = append(order, rest[p])
		rest = slices.Delete(rest, p, p+1)
	}

	if blkA.ID() == blkB.ID() {
		// Degenerate: byte-identical siblings. Resolve as a normal round; the
		// builders accept their own (already verified) handles.
		ab := nm.enrichBlock(blkA)
		for _, idx := range order {
			n := nm.nodes[idx]
			switch idx {
			case j.idx:
				require.NoErrorf(rt, n.sut.AcceptBlock(n.ctx, blkA), "%T.AcceptBlock(own identical sibling) on node %d", n.sut.VM, n.idx)
				require.NoErrorf(rt, blkA.WaitUntilExecuted(n.ctx), "%T.WaitUntilExecuted() on node %d", blkA, n.idx)
				n.acceptedCount++
			case k.idx:
				require.NoErrorf(rt, n.sut.AcceptBlock(n.ctx, blkB), "%T.AcceptBlock(own identical sibling) on node %d", n.sut.VM, n.idx)
				require.NoErrorf(rt, blkB.WaitUntilExecuted(n.ctx), "%T.WaitUntilExecuted() on node %d", blkB, n.idx)
				n.acceptedCount++
			default:
				nm.deliverBlock(rt, n, ab)
			}
		}
		nm.applyCanonical(rt, j, blkA, ab)
		// non-builders went through deliverBlock and j through applyCanonical,
		// leaving only builder k.
		nm.assertBlockEffects(k, ab)
		return
	}

	// Cross-verify: every node holds ITS OWN verified handle of BOTH siblings
	// before any resolution. A blocks.Block instance is bound to the VM that
	// produced it, so a node can only accept/reject a handle it parsed (or
	// built) itself. Builders already hold+verified their own sibling from
	// buildOn and parse+verify only the competitor's.
	parseVerify := func(n *modelNode, bytes []byte, wantID ids.ID, role string) *blocks.Block {
		blk, err := n.sut.ParseBlock(n.ctx, bytes)
		require.NoErrorf(rt, err, "%T.ParseBlock(%s sibling) on node %d", n.sut.VM, role, n.idx)
		require.Equalf(rt, wantID, blk.ID(), "parsed %s sibling ID on node %d", role, n.idx)
		require.NoErrorf(rt, n.sut.VerifyBlock(n.ctx, &block.Context{}, blk), "%T.VerifyBlock(%s sibling) on node %d", n.sut.VM, role, n.idx)
		return blk
	}
	handleA := make([]*blocks.Block, len(nm.nodes))
	handleB := make([]*blocks.Block, len(nm.nodes))
	handleA[j.idx], handleB[k.idx] = blkA, blkB // verified in buildOn
	bytesA, bytesB := blkA.Bytes(), blkB.Bytes()
	for _, n := range nm.nodes {
		if handleA[n.idx] == nil {
			handleA[n.idx] = parseVerify(n, bytesA, blkA.ID(), "A")
		}
		if handleB[n.idx] == nil {
			handleB[n.idx] = parseVerify(n, bytesB, blkB.ID(), "B")
		}
	}

	wins, loses, winner, wNode := handleA, handleB, blkA, j
	if !winnerA {
		wins, loses, winner, wNode = handleB, handleA, blkB, k
	}
	wb := nm.enrichBlock(winner)

	// Resolve on every node in the drawn order: accept the winner, wait for
	// execution, reject the loser.
	for _, idx := range order {
		n := nm.nodes[idx]
		require.NoErrorf(rt, n.sut.AcceptBlock(n.ctx, wins[n.idx]), "%T.AcceptBlock(winner sibling) on node %d", n.sut.VM, n.idx)
		require.NoErrorf(rt, wins[n.idx].WaitUntilExecuted(n.ctx), "%T.WaitUntilExecuted(winner sibling) on node %d", wins[n.idx], n.idx)
		require.NoErrorf(rt, n.sut.RejectBlock(n.ctx, loses[n.idx]), "%T.RejectBlock(loser sibling) on node %d", n.sut.VM, n.idx)
		n.acceptedCount++
	}
	nm.applyCanonical(rt, wNode, winner, wb)
	// wNode is covered by applyCanonical's applyTxEffects; every other node
	// accepted its own handle above and must be able to sign the same sends.
	for _, n := range nm.nodes {
		if n.idx != wNode.idx {
			nm.assertBlockEffects(n, wb)
		}
	}
}

// restartNode shuts a drawn node down and reopens it on its persisted state.
// The shared model keeps ALL its predictions: pending txs survive on the
// other validators (synced below before the pool is dropped), and the
// restarted node's chain state must come back exactly (continuity, asserted
// here and by the post-action check).
func (nm *networkedMachine) restartNode(rt *rapid.T) {
	idx := rapid.IntRange(0, len(nm.nodes)-1).Draw(rt, "node")
	n := nm.nodes[idx]

	// Another live (non-delayed) validator must exist: it anchors the
	// pending txs while n's pool is dropped, serves pull-gossip recovery,
	// and receives any re-pinned accounts. Without one, skip.
	var syncVdrs []*modelNode
	for _, v := range nm.nonDelayedValidators() {
		if v.idx != idx {
			syncVdrs = append(syncVdrs, v)
		}
	}
	if len(syncVdrs) == 0 {
		return
	}

	// Every model-tracked pending tx must exist somewhere other than n
	// before n's pool is dropped; push/pull gossip delivers to validators.
	// Atomic txs are synced the same way as eth txs: n's own atomic-tx pool
	// entries would otherwise vanish with nothing left to answer a later
	// buildOn's waitForAtomicTxs on any node.
	for _, v := range syncVdrs {
		v.sut.waitForPendingEthTxs(v.ctx, nm.tb, nm.pendingEthTxs...)
		v.sut.waitForAtomicTxs(v.ctx, nm.tb, nm.pendingAtomicTxs...)
	}

	// Mirror production: peers observe the node disconnect before it goes
	// down, so gossip stops sampling it while it is unreachable.
	for _, o := range nm.nodes {
		if o.idx != idx {
			require.NoErrorf(rt, o.sut.Disconnected(o.ctx, n.nodeID), "%T.Disconnected(%s)", o.sut.VM, n.nodeID)
		}
	}
	// Disconnected only updates VM-side p2p trackers; saetest senders keep
	// sampling n from their own peer maps, so an in-flight gossip delivery
	// could race n's Shutdown closing its trie database. Quiesce the
	// transport, in order: (1) close n's outbound — flushing its in-flight
	// requests also guarantees peers' response goroutines toward n have been
	// spawned; (2) drain peers so those responses (and pushes) land while n
	// is still alive — draining BEFORE RemovePeer avoids the sender's
	// unknown-peer error; (3) stop peers sampling n; (4) flush deliveries
	// that sampled n concurrently with (3). openNode gives n a fresh sender
	// and ConnectTo re-registers it with every peer.
	n.sut.Sender().Close()
	for _, o := range nm.nodes {
		if o.idx != idx {
			o.sut.Sender().Drain()
		}
	}
	for _, o := range nm.nodes {
		if o.idx != idx {
			o.sut.Sender().RemovePeer(n.nodeID)
		}
	}
	for _, o := range nm.nodes {
		if o.idx != idx {
			o.sut.Sender().Drain()
		}
	}
	require.NoErrorf(rt, n.sut.Shutdown(n.ctx), "%T.Shutdown() on node %d", n.sut.VM, idx)

	if n.storage.kv == kvLevelDB {
		// The true production restart: close and reopen the store.
		require.NoErrorf(rt, n.db.Close(), "leveldb Close() on node %d restart", idx)
		db, err := leveldb.New(n.dbDir, nil, logging.NoLog{}, prometheus.NewRegistry())
		require.NoErrorf(rt, err, "leveldb.New(%q) on node %d restart", n.dbDir, idx)
		n.db = db
	}
	nm.openNode(idx)

	// Reconnect with the original topology: a validator links to every other
	// node; a non-validator only to validators.
	var peers []*SUT
	for _, o := range nm.nodes {
		if o.idx == idx {
			continue
		}
		if n.isValidator || o.isValidator {
			peers = append(peers, o.sut)
		}
	}
	saetest.ConnectTo(nm.tb, n.sut, peers...)

	// A restarted non-delayed validator re-learns its pool via pull gossip,
	// so its pins stay valid. A restarted non-validator never will (gossip
	// reaches validators only), and neither will a restarted DELAYED
	// validator: a pinned account may have nonces already included in
	// canonical blocks still withheld from it, and those txs exist in no
	// pool anywhere — pull gossip can only resupply pool contents, so the
	// node could never promote the account's next nonce and a subsequent
	// issueTx to the pin would hang forever. Re-pin such accounts to a live
	// validator, which the pre-shutdown sync guaranteed holds every pending
	// tx.
	if !n.isValidator || n.delayed {
		for _, addr := range nm.addrs {
			if pin, ok := nm.pins[addr]; ok && pin == idx {
				v := syncVdrs[0]
				if len(syncVdrs) > 1 {
					v = syncVdrs[rapid.IntRange(0, len(syncVdrs)-1).Draw(rt, "repin")]
				}
				nm.pins[addr] = v.idx
			}
		}
	}

	// Warp storage is DB-backed (cchainwarp.Storage prefixes the VM DB), so
	// every message sent by a block this node has executed must remain
	// signable across the restart. A delayed node has executed only its
	// prefix; the snapshot count scopes the assertion to it.
	for _, ws := range nm.warpSent[:nm.snapshots[n.acceptedCount].warpSentCount] {
		msg := n.sut.newAddressedCallMessage(nm.tb, ws.from.Bytes(), ws.payload)
		n.sut.signAndVerifyWarpMessage(n.ctx, nm.tb, msg)
	}

	// Continuity: the node reports the same last-accepted block it had
	// before shutdown. (Full state equality is asserted by the post-action
	// check via checkState/checkLagging.)
	wantID := nm.genesisID
	if n.acceptedCount > 0 {
		wantID = nm.canonical[n.acceptedCount-1].id
	}
	got, err := n.sut.LastAccepted(n.ctx)
	require.NoErrorf(rt, err, "%T.LastAccepted() after restart of node %d", n.sut.VM, idx)
	require.Equalf(rt, wantID, got, "node %d last accepted across restart", idx)
}
