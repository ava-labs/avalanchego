// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/libevm/core/state"
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
	_ proposable = (*ffi.Database)(nil)
	_ proposable = (*ffi.Proposal)(nil)

	// FFI triedb operation metrics
	ffiProposeCount         = metrics.GetOrRegisterCounter("firewood/triedb/propose/count", nil)
	ffiProposeTimer         = metrics.GetOrRegisterCounter("firewood/triedb/propose/time", nil)
	ffiCommitCount          = metrics.GetOrRegisterCounter("firewood/triedb/commit/count", nil)
	ffiCommitTimer          = metrics.GetOrRegisterCounter("firewood/triedb/commit/time", nil)
	ffiCleanupTimer         = metrics.GetOrRegisterCounter("firewood/triedb/cleanup/time", nil)
	ffiOutstandingProposals = metrics.GetOrRegisterGauge("firewood/triedb/propose/outstanding", nil)

	// FFI Trie operation metrics
	ffiHashCount = metrics.GetOrRegisterCounter("firewood/triedb/hash/count", nil)
	ffiHashTimer = metrics.GetOrRegisterCounter("firewood/triedb/hash/time", nil)
	ffiReadCount = metrics.GetOrRegisterCounter("firewood/triedb/read/count", nil)
	ffiReadTimer = metrics.GetOrRegisterCounter("firewood/triedb/read/time", nil)
)

type proposable interface {
	// Propose creates a new proposal from the current state with the given keys and values.
	Propose(keys, values [][]byte) (*ffi.Proposal, error)
	Dump() (string, error)
}

// ProposalContext represents a proposal in the Firewood database.
// This tracks all outstanding proposals to allow dereferencing upon commit.
type ProposalContext struct {
	Proposal *ffi.Proposal
	Hashes   map[common.Hash]struct{} // All corresponding block hashes
	Root     common.Hash
	Block    uint64
	Parent   *ProposalContext
	Children []*ProposalContext
}

type Config struct {
	ChainDataDir         string
	CleanCacheSize       int  // Size of the clean cache in bytes
	FreeListCacheEntries uint // Number of free list entries to cache
	Revisions            uint // Number of revisions to keep in memory (must be >= 2)
	ReadCacheStrategy    ffi.CacheStrategy
	ArchiveMode          bool
}

// Note that `FilePath` is not specified, and must always be set by the user.
var Defaults = Config{
	CleanCacheSize:       1024 * 1024, // 1MB
	FreeListCacheEntries: 40_000,
	Revisions:            100,
	ReadCacheStrategy:    ffi.CacheAllReads,
}

func (c Config) BackendConstructor(ethdb.Database) triedb.DBOverride {
	db, err := New(c)
	if err != nil {
		log.Crit("firewood: error creating database", "error", err)
	}
	return db
}

type TrieDB struct {
	fwDisk       *ffi.Database // The underlying Firewood database, used for storing proposals and revisions.
	proposalLock sync.RWMutex
	// proposalMap provides O(1) access by state root to all proposals stored in the proposalTree
	proposalMap map[common.Hash][]*ProposalContext
	// The proposal tree tracks the structure of the current proposals, and which proposals are children of which.
	// This is used to ensure that we can dereference proposals correctly and commit the correct ones
	// in the case of duplicate state roots.
	// The root of the tree is stored here, and represents the top-most layer on disk.
	proposalTree *ProposalContext

	folderName string
}

// New creates a new Firewood database with the given disk database and configuration.
// Any error during creation will cause the program to exit.
func New(config Config) (*TrieDB, error) {
	path := filepath.Join(config.ChainDataDir, firewoodDir)
	if err := validatePath(path); err != nil {
		return nil, err
	}

	options := []ffi.Option{
		ffi.WithNodeCacheEntries(uint(config.CleanCacheSize / 256)), // TODO(#4750): is 256 bytes per node a good estimate?
		ffi.WithFreeListCacheEntries(config.FreeListCacheEntries),
		ffi.WithRevisions(config.Revisions),
		ffi.WithReadCacheStrategy(config.ReadCacheStrategy),
	}
	if config.ArchiveMode {
		options = append(options, ffi.WithRootStore())
	}

	fw, err := ffi.New(path, options...)
	if err != nil {
		return nil, err
	}

	currentRoot, err := fw.Root()
	if err != nil {
		return nil, err
	}

	return &TrieDB{
		fwDisk:      fw,
		proposalMap: make(map[common.Hash][]*ProposalContext),
		proposalTree: &ProposalContext{
			Root: common.Hash(currentRoot),
		},
		folderName: path,
	}, nil
}

