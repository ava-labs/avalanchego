// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	ethcommon "github.com/ava-labs/libevm/common"
)

// TestStateSyncEnabled checks that the configured value is reported back by
// [SummaryHandler.StateSyncEnabled].
func TestStateSyncEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{
			name:    "disabled",
			enabled: false,
		},
		{
			name:    "enabled",
			enabled: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sut := newSUT(t, withEnabled(tt.enabled))

			gotEnabled, err := sut.StateSyncEnabled(t.Context())
			require.NoErrorf(t, err, "%T.StateSyncEnabled()", sut.SummaryHandler)
			assert.Equalf(t, tt.enabled, gotEnabled, "%T.StateSyncEnabled()", sut.SummaryHandler)
		})
	}
}

// TestShouldAcceptSummarySkips checks the two cases in which
// [SummaryHandler.ShouldAcceptSummary] refuses to state sync.
func TestShouldAcceptSummarySkips(t *testing.T) {
	tests := []struct {
		name       string
		newHandler func(t *testing.T) *SummaryHandler
		getSummary func(t *testing.T, sh *SummaryHandler) *Summary
	}{
		{
			name: "summary_at_genesis_height",
			newHandler: func(t *testing.T) *SummaryHandler {
				return newSUT(t).SummaryHandler
			},
			getSummary: func(*testing.T, *SummaryHandler) *Summary {
				return &Summary{}
			},
		},
		{
			name: "blocks_already_accepted",
			newHandler: func(t *testing.T) *SummaryHandler {
				vm := newVM(t)
				vm.acceptBlocks(t, defaultCommitInterval)
				return vm.summaryHandler
			},
			getSummary: func(t *testing.T, sh *SummaryHandler) *Summary {
				s, err := sh.GetLastStateSummary(t.Context())
				require.NoErrorf(t, err, "%T.GetLastStateSummary()", sh)
				require.NotZerof(t, s.Height(), "%T.GetLastStateSummary().Height()", sh)
				return s
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sh := tt.newHandler(t)
			s := tt.getSummary(t, sh)

			should, err := sh.ShouldAcceptSummary(t.Context(), s)
			require.NoErrorf(t, err, "%T.ShouldAcceptSummary()", sh)
			require.Falsef(t, should, "%T.ShouldAcceptSummary()", sh)
		})
	}
}

// TestStateSyncEndToEnd syncs a fresh node from a source with non-trivial
// code and storage, verifies everything reached the synced disk, then starts
// a VM over the synced database and keeps accepting blocks to prove that
// settlement of the synced state was persisted correctly.
func TestStateSyncEndToEnd(t *testing.T) {
	t.Parallel()

	const numBlocks = defaultCommitInterval + 2
	sourceVM := newVM(t)
	sourceVM.acceptBlocks(t, numBlocks)

	// Handler to state sync
	xdb := saetest.NewExecutionResultsDB()
	db := memdb.New()
	client := newSUT(t, withDatabase(db), withXDB(xdb))
	saetest.ConnectTo[saetest.Peer](t, client, sourceVM)

	ctx := t.Context()

	summary, err := sourceVM.summaryHandler.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.summaryHandler)
	require.Equal(t, uint64(defaultCommitInterval), summary.Height(), "summary at last commit boundary")

	require.NoErrorf(t, client.syncTo(t.Context(), t, summary), "%T.syncTo(%v)", client, summary)

	// During the sync, the network continued processing
	sourceVM.acceptBlocks(t, numBlocks)

	// catch up a new VM
	clientVM := newVM(t,
		withDatabase(db),
		withXDB(saetest.CloneExecutionResultsDB(t, xdb)),
		withTime(sourceVM.clock.Now()),
	)
	lastHeight := sourceVM.lastAcceptedBlock(t).Height()
	for height := summary.Height() + 1; height <= lastHeight; height++ {
		b := sourceVM.blockAtHeight(t, height)
		parsed, err := clientVM.ParseBlock(ctx, b.Bytes())
		require.NoErrorf(t, err, "ParseBlock(%d)", b.Height())
		require.NoErrorf(t, clientVM.VerifyBlock(ctx, nil, parsed), "VerifyBlock(%d)", b.Height())
		require.NoErrorf(t, clientVM.AcceptBlock(ctx, parsed), "AcceptBlock(%d)", b.Height())
		require.NoErrorf(t, parsed.WaitUntilExecuted(ctx), "WaitUntilExecuted(%d)", b.Height())
	}

	sourceHead, err := sourceVM.LastAccepted(ctx)
	require.NoError(t, err, "source LastAccepted()")
	clientHead, err := clientVM.LastAccepted(ctx)
	require.NoError(t, err, "client LastAccepted()")
	require.Equal(t, sourceHead, clientHead, "client VM caught up to the source head")
}

