// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"sync"

	"github.com/ava-labs/avalanchego/cache"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils"
)

var _ database.Iterator = (*iterator)(nil)

type valueNodeDB struct {
	// Holds unused []byte
	bufferPool *sync.Pool

	// The underlying storage.
	// Keys written to [baseDB] are prefixed with [valueNodePrefix].
	baseDB database.Database

	// If a value is nil, the corresponding key isn't in the trie.
	// Paths in [nodeCache] aren't prefixed with [valueNodePrefix].
	nodeCache cache.Cacher[Key, *node]
	metrics   merkleMetrics

	closed       utils.Atomic[bool]
	branchFactor BranchFactor
}

func newValueNodeDB(
	db database.Database,
	bufferPool *sync.Pool,
	metrics merkleMetrics,
	cacheSize int,
	branchFactor BranchFactor,
) *valueNodeDB {
	return &valueNodeDB{
		metrics:      metrics,
		baseDB:       db,
		bufferPool:   bufferPool,
		nodeCache:    cache.NewSizedLRU(cacheSize, cacheEntrySize),
		branchFactor: branchFactor,
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
		ops: make(map[Key]*node, defaultBufferLength),
	}
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
	nodeBytes, err := db.baseDB.Get(prefixedKey)
	if err != nil {
		return nil, err
	}

	return parseNode(key, nodeBytes)
}

// Batch of database operations
type valueNodeBatch struct {
	db  *valueNodeDB
	ops map[Key]*node
}

func (b *valueNodeBatch) Put(key Key, value *node) {
	b.ops[key] = value
}

func (b *valueNodeBatch) Delete(key Key) {
	b.ops[key] = nil
}

// Write flushes any accumulated data to the underlying database.
func (b *valueNodeBatch) Write() error {
	dbBatch := b.db.baseDB.NewBatch()
	for key, n := range b.ops {
		b.db.metrics.DatabaseNodeWrite()
		b.db.nodeCache.Put(key, n)
		prefixedKey := addPrefixToKey(b.db.bufferPool, valueNodePrefix, key.Bytes())
		if n == nil {
			if err := dbBatch.Delete(prefixedKey); err != nil {
				return err
			}
		} else if err := dbBatch.Put(prefixedKey, n.bytes()); err != nil {
			return err
		}

		b.db.bufferPool.Put(prefixedKey)
	}

	return dbBatch.Write()
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
	n, err := parseNode(ToKey(key, i.db.branchFactor), i.nodeIter.Value())
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
