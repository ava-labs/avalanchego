// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saedb

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/trie"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
)

func TestNewTracker(t *testing.T) {
	defaults := Config{CommitInterval: 1}

	tests := []struct {
		name    string
		with    func(*Config)
		wantErr error
	}{
		{
			name: "defaults",
		},
		{
			name: "firewood",
			with: func(c *Config) { c.Scheme = customrawdb.FirewoodScheme },
		},
		{
			name:    "zero_commit_interval",
			with:    func(c *Config) { c.CommitInterval = 0 },
			wantErr: errZeroCommitInterval,
		},
		{
			name: "with_snapshot",
			with: func(c *Config) {
				c.SnapshotCacheMiB = 1
			},
		},
		{
			name:    "trie_cache_overflows_bytes",
			with:    func(c *Config) { c.TrieCacheMiB = math.MaxInt },
			wantErr: errCacheTooLarge,
		},
		{
			name:    "snapshot_cache_overflows_bytes",
			with:    func(c *Config) { c.SnapshotCacheMiB = math.MaxInt },
			wantErr: errCacheTooLarge,
		},
		{
			name:    "unknown_scheme",
			with:    func(c *Config) { c.Scheme = rawdb.PathScheme },
			wantErr: errUnknownScheme,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaults
			if tt.with != nil {
				tt.with(&cfg)
			}
			db := rawdb.NewMemoryDatabase()
			log := loggingtest.New(t, logging.Debug)

			tr, err := NewTracker(db, cfg, types.EmptyRootHash, t.TempDir(), log)
			require.ErrorIs(t, err, tt.wantErr, "NewTracker()")
			if err != nil {
				return
			}
			require.NoErrorf(t, tr.Close(types.EmptyRootHash), "%T.Close()", tr)

			// If the snapshot is enabled, we would expect to find the root on disk.
			var wantRoot common.Hash
			if cfg.SnapshotCacheMiB > 0 {
				wantRoot = types.EmptyRootHash
			}
			gotRoot := rawdb.ReadSnapshotRoot(db)
			require.Equal(t, wantRoot, gotRoot, "rawdb.ReadSnapshotRoot()")
		})
	}
}

// TestProtectTrieIndex simulates every pair of consecutive node runs against
// the same database. The first run, on a fresh database, always succeeds;
// only the second may error.
func TestProtectTrieIndex(t *testing.T) {
	configs := map[string]Config{
		"archival":        {Archival: true},
		"archival_allow":  {Archival: true, AllowMissingTries: true},
		"pruning":         {},
		"allowed_pruning": {AllowMissingTries: true},
	}
	wantErrs := map[string]error{
		"archival_then_pruning":       errRefuseToCorruptArchiver,
		"archival_allow_then_pruning": errRefuseToCorruptArchiver,
	}

	for name1, config1 := range configs {
		for name2, config2 := range configs {
			name := name1 + "_then_" + name2
			t.Run(name, func(t *testing.T) {
				db := rawdb.NewMemoryDatabase()
				require.NoError(t, protectTrieIndex(db, config1), "protectTrieIndex(%+v) on fresh DB", config1)
				require.ErrorIs(t, protectTrieIndex(db, config2), wantErrs[name], "protectTrieIndex(%+v) after first run", config2)
			})
		}
	}

	// An allowed pruning run bypasses the protection of an earlier archival
	// run without disabling it, so a later pruning run must still refuse.
	t.Run("archival_then_allowed_pruning_then_pruning", func(t *testing.T) {
		db := rawdb.NewMemoryDatabase()
		require.NoError(t, protectTrieIndex(db, configs["archival"]), "protectTrieIndex() archival run on fresh DB")
		require.NoError(t, protectTrieIndex(db, configs["allowed_pruning"]), "protectTrieIndex() allowed pruning run after archival")
		require.ErrorIs(t, protectTrieIndex(db, configs["pruning"]), errRefuseToCorruptArchiver, "protectTrieIndex() pruning run after allowed pruning")
	})
}

