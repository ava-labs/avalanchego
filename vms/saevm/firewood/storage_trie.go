// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie/trienode"
)

var _ state.Trie = (*storageTrie)(nil)

type storageTrie struct {
	*baseTrie
	addr           common.Address
	accountRoot    common.Hash
	storageChanged bool
}

// newStorageTrie returns a wrapper around a [baseTrie] since Firewood
// does not require a separate storage trie. All changes are tracked by the base
// trie.
func newStorageTrie(base *baseTrie, addr common.Address, accountRoot common.Hash) *storageTrie {
	return &storageTrie{
		baseTrie:    base,
		addr:        addr,
		accountRoot: accountRoot,
	}
}

// clearStaleStorageBeforeWrite clears prior-incarnation storage before the
// first storage mutation for an account that StateDB considers storage-empty.
func (s *storageTrie) clearStaleStorageBeforeWrite() error {
	if s.storageChanged || s.accountRoot != types.EmptyRootHash {
		return nil
	}
	return s.clearStaleStorage(s.addr)
}

// UpdateStorage replaces or creates the value associated with a storage key for
// this account.
func (s *storageTrie) UpdateStorage(_ common.Address, key []byte, value []byte) error {
	if err := s.clearStaleStorageBeforeWrite(); err != nil {
		return err
	}
	if err := s.baseTrie.UpdateStorage(s.addr, key, value); err != nil {
		return err
	}
	s.storageChanged = true
	return nil
}

// DeleteStorage removes the value associated with a storage key for this account.
func (s *storageTrie) DeleteStorage(_ common.Address, key []byte) error {
	if err := s.clearStaleStorageBeforeWrite(); err != nil {
		return err
	}
	if err := s.baseTrie.DeleteStorage(s.addr, key); err != nil {
		return err
	}
	s.storageChanged = true
	return nil
}

// Commit is a no-op for storage tries, as all changes are tracked by the base trie.
// It always returns a nil NodeSet.
func (s *storageTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	return s.Hash(), nil, nil
}

// Hash returns the storage root that should be written to the account.
//
// If storage was not mutated, the original account root is preserved. If
// storage was mutated, Firewood stores the slots in its flat keyspace and uses
// the zero hash as the account's storage-root sentinel. This MUST NOT be
// [types.EmptyRootHash], because [baseTrie.clearStaleStorage] relies on that
// root to mean "StateDB believes this account has no storage".
func (s *storageTrie) Hash() common.Hash {
	if !s.storageChanged {
		return s.accountRoot
	}
	return common.Hash{}
}

// Copy returns nil, as storage tries do not need to be copied separately.
// All usage of a copied storage trie should first ensure it is non-nil.
func (*storageTrie) Copy() *storageTrie {
	return nil
}
