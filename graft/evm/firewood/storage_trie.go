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
	*baseTrie
}

// newStorageTrie returns a wrapper around a [baseTrie] since Firewood
// does not require a separate storage trie. All changes are tracked by the base
// trie.
func newStorageTrie(base *baseTrie) *storageTrie {
	return &storageTrie{
		baseTrie: base,
	}
}

// Commit is a no-op for storage tries, as all changes are tracked by the base trie.
// It always returns a nil NodeSet and zero hash.
func (*storageTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	return common.Hash{}, nil, nil
}

// Hash returns an empty hash, as the storage roots are managed internally to Firewood.
func (*storageTrie) Hash() common.Hash {
	return common.Hash{}
}

// Copy returns nil, as storage tries do not need to be copied separately.
// All usage of a copied storage trie should first ensure it is non-nil.
func (*storageTrie) Copy() *storageTrie {
	return nil
}
