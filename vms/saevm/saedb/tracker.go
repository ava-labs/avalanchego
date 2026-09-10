// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saedb

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/hashdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/saevm/firewood"

	graftfw "github.com/ava-labs/avalanchego/graft/evm/firewood"
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

	// maxCacheMiB is the largest MiB value that can be converted to bytes
	// without overflowing an int.
	maxCacheMiB = math.MaxInt / units.MiB
)

// Used in [HashDBConfig.targetCap] to determine memory pressure.
const (
	defaultMaxCap           = 512 * units.MiB
	defaultTargetCommitSize = 20 * units.MiB

	// defaultTargetCommitSize >= ethdb.IdealBatchSize
	_ uint = defaultTargetCommitSize - ethdb.IdealBatchSize
)

// Config configures a [Tracker]: the trie database scheme, its cache sizes,
// and when state is committed. It is implemented only by [HashDBConfig] and
// [FirewoodConfig].
type Config interface {
	// Verify checks the user's input of the config.
	Verify() error
	// Open opens the [triedb.Database]. All arguments MUST be provided, and the
	// returned database MUST be closed by the caller.
	Open(db ethdb.Database, dataDir string, log logging.Logger) (*triedb.Database, error)

	openSnapshot(db ethdb.Database, tdb *triedb.Database, root common.Hash) (*snapshot.Tree, error)
	targetCap(height uint64) common.StorageSize
	shouldCommit(height uint64) commitDecision
}

// A commitDecision is the outcome of [Config.shouldCommit].
type commitDecision int

const (
	noCommit       commitDecision = iota // nothing is committed; memory pressure MAY be relieved
	commitSettled                        // the settled root is committed
	commitExecuted                       // the post-execution root is committed (archival)
)

var _ StateDBOpener = (*Tracker)(nil)

// Tracker provides an abstraction to state-related operations, managing all
// database operations not exposed by the [state.StateDB] itself.
//
// All methods are safe to be called even after [Tracker.Close], but state
// will be unavailable.
type Tracker struct {
	snaps *snapshot.Tree
	cache state.Database

	// recent is a ring of the post-execution roots of the most recently
	// executed blocks, each holding a reference that keeps its trie in memory
	// for the snapshot generator. Unused when the snapshot is disabled.
	recent     [core.TriesInMemory]common.Hash
	recentNext int

	config Config
	log    logging.Logger
}

// NewTracker provides a new [Tracker] on the underlying database.
func NewTracker(db ethdb.Database, c Config, lastExecuted common.Hash, dataDir string, log logging.Logger) (*Tracker, error) {
	if err := c.Verify(); err != nil {
		return nil, err
	}
	tdb, err := c.Open(db, dataDir, log)
	if err != nil {
		return nil, err
	}
	snaps, err := c.openSnapshot(db, tdb, lastExecuted)
	if err != nil {
		return nil, errors.Join(err, tdb.Close())
	}
	return &Tracker{
		snaps:  snaps,
		cache:  state.NewDatabaseWithNodeDB(db, tdb),
		config: c,
		log:    log,
	}, nil
}

// TrieDB returns the trie database used by [Tracker.StateDB].
func (t *Tracker) TrieDB() *triedb.Database {
	return t.cache.TrieDB()
}

