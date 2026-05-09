// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package state stores the C-Chain custom-transaction state: an index of
// accepted cross-chain transactions, an atomic-request trie, and the
// metadata needed to resume after restart.
//
// On-disk format must remain byte-compatible with
// graft/coreth/plugin/evm/atomic/state/ for the indices retained
// post-migration (atomicTxDB, atomicHeightTxDB, atomicTrieDB,
// atomicTrieMetaDB). New keys may be added; the obsolete
// atomicRepoMetadataDB prefix is no longer read or written.
package state

import (
	"errors"
	"iter"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

var errZeroCommitInterval = errors.New("commitInterval must be non-zero")

// State holds the C-Chain custom state. It is not safe for concurrent use.
//
// Mirrors graft/coreth/plugin/evm/atomic/state/atomic_backend.go's
// AtomicBackend, atomic_repository.go's AtomicRepository, and
// atomic_trie.go's AtomicTrie collapsed into one type.
type State struct {
	db                database.Database
	trie              *trieState
	lastAppliedHeight uint64
}

// New opens the state in db. lastAcceptedHeight is the highest block height
// the chain has accepted; commitInterval is how often the trie is flushed
// to disk (the old code used 4096).
func New(db database.Database, lastAcceptedHeight, commitInterval uint64) (*State, error) {
	if commitInterval == 0 {
		return nil, errZeroCommitInterval
	}

	t, err := newTrieState(db, lastAcceptedHeight, commitInterval)
	if err != nil {
		return nil, err
	}

	applied, err := readLastAppliedHeight(db)
	if err != nil {
		return nil, err
	}

	return &State{
		db:                db,
		trie:              t,
		lastAppliedHeight: applied,
	}, nil
}

// WriteTxs indexes txs by ID and by block height into batch. The caller
// must commit batch to persist.
func (*State) WriteTxs(batch database.KeyValueWriter, height uint64, txs []*tx.Tx) error {
	return writeTxs(batch, height, txs)
}

// GetTxByID returns the tx with the given ID along with the block height it
// was accepted at.
func (s *State) GetTxByID(txID ids.ID) (*tx.Tx, uint64, error) {
	return readTxByID(s.db, txID)
}

// IterateTxsFromHeight yields blocks of accepted txs in ascending height
// order, starting at startHeight. Heights with no transactions are skipped.
func (s *State) IterateTxsFromHeight(startHeight uint64) iter.Seq2[TxsAndHeight, error] {
	return iterateTxsFromHeight(s.db, startHeight)
}

// Apply writes ops into the atomic trie at height. The committed-root
// metadata write (when height crosses a commit boundary) is added to batch;
// trie node writes go directly to the underlying database. Returns the new
// trie root.
//
// Mirrors AtomicBackend.InsertTxs followed by AtomicTrie.AcceptTrie.
func (s *State) Apply(
	batch database.KeyValueWriter,
	height uint64,
	ops map[ids.ID]*chainsatomic.Requests,
) (common.Hash, error) {
	return s.trie.apply(batch, height, ops)
}

// LastAccepted returns the in-memory tip of the trie: the root after the
// most recent Apply, plus the height of the most recent on-disk commit.
//
// The returned height does not advance with each Apply — it advances only
// when a height crosses commitInterval.
func (s *State) LastAccepted() (common.Hash, uint64) {
	return s.trie.lastAcceptedRoot, s.trie.lastCommittedHeight
}

// LastApplied returns the most recent height that has been applied to
// shared memory, or 0 if no apply has been recorded.
func (s *State) LastApplied() uint64 {
	return s.lastAppliedHeight
}

// RecordApplied records that operations through height have been applied to
// shared memory. The write is added to batch so the caller can commit it
// atomically with the shared-memory apply.
func (s *State) RecordApplied(batch database.KeyValueWriter, height uint64) error {
	if err := writeLastAppliedHeight(batch, height); err != nil {
		return err
	}
	s.lastAppliedHeight = height
	return nil
}
