// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/metrics"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/trie/triestate"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/database"
)

const firewoodDir = "firewood"

var (
	_ triedb.DBConstructor = TrieDBConfig{}.BackendConstructor
	_ triedb.DBOverride    = (*TrieDB)(nil)

	hashCount              = metrics.GetOrRegisterCounter("firewood/triedb/hash/count", nil)
	hashTimer              = metrics.GetOrRegisterCounter("firewood/triedb/hash/time", nil)
	commitCount            = metrics.GetOrRegisterCounter("firewood/triedb/commit/count", nil)
	commitTimer            = metrics.GetOrRegisterCounter("firewood/triedb/commit/time", nil)
	proposeOnDiskCount     = metrics.GetOrRegisterCounter("firewood/triedb/propose/disk/count", nil)
	proposeOnProposeCount  = metrics.GetOrRegisterCounter("firewood/triedb/propose/proposal/count", nil)
	explicitlyDroppedCount = metrics.GetOrRegisterCounter("firewood/triedb/drop/count", nil)

	errNoProposalFound         = errors.New("no proposal found")
	errUnexpectedProposalFound = errors.New("unexpected proposal found")
)

// TrieDB is a triedb.DBOverride implementation backed by Firewood.
// It acts as a HashDB for backwards compatibility with most of the blockchain code.
type TrieDB struct {
	proposals

	// The underlying Firewood database, used for storing proposals and revisions.
	// This is exported as read-only, with knowledge that the consumer will not close it
	// and the latest state can be modified at any time.
	Firewood *ffi.Database
}

type proposals struct {
	sync.Mutex

	byStateRoot map[common.Hash][]*proposal
	// The proposal tree tracks the structure of the current proposals, and which proposals are children of which.
	// This is used to ensure that we can dereference proposals correctly and commit the correct ones
	// in the case of duplicate state roots.
	// The root of the tree is stored here, and represents the top-most layer on disk.
	tree *proposal

	// possible temporarily holds proposals created during a trie update.
	// This is cleared after the update is complete and the proposals have been sent to the database.
	// It's unexpected for multiple updates to this to occur simultaneously, but a lock is used to ensure safety.
	possible map[possibleKey]*proposal
}

type possibleKey struct {
	parentBlockHash, root common.Hash //nolint:unused // It is used as a map key
}

// A proposal carries a Firewood FFI proposal (i.e. Rust-owned memory).
// The Firewood library adds a finalizer to the proposal handle to ensure that
// the memory is freed when the Go object is garbage collected. However, because
// we form a tree of proposals, the `proposal.Proposal` field may be the only
// reference to a given proposal. To ensure that all proposals in the tree
// can be freed in a finalizer, this cannot be included in the tree structure.
type proposal struct {
	*proposalMeta
	handle *ffi.Proposal
}

type proposalMeta struct {
	parent      *proposalMeta
	children    []*proposalMeta
	blockHashes map[common.Hash]struct{} // All corresponding block hashes
	root        common.Hash
	height      uint64
}

type TrieDBConfig struct {
	DatabaseDir          string
	CacheSizeBytes       uint
	FreeListCacheEntries uint
	RevisionsInMemory    uint // must be >= 2
	CacheStrategy        ffi.CacheStrategy
	Archive              bool
}

// DefaultConfig returns a sensible TrieDBConfig with the given directory.
// The default config is:
//   - CacheSizeBytes: 10MB
//   - FreeListCacheEntries: 200,000
//   - RevisionsInMemory: 80,000
//   - CacheStrategy: [ffi.CacheAllReads]
//   - Archive: false
func DefaultConfig(dir string) TrieDBConfig {
	return TrieDBConfig{
		DatabaseDir:          dir,
		CacheSizeBytes:       1024 * 1024, // 1MB
		FreeListCacheEntries: 40_000,
		RevisionsInMemory:    80_000,
		CacheStrategy:        ffi.CacheAllReads,
		Archive:              false,
	}
}

// BackendConstructor implements the [triedb.DBConstructor] interface.
// It creates a new Firewood database with the given configuration.
// Any error during creation will cause the program to exit.
func (c TrieDBConfig) BackendConstructor(ethdb.Database) triedb.DBOverride {
	db, err := New(c)
	if err != nil {
		log.Crit("creating firewood database", "error", err)
	}
	return db
}

