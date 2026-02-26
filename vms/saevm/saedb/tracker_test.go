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
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
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
			snowCtx := snowtest.Context(t, ids.Empty)
			snowCtx.Log = loggingtest.New(t, logging.Debug)
			cfg := defaults
			if tt.with != nil {
				tt.with(&cfg)
			}
			db := rawdb.NewMemoryDatabase()

			tr, err := NewTracker(db, cfg, snowCtx, types.EmptyRootHash)
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

	snowCtx := snowtest.Context(t, ids.Empty)
	snowCtx.Log = loggingtest.New(t, logging.Debug)

	tr, err := NewTracker(rawdb.NewMemoryDatabase(), cfg, snowCtx, types.EmptyRootHash)
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

				var (
					maxPause  time.Duration
					peakDirty common.StorageSize
				)
				for b.Loop() {
					b.StopTimer()
					db := tt.open(b)
					tr, err := NewTracker(db, cfg, snowtest.Context(b, ids.Empty), types.EmptyRootHash)
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
