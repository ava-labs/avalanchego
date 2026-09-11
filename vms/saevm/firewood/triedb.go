// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/trie/triestate"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/database"
	"go.uber.org/zap"

	_ "github.com/ava-labs/libevm/core/state" // comment resolution

	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
)

// NewTrieDB opens, or creates, the Firewood database at path and returns it
// wrapped as a [triedb.Database], which MUST be closed by the caller. This is
// the only way to construct a [TrieDB].
func NewTrieDB(db ethdb.Database, config Config, path string, log logging.Logger) (*triedb.Database, error) {
	// A [triedb.DBConstructor] cannot return an error, so Firewood is opened
	// beforehand and merely returned by the constructor.
	fw, err := newTrieDB(config, path, log)
	if err != nil {
		return nil, err
	}
	return triedb.NewDatabase(db, &triedb.Config{
		DBOverride: func(ethdb.Database) triedb.DBOverride { return fw },
	}), nil
}

// Config holds the configuration for creating a [TrieDB].
type Config struct {
	CacheSizeMiB uint64
	// RevisionsInMemory controls the minimum number of revisions available at
	// all times.
	RevisionsInMemory uint64 // must be >= 2
	// RootStore adds support to forever read from any revision persisted to
	// disk. This is recommended for API providers.
	RootStore bool
	// MaxPersistGap must be < RevisionsInMemory as otherwise, it's
	// possible to reap the latest persisted revision.
	MaxPersistGap uint64
	// TODO(alarso16): Should metrics match the old implementation? Do we need libevm's registration?
}

// DefaultConfig returns a sensible [Config] with the given directory.
func DefaultConfig() Config {
	return Config{
		CacheSizeMiB:      1,
		RevisionsInMemory: 128,
		MaxPersistGap:     64,
	}
}

// maxCacheMiB is the largest [Config.CacheSizeMiB] whose size in bytes fits in
// the uint required by [ffi.WithNodeCacheSizeInBytes].
const maxCacheMiB = math.MaxUint / units.MiB

var (
	errTooFewRevisions  = errors.New("RevisionsInMemory must be >= 2")
	errPersistGapTooBig = errors.New("MaxPersistGap must be < RevisionsInMemory")
	errTooManyRevisions = fmt.Errorf("RevisionsInMemory must be <= %d", uint(math.MaxUint))
	errCacheTooLarge    = fmt.Errorf("CacheSizeMiB must be <= %d", maxCacheMiB)
)

// Verify checks the configuration invariants.
func (c Config) Verify() error {
	switch {
	case c.RevisionsInMemory < 2:
		return fmt.Errorf("%w: got %d", errTooFewRevisions, c.RevisionsInMemory)
	case c.RevisionsInMemory > math.MaxUint:
		// Can only happen on 32-bit platforms.
		return fmt.Errorf("%w: got %d", errTooManyRevisions, c.RevisionsInMemory)
	case c.MaxPersistGap >= c.RevisionsInMemory:
		return fmt.Errorf("%w: %d >= %d", errPersistGapTooBig, c.MaxPersistGap, c.RevisionsInMemory)
	case c.CacheSizeMiB > maxCacheMiB:
		return fmt.Errorf("%w: got %d", errCacheTooLarge, c.CacheSizeMiB)
	default:
		return nil
	}
}

var _ triedb.HashDB = (*TrieDB)(nil)

// TrieDB is a triedb.DBOverride implementation backed by Firewood.
// It acts as HashDB for backwards compatibility with most of our code.
//
// It MUST NOT be used for a synchronous EVM, because this implementation
// relies on any proposal being created eventually being committed, as opposed
// to the arbitrary DAG supported by `graft/evm/firewood`. Even in this
// narrower use case, the behavior of the two implementations is NOT the same.
//
// TrieDB implements [triedb.HashDB], despite being a path-based storage
// system, for easier compatibility with the rest of our codebase. Since
// Firewood internally tracks revision deletion and we have no need to journal,
// much of the complexity of [triedb.PathDB] is avoided.
type TrieDB struct {
	// The underlying Firewood database, used for storing proposals and revisions.
	// This is exported as read-only, with knowledge that the consumer will not close it
	// and the latest state can be modified at any time during execution.
	Firewood *ffi.Database

	mu          sync.RWMutex
	pending     *ffi.Proposal
	committable *linked.Hashmap[common.Hash, *ffi.Proposal]

	log logging.Logger
}

