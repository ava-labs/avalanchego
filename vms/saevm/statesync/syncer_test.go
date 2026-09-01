// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/triedb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// TestShouldAcceptSummary checks the cases in which
// [Handler.ShouldAcceptSummary] refuses to state sync.
func TestShouldAcceptSummary(t *testing.T) {
	tests := []struct {
		name    string
		newSUT  func(t *testing.T) *sut
		summary *Summary
		want    bool
	}{
		{
			name: "summary_at_genesis_height",
			newSUT: func(t *testing.T) *sut {
				return newSUT(t)
			},
			summary: &Summary{},
		},
		{
			name: "blocks_already_accepted",
			newSUT: func(t *testing.T) *sut {
				vm := newVM(t)
				vm.acceptBlocks(t, defaultCommitInterval)
				return vm.sut
			},
			summary: &Summary{AcceptedHeight: 1},
		},
		{
			name: "not_enabled",
			newSUT: func(t *testing.T) *sut {
				return newSUT(t, withEnabled(false))
			},
			summary: &Summary{AcceptedHeight: 1},
		},
		{
			name: "valid_summary",
			newSUT: func(t *testing.T) *sut {
				return newSUT(t)
			},
			summary: &Summary{AcceptedHeight: 1},
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			syncer := tt.newSUT(t).syncer()
			require.Equalf(t, tt.want, syncer.ShouldAcceptSummary(tt.summary), "%T.ShouldAcceptSummary()", syncer)
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

	// The sync must be visible on both sides' metrics: the client counted the
	// requests it sent, and the source counted the requests it served. Code is
	// deliberately absent: the client's genesis commit already wrote the only
	// contract's code to its database, so no code request is ever needed.
	for _, name := range []string{
		"sync_state_trie_leaves_requested",
		"sync_state_trie_leaves_received",
		"sync_blocks_received",
	} {
		requireCounterPositive(t, client.reg, name)
	}
	for _, name := range []string{
		"leafs_request_count",
		"block_request_count",
	} {
		requireCounterPositive(t, sourceVM.reg, name)
	}

	// During the sync, the network continued processing
	sourceVM.acceptBlocks(t, numBlocks)

	// catch up a new VM
	clientVM := client.asVM(t, sourceVM.hooks.Now())
	lastHeight := sourceVM.lastAcceptedBlock(t).Height()
	for height := summary.Height() + 1; height <= lastHeight; height++ {
		b := sourceVM.blockAtHeight(t, height)
		parsed, err := clientVM.ParseBlock(ctx, b.Bytes())
		require.NoErrorf(t, err, "ParseBlock(%d)", b.Height())
		require.NoErrorf(t, clientVM.vm.VerifyBlock(ctx, nil, parsed), "VerifyBlock(%d)", b.Height())
		require.NoErrorf(t, clientVM.vm.AcceptBlock(ctx, parsed), "AcceptBlock(%d)", b.Height())
		require.NoErrorf(t, parsed.WaitUntilExecuted(ctx), "WaitUntilExecuted(%d)", b.Height())
	}
	sourceVM.compareVMs(t, clientVM)
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
	clientVM := client.asVM(t, sourceVM.clock.Now())
	sourceVM.compareVMs(t, clientVM)
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

	clientVM := client.asVM(t, sourceVM.clock.Now())
	sourceVM.compareVMs(t, clientVM)
}

func TestCancelSync(t *testing.T) {
	t.Parallel()

	sut := newSUT(t)
	syncer := sut.syncer()

	// No peers are connected, so the sync stalls until canceled.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := syncer.Sync(ctx, NewSummary(common.Hash{0xde, 0xad}, defaultCommitInterval))
	require.ErrorIsf(t, err, context.Canceled, "%T.Sync", syncer)
}

// TestSyncErrorRecovers checks that any error in the sync process gracefully
// errors and is recoverable.
func TestSyncErrorRecovers(t *testing.T) {
	ctx := t.Context()

	source := newVM(t)
	source.acceptBlocks(t, defaultCommitInterval+2)

	summary, err := source.GetLastStateSummary(ctx)
	require.NoErrorf(t, err, "%T.GetLastStateSummary()", source.Handler)
	expectedRoot := source.getBlock(t, ids.ID(summary.AcceptedHash)).SettledStateRoot()

	for failAfter := 0; ; failAfter++ {
		fdb := saetest.NewFlakyDB(memdb.New(), math.MaxInt)
		client := newSUT(t, withDatabase(fdb))
		saetest.ConnectTo[saetest.Peer](t, client, source)
		fdb.SetFailAfter(failAfter) // Setup (e.g. the genesis commit) is done.

		err = client.syncTo(ctx, t, summary)
		if !fdb.Failed() {
			require.NoErrorf(t, err, "%T.syncTo", client)
			// Opening the snapshot also reads through the flaky database.
			fdb.SetFailAfter(math.MaxInt)
			requireSnapshotOnDisk(t, client.db, expectedRoot)
			return
		}
		require.ErrorIsf(t, err, saetest.ErrInjected, "%T.syncTo()", client)

		// Any error should be recoverable.
		t.Run("second_try", func(t *testing.T) {
			fdb.SetFailAfter(math.MaxInt)
			client := newSUT(t, withDatabase(fdb))
			saetest.ConnectTo[saetest.Peer](t, client, source)
			require.NoErrorf(t, client.syncTo(t.Context(), t, summary), "%T.syncTo()", client)
			requireSnapshotOnDisk(t, client.db, expectedRoot)
		})
	}
}

// requireSnapshotOnDisk checks that a completed sync left a snapshot that can
// be loaded from disk without rebuilding.
func requireSnapshotOnDisk(t *testing.T, db ethdb.Database, root common.Hash) {
	t.Helper()

	conf := snapshot.Config{
		CacheSize: int(saedb.DefaultSnapshotCacheSizeMiB),
		NoBuild:   true, // i.e. MUST be loaded from disk
	}
	snap, err := snapshot.New(conf, db, triedb.NewDatabase(db, nil), root)
	require.NoErrorf(t, err, "snapshot.New(NoBuild, ..., %#x)", root)
	snap.Release()
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

	clientVM := client.asVM(t, sourceVM.hooks.Now())

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
			parent := common.Hash{0xbe, 0xef}
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
	syncer := client.syncer()
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

// requireCounterPositive asserts that the counter named name is registered on
// reg and has been incremented.
func requireCounterPositive(t *testing.T, reg *prometheus.Registry, name string) {
	t.Helper()

	mfs, err := reg.Gather()
	require.NoError(t, err, "reg.Gather()")
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		var total float64
		for _, m := range mf.GetMetric() {
			total += m.GetCounter().GetValue()
		}
		require.Positivef(t, total, "counter %q", name)
		return
	}
	t.Fatalf("counter %q not registered", name)
}
