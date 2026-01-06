// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils"
)

// Holds intermediate nodes. That is, those without values.
// Changes to this database aren't written to [baseDB] until
// they're evicted from the [nodeCache] or Flush is called.
type intermediateNodeDB struct {
	bufferPool *utils.BytesPool

	// The underlying storage.
	// Keys written to [baseDB] are prefixed with [intermediateNodePrefix].
	baseDB database.Database

	// The write buffer contains nodes that have been changed but have not been written to disk.
	// Note that a call to Put may cause a node to be evicted
	// from the cache, which will call [OnEviction].
	// A non-nil error returned from Put is considered fatal.
	// Keys in [nodeCache] aren't prefixed with [intermediateNodePrefix].
	writeBuffer onEvictCache[Key, *node]

	// If a value is nil, the corresponding key isn't in the trie.
	nodeCache cache.Cacher[Key, *node]

	// the number of bytes to evict during an eviction batch
	evictionBatchSize int
	metrics           metrics
	tokenSize         int
	hasher            Hasher
}

func newIntermediateNodeDB(
	db database.Database,
	bufferPool *utils.BytesPool,
	metrics metrics,
	cacheSize int,
	writeBufferSize int,
	evictionBatchSize int,
	tokenSize int,
	hasher Hasher,
) *intermediateNodeDB {
	result := &intermediateNodeDB{
		metrics:           metrics,
		baseDB:            db,
		bufferPool:        bufferPool,
		evictionBatchSize: evictionBatchSize,
		tokenSize:         tokenSize,
		hasher:            hasher,
		nodeCache:         lru.NewSizedCache(cacheSize, cacheEntrySize),
	}
	result.writeBuffer = newOnEvictCache(
		writeBufferSize,
		cacheEntrySize,
		result.onEviction,
	)

	return result
}

// A non-nil error is considered fatal and closes [db.baseDB].
func (db *intermediateNodeDB) onEviction(key Key, n *node) error {
	writeBatch := db.baseDB.NewBatch()
	totalSize := cacheEntrySize(key, n)
	if err := db.addToBatch(writeBatch, key, n); err != nil {
		_ = db.baseDB.Close()
		return err
	}

	// Evict the oldest [evictionBatchSize] nodes from the cache
	// and write them to disk. We write a batch of them, rather than
	// just [n], so that we don't immediately evict and write another
	// node, because each time this method is called we do a disk write.
	// Evicts a total number of bytes, rather than a number of nodes
	for totalSize < db.evictionBatchSize {
		key, n, exists := db.writeBuffer.removeOldest()
		if !exists {
			// The cache is empty.
			break
		}
		totalSize += cacheEntrySize(key, n)
		if err := db.addToBatch(writeBatch, key, n); err != nil {
			_ = db.baseDB.Close()
			return err
		}
	}
	if err := writeBatch.Write(); err != nil {
		_ = db.baseDB.Close()
		return err
	}
	return nil
}

func (db *intermediateNodeDB) addToBatch(b database.KeyValueWriterDeleter, key Key, n *node) error {
	dbKey := db.constructDBKey(key)
	defer db.bufferPool.Put(dbKey)

	db.metrics.DatabaseNodeWrite()
	if n == nil {
		return b.Delete(*dbKey)
	}
	return b.Put(*dbKey, n.bytes())
}

func (db *intermediateNodeDB) Get(key Key) (*node, error) {
	if cachedValue, isCached := db.nodeCache.Get(key); isCached {
		db.metrics.IntermediateNodeCacheHit()
		if cachedValue == nil {
			return nil, database.ErrNotFound
		}
		return cachedValue, nil
	}
	if cachedValue, isCached := db.writeBuffer.Get(key); isCached {
		db.metrics.IntermediateNodeCacheHit()
		if cachedValue == nil {
			return nil, database.ErrNotFound
		}
		return cachedValue, nil
	}
	db.metrics.IntermediateNodeCacheMiss()

	dbKey := db.constructDBKey(key)
	defer db.bufferPool.Put(dbKey)

	db.metrics.DatabaseNodeRead()
	nodeBytes, err := db.baseDB.Get(*dbKey)
	if err != nil {
		return nil, err
	}

	return parseNode(db.hasher, key, nodeBytes)
}

// constructDBKey returns a key that can be used in [db.baseDB].
// We need to be able to differentiate between two keys of equal
// byte length but different bit length, so we add padding to differentiate.
// Additionally, we add a prefix indicating it is part of the intermediateNodeDB.
func (db *intermediateNodeDB) constructDBKey(key Key) *[]byte {
	if db.tokenSize == 8 {
		// For tokens of size byte, no padding is needed since byte
		// length == token length
		return addPrefixToKey(db.bufferPool, intermediateNodePrefix, key.Bytes())
	}

	var (
		prefixLen              = len(intermediateNodePrefix)
		prefixBitLen           = 8 * prefixLen
		dualIndex              = dualBitIndex(db.tokenSize)
		paddingByteValue  byte = 1 << dualIndex
		paddingSliceValue      = []byte{paddingByteValue}
		paddingKey             = Key{
			value:  byteSliceToString(paddingSliceValue),
			length: db.tokenSize,
		}
	)

	bufferPtr := db.bufferPool.Get(bytesNeeded(prefixBitLen + key.length + db.tokenSize))
	copy(*bufferPtr, intermediateNodePrefix)                          // add prefix
	copy((*bufferPtr)[prefixLen:], key.Bytes())                       // add key
	extendIntoBuffer(*bufferPtr, paddingKey, prefixBitLen+key.length) // add padding
	return bufferPtr
}

func (db *intermediateNodeDB) Put(key Key, n *node) error {
	db.nodeCache.Put(key, n)
	return db.writeBuffer.Put(key, n)
}

func (db *intermediateNodeDB) Flush() error {
	db.nodeCache.Flush()
	return db.writeBuffer.Flush()
}

func (db *intermediateNodeDB) Delete(key Key) error {
	db.nodeCache.Put(key, nil)
	return db.writeBuffer.Put(key, nil)
}

func (db *intermediateNodeDB) Clear() error {
	db.nodeCache.Flush()

	// Reset the buffer. Note we don't flush because that would cause us to
	// persist intermediate nodes we're about to delete.
	db.writeBuffer = newOnEvictCache(
		db.writeBuffer.maxSize,
		db.writeBuffer.size,
		db.writeBuffer.onEviction,
	)
	return database.AtomicClearPrefix(db.baseDB, db.baseDB, intermediateNodePrefix)
}
