// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customrawdb"
)

// trieQueue persists storage trie roots with their associated
// accounts when [RegisterStorageTrie] is called. These are
// later returned from the [getNextTrie] method.
type trieQueue struct {
	db              ethdb.Database
	nextStorageRoot []byte
}

func NewTrieQueue(db ethdb.Database) *trieQueue {
	return &trieQueue{
		db: db,
	}
}

// clearIfRootDoesNotMatch clears progress and segment markers if
// the persisted root does not match the root we are syncing to.
func (t *trieQueue) clearIfRootDoesNotMatch(root common.Hash) error {
	persistedRoot, err := customrawdb.ReadSyncRoot(t.db)
	if err != nil {
		return err
	}
	if persistedRoot != (common.Hash{}) && persistedRoot != root {
		// if not resuming, clear all progress markers
		if err := customrawdb.ClearAllSyncStorageTries(t.db); err != nil {
			return err
		}
		if err := customrawdb.ClearAllSyncSegments(t.db); err != nil {
			return err
		}
	}

	return customrawdb.WriteSyncRoot(t.db, root)
}

// RegisterStorageTrie is called by the main trie's leaf handling callbacks
// It adds a key built as [syncProgressPrefix+root+account] to the database.
// getNextTrie iterates this prefix to find storage tries and accounts
// associated with them.
func (t *trieQueue) RegisterStorageTrie(root common.Hash, account common.Hash) error {
	return customrawdb.WriteSyncStorageTrie(t.db, root, account)
}

// StorageTrieDone is called when a storage trie has completed syncing.
// This removes any progress markers for the trie.
func (t *trieQueue) StorageTrieDone(root common.Hash) error {
	return customrawdb.ClearSyncStorageTrie(t.db, root)
}

// getNextTrie returns the next storage trie to sync, along with a slice
// of accounts that point to the returned storage trie.
// Returns true if there are more storage tries to sync and false otherwise.
// Note: if a non-nil root is returned, getNextTrie guarantees that there will be at least
// one account hash in the returned slice.
func (t *trieQueue) getNextTrie() (common.Hash, []common.Hash, bool, error) {
	it := customrawdb.NewSyncStorageTriesIterator(t.db, t.nextStorageRoot)
	defer it.Release()

	var (
		root     common.Hash
		accounts []common.Hash
		more     bool
	)

	// Iterate over the keys to find the next storage trie root and all of the account hashes that contain the same storage root.
	for it.Next() {
		// Unpack the state root and account hash from the current key
		nextRoot, nextAccount := customrawdb.UnpackSyncStorageTrieKey(it.Key())
		// Set the root for the first pass
		if root == (common.Hash{}) {
			root = nextRoot
		}
		// If the next root is different than the originally set root, then we've iterated over all of the account hashes that
		// have the same storage trie root. Set more to be true, since there is at least one more storage trie.
		if root != nextRoot {
			t.nextStorageRoot = nextRoot[:]
			more = true
			break
		}
		// If we found another account with the same root, add the accountHash.
		accounts = append(accounts, nextAccount)
	}

	return root, accounts, more, it.Error()
}

func (t *trieQueue) countTries() (int, error) {
	it := customrawdb.NewSyncStorageTriesIterator(t.db, nil)
	defer it.Release()

	var (
		root  common.Hash
		tries int
	)

	for it.Next() {
		nextRoot, _ := customrawdb.UnpackSyncStorageTrieKey(it.Key())
		if root == (common.Hash{}) || root != nextRoot {
			root = nextRoot
			tries++
		}
	}

	return tries, it.Error()
}
