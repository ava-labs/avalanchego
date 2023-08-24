// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils"
)

var intermediateNodePrefixBytes = []byte(intermediateNodePrefix)

const (
	DefaultBufferLength       = 256
	intermediateNodePrefix    = "intermediateNode"
	intermediateNodePrefixLen = len(intermediateNodePrefix)
)

type intermediateNodeDB struct {
	// Holds unused []byte
	bufferPool sync.Pool

	// lock needs to be held during Close to guarantee db will not be set to nil
	// concurrently with another operation. All other operations can hold RLock.
	lock sync.RWMutex
	// The underlying storage
	underlyingDB database.Database

	// If a value is nil, the corresponding key isn't in the trie.
	// Note that a call to Put may cause a node to be evicted
	// from the cache, which will call [OnEviction].
	// A non-nil error returned from Put is considered fatal.
	nodeCache         onEvictCache[path, *node]
	onEvictionErr     utils.Atomic[error]
	evictionBatchSize int
	metrics           merkleMetrics
}

func newIntermediateNodeDB(db database.Database, metrics merkleMetrics, size int, evictionBatchSize int) *intermediateNodeDB {
	result := &intermediateNodeDB{
		metrics:      metrics,
		underlyingDB: db,
		bufferPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, DefaultBufferLength)
			},
		},
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
	copy(prefixedKey, intermediateNodePrefixBytes)
	copy(prefixedKey[intermediateNodePrefixLen:], key)
	return prefixedKey
}

func (db *intermediateNodeDB) onEviction(key path, n *node) error {
	writeBatch := db.underlyingDB.NewBatch()

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
		_ = db.underlyingDB.Close()
		return err
	}
	return nil
}

func (db *intermediateNodeDB) addToBatch(b database.Batch, key path, n *node) error {
	prefixedKey := db.prefixedKey(key.Bytes())
	defer db.bufferPool.Put(prefixedKey)
	if n == nil {
		db.metrics.IOKeyWrite()
		if err := b.Delete(prefixedKey); err != nil {
			return err
		}
		return nil
	}
	nodeBytes, err := n.marshal()
	if err != nil {
		return err
	}
	db.metrics.IOKeyWrite()
	if err := b.Put(prefixedKey, nodeBytes); err != nil {
		return err
	}
	return nil
}

func (db *intermediateNodeDB) Get(key path) (*node, error) {
	if cachedValue, isCached := db.nodeCache.Get(key); isCached {
		db.metrics.DBIntermediateNodeCacheHit()
		if cachedValue == nil {
			return nil, database.ErrNotFound
		}
		return cachedValue, nil
	}
	db.metrics.DBIntermediateNodeCacheMiss()

	prefixedKey := db.prefixedKey(key.Bytes())
	db.metrics.IOKeyRead()
	val, err := db.underlyingDB.Get(prefixedKey)
	if err != nil {
		return nil, err
	}
	db.bufferPool.Put(prefixedKey)

	n, err := parseNode(key, val)
	if err != nil {
		return nil, err
	}

	return n, err
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
