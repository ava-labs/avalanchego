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
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
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
func writeBlock(tb testing.TB, tr *Tracker, prevRoot common.Hash, height uint64) common.Hash {
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

	root, err := sdb.Commit(height, true /*EIP-158*/)
	require.NoErrorf(tb, err, "%T.Commit(%d)", sdb, height)
	return root
}

// TestTrackerClose verifies both the trie and the snapshot can be opened at the
// state persisted by [Tracker.Close].
func TestTrackerClose(t *testing.T) {
	cfg := Config{
		CommitInterval:   DefaultCommitInterval,
		SnapshotCacheMiB: 1,
	}
	db := rawdb.NewMemoryDatabase()
	log := loggingtest.New(t, logging.Debug)
	tr, err := NewTracker(db, cfg, types.EmptyRootHash, t.TempDir(), log)
	require.NoError(t, err, "NewTracker()")

	// The snapshot is initially generated asynchronously. We wait for that to
	// complete here so that the later check can expect a complete snapshot.
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			assert.NoErrorf(c, tr.snaps.Verify(types.EmptyRootHash), "%T.Verify([genesis root])", tr.snaps)
		},
		10*time.Second,      // timeout
		10*time.Millisecond, // polling interval
		"genesis snapshot generation",
	)

	root := writeBlock(t, tr, types.EmptyRootHash, 1)
	require.NoErrorf(t, tr.Close(root), "%T.Close([root])", tr)

	cache := state.NewDatabase(db)
	t.Run("trie_available", func(t *testing.T) {
		_, err := state.New(root, cache, nil)
		require.NoError(t, err, "state.New([root])")
	})
	t.Run("snapshot_available", func(t *testing.T) {
		_, err := snapshot.New(
			snapshot.Config{
				CacheSize: 1,
				NoBuild:   true,
			},
			db,
			cache.TrieDB(),
			root,
		)
		require.NoError(t, err, "snapshot.New(NoBuild, [root])")
	})
}

// TestTrackerMaybeCap checks that [Tracker.BlockExecuted] decreases memory
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
		require.NoErrorf(t, tr.BlockExecuted(common.Hash{}, root, height), "%T.BlockExecuted() at height %d", tr, height)
		after := inMemorySize()

		// Invariant: whatever schedule maybeCap uses to shrink its target, the
		// in-memory size never exceeds the configured maximum after BlockExecuted.
		require.LessOrEqualf(t, after, common.StorageSize(maxCapBytes), "in-memory size exceeds the maximum cap after %T.BlockExecuted() at height %d", tr, height)

		// BlockExecuted can ONLY decrease memory pressure
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
	require.NoErrorf(t, tr.BlockExecuted(root, root, commitInterval), "%T.BlockExecuted() at height %d", tr, commitInterval)
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
						require.NoErrorf(b, tr.BlockExecuted(root, root, height), "%T.BlockExecuted() at height %d", tr, height)
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

// readOnlySchemes are the schemes the read-only guarantee must hold on.
var readOnlySchemes = []string{rawdb.HashScheme, customrawdb.FirewoodScheme}

// newSchemeTracker returns a [Tracker] on a fresh database, and that database.
func newSchemeTracker(t *testing.T, scheme string) (*Tracker, ethdb.Database) {
	t.Helper()

	disk := rawdb.NewMemoryDatabase()
	cfg := Config{CommitInterval: DefaultCommitInterval, Scheme: scheme}
	tr, err := NewTracker(disk, cfg, types.EmptyRootHash, t.TempDir(), loggingtest.New(t, logging.Debug))
	require.NoErrorf(t, err, "NewTracker(%q)", scheme)
	t.Cleanup(func() {
		assert.NoErrorf(t, tr.Close(types.EmptyRootHash), "%T.Close()", tr)
	})
	return tr, disk
}

