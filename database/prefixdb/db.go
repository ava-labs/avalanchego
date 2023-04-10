// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prefixdb

import (
	"context"
	"sync"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	defaultBufCap = 256
)

var (
	_ database.Database = (*Database)(nil)
	_ database.Batch    = (*batch)(nil)
	_ database.Iterator = (*iterator)(nil)
)

// Database partitions a database into a sub-database by prefixing all keys with
// a unique value.
type Database struct {
	// All keys in this db begin with this byte slice
	dbPrefix []byte
	// Holds unused []byte
	bufferPool sync.Pool

	// lock needs to be held during Close to guarantee db will not be set to nil
	// concurrently with another operation. All other operations can hold RLock.
	lock sync.RWMutex
	// The underlying storage
	db     database.Database
	closed bool
}

// New returns a new prefixed database
func New(prefix []byte, db database.Database) *Database {
	if prefixDB, ok := db.(*Database); ok {
		simplePrefix := make([]byte, len(prefixDB.dbPrefix)+len(prefix))
		copy(simplePrefix, prefixDB.dbPrefix)
		copy(simplePrefix[len(prefixDB.dbPrefix):], prefix)
		return NewNested(simplePrefix, prefixDB.db)
	}
	return NewNested(prefix, db)
}

// NewNested returns a new prefixed database without attempting to compress
// prefixes.
func NewNested(prefix []byte, db database.Database) *Database {
	return &Database{
		dbPrefix: hashing.ComputeHash256(prefix),
		db:       db,
		bufferPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, defaultBufCap)
			},
		},
	}
}

// Assumes that it is OK for the argument to db.db.Has
// to be modified after db.db.Has returns
// [key] may be modified after this method returns.
func (db *Database) Has(key []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return false, database.ErrClosed
	}
	prefixedKey := db.prefix(key)
	has, err := db.db.Has(prefixedKey)
	db.bufferPool.Put(prefixedKey)
	return has, err
}

// Assumes that it is OK for the argument to db.db.Get
// to be modified after db.db.Get returns.
// [key] may be modified after this method returns.
func (db *Database) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}
	prefixedKey := db.prefix(key)
	val, err := db.db.Get(prefixedKey)
	db.bufferPool.Put(prefixedKey)
	return val, err
}

// Assumes that it is OK for the argument to db.db.Put
// to be modified after db.db.Put returns.
// [key] can be modified after this method returns.
// [value] should not be modified.
func (db *Database) Put(key, value []byte) error {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return database.ErrClosed
	}
	prefixedKey := db.prefix(key)
	err := db.db.Put(prefixedKey, value)
	db.bufferPool.Put(prefixedKey)
	return err
}

// Assumes that it is OK for the argument to db.db.Delete
// to be modified after db.db.Delete returns.
// [key] may be modified after this method returns.
func (db *Database) Delete(key []byte) error {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return database.ErrClosed
	}
	prefixedKey := db.prefix(key)
	err := db.db.Delete(prefixedKey)
	db.bufferPool.Put(prefixedKey)
	return err
}

func (db *Database) NewBatch() database.Batch {
	return &batch{
		Batch: db.db.NewBatch(),
		db:    db,
	}
}

func (db *Database) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

// Assumes it is safe to modify the arguments to db.db.NewIteratorWithStartAndPrefix after it returns.
// It is safe to modify [start] and [prefix] after this method returns.
func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return &database.IteratorError{
			Err: database.ErrClosed,
		}
	}
	prefixedStart := db.prefix(start)
	prefixedPrefix := db.prefix(prefix)
	it := &iterator{
		Iterator: db.db.NewIteratorWithStartAndPrefix(prefixedStart, prefixedPrefix),
		db:       db,
	}
	db.bufferPool.Put(prefixedStart)
	db.bufferPool.Put(prefixedPrefix)
	return it
}

func (db *Database) Compact(start, limit []byte) error {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return database.ErrClosed
	}
	return db.db.Compact(db.prefix(start), db.prefix(limit))
}

func (db *Database) Close() error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}
	db.closed = true
	return nil
}

func (db *Database) isClosed() bool {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.closed
}

func (db *Database) HealthCheck(ctx context.Context) (interface{}, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}
	return db.db.HealthCheck(ctx)
}

