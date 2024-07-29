// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"errors"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils"
)

var (
	_ database.Iterator = (*iterator)(nil)

	errNodeMissingValue = errors.New("valueNodeDB contains node without a value")
)

type valueNodeDB struct {
	bufferPool *utils.BytesPool

	// The underlying storage.
	// Keys written to [baseDB] are prefixed with [valueNodePrefix].
	baseDB database.Database

	// If a value is nil, the corresponding key isn't in the trie.
	// Paths in [nodeCache] aren't prefixed with [valueNodePrefix].
	nodeCache cache.Cacher[Key, *node]
	metrics   metrics

	hasher Hasher

	closed utils.Atomic[bool]
}

func newValueNodeDB(
	db database.Database,
	bufferPool *utils.BytesPool,
	metrics metrics,
	cacheSize int,
	hasher Hasher,
) *valueNodeDB {
	return &valueNodeDB{
		metrics:    metrics,
		baseDB:     db,
		bufferPool: bufferPool,
		nodeCache:  lru.NewSizedCache(cacheSize, cacheEntrySize),
		hasher:     hasher,
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

	return parseNode(db.hasher, key, nodeBytes)
}

func (db *valueNodeDB) Clear() error {
	db.nodeCache.Flush()
	return database.AtomicClearPrefix(db.baseDB, db.baseDB, valueNodePrefix)
}

type iterator struct {
	db       *valueNodeDB
	nodeIter database.Iterator
	key      []byte
	value    []byte
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
	return i.key
}

func (i *iterator) Value() []byte {
	return i.value
}

func (i *iterator) Next() bool {
	i.key = nil
	i.value = nil
	if i.Error() != nil || i.db.closed.Get() {
		return false
	}
	if !i.nodeIter.Next() {
		return false
	}

	i.db.metrics.DatabaseNodeRead()

	r := codecReader{
		b: i.nodeIter.Value(),
		// We are discarding the other bytes from the node, so we avoid copying
		// the value here.
		copy: false,
	}
	maybeValue, err := r.MaybeBytes()
	if err != nil {
		i.err = err
		return false
	}
	if maybeValue.IsNothing() {
		i.err = errNodeMissingValue
		return false
	}

	i.key = i.nodeIter.Key()[valueNodePrefixLen:]
	i.value = maybeValue.Value()
	return true
}

func (i *iterator) Release() {
	i.nodeIter.Release()
}
