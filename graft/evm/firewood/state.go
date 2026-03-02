// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
)

var _ state.Database = (*stateAccessor)(nil)

type stateAccessor struct {
	state.Database
	triedb *TrieDB
}

func NewStateAccessor(db state.Database, fw *TrieDB) state.Database {
	return &stateAccessor{
		Database: db,
		triedb:   fw,
	}
}

// OpenTrie opens the main account trie.
func (s *stateAccessor) OpenTrie(root common.Hash) (state.Trie, error) {
	return newAccountTrie(root, s.triedb)
}

// OpenStorageTrie opens a wrapped version of the account trie.
//
//nolint:revive // removing names loses context.
func (*stateAccessor) OpenStorageTrie(stateRoot common.Hash, addr common.Address, accountRoot common.Hash, self state.Trie) (state.Trie, error) {
	accountTrie, ok := self.(*accountTrie)
	if !ok {
		return nil, fmt.Errorf("invalid account trie type: %T", self)
	}
	return newStorageTrie(accountTrie), nil
}

// CopyTrie returns a deep copy of the given trie.
// It can be altered by the caller.
func (*stateAccessor) CopyTrie(t state.Trie) state.Trie {
	switch t := t.(type) {
	case *accountTrie:
		return t.Copy()
	case *storageTrie:
		return nil // The storage trie just wraps the account trie, so we must re-open it separately.
	default:
		panic(fmt.Errorf("unknown trie type %T", t))
	}
}
