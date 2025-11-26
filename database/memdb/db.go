// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memdb

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/ava-labs/avalanchego/database"
)

const (
	// Name is the name of this database for database switches
	Name = "memdb"

	// DefaultSize is the default initial size of the memory database
	DefaultSize = 1024
)

var (
	_ database.Database = (*Database)(nil)
	_ database.Batch    = (*batch)(nil)
	_ database.Iterator = (*iterator)(nil)
)

// Database is an ephemeral key-value store that implements the Database
// interface.
type Database struct {
	lock sync.RWMutex
	db   map[string][]byte
}

// New returns a map with the Database interface methods implemented.
func New() *Database {
	return NewWithSize(DefaultSize)
}

// Copy returns a Database with the same key-value pairs as db
func Copy(db *Database) (*Database, error) {
	db.lock.Lock()
	defer db.lock.Unlock()

	result := New()
	for k, v := range db.db {
		if err := result.Put([]byte(k), v); err != nil {
			return nil, fmt.Errorf("failed to insert key: %w", err)
		}
	}

	return result, nil
}

// NewWithSize returns a map pre-allocated to the provided size with the
// Database interface methods implemented.
func NewWithSize(size int) *Database {
	return &Database{db: make(map[string][]byte, size)}
}

func (db *Database) Close() error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.db == nil {
		return database.ErrClosed
	}
	db.db = nil
	return nil
}

func (db *Database) isClosed() bool {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.db == nil
}

func (db *Database) Has(key []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return false, database.ErrClosed
	}
	_, ok := db.db[string(key)]
	return ok, nil
}

func (db *Database) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return nil, database.ErrClosed
	}
	if entry, ok := db.db[string(key)]; ok {
		return slices.Clone(entry), nil
	}
	return nil, database.ErrNotFound
}

func (db *Database) Put(key []byte, value []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.db == nil {
		return database.ErrClosed
	}
	db.db[string(key)] = slices.Clone(value)
	return nil
}

func (db *Database) Delete(key []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.db == nil {
		return database.ErrClosed
	}
	delete(db.db, string(key))
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

	if db.db == nil {
		return &database.IteratorError{
			Err: database.ErrClosed,
		}
	}

	startString := string(start)
	prefixString := string(prefix)
	keys := make([]string, 0, len(db.db))
	for key := range db.db {
		if strings.HasPrefix(key, prefixString) && key >= startString {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys) // Keys need to be in sorted order
	values := make([][]byte, 0, len(keys))
	for _, key := range keys {
		values = append(values, db.db[key])
	}
	return &iterator{
		db:     db,
		keys:   keys,
		values: values,
	}
}

func (db *Database) Compact(_, _ []byte) error {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return database.ErrClosed
	}
	return nil
}

func (db *Database) HealthCheck(context.Context) (interface{}, error) {
	if db.isClosed() {
		return nil, database.ErrClosed
	}
	return nil, nil
}

type batch struct {
	database.BatchOps

	db *Database
}

func (b *batch) Write() error {
	b.db.lock.Lock()
	defer b.db.lock.Unlock()

	if b.db.db == nil {
		return database.ErrClosed
	}

	for _, op := range b.Ops {
		if op.Delete {
			delete(b.db.db, string(op.Key))
		} else {
			b.db.db[string(op.Key)] = op.Value
		}
	}
	return nil
}

func (b *batch) Inner() database.Batch {
	return b
}

type iterator struct {
	db          *Database
	initialized bool
	keys        []string
	values      [][]byte
	err         error
}

func (it *iterator) Next() bool {
	// Short-circuit and set an error if the underlying database has been closed.
	if it.db.isClosed() {
		it.keys = nil
		it.values = nil
		it.err = database.ErrClosed
		return false
	}

	// If the iterator was not yet initialized, do it now
	if !it.initialized {
		it.initialized = true
		return len(it.keys) > 0
	}
	// Iterator already initialize, advance it
	if len(it.keys) > 0 {
		it.keys[0] = ""
		it.keys = it.keys[1:]
		it.values[0] = nil
		it.values = it.values[1:]
	}
	return len(it.keys) > 0
}

func (it *iterator) Error() error {
	return it.err
}

func (it *iterator) Key() []byte {
	if len(it.keys) > 0 {
		return []byte(it.keys[0])
	}
	return nil
}

func (it *iterator) Value() []byte {
	if len(it.values) > 0 {
		return it.values[0]
	}
	return nil
}

func (it *iterator) Release() {
	it.keys = nil
	it.values = nil
}
