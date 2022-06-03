// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
)

// writeAccountSnapshot stores the account represented by [acc] to the snapshot at [accHash], using
// SlimAccountRLP format (omitting empty code/storage).
func writeAccountSnapshot(db ethdb.KeyValueWriter, accHash common.Hash, acc types.StateAccount) {
	slimAccount := snapshot.SlimAccountRLP(acc.Nonce, acc.Balance, acc.Root, acc.CodeHash, acc.IsMultiCoin)
	rawdb.WriteAccountSnapshot(db, accHash, slimAccount)
}

// writeAccountStorageSnapshotFromTrie iterates the trie at [storageTrie] and copies all entries
// to the storage snapshot for [accountHash].
func writeAccountStorageSnapshotFromTrie(batch ethdb.Batch, batchSize int, accountHash common.Hash, storageTrie *trie.Trie) error {
	it := trie.NewIterator(storageTrie.NodeIterator(nil))
	for it.Next() {
		rawdb.WriteStorageSnapshot(batch, accountHash, common.BytesToHash(it.Key), it.Value)
		if batch.ValueSize() > batchSize {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}
	}
	if it.Err != nil {
		return it.Err
	}
	return batch.Write()
}

// copyStorageSnapshot iterates [db] to find all storage snapshot entries for [account] and copies them to the
// storage snapshot of each account in [dstAccounts]
// Note: assumes that the storage snapshot for [account] is already complete.
func copyStorageSnapshot(db ethdb.Iteratee, account common.Hash, batch ethdb.Batch, batchSize int, dstAccounts []common.Hash) error {
	prefixLen := len(rawdb.SnapshotStoragePrefix) + common.HashLength
	it := rawdb.IterateStorageSnapshots(db, account)
	defer it.Release()
	for it.Next() {
		keyLen := len(it.Key())
		if keyLen != prefixLen+common.HashLength {
			continue
		}
		key := common.BytesToHash(it.Key()[prefixLen:])
		for _, accnt := range dstAccounts {
			rawdb.WriteStorageSnapshot(batch, accnt, key, it.Value())
		}
		if batch.ValueSize() > batchSize {
			if err := batch.Write(); err != nil {
				return err
			}
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	return batch.Write()
}

// restoreMainTrieProgressFromSnapshot iterates the account snapshots from [db] and adds
// full RLP representations as leafs to the stack trie in [tr]. Also sets [tr.startsFrom]
// to the key that syncing can begin from.
func restoreMainTrieProgressFromSnapshot(db ethdb.Iteratee, tr *TrieProgress) error {
	var lastKey []byte
	prefixLen := len(rawdb.SnapshotAccountPrefix)
	it := rawdb.IterateAccountSnapshots(db)
	defer it.Release()
	for it.Next() {
		keyLen := len(it.Key())
		if keyLen != prefixLen+common.HashLength {
			continue
		}
		key := it.Key()[prefixLen:]
		fullAccount, err := snapshot.FullAccountRLP(it.Value())
		if err != nil {
			return fmt.Errorf("could not get full account from snapshot value: %w", err)
		}
		if err := tr.trie.TryUpdate(key, fullAccount); err != nil {
			return err
		}
		if tr.batch.ValueSize() > tr.batchSize {
			if err := tr.batch.Write(); err != nil {
				return err
			}
			tr.batch.Reset()
		}
		lastKey = key
	}
	if lastKey != nil {
		// since lastKey is already added to the stack trie,
		// we should start syncing from the next key.
		tr.startFrom = lastKey
		utils.IncrOne(tr.startFrom)
	}
	return it.Error()
}

// restoreStorageTrieProgressFromSnapshot iterates the account storage snapshots for
// [account] from [db] and adds key/value pairs as leafs to the stack trie in [tr].
// Also sets [tr.startsFrom] to the key that syncing can begin from.
func restoreStorageTrieProgressFromSnapshot(db ethdb.Iteratee, tr *TrieProgress, account common.Hash) error {
	var lastKey []byte
	prefixLen := len(rawdb.SnapshotStoragePrefix) + common.HashLength
	it := rawdb.IterateStorageSnapshots(db, account)
	defer it.Release()
	for it.Next() {
		keyLen := len(it.Key())
		if keyLen != prefixLen+common.HashLength {
			continue
		}
		key := it.Key()[prefixLen:]
		if err := tr.trie.TryUpdate(key, it.Value()); err != nil {
			return err
		}
		if tr.batch.ValueSize() > tr.batchSize {
			if err := tr.batch.Write(); err != nil {
				return err
			}
			tr.batch.Reset()
		}
		lastKey = key
	}
	if lastKey != nil {
		// since lastKey is already added to the stack trie,
		// we should start syncing from the next key.
		tr.startFrom = lastKey
		utils.IncrOne(tr.startFrom)
	}
	return it.Error()
}
