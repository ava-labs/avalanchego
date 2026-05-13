// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package state manages the on-disk state for the C-Chain's cross-chain
// transactions.
package state

import (
	"errors"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/libevm/triedb/hashdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"

	oldatomic "github.com/ava-labs/avalanchego/chains/atomic"
	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
)

// These prefixes and keys are byte-compatible with the indices written by
// [state.AtomicTrie] and [state.AtomicRepository] so that SAE does not require
// a database migration.
var (
	triePrefix = []byte("atomicTrieDB") // See state.atomicTrieStoragePrefix

	commitPrefix  = prefixdb.MakePrefix([]byte("atomicTrieMetaDB"))                          // See state.atomicTrieMetaDBPrefix
	lastHeightKey = prefixdb.PrefixKey(commitPrefix, []byte("atomicTrieLastCommittedBlock")) // See state.lastCommittedKey

	txPrefix = prefixdb.MakePrefix([]byte("atomicTxDB")) // See state.atomicTxIDDBPrefix
)

// State holds the accepted transactions and the atomic-request trie.
//
// When applying operations, shared memory is updated atomically with the state.
//
// [State.Close] MUST be called when finished with the state to release
// resources.
type State struct {
	snowCtx *snow.Context
	db      database.Database
	trieDB  *triedb.Database

	currentRoot   common.Hash
	currentHeight atomic.Uint64
}

// New initializes the state with db.
//
// TODO(#5375): Coreth's commitInterval must be reduced to 1 prior to
// transitioning to SAE. Otherwise, the atomic trie may not contain operations
// for recent blocks.
func New(snowCtx *snow.Context, db database.Database) (*State, error) {
	root, height, err := readLast(db)
	if err != nil {
		return nil, err
	}

	s := &State{
		snowCtx: snowCtx,
		db:      db,
		// Coreth previously wrapped db in a versiondb before using
		// [prefixdb.New]. To maintain byte compatibility with the existing
		// trie, we must use [prefixdb.NewNested] rather than [prefixdb.New] and
		// not compress the prefix.
		trieDB: triedb.NewDatabase(
			rawdb.NewDatabase(evmdb.New(prefixdb.NewNested(triePrefix, db))),
			&triedb.Config{
				HashDB: &hashdb.Config{
					// This trie is append only, so we only need to cache the
					// leading edge of the trie.
					CleanCacheSize: units.MiB,
				},
			},
		),
		currentRoot: root,
	}
	s.currentHeight.Store(height)
	return s, nil
}

// readLast returns the last trie root and height. If none have been written, it
// returns the empty root for the genesis block.
func readLast(db database.KeyValueReader) (common.Hash, uint64, error) {
	height, err := database.GetUInt64(db, lastHeightKey)
	switch {
	case errors.Is(err, database.ErrNotFound):
		return types.EmptyRootHash, 0, nil
	case err != nil:
		return common.Hash{}, 0, fmt.Errorf("getting last height: %w", err)
	}

	root, err := readRoot(db, height)
	if err != nil {
		return common.Hash{}, 0, fmt.Errorf("getting root for height %d: %w", height, err)
	}
	return root, height, nil
}

func readRoot(db database.KeyValueReader, height uint64) (common.Hash, error) {
	if height == 0 {
		return types.EmptyRootHash, nil
	}
	root, err := database.GetID(db, rootKey(height))
	return common.Hash(root), err
}

func rootKey(height uint64) []byte {
	return prefixdb.PrefixKey(commitPrefix, database.PackUInt64(height))
}

// Apply persists the txs accepted at height. It applies their atomic
// operations to the trie, indexes the txs by ID, and applies the atomic
// operations to shared memory.
//
// Apply is a noop when height is not higher than [State.CurrentHeight].
//
// Apply is not thread safe.
func (s *State) Apply(height uint64, txs []*tx.Tx) error {
	if currentHeight := s.currentHeight.Load(); height <= currentHeight {
		// During restarts, it is expected for SAE to reprocess already-applied
		// heights. Shared memory is not safe to apply multiple times for the
		// same height, so we MUST skip these duplicate applications.
		s.snowCtx.Log.Debug("skipping writes for prior height",
			zap.Uint64("height", height),
			zap.Uint64("currentHeight", currentHeight),
		)
		return nil
	}

	ops, err := atomicRequests(txs)
	if err != nil {
		return fmt.Errorf("merging atomic ops: %w", err)
	}
	newRoot, err := applyTrie(s.trieDB, s.currentRoot, height, ops)
	if err != nil {
		return fmt.Errorf("applying trie on root %s: %w", s.currentRoot, err)
	}

	batch := s.db.NewBatch()
	for _, t := range txs {
		if err := writeTx(batch, height, t); err != nil {
			return fmt.Errorf("writing tx %s: %w", t.ID(), err)
		}
	}
	if err := database.PutUInt64(batch, lastHeightKey, height); err != nil {
		return fmt.Errorf("writing last height: %w", err)
	}
	if err := database.PutID(batch, rootKey(height), ids.ID(newRoot)); err != nil {
		return fmt.Errorf("writing root: %w", err)
	}
	// Committing the batch atomically with shared memory prevents duplicate
	// shared memory operations in the event of a crash.
	//
	// TODO(StephenButtolph): Skip applying shared memory operations for bonus
	// blocks.
	if err := s.snowCtx.SharedMemory.Apply(ops, batch); err != nil {
		return fmt.Errorf("applying shared memory: %w", err)
	}

	s.snowCtx.Log.Debug("updated atomic trie",
		zap.Uint64("height", height),
		zap.Stringer("root", newRoot),
	)

	s.currentRoot = newRoot
	s.currentHeight.Store(height)
	return nil
}

