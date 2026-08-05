// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"bytes"
	"slices"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state/snapshot"
)

// StaticPair is one key/value entry.
type StaticPair struct {
	K, V []byte
}

// StaticSnapshot is an in-memory [evmstate.SnapshotReader] for tests. Accounts
// and each Storage entry are sorted by K, accounts holding slim values. The root
// is ignored, as a real disk layer serves whatever it last flushed.
type StaticSnapshot struct {
	Accounts []StaticPair
	Storage  map[common.Hash][]StaticPair
	Err      error

	// One responder serves peers concurrently, so the record is guarded.
	mu    sync.Mutex
	reads []SnapshotRead
}

// SnapshotRead is one iterator request against a [StaticSnapshot].
type SnapshotRead struct {
	Root    common.Hash
	Account common.Hash // zero for the account trie
}

// DiskRoot is the zero hash, never a valid requested root, so a test can tell a
// disk read from a root-scoped one.
func (*StaticSnapshot) DiskRoot() common.Hash { return common.Hash{} }

// Reads returns every iterator opened, naming the layer each read.
func (s *StaticSnapshot) Reads() []SnapshotRead {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.reads)
}

func (s *StaticSnapshot) record(r SnapshotRead) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reads = append(s.reads, r)
}

func (s *StaticSnapshot) AccountIterator(root, seek common.Hash) (snapshot.AccountIterator, error) {
	s.record(SnapshotRead{Root: root})
	if s.Err != nil {
		return nil, s.Err
	}
	return &staticAccountIter{pairs: seekPairs(s.Accounts, seek), idx: -1}, nil
}

func (s *StaticSnapshot) StorageIterator(root, account, seek common.Hash) (snapshot.StorageIterator, error) {
	s.record(SnapshotRead{Root: root, Account: account})
	if s.Err != nil {
		return nil, s.Err
	}
	return &staticStorageIter{pairs: seekPairs(s.Storage[account], seek), idx: -1}, nil
}

// seekPairs drops the entries ordered before seek.
func seekPairs(pairs []StaticPair, seek common.Hash) []StaticPair {
	i := 0
	for i < len(pairs) && bytes.Compare(pairs[i].K, seek.Bytes()) < 0 {
		i++
	}
	return pairs[i:]
}

type staticAccountIter struct {
	pairs []StaticPair
	idx   int
}

func (it *staticAccountIter) Next() bool {
	it.idx++
	return it.idx < len(it.pairs)
}

func (it *staticAccountIter) Hash() common.Hash {
	if it.idx < 0 || it.idx >= len(it.pairs) {
		return common.Hash{}
	}
	return common.BytesToHash(it.pairs[it.idx].K)
}

func (it *staticAccountIter) Account() []byte { return it.pairs[it.idx].V }
func (*staticAccountIter) Error() error       { return nil }
func (*staticAccountIter) Release()           {}

type staticStorageIter struct {
	pairs []StaticPair
	idx   int
}

func (it *staticStorageIter) Next() bool {
	it.idx++
	return it.idx < len(it.pairs)
}

func (it *staticStorageIter) Hash() common.Hash {
	if it.idx < 0 || it.idx >= len(it.pairs) {
		return common.Hash{}
	}
	return common.BytesToHash(it.pairs[it.idx].K)
}

func (it *staticStorageIter) Slot() []byte { return it.pairs[it.idx].V }
func (*staticStorageIter) Error() error    { return nil }
func (*staticStorageIter) Release()        {}
