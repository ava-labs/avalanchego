// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/metrics"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/trie/triestate"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/database"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

const firewoodDir = "firewood" // MUST match graft/evm/firewood/triedb.go

var (
	_ triedb.DBConstructor = TrieDBConfig{}.BackendConstructor
	_ triedb.HashDB        = (*TrieDB)(nil)
)

// TrieDBConfig holds the configuration for creating a [TrieDB].
type TrieDBConfig struct {
	DatabaseDir       string
	CacheSizeBytes    uint
	RevisionsInMemory uint // must be >= 2
	Archive           bool
	// DeferredCommitInterval must be < RevisionsInMemory as otherwise, it's
	// possible to reap the latest persisted revision.
	DeferredCommitInterval uint64
	Log                    logging.Logger
	// TODO(alarso16): Something for metrics?
}

// DefaultConfig returns a sensible TrieDBConfig with the given directory.
// The default config is:
//   - CacheSizeBytes: 1MB
//   - RevisionsInMemory: 128
//   - CacheStrategy: [ffi.CacheAllReads]
//   - DeferredCommitInterval: 64
func DefaultConfig(dir string, log logging.Logger) TrieDBConfig {
	return TrieDBConfig{
		DatabaseDir:            dir,
		CacheSizeBytes:         1024 * 1024, // 1MB
		RevisionsInMemory:      128,
		DeferredCommitInterval: 64,
		Log:                    log,
	}
}

// BackendConstructor implements the [triedb.DBConstructor] interface.
// It creates a new Firewood database with the given configuration.
// Any error during creation will cause the program to exit.
func (c TrieDBConfig) BackendConstructor(ethdb.Database) triedb.DBOverride {
	db, err := New(c)
	if err != nil {
		if c.Log == nil {
			panic(fmt.Sprintf("creating firewood database: %v", err))
		}
		c.Log.Fatal("creating firewood database", zap.Error(err))
	}
	return db
}

// TrieDB is a triedb.DBOverride implementation backed by Firewood.
// It acts as HashDB for backwards compatibility with most our code.
//
// It MUST NOT be used for a synchronous EVM, because this implementation
// relies on any proposal being created eventually being committed, as opposed
// to the arbitrary DAG supported by `graft/evm/firewood`. Even in this
// narrower use case, the behavior of the two implementations are NOT the same.
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

	pending     *proposalRef
	committable map[common.Hash]*proposalRef

	log logging.Logger
}

// A proposalRef is an element in a doubly-linked list of proposals.
// Available in the [TrieDB.committable] map by state root, and linked together
// by parent-child relationships.
type proposalRef struct {
	p      *ffi.Proposal
	root   common.Hash
	parent *proposalRef
	child  *proposalRef
}

func New(config TrieDBConfig) (*TrieDB, error) {
	if err := validateDir(config.DatabaseDir); err != nil {
		return nil, err
	}
	path := filepath.Join(config.DatabaseDir, firewoodDir)

	// The Firewood constructor will check that commitCount is nonzero.
	minDeferredPersistenceCommitCount := uint64(config.RevisionsInMemory - 1)
	commitCount := min(config.DeferredCommitInterval, minDeferredPersistenceCommitCount)

	options := []ffi.Option{
		ffi.WithReadCacheStrategy(ffi.CacheAllReads), // Based on benchmarking, highest cache hit rate
		ffi.WithNodeCacheSizeInBytes(config.CacheSizeBytes),
		ffi.WithRevisions(config.RevisionsInMemory),
		ffi.WithDeferredPersistenceCommitCount(commitCount),
	}
	if config.Archive {
		options = append(options, ffi.WithRootStore())
	}
	if metrics.EnabledExpensive {
		// TODO(alarso16): Does Firewood use this?
		options = append(options, ffi.WithExpensiveMetrics())
	}

	fw, err := ffi.New(path, ffi.EthereumNodeHashing, options...)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	return &TrieDB{
		Firewood:    fw,
		committable: make(map[common.Hash]*proposalRef),
		log:         config.Log,
	}, nil
}

// validateDir ensures that the given directory exists and is a directory.
func validateDir(dir string) error {
	if dir == "" {
		return errors.New("chain data directory must be set")
	}

	switch info, err := os.Stat(dir); {
	case err != nil:
		return fmt.Errorf("os.Stat() on database directory: %w", err)
	case !info.IsDir():
		return fmt.Errorf("database directory path is not a directory: %q", dir)
	}

	return nil
}

// Close drops all proposals that have not yet been committed and closes the
// underlying Firewood database.  If a reference to a [state.Trie] obtained
// from this parent [state.Database] is still accessible during this call, an
// error will be returned.
//
// TODO(alarso16): Force drop all pending revisions on close once supported.
func (t *TrieDB) Close() error {
	var err error
	for _, proposal := range t.committable {
		err = errors.Join(err, proposal.p.Drop())
	}
	t.committable = nil
	if t.pending != nil {
		err = errors.Join(err, t.pending.p.Drop())
		t.pending = nil
	}

	go runtime.GC()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second) // allow some time for garbage collection.
	defer cancel()
	return errors.Join(err, t.Firewood.Close(ctx))
}