// New creates a new Firewood database with the given configuration.
// The database will not be opened on error.
func New(config TrieDBConfig) (*TrieDB, error) {
	if err := validateDir(config.DatabaseDir); err != nil {
		return nil, err
	}
	path := filepath.Join(config.DatabaseDir, firewoodDir)
	options := []ffi.Option{
		ffi.WithNodeCacheEntries(config.CacheSizeBytes / 256), // TODO(#4750): is 256 bytes per node a good estimate?
		ffi.WithFreeListCacheEntries(config.FreeListCacheEntries),
		ffi.WithRevisions(config.RevisionsInMemory),
		ffi.WithReadCacheStrategy(config.CacheStrategy),
	}
	if config.Archive {
		options = append(options, ffi.WithRootStore())
	}
	if metrics.EnabledExpensive {
		options = append(options, ffi.WithExpensiveMetrics())
	}

	fw, err := ffi.New(path, ffi.EthereumNodeHashing, options...)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	initialRoot, err := fw.Root()
	if err != nil {
		if closeErr := fw.Close(context.Background()); closeErr != nil {
			return nil, fmt.Errorf("%w: error while closing: %w", err, closeErr)
		}
		return nil, err
	}

	blockHashes := make(map[common.Hash]struct{})
	blockHashes[common.Hash{}] = struct{}{}
	return &TrieDB{
		Firewood: fw,
		proposals: proposals{
			byStateRoot: make(map[common.Hash][]*proposal),
			tree: &proposal{
				proposalMeta: &proposalMeta{
					root:        common.Hash(initialRoot),
					blockHashes: blockHashes,
					height:      0,
				},
			},
			possible: make(map[possibleKey]*proposal),
		},
	}, nil
}

// validateDir ensures that the given directory exists and is a directory.
func validateDir(dir string) error {
	if dir == "" {
		return errors.New("chain data directory must be set")
	}

	switch info, err := os.Stat(dir); {
	case os.IsNotExist(err):
		log.Info("Database directory not found, creating", "path", dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating database directory: %w", err)
		}
	case err != nil:
		return fmt.Errorf("os.Stat() on database directory: %w", err)
	case !info.IsDir():
		return fmt.Errorf("database directory path is not a directory: %q", dir)
	}

	return nil
}

// SetHashAndHeight sets the committed block hashes and height in memory.
// This must be called at startup to initialize the in-memory state if the
// database is non-empty (e.g. restart, state sync)
func (t *TrieDB) SetHashAndHeight(blockHash common.Hash, height uint64) {
	t.Lock()
	defer t.Unlock()
	clear(t.tree.blockHashes)
	t.tree.blockHashes[blockHash] = struct{}{}
	t.tree.height = height
}

// Scheme returns the scheme of the database.
// However, to avoid a slow deletion in `libevm` `StateDB`, it returns [rawdb.HashScheme].
func (*TrieDB) Scheme() string {
	return rawdb.HashScheme
}

// Initialized checks whether a non-empty genesis block has been written.
func (t *TrieDB) Initialized(common.Hash) bool {
	root, err := t.Firewood.Root()
	if err != nil {
		log.Error("get current root", "error", err)
		return false
	}

	return common.Hash(root) != types.EmptyRootHash
}

// Size is a no-op because Firewood does not track storage size.
// All memory management is handled internally by Firewood.
func (*TrieDB) Size() (common.StorageSize, common.StorageSize) {
	return 0, 0
}

// Reference is no-op because proposals are only referenced when created.
// Additionally, internal nodes do not need tracked by consumers.
func (*TrieDB) Reference(common.Hash, common.Hash) {}

// Dereference is no-op because proposals will be removed automatically.
// Additionally, internal nodes do not need tracked by consumers.
func (*TrieDB) Dereference(common.Hash) {}

// Cap is a no-op because it isn't supported by Firewood.
func (*TrieDB) Cap(common.StorageSize) error {
	return nil
}

