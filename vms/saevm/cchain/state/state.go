// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package state manages the on-disk state for the C-Chain's cross-chain
// transactions.
package state

import (
	"fmt"
	"iter"
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/avalanchego/graft/evm/triedb/hashdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"

	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
)

// These prefixes and keys are byte-compatible with the indices written by
// [state.AtomicTrie] and [state.AtomicRepository] so that SAE does not require
// a database migration.
var (
	triePrefix = []byte("atomicTrieDB") // [state.atomicTrieStoragePrefix]

	commitPrefix  = prefixdb.MakePrefix([]byte("atomicTrieMetaDB"))                          // [state.atomicTrieMetaDBPrefix]
	lastCommitKey = prefixdb.PrefixKey(commitPrefix, []byte("atomicTrieLastCommittedBlock")) // [state.lastCommittedKey]

	txIDPrefix   = prefixdb.MakePrefix([]byte("atomicTxDB"))       // [state.atomicTxIDDBPrefix]
	heightPrefix = prefixdb.MakePrefix([]byte("atomicHeightTxDB")) // [state.atomicHeightTxDBPrefix]
)

// State holds the accepted transactions and the atomic-request trie. It is not
// safe for concurrent use; callers must serialize calls to [State.Apply].
type State struct {
	db     database.Database
	trieDB *triedb.Database

	lastCommittedHeight uint64
	currentRoot         common.Hash
}

// New opens the state on db. On startup, the in-memory trie is rebuilt by
// replaying the accepted-tx index from the last committed height up to the
// current height.
func New(db database.Database) (*State, error) {
	root, height, err := readLastCommitted(db)
	if err != nil {
		return nil, err
	}

	trieDB := triedb.NewDatabase(
		rawdb.NewDatabase(evmdb.New(prefixdb.NewNested(triePrefix, db))),
		&triedb.Config{
			DBOverride: hashdb.Config{
				CleanCacheSize: 64 * units.MiB,
			}.BackendConstructor,
		},
	)

	for entry, err := range iterateTxs(db, height+1) {
		if err != nil {
			return nil, fmt.Errorf("replaying tx index: %w", err)
		}
		// Tx entries are written atomically with the committed root, so
		// [applyTrie] should never commit a new root to disk.
		root, err = applyTrie(trieDB, root, entry.height, entry.txs)
		if err != nil {
			return nil, fmt.Errorf("replaying height %d: %w", entry.height, err)
		}
	}

	return &State{
		db:                  db,
		trieDB:              trieDB,
		lastCommittedHeight: height,
		currentRoot:         root,
	}, nil
}

// readLastCommitted returns the last committed trie root and the height it
// was committed at. If none have been written, it returns the empty root for
// the genesis block.
func readLastCommitted(db database.KeyValueReader) (common.Hash, uint64, error) {
	height, err := database.GetUInt64(db, lastCommitKey)
	switch {
	case err == database.ErrNotFound:
		return types.EmptyRootHash, 0, nil
	case err != nil:
		return common.Hash{}, 0, fmt.Errorf("getting last committed height: %w", err)
	}

	root, err := readCommittedRoot(db, height)
	if err != nil {
		return common.Hash{}, 0, fmt.Errorf("getting committed root for height %d: %w", height, err)
	}
	return root, height, nil
}

func readCommittedRoot(db database.KeyValueReader, height uint64) (common.Hash, error) {
	if height == 0 {
		return types.EmptyRootHash, nil
	}
	root, err := database.GetID(db, rootKey(height))
	if err != nil {
		return common.Hash{}, err
	}
	return common.Hash(root), nil
}

func rootKey(height uint64) []byte {
	return prefixdb.PrefixKey(commitPrefix, database.PackUInt64(height))
}

type txsEntry struct {
	height uint64
	txs    []*tx.Tx
}