// TestStateSyncWithSettlementLag syncs a fresh node from a source
// whose head settles a block more than one height back.
func TestStateSyncWithSettlementLag(t *testing.T) {
	t.Parallel()

	sourceVM := newVM(t)
	ctx := t.Context()

	b1 := sourceVM.acceptBlock(t)
	b2 := sourceVM.acceptBlock(t)
	sourceVM.clock.AdvanceToSettle(ctx, t, b1)
	b3 := sourceVM.acceptBlock(t)
	sourceVM.clock.AdvanceToSettle(ctx, t, b2)
	b4 := sourceVM.acceptBlock(t)

	// Test invariants
	require.Equal(t, b1.Height(), sourceVM.hooks.SettledBy(b3.Header()).Height, "SettledBy(b3)")
	require.Equal(t, b2.Height(), sourceVM.hooks.SettledBy(b4.Header()).Height, "SettledBy(b4)")

	xdb := saetest.NewExecutionResultsDB()
	db := memdb.New()
	client := newSUT(t, withDatabase(db), withXDB(xdb))
	saetest.ConnectTo[saetest.Peer](t, client, sourceVM)

	summary, err := sourceVM.summaryHandler.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.summaryHandler)
	require.Equal(t, b4.Height(), summary.Height(), "summary at last commit boundary")

	require.NoErrorf(t, client.syncTo(ctx, t, summary), "%T.syncTo(%v)", client, summary)

	// Recovery inside VM initialization must reconstruct the settlement of
	// blocks 3 and 4, which settled blocks below the synced last settled block.
	clientVM := newVM(t,
		withDatabase(db),
		withXDB(saetest.CloneExecutionResultsDB(t, xdb)),
		withTime(sourceVM.clock.Now()),
	)

	head, err := clientVM.LastAccepted(ctx)
	require.NoError(t, err, "client LastAccepted()")
	require.Equal(t, ids.ID(b4.Hash()), head, "client VM recovered the synced head")
}

// TestSyncCanceled checks that a stalled Sync returns once its context is
// canceled.
func TestSyncCanceled(t *testing.T) {
	t.Parallel()

	sut := newSUT(t)

	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		// No peers are connected, so the sync stalls until canceled.
		errCh <- sut.Sync(ctx, NewSummary(ethcommon.Hash{0xde, 0xad}, defaultCommitInterval))
	}()
	cancel()
	require.ErrorIsf(t, <-errCh, context.Canceled, "%T.Sync() after cancel", sut.SummaryHandler)
}

// FuzzSyncErrorSurfacedViaError checks that any error in the sync process
// gracefully fatals,
func FuzzSyncErrorSurfacedViaError(f *testing.F) {
	for _, failAfter := range []int{0, 1, 4, 16, 64, math.MaxInt} {
		f.Add(failAfter)
	}
	f.Fuzz(func(t *testing.T, failAfter int) {
		ctx := t.Context()

		source := newVM(t)
		source.acceptBlocks(t, defaultCommitInterval+2)

		fdb := saetest.NewFlakyDB(memdb.New(), math.MaxInt)
		client := newSUT(t, withDatabase(fdb))
		saetest.ConnectTo[saetest.Peer](t, client, source)

		summary, err := source.summaryHandler.GetLastStateSummary(ctx)
		require.NoErrorf(t, err, "%T.GetLastStateSummary()", source.summaryHandler)

		// Setup (e.g. the genesis commit) is done; arm the write budget.
		fdb.SetFailAfter(failAfter)

		err = client.syncTo(ctx, t, summary)
		if !fdb.Failed() {
			require.NoErrorf(t, err, "%T.syncTo", client)
			return
		}
		require.ErrorIsf(t, err, saetest.ErrInjected, "%T.Error()", client)

		// Any error should be recoverable.
		t.Run("second_try", func(t *testing.T) {
			fdb.SetFailAfter(math.MaxInt)
			client := newSUT(t, withDatabase(fdb))
			saetest.ConnectTo[saetest.Peer](t, client, source)
			require.NoErrorf(t, client.syncTo(t.Context(), t, summary), "%T.syncTo()", client)
		})
	})
}

// TestSyncWithExtraOrdering checks the [SummaryHandler.SyncWith] contract:
// the extra runs after block sync (the summary's header is on disk) and
// before the finalization markers are written.
func TestSyncWithExtraOrdering(t *testing.T) {
	t.Parallel()

	sourceVM := newVM(t)
	sourceVM.acceptBlocks(t, defaultCommitInterval)

	client := newSUT(t)
	saetest.ConnectTo[saetest.Peer](t, client, sourceVM)

	ctx := t.Context()
	summary, err := sourceVM.summaryHandler.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.summaryHandler)

	extraRan := false
	err = client.SyncWith(ctx, summary, func(context.Context) error {
		extraRan = true
		require.NotNilf(t,
			rawdb.ReadHeader(client.sutEnv.db, summary.AcceptedHash, summary.AcceptedHeight),
			"synced header readable when extra runs",
		)
		require.NotEqualf(t,
			summary.AcceptedHash, rawdb.ReadHeadFastBlockHash(client.sutEnv.db),
			"finalization markers not yet written when extra runs",
		)
		return nil
	})
	require.NoErrorf(t, err, "%T.SyncWith()", client.SummaryHandler)
	require.True(t, extraRan, "extra ran")
	require.Equalf(t,
		summary.AcceptedHash, rawdb.ReadHeadFastBlockHash(client.sutEnv.db),
		"finalization markers written after SyncWith",
	)
}

// TestSyncWithExtraError checks that an extra's error aborts the sync before
// finalization: no markers are written and the error is returned.
func TestSyncWithExtraError(t *testing.T) {
	t.Parallel()

	sourceVM := newVM(t)
	sourceVM.acceptBlocks(t, defaultCommitInterval)

	client := newSUT(t)
	saetest.ConnectTo[saetest.Peer](t, client, sourceVM)

	ctx := t.Context()
	summary, err := sourceVM.summaryHandler.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.summaryHandler)

	errExtra := errors.New("extra failed")
	err = client.SyncWith(ctx, summary, func(context.Context) error {
		return errExtra
	})
	require.ErrorIsf(t, err, errExtra, "%T.SyncWith() with failing extra", client.SummaryHandler)
	require.NotEqualf(t,
		summary.AcceptedHash, rawdb.ReadHeadFastBlockHash(client.sutEnv.db),
		"no finalization markers after failed extra",
	)
}