// Close closes the database, freeing all associated resources.
// This may hang for a short period while waiting for finalizers to complete.
// If it does not close as expected, this indicates that there are still references
// to proposals or revisions in memory, and an error will be returned.
// The database should not be used after calling Close, but it is safe to call multiple times.
func (t *TrieDB) Close() error {
	p := &t.proposals
	p.Lock()
	defer p.Unlock()

	if p.tree == nil {
		return nil // already closed
	}

	// All remaining proposals can explicitly be dropped.
	for _, child := range p.tree.children {
		p.removeProposalAndChildren(child)
	}
	p.tree = nil
	p.byStateRoot = nil
	t.possible = nil

	// We must provide a context to close since it may hang while waiting for the finalizers to complete.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return t.Firewood.Close(ctx)
}

// Update updates the database to the given root at the given height.
// The parent block hash and block hash must be provided in the options.
// A proposal must have already been created from [accountTrie.Commit] with the same root,
// parent root, and height.
// If no such proposal exists, an error will be returned.
//
// Unlike for HashDB and PathDB, `Commit` must be called even if if the root is unchanged.
func (t *TrieDB) Update(root, parent common.Hash, height uint64, _ *trienode.MergedNodeSet, _ *triestate.Set, opts ...stateconf.TrieDBUpdateOption) error {
	// We require block hashes to be provided for all blocks in production.
	// However, many tests cannot reasonably provide a block blockHash for genesis, so we allow it to be omitted.
	parentBlockHash, blockHash, ok := stateconf.ExtractTrieDBUpdatePayload(opts...)
	if !ok {
		return fmt.Errorf("no block hash provided for block %d", height)
	}

	// The rest of the operations except key-value arranging must occur with a lock
	t.proposals.Lock()
	defer t.proposals.Unlock()

	p, ok := t.possible[possibleKey{parentBlockHash: parentBlockHash, root: root}]
	// It's possible that we are committing a proposal on top of an empty genesis block in testing.
	// In this case, we can still find the proposal by looking for the empty block hash
	if !ok && height == 1 && parent == types.EmptyRootHash {
		p, ok = t.possible[possibleKey{parentBlockHash: common.Hash{}, root: root}]
		p.height = 1
		p.parent.blockHashes[parentBlockHash] = struct{}{}
	}
	if !ok {
		return fmt.Errorf("%w for block %d, root %s, hash %s", errNoProposalFound, height, root.Hex(), blockHash.Hex())
	}

	// If we have already created an identical proposal, we can skip adding it again.
	if t.proposals.exists(root, blockHash, parentBlockHash) {
		// All unused proposals can be cleared, since we are already tracking an identical one.
		clear(t.possible)
		return nil
	}
	switch {
	case p.root != root:
		return fmt.Errorf("%w: expected root %#x, got %#x", errUnexpectedProposalFound, root, p.root)
	case p.parent.root != parent:
		return fmt.Errorf("%w: expected parent root %#x, got %#x", errUnexpectedProposalFound, parent, p.parent.root)
	case p.height != height:
		return fmt.Errorf("%w: expected height %d, got %d", errUnexpectedProposalFound, height, p.height)
	}

	// Track the proposal context in the tree and map.
	p.parent.children = append(p.parent.children, p.proposalMeta)
	t.proposals.byStateRoot[root] = append(t.proposals.byStateRoot[root], p)
	p.blockHashes[blockHash] = struct{}{}
	// Now, all unused proposals have no other references, since we didn't store them
	// in the proposal map or tree, so they will be garbage collected.
	// Any proposals with a different root were mistakenly created, so they can be freed as well.
	clear(t.possible)
	return nil
}

// Check if this proposal already exists.
// During reorgs, we may have already tracked this block hash.
// Additionally, we may have coincidentally created an identical proposal with a different block hash.
func (ps *proposals) exists(root, block, parentBlock common.Hash) bool {
	proposals, ok := ps.byStateRoot[root]
	if !ok {
		return false
	}

	for _, p := range proposals {
		// If the block hash is already tracked, we can skip proposing this again.
		if _, ok := p.blockHashes[block]; ok {
			log.Debug("proposal already exists", "root", root.Hex(), "parentBlock", parentBlock.Hex(), "block", block.Hex())
			return true
		}

		// We have an identical proposal, but should ensure the hash is tracked with this proposal.
		if _, ok := p.parent.blockHashes[parentBlock]; ok {
			log.Debug("proposal already exists, updating hash", "root", root.Hex(), "parentBlock", parentBlock.Hex(), "block", block.Hex())
			p.blockHashes[block] = struct{}{}
			return true
		}
	}

	return false
}

