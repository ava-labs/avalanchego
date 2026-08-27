// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"bytes"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state/snapshot"
)

type Pair struct {
	K, V []byte
}

// Snapshot is an in-memory [evmstate.Snapshot] for tests. Accounts
// and each Storage entry are sorted by K, accounts holding slim values. The root
// is ignored, as a real disk layer serves whatever it last flushed.
type Snapshot struct {
	Accounts []Pair
	Storage  map[common.Hash][]Pair
	OpenErr  error
	IterErr  error
}

func (s *Snapshot) AccountIterator(start common.Hash) (snapshot.AccountIterator, error) {
	if s.OpenErr != nil {
		return nil, s.OpenErr
	}
	return &staticAccountIter{pairs: seekPairs(s.Accounts, start), idx: -1, err: s.IterErr}, nil
}

func (s *Snapshot) StorageIterator(account, start common.Hash) (snapshot.StorageIterator, error) {
	if s.OpenErr != nil {
		return nil, s.OpenErr
	}
	return &staticStorageIter{pairs: seekPairs(s.Storage[account], start), idx: -1, err: s.IterErr}, nil
}

func seekPairs(pairs []Pair, seek common.Hash) []Pair {
	i := 0
	for i < len(pairs) && bytes.Compare(pairs[i].K, seek.Bytes()) < 0 {
		i++
	}
	return pairs[i:]
}

type staticAccountIter struct {
	pairs []Pair
	idx   int
	err   error
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
func (it *staticAccountIter) Error() error    { return it.err }
func (*staticAccountIter) Release()           {}

type staticStorageIter struct {
	pairs []Pair
	idx   int
	err   error
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
func (it *staticStorageIter) Error() error { return it.err }
func (*staticStorageIter) Release()        {}
