// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package state stores the C-Chain custom-transaction state: an index of
// accepted cross-chain transactions, an atomic-request trie, and the
// metadata needed to resume after restart.
package state

import (
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/triedb"

	// Imported for [state.AtomicBackend] comment resolution.
	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

// commitInterval is how often the trie is flushed to disk.
const commitInterval uint64 = 4096

// State holds the C-Chain custom state. It is not safe for concurrent use.
//
// Collapses [state.AtomicBackend], [state.AtomicRepository], and
// [state.AtomicTrie] into a single type.
type State struct {
	db database.Database

	trieDB              *triedb.Database
	lastAcceptedRoot    common.Hash // in-memory tip
	lastCommittedHeight uint64      // height of most recent on-disk commit

	lastAppliedHeight uint64
}

// New opens the state in db. On startup, the in-memory trie is rebuilt by
// replaying the accepted-tx index from the last committed height up to the
// last applied height.
func New(db database.Database) (*State, error) {
	lastApplied, err := readLastAppliedHeight(db)
	if err != nil {
		return nil, err
	}

	root, committedHeight, err := readLastCommittedRoot(db)
	if err != nil {
		return nil, err
	}

	s := &State{
		db:                  db,
		trieDB:              newTrieDB(db),
		lastAcceptedRoot:    root,
		lastCommittedHeight: committedHeight,
		lastAppliedHeight:   lastApplied,
	}
	if err := s.replay(); err != nil {
		return nil, err
	}
	return s, nil
}

// replay rebuilds the in-memory trie tip by re-applying every block of
// accepted txs in (lastCommittedHeight, lastAppliedHeight]. Empty blocks
// are skipped because they don't change the trie.
//
// Mirrors [state.AtomicBackend.initialize], but reads only from the tx
// index; the metadata about indexed-but-not-applied heights tracked by
// [state.AtomicBackend] is gone.
func (s *State) replay() error {
	batch := s.db.NewBatch()
	for entry, err := range iterateTxsFromHeight(s.db, s.lastCommittedHeight+1) {
		if err != nil {
			return err
		}
		if entry.height > s.lastAppliedHeight {
			break
		}
		ops, err := mergeAtomicOps(entry.txs)
		if err != nil {
			return err
		}
		if err := s.applyTrie(batch, entry.height, ops); err != nil {
			return err
		}
	}
	return batch.Write()
}

// Apply persists the txs accepted at height: it indexes them by ID and by
// height, applies their atomic operations to the trie, advances the
// last-applied marker, and commits everything atomically.
//
// Apply must be called with monotonically increasing heights, but heights
// don't have to be contiguous — empty blocks can be skipped.
func (s *State) Apply(height uint64, txs []*tx.Tx) error {
	// Sort once so the tx index and the op merge see the same order; bytes
	// written here only match those written by [state.AtomicRepository.write]
	// followed by [state.mergeAtomicOps] when both sides use the same order.
	sorted := slices.Clone(txs)
	slices.SortFunc(sorted, func(a, b *tx.Tx) int {
		return a.ID().Compare(b.ID())
	})

	batch := s.db.NewBatch()

	if err := writeSortedTxs(batch, height, sorted); err != nil {
		return err
	}

	ops, err := mergeAtomicOps(sorted)
	if err != nil {
		return err
	}
	if err := s.applyTrie(batch, height, ops); err != nil {
		return err
	}

	if err := writeLastAppliedHeight(batch, height); err != nil {
		return err
	}

	if err := batch.Write(); err != nil {
		return err
	}
	s.lastAppliedHeight = height
	return nil
}

// GetTx returns the tx with the given ID along with the block height it was
// accepted at.
func (s *State) GetTx(txID ids.ID) (*tx.Tx, uint64, error) {
	return readTxByID(s.db, txID)
}

// Mirrors [state.AtomicTrie.Root].

// GetRoot returns the atomic trie root committed at height. Only multiples
// of the commit interval (and height 0, which returns the empty root) have
// committed roots; intermediate heights return [database.ErrNotFound].
func (s *State) GetRoot(height uint64) (common.Hash, error) {
	return readCommittedRoot(s.db, height)
}

// Mirrors [state.AtomicTrie.LastCommitted], dropping the root from the
// return tuple since callers can fetch it via [State.GetRoot].

// LastCommitted returns the height of the most recent on-disk trie commit.
// The corresponding root can be retrieved via [State.GetRoot].
func (s *State) LastCommitted() uint64 {
	return s.lastCommittedHeight
}

// mergeAtomicOps groups per-tx atomic requests by destination chainID. The
// input must be sorted by tx ID for deterministic merge order.
//
// Mirrors [state.mergeAtomicOps].
func mergeAtomicOps(sorted []*tx.Tx) (map[ids.ID]*atomic.Requests, error) {
	if len(sorted) == 0 {
		return nil, nil
	}
	out := make(map[ids.ID]*atomic.Requests)
	for _, tx := range sorted {
		chainID, req, err := tx.AtomicRequests()
		if err != nil {
			return nil, err
		}
		if existing, ok := out[chainID]; ok {
			existing.PutRequests = append(existing.PutRequests, req.PutRequests...)
			existing.RemoveRequests = append(existing.RemoveRequests, req.RemoveRequests...)
		} else {
			out[chainID] = req
		}
	}
	return out, nil
}
