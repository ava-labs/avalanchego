// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/coreth/triedb/firewood"
	"github.com/ava-labs/libevm/common"
)

var (
	_ Database = (*firewoodAccessorDb)(nil)
	_ Trie     = (*firewood.AccountTrie)(nil)
	_ Trie     = (*firewood.StorageTrie)(nil)
)

type firewoodAccessorDb struct {
	Database
	fw *firewood.Database
}

// OpenTrie opens the main account trie.
func (db *firewoodAccessorDb) OpenTrie(root common.Hash) (Trie, error) {
	return firewood.NewAccountTrie(root, db.fw)
}

// OpenStorageTrie opens a wrapped version of the account trie.
func (db *firewoodAccessorDb) OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, self Trie) (Trie, error) {
	accountTrie, ok := self.(*firewood.AccountTrie)
	if !ok {
		return nil, fmt.Errorf("Invalid account trie type: %T", self)
	}
	return firewood.NewStorageTrie(accountTrie, root)
}

// CopyTrie returns a deep copy of the given trie.
// It can be altered by the caller.
func (db *firewoodAccessorDb) CopyTrie(t Trie) Trie {
	switch t := t.(type) {
	case *firewood.AccountTrie:
		return t.Copy()
	case *firewood.StorageTrie:
		return nil // The storage trie just wraps the account trie, so we must re-open it separately.
	default:
		panic(fmt.Errorf("unknown trie type %T", t))
	}
}
