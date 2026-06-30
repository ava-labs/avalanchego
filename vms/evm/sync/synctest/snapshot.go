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

// StaticSnapshot is an in-memory [evmstate.SnapshotProvider]. Each account's
// pairs ([common.Hash]{} is the account trie) must be sorted by key.
type StaticSnapshot struct {
	Pairs map[common.Hash][]StaticPair
}

// MirrorSnapshot returns a [StaticSnapshot] of the account trie.
// keys must be sorted.
func MirrorSnapshot(keys, vals [][]byte) *StaticSnapshot {
	pairs := make([]StaticPair, len(keys))
	for i := range keys {
		pairs[i] = StaticPair{K: keys[i], V: vals[i]}
	}
	return &StaticSnapshot{Pairs: map[common.Hash][]StaticPair{{}: pairs}}
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
}

func newKVIter(all []StaticPair, start []byte) *kvIter {
	i := 0
	for i < len(all) && bytes.Compare(all[i].K, start) < 0 {
		i++
	}
	return &kvIter{pairs: all[i:], idx: -1}
}

func (it *kvIter) Next() bool {
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

func (*kvIter) Error() error { return nil }
func (*kvIter) Release()     {}
