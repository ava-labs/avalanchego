// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/trie/trienode"
)

var _ state.Trie = (*storageTrie)(nil)

type storageTrie struct {
	*accountTrie
}

// `newStorageTrie` returns a wrapper around an `accountTrie` since Firewood
// does not require a separate storage trie. All changes are managed by the account trie.
func newStorageTrie(accountTrie *accountTrie) *storageTrie {
	return &storageTrie{
		accountTrie: accountTrie,
	}
}

// Actual commit is handled by the account trie.
// Return the old storage root as if there was no change since Firewood
// will manage the hash calculations without it.
// All changes are managed by the account trie.
func (*storageTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	return common.Hash{}, nil, nil
}

// Firewood doesn't require tracking storage roots inside of an account.
// They will be updated in place when hashing of the proposal takes place.
func (*storageTrie) Hash() common.Hash {
	return common.Hash{}
}

// Copy should never be called on a storage trie, as it is just a wrapper around the account trie.
// Each storage trie should be re-opened with the account trie separately.
func (*storageTrie) Copy() *storageTrie {
	return nil
}
