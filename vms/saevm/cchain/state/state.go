// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package state stores the C-Chain custom-transaction state: an index of
// accepted cross-chain transactions, an atomic-request trie, and the
// metadata needed to resume after restart.
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

// Database layout — these prefixes and keys must remain byte-compatible
// with the indices written by [state.AtomicTrie] and [state.AtomicRepository]
// so the chain can read entries produced by older nodes:
//
//	atomicTrieMetaDB / "atomicTrieLastCommittedBlock" -> uint64(8 BE)
//	atomicTrieMetaDB / [height(8 BE)]                 -> trie root (32 bytes)
//	atomicTrieDB     / <trie node keys>               -> RLP nodes (libevm trie)
//	atomicTxDB       / [txID(32)]                     -> [height(8 BE)][len(4 BE)][tx_bytes]
//	atomicHeightTxDB / [height(8 BE)]                 -> codec.Marshal(0, []*tx.Tx)
var (
	// Matches state.atomicTrieStoragePrefix.
	triePrefix = []byte("atomicTrieDB")

	// Matches state.atomicTrieMetaDBPrefix.
	commitPrefix = prefixdb.MakePrefix([]byte("atomicTrieMetaDB"))
	// Matches state.lastCommittedKey under commitPrefix.
	lastCommittedHeightKey = prefixdb.PrefixKey(commitPrefix, []byte("atomicTrieLastCommittedBlock"))

	// Matches state.atomicTxIDDBPrefix.
	txIDPrefix = prefixdb.MakePrefix([]byte("atomicTxDB"))
	// Matches state.atomicHeightTxDBPrefix.
	heightPrefix = prefixdb.MakePrefix([]byte("atomicHeightTxDB"))
)

func trieMetadataHeightKey(height uint64) []byte {
	return prefixdb.PrefixKey(commitPrefix, database.PackUInt64(height))
}

// State holds the C-Chain custom state. It is not safe for concurrent use.
//
// Collapses [state.AtomicBackend], [state.AtomicRepository], and
// [state.AtomicTrie] into a single type.
type State struct {
	db     database.Database
	trieDB *triedb.Database

	lastCommittedHeight uint64
	currentRoot         common.Hash
}

// New opens the state in db. On startup, the in-memory trie is rebuilt by
// replaying the accepted-tx index from the last committed height up to the
// current height.
func New(db database.Database) (*State, error) {
	root, height, err := readLastCommittedRoot(db)
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

	// Replay (lastCommittedHeight, currentHeight] from the tx index.
	// applyTrie handles Dereference internally; no metadata writes here.
	for entry, err := range iterateTxsFromHeight(db, height+1) {
		if err != nil {
			return nil, err
		}
		root, err = applyTrie(trieDB, root, entry.height, entry.txs)
		if err != nil {
			return nil, err
		}
	}

	return &State{
		db:                  db,
		trieDB:              trieDB,
		lastCommittedHeight: height,
		currentRoot:         root,
	}, nil
}