// writeBlock simulates the execution of a block by opening a [state.StateDB]
// at `prevRoot`, writing new accounts and storage unique to `height`, and
// committing the result, returning the post-"execution" root.
//
// Each call adds roughly 100 KiB of dirty trie nodes to the [Tracker]'s
// in-memory cache.
func writeBlock(tb testing.TB, tr *Tracker, prevRoot common.Hash, height uint64, opts ...stateconf.StateDBCommitOption) common.Hash {
	tb.Helper()

	sdb, err := tr.StateDB(prevRoot)
	require.NoErrorf(tb, err, "%T.StateDB(%#x)", tr, prevRoot)

	const (
		accountsPerBlock uint64 = 64
		slotsPerAccount  uint64 = 16
	)
	for i := range accountsPerBlock {
		var addr common.Address
		binary.BigEndian.PutUint64(addr[:8], height)
		binary.BigEndian.PutUint64(addr[8:16], i)
		sdb.SetNonce(addr, height) // MUST have a non-empty account
		for s := range slotsPerAccount {
			var key, val common.Hash
			binary.BigEndian.PutUint64(key[:8], s)
			binary.BigEndian.PutUint64(val[:8], height)
			val[31] = 1 // guarantee a non-zero value so the slot is stored
			sdb.SetState(addr, key, val)
		}
	}

	root, err := sdb.Commit(height, true /*EIP-158*/, opts...)
	require.NoErrorf(tb, err, "%T.Commit(%d)", sdb, height)
	return root
}

// TestTrackerCloseCommitsTrie verifies that, with the snapshot enabled,
// [Tracker.Close] persists the trie at the root it is closed with. A restart
// resumes any in-progress snapshot generation against the persisted snapshot
// disk root — which Close flattens to that same root — so the trie there MUST
// be resolvable from disk, not lost with the in-memory dirty cache.
func TestTrackerCloseCommitsTrie(t *testing.T) {
	cfg := Config{CommitInterval: DefaultCommitInterval, SnapshotCacheMiB: 1}
	db := rawdb.NewMemoryDatabase()
	tr, err := NewTracker(db, cfg, types.EmptyRootHash, t.TempDir(), loggingtest.New(t, logging.Info))
	require.NoError(t, err, "NewTracker()")

	root := writeBlock(t, tr, types.EmptyRootHash, 1)
	tr.Track(root)
	require.NoErrorf(t, tr.Close(root), "%T.Close()", tr)

	cache := state.NewDatabase(db)
	t.Cleanup(func() { assert.NoErrorf(t, cache.TrieDB().Close(), "%T.Close()", cache.TrieDB()) })
	_, err = state.New(root, cache, nil)
	require.NoErrorf(t, err, "state.New() from disk at the root passed to %T.Close()", tr)
}

// TestTrackerReopenLoadsPersistedSnapshot restarts a Tracker at an older root
// than it was closed with, as [sae] recovery does when it boots from the last
// committed (settled) root while the persisted snapshot disk layer sits at the
// last-executed root. The persisted snapshot MUST be loaded as-is — recovery's
// re-execution catches the chain head up to the disk layer — rather than
// discarded and regenerated, which may take hours on a mainnet-sized state.
func TestTrackerReopenLoadsPersistedSnapshot(t *testing.T) {
	cfg := Config{CommitInterval: DefaultCommitInterval, SnapshotCacheMiB: 1}
	db := rawdb.NewMemoryDatabase()
	log := loggingtest.New(t, logging.Info)
	tr, err := NewTracker(db, cfg, types.EmptyRootHash, t.TempDir(), log)
	require.NoError(t, err, "NewTracker()")

	// Wait for genesis generation so the persisted generator is marked Done;
	// a loaded-but-regenerating snapshot would be indistinguishable from a
	// discarded one below.
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			assert.NoErrorf(c, tr.snaps.Verify(types.EmptyRootHash), "%T.Verify([genesis root])", tr.snaps)
		},
		10*time.Second,      // timeout
		10*time.Millisecond, // polling interval
		"genesis snapshot generation",
	)

	root1 := writeBlock(t, tr, types.EmptyRootHash, 1, tr.StateDBCommitOptions()...)
	tr.Track(root1)
	root2 := writeBlock(t, tr, root1, 2, tr.StateDBCommitOptions()...)
	tr.Track(root2)
	require.NoErrorf(t, tr.Close(root2), "%T.Close()", tr)
	require.Equal(t, root2, rawdb.ReadSnapshotRoot(db), "rawdb.ReadSnapshotRoot() after Close: sanity-check the persisted disk root")

	tr2, err := NewTracker(db, cfg, root1, t.TempDir(), log)
	require.NoError(t, err, "NewTracker() reopening at an older root")
	t.Cleanup(func() { assert.NoErrorf(t, tr2.Close(root2), "%T.Close()", tr2) })

	require.Equalf(t, root2, tr2.Snapshot().DiskRoot(), "%T.DiskRoot() after reopening at an older root: the persisted snapshot MUST be loaded, not rebuilt at the requested root", tr2.Snapshot())
	require.NoErrorf(t, tr2.Snapshot().Verify(root2), "%T.Verify([persisted disk root]) after reopening", tr2.Snapshot())
}

