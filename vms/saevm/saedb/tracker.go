// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saedb

import (
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/hashdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	// DefaultCommitInterval is the recommended number of blocks between commits
	// of the state trie to disk.
	DefaultCommitInterval = 4096

	// DefaultTrieCacheSizeMiB is the recommended cache size for the
	// [triedb.Database] used by a [Tracker].
	DefaultTrieCacheSizeMiB = 512

	// DefaultSnapshotCacheSizeMiB is the recommended snapshot cache size used
	// by a [Tracker].
	DefaultSnapshotCacheSizeMiB = 256

	mibToBytes = 1024 * 1024

	// maxCacheMiB is the largest MiB value that can be converted to bytes
	// without overflowing an int.
	maxCacheMiB = math.MaxInt / mibToBytes
)

// Config allows parameterization of the TrieDB and when state is committed.
type Config struct {
	TrieCacheMiB     uint64 // size of the TrieDB cache
	SnapshotCacheMiB uint64 // size of the snapshot cache - if 0, snapshots are disabled
	Archival         bool   // if true, will store every state on disk
	CommitInterval   uint64 // MUST be set to a non-zero value
}

func (c Config) Verify() error {
	if c.CommitInterval == 0 {
		return errZeroCommitInterval
	}
	if c.TrieCacheMiB > maxCacheMiB {
		return fmt.Errorf("%w: TrieCacheMiB (%d)", errCacheTooLarge, c.TrieCacheMiB)
	}
	if c.SnapshotCacheMiB > maxCacheMiB {
		return fmt.Errorf("%w: SnapshotCacheMiB (%d)", errCacheTooLarge, c.SnapshotCacheMiB)
	}
	return nil
}

func (c Config) TrieDBConfig() *triedb.Config {
	return &triedb.Config{
		HashDB: &hashdb.Config{
			CleanCacheSize: int(c.TrieCacheMiB) * mibToBytes, //#nosec G115 // checked in [Config.Verify]
		},
	}
}

func (c Config) snapConfig() *snapshot.Config {
	if c.SnapshotCacheMiB <= 0 {
		return nil
	}
	return &snapshot.Config{
		CacheSize:  int(c.SnapshotCacheMiB), //#nosec G115 // checked in [Config.Verify]
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
	snaps *snapshot.Tree
	cache state.Database

	config Config
	log    logging.Logger
}

var (
	errZeroCommitInterval = errors.New("commit interval must be non-zero")
	errCacheTooLarge      = fmt.Errorf("cache size exceeds maximum of %d MiB", maxCacheMiB)
)

// NewTracker provides a new [Tracker] on the underlying database.
func NewTracker(db ethdb.Database, c Config, lastExecuted common.Hash, log logging.Logger) (*Tracker, error) {
	if err := c.Verify(); err != nil {
		return nil, err
	}
	cache := state.NewDatabaseWithConfig(db, c.TrieDBConfig())
	var snaps *snapshot.Tree
	if snapConf := c.snapConfig(); snapConf != nil {
		var err error
		// This may log a warning if the VM did not shutdown gracefully, and
		// start background generation.
		snaps, err = snapshot.New(*snapConf, db, cache.TrieDB(), lastExecuted)
		if err != nil {
			return nil, err
		}
	}
	return &Tracker{
		snaps:  snaps,
		cache:  cache,
		config: c,
		log:    log,
	}, nil
}

// Track tracks the root and may commit the trie associated with the root
// to the database if [Config.ShouldCommitTrieDB] returns true, or the [Config]
// specifies that the node is archival.
//
// This state will be available in memory until [Tracker.Untrack] has been
// called for the root as many times as [Tracker.Track] has been called.
func (t *Tracker) Track(root common.Hash) {
	// Never returns an error because it is a [triedb.HashDB].
	if err := t.cache.TrieDB().Reference(root, common.Hash{}); err != nil {
		t.log.Error("*triedb.Database.Reference()", zap.Error(err))
	}
}

// MaybeCommit potentially calls [triedb.Database.Commit], based on the
// following priorities:
//
// 1. If [Config.Archival] is true, then `executionRoot` will be committed.
// 2. If [ShouldCommitTrieDB] based on `height`, `settledRoot` is committed.
// 3. Otherwise, nothing is committed.
//
// This does NOT change in-memory tracking.
func (t *Tracker) MaybeCommit(settledRoot, executionRoot common.Hash, height uint64) error {
	var (
		commit  common.Hash
		because string
	)
	switch {
	case t.config.Archival:
		commit = executionRoot
		because = "post-execution archive"
	case ShouldCommitTrieDB(height, t.config.CommitInterval):
		commit = settledRoot
		because = "settled"
	default:
		return nil
	}

	tdb := t.cache.TrieDB()
	if err := tdb.Commit(commit, false /* log */); err != nil {
		return fmt.Errorf("%T.Commit(%#x) %s at end of block %d: %v", tdb, commit, because, height, err)
	}
	return nil
}

// Untrack informs the [Tracker] that the state corresponding
// with `root` can have its reference count reduced. If the reference
// count is 0, the state will be removed from memory.
//
// This should be called on each block after its state is no longer
// needed. If the state is already on disk, no operation is performed.
func (t *Tracker) Untrack(root common.Hash) {
	// Never returns an error because it is a [triedb.HashDB].
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
// and cancel any snapshot generation.
func (t *Tracker) Close(lastRoot common.Hash) error {
	var errs []error
	if t.snaps != nil {
		// We don't use [snapshot.Tree.Journal] because re-orgs are impossible under
		// SAE so we don't mind flattening all snapshot layers to disk. Note that
		// calling `Cap([disk root], 0)` returns an error when it's actually a
		// no-op, so we ensure there are changes.
		if lastRoot != t.snaps.DiskRoot() {
			if err := t.snaps.Cap(lastRoot, 0); err != nil {
				errs = append(errs, fmt.Errorf("%T.Cap(%s, 0): %v", t.snaps, lastRoot, err))
			}
		}

		// Cancel any background snapshot builds.
		// MUST be done before closing the TrieDB, otherwise the background
		// builds will race with the close.
		t.snaps.Release()
	}

	if err := t.cache.TrieDB().Close(); err != nil {
		errs = append(errs, fmt.Errorf("triedb.Database.Close(): %v", err))
	}

	return errors.Join(errs...)
}
