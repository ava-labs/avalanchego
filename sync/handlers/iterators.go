// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/ethdb"
)

var (
	_ ethdb.Iterator = &accountIt{}
	_ ethdb.Iterator = &storageIt{}
)

// accountIt wraps a [snapshot.AccountIterator] to conform to [ethdb.Iterator]
// accounts will be returned in consensus (FullRLP) format for compatibility with trie data.
type accountIt struct {
	snapshot.AccountIterator
	err error
	val []byte
}

func (it *accountIt) Next() bool {
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

func (it *accountIt) Key() []byte {
	if it.err != nil {
		return nil
	}
	return it.Hash().Bytes()
}

func (it *accountIt) Value() []byte {
	if it.err != nil {
		return nil
	}
	return it.val
}

func (it *accountIt) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.AccountIterator.Error()
}

// storageIt wraps a [snapshot.StorageIterator] to conform to [ethdb.Iterator]
type storageIt struct {
	snapshot.StorageIterator
}

func (it *storageIt) Key() []byte {
	return it.Hash().Bytes()
}

func (it *storageIt) Value() []byte {
	return it.Slot()
}
