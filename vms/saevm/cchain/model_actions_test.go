// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"

	evmdatabase "github.com/ava-labs/avalanchego/vms/evm/database"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	ethparams "github.com/ava-labs/libevm/params"
)

func (mm *modelMachine) actions() map[string]func(*rapid.T) {
	return map[string]func(*rapid.T){
		"buildBlock":   mm.buildBlock,
		"advanceClock": mm.advanceClock,
		"settle":       mm.settle,
		"":             mm.check,
	}
}

// advanceToBuildable moves the mock clock to the preference's earliest
// buildable time. MUST run before anything that reaches WaitForEvent: the
// VM's waitUntil computes its sleep from vm.now() once and then sleeps in
// real time, so a lagging mock clock stalls the test for real.
func (mm *modelMachine) advanceToBuildable() {
	earliest := earliestBuildTime(mm.sut.VM.VM.GetPreference())
	if mm.clock.Now().Before(earliest) {
		mm.clock.Set(earliest)
	}
}

// issueMinimalTransfer funds a block when nothing is pending (the VM refuses
// empty blocks): a zero-value self-transfer from the richest account.
func (mm *modelMachine) issueMinimalTransfer(rt *rapid.T) {
	richest := mm.addrs[0]
	richestIdx := 0
	for i, addr := range mm.addrs {
		if mm.m.balances[addr].Cmp(mm.m.balances[richest]) > 0 {
			richest, richestIdx = addr, i
		}
	}
	data := &types.DynamicFeeTx{
		To:        &richest,
		Gas:       ethparams.TxGas,
		GasFeeCap: big.NewInt(txGasFeeCap),
	}
	ethTx := mm.wallet.SetNonceAndSign(mm.tb, richestIdx, data)
	require.NoErrorf(rt, mm.sut.ethclient.SendTransaction(mm.ctx, ethTx), "SendTransaction(minimal transfer)")
	mm.trackPending(ethTx, &issuedTx{
		kind:  kindTransfer,
		from:  richest,
		to:    richest,
		value: new(uint256.Int),
		cost:  uint256.NewInt(ethparams.TxGas * txGasFeeCap),
	})
}

func (mm *modelMachine) trackPending(ethTx *types.Transaction, it *issuedTx) {
	mm.m.pendingEth[ethTx.Hash()] = it
	mm.m.pendingCost[it.from].Add(mm.m.pendingCost[it.from], it.cost)
	mm.pendingEthTxs = append(mm.pendingEthTxs, ethTx)
}

// spendable is the model's view of what addr can still commit to new txs.
func (mm *modelMachine) spendable(addr common.Address) *uint256.Int {
	s := new(uint256.Int).Set(mm.m.balances[addr])
	if pc := mm.m.pendingCost[addr]; pc != nil && s.Cmp(pc) >= 0 {
		s.Sub(s, pc)
	} else if pc != nil {
		s.Clear()
	}
	return s
}

func (mm *modelMachine) buildBlock(rt *rapid.T) {
	if len(mm.m.pendingEth) == 0 {
		mm.issueMinimalTransfer(rt)
	}
	blk := mm.buildVerifyAcceptExecute(rt)
	mm.applyBlock(rt, blk)
}

// buildVerifyAcceptExecute drives one full consensus round and waits for
// execution, so LastExecutedState reflects the new block for the model
// comparison. Task 10 adds bounded errExecutionLagging recovery here.
func (mm *modelMachine) buildVerifyAcceptExecute(rt *rapid.T) *blocks.Block {
	blockCtx := &block.Context{}
	lastAccepted, err := mm.sut.LastAccepted(mm.ctx)
	require.NoErrorf(rt, err, "%T.LastAccepted()", mm.sut.VM)
	require.NoErrorf(rt, mm.sut.SetPreference(mm.ctx, lastAccepted, blockCtx), "%T.SetPreference()", mm.sut.VM)

	mm.advanceToBuildable()
	mm.sut.waitForPendingEthTxs(mm.ctx, mm.tb, mm.pendingEthTxs...)
	mm.sut.waitForPendingTxs(mm.ctx, mm.tb)

	blk, err := mm.sut.BuildBlock(mm.ctx, blockCtx)
	require.NoErrorf(rt, err, "%T.BuildBlock()", mm.sut.VM)
	require.NoErrorf(rt, mm.sut.VerifyBlock(mm.ctx, blockCtx, blk), "%T.VerifyBlock()", mm.sut.VM)
	require.NoErrorf(rt, mm.sut.AcceptBlock(mm.ctx, blk), "%T.AcceptBlock()", mm.sut.VM)
	require.NoErrorf(rt, blk.WaitUntilExecuted(mm.ctx), "%T.WaitUntilExecuted()", blk)
	return blk
}

