// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	ethcommon "github.com/ava-labs/libevm/common"
)

// TestShouldAcceptSummary checks the cases in which
// [Handler.ShouldAcceptSummary] refuses to state sync.
func TestShouldAcceptSummary(t *testing.T) {
	tests := []struct {
		name       string
		newHandler func(t *testing.T) *Handler
		summary    *Summary
		want       bool
	}{
		{
			name: "summary_at_genesis_height",
			newHandler: func(t *testing.T) *Handler {
				return newSUT(t).Handler
			},
			summary: &Summary{},
		},
		{
			name: "blocks_already_accepted",
			newHandler: func(t *testing.T) *Handler {
				vm := newVM(t)
				vm.acceptBlocks(t, defaultCommitInterval)
				return vm.Handler
			},
			summary: &Summary{AcceptedHeight: 1},
		},
		{
			name: "not_enabled",
			newHandler: func(t *testing.T) *Handler {
				sut := newSUT(t, withEnabled(false))
				return sut.Handler
			},
			summary: &Summary{AcceptedHeight: 1},
		},
		{
			name: "valid_summary",
			newHandler: func(t *testing.T) *Handler {
				return newSUT(t).Handler
			},
			summary: &Summary{AcceptedHeight: 1},
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sh := tt.newHandler(t)
			syncer := sh.Syncer()
			require.Equalf(t, tt.want, syncer.ShouldAcceptSummary(tt.summary), "%T.ShouldAcceptSummary()", sh)
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

	summary, err := sourceVM.Handler.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.Handler)
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
		require.NoErrorf(t, clientVM.vm.VerifyBlock(ctx, nil, parsed), "VerifyBlock(%d)", b.Height())
		require.NoErrorf(t, clientVM.vm.AcceptBlock(ctx, parsed), "AcceptBlock(%d)", b.Height())
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

	summary, err := sourceVM.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.Handler)
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

// TestStateSyncSynchronousSettled confirms that a state sync finishes and
// starts the VM with the correct state if the last settled block is
// synchronous.
func TestStateSyncSynchronousSettled(t *testing.T) {
	t.Parallel()

	// Nothing settles without advancing the clock.
	sourceVM := newVM(t)
	for range defaultCommitInterval {
		sourceVM.acceptBlock(t)
	}

	ctx := t.Context()
	summary, err := sourceVM.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.Handler)

	accepted := sourceVM.blockAtHeight(t, summary.Height())
	require.Falsef(t, hook.Synchronous(sourceVM.hooks, accepted.Header()), "hook.Synchronous() last accepted block")
	settled := sourceVM.blockAtHeight(t, sourceVM.hooks.SettledBy(accepted.Header()).Height)
	require.Truef(t, hook.Synchronous(sourceVM.hooks, settled.Header()), "hook.Synchronous() settled block")

	xdb := saetest.NewExecutionResultsDB()
	db := memdb.New()
	client := newSUT(t, withDatabase(db), withXDB(xdb))
	saetest.ConnectTo[saetest.Peer](t, client, sourceVM)

	require.NoErrorf(t, client.syncTo(ctx, t, summary), "%T.syncTo(%v)", client, summary)

	clientVM := newVM(t,
		withDatabase(db),
		withXDB(saetest.CloneExecutionResultsDB(t, xdb)),
		withTime(sourceVM.clock.Now()),
	)
	head, err := clientVM.LastAccepted(ctx)
	require.NoError(t, err, "client LastAccepted()")
	require.Equal(t, ids.ID(accepted.Hash()), head, "client VM recovered the synced head")
}

func TestCancelSync(t *testing.T) {
	t.Parallel()

	sut := newSUT(t)
	syncer := sut.Handler.Syncer()

	// No peers are connected, so the sync stalls until canceled.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := syncer.Sync(ctx, NewSummary(ethcommon.Hash{0xde, 0xad}, defaultCommitInterval))
	require.ErrorIsf(t, err, context.Canceled, "%T.Sync", syncer)
}

// FuzzSyncErrorSurfacedViaError checks that any error in the sync process
// gracefully fatals.
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

		summary, err := source.GetLastStateSummary(ctx)
		require.NoErrorf(t, err, "%T.GetLastStateSummary()", source.Handler)

		// Setup (e.g. the genesis commit) is done.
		fdb.SetFailAfter(failAfter)

		err = client.syncTo(ctx, t, summary)
		if !fdb.Failed() {
			require.NoErrorf(t, err, "%T.syncTo", client)
			return
		}
		require.ErrorIsf(t, err, saetest.ErrInjected, "%T.syncTo()", client)

		// Any error should be recoverable.
		t.Run("second_try", func(t *testing.T) {
			fdb.SetFailAfter(math.MaxInt)
			client := newSUT(t, withDatabase(fdb))
			saetest.ConnectTo[saetest.Peer](t, client, source)
			require.NoErrorf(t, client.syncTo(t.Context(), t, summary), "%T.syncTo()", client)
		})
	})
}

