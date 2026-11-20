// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

// produceBlocks accepts n blocks on s, each holding one transfer from account
// from of w, and advances the clock so that each block settles once a
// descendant is accepted. It returns the last block.
func (s *SUT) produceBlocks(ctx context.Context, tb testing.TB, w *saetest.Wallet, from, n int) *blocks.Block {
	tb.Helper()

	var blk *blocks.Block
	for range n {
		s.send(ctx, tb, w, from, transferTx(common.Address{1}, common.Big1))
		blk = s.RunConsensusLoop(ctx, tb)
		s.clock.AdvanceToSettle(ctx, tb, blk)
	}
	return blk
}

// startStateSyncFrom accepts src's last state summary on s, as the engine
// would, and returns the summary's height.
func (s *SUT) startStateSyncFrom(ctx context.Context, tb testing.TB, src *SUT) uint64 {
	tb.Helper()

	summary, err := src.GetLastStateSummary(ctx)
	require.NoErrorf(tb, err, "%T.GetLastStateSummary()", src.VM)

	parsed, err := s.ParseStateSummary(ctx, summary.Bytes())
	require.NoErrorf(tb, err, "%T.ParseStateSummary()", s.VM)
	require.Equalf(tb, summary.ID(), parsed.ID(), "%T.ParseStateSummary() round trip", s.VM)

	mode, err := s.AcceptSummary(ctx, parsed)
	require.NoErrorf(tb, err, "%T.AcceptSummary()", s.VM)
	require.Equalf(tb, block.StateSyncStatic, mode, "%T.AcceptSummary()", s.VM)
	return parsed.Height()
}

// awaitStateSync waits for a sync started by [SUT.startStateSyncFrom] to
// finish and asserts that it succeeded.
func (s *SUT) awaitStateSync(ctx context.Context, tb testing.TB) {
	tb.Helper()

	msg, err := s.WaitForEvent(ctx)
	require.NoErrorf(tb, err, "%T.WaitForEvent()", s.VM)
	require.Equalf(tb, snowcommon.StateSyncDone, msg, "%T.WaitForEvent()", s.VM)
	require.NoErrorf(tb, s.SyncError(), "%T.SyncError()", s.VM)
}

// bootstrapFrom accepts src's blocks above fromHeight on s and then enters
// normal operation, as the engine would.
func (s *SUT) bootstrapFrom(ctx context.Context, tb testing.TB, src *SUT, fromHeight uint64) {
	tb.Helper()

	require.NoErrorf(tb, s.SetState(ctx, snow.Bootstrapping), "%T.SetState(%s)", s.VM, snow.Bootstrapping)
	for height := fromHeight + 1; height <= src.LastAcceptedHeight(ctx, tb); height++ {
		s.ParseVerifyAccept(ctx, tb, src.BlockAtHeight(ctx, tb, height))
	}
	require.NoErrorf(tb, s.SetState(ctx, snow.NormalOp), "%T.SetState(%s)", s.VM, snow.NormalOp)
	require.NoErrorf(tb, s.SetPreference(ctx, s.LastAcceptedID(ctx, tb), nil), "%T.SetPreference()", s.VM)
}

// assertChainsMatch asserts that s and other agree on the last-accepted block
// and its post-execution state root.
func (s *SUT) assertChainsMatch(ctx context.Context, tb testing.TB, other *SUT) {
	tb.Helper()

	require.Equalf(tb, other.LastAcceptedID(ctx, tb), s.LastAcceptedID(ctx, tb), "%T.LastAccepted()", s.VM)

	head := s.LastAcceptedHeight(ctx, tb)
	got := s.BlockAtHeight(ctx, tb, head)
	want := other.BlockAtHeight(ctx, tb, head)
	for _, b := range []*blocks.Block{got, want} {
		require.NoErrorf(tb, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	}
	require.Equalf(tb, want.PostExecutionStateRoot(), got.PostExecutionStateRoot(), "%T.PostExecutionStateRoot()", got)
}

// TestStateSyncNewNode asserts that a fresh node MUST be able to state sync,
// bootstrap the remaining blocks, and then both follow and extend the chain.
// The synced node MUST enforce precompile state that it never executed.
func TestStateSyncNewNode(t *testing.T) {
	for _, scheme := range []string{rawdb.HashScheme, firewood.Scheme} {
		t.Run(scheme, func(t *testing.T) {
			testStateSyncNewNode(t, scheme)
		})
	}
}

func testStateSyncNewNode(t *testing.T, scheme string) {
	const (
		commitInterval = 4
		adminIdx       = 0
		blockedIdx     = 1
	)

	w := newWallet(t, 2)
	addrs := w.Addresses()
	timeOpt, _ := withVMTime(testStartTime)
	// Every peer of src MUST share these to share its chain.
	sharedOpts := []sutOption{
		withScheme(scheme),
		timeOpt,
		withMaxAllocFor(addrs...),
		withGenesisPrecompiles(extras.Precompiles{
			txallowlist.ConfigKey: txallowlist.NewConfig(
				new(uint64(0)),
				[]common.Address{addrs[adminIdx]},
				nil,
				nil,
			),
		}),
		withCommitInterval(commitInterval),
		withStateSyncEnabled(),
	}
	srcCtx, src := newSUT(t, sharedOpts...)
	// One block past the commit boundary, so the summary is below the head
	// and bootstrapping has a block to accept.
	src.produceBlocks(srcCtx, t, w, adminIdx, commitInterval+1)

	ctx, dst := newSUT(t, append(sharedOpts, withState(snow.StateSyncing))...)
	saetest.ConnectTo(t, dst, src)

	summaryHeight := dst.startStateSyncFrom(ctx, t, src)
	require.Equal(t, uint64(commitInterval), summaryHeight, "summary height")
	dst.awaitStateSync(ctx, t)
	require.Equalf(t, src.BlockAtHeight(srcCtx, t, summaryHeight).ID(), dst.LastAcceptedID(ctx, t), "%T.LastAccepted() after state sync", dst.VM)

	dst.bootstrapFrom(ctx, t, src, summaryHeight)
	dst.assertChainsMatch(ctx, t, src)

	blocked := w.SetNonceAndSign(t, blockedIdx, transferTx(addrs[adminIdx], common.Big1))
	require.ErrorContains(t, //nolint:forbidigo // JSON-RPC loses the error chain
		dst.EthClient().SendTransaction(ctx, blocked),
		vmerrors.ErrSenderAddressNotAllowListed.Error(),
		"tx from sender excluded by synced allow list",
	)

	dst.ParseVerifyAccept(ctx, t, src.produceBlocks(srcCtx, t, w, adminIdx, 1))
	src.ParseVerifyAccept(srcCtx, t, dst.produceBlocks(ctx, t, w, adminIdx, 1))
	dst.assertChainsMatch(ctx, t, src)
}

// TestStateSyncDisabledByDefault asserts that, like Subnet-EVM, the VM MUST
// NOT state sync unless configured to.
func TestStateSyncDisabledByDefault(t *testing.T) {
	ctx, sut := newSUT(t, withState(snow.Initializing))
	enabled, err := sut.StateSyncEnabled(ctx)
	require.NoErrorf(t, err, "%T.StateSyncEnabled()", sut.VM)
	require.Falsef(t, enabled, "%T.StateSyncEnabled()", sut.VM)
}