// TestTrackerMaybeCap checks that [Tracker.MaybeCommit] decreases memory
// pressure to prevent a [triedb.Database.Commit] from being too expensive.
func TestTrackerMaybeCap(t *testing.T) {
	const (
		commitInterval    = 64
		maxCapBytes       = 2 * mibToBytes
		targetCommitBytes = 128 * 1024

		// MUST be > [ethdb.IdealBatchSize] so that [Tracker.maybeCap] never
		// calls Cap with a negative limit.
		_ uint = targetCommitBytes - ethdb.IdealBatchSize
	)

	cfg := Config{
		CommitInterval:    commitInterval,
		maxCapBytes:       maxCapBytes,
		targetCommitBytes: targetCommitBytes,
	}

	log := loggingtest.New(t, logging.Debug)
	tr, err := NewTracker(rawdb.NewMemoryDatabase(), cfg, types.EmptyRootHash, t.TempDir(), log)
	require.NoError(t, err, "NewTracker()")

	prevRoot := types.EmptyRootHash
	t.Cleanup(func() { assert.NoErrorf(t, tr.Close(prevRoot), "%T.Close()", tr) })

	inMemorySize := func() common.StorageSize {
		_, dirties, _ := tr.cache.TrieDB().Size()
		return dirties
	}

	var capsFired int
	for height := uint64(1); height < cfg.CommitInterval; height++ {
		root := writeBlock(t, tr, prevRoot, height)
		before := inMemorySize()
		require.NoErrorf(t, tr.MaybeCommit(common.Hash{}, root, height), "%T.MaybeCommit() at height %d", tr, height)
		after := inMemorySize()

		// Invariant: whatever schedule maybeCap uses to shrink its target, the
		// in-memory size never exceeds the configured maximum after MaybeCommit.
		require.LessOrEqualf(t, after, common.StorageSize(maxCapBytes), "in-memory size exceeds the maximum cap after %T.MaybeCommit() at height %d", tr, height)

		// MaybeCommit can ONLY decrease memory pressure
		if after < before {
			capsFired++
		}
		prevRoot = root
	}

	// Each run will generate the same state, so this is deterministic
	require.Greater(t, capsFired, 5, "test did not generate enough state to exercise capping")

	root := writeBlock(t, tr, prevRoot, commitInterval)
	prevRoot = root // for cleanup
	before := inMemorySize()
	require.NoErrorf(t, tr.MaybeCommit(root, root, commitInterval), "%T.MaybeCommit() at height %d", tr, commitInterval)
	require.Less(t, inMemorySize(), before, "in-memory size did not drop after commit at the interval")
}