// Commit persists a proposal as a revision to the database.
//
// Any time this is called, we expect either:
//  1. The root is the same as the current root of the database (empty block during bootstrapping)
//  2. We have created a valid propsal with that root, and it is of height +1 above the proposal tree root.
//     Additionally, this will be unique.
//
// Afterward, we know that no other proposal at this height can be committed, so we can dereference all
// children in the the other branches of the proposal tree.
//
// Unlike for HashDB and PathDB, `Commit` must be called even if if the root is unchanged.
func (t *TrieDB) Commit(root common.Hash, report bool) error {
	start := time.Now()
	defer func() {
		commitTimer.Inc(time.Since(start).Milliseconds())
		commitCount.Inc(1)
	}()

	t.proposals.Lock()
	defer t.proposals.Unlock()

	p, err := t.proposals.findProposalToCommitWhenLocked(root)
	if err != nil {
		return err
	}

	if err := p.handle.Commit(); err != nil {
		return fmt.Errorf("committing proposal %s: %w", root.Hex(), err)
	}
	p.handle = nil // The proposal has been committed.

	newRoot, err := t.Firewood.Root()
	if err != nil {
		return fmt.Errorf("getting current root after commit: %w", err)
	}
	if common.Hash(newRoot) != root {
		return fmt.Errorf("root after commit (%x) does not match expected root %x", newRoot, root)
	}

	logFn := log.Debug
	if report {
		logFn = log.Info
	}
	logFn("Persisted proposal to firewood database", "root", root)

	// On success, we should remove all children of the committed proposal.
	// They will never be committed.
	t.cleanupCommittedProposal(p)
	return nil
}

func (ps *proposals) findProposalToCommitWhenLocked(root common.Hash) (*proposal, error) {
	var candidate *proposal

	for _, p := range ps.byStateRoot[root] {
		if p.parent.root != ps.tree.root || p.parent.height != ps.tree.height {
			continue
		}
		if candidate != nil {
			// This should never happen, as we ensure that we don't create duplicate proposals in `propose`.
			return nil, fmt.Errorf("multiple proposals found for root %#x", root)
		}
		candidate = p
	}

	if candidate == nil {
		return nil, fmt.Errorf("committable proposal not found for %d:%#x", ps.tree.height+1, root)
	}
	return candidate, nil
}

// createProposal creates a new proposal from the given layer
func (t *TrieDB) createProposal(parent *proposal, ops []ffi.BatchOp) (*proposal, error) {
	propose := t.Firewood.Propose
	if h := parent.handle; h != nil {
		propose = h.Propose
		proposeOnProposeCount.Inc(1)
	} else {
		proposeOnDiskCount.Inc(1)
	}
	handle, err := propose(ops)
	if err != nil {
		return nil, fmt.Errorf("create proposal from parent root %s: %w", parent.root.Hex(), err)
	}

	// Edge case: genesis block
	block := parent.height + 1
	if _, ok := parent.blockHashes[common.Hash{}]; ok && parent.root == types.EmptyRootHash {
		block = 0
	}

	p := &proposal{
		handle: handle,
		proposalMeta: &proposalMeta{
			blockHashes: make(map[common.Hash]struct{}),
			parent:      parent.proposalMeta,
			height:      block,
		},
	}

	root, err := handle.Root()
	if err != nil {
		return nil, fmt.Errorf("getting root of proposal: %w", err)
	}
	p.root = common.Hash(root)

	return p, nil
}

// cleanupCommittedProposal dereferences the proposal and removes it from the proposal map.
// It also recursively dereferences all children of the proposal.
func (ps *proposals) cleanupCommittedProposal(p *proposal) {
	oldChildren := ps.tree.children
	ps.tree = p
	ps.tree.parent = nil
	ps.tree.handle = nil

	// Since this propose has been committed, it doesn't need dropped.
	ps.removeProposalFromMap(p.proposalMeta, false)

	for _, child := range oldChildren {
		if child != p.proposalMeta {
			ps.removeProposalAndChildren(child)
		}
	}
}