// applyBlock advances the model by one accepted block: dynamic-parameter
// ramps, height, and per-tx effects reconciled against receipts.
func (mm *modelMachine) applyBlock(rt *rapid.T, blk *blocks.Block) {
	m := mm.m

	// Dynamic-parameter ramp: an independent recomputation of Toward.
	m.target = m.target.Toward(m.desiredTarget)
	m.price = m.price.Toward(m.desiredPrice)
	m.delay = m.delay.Toward(m.desiredDelay)
	he := customtypes.GetHeaderExtra(blk.Header())
	require.NotNilf(rt, he.TargetExponent, "block %d TargetExponent extra", blk.NumberU64())
	require.Equalf(rt, m.target, *he.TargetExponent, "block %d TargetExponent ramp", blk.NumberU64())
	require.NotNilf(rt, he.MinPriceExponent, "block %d MinPriceExponent extra", blk.NumberU64())
	require.Equalf(rt, m.price, *he.MinPriceExponent, "block %d MinPriceExponent ramp", blk.NumberU64())
	require.NotNilf(rt, he.MinDelayExcess, "block %d MinDelayExcess extra", blk.NumberU64())
	require.Equalf(rt, m.delay, dynamic.DelayExponent(*he.MinDelayExcess), "block %d MinDelayExcess ramp", blk.NumberU64())

	// Heights.
	require.Equalf(rt, m.lastAcceptedHeight+1, blk.NumberU64(), "accepted block height")
	m.lastAccepted = blk
	m.lastAcceptedID = blk.ID()
	m.lastAcceptedHeight = blk.NumberU64()

	// Gas-time monotonicity (compare via AsTime — stable across the
	// gastime/proxytime API surface).
	gt := blk.ExecutedByGasTime()
	if m.lastGasTime != nil {
		require.Falsef(rt, gt.AsTime().Before(m.lastGasTime.AsTime()), "ExecutedByGasTime went backward at block %d", blk.NumberU64())
	}
	m.lastGasTime = gt

	// Per-tx effects, driven by what the block actually included.
	receipts := make(map[common.Hash]*types.Receipt, len(blk.Receipts()))
	for _, r := range blk.Receipts() {
		receipts[r.TxHash] = r
	}
	for _, ethTx := range blk.Transactions() {
		it, ok := m.pendingEth[ethTx.Hash()]
		require.Truef(rt, ok, "block %d contains unexpected tx %s", blk.NumberU64(), ethTx.Hash())
		delete(m.pendingEth, ethTx.Hash())
		m.pendingCost[it.from].Sub(m.pendingCost[it.from], it.cost)

		r, ok := receipts[ethTx.Hash()]
		require.Truef(rt, ok, "missing receipt for tx %s", ethTx.Hash())

		// Gas reconciliation: deduct actual gas charged.
		price, overflow := uint256.FromBig(r.EffectiveGasPrice)
		require.Falsef(rt, overflow, "EffectiveGasPrice overflows uint256")
		fee := new(uint256.Int).Mul(uint256.NewInt(r.GasUsed), price)
		m.balances[it.from].Sub(m.balances[it.from], fee)
		m.nonces[it.from]++

		mm.applyTxEffects(rt, it, r)
	}
	// Task 8 extends this with atomic (cross-chain) txs from blockTxs(blk).

	// Drop pending txs from the machine's wait-list once included.
	included := make(map[common.Hash]bool, len(blk.Transactions()))
	for _, ethTx := range blk.Transactions() {
		included[ethTx.Hash()] = true
	}
	kept := mm.pendingEthTxs[:0]
	for _, ethTx := range mm.pendingEthTxs {
		if !included[ethTx.Hash()] {
			kept = append(kept, ethTx)
		}
	}
	mm.pendingEthTxs = kept
}

