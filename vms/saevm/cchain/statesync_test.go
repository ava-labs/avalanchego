// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"io"
	"math"
	"math/big"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	ethparams "github.com/ava-labs/libevm/params"
)

// acceptBlockWith issues stx and ethTx on s, accepts one block that MUST
// contain exactly both, then advances the shared clock so the block can settle
// once a descendant is accepted.
func (s *SUT) acceptBlockWith(ctx context.Context, tb testing.TB, stx *tx.Tx, ethTx *types.Transaction) *blocks.Block {
	tb.Helper()

	require.NoErrorf(tb, s.ethclient.SendTransaction(ctx, ethTx), "%T.SendTransaction()", s.ethclient)
	s.waitForPendingEthTxs(ctx, tb, ethTx)
	blk := s.issueAndExecute(ctx, tb, stx)
	assertBlockIncludes(tb, blk, types.Transactions{ethTx}, []*tx.Tx{stx})

	s.clock.AdvanceToSettle(ctx, tb, blk)
	return blk
}

// newEthTx returns a signed self-transfer from ethW's first account. The
// generous fee cap keeps the tx includable even after many blocks move the
// base fee.
func newEthTx(tb testing.TB, ethW *saetest.Wallet) *types.Transaction {
	tb.Helper()

	to := ethW.Addresses()[0]
	return ethW.SetNonceAndSign(tb, 0, &types.DynamicFeeTx{
		To:        &to,
		Gas:       ethparams.TxGas,
		GasFeeCap: big.NewInt(ethparams.GWei),
	})
}

// produceBlocks accepts n blocks on s, each containing a minimal export tx
// from w and a minimal eth transfer from ethW, and returns the last block and
// its cross-chain tx.
func (s *SUT) produceBlocks(ctx context.Context, t *testing.T, w *wallet, ethW *saetest.Wallet, n int) *blocks.Block {
	t.Helper()

	var blk *blocks.Block
	for range n {
		blk = s.acceptBlockWith(ctx, t, w.newMinimalTx(t), newEthTx(t, ethW))
	}
	return blk
}

// startStateSync fetches src's last state summary and parses and accepts it
// on dst, as the engine would, returning the accepted summary's height. The
// sync then proceeds in the background until [awaitStateSync] observes its
// completion.
func startStateSync(ctx context.Context, t *testing.T, src, dst *SUT) uint64 {
	t.Helper()

	// The engine fetches summaries from peers; parsing happens locally.
	summary, err := src.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", src.VM)

	parsed, err := dst.ParseStateSummary(ctx, summary.Bytes())
	require.NoErrorf(t, err, "%T.ParseStateSummary()", dst.VM)
	require.Equalf(t, summary.ID(), parsed.ID(), "%T.ParseStateSummary() round trip", dst.VM)

	mode, err := dst.AcceptSummary(ctx, parsed)
	require.NoErrorf(t, err, "%T.AcceptSummary()", dst.VM)
	require.Equalf(t, block.StateSyncStatic, mode, "%T.AcceptSummary() mode", dst.VM)
	return parsed.Height()
}

// awaitStateSync blocks until dst announces completion of a sync started by
// [startStateSync] and asserts that it succeeded.
func awaitStateSync(ctx context.Context, t *testing.T, dst *SUT) {
	t.Helper()

	msg, err := dst.WaitForEvent(ctx)
	require.NoErrorf(t, err, "%T.WaitForEvent()", dst.VM)
	require.Equalf(t, snowcommon.StateSyncDone, msg, "%T.WaitForEvent()", dst.VM)
	require.NoErrorf(t, dst.Handler.Error(), "%T.Error()", dst.Handler)
}

