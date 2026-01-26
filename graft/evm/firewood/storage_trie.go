// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/trie"
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

// Commit is a no-op for storage tries, as all changes are managed by the account trie.
// It always returns a nil NodeSet and zero hash.
func (*storageTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	return common.Hash{}, nil, nil
}

// Hash returns an empty hash, as the storage roots are managed internally to Firewood.
func (*storageTrie) Hash() common.Hash {
	return common.Hash{}
}

var errStorageIteratorNotSupported = errors.New("storage trie does not support node iteration")

func (*storageTrie) NodeIterator([]byte) (trie.NodeIterator, error) {
	return nil, errStorageIteratorNotSupported
}

// Copy returns nil, as storage tries do not need to be copied separately.
// All usage of a copied storage trie should first ensure it is non-nil.
func (*storageTrie) Copy() *storageTrie {
	return nil
}