// applyTxEffects applies kind-specific model updates for an included tx.
func (mm *modelMachine) applyTxEffects(rt *rapid.T, it *issuedTx, r *types.Receipt) {
	switch it.kind {
	case kindTransfer:
		require.Equalf(rt, types.ReceiptStatusSuccessful, r.Status, "transfer receipt status")
		mm.m.balances[it.from].Sub(mm.m.balances[it.from], it.value)
		mm.m.balances[it.to].Add(mm.m.balances[it.to], it.value)
	}
}

func (mm *modelMachine) advanceClock(rt *rapid.T) {
	var d time.Duration
	if rapid.IntRange(0, 9).Draw(rt, "isStall") == 0 {
		// Rare multi-Tau jump: the "GC stall" / slow-processing scenario.
		d = time.Duration(rapid.Int64Range(int64(saeparams.Tau), int64(10*saeparams.Tau)).Draw(rt, "stall"))
	} else {
		d = time.Duration(rapid.Int64Range(int64(time.Millisecond), int64(2*time.Second)).Draw(rt, "tick"))
	}
	mm.clock.Advance(d)
}

func (mm *modelMachine) settle(_ *rapid.T) {
	if mm.m.lastAccepted == nil {
		return
	}
	mm.clock.AdvanceToSettle(mm.ctx, mm.tb, mm.m.lastAccepted)
}

// check is the rapid invariant action, run around every other action.
func (mm *modelMachine) check(rt *rapid.T) {
	got, err := mm.sut.LastAccepted(mm.ctx)
	require.NoErrorf(rt, err, "%T.LastAccepted()", mm.sut.VM)
	require.Equalf(rt, mm.m.lastAcceptedID, got, "last accepted ID")

	state, err := mm.sut.LastExecutedState()
	require.NoErrorf(rt, err, "%T.LastExecutedState()", mm.sut.VM)
	for addr, want := range mm.m.balances {
		require.Equalf(rt, *want, *state.GetBalance(addr), "balance of %s", addr)
		require.Equalf(rt, mm.m.nonces[addr], state.GetNonce(addr), "nonce of %s", addr)
	}
	mm.checkRawdbPointers(rt)
	// Later tasks extend: contract storage (6), exported UTXOs (8).
}

// checkRawdbPointers spot-checks invariants.md pointer discipline on the
// persisted chain: Finalized (settled) ≤ Head (executed) ≤ HeadFast
// (accepted) along the canonical chain.
func (mm *modelMachine) checkRawdbPointers(rt *rapid.T) {
	ethDB := rawdb.NewDatabase(evmdatabase.New(prefixdb.NewNested(ethDBPrefix, prefixdb.New([]byte("chain"), mm.db))))

	heightOf := func(name string, hash common.Hash) uint64 {
		if hash == (common.Hash{}) {
			return 0 // pointer not written yet (genesis-only chain)
		}
		n := rawdb.ReadHeaderNumber(ethDB, hash)
		require.NotNilf(rt, n, "rawdb header number for %s pointer %s", name, hash)
		return *n
	}
	finalized := heightOf("finalized", rawdb.ReadFinalizedBlockHash(ethDB))
	head := heightOf("head", rawdb.ReadHeadBlockHash(ethDB))
	headFast := heightOf("head-fast", rawdb.ReadHeadFastBlockHash(ethDB))
	require.LessOrEqualf(rt, finalized, head, "rawdb Finalized (settled) vs Head (executed)")
	require.LessOrEqualf(rt, head, headFast, "rawdb Head (executed) vs HeadFast (accepted)")
}