// Snapshot returns any snapshot that is used by a [state.StateDB] returned
// by [Tracker.StateDB]. This MAY be nil.
func (t *Tracker) Snapshot() *snapshot.Tree {
	return t.snaps
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

// BlockExecuted informs the Tracker that the block at height executed to
// executionRoot, settling the state at settledRoot. It commits whichever root
// the [Config] selects or, if none, flushes the oldest trie nodes to disk
// under sufficient memory pressure.
//
// While the snapshot is enabled, the Tracker also holds its own reference to
// the executed state, as if by [Tracker.Track], and releases it once
// [core.TriesInMemory] later blocks have executed.
func (t *Tracker) BlockExecuted(settledRoot, executionRoot common.Hash, height uint64) error {
	t.retain(executionRoot)

	var (
		root    common.Hash
		because string
	)
	switch d := t.config.shouldCommit(height); d {
	case commitSettled:
		root, because = settledRoot, "settled"
	case commitExecuted:
		root, because = executionRoot, "post-execution"
	case noCommit:
		return t.maybeCap(height)
	default:
		return fmt.Errorf("unknown %T %d at block %d", d, d, height)
	}
	tdb := t.cache.TrieDB()
	if err := tdb.Commit(root, false /* log */); err != nil {
		return fmt.Errorf("%T.Commit(%#x) %s at end of block %d: %v", tdb, root, because, height, err)
	}
	return nil
}

// maybeCap checks if the in-memory state exceeds [Config.targetCap] and, if
// so, moves the oldest state to disk.
func (t *Tracker) maybeCap(height uint64) error {
	targetCap := t.config.targetCap(height)

	tdb := t.cache.TrieDB()
	_, inMemory, _ := tdb.Size()
	if inMemory <= targetCap {
		return nil
	}

	if err := tdb.Cap(targetCap - ethdb.IdealBatchSize); err != nil { // avoid small DB writes
		return fmt.Errorf("%T.Cap() at block %d: %v", tdb, height, err)
	}
	return nil
}

// retain holds a reference to `root` until [core.TriesInMemory] later roots
// have been retained. This is necessary because snapshot generation resumes at
// the root of its disk layer, which is never more than [core.TriesInMemory]
// blocks behind. If [Tracker.snaps] is nil then retain is a no-op.
func (t *Tracker) retain(root common.Hash) {
	if t.snaps == nil {
		return
	}
	t.Track(root)
	if toEvict := t.recent[t.recentNext]; toEvict != (common.Hash{}) {
		t.Untrack(toEvict)
	}
	t.recent[t.recentNext] = root
	t.recentNext++
	t.recentNext %= len(t.recent)
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

// Close commits the state at root to disk, flattens any snapshot onto it, and
// releases all resources associated with the [triedb.Database]. root SHOULD be
// the state that a subsequent [NewTracker] opens at, otherwise the snapshot is
// regenerated instead of loaded.
//
// TODO(StephenButtolph): Close fails if the snapshot layer at root was already
// flattened away, which happens when settlement lags execution by more than the
// number of retained diff layers. In this case, shutdown will report the error
// and the next start will regenerate the snapshot.
func (t *Tracker) Close(root common.Hash) error {
	var errs []error

	tdb := t.cache.TrieDB()
	if err := tdb.Commit(root, false /* log */); err != nil {
		errs = append(errs, fmt.Errorf("%T.Commit(%#x): %v", tdb, root, err))
	}

	if t.snaps != nil {
		// We don't use [snapshot.Tree.Journal] because re-orgs are impossible under
		// SAE so we don't mind flattening all snapshot layers to disk. Note that
		// calling `Cap([disk root], 0)` returns an error when it's actually a
		// no-op, so we ensure there are changes.
		if root != t.snaps.DiskRoot() {
			if err := t.snaps.Cap(root, 0); err != nil {
				errs = append(errs, fmt.Errorf("%T.Cap(%#x, 0): %v", t.snaps, root, err))
			}
		}

		// Cancel any background snapshot builds.
		// MUST be done before closing the TrieDB, otherwise the background
		// builds will race with the close.
		t.snaps.Release()
	}

	if err := tdb.Close(); err != nil {
		errs = append(errs, fmt.Errorf("%T.Close(): %v", tdb, err))
	}

	return errors.Join(errs...)
}

var _ Config = HashDBConfig{}

// A HashDBConfig configures a [hashdb.Database]-backed [Tracker].
type HashDBConfig struct {
	TrieCacheMiB     uint64 // size of the TrieDB clean cache
	SnapshotCacheMiB uint64 // size of the snapshot cache - if 0, snapshots are disabled
	// CommitInterval is the number of blocks between commits of the settled
	// state. It MUST be non-zero, but is ignored if Archival is true.
	CommitInterval uint64
	// Archival commits the post-execution state of every block for
	// RPC support (archival).
	Archival bool
	// AllowMissingTries allows switching from archival to pruning on a
	// database that previously ran archival.
	AllowMissingTries bool

	// only configurable for tests; see [HashDBConfig.targetCap]
	maxCapBytes       common.StorageSize
	targetCommitBytes common.StorageSize
}

var (
	errZeroCommitInterval = errors.New("commit interval must be non-zero")
	errCacheTooLarge      = fmt.Errorf("cache size exceeds maximum of %d MiB", maxCacheMiB)
)

func (h HashDBConfig) Verify() error {
	if h.CommitInterval == 0 {
		return errZeroCommitInterval
	}
	if h.TrieCacheMiB > maxCacheMiB {
		return fmt.Errorf("%w: TrieCacheMiB (%d)", errCacheTooLarge, h.TrieCacheMiB)
	}
	if h.SnapshotCacheMiB > maxCacheMiB {
		return fmt.Errorf("%w: SnapshotCacheMiB (%d)", errCacheTooLarge, h.SnapshotCacheMiB)
	}
	return nil
}

// Open guards the trie index with [protectTrieIndex] and then opens the trie
// database.
func (h HashDBConfig) Open(db ethdb.Database, _ string, _ logging.Logger) (*triedb.Database, error) {
	if err := protectTrieIndex(db, h); err != nil {
		return nil, fmt.Errorf("preventing missing tries: %w", err)
	}
	return triedb.NewDatabase(db, &triedb.Config{
		HashDB: &hashdb.Config{
			CleanCacheSize: int(h.TrieCacheMiB) * units.MiB, // #nosec G115 -- checked in [HashDBConfig.Verify]
		},
	}), nil
}

var errRefuseToCorruptArchiver = errors.New(`node is switching from non-pruning to pruning; if this is intentional, set "allow-missing-tries" via config, otherwise disable pruning`)

// protectTrieIndex prevents a pruning run from deleting tries stored by a
// previous archival run. Archival runs persistently mark the database, and
// the marker is never removed. Pruning runs against a marked database return
// [errRefuseToCorruptArchiver] unless [HashDBConfig.AllowMissingTries] is set,
// which bypasses the check without removing the marker.
func protectTrieIndex(db ethdb.KeyValueStore, h HashDBConfig) error {
	if h.Archival {
		return customrawdb.WritePruningDisabled(db)
	}
	prevArchival, err := customrawdb.HasPruningDisabled(db)
	if err != nil {
		return err
	}
	if prevArchival && !h.AllowMissingTries {
		return errRefuseToCorruptArchiver
	}
	return nil
}

// openSnapshot opens the snapshot at root, or returns nil if
// [HashDBConfig.SnapshotCacheMiB] is zero. If the node did not shut down
// cleanly, generation resumes in the background.
func (h HashDBConfig) openSnapshot(db ethdb.Database, tdb *triedb.Database, root common.Hash) (*snapshot.Tree, error) {
	if h.SnapshotCacheMiB == 0 {
		return nil, nil
	}
	return snapshot.New(
		snapshot.Config{
			CacheSize:  int(h.SnapshotCacheMiB), //#nosec G115 -- checked in [HashDBConfig.Verify]
			AsyncBuild: true,
		},
		db, tdb, root,
	)
}

func (h HashDBConfig) maxCap() common.StorageSize {
	if h.maxCapBytes > 0 {
		return h.maxCapBytes
	}
	return defaultMaxCap
}

func (h HashDBConfig) targetCommitSize() common.StorageSize {
	if h.targetCommitBytes > 0 {
		return h.targetCommitBytes
	}
	return defaultTargetCommitSize
}

// targetCap shrinks linearly from [HashDBConfig.maxCap] down to
// [HashDBConfig.targetCommitSize] as height approaches the next commit, so the
// [triedb.Database.Commit] at the [HashDBConfig.CommitInterval] boundary is
// small.
func (h HashDBConfig) targetCap(height uint64) common.StorageSize {
	var (
		maxCap           = h.maxCap()
		targetCommitSize = h.targetCommitSize()
		commitInterval   = h.CommitInterval
	)
	distanceFromCommit := commitInterval - height%commitInterval
	slope := (maxCap - targetCommitSize) / common.StorageSize(commitInterval)
	return common.StorageSize(distanceFromCommit)*slope + targetCommitSize
}

// shouldCommit decides, in order of priority:
//
// 1. If [HashDBConfig.Archival] is true, [commitExecuted].
// 2. If [ShouldCommitTrieDB] based on `height`, [commitSettled].
// 3. Otherwise, [noCommit].
func (h HashDBConfig) shouldCommit(height uint64) commitDecision {
	switch {
	case h.Archival:
		return commitExecuted
	case ShouldCommitTrieDB(height, h.CommitInterval):
		return commitSettled
	default:
		return noCommit
	}
}

var _ Config = FirewoodConfig{}

// FirewoodConfig configures a [firewood.TrieDB]-backed [Tracker]. The settled
// state is committed after every block. Setting [firewood.Config.RootStore]
// retains every committed root for RPC support (archival).
//
// The cache size defaults to [saedb.DefaultTrieCacheSizeMiB]. The number of
// revisions kept in memory is at least [firewood.Config.MaxPersistGap]+1.
type FirewoodConfig struct {
	firewood.Config
}

func (f FirewoodConfig) withDefaults() firewood.Config {
	cfg := f.Config
	cfg.RevisionsInMemory = max(cfg.MaxPersistGap+1, cfg.RevisionsInMemory)
	if cfg.CacheSizeMiB == 0 {
		cfg.CacheSizeMiB = DefaultTrieCacheSizeMiB
	}
	return cfg
}

// Verify checks the config after applying defaults.
func (f FirewoodConfig) Verify() error {
	return f.withDefaults().Verify()
}

// Open opens the Firewood database under dataDir.
func (f FirewoodConfig) Open(db ethdb.Database, dataDir string, log logging.Logger) (*triedb.Database, error) {
	return firewood.NewTrieDB(db, f.withDefaults(), filepath.Join(dataDir, graftfw.Directory), log)
}

// openSnapshot always returns nil because Firewood already has efficient lookups.
func (FirewoodConfig) openSnapshot(ethdb.Database, *triedb.Database, common.Hash) (*snapshot.Tree, error) {
	return nil, nil
}

// targetCap is never consulted because every block commits, so it imposes no
// limit.
func (FirewoodConfig) targetCap(uint64) common.StorageSize { return math.MaxInt64 }

// shouldCommit always returns [commitSettled]. Firewood prunes all but the
// last state on disk after shutdown, so persisting the execution root would
// make VM recovery (re-execution since last settled) impossible, as Firewood
// can only build off the most recent state.
func (FirewoodConfig) shouldCommit(uint64) commitDecision { return commitSettled }
