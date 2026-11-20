// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"math/big"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	ethparams "github.com/ava-labs/libevm/params"
)

// produceBlocks accepts n blocks on s, each containing a single transfer from
// w, and returns the last block. The clock is advanced after every block so
// that it can settle once a descendant is accepted.
func (s *SUT) produceBlocks(ctx context.Context, t *testing.T, w *saetest.Wallet, clock *saetest.Clock, n int) *blocks.Block {
	t.Helper()

	var blk *blocks.Block
	for range n {
		tx := w.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &common.Address{'r', 'e', 'c', 'i', 'p', 'i', 'e', 'n', 't'},
			Value:     big.NewInt(1),
			Gas:       ethparams.TxGas,
			GasTipCap: big.NewInt(1),
			GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
		})
		require.NoErrorf(t, s.EthClient().SendTransaction(ctx, tx), "%T.SendTransaction()", s.EthClient())
		s.WaitForPendingTxs(ctx, t, tx)

		blk = s.BuildVerifyAccept(ctx, t)
		require.Lenf(t, blk.Transactions(), 1, "%T.Transactions()", blk)
		clock.AdvanceToSettle(ctx, t, blk)
	}
	return blk
}

// startStateSyncFrom fetches src's last state summary and parses and accepts it
// on the SUT, as the engine would, returning the accepted summary's height. The
// sync then proceeds in the background until [SUT.awaitStateSync] observes its
// completion.
func (s *SUT) startStateSyncFrom(ctx context.Context, t *testing.T, src *SUT) uint64 {
	t.Helper()

	// The engine fetches summaries from peers; parsing happens locally.
	summary, err := src.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", src.VM)

	parsed, err := s.ParseStateSummary(ctx, summary.Bytes())
	require.NoErrorf(t, err, "%T.ParseStateSummary()", s.VM)
	require.Equalf(t, summary.ID(), parsed.ID(), "%T.ParseStateSummary() round trip", s.VM)

	mode, err := s.AcceptSummary(ctx, parsed)
	require.NoErrorf(t, err, "%T.AcceptSummary()", s.VM)
	require.Equalf(t, block.StateSyncStatic, mode, "%T.AcceptSummary() mode", s.VM)
	return parsed.Height()
}

// awaitStateSync blocks until the SUT announces completion of a sync started by
// [SUT.startStateSyncFrom] and asserts that it succeeded.
func (s *SUT) awaitStateSync(ctx context.Context, t *testing.T) {
	t.Helper()

	msg, err := s.WaitForEvent(ctx)
	require.NoErrorf(t, err, "%T.WaitForEvent()", s.VM)
	require.Equalf(t, snowcommon.StateSyncDone, msg, "%T.WaitForEvent()", s.VM)
	require.NoErrorf(t, s.SyncError(), "%T.SyncError()", s.VM)
}

// bootstrapFrom transitions s to [snow.Bootstrapping], replays src's accepted
// blocks above fromHeight, and enters [snow.NormalOp], mirroring the engine's
// post-state-sync behavior.
func (s *SUT) bootstrapFrom(ctx context.Context, t *testing.T, src *SUT, fromHeight uint64) {
	t.Helper()

	require.NoErrorf(t, s.SetState(ctx, snow.Bootstrapping), "%T.SetState(Bootstrapping)", s.VM)

	head := src.LastAcceptedHeight(ctx, t)
	for height := fromHeight + 1; height <= head; height++ {
		s.ParseVerifyAccept(ctx, t, src.BlockAtHeight(ctx, t, height))
	}

	require.NoErrorf(t, s.SetState(ctx, snow.NormalOp), "%T.SetState(NormalOp)", s.VM)
	require.NoErrorf(t, s.SetPreference(ctx, s.LastAcceptedID(ctx, t), nil), "%T.SetPreference()", s.VM)
}

