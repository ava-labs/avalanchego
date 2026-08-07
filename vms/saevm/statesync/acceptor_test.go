// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
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

// TestAcceptSummarySkips checks the two cases in which
// [SummaryHandler.AcceptSummary] refuses to state sync.
func TestAcceptSummarySkips(t *testing.T) {
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

			mode, err := sh.AcceptSummary(t.Context(), s)
			require.NoErrorf(t, err, "%T.AcceptSummary()", sh)
			require.Equalf(t, block.StateSyncSkipped, mode, "%T.AcceptSummary()", sh)
		})
	}
}

func TestShutdownCancelsMidSync(t *testing.T) {
	t.Parallel()

	sut := newSUT(t)

	// No peers are connected, so the sync stalls until canceled.
	mode, err := sut.AcceptSummary(t.Context(), NewSummary(ethcommon.Hash{0xde, 0xad}, defaultCommitInterval))
	require.NoErrorf(t, err, "%T.AcceptSummary()", sut.SummaryHandler)
	require.Equalf(t, block.StateSyncStatic, mode, "%T.AcceptSummary()", sut.SummaryHandler)

	require.NoErrorf(t, sut.Shutdown(t.Context()), "%T.Shutdown()", sut.SummaryHandler)

	msg, err := sut.WaitForEvent(t.Context())
	require.NoErrorf(t, err, "%T.WaitForEvent()", sut.SummaryHandler)
	require.Equal(t, common.StateSyncDone, msg, "WaitForEvent()")
	require.ErrorIsf(t, sut.Error(t.Context()), context.Canceled, "%T.Error()", sut.SummaryHandler)
}

// TestSyncErrorSurfacedViaError checks that a sync that fails after being
// accepted still reports [common.StateSyncDone] via WaitForEvent, with the
// failure surfaced by [SummaryHandler.Error].
func TestSyncErrorSurfacedViaError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	source := newVM(t)
	source.acceptBlocks(t, defaultCommitInterval+2)

	fdb := saetest.NewFlakyDB(memdb.New(), math.MaxInt)
	client := newSUT(t, withDatabase(fdb))
	saetest.ConnectTo[saetest.Peer](t, client, source)

	summary, err := source.summaryHandler.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", source.summaryHandler)

	// Setup (e.g. the genesis commit) is done; fail any write now.
	fdb.SetFailAfter(0)

	mode, err := client.AcceptSummary(ctx, summary)
	require.NoErrorf(t, err, "%T.AcceptSummary()", client.SummaryHandler)
	require.Equalf(t, block.StateSyncStatic, mode, "%T.AcceptSummary()", client.SummaryHandler)

	msg, err := client.WaitForEvent(ctx)
	require.NoErrorf(t, err, "%T.WaitForEvent()", client.SummaryHandler)
	require.Equal(t, common.StateSyncDone, msg, "WaitForEvent()")

	require.ErrorIsf(t, client.Error(ctx), saetest.ErrInjected, "%T.Error()", client.SummaryHandler)
}

func TestWaitForEventCanceled(t *testing.T) {
	sut := newSUT(t)

	type waitResult struct {
		msg common.Message
		err error
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan waitResult, 1)
	go func() {
		msg, err := sut.WaitForEvent(ctx)
		done <- waitResult{msg: msg, err: err}
	}()

	cancel()
	r := <-done
	assert.ErrorIsf(t, r.err, context.Canceled, "%T.WaitForEvent()", sut.SummaryHandler) //nolint:testifylint // msg is informative
	assert.Equalf(t, common.Message(0), r.msg, "%T.WaitForEvent()", sut.SummaryHandler)
}

func runStateSync(ctx context.Context, t *testing.T, source *SummaryHandler, client *shSUT) *Summary {
	t.Helper()

	summary, err := source.GetLastStateSummary(ctx)
	require.NoError(t, err, "GetLastStateSummary()")

	parsed, err := client.ParseStateSummary(ctx, summary.Bytes())
	require.NoError(t, err, "ParseStateSummary()")

	mode, err := client.AcceptSummary(ctx, parsed)
	require.NoErrorf(t, err, "%T.AcceptSummary()", client.SummaryHandler)
	require.Equal(t, block.StateSyncStatic, mode, "AcceptSummary() mode")

	msg, err := client.WaitForEvent(ctx)
	require.NoErrorf(t, err, "%T.WaitForEvent()", client.SummaryHandler)
	require.Equal(t, common.StateSyncDone, msg, "WaitForEvent() message")

	require.NoErrorf(t, client.Error(t.Context()), "%T.Error()", client.SummaryHandler)

	return parsed
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
	summary := runStateSync(ctx, t, sourceVM.summaryHandler, client)
	require.Equal(t, uint64(defaultCommitInterval), summary.Height(), "summary at last commit boundary")

	// During the sync, the network continued processing
	sourceVM.acceptBlocks(t, numBlocks)

	// State syncer closed height index when done, but the test double can't
	// just be "re-opened" - but it can be copied.
	clonable, ok := xdb.HeightIndex.(saetest.ClonableHeightIndex)
	require.True(t, ok, "xdb.HeightIndex is not ClonableHeightIndex")
	xdb.HeightIndex = clonable.Clone()

	// catch up a new VM
	clientVM := newVM(t, withDatabase(db), withXDB(xdb), withTime(sourceVM.clock.Now()))
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