// newTrieDB opens, or creates, the Firewood database at path.
func newTrieDB(config Config, path string, log logging.Logger) (*TrieDB, error) {
	if err := config.Verify(); err != nil {
		return nil, err
	}

	options := []ffi.Option{
		ffi.WithReadCacheStrategy(ffi.CacheAllReads),                        // Based on benchmarking, highest cache hit rate
		ffi.WithNodeCacheSizeInBytes(uint(config.CacheSizeMiB) * units.MiB), // overflow checked in [Config.Verify]
		ffi.WithRevisions(uint(config.RevisionsInMemory)),                   // overflow checked in [Config.Verify]
		ffi.WithDeferredPersistenceCommitCount(config.MaxPersistGap),
		ffi.WithExpensiveMetrics(),
	}
	if config.RootStore {
		options = append(options, ffi.WithRootStore())
	}

	fw, err := ffi.New(path, ffi.EthereumNodeHashing, options...)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if root := common.Hash(fw.Root()); root == types.EmptyRootHash {
		log.Info("empty firewood database opened", zap.String("path", path))
	} else {
		log.Info("firewood database opened", zap.Stringer("root", root), zap.String("path", path))
	}

	return &TrieDB{
		Firewood:    fw,
		committable: linked.NewHashmap[common.Hash, *ffi.Proposal](),
		log:         log,
	}, nil
}

// Close drops all proposals that have not yet been committed and closes the
// underlying Firewood database.  Any references to a [state.Trie] obtained
// from this parent [state.Database] will no longer be valid after this call.
func (t *TrieDB) Close() error {
	// The force close below will iterate through all open handles and free
	// their Rust-side memory explicitly
	t.mu.Lock()
	defer t.mu.Unlock()
	t.committable.Clear()
	t.pending = nil

	// Firewood will iterate through all open handles and close them, but this
	// isn't guaranteed to finish quickly.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return t.Firewood.Close(ctx, ffi.WithForceCloseHandles())
}

// Initialized indicates whether any state has been committed, typically used to
// check if the genesis block has been committed.
func (t *TrieDB) Initialized(genesisRoot common.Hash) bool {
	// If the genesis root is empty, no state changes are necessary to Firewood,
	// and thus the genesis state can be considered committed.
	if genesisRoot == types.EmptyRootHash {
		return true
	}

	// If the disk root is not empty, then we must have committed something,
	// so we must have committed the genesis root, regardless of its value.
	return common.Hash(t.Firewood.Root()) != types.EmptyRootHash
}

var errParentNotLatest = errors.New("proposal parent is not the latest root")

// Update considers the given root as the new head of tracked roots.  This root,
// if different than parent, must have been created via [state.Trie.Commit] with
// a Firewood-backed [state.StateDB]. After Update returns, the user can call
// [TrieDB.Commit] to queue the proposal to be moved to disk.
//
//nolint:revive // removing names loses context.
func (t *TrieDB) Update(root common.Hash, parent common.Hash, block uint64, nodes *trienode.MergedNodeSet, states *triestate.Set, _ ...stateconf.TrieDBUpdateOption) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	possible := t.pending
	if possible == nil {
		// Update will never be called if no state change is proposed.
		return fmt.Errorf("no pending proposal to update for root %s", root)
	}

	// Proposals form a linear chain, so the parent MUST be the newest root
	// tracked here.
	latest, _, ok := t.committable.Newest()
	if !ok {
		latest = common.Hash(t.Firewood.Root())
	}
	if parent != latest {
		return fmt.Errorf("%w: parent %s, latest %s", errParentNotLatest, parent, latest)
	}

	if gotRoot := common.Hash(possible.Root()); gotRoot != root {
		return fmt.Errorf("proposal root %s does not match update root %s", gotRoot, root)
	}

	if _, ok := t.committable.Get(root); ok {
		return fmt.Errorf("root %s already queued for commit", root)
	}

	t.pending = nil
	t.committable.Put(root, possible) // appends to list
	return nil
}

