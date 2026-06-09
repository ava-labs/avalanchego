// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saedb

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/hashdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/saevm/firewood"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

const (
	// DefaultSnapshotCacheSizeMB is the snapshot cache size used by the executor.
	DefaultSnapshotCacheSizeMB = 128
	// DefaultTrieDBCacheSizeMB is the default cache size for trie nodes.
	DefaultTrieDBCacheSizeMB = 512
	// defaultCommitInterval is the default number of blocks between commits of the
	// state trie to disk.
	defaultCommitInterval = 4096

	mbToBytes = 1024 * 1024
)

// Config allows parameterization of the TrieDB and when
// state is committed.
type Config struct {
	Archival            bool   // whether to store every state on disk. Equivalent to setting [CommitInterval] to 1 for HashDB.
	Scheme              string // Default:[rawdb.HashScheme]. For Firewood, use [customrawdb.FirewoodScheme].
	SnapshotCacheSizeMB int    // Default: [DefaultSnapshotCacheSizeMB]. Set < 0 to disable snapshot.
	TrieDBCacheSizeMB   int    // Default: [DefaultTrieDBCacheSizeMB]. Set < 0 for a no-caching HashDB.
	TrieCommitInterval  uint64 // Default: [defaultCommitInterval].
}

// TrieDBConfig returns a config that can be used to create a [triedb.Database] based on
// the [Config] parameters provided.
//
// All [triedb.Database] must be closed, unless the TrieDB cache is disabled, as
// this will result in a memory leak.
func (c Config) TrieDBConfig(snowCtx *snow.Context) *triedb.Config {
	cacheSize := DefaultTrieDBCacheSizeMB
	switch {
	case c.TrieDBCacheSizeMB < 0:
		return nil
	case c.TrieDBCacheSizeMB > 0:
		cacheSize = c.TrieDBCacheSizeMB
	}

	switch c.Scheme {
	case customrawdb.FirewoodScheme:
		commitInterval := c.CommitInterval()
		if c.Archival {
			// TODO(alarso16): allow re-execution for archival nodes.
			commitInterval = 1
		}
		return &triedb.Config{
			DBOverride: firewood.TrieDBConfig{
				DatabaseDir:            snowCtx.ChainDataDir,
				CacheSizeBytes:         uint(cacheSize) * mbToBytes, // #nosec G115 -- known non-negative
				RevisionsInMemory:      uint(2 * c.CommitInterval()),
				DeferredCommitInterval: commitInterval,
				Archive:                c.Archival,
				Log:                    snowCtx.Log,
			}.BackendConstructor,
		}
	case rawdb.HashScheme, "":
		return &triedb.Config{
			HashDB: &hashdb.Config{
				CleanCacheSize: cacheSize,
			},
		}
	default:
		snowCtx.Log.Warn("unknown trie database scheme %q, defaulting to hashdb", zap.String("scheme", c.Scheme))
		return nil
	}
}

// CommitInterval returns the trie commit interval.
func (c Config) CommitInterval() uint64 {
	if c.TrieCommitInterval == 0 {
		return defaultCommitInterval
	}
	return c.TrieCommitInterval
}

func (c Config) snapConfig() *snapshot.Config {
	size := DefaultSnapshotCacheSizeMB
	switch {
	case c.Scheme == customrawdb.FirewoodScheme:
		return nil
	case c.SnapshotCacheSizeMB < 0:
		return nil
	case c.SnapshotCacheSizeMB > 0:
		size = c.SnapshotCacheSizeMB
	}

	return &snapshot.Config{
		CacheSize:  size,
		AsyncBuild: true,
	}
}

var _ StateDBOpener = (*Tracker)(nil)

// Tracker provides an abstraction to state-related operations, managing all
// database operations not exposed by the [state.StateDB] itself.
//
// All methods are safe to be called even after [Tracker.Close], but state
// will be unavailable.
type Tracker struct {
	config Config
	snaps  *snapshot.Tree
	cache  state.Database
	log    logging.Logger
}

// NewTracker provides a new [Tracker] on the underlying database.
func NewTracker(db ethdb.Database, c Config, snowCtx *snow.Context, lastExecuted common.Hash) (*Tracker, error) {
	cache := state.NewDatabaseWithConfig(db, c.TrieDBConfig(snowCtx))
	_, isHashDB := cache.TrieDB().Backend().(triedb.HashDB)
	if !isHashDB {
		return nil, fmt.Errorf("unsupported DB: %T", cache.TrieDB().Backend())
	}
	var snaps *snapshot.Tree
	if snapConf := c.snapConfig(); snapConf != nil {
		var err error
		snaps, err = snapshot.New(*snapConf, db, cache.TrieDB(), lastExecuted)
		if err != nil {
			return nil, errors.Join(err, cache.TrieDB().Close())
		}
	}
	return &Tracker{
		config: c,
		snaps:  snaps,
		cache:  cache,
		log:    snowCtx.Log,
	}, nil
}

// Track tracks the root and may commit the trie associated with the root
// to the database if [Config.ShouldCommitTrieDB] returns true, or the [Config]
// specifies that the node is archival.
//
// This state will be available in memory until [Tracker.Untrack] has been
// called for the root as many times as [Tracker.Track] has been called.
func (t *Tracker) Track(root common.Hash) {
	// Never returns an error because it's [triedb.HashDB].
	if err := t.cache.TrieDB().Reference(root, common.Hash{}); err != nil {
		t.log.Error("*triedb.Database.Reference()", zap.Error(err))
	}
}

