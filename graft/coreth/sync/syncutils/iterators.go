// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncutils

import (
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/ethdb"
)

var (
	_ ethdb.Iterator = &AccountIterator{}
	_ ethdb.Iterator = &StorageIterator{}
)

// AccountIterator wraps a [snapshot.AccountIterator] to conform to [ethdb.Iterator]
// accounts will be returned in consensus (FullRLP) format for compatibility with trie data.
type AccountIterator struct {
	snapshot.AccountIterator
	err error
	val []byte
}

func (it *AccountIterator) Next() bool {
	if it.err != nil {
		return false
	}
	for it.AccountIterator.Next() {
		it.val, it.err = snapshot.FullAccountRLP(it.Account())
		return it.err == nil
	}
	it.val = nil
	return false
}

func (it *AccountIterator) Key() []byte {
	if it.err != nil {
		return nil
	}
	return it.Hash().Bytes()
}

func (it *AccountIterator) Value() []byte {
	if it.err != nil {
		return nil
	}
	return it.val
}

func (it *AccountIterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.AccountIterator.Error()
}

// StorageIterator wraps a [snapshot.StorageIterator] to conform to [ethdb.Iterator]
type StorageIterator struct {
	snapshot.StorageIterator
}

func (it *StorageIterator) Key() []byte {
	return it.Hash().Bytes()
}

func (it *StorageIterator) Value() []byte {
	return it.Slot()
}
