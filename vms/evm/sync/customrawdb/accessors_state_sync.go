// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"encoding/binary"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// AddCodeToFetch adds a marker that we need to fetch the code for `hash`.
func AddCodeToFetch(db ethdb.KeyValueWriter, hash common.Hash) {
	if err := db.Put(codeToFetchKey(hash), nil); err != nil {
		log.Crit("Failed to put code to fetch", "codeHash", hash, "err", err)
	}
}

// DeleteCodeToFetch removes the marker that the code corresponding to `hash` needs to be fetched.
func DeleteCodeToFetch(db ethdb.KeyValueWriter, hash common.Hash) {
	if err := db.Delete(codeToFetchKey(hash)); err != nil {
		log.Crit("Failed to delete code to fetch", "codeHash", hash, "err", err)
	}
}

// NewCodeToFetchIterator returns a KeyLength iterator over all code
// hashes that are pending syncing. It is the caller's responsibility to
// unpack the key and call Release on the returned iterator.
func NewCodeToFetchIterator(db ethdb.Iteratee) ethdb.Iterator {
	return rawdb.NewKeyLengthIterator(
		db.NewIterator(CodeToFetchPrefix, nil),
		codeToFetchKeyLength,
	)
}

func codeToFetchKey(hash common.Hash) []byte {
	codeToFetchKey := make([]byte, codeToFetchKeyLength)
	copy(codeToFetchKey, CodeToFetchPrefix)
	copy(codeToFetchKey[len(CodeToFetchPrefix):], hash[:])
	return codeToFetchKey
}

// WriteSyncSegment adds a trie segment for root at the given start position.
func WriteSyncSegment(db ethdb.KeyValueWriter, root common.Hash, start common.Hash) error {
	return db.Put(packSyncSegmentKey(root, start), []byte{0x01})
}

// ClearAllSyncSegments removes all segment markers from db
func ClearAllSyncSegments(db ethdb.KeyValueStore) error {
	return clearPrefix(db, syncSegmentsPrefix, syncSegmentsKeyLength)
}

// packSyncSegmentKey packs root and account into a key for storage in db.
func packSyncSegmentKey(root common.Hash, start common.Hash) []byte {
	bytes := make([]byte, syncSegmentsKeyLength)
	copy(bytes, syncSegmentsPrefix)
	copy(bytes[len(syncSegmentsPrefix):], root[:])
	copy(bytes[len(syncSegmentsPrefix)+common.HashLength:], start.Bytes())
	return bytes
}

// WriteSyncStorageTrie adds a storage trie for account (with the given root) to be synced.
func WriteSyncStorageTrie(db ethdb.KeyValueWriter, root common.Hash, account common.Hash) error {
	return db.Put(packSyncStorageTrieKey(root, account), []byte{0x01})
}

// packSyncStorageTrieKey packs root and account into a key for storage in db.
func packSyncStorageTrieKey(root common.Hash, account common.Hash) []byte {
	bytes := make([]byte, 0, syncStorageTriesKeyLength)
	bytes = append(bytes, syncStorageTriesPrefix...)
	bytes = append(bytes, root[:]...)
	bytes = append(bytes, account[:]...)
	return bytes
}

// WriteSyncPerformed logs an entry in `db` indicating the VM state synced to `blockNumber`.
func WriteSyncPerformed(db ethdb.KeyValueWriter, blockNumber uint64) error {
	syncPerformedPrefixLen := len(syncPerformedPrefix)
	bytes := make([]byte, syncPerformedPrefixLen+wrappers.LongLen)
	copy(bytes[:syncPerformedPrefixLen], syncPerformedPrefix)
	binary.BigEndian.PutUint64(bytes[syncPerformedPrefixLen:], blockNumber)
	return db.Put(bytes, []byte{0x01})
}

// clearPrefix removes all keys in db that begin with prefix and match an
// expected key length. `keyLen` must include the length of the prefix.
func clearPrefix(db ethdb.KeyValueStore, prefix []byte, keyLen int) error {
	it := db.NewIterator(prefix, nil)
	defer it.Release()

	batch := db.NewBatch()
	for it.Next() {
		key := common.CopyBytes(it.Key())
		if len(key) != keyLen {
			continue
		}
		if err := batch.Delete(key); err != nil {
			return err
		}
		if batch.ValueSize() > ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	return batch.Write()
}
