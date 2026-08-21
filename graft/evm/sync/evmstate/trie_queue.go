// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"errors"
	"iter"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

// trieQueue persists the storage tries discovered while syncing the account trie and
// hands them back grouped by root. Markers are durable, so an interrupted sync resumes.
type trieQueue struct {
	db ethdb.Database
}

// storageTrieRef is one queued storage trie and every account that references it.
type storageTrieRef struct {
	root common.Hash
	// accounts is never empty for a yielded ref.
	accounts []common.Hash
}

func newTrieQueue(db ethdb.Database) *trieQueue {
	return &trieQueue{db: db}
}

// clearIfRootDoesNotMatch wipes storage-trie and segment markers when the persisted
// root differs from root, then records root as the current target.
//
// It clears markers only. The snapshots are the caller's to wipe, see
// [NewHashDBSyncer], because leaves left there still count as resume progress.
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

// storageTries iterates the queued tries, grouped by root, in root order, yielding an
// error and stopping if the scan fails.
//
// The scan reopens per trie so a consumer that blocks between them, as the syncer does
// while waiting for a slot, never holds a database iterator open for the whole sync.
func (t *trieQueue) storageTries() iter.Seq2[storageTrieRef, error] {
	return func(yield func(storageTrieRef, error) bool) {
		var cursor []byte
		for {
			ref, next, err := t.trieAt(cursor)
			if err != nil {
				yield(storageTrieRef{}, err)
				return
			}
			if len(ref.accounts) == 0 {
				return
			}
			if !yield(ref, nil) || next == nil {
				return
			}
			cursor = next
		}
	}
}

// trieAt collects the accounts of the first trie at or after cursor, and where the next
// one starts, or nil if this was the last.
func (t *trieQueue) trieAt(cursor []byte) (storageTrieRef, []byte, error) {
	it := customrawdb.NewSyncStorageTriesIterator(t.db, cursor)
	defer it.Release()

	var ref storageTrieRef
	for it.Next() {
		root, account := customrawdb.ParseSyncStorageTrieKey(it.Key())
		if len(ref.accounts) == 0 {
			ref.root = root
		}
		if ref.root != root {
			// A new root means every account for this trie is collected.
			return ref, root[:], it.Error()
		}
		ref.accounts = append(ref.accounts, account)
	}
	return ref, nil, it.Error()
}
