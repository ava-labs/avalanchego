// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie/trienode"

	_ "github.com/ava-labs/firewood-go-ethhash/ffi" // comment resolution
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

// Commit is a no-op for storage tries, as all changes are tracked by the base
// trie.  It always returns a nil NodeSet and zero hash. See [baseTrie] for more
// info on committing changes.
//
// The nil nodeset is not merged in [state.StateDB.Commit] and the merged
// nodeset is ignored by [TrieDB.Update], since all changes are tracked by the
// [ffi.Proposal]. The boolean input was intended to indicate whether to add
// the values as a leaf in the nodeset (corresponding to whether the caller
// expects this to be an account trie or not).
func (*storageTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	return common.Hash{}, nil, nil
}

// Hash returns an empty hash, as the storage roots are managed internally to
// Firewood. See [baseTrie.UpdateAccount] - this isn't used during hashing.
//
// This does affect [state.StateDB.GetStorageRoot], but this is unused outside
// debug APIs that SAE doesn't support.
func (*storageTrie) Hash() common.Hash {
	return common.Hash{}
}

// Prove writes the inclusion or exclusion proof for the already hashed key to
// the provided writer.
//
// TODO(alarso16): Implement.
func (*storageTrie) Prove([]byte, ethdb.KeyValueWriter) error {
	return errProveNotImplemented
}
