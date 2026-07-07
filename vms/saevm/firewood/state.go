// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package firewood implements a single-tier trie for the EVM in a database
// with built-in merkleization. See github.com/ava-labs/firewood for more
// details.
package firewood

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"go.uber.org/zap"

	// Need metrics registration from init function.
	// TODO(alarso16): Move metrics initialization after deletion of graft.
	graft "github.com/ava-labs/avalanchego/graft/evm/firewood"
)

var _ state.Database = (*stateAccessor)(nil)

func init() {
	var _ *graft.TrieDB // protect import for metrics registration
	state.RegisterDatabaseInterceptor(interceptor)
}

// interceptor takes any arbitrary [state.Database] and, if it is backed by
// Firewood, returns a wrapped version that will return a different
// [state.Trie] implementation.
func interceptor(db state.Database) state.Database {
	if tdb, ok := db.TrieDB().Backend().(*TrieDB); ok {
		return &stateAccessor{
			Database: db,
			triedb:   tdb,
		}
	}
	return db
}

type stateAccessor struct {
	state.Database
	triedb *TrieDB
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
	return newStorageTrie(accountTrie.baseTrie), nil
}

// CopyTrie returns a deep copy of the given trie.
// It can be altered by the caller.
func (s *stateAccessor) CopyTrie(t state.Trie) state.Trie {
	switch t := t.(type) {
	case *accountTrie:
		return t.Copy() // MUST NOT be nil
	case *storageTrie:
		// The storage trie wraps the base trie, and the [state.StateDB] will
		// reopen as necessary. It is impossible to obtain a reference to the
		// copied base trie, so nil is the best we can do.
		return nil
	default:
		s.triedb.log.Fatal("unknown trie type", zap.String("type", fmt.Sprintf("%T", t)))
		return nil
	}
}