// Commit ensures the given root is persisted to disk. If a proposal
// corresponding with this root is not found, Commit will return nil.
//
// Any error returned from this function should be treated as fatal.
func (t *TrieDB) Commit(root common.Hash, report bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.committable.Get(root); !ok {
		// Ideally, one would check that the root is on disk, since one should
		// never pass a non-existent root to Commit. However, Firewood loses
		// all commit history on startup.
		return nil
	}

	// Iterator iterates from oldest to newest
	var committed []common.Hash
	for it := t.committable.NewIterator(); it.Next(); {
		proposalRoot := it.Key()
		if err := it.Value().Commit(); err != nil {
			return fmt.Errorf("committing proposal with root %s: %w", proposalRoot, err)
		}
		committed = append(committed, proposalRoot)
		if proposalRoot == root {
			// Guaranteed to be hit because of the above check
			break
		}
	}

	for _, root := range committed {
		t.committable.Delete(root)
	}

	log := t.log.Debug
	if report {
		log = t.log.Info
	}
	log("committing proposal",
		zap.Stringer("root", root),
		zap.Stringers("committed", committed),
		zap.Int("remaining_proposals", t.committable.Len()),
	)

	return nil
}

// newRevision returns the [ffi.Revision] at root, or a [trie.MissingNodeError]
// if Firewood has no revision for root.
func (t *TrieDB) newRevision(root common.Hash) (*ffi.Revision, error) {
	revision, err := t.Firewood.Revision(ffi.Hash(root))
	if errors.Is(err, ffi.ErrRevisionNotFound) {
		return nil, &trie.MissingNodeError{NodeHash: root}
	}
	return revision, err
}

var errNotProposable = errors.New("parent root is not proposable")

// newProposal creates a new proposal from either a committable proposal or the
// tip of the database. Returns an error wrapping [errNotProposable] otherwise.
func (t *TrieDB) newProposal(parentRoot common.Hash, batchOps []ffi.BatchOp) (*ffi.Proposal, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	switch parent, foundProposal := t.committable.Get(parentRoot); {
	case foundProposal:
		return parent.Propose(batchOps)
	case parentRoot == common.Hash(t.Firewood.Root()):
		return t.Firewood.Propose(batchOps)
	default:
		return nil, fmt.Errorf("%w: %+x", errNotProposable, parentRoot)
	}
}

func (t *TrieDB) newReconstructed(root common.Hash) (*ffi.Reconstructed, error) {
	rev, err := t.newRevision(root)
	if err != nil {
		return nil, err
	}
	return rev.Reconstruct(nil)
}

var errProposalPending = errors.New("a proposal is already pending")

// trieCommit considers the provided proposal as canonical, to be consumed by
// the next call to [TrieDB.Update]. Should be called on [state.Trie.Commit].
//
// Returns an error wrapping [errProposalPending] if a previous proposal would
// be lost. p MUST not be nil.
func (t *TrieDB) trieCommit(p *ffi.Proposal) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending != nil {
		return fmt.Errorf("%w: root %s", errProposalPending, common.Hash(t.pending.Root()))
	}
	t.pending = p
	return nil
}

// Scheme returns [rawdb.HashScheme] to identify the database.
//
// During a [state.StateDB.Commit] operation, providing [rawdb.HashScheme] from
// this function will prevent the statedb from trying to iterate over a self-
// destructed account's storage trie, since Firewood will prefix-delete the
// storage trie and does not implement an iterator.
func (*TrieDB) Scheme() string {
	return rawdb.HashScheme
}

var errReaderNotSupported = errors.New("TrieDB does not support creating a trie reader")

// Reader satisfies [triedb.Backend]. This is expected to be used by a [trie.Trie],
// so Firewood does not need to support this.
func (*TrieDB) Reader(common.Hash) (database.Reader, error) {
	return nil, errReaderNotSupported
}

// Size is managed by Firewood, so this returns 0 for both values to avoid confusion.
func (*TrieDB) Size() (common.StorageSize, common.StorageSize) {
	return 0, 0
}

// Cap is not supported.
func (*TrieDB) Cap(common.StorageSize) error {
	return nil
}

// Reference is a no-op since Firewood doesn't require explicit reference counting.
func (*TrieDB) Reference(common.Hash, common.Hash) {}

// Dereference is a no-op since Firewood doesn't require explicit reference counting.
func (*TrieDB) Dereference(common.Hash) {}
