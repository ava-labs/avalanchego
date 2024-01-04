// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"errors"
	"sync"

	"github.com/ava-labs/avalanchego/cache"

	"github.com/ava-labs/avalanchego/database"
)

const defaultBufferLength = 256

var errCacheSizeTooSmall = errors.New("cache size must be larger than or equal to write buffer size")

// Holds intermediate nodes. That is, those without values.
// Changes to this database aren't written to [baseDB] until
// they're evicted from the [nodeCache] or Flush is called.
type intermediateNodeDB struct {
	// Holds unused []byte
	bufferPool *sync.Pool

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
	metrics           merkleMetrics
	tokenSize         int
}

func newIntermediateNodeDB(
	db database.Database,
	bufferPool *sync.Pool,
	metrics merkleMetrics,
	cacheSize int,
	writeBufferSize int,
	evictionBatchSize int,
	tokenSize int,
) (*intermediateNodeDB, error) {
	if cacheSize < writeBufferSize {
		return nil, errCacheSizeTooSmall
	}
	result := &intermediateNodeDB{
		metrics:           metrics,
		baseDB:            db,
		bufferPool:        bufferPool,
		evictionBatchSize: evictionBatchSize,
		tokenSize:         tokenSize,
		nodeCache:         cache.NewSizedLRU(cacheSize, cacheEntrySize),
	}
	result.writeBuffer = newOnEvictCache(
		writeBufferSize,
		cacheEntrySize,
		result.onEviction,
	)

	return result, nil
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

func (db *intermediateNodeDB) addToBatch(b database.Batch, key Key, n *node) error {
	dbKey := db.constructDBKey(key)
	defer db.bufferPool.Put(dbKey)
	db.metrics.DatabaseNodeWrite()
	if n == nil {
		return b.Delete(dbKey)
	}
	return b.Put(dbKey, n.bytes())
}

func (db *intermediateNodeDB) Get(key Key) (*node, error) {
	if cachedValue, isCached := db.writeBuffer.Get(key); isCached {
		db.metrics.IntermediateNodeCacheHit()
		if cachedValue == nil {
			return nil, database.ErrNotFound
		}
		return cachedValue, nil
	}
	db.metrics.IntermediateNodeCacheMiss()

	if cachedValue, isCached := db.nodeCache.Get(key); isCached {
		db.metrics.IntermediateNodeCacheHit()
		if cachedValue == nil {
			return nil, database.ErrNotFound
		}
		return cachedValue, nil
	}
	db.metrics.IntermediateNodeCacheMiss()

	dbKey := db.constructDBKey(key)
	db.metrics.DatabaseNodeRead()
	nodeBytes, err := db.baseDB.Get(dbKey)
	if err != nil {
		return nil, err
	}
	db.bufferPool.Put(dbKey)

	return parseNode(key, nodeBytes)
}

// constructDBKey returns a key that can be used in [db.baseDB].
// We need to be able to differentiate between two keys of equal
// byte length but different bit length, so we add padding to differentiate.
// Additionally, we add a prefix indicating it is part of the intermediateNodeDB.
func (db *intermediateNodeDB) constructDBKey(key Key) []byte {
	if db.tokenSize == 8 {
		// For tokens of size byte, no padding is needed since byte length == token length
		return addPrefixToKey(db.bufferPool, intermediateNodePrefix, key.Bytes())
	}

	return addPrefixToKey(db.bufferPool, intermediateNodePrefix, key.Extend(ToToken(1, db.tokenSize)).Bytes())
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