// TestReadOnlyStateDB verifies that read-only state hashes but cannot be
// committed, on every scheme rather than only where the backend enforces it.
func TestReadOnlyStateDB(t *testing.T) {
	for _, scheme := range readOnlySchemes {
		t.Run(scheme, func(t *testing.T) {
			tr, _ := newSchemeTracker(t, scheme)

			sdb, err := tr.ReadOnlyStateDB(types.EmptyRootHash)
			require.NoErrorf(t, err, "%T.ReadOnlyStateDB()", tr)
			sdb.SetNonce(common.Address{1}, 1)
			got := sdb.IntermediateRoot(true)

			canonical, err := tr.StateDB(types.EmptyRootHash)
			require.NoErrorf(t, err, "%T.StateDB()", tr)
			canonical.SetNonce(common.Address{1}, 1)
			require.Equal(t, canonical.IntermediateRoot(true), got, "read-only root matches canonical")

			_, err = sdb.Commit(1, true)
			require.ErrorIsf(t, err, ErrReadOnlyStateDB, "%T.Commit() on read-only state", sdb)

			// Canonical state stays committable.
			_, err = canonical.Commit(1, true)
			require.NoErrorf(t, err, "%T.Commit() on canonical state", canonical)
		})
	}
}

// TestReadOnlyStateDBDiscardsCodeWrites verifies that a rejected commit leaves
// nothing on disk, since [state.StateDB.Commit] flushes code first.
func TestReadOnlyStateDBDiscardsCodeWrites(t *testing.T) {
	for _, scheme := range readOnlySchemes {
		t.Run(scheme, func(t *testing.T) {
			tr, disk := newSchemeTracker(t, scheme)

			code := []byte{0x60, 0x00, 0x60, 0x00}
			hash := crypto.Keccak256Hash(code)

			sdb, err := tr.ReadOnlyStateDB(types.EmptyRootHash)
			require.NoErrorf(t, err, "%T.ReadOnlyStateDB()", tr)
			// Code without storage writes, so no storage trie errors first.
			sdb.SetCode(common.Address{0xAA}, code)
			sdb.SetNonce(common.Address{0xAA}, 1)

			_, err = sdb.Commit(1, true)
			require.ErrorIsf(t, err, ErrReadOnlyStateDB, "%T.Commit()", sdb)
			require.Falsef(t, rawdb.HasCode(disk, hash), "code %s persisted by a rejected commit", hash)
		})
	}
}

// TestReadOnlyDiskDBRefusesClose verifies that a read-only view cannot close the
// store the rest of the node is using.
func TestReadOnlyDiskDBRefusesClose(t *testing.T) {
	for _, scheme := range readOnlySchemes {
		t.Run(scheme, func(t *testing.T) {
			tr, disk := newSchemeTracker(t, scheme)

			ro := readOnlyDatabase{tr.cache}
			require.NoError(t, ro.DiskDB().Close(), "read-only DiskDB().Close()")
			require.NoErrorf(t, disk.Put([]byte("k"), []byte("v")), "%T still usable afterwards", disk)
		})
	}
}

// TestReadOnlyDatabaseWrapsEveryTrie verifies that the commit rejection reaches
// storage tries and copies, not just the account trie the StateDB opens first.
func TestReadOnlyDatabaseWrapsEveryTrie(t *testing.T) {
	for _, scheme := range readOnlySchemes {
		t.Run(scheme, func(t *testing.T) {
			tr, _ := newSchemeTracker(t, scheme)
			ro := readOnlyDatabase{tr.cache}

			acct, err := ro.OpenTrie(types.EmptyRootHash)
			require.NoError(t, err, "OpenTrie()")
			requireRejectsCommit(t, acct, "account trie")
			requireRejectsCommit(t, ro.CopyTrie(acct), "copy of the account trie")

			addr := common.Address{1}
			storage, err := ro.OpenStorageTrie(types.EmptyRootHash, addr, types.EmptyRootHash, acct)
			require.NoError(t, err, "OpenStorageTrie()")
			requireRejectsCommit(t, storage, "storage trie")

			// A nil copy MUST stay nil rather than become a non-nil interface
			// holding a nil trie, which the StateDB would then use.
			if cp := ro.CopyTrie(storage); cp != nil {
				requireRejectsCommit(t, cp, "copy of the storage trie")
				require.NotPanicsf(t, func() { cp.Hash() }, "a non-nil copy is usable")
			}
		})
	}
}

func requireRejectsCommit(t *testing.T, tr state.Trie, what string) {
	t.Helper()

	require.NotNilf(t, tr, "%s is not nil", what)
	_, _, err := tr.Commit(true)
	require.ErrorIsf(t, err, ErrReadOnlyStateDB, "%s Commit()", what)
}
