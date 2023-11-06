// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"sync"

	"github.com/ava-labs/avalanchego/database"
)

const defaultBufferLength = 256

// Holds node information except for the value
// Changes to this database aren't written to [baseDB] until
// they're evicted from the [cache] or Flush is called.
type nodeDB struct {
	// Holds unused []byte
	bufferPool *sync.Pool

	// The underlying storage.
	// Keys written to [baseDB] are prefixed with [nodePrefix].
	baseDB database.Database

	// If a value is nil, the corresponding key isn't in the trie.
	// Note that a call to Put may cause a node to be evicted
	// from the cache, which will call [OnEviction].
	// A non-nil error returned from Put is considered fatal.
	// Keys in [cache] aren't prefixed with [nodePrefix].
	nodeCache onEvictCache[Key, *node]
	// the number of bytes to evict during an eviction batch
	evictionBatchSize int
	metrics           merkleMetrics
	tokenSize         int
}

func newNodeDB(
	db database.Database,
	bufferPool *sync.Pool,
	metrics merkleMetrics,
	size int,
	evictionBatchSize int,
	tokenSize int,
) *nodeDB {
	result := &nodeDB{
		metrics:           metrics,
		baseDB:            db,
		bufferPool:        bufferPool,
		evictionBatchSize: evictionBatchSize,
		tokenSize:         tokenSize,
	}
	result.nodeCache = newOnEvictCache(
		size,
		func(k Key, n *node) int {
			return len(k.value) + codec.encodedNodeSize(n)
		},
		result.onEviction,
	)
	return result
}

// A non-nil error is considered fatal and closes [db.baseDB].
func (db *nodeDB) onEviction(key Key, n *node) error {
	writeBatch := db.baseDB.NewBatch()

	totalSize := db.nodeCache.size(key, n)
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
		key, n, exists := db.nodeCache.removeOldest()
		if !exists {
			// The cache is empty.
			break
		}
		totalSize += db.nodeCache.size(key, n)
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

func (db *nodeDB) addToBatch(b database.Batch, key Key, n *node) error {
	dbKey := db.constructDBKey(key)
	defer db.bufferPool.Put(dbKey)
	db.metrics.DatabaseNodeWrite()
	if n == nil {
		return b.Delete(dbKey)
	}
	return b.Put(dbKey, codec.encodeNode(n))
}

func (db *nodeDB) Get(key Key) (*node, error) {
	if cachedValue, isCached := db.nodeCache.Get(key); isCached {
		db.metrics.NodeCacheHit()
		if cachedValue == nil {
			return nil, database.ErrNotFound
		}
		return cachedValue, nil
	}
	db.metrics.NodeCacheMiss()

	dbKey := db.constructDBKey(key)
	db.metrics.DatabaseNodeRead()
	nodeBytes, err := db.baseDB.Get(dbKey)
	if err != nil {
		return nil, err
	}
	db.bufferPool.Put(dbKey)
	children, err := codec.decodeNode(nodeBytes)
	if err != nil {
		return nil, err
	}
	return children, nil
}

// constructDBKey returns a key that can be used in [db.baseDB].
// We need to be able to differentiate between two keys of equal
// byte length but different token length, so we add padding to differentiate.
// Additionally, we add a prefix indicating it is part of the nodeDB.
func (db *nodeDB) constructDBKey(key Key) []byte {
	if db.tokenSize == 8 {
		// For tokens of size byte, no padding is needed since byte length == token length
		return addPrefixToKey(db.bufferPool, nodePrefix, key.Bytes())
	}

	return addPrefixToKey(db.bufferPool, nodePrefix, key.Extend(ToToken(1, db.tokenSize)).Bytes())
}

func (db *nodeDB) Put(key Key, n *node) error {
	return db.nodeCache.Put(key, n)
}

func (db *nodeDB) Flush() error {
	return db.nodeCache.Flush()
}

func (db *nodeDB) Delete(key Key) error {
	return db.nodeCache.Put(key, nil)
}
