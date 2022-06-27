// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ethereum/go-ethereum/common"
)

var (
	// keys: prefix + root + account
	// main trie is stored with common.Hash{} as the account
	syncProgressPrefix = []byte("sync_progress")
	syncProgressKeyLen = len(syncProgressPrefix) + common.HashLength + common.HashLength
)

func packKey(root common.Hash, account common.Hash) []byte {
	bytes := make([]byte, 0, syncProgressKeyLen)
	bytes = append(bytes, syncProgressPrefix...)
	bytes = append(bytes, root[:]...)
	bytes = append(bytes, account[:]...)
	return bytes
}

func unpackKey(bytes []byte) (common.Hash, common.Hash) {
	bytes = bytes[len(syncProgressPrefix):] // skip prefix
	root := common.BytesToHash(bytes[:common.HashLength])
	bytes = bytes[common.HashLength:]
	account := common.BytesToHash(bytes)
	return root, account
}

// loadProgress checks for a progress marker serialized to [db] and returns it if it matches [root].
// if the existing marker does not match [root] or one is not found, a new one is created, persisted, and returned.
// Additionally, any previous progress marker is wiped if this is the case.
func loadProgress(db ethdb.Database, root common.Hash) (*StateSyncProgress, error) {
	progress := &StateSyncProgress{
		StorageTries: make(map[common.Hash]*StorageTrieProgress),
	}

	// load from disk
	it := db.NewIterator(syncProgressPrefix, nil)
	defer it.Release()
	for it.Next() {
		if syncProgressKeyLen != len(it.Key()) {
			continue
		}
		root, account := unpackKey(it.Key())
		if account == (common.Hash{}) {
			progress.Root = root
			continue
		}
		if storageTrie, exists := progress.StorageTries[root]; exists {
			storageTrie.AdditionalAccounts = append(storageTrie.AdditionalAccounts, account)
		} else {
			progress.StorageTries[root] = &StorageTrieProgress{
				Account: account,
			}
		}
	}
	if err := it.Error(); err != nil {
		return nil, err
	}

	if progress.Root == root {
		// marker found on disk and matches
		return progress, nil
	} else if progress.Root != root {
		// marker found but does not match, delete it
		// first, clear account storage trie markers
		for root, storageTrie := range progress.StorageTries {
			if err := removeInProgressStorageTrie(db, root, storageTrie); err != nil {
				return nil, err
			}
		}
		// then clear the root marker itself
		if err := removeInProgressTrie(db, progress.Root, common.Hash{}); err != nil {
			return nil, err
		}
	}
	progress.Root = root
	progress.StorageTries = make(map[common.Hash]*StorageTrieProgress)
	return progress, addInProgressTrie(db, root, common.Hash{})
}

// addInProgressTrie adds a DB marker for an in-progress trie sync.
func addInProgressTrie(db ethdb.KeyValueWriter, root common.Hash, account common.Hash) error {
	return db.Put(packKey(root, account), []byte{0x1})
}

// removeInProgressStorageTrie removes progress markers for all accounts associated with storageTrie
func removeInProgressStorageTrie(db ethdb.KeyValueWriter, root common.Hash, storageTrie *StorageTrieProgress) error {
	for _, account := range storageTrie.AdditionalAccounts {
		if err := removeInProgressTrie(db, root, account); err != nil {
			return err
		}
	}
	return removeInProgressTrie(db, root, storageTrie.Account)
}

// removeInProgressTrie removes the DB marker for an in-progress trie sync.
func removeInProgressTrie(db ethdb.KeyValueWriter, root common.Hash, account common.Hash) error {
	return db.Delete(packKey(root, account))
}
