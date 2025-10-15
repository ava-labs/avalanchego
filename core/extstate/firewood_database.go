// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"

	"github.com/ava-labs/subnet-evm/triedb/firewood"
)

var (
	_ state.Database = (*firewoodAccessorDB)(nil)
	_ state.Trie     = (*firewood.AccountTrie)(nil)
	_ state.Trie     = (*firewood.StorageTrie)(nil)
)

type firewoodAccessorDB struct {
	state.Database
	fw *firewood.Database
}

// OpenTrie opens the main account trie.
func (db *firewoodAccessorDB) OpenTrie(root common.Hash) (state.Trie, error) {
	return firewood.NewAccountTrie(root, db.fw)
}

// OpenStorageTrie opens a wrapped version of the account trie.
//
//nolint:revive // removing names loses context.
func (*firewoodAccessorDB) OpenStorageTrie(stateRoot common.Hash, addr common.Address, accountRoot common.Hash, self state.Trie) (state.Trie, error) {
	accountTrie, ok := self.(*firewood.AccountTrie)
	if !ok {
		return nil, fmt.Errorf("invalid account trie type: %T", self)
	}
	return firewood.NewStorageTrie(accountTrie)
}

// CopyTrie returns a deep copy of the given trie.
// It can be altered by the caller.
func (*firewoodAccessorDB) CopyTrie(t state.Trie) state.Trie {
	switch t := t.(type) {
	case *firewood.AccountTrie:
		return t.Copy()
	case *firewood.StorageTrie:
		return nil // The storage trie just wraps the account trie, so we must re-open it separately.
	default:
		panic(fmt.Errorf("unknown trie type %T", t))
	}
}
