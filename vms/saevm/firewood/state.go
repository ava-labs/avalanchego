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

	_ "github.com/ava-labs/libevm/trie" // comment resolution

	// Need metrics registration from init function.
	// TODO(alarso16): Move metrics initialization after deletion of graft.
	graft "github.com/ava-labs/avalanchego/graft/evm/firewood"
)

var _ state.Database = (*stateAccessor)(nil)

func init() {
	var _ *graft.TrieDB // protect import for metrics registration
	state.RegisterDatabaseInterceptor(interceptor)
}

// interceptor overrides the results of [state.NewDatabase],
// [state.NewDatabaseWithConfig], and [state.NewDatabaseWithNodeDB].
//
// If db is backed by a firewood [TrieDB], interceptor wraps db in a
// [stateAccessor], whose tries are [accountTrie] and [storageTrie] rather than
// the normal [trie.StateTrie]. Otherwise db is returned unchanged.
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
	return newAccountTrie(root, s.triedb, nil /*ops*/)
}

// OpenStorageTrie opens a wrapped version of the account trie.
//
//nolint:revive // removing names loses context.
func (*stateAccessor) OpenStorageTrie(stateRoot common.Hash, addr common.Address, accountRoot common.Hash, tr state.Trie) (state.Trie, error) {
	accountTrie, ok := tr.(*accountTrie)
	if !ok {
		return nil, fmt.Errorf("invalid account trie type: %T", tr)
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
		// Before the [state.StateDB] accesses a storageTrie, it first checks to
		// see if it is nil. For nil storageTries, the stateDB will open it with
		// [stateAccessor.OpenStorageTrie]. By returning nil here, during
		// [state.StateDB.Copy], all the storageTries will get set to nil and
		// later reopened on top of the copied [accountTrie].
		//
		// There is no cleaner way to share the same baseTrie with all the
		// storageTries and the accountTrie.
		return nil
	default:
		s.triedb.log.Fatal("unknown trie type", zap.String("type", fmt.Sprintf("%T", t)))
		return nil
	}
}