func validatePath(path string) error {
	if path == "" {
		return errors.New("firewood database file path must be set")
	}

	// Check that the directory exists
	dir := filepath.Dir(path)
	switch info, err := os.Stat(dir); {
	case os.IsNotExist(err):
		log.Info("Database directory not found, creating", "path", dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("error creating database directory: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("error checking database directory: %w", err)
	case !info.IsDir():
		return fmt.Errorf("database directory path is not a directory: %s", dir)
	}

	return nil
}

// Scheme returns the scheme of the database.
// This is only used in some API calls
// and in StateDB to avoid iterating through deleted storage tries.
// WARNING: If cherry-picking anything from upstream that uses this,
// it must be overwritten to use something like:
// `_, ok := db.(*Database); if !ok { return "" }`
// to recognize the Firewood database.
func (*TrieDB) Scheme() string {
	return rawdb.HashScheme
}

// Initialized checks whether a non-empty genesis block has been written.
func (t *TrieDB) Initialized(common.Hash) bool {
	root, err := t.fwDisk.Root()
	if err != nil {
		log.Error("firewood: error getting current root", "error", err)
		return false
	}

	// If the current root isn't empty, then unless the database is empty, we have a genesis block recorded.
	return common.Hash(root) != types.EmptyRootHash
}

// Update takes a root and a set of keys-values and creates a new proposal.
// It will not be committed until the Commit method is called.
// This function should be called even if there are no changes to the state to ensure proper tracking of block hashes.
func (t *TrieDB) Update(root common.Hash, parentRoot common.Hash, block uint64, nodes *trienode.MergedNodeSet, _ *triestate.Set, opts ...stateconf.TrieDBUpdateOption) error {
	// We require block hashes to be provided for all blocks in production.
	// However, many tests cannot reasonably provide a block hash for genesis, so we allow it to be omitted.
	parentHash, hash, ok := stateconf.ExtractTrieDBUpdatePayload(opts...)
	if !ok {
		log.Error("firewood: no block hash provided for block %d", block)
	}

	// The rest of the operations except key-value arranging must occur with a lock
	t.proposalLock.Lock()
	defer t.proposalLock.Unlock()

	// Check if this proposal already exists.
	// During reorgs, we may have already created this proposal.
	// Additionally, we may have already created this proposal with a different block hash.
	if existingProposals, ok := t.proposalMap[root]; ok {
		for _, existing := range existingProposals {
			// If the block hash is already tracked, we can skip proposing this again.
			if _, exists := existing.Hashes[hash]; exists {
				log.Debug("firewood: proposal already exists", "root", root.Hex(), "parent", parentRoot.Hex(), "block", block, "hash", hash.Hex())
				return nil
			}
			// We already have this proposal, but should create a new context with the correct hash.
			// This solves the case of a unique block hash, but the same underlying proposal.
			if _, exists := existing.Parent.Hashes[parentHash]; exists {
				log.Debug("firewood: proposal already exists, updating hash", "root", root.Hex(), "parent", parentRoot.Hex(), "block", block, "hash", hash.Hex())
				existing.Hashes[hash] = struct{}{}
				return nil
			}
		}
	}

	keys, values := arrangeKeyValuePairs(nodes) // may return nil, nil if no changes
	return t.propose(root, parentRoot, hash, parentHash, block, keys, values)
}

// propose creates a new proposal for every possible parent with the given keys and values.
// If the parent cannot be found, an error will be returned.
//
// To avoid having to create a new proposal for each valid state root, the block hashes are
// provided to ensure uniqueness. When this method is called, we can guarantee that the proposalContext
// must be created and tracked.
//
// Should only be accessed with the proposal lock held.
func (t *TrieDB) propose(root common.Hash, parentRoot common.Hash, hash common.Hash, parentHash common.Hash, block uint64, keys [][]byte, values [][]byte) error {
	// Find the parent proposal with the correct hash.
	// We assume the number of proposals at a given root is small, so we can iterate through them.
	for _, parentProposal := range t.proposalMap[parentRoot] {
		// If we know this proposal cannot be the parent, we can skip it.
		// Since the only possible block that won't have a parent hash is block 1,
		// and that will always be proposed from the database root,
		// we can guarantee that the parent hash will be present in one of the proposals.
		if _, exists := parentProposal.Hashes[parentHash]; !exists {
			continue
		}
		log.Debug("firewood: proposing from parent proposal", "parent", parentProposal.Root.Hex(), "root", root.Hex(), "height", block)
		p, err := createProposal(parentProposal.Proposal, root, keys, values)
		if err != nil {
			return err
		}
		pCtx := &ProposalContext{
			Proposal: p,
			Hashes:   map[common.Hash]struct{}{hash: {}},
			Root:     root,
			Block:    block,
			Parent:   parentProposal,
		}

		t.proposalMap[root] = append(t.proposalMap[root], pCtx)
		parentProposal.Children = append(parentProposal.Children, pCtx)
		return nil
	}

	// Since we were unable to find a parent proposal with the given parent hash,
	// we must create a new proposal from the database root.
	// We must avoid the case in which we are reexecuting blocks upon startup, and haven't yet stored the parent block.
	if _, exists := t.proposalTree.Hashes[parentHash]; t.proposalTree.Block != 0 && !exists {
		return fmt.Errorf("firewood: parent hash %s not found for block %s at height %d", parentHash.Hex(), hash.Hex(), block)
	} else if t.proposalTree.Root != parentRoot {
		return fmt.Errorf("firewood: parent root %s does not match proposal tree root %s for root %s at height %d", parentRoot.Hex(), t.proposalTree.Root.Hex(), root.Hex(), block)
	}

	log.Debug("firewood: proposing from database root", "root", root.Hex(), "height", block)
	p, err := createProposal(t.fwDisk, root, keys, values)
	if err != nil {
		return err
	}
	pCtx := &ProposalContext{
		Proposal: p,
		Hashes:   map[common.Hash]struct{}{hash: {}}, // This may be common.Hash{} for genesis blocks.
		Root:     root,
		Block:    block,
		Parent:   t.proposalTree,
	}
	t.proposalMap[root] = append(t.proposalMap[root], pCtx)
	t.proposalTree.Children = append(t.proposalTree.Children, pCtx)

	return nil
}

// Commit persists a proposal as a revision to the database.
//
// Any time this is called, we expect either:
//  1. The root is the same as the current root of the database (empty block during bootstrapping)
//  2. We have created a valid propsal with that root, and it is of height +1 above the proposal tree root.
//     Additionally, this should be unique.
//
// Afterward, we know that no other proposal at this height can be committed, so we can dereference all
// children in the the other branches of the proposal tree.
func (t *TrieDB) Commit(root common.Hash, report bool) error {
	// We need to lock the proposal tree to prevent concurrent writes.
	t.proposalLock.Lock()
	defer t.proposalLock.Unlock()

	// Find the proposal with the given root.
	var pCtx *ProposalContext
	for _, possible := range t.proposalMap[root] {
		if possible.Parent.Root == t.proposalTree.Root && possible.Parent.Block == t.proposalTree.Block {
			// We found the proposal with the correct parent.
			if pCtx != nil {
				// This should never happen, as we ensure that we don't create duplicate proposals in `propose`.
				return fmt.Errorf("firewood: multiple proposals found for %s", root.Hex())
			}
			pCtx = possible
		}
	}
	if pCtx == nil {
		return fmt.Errorf("firewood: committable proposal not found for %s", root.Hex())
	}

	start := time.Now()
	// Commit the proposal to the database.
	if err := pCtx.Proposal.Commit(); err != nil {
		t.dereference(pCtx) // no longer committable
		return fmt.Errorf("firewood: error committing proposal %s: %w", root.Hex(), err)
	}
	ffiCommitCount.Inc(1)
	ffiCommitTimer.Inc(time.Since(start).Milliseconds())
	ffiOutstandingProposals.Dec(1)
	// Now that the proposal is committed, we should clean up the proposal tree on return.
	defer t.cleanupCommittedProposal(pCtx)

	// Assert that the root of the database matches the committed proposal root.
	currentRoot, err := t.fwDisk.Root()
	if err != nil {
		return fmt.Errorf("firewood: error getting current root after commit: %w", err)
	}

	currentRootHash := common.Hash(currentRoot)
	if currentRootHash != root {
		return fmt.Errorf("firewood: current root %s does not match expected root %s", currentRootHash.Hex(), root.Hex())
	}

	if report {
		log.Info("Persisted proposal to firewood database", "root", root)
	} else {
		log.Debug("Persisted proposal to firewood database", "root", root)
	}
	return nil
}

// Size returns the storage size of diff layer nodes above the persistent disk
// layer and the dirty nodes buffered within the disk layer
// Only used for metrics and Commit intervals in APIs.
// This will be implemented in the firewood database eventually.
// Currently, Firewood stores all revisions in disk and proposals in memory.
func (*TrieDB) Size() (common.StorageSize, common.StorageSize) {
	return 0, 0
}

// Reference is a no-op.
func (*TrieDB) Reference(common.Hash, common.Hash) {}

// Dereference is a no-op since Firewood handles unused state roots internally.
func (*TrieDB) Dereference(common.Hash) {}

// Firewood does not support this.
func (*TrieDB) Cap(common.StorageSize) error {
	return nil
}

func (t *TrieDB) Close() error {
	t.proposalLock.Lock()
	defer t.proposalLock.Unlock()

	// before closing, we must deference any outstanding proposals to free the
	// memory owned by firewood (outside of go's memory management)
	for _, pCtx := range t.proposalTree.Children {
		t.dereference(pCtx)
	}

	t.proposalMap = nil
	t.proposalTree.Children = nil

	// Close the database
	// This may block momentarily while finalizers for Firewood objects run.
	return t.fwDisk.Close(context.Background())
}

// createProposal creates a new proposal from the given layer
// If there are no changes, it will return nil.
func createProposal(layer proposable, root common.Hash, keys, values [][]byte) (p *ffi.Proposal, err error) {
	// If there's an error after creating the proposal, we must drop it.
	defer func() {
		if err != nil && p != nil {
			if dropErr := p.Drop(); dropErr != nil {
				// We should still return the original error.
				log.Error("firewood: error dropping proposal after error", "root", root.Hex(), "error", dropErr)
			}
			p = nil
		}
	}()

	if len(keys) != len(values) {
		return nil, fmt.Errorf("firewood: keys and values must have the same length, got %d keys and %d values", len(keys), len(values))
	}

	start := time.Now()
	p, err = layer.Propose(keys, values)
	if err != nil {
		return nil, fmt.Errorf("firewood: unable to create proposal for root %s: %w", root.Hex(), err)
	}
	ffiProposeCount.Inc(1)
	ffiProposeTimer.Inc(time.Since(start).Milliseconds())
	ffiOutstandingProposals.Inc(1)

	currentRoot, err := p.Root()
	if err != nil {
		return nil, fmt.Errorf("firewood: error getting root of proposal %s: %w", root, err)
	}

	currentRootHash := common.Hash(currentRoot)
	if root != currentRootHash {
		return nil, fmt.Errorf("firewood: proposed root %s does not match expected root %s", currentRootHash.Hex(), root.Hex())
	}

	return p, nil
}

// cleanupCommittedProposal dereferences the proposal and removes it from the proposal map.
// It also recursively dereferences all children of the proposal.
func (t *TrieDB) cleanupCommittedProposal(pCtx *ProposalContext) {
	start := time.Now()
	oldChildren := t.proposalTree.Children
	t.proposalTree = pCtx
	t.proposalTree.Parent = nil

	t.removeProposalFromMap(pCtx)

	for _, childCtx := range oldChildren {
		// Don't dereference the recently commit proposal.
		if childCtx != pCtx {
			t.dereference(childCtx)
		}
	}
	ffiCleanupTimer.Inc(time.Since(start).Milliseconds())
}

// Internally removes all references of the proposal from the database.
// Should only be accessed with the proposal lock held.
// Consumer must not be iterating the proposal map at this root.
func (t *TrieDB) dereference(pCtx *ProposalContext) {
	// Base case: if there are children, we need to dereference them as well.
	for _, child := range pCtx.Children {
		t.dereference(child)
	}
	pCtx.Children = nil

	// Remove the proposal from the map.
	t.removeProposalFromMap(pCtx)

	// Drop the proposal in the backend.
	if err := pCtx.Proposal.Drop(); err != nil {
		log.Error("firewood: error dropping proposal", "root", pCtx.Root.Hex(), "error", err)
	}
	ffiOutstandingProposals.Dec(1)
}

// removeProposalFromMap removes the proposal from the proposal map.
// The proposal lock must be held when calling this function.
func (t *TrieDB) removeProposalFromMap(pCtx *ProposalContext) {
	rootList := t.proposalMap[pCtx.Root]
	for i, p := range rootList {
		if p == pCtx { // pointer comparison - guaranteed to be unique
			rootList[i] = rootList[len(rootList)-1]
			rootList[len(rootList)-1] = nil
			rootList = rootList[:len(rootList)-1]
			break
		}
	}
	if len(rootList) == 0 {
		delete(t.proposalMap, pCtx.Root)
	} else {
		t.proposalMap[pCtx.Root] = rootList
	}
}

// Reader retrieves a node reader belonging to the given state root.
// An error will be returned if the requested state is not available.
func (t *TrieDB) Reader(root common.Hash) (database.Reader, error) {
	if _, err := t.fwDisk.GetFromRoot(ffi.Hash(root), []byte{}); err != nil {
		return nil, fmt.Errorf("firewood: unable to retrieve from root %s: %w", root.Hex(), err)
	}
	return &reader{db: t, root: ffi.Hash(root)}, nil
}

// reader is a state reader of Database which implements the Reader interface.
type reader struct {
	db   *TrieDB
	root ffi.Hash // The root of the state this reader is reading.
}

// Node retrieves the trie node with the given node hash. No error will be
// returned if the node is not found.
func (reader *reader) Node(_ common.Hash, path []byte, _ common.Hash) ([]byte, error) {
	// This function relies on Firewood's internal locking to ensure concurrent reads are safe.
	// This is safe even if a proposal is being committed concurrently.
	start := time.Now()
	result, err := reader.db.fwDisk.GetFromRoot(reader.root, path)
	if metrics.EnabledExpensive {
		ffiReadCount.Inc(1)
		ffiReadTimer.Inc(time.Since(start).Milliseconds())
	}
	return result, err
}

// getProposalHash calculates the hash if the set of keys and values are
// proposed from the given parent root.
func (t *TrieDB) getProposalHash(parentRoot common.Hash, keys, values [][]byte) (common.Hash, error) {
	// This function only reads from existing tracked proposals, so we can use a read lock.
	t.proposalLock.RLock()
	defer t.proposalLock.RUnlock()

	var (
		p   *ffi.Proposal
		err error
	)
	start := time.Now()
	if t.proposalTree.Root == parentRoot {
		// Propose from the database root.
		p, err = t.fwDisk.Propose(keys, values)
		if err != nil {
			return common.Hash{}, fmt.Errorf("firewood: error proposing from root %s: %w", parentRoot.Hex(), err)
		}
	} else {
		// Find any proposal with the given parent root.
		// Since we are only using the proposal to find the root hash,
		// we can use the first proposal found.
		proposals, ok := t.proposalMap[parentRoot]
		if !ok || len(proposals) == 0 {
			return common.Hash{}, fmt.Errorf("firewood: no proposal found for parent root %s", parentRoot.Hex())
		}
		rootProposal := proposals[0].Proposal

		p, err = rootProposal.Propose(keys, values)
		if err != nil {
			return common.Hash{}, fmt.Errorf("firewood: error proposing from parent proposal %s: %w", parentRoot.Hex(), err)
		}
	}
	ffiHashCount.Inc(1)
	ffiHashTimer.Inc(time.Since(start).Milliseconds())

	// We succesffuly created a proposal, so we must drop it after use.
	defer func() {
		if err := p.Drop(); err != nil {
			log.Error("firewood: error dropping proposal after hash computation", "parentRoot", parentRoot.Hex(), "error", err)
		}
	}()

	root, err := p.Root()
	if err != nil {
		return common.Hash{}, err
	}
	return common.Hash(root), nil
}

func arrangeKeyValuePairs(nodes *trienode.MergedNodeSet) ([][]byte, [][]byte) {
	if nodes == nil {
		return nil, nil // No changes to propose
	}
	// Create key-value pairs for the nodes in bytes.
	var (
		acctKeys      [][]byte
		acctValues    [][]byte
		storageKeys   [][]byte
		storageValues [][]byte
	)

	flattenedNodes := nodes.Flatten()

	for _, nodeset := range flattenedNodes {
		for str, node := range nodeset {
			if len(str) == common.HashLength {
				// This is an account node.
				acctKeys = append(acctKeys, []byte(str))
				acctValues = append(acctValues, node.Blob)
			} else {
				storageKeys = append(storageKeys, []byte(str))
				storageValues = append(storageValues, node.Blob)
			}
		}
	}

	// We need to do all storage operations first, so prefix-deletion works for accounts.
	return append(storageKeys, acctKeys...), append(storageValues, acctValues...)
}

// DumpAll writes all nodes in the given state database to Firewood.
func (t *TrieDB) DumpAll(statedb *state.StateDB, height uint64, parentHash, blockHash, parentRoot, expectedRoot common.Hash) error {
	triedbOpt := stateconf.WithTrieDBUpdatePayload(parentHash, blockHash)
	statedbOpt := stateconf.WithTrieDBUpdateOpts(triedbOpt)
	wrongRoot, err := statedb.Commit(height, false, statedbOpt)
	if err != nil {
		return fmt.Errorf("firewood: error committing state db for dump: %w", err)
	}
	if wrongRoot == expectedRoot {
		return fmt.Errorf("firewood: unexpected matching root when dumping all nodes, got %s", expectedRoot.Hex())
	}

	t.proposalLock.Lock()
	defer t.proposalLock.Unlock()

	root, err := t.fwDisk.Root()
	if err != nil {
		return fmt.Errorf("firewood: error getting current root for dump: %w", err)
	}

	var parentP proposable
	if common.Hash(root) != parentRoot {
		log.Error("parent state not yet committed, dumping last committed state")
		if err := t.dumpState(t.fwDisk, height-2); err != nil {
			log.Error("dumping height - 2", "error", err)
		}
		log.Info("firewood: dumping parent state", "height", height-1, "expectedRoot", parentRoot.Hex())
		parents, _ := t.proposalMap[parentRoot]
		if len(parents) != 1 {
			return fmt.Errorf("firewood: %d proposals found for parent root %s during dump", len(parents), parentRoot.Hex())
		}
		log.Info("firewood: found proposal for parent root during dump", "height", height-1, "expectedRoot", parentRoot.Hex())
		parentP = parents[0].Proposal
		if err := t.dumpState(parentP, height-1); err != nil {
			log.Error("dumping parent proposal", "error", err)
		}
	} else {
		log.Info("firewood: dumping parent state from disk", "height", height-1, "expectedRoot", parentRoot.Hex())
		if err := t.dumpState(t.fwDisk, height-1); err != nil {
			log.Error("dumping parent from disk", "error", err)
		}
		parentP = t.fwDisk
	}

	// Dump current state
	if err := t.dumpState(parentP, height); err != nil {
		log.Error("dumping current proposal", "error", err)
	}
	return nil
}

// dumpState writes all nodes in the given state database to Firewood.
// File name is derived from height, in the folder specified in the database.
func (t *TrieDB) dumpState(d proposable, height uint64) error {
	graph, err := d.Dump()
	if err != nil {
		return fmt.Errorf("firewood: error dumping state at height %d: %w", height, err)
	}

	// Open file for writing
	filename := fmt.Sprintf("dump-%d.graph", height)
	path := filepath.Join(t.folderName, filename)
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("firewood: error creating dump file %s: %w", path, err)
	}
	defer file.Close()

	// Write graph to file
	if _, err := file.WriteString(graph); err != nil {
		return fmt.Errorf("firewood: error writing dump to file %s: %w", path, err)
	}

	log.Info("Firewood: dumped state graph to file", "height", height, "file", path)
	return nil
}
