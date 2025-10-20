// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"encoding/binary"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	// syncRootKey indicates the root of the main account trie currently being synced.
	syncRootKey = []byte("sync_root")
	// syncStorageTriesPrefix + trie root + account hash indicates a storage trie must be fetched for the account.
	syncStorageTriesPrefix = []byte("sync_storage")
	// syncSegmentsPrefix + trie root + 32-byte start key indicates the trie at root has a segment starting at the specified key.
	syncSegmentsPrefix = []byte("sync_segments")
	// CodeToFetchPrefix + code hash -> empty value tracks the outstanding code hashes we need to fetch.
	CodeToFetchPrefix = []byte("CP")

	// === State sync progress key lengths ===
	syncStorageTriesKeyLength = len(syncStorageTriesPrefix) + 2*common.HashLength
	syncSegmentsKeyLength     = len(syncSegmentsPrefix) + 2*common.HashLength
	codeToFetchKeyLength      = len(CodeToFetchPrefix) + common.HashLength

	// === State sync metadata ===
	syncPerformedPrefix = []byte("sync_performed")
	// syncPerformedKeyLength is the length of the key for the sync performed metadata key,
	// and is equal to [syncPerformedPrefix] + block number as uint64.
	syncPerformedKeyLength = len(syncPerformedPrefix) + wrappers.LongLen
)

