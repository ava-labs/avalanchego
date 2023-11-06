// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"errors"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"sync"

	"github.com/ava-labs/avalanchego/cache"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils"
)

var _ database.Iterator = (*valueIterator)(nil)

type valueDB struct {
	// Holds unused []byte
	bufferPool *sync.Pool

	// The underlying storage.
	// Keys written to [baseDB] are prefixed with [valuePrefix].
	baseDB database.Database

	// If a value is nil, the corresponding key isn't in the trie.
	// Paths in [cache] aren't prefixed with [valuePrefix].
	cache   cache.Cacher[Key, maybe.Maybe[[]byte]]
	metrics merkleMetrics

	closed utils.Atomic[bool]
}

func newValueDB(
	db database.Database,
	bufferPool *sync.Pool,
	metrics merkleMetrics,
	cacheSize int,
) *valueDB {
	return &valueDB{
		metrics:    metrics,
		baseDB:     db,
		bufferPool: bufferPool,
		cache: cache.NewSizedLRU(cacheSize, func(k Key, v maybe.Maybe[[]byte]) int {
			return len(k.value) + len(v.Value()) + 10
		}),
	}
}

func (db *valueDB) newIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	prefixedStart := addPrefixToKey(db.bufferPool, valuePrefix, start)
	prefixedPrefix := addPrefixToKey(db.bufferPool, valuePrefix, prefix)
	i := &valueIterator{
		db:       db,
		nodeIter: db.baseDB.NewIteratorWithStartAndPrefix(prefixedStart, prefixedPrefix),
	}
	db.bufferPool.Put(prefixedStart)
	db.bufferPool.Put(prefixedPrefix)
	return i
}

func (db *valueDB) Close() {
	db.closed.Set(true)
}

func (db *valueDB) NewBatch() *valueBatch {
	return &valueBatch{
		db:  db,
		ops: make(map[Key]maybe.Maybe[[]byte], defaultBufferLength),
	}
}

func (db *valueDB) Get(key Key) (maybe.Maybe[[]byte], error) {
	// partial byte keys never have a value
	if key.hasPartialByte() {
		return maybe.Nothing[[]byte](), nil
	}

	if cachedValue, isCached := db.cache.Get(key); isCached {
		db.metrics.ValueCacheHit()
		return cachedValue, nil
	}
	db.metrics.ValueCacheMiss()

	prefixedKey := addPrefixToKey(db.bufferPool, valuePrefix, key.Bytes())
	defer db.bufferPool.Put(prefixedKey)

	db.metrics.DatabaseNodeRead()
	result := maybe.Nothing[[]byte]()
	val, err := db.baseDB.Get(prefixedKey)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return maybe.Nothing[[]byte](), nil
		}
		return maybe.Nothing[[]byte](), nil
	} else {
		result = maybe.Some(val)
	}

	return result, nil
}

// Batch of database operations
type valueBatch struct {
	db  *valueDB
	ops map[Key]maybe.Maybe[[]byte]
}

func (b *valueBatch) Put(key Key, value []byte) {
	b.ops[key] = maybe.Some(value)
}

func (b *valueBatch) Delete(key Key) {
	b.ops[key] = maybe.Nothing[[]byte]()
}

// Write flushes any accumulated data to the underlying database.
func (b *valueBatch) Write() error {
	dbBatch := b.db.baseDB.NewBatch()
	for key, n := range b.ops {
		b.db.metrics.DatabaseNodeWrite()
		b.db.cache.Put(key, n)
		prefixedKey := addPrefixToKey(b.db.bufferPool, valuePrefix, key.Bytes())
		if n.IsNothing() {
			if err := dbBatch.Delete(prefixedKey); err != nil {
				return err
			}
		} else if err := dbBatch.Put(prefixedKey, n.Value()); err != nil {
			return err
		}

		b.db.bufferPool.Put(prefixedKey)
	}

	return dbBatch.Write()
}

type valueIterator struct {
	db           *valueDB
	nodeIter     database.Iterator
	currentKey   Key
	currentValue []byte
	err          error
}

func (i *valueIterator) Error() error {
	if i.err != nil {
		return i.err
	}
	if i.db.closed.Get() {
		return database.ErrClosed
	}
	return i.nodeIter.Error()
}

func (i *valueIterator) Key() []byte {
	return i.currentKey.Bytes()
}

func (i *valueIterator) Value() []byte {
	return i.currentValue
}

func (i *valueIterator) Next() bool {
	i.currentKey = emptyKey
	i.currentValue = nil
	if i.Error() != nil || i.db.closed.Get() {
		return false
	}
	if !i.nodeIter.Next() {
		return false
	}

	i.db.metrics.DatabaseNodeRead()
	key := i.nodeIter.Key()
	key = key[valuePrefixLen:]
	i.currentKey = ToKey(key)
	i.currentValue = i.nodeIter.Value()
	return true
}

func (i *valueIterator) Release() {
	i.nodeIter.Release()
}
