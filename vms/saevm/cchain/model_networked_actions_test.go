// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"errors"
	"math/big"
	"slices"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	ethparams "github.com/ava-labs/libevm/params"
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
		// partitionNetwork/healPartition get single aliases: severance costs
		// a full-network pre-sync plus three drain sweeps, and a partition's
		// lasting cost is every stranded tx it forces later builds to skip.
		"partitionNetwork": nm.partitionNetwork,
		"healPartition":    nm.healPartition,
		// minorityBuild gets a single alias: one build+verify on one node
		// with no cross-node sync waits — cheap next to buildAndDistribute —
		// and it only fires inside a partition holding fresh stranded txs,
		// already a rare state.
		"minorityBuild": nm.minorityBuild,
		// lateJoin is once-only (and a no-op in runs without a joiner), so a
		// single alias costs nothing; its lasting cost is the +1 node every
		// subsequent action pays, which the joiner-presence odds in
		// genNetworkedRunConfig keep rare.
		"lateJoin": nm.lateJoin,
		"":         nm.check,
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
		// draws to non-delayed nodes — plus, during a partition, minority
		// validators for which stranding this account is admission-safe
		// (strandSafe: balance and nonce untouched since the node's accepted
		// prefix, so neither hazard applies). delayNode's guard keeps at
		// least one validator non-delayed, so eligible is never empty.
		eligible := nm.issueTargets(from)
		nodeIdx = eligible[rapid.IntRange(0, len(eligible)-1).Draw(rt, "node")].idx
	}
	// A pinned account's node can go delayed after the pin was set (delayNode
	// has no knowledge of pins). Two hazards then apply: (i) nonce — like the
	// fresh-draw case above, a delayed node has no sync point covering its
	// pool, so a new tx can never promote to pending there; (ii) balance —
	// pool admission validates against the node's last-executed state, so
	// model-visible credits from canonical blocks the node hasn't executed
	// are invisible to it, and the model can size a value/gas draw from a
	// balance the pinned node's pool cannot yet see, spuriously rejecting
	// with core.ErrInsufficientFunds. One carve-out: a pinned minority
	// VALIDATOR stays a legal target while stranding remains admission-safe
	// (strandSafe rules out both hazards). Otherwise no-op rather than
	// issue: the condition is pure model state (pins + delayed + minority +
	// snapshots), so the draw count stays a function of model state alone,
	// preserving replay determinism.
	if pinned {
		if n := nm.nodes[nodeIdx]; n.delayed && (!nm.inMinority(nodeIdx) || !n.isValidator || !nm.strandSafe(from, n)) {
			return
		}
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
	if nm.inMinority(nodeIdx) {
		// Issued behind the partition: unreachable by majority builders
		// until heal, so majority-side sync waits must skip it (syncEthTxs)
		// and no canonical block may contain it (applyCanonical's invariant).
		nm.stranded[nm.pendingEthTxs[len(nm.pendingEthTxs)-1].Hash()] = nodeIdx
	}
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
		// per issuance against the target node. Exports are single-flight per
		// key (issueExport's pendingAtomicCount guard) and imports never
		// consume an EVM nonce, so the model's executed nonce is always
		// current here.
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
	// Builders are always majority-side; stranded txs cannot reach them
	// until heal, so the wait covers the sync set only.
	n.sut.waitForPendingEthTxs(n.ctx, nm.tb, nm.syncEthTxs()...)
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

// issueMinimalTransferUnpinned is the networked analogue of the shared
// issueMinimalTransfer: it funds a block when the builder-reachable pool is
// empty, drawing the richest account with no in-flight txs (equivalently,
// unpinned — pins are GC'd on drain). A pinned account is never a safe
// funder here: with an empty sync set its pending txs are stranded behind
// the partition, so its next nonce could never promote in the builder's
// pool. Returns the chosen account index for pinning, or -1 when every
// account is pinned (only possible while a partition strands them all).
// With no partition active, all pins imply sync-set entries, so this is
// only ever called with every account unpinned — identical behavior to
// issueMinimalTransfer.
//
//nolint:revive // context-as-argument: rt, not ctx, is this file's leading-parameter convention (mirrors *testing.T)
func (nm *networkedMachine) issueMinimalTransferUnpinned(rt *rapid.T, ctx context.Context, sut *SUT) int {
	richestIdx := -1
	for i, addr := range nm.addrs {
		if _, pinned := nm.pins[addr]; pinned {
			continue
		}
		if richestIdx == -1 || nm.m.balances[addr].Cmp(nm.m.balances[nm.addrs[richestIdx]]) > 0 {
			richestIdx = i
		}
	}
	if richestIdx == -1 {
		return -1
	}
	richest := nm.addrs[richestIdx]
	data := &types.DynamicFeeTx{
		To:        &richest,
		Gas:       ethparams.TxGas,
		GasFeeCap: big.NewInt(txGasFeeCap),
	}
	ethTx := nm.wallet.SetNonceAndSign(nm.tb, richestIdx, data)
	require.NoErrorf(rt, sut.ethclient.SendTransaction(ctx, ethTx), "SendTransaction(minimal transfer)")
	nm.trackPending(ethTx, &issuedTx{
		kind:  kindTransfer,
		from:  richest,
		to:    richest,
		value: new(uint256.Int),
		cost:  uint256.NewInt(ethparams.TxGas * txGasFeeCap),
	})
	return richestIdx
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
	// Invariant: no gossip crosses a severed link. A stranded tx inside a
	// canonical block means eth-tx gossip leaked across the partition (e.g.
	// a cross-side edge survived severance). Checked before applyBlock's
	// reconciliation, which would otherwise silently absorb the tx.
	// Detection window: healPartition clears stranded, so a leak first built
	// into a block post-heal is indistinguishable from legitimate post-heal
	// inclusion; this invariant covers leaks built while the partition is up.
	for _, ethTx := range blk.Transactions() {
		_, isStranded := nm.stranded[ethTx.Hash()]
		require.Falsef(rt, isStranded, "canonical block %s contains stranded tx %s: gossip crossed the severed partition", blk.ID(), ethTx.Hash())
	}
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
	if len(nm.syncEthTxs()) == 0 && len(nm.m.pendingAtomic) == 0 {
		// The VM refuses empty blocks, and only sync-set txs can reach the
		// builder — with every pending tx stranded behind a partition the
		// builder's pool is effectively empty. Fund the block from an
		// unpinned account; if every account is stranded, nothing can fund a
		// block, so the round no-ops (pure model state, draw-count safe).
		richestIdx := nm.issueMinimalTransferUnpinned(rt, b.ctx, b.sut)
		if richestIdx < 0 {
			return
		}
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
	// A minority node's lag is the partition's doing: delivering its queue
	// would carry blocks across the severed cut. It becomes catchable the
	// moment healPartition dissolves the overlay.
	if !n.delayed || nm.inMinority(idx) {
		return
	}
	// hadProgress records whether the loop below will run at all. When it
	// does and the in-loop resolution arm is intact, the arm fires on the
	// loop's first iteration (see comment below) and nils the fork before
	// the loop exits — so the sweep below can only legally observe a
	// surviving fork when hadProgress is false.
	hadProgress := n.acceptedCount < len(nm.canonical)
	for n.acceptedCount < len(nm.canonical) {
		nm.deliverBlock(rt, n, nm.canonical[n.acceptedCount])
		// Fork resolution: the canonical block just accepted (deliverBlock
		// bumped acceptedCount) is the doomed root's sibling — the engine's
		// cue to reject the competing chain, root-first (rejection cascades
		// from a decided competitor down through its processing
		// descendants). Because the fork roots on the node's prefix as
		// frozen at partition time and nothing was delivered since, this
		// fires on the loop's first iteration. Handles are valid: restartNode
		// drops any fork before destroying the VM instance they are bound to.
		if len(n.fork) > 0 && n.fork[0].height == nm.canonical[n.acceptedCount-1].height {
			for _, db := range n.fork {
				require.NoErrorf(rt, n.sut.RejectBlock(n.ctx, db.blk), "%T.RejectBlock(doomed block at height %d) on node %d", n.sut.VM, db.height, n.idx)
			}
			n.fork = nil
		}
	}
	// No-canonical-progress case: the partition produced no canonical blocks
	// at all, so the loop above never ran (n.acceptedCount was already
	// len(nm.canonical)) and any fork survived untouched. No sibling will
	// ever arrive at the fork root's height for THIS catch-up — n abandons
	// its fork here instead. RejectBlock is a near no-op in SAE; leaving the
	// blocks processing would strand them.
	//
	// This sweep must only ever fire for that no-progress case: if
	// hadProgress is true, the in-loop arm above should have already nilled
	// the fork on its first iteration. A fork surviving here despite
	// hadProgress means the in-loop arm is broken, not that this sweep is
	// doing legitimate work — assert on it rather than silently masking it.
	if len(n.fork) > 0 {
		require.Falsef(rt, hadProgress, "fork survived the delivery loop despite a canonical sibling at the fork root's height")
		for _, db := range n.fork {
			require.NoErrorf(rt, n.sut.RejectBlock(n.ctx, db.blk), "%T.RejectBlock(doomed block at height %d, no-sibling catch-up) on node %d", n.sut.VM, db.height, n.idx)
		}
		n.fork = nil
	}
	n.delayed = false
}

// partitionNetwork splits the network into a majority side, which keeps
// building and accepting canonical blocks, and a minority side, which
// receives no blocks and no cross-side gossip until healPartition. One
// minority flag is drawn per node — unconditionally, so the draw count is a
// function of node count alone — and the action no-ops when a partition is
// already active or the draw is illegal (empty minority, or no non-delayed
// majority validator left to build). Minority nodes become ordinary delayed
// nodes (canonical[acceptedCount:] is their implicit queue) plus the
// transport cut performed here; already-delayed nodes may land on either
// side and just keep their acceptedCount.
func (nm *networkedMachine) partitionNetwork(rt *rapid.T) {
	if nm.partitionActive() {
		return
	}
	minority := make(map[int]struct{})
	for _, n := range nm.nodes {
		if rapid.Bool().Draw(rt, "minority") {
			minority[n.idx] = struct{}{}
		}
	}
	// Legality: nonempty minority, and the majority keeps at least one
	// non-delayed validator to build on. Illegal draws no-op; rapid explores
	// legal ones.
	if len(minority) == 0 {
		return
	}
	hasMajorityBuilder := false
	for _, n := range nm.nodes {
		if _, inMin := minority[n.idx]; !inMin && n.isValidator && !n.delayed {
			hasMajorityBuilder = true
			break
		}
	}
	if !hasMajorityBuilder {
		return
	}

	// Pre-sync (the restartNode pattern): every model-tracked pending tx
	// reaches every non-delayed validator BEFORE severance. Tx placement is
	// then unambiguous — exactly the txs issued to minority nodes after this
	// action are stranded — and majority builders can include every
	// pre-partition pending tx, even ones pinned to a node about to go
	// minority. No partition is active here, so the sync set is the full
	// pending set.
	for _, v := range nm.nonDelayedValidators() {
		v.sut.waitForPendingEthTxs(v.ctx, nm.tb, nm.pendingEthTxs...)
		v.sut.waitForAtomicTxs(v.ctx, nm.tb, nm.pendingAtomicTxs...)
	}

	nm.minority = minority

	// Sever every cross-side edge the topology has (validator ↔ everyone,
	// non-validator ↔ validators only). VM level first: request traffic
	// (pull gossip) picks targets from the connection-tracked p2p peer set,
	// so after Disconnected neither side issues new requests across the cut.
	for _, a := range nm.nodes {
		if nm.inMinority(a.idx) {
			continue
		}
		for _, b := range nm.nodes {
			if !nm.inMinority(b.idx) || (!a.isValidator && !b.isValidator) {
				continue
			}
			require.NoErrorf(rt, a.sut.Disconnected(a.ctx, b.nodeID), "%T.Disconnected(%s) severing partition", a.sut.VM, b.nodeID)
			require.NoErrorf(rt, b.sut.Disconnected(b.ctx, a.nodeID), "%T.Disconnected(%s) severing partition", b.sut.VM, a.nodeID)
		}
	}
	// Sender level, in a race-safe order (compare restartNode's analysis):
	// (1) drain everyone — flushes every in-flight cross-side request, and
	//     handling a request spawns its response send inline, so
	// (2) drain everyone again — lands those responses while both peer maps
	//     still contain each other (avoiding the sender's unknown-peer
	//     error); Disconnected above guarantees no NEW cross-side requests
	//     spawn meanwhile.
	// (3) RemovePeer both ways — push gossip samples the sender's own peer
	//     map, so this is what actually stops pushes crossing the cut.
	// (4) drain everyone once more — flushes pushes that sampled a
	//     cross-side peer concurrently with (3); they carry only
	//     pre-partition txs, which the pre-sync already delivered
	//     everywhere.
	for range 2 {
		for _, n := range nm.nodes {
			n.sut.Sender().Drain()
		}
	}
	for _, a := range nm.nodes {
		if nm.inMinority(a.idx) {
			continue
		}
		for _, b := range nm.nodes {
			if !nm.inMinority(b.idx) || (!a.isValidator && !b.isValidator) {
				continue
			}
			a.sut.Sender().RemovePeer(b.nodeID)
			b.sut.Sender().RemovePeer(a.nodeID)
		}
	}
	for _, n := range nm.nodes {
		n.sut.Sender().Drain()
	}

	// Group lag: from here on every minority node is an ordinary delayed
	// node plus the transport cut.
	for _, n := range nm.nodes {
		if nm.inMinority(n.idx) {
			n.delayed = true
		}
	}
}

// healPartition reconnects every severed cross-side edge and dissolves the
// partition. Ex-minority nodes stay delayed with their acceptedCount intact
// — catchUpNode converges them as usual, with checkLagging asserting their
// prefix at every check in between — and stranded txs rejoin the sync set,
// so the next buildOn cannot complete until real push/pull gossip has
// carried them across the healed links to the builder (an existing sync
// point, never a timer). Makes no draws; no-op without an active partition
// (pure model state, draw-count safe).
func (nm *networkedMachine) healPartition(_ *rapid.T) {
	if !nm.partitionActive() {
		return
	}
	// Reconnect per the original topology rule: validator ↔ everyone,
	// non-validator ↔ validators only. ConnectTo re-registers both senders
	// and delivers Connected notifications both ways. Intra-side edges were
	// never severed.
	for _, n := range nm.nodes {
		if !nm.inMinority(n.idx) {
			continue
		}
		var peers []*SUT
		for _, o := range nm.nodes {
			if nm.inMinority(o.idx) || (!n.isValidator && !o.isValidator) {
				continue
			}
			peers = append(peers, o.sut)
		}
		saetest.ConnectTo(nm.tb, n.sut, peers...)
	}
	clear(nm.stranded)
	// The floor counts entries of the map just cleared; a stale floor would
	// make a re-partitioned node permanently ineligible in freshStrandedFor.
	for _, n := range nm.nodes {
		n.strandedConsumed = 0
	}
	nm.minority = nil
}

// minorityBuild has a drawn minority validator build and verify one block on
// its own fork tip — its previous doomed block, else its accepted-prefix tip
// — creating a verified block Snowman finality dooms: the sub-quorum
// minority can never accept it, and catchUpNode rejects it after heal when
// the canonical sibling at the fork root's height is accepted. The block
// deliberately never touches the model: RejectBlock is a near no-op in SAE
// and pool eviction happens only at execution, so its txs stay
// pending/stranded and the model's accounting is unchanged. Doomed CHAINS
// emerge from repeated firings interleaved with stranded issueTx. Eligibility
// (partition up, minority validator, fresh stranded tx per freshStrandedFor)
// is pure machine/model state, so draw counts stay replay-deterministic.
func (nm *networkedMachine) minorityBuild(rt *rapid.T) {
	if !nm.partitionActive() {
		return
	}
	var builders []*modelNode
	for _, n := range nm.nodes {
		if nm.inMinority(n.idx) && n.isValidator && nm.freshStrandedFor(n) {
			builders = append(builders, n)
		}
	}
	if len(builders) == 0 {
		return
	}
	b := builders[0]
	if len(builders) > 1 {
		b = builders[rapid.IntRange(0, len(builders)-1).Draw(rt, "doomedBuilder")]
	}

	// Fork tip: extend the doomed chain if one exists, else root a new fork
	// on the accepted prefix (possibly genesis). SetPreference to a
	// processing (unaccepted) block is production Snowman's normal case.
	parentID := nm.snapshots[b.acceptedCount].id
	if len(b.fork) > 0 {
		parentID = b.fork[len(b.fork)-1].id
	}
	blockCtx := &block.Context{}
	require.NoErrorf(rt, b.sut.SetPreference(b.ctx, parentID, blockCtx), "%T.SetPreference(fork tip) on minority builder %d", b.sut.VM, b.idx)
	nm.advanceToBuildable(b)

	// No pre-build sync waits: block contents never feed the model, so
	// unguaranteed extras (gossiped stranded txs from other minority
	// validators, pre-partition pool leftovers — including txs the majority
	// has since included canonically, the classic same-tx-on-both-sides fork
	// shape) are harmless coverage. The freshness guard's tx is already
	// pending here (issueTx's admission sync ran at issuance).
	const maxBuildAttempts = 5
	var blk *blocks.Block
	for attempt := 1; ; attempt++ {
		var err error
		blk, err = b.sut.BuildBlock(b.ctx, blockCtx)
		if err == nil {
			break
		}
		if errors.Is(err, sae.ErrExecutionLagging) {
			// Terminal, not recoverable: lastToSettle (sae/block_builder.go)
			// computes settleAt = blockTime − Tau and walks parent links
			// looking for an execution record. A doomed ancestor is never
			// accepted, so never enqueued to the executor and never gets
			// one — settlement can never catch up onto it, no matter how
			// many times we nm.settle(rt) the accepted prefix. This matches
			// production: a minority validator genuinely cannot extend its
			// fork past its own settlement window. Leave the fork as-is
			// (nothing appended) and report the action as a no-op.
			return
		}
		require.Lessf(rt, attempt, maxBuildAttempts, "BuildBlock (doomed) on node %d never recovered after %d attempts: %v", b.idx, attempt, err)
		// Only worst-case spendability may legally fail here: it validates
		// against the last-SETTLED state (ACP-194), and settle's shared
		// clock advance settles this node's own accepted prefix too.
		require.ErrorIsf(rt, err, errEmptyBlock, "%T.BuildBlock() (doomed) attempt %d on node %d", b.sut.VM, attempt, b.idx)
		require.NotNilf(rt, nm.m.lastAccepted, "%T.BuildBlock() (doomed) returned errEmptyBlock with nothing accepted to settle", b.sut.VM)
		nm.settle(rt)
		nm.advanceToBuildable(b)
	}
	require.NoErrorf(rt, b.sut.VerifyBlock(b.ctx, blockCtx, blk), "%T.VerifyBlock(doomed) on minority builder %d", b.sut.VM, b.idx)

	// Record only. No acceptedCount bump, no model/pins/snapshots update —
	// the very next check's checkLagging asserts exactly that (invariant 10:
	// doomed building is state-inert). The builder's VM preference is
	// deliberately left pointed at the doomed tip: harmless, since every
	// later preference consumer (buildOn, another minorityBuild, ...)
	// re-runs SetPreference before building.
	b.fork = append(b.fork, newDoomedBlock(blk))
	// Consumed floor: the SAE builder is greedy and may have just swept up
	// more than one issued-to-b stranded tx into this single block, so
	// record the current count rather than incrementing by one — otherwise
	// freshStrandedFor would judge b eligible again on txs that already sit
	// inside this doomed ancestor (see flake-investigation.md).
	b.strandedConsumed = nm.strandedCountFor(b)
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
	// anyDelayed() == false here, so this is every validator — including a
	// caught-up late joiner, which the nodes[:numValidators] prefix would
	// miss. Draw ranges match the old numValidators-based ones in runs
	// without a joiner.
	vdrs := nm.nonDelayedValidators()
	jIdx := rapid.IntRange(0, len(vdrs)-1).Draw(rt, "builderA")
	kIdx := rapid.IntRange(0, len(vdrs)-2).Draw(rt, "builderB")
	if kIdx >= jIdx {
		kIdx++
	}
	j, k := vdrs[jIdx], vdrs[kIdx]

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
	// Conservative v1: a minority node is unreachable from the majority, so
	// nothing could re-anchor its pool contents before the drop. Skip it
	// until the partition heals.
	if nm.inMinority(idx) {
		return
	}

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
		// syncVdrs are majority-side; stranded txs cannot reach them, and a
		// restarting majority node never held them anyway. Atomic txs are
		// never stranded (issueAtomicTx targets non-delayed nodes only).
		v.sut.waitForPendingEthTxs(v.ctx, nm.tb, nm.syncEthTxs()...)
		v.sut.waitForAtomicTxs(v.ctx, nm.tb, nm.pendingAtomicTxs...)
	}

	// Mirror production: peers observe the node disconnect before it goes
	// down, so gossip stops sampling it while it is unreachable. During a
	// partition all loops below are same-side (majority) only: cross-side
	// edges are already severed — n is majority (minority restarts no-op
	// above) and minority peers neither hold n in their peer maps nor may be
	// reconnected to it.
	for _, o := range nm.nodes {
		if o.idx != idx && !nm.inMinority(o.idx) {
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
		if o.idx != idx && !nm.inMinority(o.idx) {
			o.sut.Sender().Drain()
		}
	}
	for _, o := range nm.nodes {
		if o.idx != idx && !nm.inMinority(o.idx) {
			o.sut.Sender().RemovePeer(n.nodeID)
		}
	}
	for _, o := range nm.nodes {
		if o.idx != idx && !nm.inMinority(o.idx) {
			o.sut.Sender().Drain()
		}
	}
	// A processing (verified-but-unaccepted) doomed fork is memory-only: a
	// real node loses it on restart and it simply never gets rejected. Drop
	// the records — the handles are bound to the VM instance Shutdown below
	// destroys — and let the existing machinery converge the node; the
	// fork's txs live on in pools regardless (pool eviction happens only at
	// execution), like any other pending txs.
	n.fork = nil

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
		if o.idx == idx || nm.inMinority(o.idx) {
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

// lateJoin brings up the run's pre-drawn joiner, if any: a brand-new node
// with a fresh database that must replay the entire canonical chain. It
// arrives delayed at acceptedCount 0 — canonical[0:] is its implicit queue,
// and checkLagging measures it against the genesis snapshot from the very
// next check — and it converges via the existing catchUpNode action, whose
// per-block deliverBlock asserts warp signability and atomic effects along
// the way. No-op when the run drew no joiner or it already joined (pure
// config + model state, so draw counts stay deterministic). Makes no draws.
func (nm *networkedMachine) lateJoin(rt *rapid.T) {
	// Deferred while a partition is up (conservative v1): the joiner would
	// need side-aware wiring. The no-op leaves nm.joined false, so the
	// action can still fire after healPartition.
	if nm.cfg.joiner == nil || nm.joined || nm.partitionActive() {
		return
	}
	nm.joined = true
	n := &modelNode{
		idx:         len(nm.nodes),
		isValidator: nm.cfg.joinerIsValidator,
		storage:     *nm.cfg.joiner,
		dataDir:     nm.tb.TempDir(),
		delayed:     true,
	}
	if n.isValidator {
		n.nodeID = nm.joinerNodeID
	} else {
		n.nodeID = ids.GenerateTestNodeID()
	}
	switch n.storage.kv {
	case kvLevelDB:
		n.dbDir = nm.tb.TempDir()
		db, err := leveldb.New(n.dbDir, nil, logging.NoLog{}, prometheus.NewRegistry())
		require.NoErrorf(nm.tb, err, "leveldb.New(%q) for late joiner", n.dbDir)
		n.db = db
		nm.tb.Cleanup(func() { _ = n.db.Close() })
	default:
		n.db = memdb.New()
	}
	nm.nodes = append(nm.nodes, n)
	nm.openNode(n.idx)

	// A brand-new store must open exactly at genesis; anything else means
	// openNode inherited state (e.g. a reused directory).
	got, err := n.sut.LastAccepted(n.ctx)
	require.NoErrorf(rt, err, "%T.LastAccepted() on late joiner (node %d)", n.sut.VM, n.idx)
	require.Equalf(rt, nm.genesisID, got, "late joiner (node %d) must open at genesis", n.idx)

	// Seed shared memory with every UTXO the harness has ever provisioned —
	// including ones the model already counts as consumed: replaying the
	// canonical chain re-applies those imports on this node, and its VM must
	// find the UTXOs present to remove them. Exported UTXOs are NOT seeded;
	// replaying the export blocks must recreate them (checkLagging and the
	// converged checks assert exactly that).
	for _, chain := range nm.remoteChains(n.sut) {
		if utxos := nm.provisionedEver[chain]; len(utxos) > 0 {
			n.sut.addUTXOs(nm.tb, n.sut.ctx.ChainID, chain, utxos...)
		}
	}

	// Original topology rule: a validator links to every live node, a
	// non-validator only to validators.
	var peers []*SUT
	for _, o := range nm.nodes[:n.idx] {
		if n.isValidator || o.isValidator {
			peers = append(peers, o.sut)
		}
	}
	saetest.ConnectTo(nm.tb, n.sut, peers...)
}
