// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memdb

import (
	"context"
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

	prefixString := string(prefix)
	keys := make([]string, 0, len(db.db))
	for key := range db.db {
		// Keys below start stay in the snapshot, they are reachable by
		// [iterator.Prev].
		if strings.HasPrefix(key, prefixString) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys) // Keys need to be in sorted order
	values := make([][]byte, len(keys))
	for i, key := range keys {
		values[i] = db.db[key]
	}
	return &iterator{
		db:     db,
		start:  string(start),
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
	start       string
	initialized bool
	pos         int
	keys        []string
	values      [][]byte
	err         error
}

func (it *iterator) Next() bool {
	return it.move(true)
}

func (it *iterator) Prev() bool {
	return it.move(false)
}

func (it *iterator) move(forward bool) bool {
	// Short-circuit and set an error if the underlying database has been closed.
	if it.db.isClosed() {
		it.keys = nil
		it.values = nil
		it.err = database.ErrClosed
		return false
	}

	switch {
	case !it.initialized:
		it.initialized = true
		switch {
		case forward:
			// The smallest key that is at least start.
			it.pos, _ = slices.BinarySearch(it.keys, it.start)
		case it.start == "":
			it.pos = len(it.keys) - 1
		default:
			// The largest key that is strictly below start.
			at, _ := slices.BinarySearch(it.keys, it.start)
			it.pos = at - 1
		}
	case forward:
		if it.pos < len(it.keys) {
			it.pos++
		}
	default:
		if it.pos >= 0 {
			it.pos--
		}
	}
	return it.pos >= 0 && it.pos < len(it.keys)
}

func (it *iterator) Error() error {
	return it.err
}

func (it *iterator) Key() []byte {
	if it.pos >= 0 && it.pos < len(it.keys) {
		return []byte(it.keys[it.pos])
	}
	return nil
}

func (it *iterator) Value() []byte {
	if it.pos >= 0 && it.pos < len(it.values) {
		return it.values[it.pos]
	}
	return nil
}

func (it *iterator) Release() {
	it.keys = nil
	it.values = nil
}
