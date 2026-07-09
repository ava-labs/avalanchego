// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"bytes"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state/snapshot"
)

// StaticPair is one key/value entry.
type StaticPair struct {
	K, V []byte
}

// StaticSnapshot is an in-memory [evmstate.SnapshotReader] for tests. Accounts
// is sorted by K with slim values, the root is ignored. A set Err fails the
// iterator constructor.
type StaticSnapshot struct {
	Accounts []StaticPair
	Err      error
}

func (s *StaticSnapshot) AccountIterator(_, seek common.Hash) (snapshot.AccountIterator, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	i := 0
	for i < len(s.Accounts) && bytes.Compare(s.Accounts[i].K, seek.Bytes()) < 0 {
		i++
	}
	return &staticAccountIter{pairs: s.Accounts[i:], idx: -1}, nil
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