// assertChainsMatch asserts that s and other agree on the last-accepted block
// and its post-execution state.
func (s *SUT) assertChainsMatch(ctx context.Context, t *testing.T, other *SUT) {
	t.Helper()

	require.Equalf(t, other.LastAcceptedID(ctx, t), s.LastAcceptedID(ctx, t), "%T.LastAccepted()", s.VM)

	head := s.LastAcceptedHeight(ctx, t)
	headBlk := s.BlockAtHeight(ctx, t, head)
	otherHeadBlk := other.BlockAtHeight(ctx, t, head)
	require.NoErrorf(t, headBlk.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted(height %d)", headBlk, head)
	require.NoErrorf(t, otherHeadBlk.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted(height %d)", otherHeadBlk, head)

	require.Equalf(t, otherHeadBlk.PostExecutionStateRoot(), headBlk.PostExecutionStateRoot(), "%T.PostExecutionStateRoot() at height %d", headBlk, head)
}

// TestStateSyncNewNode is the happy path: a fresh node state syncs from a
// running source VM, bootstraps the remaining blocks, and then participates in
// TestStateSyncNewNode is the happy path: a fresh node state syncs from a
// running source VM, bootstraps the remaining blocks, and then participates in
// the network, both accepting the source's blocks and building its own.
func TestStateSyncNewNode(t *testing.T) {
	const commitInterval = 8

	w := saetest.NewUNSAFEWallet(t, 1, types.LatestSignerForChainID(big.NewInt(testChainID)))
	timeOpt, clock := withVMTime(testStartTime)
	// sharedOpts are the genesis-, clock-, and config-affecting options that
	// every peer of src MUST reuse to share src's chain.
	sharedOpts := []sutOption{
		timeOpt,
		withMaxAllocFor(w.Addresses()...),
		withCommitInterval(commitInterval),
	}
	srcCtx, src := newSUT(t, sharedOpts...)

	// Fill the chain to just past the first commit boundary so
	// GetLastStateSummary snaps down to commitInterval.
	src.produceBlocks(srcCtx, t, w, clock, commitInterval+2)

	ctx, dst := newSUT(t, append(slices.Clone(sharedOpts), withState(snow.StateSyncing))...)
	saetest.ConnectTo(t, dst, src)

	summaryHeight := dst.startStateSyncFrom(ctx, t, src)
	require.Equal(t, uint64(commitInterval), summaryHeight, "summary at last commit boundary")
	dst.awaitStateSync(ctx, t)

	dst.bootstrapFrom(ctx, t, src, summaryHeight)
	dst.assertChainsMatch(ctx, t, src)

	// dst keeps up with the network...
	blk := src.produceBlocks(srcCtx, t, w, clock, 1)
	dst.ParseVerifyAccept(ctx, t, blk)

	// ...and produces a block of its own that src accepts.
	dstBlk := dst.produceBlocks(ctx, t, w, clock, 1)
	src.ParseVerifyAccept(srcCtx, t, dstBlk)
	dst.assertChainsMatch(ctx, t, src)
}

// TestStateSyncWhileNetworkAdvances checks that a node can sync from a source
// that keeps accepting blocks: the sync lands on the older summary and
// bootstrapping replays the gap up to the source's new head.
func TestStateSyncWhileNetworkAdvances(t *testing.T) {
	const commitInterval = 4

	w := saetest.NewUNSAFEWallet(t, 1, types.LatestSignerForChainID(big.NewInt(testChainID)))
	timeOpt, clock := withVMTime(testStartTime)
	sharedOpts := []sutOption{
		timeOpt,
		withMaxAllocFor(w.Addresses()...),
		withCommitInterval(commitInterval),
	}
	srcCtx, src := newSUT(t, sharedOpts...)

	src.produceBlocks(srcCtx, t, w, clock, commitInterval+1)

	ctx, dst := newSUT(t, append(slices.Clone(sharedOpts), withState(snow.StateSyncing))...)
	saetest.ConnectTo(t, dst, src)

	summaryHeight := dst.startStateSyncFrom(ctx, t, src)
	require.Equal(t, uint64(commitInterval), summaryHeight, "summary at last commit boundary")

	src.produceBlocks(srcCtx, t, w, clock, commitInterval)
	dst.awaitStateSync(ctx, t)

	// The network keeps advancing, past another commit boundary, before dst
	// starts bootstrapping.
	src.produceBlocks(srcCtx, t, w, clock, commitInterval)

	dst.bootstrapFrom(ctx, t, src, summaryHeight)
	dst.assertChainsMatch(ctx, t, src)
}
