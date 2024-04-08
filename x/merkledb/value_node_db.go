// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils"
)

var _ database.Iterator = (*iterator)(nil)

type valueNodeDB struct {
	bufferPool *utils.BytesPool

	// The underlying storage.
	// Keys written to [baseDB] are prefixed with [valueNodePrefix].
	baseDB database.Database

	// If a value is nil, the corresponding key isn't in the trie.
	// Paths in [nodeCache] aren't prefixed with [valueNodePrefix].
	nodeCache cache.Cacher[Key, *node]
	metrics   merkleMetrics

	closed utils.Atomic[bool]
}

func newValueNodeDB(
	db database.Database,
	bufferPool *utils.BytesPool,
	metrics merkleMetrics,
	cacheSize int,
) *valueNodeDB {
	return &valueNodeDB{
		metrics:    metrics,
		baseDB:     db,
		bufferPool: bufferPool,
		nodeCache:  cache.NewSizedLRU(cacheSize, cacheEntrySize),
	}
}

func (db *valueNodeDB) Write(batch database.KeyValueWriterDeleter, key Key, n *node) error {
	db.metrics.DatabaseNodeWrite()
	db.nodeCache.Put(key, n)
	prefixedKey := addPrefixToKey(db.bufferPool, valueNodePrefix, key.Bytes())
	defer db.bufferPool.Put(prefixedKey)

	if n == nil {
		return batch.Delete(*prefixedKey)
	}
	return batch.Put(*prefixedKey, n.bytes())
}

func (db *valueNodeDB) newIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	prefixedStart := addPrefixToKey(db.bufferPool, valueNodePrefix, start)
	defer db.bufferPool.Put(prefixedStart)

	prefixedPrefix := addPrefixToKey(db.bufferPool, valueNodePrefix, prefix)
	defer db.bufferPool.Put(prefixedPrefix)

	return &iterator{
		db:       db,
		nodeIter: db.baseDB.NewIteratorWithStartAndPrefix(*prefixedStart, *prefixedPrefix),
	}
}

func (db *valueNodeDB) Close() {
	db.closed.Set(true)
}

func (db *valueNodeDB) Get(key Key) (*node, error) {
	if cachedValue, isCached := db.nodeCache.Get(key); isCached {
		db.metrics.ValueNodeCacheHit()
		if cachedValue == nil {
			return nil, database.ErrNotFound
		}
		return cachedValue, nil
	}
	db.metrics.ValueNodeCacheMiss()

	prefixedKey := addPrefixToKey(db.bufferPool, valueNodePrefix, key.Bytes())
	defer db.bufferPool.Put(prefixedKey)

	db.metrics.DatabaseNodeRead()
	nodeBytes, err := db.baseDB.Get(*prefixedKey)
	if err != nil {
		return nil, err
	}

	return parseNode(key, nodeBytes)
}

func (db *valueNodeDB) Clear() error {
	db.nodeCache.Flush()
	return database.AtomicClearPrefix(db.baseDB, db.baseDB, valueNodePrefix)
}

type iterator struct {
	db       *valueNodeDB
	nodeIter database.Iterator
	current  *node
	err      error
}

func (i *iterator) Error() error {
	if i.err != nil {
		return i.err
	}
	if i.db.closed.Get() {
		return database.ErrClosed
	}
	return i.nodeIter.Error()
}

func (i *iterator) Key() []byte {
	if i.current == nil {
		return nil
	}
	return i.current.key.Bytes()
}

func (i *iterator) Value() []byte {
	if i.current == nil {
		return nil
	}
	return i.current.value.Value()
}

func (i *iterator) Next() bool {
	i.current = nil
	if i.Error() != nil || i.db.closed.Get() {
		return false
	}
	if !i.nodeIter.Next() {
		return false
	}

	i.db.metrics.DatabaseNodeRead()
	key := i.nodeIter.Key()
	key = key[valueNodePrefixLen:]
	n, err := parseNode(ToKey(key), i.nodeIter.Value())
	if err != nil {
		i.err = err
		return false
	}

	i.current = n
	return true
}

func (i *iterator) Release() {
	i.nodeIter.Release()
}