// Internally removes all references of the proposal from the database.
// Frees the associated Rust memory for the proposal and all its children.
// Should only be accessed with the proposal lock held.
func (ps *proposals) removeProposalAndChildren(p *proposalMeta) {
	for _, child := range p.children {
		ps.removeProposalAndChildren(child)
	}
	ps.removeProposalFromMap(p, true)
}

// removeProposalFromMap removes the proposal from the state root map.
// The proposal lock must be held when calling this function.
// The Rust memory is explicitly freed if drop is true.
func (ps *proposals) removeProposalFromMap(meta *proposalMeta, drop bool) {
	rootList := ps.byStateRoot[meta.root]
	for i, p := range rootList {
		if p.proposalMeta == meta { // pointer comparison - guaranteed to be unique
			rootList[i] = rootList[len(rootList)-1]
			rootList[len(rootList)-1] = nil
			rootList = rootList[:len(rootList)-1]

			if drop {
				explicitlyDroppedCount.Inc(1)
				if err := p.handle.Drop(); err != nil {
					log.Error("while dropping proposal", "root", meta.root, "height", meta.height, "err", err)
				}
			}
			break
		}
	}
	if len(rootList) == 0 {
		delete(ps.byStateRoot, meta.root)
	} else {
		ps.byStateRoot[meta.root] = rootList
	}
}

// createProposals calculates the hash if the set of keys and values are
// proposed from the given parent root.
// All proposals created will be tracked for future use.
func (t *TrieDB) createProposals(parentRoot common.Hash, ops []ffi.BatchOp) (common.Hash, error) {
	start := time.Now()
	defer func() {
		hashTimer.Inc(time.Since(start).Milliseconds())
		hashCount.Inc(1)
	}()

	// Must prevent a simultaneous `Commit`, as it alters the proposal tree/disk state.
	t.proposals.Lock()
	defer t.proposals.Unlock()

	var (
		count int         // number of proposals created.
		root  common.Hash // The resulting root hash, should match between proposals
	)
	if t.proposals.tree.root == parentRoot {
		// Propose from the database root.
		p, err := t.createProposal(t.proposals.tree, ops)
		if err != nil {
			return common.Hash{}, fmt.Errorf("proposing from root %s: %w", parentRoot.Hex(), err)
		}
		root = p.root
		for parentHash := range t.proposals.tree.blockHashes {
			t.possible[possibleKey{parentBlockHash: parentHash, root: root}] = p
		}
		count++
	}

	// Find any proposal with the given parent root.
	// Since we are only using the proposal to find the root hash,
	// we can use the first proposal found.
	for _, parent := range t.proposals.byStateRoot[parentRoot] {
		p, err := t.createProposal(parent, ops)
		if err != nil {
			return common.Hash{}, fmt.Errorf("proposing from root %s: %w", parentRoot.Hex(), err)
		}
		if root != (common.Hash{}) && p.root != root {
			return common.Hash{}, fmt.Errorf("inconsistent proposal roots found for parent root %s: %#x and %#x", parentRoot.Hex(), root, p.root)
		}
		root = p.root
		for parentHash := range parent.blockHashes {
			t.possible[possibleKey{parentBlockHash: parentHash, root: root}] = p
		}
		count++
	}

	// This should never occur, as to process a block, there must be a revision to read from.
	if count == 0 {
		return common.Hash{}, fmt.Errorf("no proposals found with parent root %s", parentRoot.Hex())
	}

	return root, nil
}

// Reader retrieves a node reader belonging to the given state root.
// An error will be returned if the requested state is not available.
func (t *TrieDB) Reader(root common.Hash) (database.Reader, error) {
	revision, err := t.Firewood.Revision(ffi.Hash(root))
	if err != nil {
		return nil, fmt.Errorf("retrieve revision at root %s: %w", root.Hex(), err)
	}
	return &reader{revision: revision}, nil
}

// reader is a state reader of Database which implements the Reader interface.
type reader struct {
	revision *ffi.Revision
}

// Node retrieves the trie node with the given node hash. No error will be
// returned if the node is not found.
func (r *reader) Node(_ common.Hash, path []byte, _ common.Hash) ([]byte, error) {
	return r.revision.Get(path)
}