// BenchmarkTrackerCommitInterval measures the cost of block processing under
// a [Tracker] over a full commit interval.
//
// Each database runs in two modes to isolate the effect of capping:
//   - capped: [Tracker.maybeCap] flushes state throughout the interval.
//   - uncapped: the cap never fires, so all dirty state accumulates until
//     the single trie commit at the interval boundary.
//
// The goal is to minimize the `max-pause-ms` metric, which is the maximum time
// spent in a single block.
func BenchmarkTrackerCommitInterval(b *testing.B) {
	const (
		maxCapBytes       = 8 * mibToBytes
		targetCommitBytes = 512 * 1024

		// MUST be >= [ethdb.IdealBatchSize] so that [Tracker.maybeCap] never
		// calls Cap with a negative limit.
		_ uint = targetCommitBytes - ethdb.IdealBatchSize
	)

	modes := []struct {
		name        string
		maxCapBytes common.StorageSize
	}{
		{name: "capped", maxCapBytes: maxCapBytes},
		// Large enough that the target cap always exceeds the dirty size.
		{name: "uncapped", maxCapBytes: 1 << 40},
	}

	// Each call to open MUST return a fresh, empty database.
	tests := []struct {
		name string
		open func(b *testing.B) ethdb.Database
	}{
		{
			name: "memdb",
			open: func(*testing.B) ethdb.Database {
				return rawdb.NewMemoryDatabase()
			},
		},
		{
			name: "avalanchego_pebble",
			open: func(b *testing.B) ethdb.Database {
				db, err := pebbledb.New(b.TempDir(), nil, loggingtest.New(b, logging.Debug), prometheus.NewRegistry())
				require.NoError(b, err, "pebbledb.New()")
				return rawdb.NewDatabase(evmdb.New(db))
			},
		},
		{
			name: "avalanchego_leveldb",
			open: func(b *testing.B) ethdb.Database {
				db, err := leveldb.New(b.TempDir(), nil, loggingtest.New(b, logging.Debug), prometheus.NewRegistry())
				require.NoError(b, err, "leveldb.New()")
				return rawdb.NewDatabase(evmdb.New(db))
			},
		},
	}
	for _, tt := range tests {
		for _, mode := range modes {
			b.Run(tt.name+"/"+mode.name, func(b *testing.B) {
				cfg := Config{
					CommitInterval:    64,
					TrieCacheMiB:      1,
					maxCapBytes:       mode.maxCapBytes,
					targetCommitBytes: targetCommitBytes,
				}
				log := loggingtest.New(b, logging.Debug)

				var (
					maxPause  time.Duration
					peakDirty common.StorageSize
				)
				for b.Loop() {
					b.StopTimer()
					db := tt.open(b)
					tr, err := NewTracker(db, cfg, types.EmptyRootHash, b.TempDir(), log)
					require.NoError(b, err, "NewTracker()")
					b.StartTimer()

					prevRoot := types.EmptyRootHash
					for height := uint64(1); height <= cfg.CommitInterval; height++ {
						root := writeBlock(b, tr, prevRoot, height)

						_, dirty, _ := tr.cache.TrieDB().Size()
						peakDirty = max(peakDirty, dirty)

						start := time.Now()
						require.NoErrorf(b, tr.MaybeCommit(root, root, height), "%T.MaybeCommit() at height %d", tr, height)
						maxPause = max(maxPause, time.Since(start))

						prevRoot = root
					}

					b.StopTimer()
					require.NoErrorf(b, tr.Close(prevRoot), "%T.Close()", tr)
					require.NoErrorf(b, db.Close(), "%T.Close()", db)
					b.StartTimer()
				}
				b.ReportMetric(float64(cfg.CommitInterval), "blocks/op")
				b.ReportMetric(float64(maxPause.Milliseconds()), "max-pause-ms")
				b.ReportMetric(float64(peakDirty)/mibToBytes, "peak-dirty-MiB")
			})
		}
	}
}

