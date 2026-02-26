// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/trie/triestate"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/database"
	"go.uber.org/zap"

	// comment resolution
	_ "github.com/ava-labs/libevm/core/state"
	_ "github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	_ triedb.DBConstructor = Config{}.BackendConstructor
	_ triedb.HashDB        = (*TrieDB)(nil)
)

// Config holds the configuration for creating a [TrieDB].
type Config struct {
	Path              string
	Log               logging.Logger
	CacheSizeBytes    uint
	RevisionsInMemory uint // must be >= 2
	Archive           bool
	// DeferredCommitInterval must be < RevisionsInMemory as otherwise, it's
	// possible to reap the latest persisted revision.
	DeferredCommitInterval uint64
	// TODO(alarso16): Should metrics match the old implementation? Do we need libevm's registration?
}

// DefaultConfig returns a sensible Config with the given directory.
func DefaultConfig(path string, log logging.Logger) Config {
	return Config{
		Path:                   path,
		Log:                    log,
		CacheSizeBytes:         1024 * 1024,
		RevisionsInMemory:      128,
		DeferredCommitInterval: 64,
	}
}

// BackendConstructor can be supplied as a [triedb.DBConstructor].  It creates a
// new Firewood database with the given configuration.  If no logger is
// provided, it will panic. If any other error occurs, the error will be logged
// as [logging.Fatal] and will return nil.
func (c Config) BackendConstructor(ethdb.Database) triedb.DBOverride {
	db, err := New(c)
	if err != nil {
		if c.Log == nil {
			panic(fmt.Errorf("creating firewood database: %w", err))
		} else {
			c.Log.Fatal("creating firewood database", zap.Error(err))
		}
	}
	return db
}

var (
	errNoLogger             = errors.New("Log must be provided")  //nolint:staticcheck // false positive
	errPathNotProvided      = errors.New("Path must be provided") //nolint:staticcheck // false positive
	errTooFewRevisions      = errors.New("RevisionsInMemory must be >= 2")
	errCommitIntervalTooBig = errors.New("DeferredCommitInterval must be < RevisionsInMemory")
)

func (c Config) validate() error {
	switch {
	case c.Log == nil:
		return errNoLogger
	case c.Path == "":
		return errPathNotProvided
	case c.RevisionsInMemory < 2:
		return fmt.Errorf("%w: got %d", errTooFewRevisions, c.RevisionsInMemory)
	case c.DeferredCommitInterval >= uint64(c.RevisionsInMemory):
		return fmt.Errorf("%w: %d >= %d", errCommitIntervalTooBig, c.DeferredCommitInterval, c.RevisionsInMemory)
	default:
		return nil
	}
}

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

	pending     *ffi.Proposal
	committable *linked.Hashmap[common.Hash, *ffi.Proposal]

	log logging.Logger
}

func New(config Config) (*TrieDB, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}

	options := []ffi.Option{
		ffi.WithReadCacheStrategy(ffi.CacheAllReads), // Based on benchmarking, highest cache hit rate
		ffi.WithNodeCacheSizeInBytes(config.CacheSizeBytes),
		ffi.WithRevisions(config.RevisionsInMemory),
		ffi.WithDeferredPersistenceCommitCount(config.DeferredCommitInterval),
		ffi.WithExpensiveMetrics(),
	}
	if config.Archive {
		options = append(options, ffi.WithRootStore())
	}

	fw, err := ffi.New(config.Path, ffi.EthereumNodeHashing, options...)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if root := common.Hash(fw.Root()); root == types.EmptyRootHash {
		config.Log.Info("empty firewood database opened", zap.String("path", config.Path))
	} else {
		config.Log.Info("firewood database opened", zap.Stringer("root", root), zap.String("path", config.Path))
	}

	return &TrieDB{
		Firewood:    fw,
		committable: linked.NewHashmap[common.Hash, *ffi.Proposal](),
		log:         config.Log,
	}, nil
}

// Close drops all proposals that have not yet been committed and closes the
// underlying Firewood database.  If a reference to a [state.Trie] obtained
// from this parent [state.Database] is still accessible during this call, an
// error will be returned.
func (t *TrieDB) Close() error {
	errs := make([]error, 0, t.committable.Len()+2)
	for it := t.committable.NewIterator(); it.Next(); {
		errs = append(errs, it.Value().Drop())
	}
	t.committable.Clear()
	if t.pending != nil {
		errs = append(errs, t.pending.Drop())
		t.pending = nil
	}

	// Firewood will iterate through all open handles and close them, but this
	// isn't guaranteed to finish.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	errs = append(errs, t.Firewood.Close(ctx, ffi.WithForceCloseHandles()))

	return errors.Join(errs...)
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

// Update considers the given root as the new head of tracked roots.  This root,
// if different than parent, must have been created via [state.Trie.Commit] with
// a Firewood-backed [state.StateDB]. After Update returns, the user can call
// [TrieDB.Commit] to queue the proposal to be moved to disk.
//
//nolint:revive // removing names loses context.
func (t *TrieDB) Update(root common.Hash, parent common.Hash, block uint64, nodes *trienode.MergedNodeSet, states *triestate.Set, _ ...stateconf.TrieDBUpdateOption) error {
	possible := t.pending
	if possible == nil {
		// Update will never be called if no state change is proposed.
		return fmt.Errorf("no pending proposal to update for root %s", root)
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

// newProposal creates a new proposal from either a committable proposal or the tip of the database.
func (t *TrieDB) newProposal(parentRoot common.Hash, batchOps []ffi.BatchOp) (*ffi.Proposal, error) {
	switch parent, foundProposal := t.committable.Get(parentRoot); {
	case foundProposal:
		return parent.Propose(batchOps)
	case parentRoot == common.Hash(t.Firewood.Root()):
		return t.Firewood.Propose(batchOps)
	default:
		return nil, fmt.Errorf("parent root %+x is not proposable", parentRoot)
	}
}

// trieCommit considers the provided proposal as canonical.
// Should be called on [state.Trie.Commit].
//
// p MUST not be nil.
func (t *TrieDB) trieCommit(p *ffi.Proposal) {
	t.pending = p
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
