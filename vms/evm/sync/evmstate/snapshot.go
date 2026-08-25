// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/types"
)

// Snapshot opens flat iterators over the snapshot leaves. The implementation
// does not need to guarantee anything about the state it serves. A state that
// happens to match a request speeds up the response but never changes it.
type Snapshot interface {
	AccountIterator(start common.Hash) (snapshot.AccountIterator, error)
	StorageIterator(account, start common.Hash) (snapshot.StorageIterator, error)
}

// SnapshotPointer is a type constraint for a pointer that implements
// [Snapshot].
//
// It can be used to avoid typed-nil interface panics.
type SnapshotPointer[V any] interface {
	Snapshot
	*V
}

var (
	_ trieSnapshot = (*accountSnapshot)(nil)
	_ trieSnapshot = (*storageSnapshot)(nil)
)

// trieSnapshot is a flat view over the leaves of one trie.
type trieSnapshot interface {
	// newIterator returns an iterator starting at start.
	//
	// If a nil error is returned, the iterator MUST be released.
	newIterator(start common.Hash) (iterator, error)
}

var (
	_ iterator = (*accountIterator)(nil)
	_ iterator = (*storageIterator)(nil)
)

// iterator walks snapshot leaves. Value returns the trie-encoded value at the
// cursor.
type iterator interface {
	snapshot.Iterator
	Value() ([]byte, error)
}

type accountSnapshot struct{ s Snapshot }

func (a accountSnapshot) newIterator(start common.Hash) (iterator, error) {
	it, err := a.s.AccountIterator(start)
	return accountIterator{it}, err
}

type accountIterator struct{ snapshot.AccountIterator }

func (it accountIterator) Value() ([]byte, error) {
	return types.FullAccountRLP(it.Account())
}

type storageSnapshot struct {
	s       Snapshot
	account common.Hash
}

func (s storageSnapshot) newIterator(start common.Hash) (iterator, error) {
	it, err := s.s.StorageIterator(s.account, start)
	return storageIterator{it}, err
}

type storageIterator struct{ snapshot.StorageIterator }

func (it storageIterator) Value() ([]byte, error) {
	return it.Slot(), nil
}