// Return a copy of [key], prepended with this db's prefix.
// The returned slice should be put back in the pool
// when it's done being used.
func (db *Database) prefix(key []byte) []byte {
	// Get a []byte from the pool
	prefixedKey := db.bufferPool.Get().([]byte)
	keyLen := len(db.dbPrefix) + len(key)
	if cap(prefixedKey) >= keyLen {
		// The [] byte we got from the pool is big enough to hold the prefixed key
		prefixedKey = prefixedKey[:keyLen]
	} else {
		// The []byte from the pool wasn't big enough.
		// Put it back and allocate a new, bigger one
		db.bufferPool.Put(prefixedKey)
		prefixedKey = make([]byte, keyLen)
	}
	copy(prefixedKey, db.dbPrefix)
	copy(prefixedKey[len(db.dbPrefix):], key)
	return prefixedKey
}

// Batch of database operations
type batch struct {
	database.Batch
	db *Database

	// Each key is prepended with the database's prefix.
	// Each byte slice underlying a key should be returned to the pool
	// when this batch is reset.
	ops []database.BatchOp
}

// Assumes that it is OK for the argument to b.Batch.Put
// to be modified after b.Batch.Put returns
// [key] may be modified after this method returns.
// [value] may be modified after this method returns.
func (b *batch) Put(key, value []byte) error {
	prefixedKey := b.db.prefix(key)
	copiedValue := slices.Clone(value)
	b.ops = append(b.ops, database.BatchOp{
		Key:   prefixedKey,
		Value: copiedValue,
	})
	return b.Batch.Put(prefixedKey, copiedValue)
}

// Assumes that it is OK for the argument to b.Batch.Delete
// to be modified after b.Batch.Delete returns
// [key] may be modified after this method returns.
func (b *batch) Delete(key []byte) error {
	prefixedKey := b.db.prefix(key)
	b.ops = append(b.ops, database.BatchOp{
		Key:    prefixedKey,
		Delete: true,
	})
	return b.Batch.Delete(prefixedKey)
}

// Write flushes any accumulated data to the memory database.
func (b *batch) Write() error {
	b.db.lock.RLock()
	defer b.db.lock.RUnlock()

	if b.db.closed {
		return database.ErrClosed
	}
	return b.Batch.Write()
}

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	// Return the byte buffers underneath each key back to the pool.
	// Don't return the byte buffers underneath each value back to the pool
	// because we assume in batch.Replay that it's not safe to modify the
	// value argument to w.Put.
	for _, op := range b.ops {
		b.db.bufferPool.Put(op.Key)
	}

	// Clear b.writes
	if cap(b.ops) > len(b.ops)*database.MaxExcessCapacityFactor {
		b.ops = make([]database.BatchOp, 0, cap(b.ops)/database.CapacityReductionFactor)
	} else {
		b.ops = b.ops[:0]
	}
	b.Batch.Reset()
}

// Replay replays the batch contents.
// Assumes it's safe to modify the key argument to w.Delete and w.Put
// after those methods return.
func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	for _, op := range b.ops {
		keyWithoutPrefix := op.Key[len(b.db.dbPrefix):]
		if op.Delete {
			if err := w.Delete(keyWithoutPrefix); err != nil {
				return err
			}
		} else {
			if err := w.Put(keyWithoutPrefix, op.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

type iterator struct {
	database.Iterator
	db *Database

	key, val []byte
	err      error
}

// Next calls the inner iterators Next() function and strips the keys prefix
func (it *iterator) Next() bool {
	if it.db.isClosed() {
		it.key = nil
		it.val = nil
		it.err = database.ErrClosed
		return false
	}

	hasNext := it.Iterator.Next()
	if hasNext {
		key := it.Iterator.Key()
		if prefixLen := len(it.db.dbPrefix); len(key) >= prefixLen {
			key = key[prefixLen:]
		}
		it.key = key
		it.val = it.Iterator.Value()
	} else {
		it.key = nil
		it.val = nil
	}

	return hasNext
}

func (it *iterator) Key() []byte {
	return it.key
}

func (it *iterator) Value() []byte {
	return it.val
}

// Error returns [database.ErrClosed] if the underlying db was closed
// otherwise it returns the normal iterator error.
func (it *iterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.Iterator.Error()
}
