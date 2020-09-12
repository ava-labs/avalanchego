// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prefixdb

import (
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/nodb"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// Database partitions a database into a sub-database by prefixing all keys with
// a unique value.
type Database struct {
	lock     sync.RWMutex
	dbPrefix []byte
	db       database.Database
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
	}
}

// Has implements the Database interface
func (db *Database) Has(key []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return false, database.ErrClosed
	}
	return db.db.Has(db.prefix(key))
}

// Get implements the Database interface
func (db *Database) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return nil, database.ErrClosed
	}
	return db.db.Get(db.prefix(key))
}

// Put implements the Database interface
func (db *Database) Put(key, value []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.db == nil {
		return database.ErrClosed
	}
	return db.db.Put(db.prefix(key), value)
}

// Delete implements the Database interface
func (db *Database) Delete(key []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.db == nil {
		return database.ErrClosed
	}
	return db.db.Delete(db.prefix(key))
}

// NewBatch implements the Database interface
func (db *Database) NewBatch() database.Batch {
	return &batch{
		Batch: db.db.NewBatch(),
		db:    db,
	}
}

// NewIterator implements the Database interface
func (db *Database) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

// NewIteratorWithStart implements the Database interface
func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

// NewIteratorWithPrefix implements the Database interface
func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

// NewIteratorWithStartAndPrefix implements the Database interface
func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return &nodb.Iterator{Err: database.ErrClosed}
	}
	return &iterator{
		Iterator: db.db.NewIteratorWithStartAndPrefix(db.prefix(start), db.prefix(prefix)),
		db:       db,
	}
}

// Stat implements the Database interface
func (db *Database) Stat(stat string) (string, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return "", database.ErrClosed
	}
	return db.db.Stat(stat)
}

// Compact implements the Database interface
func (db *Database) Compact(start, limit []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.db == nil {
		return database.ErrClosed
	}
	return db.db.Compact(db.prefix(start), db.prefix(limit))
}

// Close implements the Database interface
func (db *Database) Close() error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.db == nil {
		return database.ErrClosed
	}
	db.db = nil
	return nil
}

func (db *Database) prefix(key []byte) []byte {
	prefixedKey := make([]byte, len(db.dbPrefix)+len(key))
	copy(prefixedKey, db.dbPrefix)
	copy(prefixedKey[len(db.dbPrefix):], key)
	return prefixedKey
}

type keyValue struct {
	key    []byte
	value  []byte
	delete bool
}

type batch struct {
	database.Batch
	db     *Database
	writes []keyValue
}

// Put implements the Batch interface
func (b *batch) Put(key, value []byte) error {
	b.writes = append(b.writes, keyValue{utils.CopyBytes(key), utils.CopyBytes(value), false})
	return b.Batch.Put(b.db.prefix(key), value)
}

// Delete implements the Batch interface
func (b *batch) Delete(key []byte) error {
	b.writes = append(b.writes, keyValue{utils.CopyBytes(key), nil, true})
	return b.Batch.Delete(b.db.prefix(key))
}

// Write flushes any accumulated data to the memory database.
func (b *batch) Write() error {
	b.db.lock.Lock()
	defer b.db.lock.Unlock()

	if b.db.db == nil {
		return database.ErrClosed
	}

	return b.Batch.Write()
}

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	if cap(b.writes) > len(b.writes)*database.MaxExcessCapacityFactor {
		b.writes = make([]keyValue, 0, cap(b.writes)/database.CapacityReductionFactor)
	} else {
		b.writes = b.writes[:0]
	}
	b.Batch.Reset()
}

// Replay replays the batch contents.
func (b *batch) Replay(w database.KeyValueWriter) error {
	for _, keyvalue := range b.writes {
		if keyvalue.delete {
			if err := w.Delete(keyvalue.key); err != nil {
				return err
			}
		} else if err := w.Put(keyvalue.key, keyvalue.value); err != nil {
			return err
		}
	}
	return nil
}

type iterator struct {
	database.Iterator
	db *Database
}

// Key calls the inner iterators Key and strips the prefix
func (it *iterator) Key() []byte {
	key := it.Iterator.Key()
	if prefixLen := len(it.db.dbPrefix); len(key) >= prefixLen {
		return key[prefixLen:]
	}
	return key
}
