// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"errors"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

// trieQueue persists storage tries discovered while syncing the account trie and
// hands them back grouped by root. Markers are durable so an interrupted sync resumes.
type trieQueue struct {
	db              ethdb.Database
	nextStorageRoot []byte
}

func newTrieQueue(db ethdb.Database) *trieQueue {
	return &trieQueue{db: db}
}

// clearIfRootDoesNotMatch wipes storage-trie and segment markers when the persisted
// root differs from root, so a new target never resumes against stale progress. It
// then records root as the current target.
func (t *trieQueue) clearIfRootDoesNotMatch(root common.Hash) error {
	persistedRoot, err := customrawdb.ReadSyncRoot(t.db)
	switch {
	case errors.Is(err, database.ErrNotFound):
		persistedRoot = common.Hash{}
	case err != nil:
		return err
	}

	if persistedRoot != (common.Hash{}) && persistedRoot != root {
		if err := customrawdb.ClearAllSyncStorageTries(t.db); err != nil {
			return err
		}
		if err := customrawdb.ClearAllSyncSegments(t.db); err != nil {
			return err
		}
	}

	return customrawdb.WriteSyncRoot(t.db, root)
}

// RegisterStorageTrie records that root's storage trie must be synced for account.
// Multiple accounts may share a root.
func (t *trieQueue) RegisterStorageTrie(root, account common.Hash) error {
	return customrawdb.WriteSyncStorageTrie(t.db, root, account)
}

// StorageTrieDone clears the markers for a storage trie that finished syncing.
func (t *trieQueue) StorageTrieDone(root common.Hash) error {
	return customrawdb.ClearSyncStorageTrie(t.db, root)
}

// countTries returns the number of distinct storage tries still to sync.
func (t *trieQueue) countTries() (int, error) {
	it := customrawdb.NewSyncStorageTriesIterator(t.db, nil)
	defer it.Release()

	var (
		root  common.Hash
		tries int
	)
	for it.Next() {
		nextRoot, _ := customrawdb.ParseSyncStorageTrieKey(it.Key())
		if root == (common.Hash{}) || root != nextRoot {
			root = nextRoot
			tries++
		}
	}
	return tries, it.Error()
}

// getNextTrie returns the next storage trie, every account referencing it, and
// whether more remain. A non-empty root guarantees a non-empty account slice.
func (t *trieQueue) getNextTrie() (common.Hash, []common.Hash, bool, error) {
	it := customrawdb.NewSyncStorageTriesIterator(t.db, t.nextStorageRoot)
	defer it.Release()

	var (
		root     common.Hash
		accounts []common.Hash
		more     bool
	)

	for it.Next() {
		nextRoot, nextAccount := customrawdb.ParseSyncStorageTrieKey(it.Key())
		if root == (common.Hash{}) {
			root = nextRoot
		}
		// A new root means every account for the current root is collected.
		// Remember where to resume.
		if root != nextRoot {
			t.nextStorageRoot = nextRoot[:]
			more = true
			break
		}
		accounts = append(accounts, nextAccount)
	}

	return root, accounts, more, it.Error()
}
