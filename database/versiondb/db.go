// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package versiondb

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

var (
	_ database.Database = (*Database)(nil)
	_ Commitable        = (*Database)(nil)
	_ database.Batch    = (*batch)(nil)
	_ database.Iterator = (*iterator)(nil)
)

// Commitable defines the interface that specifies that something may be
// committed.
type Commitable interface {
	// Commit writes all the queued operations to the underlying data structure.
	Commit() error
}

// Database implements the Database interface by living on top of another
// database, writing changes to the underlying database only when commit is
// called.
type Database struct {
	lock  sync.RWMutex
	mem   map[string]valueDelete
	db    database.Database
	batch database.Batch
}

type valueDelete struct {
	value  []byte
	delete bool
}

// New returns a new versioned database
func New(db database.Database) *Database {
	return &Database{
		mem:   make(map[string]valueDelete, memdb.DefaultSize),
		db:    db,
		batch: db.NewBatch(),
	}
}

func (db *Database) Has(key []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.mem == nil {
		return false, database.ErrClosed
	}
	if val, has := db.mem[string(key)]; has {
		return !val.delete, nil
	}
	return db.db.Has(key)
}

func (db *Database) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.mem == nil {
		return nil, database.ErrClosed
	}
	if val, has := db.mem[string(key)]; has {
		if val.delete {
			return nil, database.ErrNotFound
		}
		return slices.Clone(val.value), nil
	}
	return db.db.Get(key)
}

func (db *Database) Put(key, value []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.mem == nil {
		return database.ErrClosed
	}
	db.mem[string(key)] = valueDelete{value: slices.Clone(value)}
	return nil
}

func (db *Database) Delete(key []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.mem == nil {
		return database.ErrClosed
	}
	db.mem[string(key)] = valueDelete{delete: true}
	return nil
}

func (db *Database) NewBatch() database.Batch {
	return &batch{db: db}
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

func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.mem == nil {
		return &database.IteratorError{
			Err: database.ErrClosed,
		}
	}

	startString := string(start)
	prefixString := string(prefix)
	keys := make([]string, 0, len(db.mem))
	for key := range db.mem {
		if strings.HasPrefix(key, prefixString) && key >= startString {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys) // Keys need to be in sorted order
	values := make([]valueDelete, len(keys))
	for i, key := range keys {
		values[i] = db.mem[key]
	}

	return &iterator{
		db:       db,
		Iterator: db.db.NewIteratorWithStartAndPrefix(start, prefix),
		keys:     keys,
		values:   values,
	}
}

func (db *Database) Compact(start, limit []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.mem == nil {
		return database.ErrClosed
	}
	return db.db.Compact(start, limit)
}

// SetDatabase changes the underlying database to the specified database
func (db *Database) SetDatabase(newDB database.Database) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.mem == nil {
		return database.ErrClosed
	}

	db.db = newDB
	db.batch = newDB.NewBatch()
	return nil
}

// GetDatabase returns the underlying database
func (db *Database) GetDatabase() database.Database {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.db
}

// Commit writes all the operations of this database to the underlying database
func (db *Database) Commit() error {
	db.lock.Lock()
	defer db.lock.Unlock()

	batch, err := db.commitBatch()
	if err != nil {
		return err
	}
	if err := batch.Write(); err != nil {
		return err
	}
	batch.Reset()
	db.abort()
	return nil
}

// Abort all changes to the underlying database
func (db *Database) Abort() {
	db.lock.Lock()
	defer db.lock.Unlock()

	db.abort()
}

func (db *Database) abort() {
	clear(db.mem)
}

// CommitBatch returns a batch that contains all uncommitted puts/deletes.
// Calling Write() on the returned batch causes the puts/deletes to be
// written to the underlying database. The returned batch should be written before
// future calls to this DB unless the batch will never be written.
func (db *Database) CommitBatch() (database.Batch, error) {
	db.lock.Lock()
	defer db.lock.Unlock()

	return db.commitBatch()
}

