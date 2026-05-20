// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"bytes"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
)

// StaticPair is one key/value entry.
type StaticPair struct {
	K, V []byte
}

// StaticSnapshot is an in-memory [evmstate.SnapshotProvider]. Pairs is
// keyed by account ([common.Hash]{} for the account trie) and each
// slice must be sorted ascending by K.
type StaticSnapshot struct {
	Pairs map[common.Hash][]StaticPair
}

func (s *StaticSnapshot) AccountIterator(start common.Hash) ethdb.Iterator {
	return newKVIter(s.Pairs[common.Hash{}], start.Bytes())
}

func (s *StaticSnapshot) StorageIterator(account, start common.Hash) ethdb.Iterator {
	return newKVIter(s.Pairs[account], start.Bytes())
}

type kvIter struct {
	pairs []StaticPair
	idx   int
	err   error
}

func newKVIter(all []StaticPair, start []byte) *kvIter {
	i := 0
	for i < len(all) && bytes.Compare(all[i].K, start) < 0 {
		i++
	}
	return &kvIter{pairs: all[i:], idx: -1}
}

func (it *kvIter) Next() bool {
	if it.err != nil {
		return false
	}
	it.idx++
	return it.idx < len(it.pairs)
}

func (it *kvIter) Key() []byte {
	if it.idx < 0 || it.idx >= len(it.pairs) {
		return nil
	}
	return it.pairs[it.idx].K
}

func (it *kvIter) Value() []byte {
	if it.idx < 0 || it.idx >= len(it.pairs) {
		return nil
	}
	return it.pairs[it.idx].V
}

func (it *kvIter) Error() error { return it.err }
func (*kvIter) Release()        {}
