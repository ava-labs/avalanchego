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
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	ethcommon "github.com/ava-labs/libevm/common"
)

// TestShouldAcceptSummary checks the cases in which
// [SummaryHandler.ShouldAcceptSummary] refuses to state sync.
func TestShouldAcceptSummary(t *testing.T) {
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
				return vm.SummaryHandler
			},
			getSummary: func(t *testing.T, sh *SummaryHandler) *Summary {
				s, err := sh.GetLastStateSummary(t.Context())
				require.NoErrorf(t, err, "%T.GetLastStateSummary()", sh)
				require.NotZerof(t, s.Height(), "%T.GetLastStateSummary().Height()", sh)
				return s
			},
		},
		{
			name: "not_enabled",
			newHandler: func(t *testing.T) *SummaryHandler {
				sut := newSUT(t, withEnabled(false))
				return sut.SummaryHandler
			},
			getSummary: func(*testing.T, *SummaryHandler) *Summary {
				return &Summary{AcceptedHeight: 1}
			},
		},
		{
			name: "firewood_scheme",
			newHandler: func(t *testing.T) *SummaryHandler {
				sut := newSUT(t, withScheme(customrawdb.FirewoodScheme), withRecordedLog())
				return sut.SummaryHandler
			},
			getSummary: func(*testing.T, *SummaryHandler) *Summary {
				return &Summary{AcceptedHeight: 1}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sh := tt.newHandler(t)
			s := tt.getSummary(t, sh)

			require.Falsef(t, sh.ShouldAcceptSummary(s), "%T.ShouldAcceptSummary()", sh)
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
	client := sourceVM.newSyncClient(t)

	ctx := t.Context()

	summary, err := sourceVM.SummaryHandler.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.SummaryHandler)
	require.Equal(t, uint64(defaultCommitInterval), summary.Height(), "summary at last commit boundary")

	require.NoErrorf(t, client.syncTo(t.Context(), t, summary), "%T.syncTo(%v)", client, summary)

	accepted := sourceVM.blockAtHeight(t, summary.Height())
	settled := sourceVM.blockAtHeight(t, sourceVM.hooks.SettledBy(accepted.Header()).Height)
	requireSyncedMarkers(t, client, accepted, settled)

	// During the sync, the network continued processing
	sourceVM.acceptBlocks(t, numBlocks)

	// catch up a new VM
	clientVM := client.restartAsVM(t, sourceVM.clock.Now())
	lastHeight := sourceVM.lastAcceptedBlock(t).Height()
	for height := summary.Height() + 1; height <= lastHeight; height++ {
		b := sourceVM.blockAtHeight(t, height)
		parsed, err := clientVM.ParseBlock(ctx, b.Bytes())
		require.NoErrorf(t, err, "ParseBlock(%d)", b.Height())
		require.NoErrorf(t, clientVM.vm.VerifyBlock(ctx, nil, parsed), "VerifyBlock(%d)", b.Height())
		require.NoErrorf(t, clientVM.vm.AcceptBlock(ctx, parsed), "AcceptBlock(%d)", b.Height())
		require.NoErrorf(t, parsed.WaitUntilExecuted(ctx), "WaitUntilExecuted(%d)", b.Height())
	}

	requireVMHead(t, clientVM, sourceVM.lastAcceptedBlock(t).ID())
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

	client := sourceVM.newSyncClient(t)

	summary, err := sourceVM.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.SummaryHandler)
	require.Equal(t, b4.Height(), summary.Height(), "summary at last commit boundary")

	require.NoErrorf(t, client.syncTo(ctx, t, summary), "%T.syncTo(%v)", client, summary)

	requireSyncedMarkers(t, client, b4, b2)

	// Recovery inside VM initialization must reconstruct the settlement of
	// blocks 3 and 4, which settled blocks below the synced last settled block.
	clientVM := client.restartAsVM(t, sourceVM.clock.Now())

	requireVMHead(t, clientVM, b4.ID())
}

// Syncs to a summary settled by genesis, which is synchronous, so no execution
// results are persisted but the markers are.
func TestStateSyncSynchronousSettled(t *testing.T) {
	t.Parallel()

	// Nothing settles without advancing the clock.
	sourceVM := newVM(t)
	for range defaultCommitInterval {
		sourceVM.acceptBlock(t)
	}

	ctx := t.Context()
	summary, err := sourceVM.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.SummaryHandler)

	accepted := sourceVM.blockAtHeight(t, summary.Height())
	settled := sourceVM.blockAtHeight(t, sourceVM.hooks.SettledBy(accepted.Header()).Height)
	require.Zerof(t, settled.Height(), "settled block is genesis, so %T.Synchronous", settled)

	client := sourceVM.newSyncClient(t)

	require.NoErrorf(t, client.syncTo(ctx, t, summary), "%T.syncTo(%v)", client, summary)

	requireSyncedMarkers(t, client, accepted, settled)

	// The skip is the point: a synchronous block's results come from its header.
	has, err := client.cfg.xdb.HeightIndex.Has(settled.Height())
	require.NoErrorf(t, err, "%T.Has(%d)", client.cfg.xdb.HeightIndex, settled.Height())
	require.Falsef(t, has, "execution results persisted for synchronous block %d", settled.Height())

	// Starting a VM proves the skipped results were not needed.
	clientVM := client.restartAsVM(t, sourceVM.clock.Now())
	requireVMHead(t, clientVM, accepted.ID())
}

// Checks which bloom sections a sync marks as indexed.
func TestUpdateBloomIndexer(t *testing.T) {
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

			sut := newSUT(t)
			parent := ethcommon.Hash{0xbe, 0xef}
			settler := &types.Header{
				Number:     new(big.Int).SetUint64(tt.height),
				ParentHash: parent,
			}
			// The head must be the canonical block ending the section.
			rawdb.WriteCanonicalHash(sut.db, parent, tt.height-1)

			require.NoErrorf(t, sut.updateBloomIndexer(settler), "updateBloomIndexer(%d)", tt.height)

			// Only a fresh indexer re-validates the stored sections.
			idx := core.NewBloomIndexer(sut.db, sectionSize, 0)
			defer idx.Close()

			gotSections, _, gotHead := idx.Sections()
			require.Equalf(t, tt.wantSections, gotSections, "indexed sections after updateBloomIndexer(%d)", tt.height)
			if tt.wantSections > 0 {
				require.Equal(t, parent, gotHead, "head of the last indexed section")
			}
		})
	}
}

func TestShutdownCancelsMidSync(t *testing.T) {
	t.Parallel()

	sut := newSUT(t)

	// No peers are connected, so the sync stalls until canceled.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := sut.Sync(ctx, NewSummary(ethcommon.Hash{0xde, 0xad}, defaultCommitInterval))
	require.ErrorIsf(t, err, context.Canceled, "%T.StateSync", sut.SummaryHandler)
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
		client := source.newSyncClient(t, withDatabase(fdb))

		summary, err := source.GetLastStateSummary(ctx)
		require.NoErrorf(t, err, "%T.GetLastStateSummary()", source.SummaryHandler)

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
			client := source.newSyncClient(t, withDatabase(fdb))
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

	client := sourceVM.newSyncClient(t)

	summary, err := sourceVM.GetLastStateSummary(t.Context())
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", sourceVM.SummaryHandler)
	require.NoErrorf(t, client.syncTo(t.Context(), t, summary), "%T.syncTo(%v)", client, summary)

	clientVM := client.restartAsVM(t, sourceVM.clock.Now())

	requireVMHead(t, clientVM, sourceVM.lastAcceptedBlock(t).ID())
}