func TestTrackerStateDBCommitOptions(t *testing.T) {
	log := loggingtest.New(t, logging.Info)

	tests := []struct {
		name       string
		cfg        Config
		wantLayers int
	}{
		{
			name:       "snapshots enabled",
			cfg:        Config{CommitInterval: 4096, SnapshotCacheMiB: 16},
			wantLayers: SnapshotCapLayers,
		},
		{
			name: "snapshots disabled",
			cfg:  Config{CommitInterval: 4096},
			// Without snapshots there are no options, so extraction falls
			// back to libevm's default.
			wantLayers: stateconf.DefaultSnapshotCapLayers,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := NewTracker(rawdb.NewMemoryDatabase(), tt.cfg, types.EmptyRootHash, t.TempDir(), log)
			require.NoError(t, err, "NewTracker()")
			t.Cleanup(func() { assert.NoErrorf(t, tr.Close(types.EmptyRootHash), "%T.Close()", tr) })

			opts := tr.StateDBCommitOptions()
			if tt.cfg.SnapshotCacheMiB == 0 {
				require.Empty(t, opts, "Tracker.StateDBCommitOptions() with snapshots disabled")
			}
			require.Equal(t, tt.wantLayers, stateconf.ExtractSnapshotCapLayers(opts...), "cap layers extracted from Tracker.StateDBCommitOptions()")
		})
	}
}

// TestTrackerPinSnapshotDiskRoot drives the flatten + settle + pin cycle
// directly: the pinned disk root's trie MUST stay resolvable after the VM
// untracks it (settlement-based pruning), and the pin MUST transfer — and
// release the previous root — when the disk layer advances.
func TestTrackerPinSnapshotDiskRoot(t *testing.T) {
	cfg := Config{CommitInterval: 4096, SnapshotCacheMiB: 16}
	log := loggingtest.New(t, logging.Info)
	tr, err := NewTracker(rawdb.NewMemoryDatabase(), cfg, types.EmptyRootHash, t.TempDir(), log)
	require.NoError(t, err, "NewTracker()")

	root1 := writeBlock(t, tr, types.EmptyRootHash, 1, tr.StateDBCommitOptions()...)
	tr.Track(root1)

	// Force the flatten that [state.StateDB.Commit] performs whenever
	// generation is in progress: root1 becomes the disk layer's root.
	require.NoErrorf(t, tr.snaps.Cap(root1, 0), "%T.Cap(root1, 0)", tr.snaps)
	require.Equal(t, root1, tr.Snapshot().DiskRoot(), "snapshot disk root after flattening block 1")
	tr.PinSnapshotDiskRoot()

	root2 := writeBlock(t, tr, root1, 2, tr.StateDBCommitOptions()...)
	tr.Track(root2)

	// Settlement-based pruning: the VM untracks root1 once settled. The pin
	// MUST keep the disk root's trie resolvable regardless.
	tr.Untrack(root1)
	_, err = trie.New(trie.TrieID(root1), tr.TrieDB())
	require.NoError(t, err, "trie.New() at the pinned snapshot disk root after Untrack()")

	// The disk layer advances to root2; the pin transfers, releasing root1,
	// whose (already untracked) trie is now pruned.
	require.NoErrorf(t, tr.snaps.Cap(root2, 0), "%T.Cap(root2, 0)", tr.snaps)
	tr.PinSnapshotDiskRoot()
	_, err = trie.New(trie.TrieID(root1), tr.TrieDB())
	require.ErrorAs(t, err, new(*trie.MissingNodeError), "trie.New() at the released previous disk root: the transferred pin must drop its reference")
	_, err = trie.New(trie.TrieID(root2), tr.TrieDB())
	require.NoError(t, err, "trie.New() at the newly pinned snapshot disk root")

	// Close releases the pin without error.
	require.NoErrorf(t, tr.Close(root2), "%T.Close()", tr)
}

func TestTrackerPinSnapshotDiskRootWithoutSnapshots(t *testing.T) {
	cfg := Config{CommitInterval: 4096} // snapshots disabled
	log := loggingtest.New(t, logging.Info)
	tr, err := NewTracker(rawdb.NewMemoryDatabase(), cfg, types.EmptyRootHash, t.TempDir(), log)
	require.NoError(t, err, "NewTracker()")
	t.Cleanup(func() { assert.NoErrorf(t, tr.Close(types.EmptyRootHash), "%T.Close()", tr) })

	tr.PinSnapshotDiskRoot() // MUST NOT panic
}