// ReadSyncRoot reads the root corresponding to the main trie of an in-progress
// sync and returns common.Hash{} if no in-progress sync was found.
func ReadSyncRoot(db ethdb.KeyValueReader) (common.Hash, error) {
	ok, err := db.Has(syncRootKey)
	if err != nil {
		return common.Hash{}, err
	}
	if !ok {
		return common.Hash{}, database.ErrNotFound
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

// WriteCodeToFetch adds a marker that we need to fetch the code for `hash`.
func WriteCodeToFetch(db ethdb.KeyValueWriter, codeHash common.Hash) error {
	return db.Put(codeToFetchKey(codeHash), nil)
}

// DeleteCodeToFetch removes the marker that the code corresponding to `hash` needs to be fetched.
func DeleteCodeToFetch(db ethdb.KeyValueWriter, codeHash common.Hash) error {
	return db.Delete(codeToFetchKey(codeHash))
}

// NewCodeToFetchIterator returns a KeyLength iterator over all code
// hashes that are pending syncing. It is the caller's responsibility to
// parse the key and call Release on the returned iterator.
func NewCodeToFetchIterator(db ethdb.Iteratee) ethdb.Iterator {
	return rawdb.NewKeyLengthIterator(
		db.NewIterator(CodeToFetchPrefix, nil),
		codeToFetchKeyLength,
	)
}

func codeToFetchKey(codeHash common.Hash) []byte {
	codeToFetchKey := make([]byte, codeToFetchKeyLength)
	copy(codeToFetchKey, CodeToFetchPrefix)
	copy(codeToFetchKey[len(CodeToFetchPrefix):], codeHash[:])
	return codeToFetchKey
}

// NewSyncSegmentsIterator returns a KeyLength iterator over all trie segments
// added for root. It is the caller's responsibility to parse the key and call
// Release on the returned iterator.
func NewSyncSegmentsIterator(db ethdb.Iteratee, root common.Hash) ethdb.Iterator {
	segmentsPrefix := make([]byte, len(syncSegmentsPrefix)+common.HashLength)
	copy(segmentsPrefix, syncSegmentsPrefix)
	copy(segmentsPrefix[len(syncSegmentsPrefix):], root[:])

	return rawdb.NewKeyLengthIterator(
		db.NewIterator(segmentsPrefix, nil),
		syncSegmentsKeyLength,
	)
}

// WriteSyncSegment adds a trie segment for root at the given start position.
func WriteSyncSegment(db ethdb.KeyValueWriter, root common.Hash, start common.Hash) error {
	// packs root and account into a key for storage in db.
	bytes := make([]byte, syncSegmentsKeyLength)
	copy(bytes, syncSegmentsPrefix)
	copy(bytes[len(syncSegmentsPrefix):], root[:])
	copy(bytes[len(syncSegmentsPrefix)+common.HashLength:], start.Bytes())
	return db.Put(bytes, nil)
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

// ParseSyncSegmentKey returns the root and start position for a trie segment
// key returned from NewSyncSegmentsIterator.
func ParseSyncSegmentKey(keyBytes []byte) (common.Hash, []byte) {
	keyBytes = keyBytes[len(syncSegmentsPrefix):] // skip prefix
	root := common.BytesToHash(keyBytes[:common.HashLength])
	start := keyBytes[common.HashLength:]
	return root, start
}

// NewSyncStorageTriesIterator returns a KeyLength iterator over all storage tries
// added for syncing (beginning at seek). It is the caller's responsibility to parse
// the key and call Release on the returned iterator.
func NewSyncStorageTriesIterator(db ethdb.Iteratee, seek []byte) ethdb.Iterator {
	return rawdb.NewKeyLengthIterator(db.NewIterator(syncStorageTriesPrefix, seek), syncStorageTriesKeyLength)
}

// WriteSyncStorageTrie adds a storage trie for account (with the given root) to be synced.
func WriteSyncStorageTrie(db ethdb.KeyValueWriter, root common.Hash, account common.Hash) error {
	bytes := make([]byte, syncStorageTriesKeyLength)
	copy(bytes, syncStorageTriesPrefix)
	copy(bytes[len(syncStorageTriesPrefix):], root[:])
	copy(bytes[len(syncStorageTriesPrefix)+common.HashLength:], account[:])
	return db.Put(bytes, nil)
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

// ParseSyncStorageTrieKey returns the root and account for a storage trie
// key returned from NewSyncStorageTriesIterator. It assumes the key has the
// `syncStorageTriesPrefix` followed by a 32-byte root and 32-byte account hash,
// and panics if the key is shorter than len(syncStorageTriesPrefix)+2*common.HashLength.
func ParseSyncStorageTrieKey(keyBytes []byte) (common.Hash, common.Hash) {
	keyBytes = keyBytes[len(syncStorageTriesPrefix):] // skip prefix
	root := common.BytesToHash(keyBytes[:common.HashLength])
	account := common.BytesToHash(keyBytes[common.HashLength:])
	return root, account
}

// WriteSyncPerformed logs an entry in `db` indicating the VM state synced to `blockNumber`.
func WriteSyncPerformed(db ethdb.KeyValueWriter, blockNumber uint64) error {
	bytes := make([]byte, syncPerformedKeyLength)
	copy(bytes, syncPerformedPrefix)
	binary.BigEndian.PutUint64(bytes[len(syncPerformedPrefix):], blockNumber)
	return db.Put(bytes, nil)
}

// NewSyncPerformedIterator returns an iterator over all block numbers the VM
// has state synced to.
func NewSyncPerformedIterator(db ethdb.Iteratee) ethdb.Iterator {
	return rawdb.NewKeyLengthIterator(db.NewIterator(syncPerformedPrefix, nil), syncPerformedKeyLength)
}

// ParseSyncPerformedKey returns the block number from keys the iterator returned
// from NewSyncPerformedIterator. It assumes the key has the syncPerformedPrefix
// followed by an 8-byte big-endian block number, and panics if the key is shorter
// than len(syncPerformedPrefix)+wrappers.LongLen.
func ParseSyncPerformedKey(key []byte) uint64 {
	return binary.BigEndian.Uint64(key[len(syncPerformedPrefix):])
}

// GetLatestSyncPerformed returns the latest block number state synced performed to.
func GetLatestSyncPerformed(db ethdb.Iteratee) (uint64, error) {
	it := NewSyncPerformedIterator(db)
	defer it.Release()

	var latestSyncPerformed uint64
	for it.Next() {
		syncPerformed := ParseSyncPerformedKey(it.Key())
		if syncPerformed > latestSyncPerformed {
			latestSyncPerformed = syncPerformed
		}
	}
	return latestSyncPerformed, it.Error()
}

// clearPrefix removes all keys in db that begin with prefix and match an
// expected key length. `keyLen` must include the length of the prefix.
func clearPrefix(db ethdb.KeyValueStore, prefix []byte, keyLen int) error {
	it := db.NewIterator(prefix, nil)
	defer it.Release()

	batch := db.NewBatch()
	for it.Next() {
		key := it.Key()
		if len(key) != keyLen {
			continue
		}
		key = common.CopyBytes(key)

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
