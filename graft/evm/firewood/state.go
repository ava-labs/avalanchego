// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
)

var _ state.Database = (*firewoodState)(nil)

type firewoodState struct {
	state.Database
	fw *TrieDB
}

func NewStateWrapper(db state.Database, fw *TrieDB) state.Database {
	return &firewoodState{
		Database: db,
		fw:       fw,
	}
}

// OpenTrie opens the main account trie.
func (db *firewoodState) OpenTrie(root common.Hash) (state.Trie, error) {
	return newAccountTrie(root, db.fw)
}

// OpenStorageTrie opens a wrapped version of the account trie.
//
//nolint:revive // removing names loses context.
func (*firewoodState) OpenStorageTrie(stateRoot common.Hash, addr common.Address, accountRoot common.Hash, self state.Trie) (state.Trie, error) {
	accountTrie, ok := self.(*accountTrie)
	if !ok {
		return nil, fmt.Errorf("invalid account trie type: %T", self)
	}
	return newStorageTrie(accountTrie)
}

// CopyTrie returns a deep copy of the given trie.
// It can be altered by the caller.
func (*firewoodState) CopyTrie(t state.Trie) state.Trie {
	switch t := t.(type) {
	case *accountTrie:
		return t.Copy()
	case *storageTrie:
		return nil // The storage trie just wraps the account trie, so we must re-open it separately.
	default:
		panic(fmt.Errorf("unknown trie type %T", t))
	}
}
