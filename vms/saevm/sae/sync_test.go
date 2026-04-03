// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/big"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/snow"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

func TestStateSync(t *testing.T) {
	const (
		numAccounts    = 10
		commitInterval = 16
		numBlocks      = commitInterval + 4
	)

	opt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	ctx, src := newSUT(t, numAccounts, opt, withCommitInterval(commitInterval))

	r := rand.New(rand.NewPCG(0, 0))
	srcBlocks := make([]*blocks.Block, numBlocks)
	for i := range numBlocks {
		vmTime.advanceToSettle(ctx, t, src.lastAcceptedBlock(t))
		acct := r.IntN(numAccounts)
		srcBlocks[i] = src.runConsensusLoop(t, src.wallet.SetNonceAndSign(t, acct, &types.DynamicFeeTx{
			To:    &common.Address{},
			Gas:   params.TxGas, //nolint:gosec // Won't overflow
			Value: big.NewInt(int64(i + 10)),
		}))
	}
	for _, b := range srcBlocks {
		require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	}

	// Get state summary from src (at the last committed height == commitInterval).
	summary, err := src.rawVM.GetLastStateSummary(ctx)
	require.NoError(t, err, "src.GetLastStateSummary()")
	require.EqualValues(t, commitInterval, summary.Height(), "summary height")

	// Create dest with state sync enabled, using src's ethdb as the sync source
	// and the same genesis so the chain configs match.
	destCtx, dest := newSUT(t, 0,
		withStateSyncEnabled(src.db),
		withGenesis(src.rawGenesis),
		withCommitInterval(commitInterval),
	)

	require.NoError(t, dest.SetState(destCtx, snow.StateSyncing), "dest.SetState(StateSyncing)")

	parsedSummary, err := dest.rawVM.ParseStateSummary(destCtx, summary.Bytes())
	require.NoError(t, err, "dest.ParseStateSummary()")

	// AcceptSummary must not block; the sync goroutine runs in the background.
	mode, err := dest.rawVM.AcceptSummary(destCtx, parsedSummary)
	require.NoError(t, err, "dest.AcceptSummary()")
	require.Equal(t, block.StateSyncStatic, mode, "AcceptSummary() mode")

	msg, err := dest.WaitForEvent(destCtx)
	require.NoError(t, err, "dest.WaitForEvent() after AcceptSummary")
	require.Equal(t, snowcommon.StateSyncDone, msg, "WaitForEvent() should signal StateSyncDone")

	// Switch to bootstrapping so block acceptance waits for execution.
	require.NoError(t, dest.SetState(destCtx, snow.Bootstrapping), "dest.SetState(Bootstrapping)")

	// Parse, verify, and accept the remaining blocks (17–20) on dest, then
	// confirm that the post-execution state roots match src.
	for _, srcBlock := range srcBlocks[commitInterval:] {
		srcSnowBlock, err := src.GetBlock(ctx, srcBlock.ID())
		require.NoErrorf(t, err, "src.GetBlock(height=%d)", srcBlock.NumberU64())

		destSnowBlock, err := dest.ParseBlock(destCtx, srcSnowBlock.Bytes())
		require.NoErrorf(t, err, "dest.ParseBlock(height=%d)", srcBlock.NumberU64())
		require.NoErrorf(t, destSnowBlock.Verify(destCtx), "dest block Verify(height=%d)", srcBlock.NumberU64())
		require.NoErrorf(t, destSnowBlock.Accept(destCtx), "dest block Accept(height=%d)", srcBlock.NumberU64())

		destBlock := unwrap(t, destSnowBlock)
		require.NoErrorf(t, destBlock.WaitUntilExecuted(destCtx), "dest block WaitUntilExecuted(height=%d)", srcBlock.NumberU64())
		require.Equalf(t,
			srcBlock.PostExecutionStateRoot(),
			destBlock.PostExecutionStateRoot(),
			"post-execution state root mismatch at height %d", srcBlock.NumberU64(),
		)
	}
}
