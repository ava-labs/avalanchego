// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/trie/trienode"
)

type StorageTrie struct {
	*AccountTrie
	storageRoot common.Hash
}

// `NewStorageTrie` returns a wrapper around an `AccountTrie` since Firewood
// does not require a separate storage trie. All changes are managed by the account trie.
func NewStorageTrie(accountTrie *AccountTrie, storageRoot common.Hash) (*StorageTrie, error) {
	return &StorageTrie{
		AccountTrie: accountTrie,
		storageRoot: storageRoot,
	}, nil
}

// Actual commit is handled by the account trie.
// Return the old storage root as if there was no change - we don't want to use the
// actual account trie hash and nodeset here.
func (s *StorageTrie) Commit(collectLeaf bool) (common.Hash, *trienode.NodeSet, error) {
	return s.storageRoot, nil, nil
}

// Firewood doesn't require tracking storage roots inside of an account.
func (s *StorageTrie) Hash() common.Hash {
	return s.storageRoot // only used in statedb to populate a `StateAccount`
}

// Copy should never be called on a storage trie, as it is just a wrapper around the account trie.
// Each storage trie should be re-opened with the account trie separately.
func (s *StorageTrie) Copy() *StorageTrie {
	return nil
}
