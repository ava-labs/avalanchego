// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"sync"

	"github.com/ava-labs/avalanchego/database"
)

const (
	defaultBufferLength       = 256
	intermediateNodePrefixLen = 1
)

var intermediateNodePrefix = []byte{2}

type intermediateNodeDB struct {
	// Holds unused []byte
	bufferPool *sync.Pool

	// The underlying storage
	baseDB database.Database

	// If a value is nil, the corresponding key isn't in the trie.
	// Note that a call to Put may cause a node to be evicted
	// from the cache, which will call [OnEviction].
	// A non-nil error returned from Put is considered fatal.
	nodeCache         onEvictCache[path, *node]
	evictionBatchSize int
	metrics           merkleMetrics
}

func newIntermediateNodeDB(db database.Database, bufferPool *sync.Pool, metrics merkleMetrics, size int, evictionBatchSize int) *intermediateNodeDB {
	result := &intermediateNodeDB{
		metrics:           metrics,
		baseDB:            db,
		bufferPool:        bufferPool,
		evictionBatchSize: evictionBatchSize,
	}
	result.nodeCache = newOnEvictCache[path](size, result.onEviction)
	return result
}

func (db *intermediateNodeDB) prefixedKey(key []byte) []byte {
	prefixedKey := db.bufferPool.Get().([]byte)
	keyLen := intermediateNodePrefixLen + len(key)
	if cap(prefixedKey) >= keyLen {
		// The [] byte we got from the pool is big enough to hold the prefixed key
		prefixedKey = prefixedKey[:keyLen]
	} else {
		// The []byte from the pool wasn't big enough.
		// Put it back and allocate a new, bigger one
		db.bufferPool.Put(prefixedKey)
		prefixedKey = make([]byte, keyLen)
	}
	copy(prefixedKey, intermediateNodePrefix)
	copy(prefixedKey[intermediateNodePrefixLen:], key)
	return prefixedKey
}

func (db *intermediateNodeDB) onEviction(key path, n *node) error {
	writeBatch := db.baseDB.NewBatch()

	if err := db.addToBatch(writeBatch, key, n); err != nil {
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
	prefixedKey := db.prefixedKey(key.Bytes())
	defer db.bufferPool.Put(prefixedKey)
	db.metrics.IOKeyWrite()
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

	prefixedKey := db.prefixedKey(key.Bytes())
	db.metrics.IOKeyRead()
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
