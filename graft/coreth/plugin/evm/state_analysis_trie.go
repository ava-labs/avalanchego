// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
)

// newStateTrie creates a new state trie for iteration
func newStateTrie(stateRoot common.Hash, trieDB *triedb.Database) (*trie.StateTrie, error) {
	return trie.NewStateTrie(trie.StateTrieID(stateRoot), trieDB)
}

// decodeAccount decodes an account from RLP bytes
func decodeAccount(data []byte) (*types.StateAccount, error) {
	var acc types.StateAccount
	if err := rlp.DecodeBytes(data, &acc); err != nil {
		return nil, err
	}
	return &acc, nil
}

// isAccountWithCode checks if account has code
func isAccountWithCode(acc *types.StateAccount) bool {
	return len(acc.CodeHash) > 0 && common.BytesToHash(acc.CodeHash) != types.EmptyCodeHash
}

// hasStorageRoot checks if account has storage
func hasStorageRoot(acc *types.StateAccount) bool {
	return acc.Root != types.EmptyRootHash
}

// getStorageRoot returns the storage root of an account
func getStorageRoot(acc *types.StateAccount) common.Hash {
	return acc.Root
}

// countStorageWithTrie counts storage slots using trie iterator
func countStorageWithTrie(trieDB *triedb.Database, stateRoot, accountHash, storageRoot common.Hash) (storageStats, error) {
	storageTrieID := trie.StorageTrieID(stateRoot, accountHash, storageRoot)
	storageTrie, err := trie.NewStateTrie(storageTrieID, trieDB)
	if err != nil {
		return storageStats{}, err
	}

	nodeIt, err := storageTrie.NodeIterator(nil)
	if err != nil {
		return storageStats{}, err
	}

	var stats storageStats
	for nodeIt.Next(true) {
		if nodeIt.Leaf() {
			stats.slots++
			stats.bytes += uint64(len(nodeIt.LeafBlob()))
		}
	}

	if err := nodeIt.Error(); err != nil {
		return stats, err
	}

	return stats, nil
}