// Put all of the puts/deletes in memory into db.batch
// and return the batch
func (db *Database) commitBatch() (database.Batch, error) {
	if db.mem == nil {
		return nil, database.ErrClosed
	}

	db.batch.Reset()
	for key, value := range db.mem {
		if value.delete {
			if err := db.batch.Delete([]byte(key)); err != nil {
				return nil, err
			}
		} else if err := db.batch.Put([]byte(key), value.value); err != nil {
			return nil, err
		}
	}

	return db.batch, nil
}

func (db *Database) Close() error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.mem == nil {
		return database.ErrClosed
	}
	db.batch = nil
	db.mem = nil
	db.db = nil
	return nil
}

func (db *Database) isClosed() bool {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.db == nil
}

func (db *Database) HealthCheck(ctx context.Context) (interface{}, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.mem == nil {
		return nil, database.ErrClosed
	}
	return db.db.HealthCheck(ctx)
}

type batch struct {
	database.BatchOps

	db *Database
}

func (b *batch) Write() error {
	b.db.lock.Lock()
	defer b.db.lock.Unlock()

	if b.db.mem == nil {
		return database.ErrClosed
	}

	for _, op := range b.Ops {
		b.db.mem[string(op.Key)] = valueDelete{
			value:  op.Value,
			delete: op.Delete,
		}
	}
	return nil
}

func (b *batch) Inner() database.Batch {
	return b
}

// iterator walks over both the in memory database and the underlying database
// at the same time.
type iterator struct {
	database.Iterator

	db         *Database
	key, value []byte
	err        error

	keys   []string
	values []valueDelete

	initialized, exhausted bool
}

// Next moves the iterator to the next key/value pair. It returns whether the
// iterator is exhausted. We must pay careful attention to set the proper values
// based on if the in memory db or the underlying db should be read next
func (it *iterator) Next() bool {
	// Short-circuit and set an error if the underlying database has been closed.
	if it.db.isClosed() {
		it.key = nil
		it.value = nil
		it.err = database.ErrClosed
		return false
	}

	if !it.initialized {
		it.exhausted = !it.Iterator.Next()
		it.initialized = true
	}

	for {
		switch {
		case it.exhausted && len(it.keys) == 0:
			it.key = nil
			it.value = nil
			return false
		case it.exhausted:
			nextKey := it.keys[0]
			nextValue := it.values[0]

			it.keys[0] = ""
			it.keys = it.keys[1:]
			it.values[0].value = nil
			it.values = it.values[1:]

			if !nextValue.delete {
				it.key = []byte(nextKey)
				it.value = nextValue.value
				return true
			}
		case len(it.keys) == 0:
			it.key = it.Iterator.Key()
			it.value = it.Iterator.Value()
			it.exhausted = !it.Iterator.Next()
			return true
		default:
			memKey := it.keys[0]
			memValue := it.values[0]

			dbKey := it.Iterator.Key()

			dbStringKey := string(dbKey)
			switch {
			case memKey < dbStringKey:
				it.keys[0] = ""
				it.keys = it.keys[1:]
				it.values[0].value = nil
				it.values = it.values[1:]

				if !memValue.delete {
					it.key = []byte(memKey)
					it.value = memValue.value
					return true
				}
			case dbStringKey < memKey:
				it.key = dbKey
				it.value = it.Iterator.Value()
				it.exhausted = !it.Iterator.Next()
				return true
			default:
				it.keys[0] = ""
				it.keys = it.keys[1:]
				it.values[0].value = nil
				it.values = it.values[1:]
				it.exhausted = !it.Iterator.Next()

				if !memValue.delete {
					it.key = []byte(memKey)
					it.value = memValue.value
					return true
				}
			}
		}
	}
}

func (it *iterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.Iterator.Error()
}

func (it *iterator) Key() []byte {
	return it.key
}

func (it *iterator) Value() []byte {
	return it.value
}

func (it *iterator) Release() {
	it.key = nil
	it.value = nil
	it.keys = nil
	it.values = nil
	it.Iterator.Release()
}
