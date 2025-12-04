// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/subnet-evm/sync/syncutils"
)

var (
	_ syncTask = (*mainTrieTask)(nil)
	_ syncTask = (*storageTrieTask)(nil)
)

type syncTask interface {
	// IterateLeafs should return an iterator over
	// trie leafs already persisted to disk for this
	// trie. Used for restoring progress in case of an
	// interrupted sync and for hashing segments.
	IterateLeafs(seek common.Hash) ethdb.Iterator

	// callbacks used to form a LeafSyncTask
	OnStart() (bool, error)
	OnLeafs(db ethdb.KeyValueWriter, keys, vals [][]byte) error
	OnFinish() error
}

type mainTrieTask struct {
	sync *stateSync
}

func NewMainTrieTask(sync *stateSync) syncTask {
	return &mainTrieTask{
		sync: sync,
	}
}

func (m *mainTrieTask) IterateLeafs(seek common.Hash) ethdb.Iterator {
	snapshot := m.sync.snapshot
	return &syncutils.AccountIterator{AccountIterator: snapshot.AccountIterator(seek)}
}

// OnStart always returns false since the main trie task cannot be skipped.
func (*mainTrieTask) OnStart() (bool, error) {
	return false, nil
}

func (m *mainTrieTask) OnFinish() error {
	return m.sync.onMainTrieFinished()
}

func (m *mainTrieTask) OnLeafs(db ethdb.KeyValueWriter, keys, vals [][]byte) error {
	codeHashes := make([]common.Hash, 0)
	// loop over the keys, decode them as accounts, then check for any
	// storage or code we need to sync as well.
	for i, key := range keys {
		var acc types.StateAccount
		accountHash := common.BytesToHash(key)
		if err := rlp.DecodeBytes(vals[i], &acc); err != nil {
			return fmt.Errorf("could not decode main trie as account, key=%s, valueLen=%d, err=%w", accountHash, len(vals[i]), err)
		}

		// persist the account data
		writeAccountSnapshot(db, accountHash, acc)

		// check if this account has storage root that we need to fetch
		if acc.Root != (common.Hash{}) && acc.Root != types.EmptyRootHash {
			if err := m.sync.trieQueue.RegisterStorageTrie(acc.Root, accountHash); err != nil {
				return err
			}
		}

		// check if this account has code and add it to codeHashes to fetch
		// at the end of this loop.
		codeHash := common.BytesToHash(acc.CodeHash)
		if codeHash != (common.Hash{}) && codeHash != types.EmptyCodeHash {
			codeHashes = append(codeHashes, codeHash)
		}
	}
	// Add collected code hashes to the code syncer.
	return m.sync.codeSyncer.addCode(codeHashes)
}

type storageTrieTask struct {
	sync     *stateSync
	root     common.Hash
	accounts []common.Hash
}

func NewStorageTrieTask(sync *stateSync, root common.Hash, accounts []common.Hash) syncTask {
	return &storageTrieTask{
		sync:     sync,
		root:     root,
		accounts: accounts,
	}
}

func (s *storageTrieTask) IterateLeafs(seek common.Hash) ethdb.Iterator {
	snapshot := s.sync.snapshot
	it, _ := snapshot.StorageIterator(s.accounts[0], seek)
	return &syncutils.StorageIterator{StorageIterator: it}
}

func (s *storageTrieTask) OnStart() (bool, error) {
	// check if this storage root is on disk
	var firstAccount common.Hash
	if len(s.accounts) > 0 {
		firstAccount = s.accounts[0]
	}
	storageTrie, err := trie.New(trie.StorageTrieID(s.sync.root, s.root, firstAccount), s.sync.trieDB)
	if err != nil {
		return false, nil //nolint:nilerr // the storage trie does not exist, so it should be rerequested
	}

	// If the storage trie is already on disk, we only need to populate the storage snapshot for [accountHash]
	// with the trie contents. There is no need to re-sync the trie, since it is already present.
	for _, account := range s.accounts {
		if err := writeAccountStorageSnapshotFromTrie(s.sync.db.NewBatch(), s.sync.batchSize, account, storageTrie); err != nil {
			// If the storage trie cannot be iterated (due to an incomplete trie from pruning this storage trie in the past)
			// then we re-sync it here. Therefore, this error is not fatal and we can safely continue here.
			log.Info("could not populate storage snapshot from trie with existing root, syncing from peers instead", "account", account, "root", s.root, "err", err)
			return false, nil
		}
	}

	// Populating the snapshot from the existing storage trie succeeded,
	// return true to skip this task.
	return true, s.sync.onStorageTrieFinished(s.root)
}

func (s *storageTrieTask) OnFinish() error {
	return s.sync.onStorageTrieFinished(s.root)
}

func (s *storageTrieTask) OnLeafs(db ethdb.KeyValueWriter, keys, vals [][]byte) error {
	// persists the trie leafs to the snapshot for all accounts associated with this root
	for _, account := range s.accounts {
		for i, key := range keys {
			rawdb.WriteStorageSnapshot(db, account, common.BytesToHash(key), vals[i])
		}
	}
	return nil
}