// TestStateSyncLong ensures that the VM can startup with blocks missing, which
// is the normal state-syncing case.
func TestStateSyncLong(t *testing.T) {
	const numBlocks = 1024 // > num blocks fetched by syncer

	sourceVM := newVM(t)
	sourceVM.acceptBlocks(t, numBlocks)

	xdb := saetest.NewExecutionResultsDB()
	db := memdb.New()
	client := newSUT(t, withDatabase(db), withXDB(xdb))
	saetest.ConnectTo[saetest.Peer](t, client, sourceVM)

	summary, err := sourceVM.GetLastStateSummary(t.Context())
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.Handler)
	require.NoErrorf(t, client.syncTo(t.Context(), t, summary), "%T.syncTo(%v)", client, summary)

	clientVM := newVM(t,
		withDatabase(db),
		withXDB(saetest.CloneExecutionResultsDB(t, xdb)),
		withTime(sourceVM.clock.Now()),
	)

	wantLast, err := sourceVM.LastAccepted(t.Context())
	require.NoErrorf(t, err, "%T.LastAccepted()", sourceVM)
	gotLast, err := clientVM.LastAccepted(t.Context())
	require.NoErrorf(t, err, "%T.LastAccepted()", clientVM)
	require.Equal(t, wantLast, gotLast, "last accepted ID")
}

// Checks which bloom sections a sync marks as indexed.
func TestWriteBloomIndexer(t *testing.T) {
	t.Parallel()

	const sectionSize = params.BloomBitsBlocks
	tests := []struct {
		name         string
		height       uint64
		wantSections uint64
	}{
		{
			// Right end state for the wrong reason: a checkpoint is written
			// with a head the indexer rejects, rolling back to zero.
			name:   "no_whole_section_indexed",
			height: defaultCommitInterval,
		},
		{
			name:         "first_section_boundary",
			height:       sectionSize,
			wantSections: 1,
		},
		{
			name:         "third_section_boundary",
			height:       3 * sectionSize,
			wantSections: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := rawdb.NewMemoryDatabase()
			parent := ethcommon.Hash{0xbe, 0xef}
			settler := &types.Header{
				Number:     new(big.Int).SetUint64(tt.height),
				ParentHash: parent,
			}
			// The head must be the canonical block ending the section.
			rawdb.WriteCanonicalHash(db, parent, tt.height-1)

			require.NoErrorf(t, writeBloomIndex(db, settler), "writeBloomIndex(%d)", tt.height)

			// Only a fresh indexer re-validates the stored sections.
			idx := core.NewBloomIndexer(db, sectionSize, 0)
			defer idx.Close()

			gotSections, _, gotHead := idx.Sections()
			require.Equalf(t, tt.wantSections, gotSections, "indexed sections after updateBloomIndexer(%d)", tt.height)
			if tt.wantSections > 0 {
				require.Equal(t, parent, gotHead, "head of the last indexed section")
			}
		})
	}
}

// TestSyncAfterAbandonedSync checks that a sync MUST succeed over a database
// holding the results of an earlier sync to a different summary that was never
// finalized. This is the disk state of a node that shut down before
// [Syncer.WriteSynced] and was offered a newer summary on restart.
func TestSyncAfterAbandonedSync(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sourceVM := newVM(t)
	sourceVM.acceptBlocks(t, defaultCommitInterval)

	db := memdb.New()
	client := newSUT(t, withDatabase(db))
	saetest.ConnectTo[saetest.Peer](t, client, sourceVM)

	abandoned, err := sourceVM.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.Handler)
	syncer := client.Handler.Syncer()
	require.NoErrorf(t, syncer.Sync(ctx, abandoned), "%T.Sync(%v)", syncer, abandoned)
	// [Syncer.WriteSynced] is deliberately skipped, as if the node shut down.

	// The network moved on while the node was down.
	sourceVM.acceptBlocks(t, defaultCommitInterval)
	summary, err := sourceVM.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.Handler)

	// Test invariant: the two summaries commit to different state.
	abandonedRoot := sourceVM.blockAtHeight(t, abandoned.Height()).Header().Root
	summaryRoot := sourceVM.blockAtHeight(t, summary.Height()).Header().Root
	require.NotEqual(t, abandonedRoot, summaryRoot, "state roots of the two summaries")

	// A restarted node constructs a fresh handler over the same database.
	client = newSUT(t, withDatabase(db))
	saetest.ConnectTo[saetest.Peer](t, client, sourceVM)
	require.NoErrorf(t, client.syncTo(ctx, t, summary), "%T.syncTo(%v)", client, summary)
}