// iterateTxs returns an iterator yielding (height, txs) entries starting at
// startHeight in ascending height order.
func iterateTxs(db database.Iteratee, startHeight uint64) iter.Seq2[txsEntry, error] {
	return func(yield func(txsEntry, error) bool) {
		it := db.NewIteratorWithStartAndPrefix(
			prefixdb.PrefixKey(heightPrefix, database.PackUInt64(startHeight)),
			heightPrefix,
		)
		defer it.Release()

		for it.Next() {
			height, err := database.ParseUInt64(it.Key()[len(heightPrefix):])
			if err != nil {
				yield(txsEntry{}, fmt.Errorf("parsing height key %x: %w", it.Key(), err))
				return
			}

			txs, err := tx.ParseSlice(it.Value())
			if err != nil {
				yield(txsEntry{}, fmt.Errorf("parsing txs at height %d: %w", height, err))
				return
			}

			if !yield(txsEntry{height: height, txs: txs}, nil) {
				return
			}
		}

		if err := it.Error(); err != nil {
			yield(txsEntry{}, fmt.Errorf("iterating tx index: %w", err))
		}
	}
}

const commitInterval = 4096 // [config.defaultCommitInterval]

// Apply persists the txs accepted at height. It applies their atomic
// operations to the trie, indexes the txs by ID and by height, and on
// commit-interval boundaries flushes the trie to disk. All on-disk writes are
// committed atomically.
//
// Apply must be called with contiguous monotonically-increasing heights.
func (s *State) Apply(height uint64, txs []*tx.Tx) error {
	// The trie and the height-based index were initially generated from the
	// already existing id-based index. This resulted in data canonically sorted
	// by txID. To maintain this behavior, txs must be sorted by ID before
	// applying to the trie and writing to the height index.
	sorted := slices.Clone(txs)
	slices.SortFunc(sorted, func(a, b *tx.Tx) int {
		return a.ID().Compare(b.ID())
	})

	var err error
	s.currentRoot, err = applyTrie(s.trieDB, s.currentRoot, height, sorted)
	if err != nil {
		return fmt.Errorf("applying height %d: %w", height, err)
	}

	batch := s.db.NewBatch()
	for _, t := range sorted {
		if err := writeTxByID(batch, height, t); err != nil {
			return fmt.Errorf("writing tx %s at height %d: %w", t.ID(), height, err)
		}
	}
	if err := writeTxsByHeight(batch, height, sorted); err != nil {
		return fmt.Errorf("writing txs at height %d: %w", height, err)
	}
	if height%commitInterval == 0 {
		if err := writeCommittedRoot(batch, height, s.currentRoot); err != nil {
			return err
		}
		s.lastCommittedHeight = height
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("committing batch at height %d: %w", height, err)
	}
	return nil
}

func writeTxByID(db database.KeyValueWriter, height uint64, tx *tx.Tx) error {
	txBytes, err := tx.Bytes()
	if err != nil {
		return err
	}

	p := wrappers.Packer{
		Bytes: make([]byte, wrappers.LongLen+wrappers.IntLen+len(txBytes)),
	}
	p.PackLong(height)
	p.PackBytes(txBytes)

	txID := tx.ID()
	return db.Put(prefixdb.PrefixKey(txIDPrefix, txID[:]), p.Bytes)
}

// writeTxsByHeight stores txs under the height key. Empty heights skip the
// write: an empty payload would persist a byte sequence that no read
// distinguishes from an absent key.
func writeTxsByHeight(db database.KeyValueWriter, height uint64, txs []*tx.Tx) error {
	if len(txs) == 0 {
		return nil
	}
	txsBytes, err := tx.MarshalSlice(txs)
	if err != nil {
		return err
	}
	return db.Put(prefixdb.PrefixKey(heightPrefix, database.PackUInt64(height)), txsBytes)
}

