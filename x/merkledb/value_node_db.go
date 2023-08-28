// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"sync"

	"github.com/ava-labs/avalanchego/cache"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils"
)

const valueNodePrefixLen = 1

var valueNodePrefix = []byte{1}

type valueNodeDB struct {
	// Holds unused []byte
	bufferPool *sync.Pool

	// The underlying storage
	baseDB database.Database

	// If a value is nil, the corresponding key isn't in the trie.
	// A non-nil error returned from Put is considered fatal.
	nodeCache cache.LRU[path, *node]
	metrics   merkleMetrics

	closed utils.Atomic[bool]
}

func newValueNodeDB(db database.Database, bufferPool *sync.Pool, metrics merkleMetrics, size int) *valueNodeDB {
	return &valueNodeDB{
		metrics:    metrics,
		baseDB:     db,
		bufferPool: bufferPool,
		nodeCache:  cache.LRU[path, *node]{Size: size},
	}
}

func (db *valueNodeDB) newIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	prefixedStart := addPrefixToKey(db.bufferPool, valueNodePrefix, start)
	prefixedPrefix := addPrefixToKey(db.bufferPool, valueNodePrefix, prefix)
	i := &iterator{
		db:       db,
		nodeIter: db.baseDB.NewIteratorWithStartAndPrefix(prefixedStart, prefixedPrefix),
	}
	db.bufferPool.Put(prefixedStart)
	db.bufferPool.Put(prefixedPrefix)
	return i
}

func (db *valueNodeDB) Close() {
	db.closed.Set(true)
}

func (db *valueNodeDB) NewBatch() *valueNodeBatch {
	return &valueNodeBatch{
		db:  db,
		ops: make(map[path]*node, defaultBufferLength),
	}
}

func (db *valueNodeDB) Get(key path) (*node, error) {
	if cachedValue, isCached := db.nodeCache.Get(key); isCached {
		db.metrics.ValueNodeCacheHit()
		if cachedValue == nil {
			return nil, database.ErrNotFound
		}
		return cachedValue, nil
	}
	db.metrics.IntermediateNodeCacheMiss()

	prefixedKey := addPrefixToKey(db.bufferPool, valueNodePrefix, key.Serialize().Value)
	db.metrics.IOKeyRead()
	nodeBytes, err := db.baseDB.Get(prefixedKey)
	if err != nil {
		return nil, err
	}
	db.bufferPool.Put(prefixedKey)

	return parseNode(key, nodeBytes)
}

// Batch of database operations
type valueNodeBatch struct {
	db  *valueNodeDB
	ops map[path]*node
}

func (b *valueNodeBatch) Put(key path, value *node) {
	b.ops[key] = value
}

func (b *valueNodeBatch) Delete(key path) {
	b.ops[key] = nil
}

// Write flushes any accumulated data to the underlying database.
func (b *valueNodeBatch) Write() error {
	dbBatch := b.db.baseDB.NewBatch()
	for key, n := range b.ops {
		b.db.metrics.IOKeyWrite()
		b.db.nodeCache.Put(key, n)
		prefixedKey := addPrefixToKey(b.db.bufferPool, valueNodePrefix, key.Serialize().Value)
		if n == nil {
			if err := dbBatch.Delete(prefixedKey); err != nil {
				return err
			}
		} else if err := dbBatch.Put(prefixedKey, n.marshal()); err != nil {
			return err
		}

		b.db.bufferPool.Put(prefixedKey)
	}

	return dbBatch.Write()
}

var _ database.Iterator = (*iterator)(nil)

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
	return i.current.key.Serialize().Value
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

	i.db.metrics.IOKeyRead()
	key := i.nodeIter.Key()
	key = key[valueNodePrefixLen:]
	n, err := parseNode(newPath(key), i.nodeIter.Value())
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
