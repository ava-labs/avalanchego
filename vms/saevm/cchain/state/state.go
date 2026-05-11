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
	"github.com/ava-labs/libevm/triedb/hashdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
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

	applyPrefix  = prefixdb.MakePrefix([]byte("atomicRepoMetadataDB"))                 // [state.atomicRepoMetadataDBPrefix]
	lastApplyKey = prefixdb.PrefixKey(applyPrefix, []byte("maxIndexedAtomicTxHeight")) // [state.maxIndexedHeightKey]

	txIDPrefix   = prefixdb.MakePrefix([]byte("atomicTxDB"))       // [state.atomicTxIDDBPrefix]
	heightPrefix = prefixdb.MakePrefix([]byte("atomicHeightTxDB")) // [state.atomicHeightTxDBPrefix]
)

// State holds the accepted transactions and the atomic-request trie. It is not
// safe for concurrent use; callers must serialize calls to [State.Apply].
//
// When applying operations, shared memory is updated atomically with the state.
type State struct {
	snowCtx *snow.Context
	db      database.Database
	trieDB  *triedb.Database

	lastCommittedHeight uint64
	currentHeight       uint64
	currentRoot         common.Hash
}

// New initializes the state with db. On startup, the in-memory trie is rebuilt
// by replaying any applied but not yet committed transactions.
func New(snowCtx *snow.Context, db database.Database) (*State, error) {
	root, committedHeight, err := readLastCommitted(db)
	if err != nil {
		return nil, err
	}
	currentHeight, err := readLastApplied(db)
	if err != nil {
		return nil, err
	}

	trieDB := triedb.NewDatabase(
		rawdb.NewDatabase(evmdb.New(prefixdb.NewNested(triePrefix, db))),
		&triedb.Config{
			HashDB: &hashdb.Config{
				CleanCacheSize: 64 * units.MiB,
			},
		},
	)

	for entry, err := range iterateTxs(db, committedHeight+1) {
		if err != nil {
			return nil, fmt.Errorf("replaying tx index: %w", err)
		}
		ops, err := atomicRequests(entry.txs)
		if err != nil {
			return nil, fmt.Errorf("constructing ops at height %d: %w", entry.height, err)
		}
		// Tx entries are written atomically with the committed root, so
		// [applyTrie] should never commit a new root to disk. The per-chain
		// requests were already applied to shared memory at original Apply
		// time; replay only rebuilds the in-memory trie tip.
		root, err = applyTrie(trieDB, root, entry.height, ops)
		if err != nil {
			return nil, fmt.Errorf("replaying height %d on root %s: %w", entry.height, root, err)
		}
	}

	return &State{
		snowCtx:             snowCtx,
		db:                  db,
		trieDB:              trieDB,
		lastCommittedHeight: committedHeight,
		currentHeight:       currentHeight,
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

// readLastApplied returns the most recent height passed to [State.Apply]. If
// none exists, it returns 0.
func readLastApplied(db database.KeyValueReader) (uint64, error) {
	switch height, err := database.GetUInt64(db, lastApplyKey); {
	case err == database.ErrNotFound:
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("getting last applied height: %w", err)
	default:
		return height, nil
	}
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

// atomicRequests groups the atomic requests from txs by chainID.
func atomicRequests(txs []*tx.Tx) (map[ids.ID]*atomic.Requests, error) {
	ops := make(map[ids.ID]*atomic.Requests)
	for _, t := range txs {
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

const commitInterval = 4096 // [config.defaultCommitInterval]

// Apply persists the txs accepted at height. It applies their atomic
// operations to the trie, indexes the txs by ID and by height, and on
// commit-interval boundaries flushes the trie to disk. All on-disk writes are
// committed atomically.
//
// Apply is a noop when height is not higher than the highest previously applied
// height.
func (s *State) Apply(height uint64, txs []*tx.Tx) error {
	if height <= s.currentHeight {
		// During restarts, it is expected for SAE to reprocess already-applied
		// heights. Shared memory is not safe to apply multiple times for the
		// same height, so we MUST skip these duplicate applications.
		s.snowCtx.Log.Debug("skipping writes for old height",
			zap.Uint64("height", height),
			zap.Uint64("currentHeight", s.currentHeight),
		)
		return nil
	}

	// The trie and the height-based index were initially generated from the
	// already existing id-based index. This resulted in data canonically sorted
	// by txID. To maintain this behavior, txs must be sorted by ID before
	// applying to the trie and writing to the height index.
	sorted := slices.Clone(txs)
	slices.SortFunc(sorted, func(a, b *tx.Tx) int {
		return a.ID().Compare(b.ID())
	})

	ops, err := atomicRequests(sorted)
	if err != nil {
		return fmt.Errorf("merging atomic ops: %w", err)
	}
	newRoot, err := applyTrie(s.trieDB, s.currentRoot, height, ops)
	if err != nil {
		return fmt.Errorf("applying trie on root %s: %w", s.currentRoot, err)
	}
	s.currentRoot = newRoot

	batch := s.db.NewBatch()
	for _, t := range sorted {
		if err := writeTxByID(batch, height, t); err != nil {
			return fmt.Errorf("writing tx %s: %w", t.ID(), err)
		}
	}
	if err := writeTxsByHeight(batch, height, sorted); err != nil {
		return fmt.Errorf("writing txs: %w", err)
	}
	if height%commitInterval == 0 {
		if err := writeCommittedRoot(batch, height, s.currentRoot); err != nil {
			return err
		}
		s.lastCommittedHeight = height
	}
	if err := database.PutUInt64(batch, lastApplyKey, height); err != nil {
		return fmt.Errorf("writing last applied height: %w", err)
	}
	s.currentHeight = height
	if err := s.snowCtx.SharedMemory.Apply(ops, batch); err != nil {
		return fmt.Errorf("applying shared memory: %w", err)
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
		return fmt.Errorf("writing committed root: %w", err)
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

// applyTrie writes the per-chain ops into the trie rooted at oldRoot, flushes
// the new trie to disk if height is a commit boundary, releases the prior
// root, and returns the new root.
func applyTrie(trieDB *triedb.Database, oldRoot common.Hash, height uint64, ops map[ids.ID]*atomic.Requests) (common.Hash, error) {
	tr, err := trie.New(trie.TrieID(oldRoot), trieDB)
	if err != nil {
		return common.Hash{}, fmt.Errorf("opening trie: %w", err)
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
		return common.Hash{}, fmt.Errorf("committing in-memory trie: %w", err)
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
		return common.Hash{}, fmt.Errorf("referencing new root %s: %w", newRoot, err)
	}

	const (
		memoryCap = 64 * units.MiB
		// Cap trims to memoryCap - IdealBatchSize rather than memoryCap to
		// avoid a flush on every call once the resident size sits near the cap.
		memoryCapTrim = memoryCap - ethdb.IdealBatchSize
	)
	if _, nodeSize, _ := trieDB.Size(); nodeSize > memoryCap {
		if err := trieDB.Cap(memoryCapTrim); err != nil {
			return common.Hash{}, fmt.Errorf("capping trie: %w", err)
		}
	}
	if height%commitInterval == 0 {
		if err := trieDB.Commit(newRoot, false); err != nil {
			return common.Hash{}, fmt.Errorf("committing trieDB at root %s: %w", newRoot, err)
		}
	}

	if err := trieDB.Dereference(oldRoot); err != nil {
		return common.Hash{}, fmt.Errorf("dereferencing old root: %w", err)
	}
	return newRoot, nil
}
