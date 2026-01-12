// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"encoding/binary"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/utils/wrappers"

	ethrawdb "github.com/ava-labs/libevm/core/rawdb"
)

// ReadSyncRoot reads the root corresponding to the main trie of an in-progress
// sync and returns common.Hash{} if no in-progress sync was found.
func ReadSyncRoot(db ethdb.KeyValueReader) (common.Hash, error) {
	has, err := db.Has(syncRootKey)
	if err != nil || !has {
		return common.Hash{}, err
	}
	root, err := db.Get(syncRootKey)
	if err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(root), nil
}

// WriteSyncRoot writes root as the root of the main trie of the in-progress sync.
func WriteSyncRoot(db ethdb.KeyValueWriter, root common.Hash) error {
	return db.Put(syncRootKey, root[:])
}

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
	return ethrawdb.NewKeyLengthIterator(
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

// NewSyncSegmentsIterator returns a KeyLength iterator over all trie segments
// added for root. It is the caller's responsibility to unpack the key and call
// Release on the returned iterator.
func NewSyncSegmentsIterator(db ethdb.Iteratee, root common.Hash) ethdb.Iterator {
	segmentsPrefix := make([]byte, len(syncSegmentsPrefix)+common.HashLength)
	copy(segmentsPrefix, syncSegmentsPrefix)
	copy(segmentsPrefix[len(syncSegmentsPrefix):], root[:])

	return ethrawdb.NewKeyLengthIterator(
		db.NewIterator(segmentsPrefix, nil),
		syncSegmentsKeyLength,
	)
}

// WriteSyncSegment adds a trie segment for root at the given start position.
func WriteSyncSegment(db ethdb.KeyValueWriter, root common.Hash, start common.Hash) error {
	return db.Put(packSyncSegmentKey(root, start), []byte{0x01})
}

// ClearSyncSegments removes segment markers for root from db
func ClearSyncSegments(db ethdb.KeyValueStore, root common.Hash) error {
	segmentsPrefix := make([]byte, len(syncSegmentsPrefix)+common.HashLength)
	copy(segmentsPrefix, syncSegmentsPrefix)
	copy(segmentsPrefix[len(syncSegmentsPrefix):], root[:])
	return clearPrefix(db, segmentsPrefix, syncSegmentsKeyLength)
}

// ClearAllSyncSegments removes all segment markers from db
func ClearAllSyncSegments(db ethdb.KeyValueStore) error {
	return clearPrefix(db, syncSegmentsPrefix, syncSegmentsKeyLength)
}

// UnpackSyncSegmentKey returns the root and start position for a trie segment
// key returned from NewSyncSegmentsIterator.
func UnpackSyncSegmentKey(keyBytes []byte) (common.Hash, []byte) {
	keyBytes = keyBytes[len(syncSegmentsPrefix):] // skip prefix
	root := common.BytesToHash(keyBytes[:common.HashLength])
	start := keyBytes[common.HashLength:]
	return root, start
}

// packSyncSegmentKey packs root and account into a key for storage in db.
func packSyncSegmentKey(root common.Hash, start common.Hash) []byte {
	bytes := make([]byte, syncSegmentsKeyLength)
	copy(bytes, syncSegmentsPrefix)
	copy(bytes[len(syncSegmentsPrefix):], root[:])
	copy(bytes[len(syncSegmentsPrefix)+common.HashLength:], start.Bytes())
	return bytes
}

// NewSyncStorageTriesIterator returns a KeyLength iterator over all storage tries
// added for syncing (beginning at seek). It is the caller's responsibility to unpack
// the key and call Release on the returned iterator.
func NewSyncStorageTriesIterator(db ethdb.Iteratee, seek []byte) ethdb.Iterator {
	return ethrawdb.NewKeyLengthIterator(db.NewIterator(syncStorageTriesPrefix, seek), syncStorageTriesKeyLength)
}

// WriteSyncStorageTrie adds a storage trie for account (with the given root) to be synced.
func WriteSyncStorageTrie(db ethdb.KeyValueWriter, root common.Hash, account common.Hash) error {
	return db.Put(packSyncStorageTrieKey(root, account), []byte{0x01})
}

// ClearSyncStorageTrie removes all storage trie accounts (with the given root) from db.
// Intended for use when the trie with root has completed syncing.
func ClearSyncStorageTrie(db ethdb.KeyValueStore, root common.Hash) error {
	accountsPrefix := make([]byte, len(syncStorageTriesPrefix)+common.HashLength)
	copy(accountsPrefix, syncStorageTriesPrefix)
	copy(accountsPrefix[len(syncStorageTriesPrefix):], root[:])
	return clearPrefix(db, accountsPrefix, syncStorageTriesKeyLength)
}

// ClearAllSyncStorageTries removes all storage tries added for syncing from db
func ClearAllSyncStorageTries(db ethdb.KeyValueStore) error {
	return clearPrefix(db, syncStorageTriesPrefix, syncStorageTriesKeyLength)
}

// UnpackSyncStorageTrieKey returns the root and account for a storage trie
// key returned from NewSyncStorageTriesIterator.
func UnpackSyncStorageTrieKey(keyBytes []byte) (common.Hash, common.Hash) {
	keyBytes = keyBytes[len(syncStorageTriesPrefix):] // skip prefix
	root := common.BytesToHash(keyBytes[:common.HashLength])
	account := common.BytesToHash(keyBytes[common.HashLength:])
	return root, account
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

// NewSyncPerformedIterator returns an iterator over all block numbers the VM
// has state synced to.
func NewSyncPerformedIterator(db ethdb.Iteratee) ethdb.Iterator {
	return ethrawdb.NewKeyLengthIterator(db.NewIterator(syncPerformedPrefix, nil), syncPerformedKeyLength)
}

// UnpackSyncPerformedKey returns the block number from keys the iterator returned
// from NewSyncPerformedIterator.
func UnpackSyncPerformedKey(key []byte) uint64 {
	return binary.BigEndian.Uint64(key[len(syncPerformedPrefix):])
}

// GetLatestSyncPerformed returns the latest block number state synced performed to.
func GetLatestSyncPerformed(db ethdb.Iteratee) uint64 {
	it := NewSyncPerformedIterator(db)
	defer it.Release()

	var latestSyncPerformed uint64
	for it.Next() {
		syncPerformed := UnpackSyncPerformedKey(it.Key())
		if syncPerformed > latestSyncPerformed {
			latestSyncPerformed = syncPerformed
		}
	}
	return latestSyncPerformed
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