// readLastCommittedRoot returns the last committed trie root and height. If
// none have been written, it returns the empty root for the genesis block.
func readLastCommittedRoot(db database.KeyValueReader) (common.Hash, uint64, error) {
	height, err := database.GetUInt64(db, lastCommittedHeightKey)
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

// readCommittedRoot returns the trie root committed at height, or the empty
// root if height == 0.
func readCommittedRoot(db database.KeyValueReader, height uint64) (common.Hash, error) {
	if height == 0 {
		return types.EmptyRootHash, nil
	}
	root, err := database.GetID(db, trieMetadataHeightKey(height))
	if err != nil {
		return common.Hash{}, err
	}
	return common.Hash(root), nil
}

// txsAndHeight pairs a slice of accepted transactions with the block height
// they were accepted at.
type txsAndHeight struct {
	txs    []*tx.Tx
	height uint64
}

// iterateTxsFromHeight returns an iterator yielding (txs, height) entries
// starting at startHeight in ascending height order. Heights with no txs
// are skipped (the caller is expected to know that empty heights don't
// change the trie).
func iterateTxsFromHeight(db database.Iteratee, startHeight uint64) iter.Seq2[txsAndHeight, error] {
	return func(yield func(txsAndHeight, error) bool) {
		it := db.NewIteratorWithStartAndPrefix(
			prefixdb.PrefixKey(heightPrefix, database.PackUInt64(startHeight)),
			heightPrefix,
		)
		defer it.Release()

		for it.Next() {
			height, err := database.ParseUInt64(it.Key()[len(heightPrefix):])
			if err != nil {
				yield(txsAndHeight{}, err)
				return
			}

			txs, err := tx.ParseSlice(it.Value())
			if err != nil {
				yield(txsAndHeight{}, err)
				return
			}

			if !yield(txsAndHeight{txs: txs, height: height}, nil) {
				return
			}
		}

		if err := it.Error(); err != nil {
			yield(txsAndHeight{}, err)
		}
	}
}

// commitInterval is how often the trie is flushed to disk.
const commitInterval = 4096

// Apply persists the txs accepted at height: it applies their atomic
// operations to the trie, flushes the trie to disk if a commit interval is
// crossed, indexes the txs by ID and by height, advances the last-applied
// marker, and commits everything atomically.
//
// Apply must be called with contiguous monotonically-increasing heights.
func (s *State) Apply(height uint64, txs []*tx.Tx) error {
	sorted := slices.Clone(txs)
	slices.SortFunc(sorted, func(a, b *tx.Tx) int {
		return a.ID().Compare(b.ID())
	})

	var err error
	s.currentRoot, err = applyTrie(s.trieDB, s.currentRoot, height, sorted)
	if err != nil {
		return err
	}

	batch := s.db.NewBatch()
	for _, t := range sorted {
		if err := writeTxByID(batch, height, t); err != nil {
			return err
		}
	}
	if err := writeTxsByHeight(batch, height, sorted); err != nil {
		return err
	}
	if height%commitInterval == 0 {
		if err := writeCommittedRoot(batch, height, s.currentRoot); err != nil {
			return err
		}
		s.lastCommittedHeight = height
	}
	return batch.Write()
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
	if err := database.PutUInt64(db, lastCommittedHeightKey, height); err != nil {
		return fmt.Errorf("writing last committed height: %w", err)
	}
	if err := database.PutID(db, trieMetadataHeightKey(height), ids.ID(root)); err != nil {
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
// The corresponding root can be retrieved via [State.GetRoot].
func (s *State) LastCommitted() uint64 {
	return s.lastCommittedHeight
}

const trieKeyLength = state.TrieKeyLength

// applyTrie merges the atomic ops in sorted into the trie rooted at root,
// registers the new tip in trieDB, flushes it to disk if height is a commit
// boundary, and dereferences the prior tip. Returns the new root. sorted
// must be sorted by tx ID.
func applyTrie(trieDB *triedb.Database, root common.Hash, height uint64, sorted []*tx.Tx) (common.Hash, error) {
	tr, err := trie.New(trie.TrieID(root), trieDB)
	if err != nil {
		return common.Hash{}, fmt.Errorf("opening atomic trie at root %s: %w", root, err)
	}

	ops := make(map[ids.ID]*atomic.Requests)
	for _, t := range sorted {
		chainID, req, err := t.AtomicRequests()
		if err != nil {
			return common.Hash{}, err
		}
		if existing, ok := ops[chainID]; ok {
			existing.PutRequests = append(existing.PutRequests, req.PutRequests...)
			existing.RemoveRequests = append(existing.RemoveRequests, req.RemoveRequests...)
		} else {
			ops[chainID] = req
		}
	}
	for chainID, requests := range ops {
		valueBytes, err := c.Marshal(codecVersion, requests)
		if err != nil {
			return common.Hash{}, fmt.Errorf("marshaling atomic requests for chain %s: %w", chainID, err)
		}

		keyPacker := wrappers.Packer{Bytes: make([]byte, trieKeyLength)}
		keyPacker.PackLong(height)
		keyPacker.PackFixedBytes(chainID[:])
		if err := tr.Update(keyPacker.Bytes, valueBytes); err != nil {
			return common.Hash{}, fmt.Errorf("inserting trie key for chain %s: %w", chainID, err)
		}
	}

	newRoot, nodes, err := tr.Commit(false)
	if err != nil {
		return common.Hash{}, fmt.Errorf("committing in-memory trie at height %d: %w", height, err)
	}
	if nodes != nil {
		if err := trieDB.Update(newRoot, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil); err != nil {
			return common.Hash{}, fmt.Errorf("updating trieDB with new nodes: %w", err)
		}
	}
	if err := trieDB.Reference(newRoot, common.Hash{}); err != nil {
		return common.Hash{}, fmt.Errorf("referencing root %s: %w", newRoot, err)
	}

	// Memory cap for the in-memory trieDB between commits. Matches the
	// unexported state.atomicTrieMemoryCap.
	const (
		trieMemoryCap     = 64 * units.MiB
		trieMemoryCapTrim = trieMemoryCap - ethdb.IdealBatchSize
	)
	if _, nodeSize, _ := trieDB.Size(); nodeSize > trieMemoryCap {
		if err := trieDB.Cap(trieMemoryCapTrim); err != nil {
			return common.Hash{}, fmt.Errorf("capping atomic trie at root %s: %w", newRoot, err)
		}
	}
	if height%commitInterval == 0 {
		if err := trieDB.Commit(newRoot, false); err != nil {
			return common.Hash{}, fmt.Errorf("committing trieDB at height %d root %s: %w", height, newRoot, err)
		}
	}

	if err := trieDB.Dereference(root); err != nil {
		return common.Hash{}, fmt.Errorf("dereferencing prior root %s: %w", root, err)
	}
	return newRoot, nil
}