// bootstrapFrom transitions s to [snow.Bootstrapping], replays src's accepted
// blocks above fromHeight, and enters [snow.NormalOp], mirroring the engine's
// post-state-sync behavior.
func (s *SUT) bootstrapFrom(ctx context.Context, t *testing.T, src *SUT, fromHeight uint64) {
	t.Helper()

	require.NoErrorf(t, s.SetState(ctx, snow.Bootstrapping), "%T.SetState(Bootstrapping)", s.VM)

	head := src.lastAcceptedHeight(ctx, t)
	for height := fromHeight + 1; height <= head; height++ {
		s.parseVerifyAccept(ctx, t, src.blockAtHeight(ctx, t, height))
	}

	require.NoErrorf(t, s.SetState(ctx, snow.NormalOp), "%T.SetState(NormalOp)", s.VM)
	require.NoErrorf(t, s.SetPreference(ctx, s.lastAccepted(ctx, t), nil), "%T.SetPreference()", s.VM)
}

// assertChainsMatch asserts that s and other agree on the last-accepted block
// and its post-execution state.
func (s *SUT) assertChainsMatch(ctx context.Context, t *testing.T, other *SUT) {
	t.Helper()

	require.Equalf(t, other.lastAccepted(ctx, t), s.lastAccepted(ctx, t), "%T.LastAccepted()", s.VM)

	head := s.lastAcceptedHeight(ctx, t)
	headBlk := s.blockAtHeight(ctx, t, head)
	otherHeadBlk := other.blockAtHeight(ctx, t, head)
	require.NoErrorf(t, headBlk.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted(height %d)", headBlk, head)
	require.NoErrorf(t, otherHeadBlk.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted(height %d)", otherHeadBlk, head)

	require.Equalf(t, otherHeadBlk.PostExecutionStateRoot(), headBlk.PostExecutionStateRoot(), "%T.PostExecutionStateRoot() at height %d", headBlk, head)

	wantRoot, err := other.state.GetRoot(head)
	require.NoErrorf(t, err, "%T.GetRoot(%d)", other.state, head)
	gotRoot, err := s.state.GetRoot(head)
	require.NoErrorf(t, err, "%T.GetRoot(%d)", s.state, head)
	require.Equalf(t, wantRoot, gotRoot, "%T.GetRoot(%d)", s.state, head)

	saetest.RequireEqualDBs(t, other.sharedMemoryDB, s.sharedMemoryDB, "shared memory")
}

// TestStateSyncNewNodeJoins is the happy path: a fresh node state syncs from a
// running source VM, bootstraps the remaining blocks, and then participates in
// the network, both accepting the source's blocks and building its own.
func TestStateSyncNewNodeJoins(t *testing.T) {
	const commitInterval = 8

	key := txtest.NewKey(t)
	dstKey := txtest.NewKey(t)
	ethW := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	timeOpt, _ := withVMTime(testStartTime)
	// sharedOpts are the genesis-, clock-, and config-affecting options that
	// every peer of src MUST reuse to share src's chain.
	sharedOpts := []sutOption{
		timeOpt,
		withMaxAllocFor(key.EthAddress(), dstKey.EthAddress(), ethW.Addresses()[0]),
		withCommitInterval(commitInterval),
	}
	srcCtx, src := newSUT(t, sharedOpts...)
	w := newWallet(key, src.ctx, src.Client)

	// The export and import land safely below the summary height so the atomic
	// trie in the synced range is non-trivial.
	const (
		txFee           = 50
		utxoAmount      = 100
		_          uint = utxoAmount - txFee
	)

	// Height 1: export, giving the X-chain's shared memory observable state.
	exportTx, _ := w.newExportTx(
		t,
		snowtest.XChainID,
		txFee,
		txtest.NewTransferOutput(utxoAmount, key.Address()),
	)
	src.acceptBlockWith(srcCtx, t, exportTx, newEthTx(t, ethW))

	// Height 2: import, consuming a UTXO the simulated X-chain wrote.
	seededUTXO := txtest.NewUTXO(utxoAmount, src.ctx.AVAXAssetID, key.Address())
	src.addUTXOs(t, src.ctx.ChainID, snowtest.XChainID, seededUTXO)
	importReceiver := txtest.NewKey(t)
	importTx := w.newImportTx(srcCtx, t, snowtest.XChainID, importReceiver.EthAddress(), txFee)
	src.acceptBlockWith(srcCtx, t, importTx, newEthTx(t, ethW))

	// Fill the chain to just past the first commit boundary so
	// GetLastStateSummary snaps down to syncCommitInterval.
	src.produceBlocks(srcCtx, t, w, ethW, commitInterval)

	// A second import lands above the summary height so it is replayed during
	// bootstrapping rather than applied by the state sync.
	replayUTXO := txtest.NewUTXO(utxoAmount, src.ctx.AVAXAssetID, key.Address())
	src.addUTXOs(t, src.ctx.ChainID, snowtest.XChainID, replayUTXO)
	replayImportTx := w.newImportTx(srcCtx, t, snowtest.XChainID, importReceiver.EthAddress(), txFee)
	src.acceptBlockWith(srcCtx, t, replayImportTx, newEthTx(t, ethW))

	ctx, dst := newSUT(t, append(sharedOpts, withState(snow.StateSyncing))...)
	saetest.ConnectTo(t, dst, src)

	dst.addUTXOs(t, dst.ctx.ChainID, snowtest.XChainID, seededUTXO, replayUTXO)

	summaryHeight := startStateSync(ctx, t, src, dst)
	require.Equal(t, uint64(commitInterval), summaryHeight, "summary at last commit boundary")
	awaitStateSync(ctx, t, dst)

	dst.bootstrapFrom(ctx, t, src, summaryHeight)
	dst.assertChainsMatch(ctx, t, src)

	// dst keeps up with the network...
	blk := src.produceBlocks(srcCtx, t, w, ethW, 1)
	dst.parseVerifyAccept(ctx, t, blk)

	// ...and produces a block of its own that src accepts.
	dstW := newWallet(dstKey, dst.ctx, dst.Client)
	dstBlk := dst.acceptBlockWith(ctx, t, dstW.newMinimalTx(t), newEthTx(t, ethW))
	src.parseVerifyAccept(srcCtx, t, dstBlk)
	dst.assertChainsMatch(ctx, t, src)
}

// TestStateSyncWhileNetworkAdvances checks that a node can sync from a source
// that keeps accepting blocks: the sync lands on the older summary and
// bootstrapping replays the gap up to the source's new head.
func TestStateSyncWhileNetworkAdvances(t *testing.T) {
	const commitInterval = 4

	key := txtest.NewKey(t)
	ethW := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	timeOpt, _ := withVMTime(testStartTime)
	sharedOpts := []sutOption{
		timeOpt,
		withMaxAllocFor(key.EthAddress(), ethW.Addresses()[0]),
		withCommitInterval(commitInterval),
	}
	srcCtx, src := newSUT(t, sharedOpts...)
	w := newWallet(key, src.ctx, src.Client)

	// Fill the chain to just past the first commit boundary so
	// GetLastStateSummary snaps down to syncCommitInterval.
	src.produceBlocks(srcCtx, t, w, ethW, commitInterval+1)

	ctx, dst := newSUT(t, append(slices.Clone(sharedOpts), withState(snow.StateSyncing))...)
	saetest.ConnectTo(t, dst, src)

	summaryHeight := startStateSync(ctx, t, src, dst)
	require.Equal(t, uint64(commitInterval), summaryHeight, "summary at last commit boundary")

	src.produceBlocks(srcCtx, t, w, ethW, commitInterval)
	awaitStateSync(ctx, t, dst)

	// The network keeps advancing, past another commit boundary, before dst
	// starts bootstrapping.
	src.produceBlocks(srcCtx, t, w, ethW, commitInterval)

	dst.bootstrapFrom(ctx, t, src, summaryHeight)
	dst.assertChainsMatch(ctx, t, src)
}

// TestStateSyncDisabled checks the non-syncing path of the same wiring: a node
// with state sync disabled reports so to the engine and instead bootstraps the
// whole chain from genesis.
func TestStateSyncDisabled(t *testing.T) {
	const commitInterval = 4
	key := txtest.NewKey(t)
	ethW := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	timeOpt, _ := withVMTime(testStartTime)
	sharedOpts := []sutOption{
		timeOpt,
		withMaxAllocFor(key.EthAddress(), ethW.Addresses()[0]),
		withCommitInterval(commitInterval),
	}
	srcCtx, src := newSUT(t, sharedOpts...)
	w := newWallet(key, src.ctx, src.Client)

	src.produceBlocks(srcCtx, t, w, ethW, commitInterval+2)

	ctx, dst := newSUT(t, append(
		sharedOpts,
		withState(snow.Initializing),
		withStateSyncDisabled())...,
	)
	saetest.ConnectTo(t, dst, src)

	enabled, err := dst.StateSyncEnabled(ctx)
	require.NoErrorf(t, err, "%T.StateSyncEnabled()", dst.VM)
	require.Falsef(t, enabled, "%T.StateSyncEnabled()", dst.VM)

	dst.bootstrapFrom(ctx, t, src, 0)
	dst.assertChainsMatch(ctx, t, src)
}

// faultTolerantLogger returns a logger for a node whose state sync is expected
// to fail: library code (e.g. graft's evmstate syncer, surfaced through
// vms/saevm/libevmlog) legitimately logs at ERROR while persisting progress
// for an injected fault, which the default [loggingtest.Logger] would turn
// into a test failure and a canceled context.
func faultTolerantLogger() logging.Logger {
	return logging.NewLogger("", logging.NewWrappedCore(
		logging.Info,
		nopWriteCloser{io.Discard},
		logging.Plain.ConsoleEncoder(),
	))
}

// TestStateSyncDBFailure checks that a database failure is surfaced to the
// engine.
func TestStateSyncDBFailure(t *testing.T) {
	const commitInterval = 4

	key := txtest.NewKey(t)
	ethW := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	timeOpt, _ := withVMTime(testStartTime)
	sharedOpts := []sutOption{
		timeOpt,
		withMaxAllocFor(key.EthAddress(), ethW.Addresses()[0]),
		withCommitInterval(commitInterval),
	}
	srcCtx, src := newSUT(t, sharedOpts...)
	w := newWallet(key, src.ctx, src.Client)

	src.produceBlocks(srcCtx, t, w, ethW, commitInterval+1)

	for numOps := 0; ; numOps++ {
		flaky := saetest.NewFlakyDB(memdb.New(), math.MaxInt)
		ctx, dst := newSUT(t, append(slices.Clone(sharedOpts), withState(snow.StateSyncing), withDB(flaky), withLogger(faultTolerantLogger()))...)
		saetest.ConnectTo(t, dst, src)

		flaky.SetFailAfter(numOps)

		startStateSync(ctx, t, src, dst)
		msg, err := dst.WaitForEvent(ctx)
		require.NoErrorf(t, err, "%T.WaitForEvent()", dst.VM)
		require.Equalf(t, snowcommon.StateSyncDone, msg, "%T.WaitForEvent()", dst.VM)

		var wantErr error
		if flaky.Failed() {
			wantErr = saetest.ErrInjected
		}
		// We MUST restore the db to allow a graceful transition, we're not testing DB failures there.
		flaky.SetFailAfter(math.MaxInt)

		err = dst.SetState(t.Context(), snow.Bootstrapping)
		require.ErrorIsf(t, err, wantErr, "%T.SetState(%s)", dst.VM, snow.Bootstrapping)
		if wantErr == nil {
			return
		}

		t.Run("second_try", func(t *testing.T) {
			ctx, dst := newSUT(t, append(slices.Clone(sharedOpts), withState(snow.StateSyncing), withDB(flaky))...)
			saetest.ConnectTo(t, dst, src)
			summaryHeight := startStateSync(ctx, t, src, dst)
			awaitStateSync(ctx, t, dst)
			dst.bootstrapFrom(ctx, t, src, summaryHeight)
			dst.assertChainsMatch(ctx, t, src)
		})
	}
}