// MaybeCommit potentially calls [triedb.Database.Commit], based on the
// following priorities:
//
// 1. If [Config.Archival] is true, then `executionRoot` will be committed.
// 2. If [ShouldCommitTrieDB] based on `height`, `settledRoot` is committed.
// 3. If there is sufficient memory pressure in HashDB, commits the oldest state to disk.
// 4. Otherwise, nothing is committed.
//
// This does NOT change in-memory tracking.
func (t *Tracker) MaybeCommit(settledRoot, executionRoot common.Hash, height uint64) error {
	var (
		commit  common.Hash
		because string
	)
	switch commitInterval := t.config.CommitInterval(); {
	case t.config.Archival:
		// If every state root should be available, then we can recover from a crash even if we commit the post-execution root.
		commit = executionRoot
		because = "post-execution archive"
	case t.config.Scheme == customrawdb.FirewoodScheme:
		fallthrough // firewood decides when to persist
	case ShouldCommitTrieDB(height, commitInterval):
		commit = settledRoot
		because = "settled"
	default:
		if err := t.maybeCap(height); err != nil {
			return fmt.Errorf("triedb.Database.Cap() at block %d: %v", height, err)
		}
		return nil
	}

	tdb := t.cache.TrieDB()
	if err := tdb.Commit(commit, false /* log */); err != nil {
		return fmt.Errorf("%T.Commit(%#x) %s at end of block %d: %v", tdb, settledRoot, because, height, err)
	}
	return nil
}

// maybeCap checks if the in-memory state of a HashDB is too high for an efficient
// [triedb.Database.Commit] and, if so, moves the oldest state to disk.
func (t *Tracker) maybeCap(height uint64) error {
	const (
		maxCap           = 512 * mbToBytes
		targetCommitSize = 20 * mbToBytes // for [triedb.Database.Commit]
	)

	// The cap shrinks linearly as we approach the commit interval
	commitInterval := t.config.CommitInterval()
	distanceFromCommit := commitInterval - height%commitInterval
	slope := common.StorageSize(maxCap-targetCommitSize) / common.StorageSize(commitInterval)
	targetCap := common.StorageSize(distanceFromCommit)*slope + targetCommitSize

	tdb := t.cache.TrieDB()
	_, inMemory, _ := tdb.Size()
	if inMemory <= targetCap {
		return nil
	}

	return tdb.Cap(targetCap - ethdb.IdealBatchSize) // avoid small DB writes
}

// LastHeightWithExecutionRootCommitted returns the greatest block height for
// which [Tracker.MaybeCommit] called [triedb.Database.Commit] with the
// post-execution state root of the block.
func LastHeightWithExecutionRootCommitted(db ethdb.Database, c Config, hooks hook.Points, lastSynchronous uint64) uint64 {
	switch head := rawdb.ReadHeadHeader(db).Number.Uint64(); {
	case head <= lastSynchronous:
		return lastSynchronous

	case c.Archival:
		return head

	default:
		num := LastCommittedTrieDBHeight(head, c.CommitInterval())
		if num <= lastSynchronous {
			return lastSynchronous
		}
		return hooks.SettledBy(
			rawdb.ReadHeader(
				db,
				rawdb.ReadCanonicalHash(db, num),
				num,
			),
		).Height
	}
}

// Untrack informs the [Tracker] that the state corresponding
// with `root` can have its reference count reduced. If the reference
// count is 0, the state will be removed from memory.
//
// This should be called on each block after its state is no longer
// needed. If the state is already on disk, no operation is performed.
func (t *Tracker) Untrack(root common.Hash) {
	// Never returns an error because it's [triedb.HashDB].
	if err := t.cache.TrieDB().Dereference(root); err != nil {
		t.log.Error("*triedb.Database.Dereference()", zap.Error(err))
	}
}

// StateDB provides a [state.StateDB] at the given root.
//
// Each [state.StateDB] can be constructed and used concurrently.
// However, the right to call [state.StateDB.Commit] is reserved
// for canonical blocks, as any other use could result in a memory
// leak or state corruption.
func (t *Tracker) StateDB(root common.Hash) (*state.StateDB, error) {
	return state.New(root, t.cache, t.snaps)
}

// Close releases all resources associated with the `[triedb.Database]`
// and persists `lastRoot` to the snapshot layer. `lastRoot` should be a
// recent state root.
func (t *Tracker) Close(lastRoot common.Hash) error {
	if t.snaps == nil {
		return t.cache.TrieDB().Close()
	}

	defer t.snaps.Release()

	// We don't use [snapshot.Tree.Journal] because re-orgs are impossible under
	// SAE so we don't mind flattening all snapshot layers to disk. Note that
	// calling `Cap([disk root], 0)` returns an error when it's actually a
	// no-op, so we ensure there are changes.
	if lastRoot != t.snaps.DiskRoot() {
		if err := t.snaps.Cap(lastRoot, 0); err != nil {
			return fmt.Errorf("snapshot.Tree.Cap([last post-execution state root], 0): %v", err)
		}
	}

	return nil
}
