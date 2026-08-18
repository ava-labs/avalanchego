// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	ethcommon "github.com/ava-labs/libevm/common"
)

// TestSyncProgressReportsRealSync checks that a real sync is reported from
// before it is started until after it completes, so that the target recorded by
// [SummaryHandler.StateSync] is the summary the engine accepted.
func TestSyncProgressReportsRealSync(t *testing.T) {
	t.Parallel()

	const numBlocks = defaultCommitInterval + 2
	sourceVM := newVM(t)
	sourceVM.acceptBlocks(t, numBlocks)

	client := newSUT(t, withDatabase(memdb.New()))
	saetest.ConnectTo[saetest.Peer](t, client, sourceVM)

	ctx := t.Context()

	// No summary has been accepted, so there is no sync to report on.
	require.Equalf(
		t,
		sae.StateSyncProgress{Enabled: true},
		client.SyncProgress(),
		"%T.SyncProgress() before accepting a summary",
		client.SummaryHandler,
	)

	summary, err := sourceVM.summaryHandler.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.summaryHandler)

	require.NoErrorf(t, client.syncTo(ctx, t, summary), "%T.syncTo(%v)", client, summary)

	require.Equalf(
		t,
		sae.StateSyncProgress{
			Enabled:       true,
			Started:       true,
			SummaryHeight: summary.AcceptedHeight,
			SummaryHash:   summary.AcceptedHash,
			Finished:      true,
		},
		client.SyncProgress(),
		"%T.SyncProgress() after a successful sync",
		client.SummaryHandler,
	)
}

// TestSyncProgressPhases checks the phases of a sync that a real sync passes
// through too quickly to observe reliably. It drives the handler's lifecycle
// primitives directly, holding to their documented contract: the target is
// recorded when the sync starts, and the error is written before done is closed.
func TestSyncProgressPhases(t *testing.T) {
	t.Parallel()

	target := NewSummary(ethcommon.Hash(ids.GenerateTestID()), 4096)
	syncErr := errors.New("peers ran out of state")

	// started is the progress expected once target's sync is underway.
	started := sae.StateSyncProgress{
		Enabled:       true,
		Started:       true,
		SummaryHeight: target.AcceptedHeight,
		SummaryHash:   target.AcceptedHash,
	}

	tests := []struct {
		name string
		// drive advances a freshly constructed handler to the phase under test.
		drive func(*SummaryHandler)
		want  sae.StateSyncProgress
	}{
		{
			name:  "no_summary_accepted",
			drive: func(*SummaryHandler) {},
			want:  sae.StateSyncProgress{Enabled: true},
		},
		{
			// The handler declines a summary at genesis height, which the health
			// check reports so that a plain bootstrap is distinguishable from a
			// node awaiting a summary.
			name: "summary_declined",
			drive: func(h *SummaryHandler) {
				should, err := h.ShouldAcceptSummary(NewSummary(ethcommon.Hash{}, 0))
				require.NoErrorf(t, err, "%T.ShouldAcceptSummary()", h)
				require.Falsef(t, should, "%T.ShouldAcceptSummary() at genesis height", h)
			},
			want: sae.StateSyncProgress{Enabled: true, Skipped: true},
		},
		{
			name: "in_progress",
			drive: func(h *SummaryHandler) {
				h.target.Set(target)
			},
			want: started,
		},
		{
			name: "in_progress_ignores_undefined_error",
			drive: func(h *SummaryHandler) {
				h.target.Set(target)
				// A sync's error is only defined once done is closed, so it
				// MUST NOT be reported before then.
				h.err.Set(syncErr)
			},
			want: started,
		},
		{
			name: "complete",
			drive: func(h *SummaryHandler) {
				h.target.Set(target)
				close(h.done)
			},
			want: func() sae.StateSyncProgress {
				p := started
				p.Finished = true
				return p
			}(),
		},
		{
			name: "failed",
			drive: func(h *SummaryHandler) {
				h.target.Set(target)
				h.err.Set(syncErr)
				close(h.done)
			},
			want: func() sae.StateSyncProgress {
				p := started
				p.Finished = true
				p.Err = syncErr
				return p
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sut := newSUT(t)
			tt.drive(sut.SummaryHandler)

			require.Equalf(t, tt.want, sut.SyncProgress(), "%T.SyncProgress()", sut.SummaryHandler)
		})
	}
}

// TestSyncProgressDisabled checks that a handler with state sync configured off
// reports it, so that an operator can tell a node that will bootstrap from
// genesis from one that has yet to choose a summary.
func TestSyncProgressDisabled(t *testing.T) {
	t.Parallel()

	sut := newSUT(t, withEnabled(false))

	require.Equalf(
		t,
		sae.StateSyncProgress{},
		sut.SyncProgress(),
		"%T.SyncProgress() with state sync disabled",
		sut.SummaryHandler,
	)
	require.Equalf(
		t,
		&sae.StateSync{Status: sae.StateSyncDisabled},
		sut.Health(),
		"%T.Health() with state sync disabled",
		sut.SummaryHandler,
	)
}
