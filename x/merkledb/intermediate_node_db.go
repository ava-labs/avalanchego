// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"sync"

	"github.com/ava-labs/avalanchego/database"
)

const defaultBufferLength = 256

// Holds intermediate nodes. That is, those without values.
// Changes to this database aren't written to [baseDB] until
// they're evicted from the [nodeCache] or Flush is called..
type intermediateNodeDB struct {
	// Holds unused []byte
	bufferPool *sync.Pool

	// The underlying storage.
	// Keys written to [baseDB] are prefixed with [intermediateNodePrefix].
	baseDB database.Database

	// If a value is nil, the corresponding key isn't in the trie.
	// Note that a call to Put may cause a node to be evicted
	// from the cache, which will call [OnEviction].
	// A non-nil error returned from Put is considered fatal.
	// Keys in [nodeCache] aren't prefixed with [intermediateNodePrefix].
	nodeCache         onEvictCache[path, *node]
	evictionBatchSize int
	metrics           merkleMetrics
}

func newIntermediateNodeDB(
	db database.Database,
	bufferPool *sync.Pool,
	metrics merkleMetrics,
	size int,
	evictionBatchSize int,
) *intermediateNodeDB {
	result := &intermediateNodeDB{
		metrics:           metrics,
		baseDB:            db,
		bufferPool:        bufferPool,
		evictionBatchSize: evictionBatchSize,
	}
	result.nodeCache = newOnEvictCache[path](size, result.onEviction)
	return result
}

// A non-nil error is considered fatal and closes [db.baseDB].
func (db *intermediateNodeDB) onEviction(key path, n *node) error {
	writeBatch := db.baseDB.NewBatch()

	if err := db.addToBatch(writeBatch, key, n); err != nil {
		_ = db.baseDB.Close()
		return err
	}

	// Evict the oldest [evictionBatchSize] nodes from the cache
	// and write them to disk. We write a batch of them, rather than
	// just [n], so that we don't immediately evict and write another
	// node, because each time this method is called we do a disk write.
	// we have already removed the passed n, so the remove count starts at 1
	for removedCount := 1; removedCount < db.evictionBatchSize; removedCount++ {
		key, n, exists := db.nodeCache.removeOldest()
		if !exists {
			// The cache is empty.
			break
		}
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

func (db *intermediateNodeDB) addToBatch(b database.Batch, key path, n *node) error {
	prefixedKey := addPrefixToKey(db.bufferPool, intermediateNodePrefix, key.Bytes())
	defer db.bufferPool.Put(prefixedKey)
	db.metrics.DatabaseNodeWrite()
	if n == nil {
		return b.Delete(prefixedKey)
	}
	return b.Put(prefixedKey, n.marshal())
}

func (db *intermediateNodeDB) Get(key path) (*node, error) {
	if cachedValue, isCached := db.nodeCache.Get(key); isCached {
		db.metrics.IntermediateNodeCacheHit()
		if cachedValue == nil {
			return nil, database.ErrNotFound
		}
		return cachedValue, nil
	}
	db.metrics.IntermediateNodeCacheMiss()

	prefixedKey := addPrefixToKey(db.bufferPool, intermediateNodePrefix, key.Bytes())
	db.metrics.DatabaseNodeRead()
	nodeBytes, err := db.baseDB.Get(prefixedKey)
	if err != nil {
		return nil, err
	}
	db.bufferPool.Put(prefixedKey)

	return parseNode(key, nodeBytes)
}

func (db *intermediateNodeDB) Put(key path, n *node) error {
	return db.nodeCache.Put(key, n)
}

func (db *intermediateNodeDB) Flush() error {
	return db.nodeCache.Flush()
}

func (db *intermediateNodeDB) Delete(key path) error {
	return db.nodeCache.Put(key, nil)
}