// Initialized indicates where any state has been committed, typically used to
// check if the genesis block has been committed.
func (t *TrieDB) Initialized(genesisRoot common.Hash) bool {
	if genesisRoot == types.EmptyRootHash {
		return true
	}
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

	if possible.root != root {
		return fmt.Errorf("proposal root %s does not match update root %s", possible.root, root)
	}

	if possible.parent != nil && possible.parent.root != parent {
		return fmt.Errorf("proposal parent root %s does not match update parent %s", possible.parent.root, parent)
	}

	t.pending = nil
	t.committable[root] = possible
	return nil
}

// Commit allows this proposal to be persisted to disk. The given root must have
// been previously provided to [TrieDB.Update]. any error returned from this
// function should be treated as fatal.
func (t *TrieDB) Commit(root common.Hash, report bool) error {
	p, ok := t.committable[root]
	if !ok {
		// If this state root is available only on disk, we must have already committed it.
		rev, err := t.Firewood.Revision(ffi.Hash(root))
		if err != nil {
			return fmt.Errorf("no committable proposal found for root %s", root)
		}
		return rev.Drop()
	}
	if child := p.child; child != nil {
		child.parent = nil // detach from doubly-linked list
	}

	// Walk back the proposal chain and collect all proposals to apply in order.
	var proposals []*ffi.Proposal
	for cur := p; cur != nil; cur = cur.parent {
		proposals = append(proposals, cur.p)
		cur.p = nil
		delete(t.committable, cur.root)
	}

	var err error
	for _, proposal := range slices.Backward(proposals) {
		if err != nil {
			// If a previous proposal failed to commit, we need to drop this proposal to avoid a leak.
			err = errors.Join(err, proposal.Drop())
			continue
		}
		err = proposal.Commit()
	}

	var log logging.Func
	if report {
		log = t.log.Info
	} else {
		log = t.log.Debug
	}
	log("committing proposal", zap.Stringer("root", root), zap.Int("numProposals", len(proposals)), zap.Error(err))

	return err
}

// trieHash creates a new proposal from either a committable proposal or the tip of the database.
// Returns (non-nil, true, nil) if a proposal was successfully created.
// Returns (nil, true, nil) when the ops produce no state change (nothing to track).
// Returns (nil, false, nil) when the parent root exists but is not proposable.
// Returns (nil, _, err) for any error.
func (t *TrieDB) trieHash(parentRoot common.Hash, batchOps []ffi.BatchOp) (*proposalRef, bool, error) {
	parent, foundProposal := t.committable[parentRoot]
	var propose func([]ffi.BatchOp) (*ffi.Proposal, error)
	switch {
	case foundProposal:
		propose = parent.p.Propose
	case parentRoot == common.Hash(t.Firewood.Root()):
		propose = t.Firewood.Propose
		parent = nil // nothing else would be required to be committed first
	default:
		// check if a reconstruction could be made
		if rev, err := t.Firewood.Revision(ffi.Hash(parentRoot)); err == nil {
			return nil, false, rev.Drop()
		}
		return nil, false, fmt.Errorf("parent root %+x not found in database", parentRoot)
	}

	proposal, err := propose(batchOps)
	if err != nil {
		return nil, false, fmt.Errorf("creating proposal: %w", err)
	}

	// If there are no state changes, we don't need to track this proposal.
	// [TrieDB.Update] will never be called.
	if proposal.Root() == ffi.Hash(parentRoot) {
		return nil, true, proposal.Drop()
	}

	return &proposalRef{
		p:      proposal,
		root:   common.Hash(proposal.Root()),
		parent: parent,
	}, true, nil
}

// trieCommit considers the provided proposal as canonical.
// Should be called on [state.Trie.Commit].
func (t *TrieDB) trieCommit(p *proposalRef) {
	if p == nil {
		return
	}

	// add to linked list, since proposal is final.
	if parent := p.parent; parent != nil {
		if parent.p != nil {
			parent.child = p
		} else {
			// parent proposal is already committed, detach from list
			p.parent = nil
		}
	}
	t.pending = p
}

// Scheme returns [rawdb.HashScheme] to identify the database.
//
// During a [state.StateDB.Commit] operation, providing [rawdb.HashScheme] from
// this function will prevent the statedb from trying to iterate over a self-
// destructed account's storage trie, since Firewood will prefix-delete the
// the storage trie and does not implement an iterator.
func (*TrieDB) Scheme() string {
	var _ state.StateDB // protect import
	return rawdb.HashScheme
}

var errReaderNotSupported = errors.New("TrieDB does not support creating a trie reader")

// Reader implements [triedb.Backend]. This is expected to be used by a [trie.Trie],
// so Firewood does not need to support this.
func (*TrieDB) Reader(common.Hash) (database.Reader, error) {
	var _ trie.Trie // protect import
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