// atomicRequests groups the atomic requests from txs by chainID.
func atomicRequests(txs []*tx.Tx) (map[ids.ID]*oldatomic.Requests, error) {
	// To produce a byte-identical trie, txs must be merged in txID order.
	// This matches the order they were originally read from the tx index when
	// the trie was first built. Without sorting, the PutRequests and
	// RemoveRequests within a chain's atomic.Requests could be appended in a
	// different order, changing the trie value.
	sorted := slices.Clone(txs)
	slices.SortFunc(sorted, func(a, b *tx.Tx) int {
		return a.ID().Compare(b.ID())
	})

	ops := make(map[ids.ID]*oldatomic.Requests)
	for _, t := range sorted {
		chainID, req, err := t.AtomicRequests()
		if err != nil {
			return nil, fmt.Errorf("getting atomic requests for tx %s: %w", t.ID(), err)
		}
		if existing, ok := ops[chainID]; ok {
			existing.PutRequests = append(existing.PutRequests, req.PutRequests...)
			existing.RemoveRequests = append(existing.RemoveRequests, req.RemoveRequests...)
		} else {
			ops[chainID] = req
		}
	}
	return ops, nil
}

const trieKeyLength = state.TrieKeyLength

// applyTrie writes the per-chain ops into the trie rooted at oldRoot, flushes
// the resulting trie to disk, and returns the new root.
func applyTrie(trieDB *triedb.Database, oldRoot common.Hash, height uint64, ops map[ids.ID]*oldatomic.Requests) (common.Hash, error) {
	// Most blocks don't have atomic requests, so we avoid any unnecessary disk
	// reads in that case.
	if len(ops) == 0 {
		return oldRoot, nil
	}

	tr, err := trie.New(trie.TrieID(oldRoot), trieDB)
	if err != nil {
		return common.Hash{}, fmt.Errorf("opening trie: %w", err)
	}

	// Since each map entry corresponds to a different entry in the trie, the
	// trie root is order-independent.
	for chainID, requests := range ops {
		v, err := c.Marshal(codecVersion, requests)
		if err != nil {
			return common.Hash{}, fmt.Errorf("marshaling atomic requests for chain %s: %w", chainID, err)
		}

		p := wrappers.Packer{Bytes: make([]byte, trieKeyLength)}
		p.PackLong(height)
		p.PackFixedBytes(chainID[:])
		if err := tr.Update(p.Bytes, v); err != nil {
			return common.Hash{}, fmt.Errorf("inserting trie key for chain %s: %w", chainID, err)
		}
	}

	newRoot, nodes, err := tr.Commit(false)
	if err != nil {
		return common.Hash{}, fmt.Errorf("committing in-memory trie: %w", err)
	}
	if nodes != nil {
		// The parent-root and block-number args have no effect with hashdb;
		// EmptyRootHash suppresses an otherwise-spurious parent-presence
		// warning.
		if err := trieDB.Update(newRoot, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil); err != nil {
			return common.Hash{}, fmt.Errorf("updating trieDB with new nodes: %w", err)
		}
	}
	if err := trieDB.Commit(newRoot, false); err != nil {
		return common.Hash{}, fmt.Errorf("committing trieDB at root %s: %w", newRoot, err)
	}
	return newRoot, nil
}

// TODO(StephenButtolph): There isn't a good reason for us to actually write the
// full tx to the database. We could just write the height, and then expect the
// caller to fetch the block to get the tx. Paying that extra block fetch on the
// read side is almost certainly worth avoiding the duplicate write, since this
// index is only read from the API.
func writeTx(db database.KeyValueWriter, height uint64, t *tx.Tx) error {
	txBytes, err := t.Bytes()
	if err != nil {
		return err
	}

	p := wrappers.Packer{
		Bytes: make([]byte, wrappers.LongLen+wrappers.IntLen+len(txBytes)),
	}
	p.PackLong(height)
	p.PackBytes(txBytes)

	txID := t.ID()
	return db.Put(txKey(txID), p.Bytes)
}

func txKey(id ids.ID) []byte {
	return prefixdb.PrefixKey(txPrefix, id[:])
}

// GetTx returns the tx with the given ID along with the block height it was
// accepted at.
func (s *State) GetTx(txID ids.ID) (*tx.Tx, uint64, error) {
	b, err := s.db.Get(txKey(txID))
	if err != nil {
		return nil, 0, fmt.Errorf("reading tx: %w", err)
	}

	p := wrappers.Packer{Bytes: b}
	height := p.UnpackLong()
	txBytes := p.UnpackBytes()
	if p.Errored() {
		return nil, 0, fmt.Errorf("unpacking tx: %w", p.Err)
	}

	parsed, err := tx.Parse(txBytes)
	if err != nil {
		return nil, 0, fmt.Errorf("parsing tx: %w", err)
	}
	return parsed, height, nil
}

// GetRoot returns the atomic trie root at height.
func (s *State) GetRoot(height uint64) (common.Hash, error) {
	return readRoot(s.db, height)
}

// CurrentHeight returns the highest height successfully applied via
// [State.Apply].
func (s *State) CurrentHeight() uint64 {
	return s.currentHeight.Load()
}

// Close closes the state, releasing any resources.
func (s *State) Close() error {
	return s.trieDB.Close()
}
