// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie/trienode"
)

var _ state.Trie = (*accountTrie)(nil)

// accountTrie implements [state.Trie] for managing account states.
// Although it fulfills the [state.Trie] interface, it has some important differences:
//  1. [accountTrie.Commit] is not used as expected in the state package. The `StorageTrie` doesn't return
//     values, and we thus rely on the `accountTrie`. Additionally, no [trienode.NodeSet] is
//     actually constructed, since Firewood manages nodes internally and the list of changes
//     is not needed externally.
//  2. The [accountTrie.Hash] method actually creates the [ffi.Proposal], since Firewood cannot calculate
//     the hash of the trie without committing it.
//
// Note this is not concurrent safe.
type accountTrie struct {
	*baseTrie

	fw         *TrieDB
	parentRoot common.Hash
}

func newAccountTrie(root common.Hash, db *TrieDB) (*accountTrie, error) {
	reader, err := db.Reader(root)
	if err != nil {
		return nil, err
	}
	return &accountTrie{
		baseTrie: &baseTrie{
			reader:     reader,
			dirtyKeys:  make(map[string][]byte),
			hasChanges: true, // Start with hasChanges true to allow computing the proposal hash
		},
		fw:         db,
		parentRoot: root,
	}, nil
}

// Hash returns the current hash of the state trie.
// This will create the necessary proposals to guarantee that the changes can
// later be committed. All new proposals will be tracked by the [TrieDB].
// If there are no changes since the last call, the cached root is returned.
// On error, the zero hash is returned.
func (a *accountTrie) Hash() common.Hash {
	hash, err := a.hash()
	if err != nil {
		log.Error("Failed to hash account trie", "error", err)
		return common.Hash{}
	}
	return hash
}

func (a *accountTrie) hash() (common.Hash, error) {
	// If we haven't already hashed, we need to do so.
	if a.hasChanges {
		root, err := a.fw.createProposals(a.parentRoot, a.updateOps)
		if err != nil {
			return common.Hash{}, err
		}
		a.root = root
		a.hasChanges = false // Avoid re-hashing until next update
	}
	return a.root, nil
}

// Commit returns the new root hash of the trie and an empty [trienode.NodeSet].
// The boolean input is ignored, as it is a relic of the StateTrie implementation.
// If the changes are not yet already tracked by the [TrieDB], they are created.
func (a *accountTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	// Get the hash of the trie.
	// Ensures all changes are tracked by the Database.
	hash, err := a.hash()
	if err != nil {
		return common.Hash{}, nil, err
	}

	set := trienode.NewNodeSet(common.Hash{})
	return hash, set, nil
}

// Copy creates a deep copy of the [accountTrie].
// The [database.Reader] is shared, since it is read-only.
func (a *accountTrie) Copy() *accountTrie {
	return &accountTrie{
		baseTrie:   a.baseTrie.copy(),
		fw:         a.fw,
		parentRoot: a.parentRoot,
	}
}