// writeCommittedRoot maps height to root and marks it as the last committed
// entry.
func writeCommittedRoot(db database.KeyValueWriter, height uint64, root common.Hash) error {
	if err := database.PutUInt64(db, lastCommitKey, height); err != nil {
		return fmt.Errorf("writing last committed height: %w", err)
	}
	if err := database.PutID(db, rootKey(height), ids.ID(root)); err != nil {
		return fmt.Errorf("writing committed root for height %d: %w", height, err)
	}
	return nil
}

// GetTx returns the tx with the given ID along with the block height it was
// accepted at.
func (s *State) GetTx(txID ids.ID) (*tx.Tx, uint64, error) {
	b, err := s.db.Get(prefixdb.PrefixKey(txIDPrefix, txID[:]))
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

// GetRoot returns the atomic trie root committed at height. Only multiples
// of the commit interval (and height 0, which returns the empty root) have
// committed roots; intermediate heights return [database.ErrNotFound].
func (s *State) GetRoot(height uint64) (common.Hash, error) {
	return readCommittedRoot(s.db, height)
}

// LastCommitted returns the height of the most recent on-disk trie commit.
func (s *State) LastCommitted() uint64 {
	return s.lastCommittedHeight
}

const trieKeyLength = state.TrieKeyLength

// applyTrie updates the trie to include the atomic requests from txs into the
// trie rooted at root. It flushes the new trie to disk if height is a commit
// boundary and releases the prior root. It returns the new root.
func applyTrie(trieDB *triedb.Database, oldRoot common.Hash, height uint64, txs []*tx.Tx) (common.Hash, error) {
	tr, err := trie.New(trie.TrieID(oldRoot), trieDB)
	if err != nil {
		return common.Hash{}, fmt.Errorf("opening atomic trie at root %s: %w", oldRoot, err)
	}

	ops := make(map[ids.ID]*atomic.Requests)
	for _, t := range txs {
		chainID, req, err := t.AtomicRequests()
		if err != nil {
			return common.Hash{}, fmt.Errorf("getting atomic requests for tx %s: %w", t.ID(), err)
		}
		if existing, ok := ops[chainID]; ok {
			existing.PutRequests = append(existing.PutRequests, req.PutRequests...)
			existing.RemoveRequests = append(existing.RemoveRequests, req.RemoveRequests...)
		} else {
			ops[chainID] = req
		}
	}
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
		return common.Hash{}, fmt.Errorf("committing in-memory trie at height %d: %w", height, err)
	}
	if nodes != nil {
		// The parent-root and block-number args have no effect under hashdb;
		// EmptyRootHash suppresses an otherwise-spurious parent-presence
		// warning.
		if err := trieDB.Update(newRoot, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil); err != nil {
			return common.Hash{}, fmt.Errorf("updating trieDB with new nodes: %w", err)
		}
	}
	if err := trieDB.Reference(newRoot, common.Hash{}); err != nil {
		return common.Hash{}, fmt.Errorf("referencing root %s: %w", newRoot, err)
	}

	const (
		memoryCap = 64 * units.MiB
		// Cap trims to memoryCap - IdealBatchSize rather than memoryCap to
		// avoid a flush on every call once the resident size sits near the cap.
		memoryCapTrim = memoryCap - ethdb.IdealBatchSize
	)
	if _, nodeSize, _ := trieDB.Size(); nodeSize > memoryCap {
		if err := trieDB.Cap(memoryCapTrim); err != nil {
			return common.Hash{}, fmt.Errorf("capping atomic trie at root %s: %w", newRoot, err)
		}
	}
	if height%commitInterval == 0 {
		if err := trieDB.Commit(newRoot, false); err != nil {
			return common.Hash{}, fmt.Errorf("committing trieDB at height %d root %s: %w", height, newRoot, err)
		}
	}

	if err := trieDB.Dereference(oldRoot); err != nil {
		return common.Hash{}, fmt.Errorf("dereferencing prior root %s: %w", oldRoot, err)
	}
	return newRoot, nil
}
