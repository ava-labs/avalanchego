//go:build linux && amd64 && rocksdballowed
// +build linux,amd64,rocksdballowed

// ^ Only build this file if this computer linux AND it's AMD64 AND rocksdb is allowed
// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package rocksdb

import (
	"bytes"
	"errors"
	"os"
	"runtime"
	"sync"

	"github.com/linxGnu/grocksdb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/nodb"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	MemoryBudget   = 512 * units.MiB // 512 MiB
	BitsPerKey     = 10              // 10 bits
	BlockCacheSize = 12 * units.MiB  // 12 MiB
	BlockSize      = 8 * units.KiB   // 8 KiB

	// rocksDBByteOverhead is the number of bytes of constant overhead that
	// should be added to a batch size per operation.
	rocksDBByteOverhead = 8
)

var (
	errFailedToCreateIterator = errors.New("failed to create iterator")

	_ database.Database = &Database{}
	_ database.Batch    = &batch{}
	_ database.Iterator = &iterator{}
)

// Database is a persistent key-value store. Apart from basic data storage
// functionality it also supports batch writes and iterating over the keyspace
// in binary-alphabetical order.
type Database struct {
	lock            sync.RWMutex
	db              *grocksdb.DB
	readOptions     *grocksdb.ReadOptions
	iteratorOptions *grocksdb.ReadOptions
	writeOptions    *grocksdb.WriteOptions
}

// New returns a wrapped RocksDB object.
// TODO: use configBytes to config the database options
func New(file string, configBytes []byte, log logging.Logger) (database.Database, error) {
	filter := grocksdb.NewBloomFilter(BitsPerKey)

	blockOptions := grocksdb.NewDefaultBlockBasedTableOptions()
	blockOptions.SetBlockCache(grocksdb.NewLRUCache(BlockCacheSize))
	blockOptions.SetBlockSize(BlockSize)
	blockOptions.SetFilterPolicy(filter)

	options := grocksdb.NewDefaultOptions()
	options.SetCreateIfMissing(true)
	options.OptimizeUniversalStyleCompaction(MemoryBudget)
	options.SetBlockBasedTableFactory(blockOptions)

	if err := os.MkdirAll(file, perms.ReadWriteExecute); err != nil {
		return nil, err
	}

	db, err := grocksdb.OpenDb(options, file)
	if err != nil {
		return nil, err
	}

	iteratorOptions := grocksdb.NewDefaultReadOptions()
	iteratorOptions.SetFillCache(false)

	return &Database{
		db:              db,
		readOptions:     grocksdb.NewDefaultReadOptions(),
		iteratorOptions: iteratorOptions,
		writeOptions:    grocksdb.NewDefaultWriteOptions(),
	}, nil
}

// Has returns if the key is set in the database
func (db *Database) Has(key []byte) (bool, error) {
	_, err := db.Get(key)
	switch err {
	case nil:
		return true, nil
	case database.ErrNotFound:
		return false, nil
	default:
		return false, err
	}
}

// Get returns the value the key maps to in the database
func (db *Database) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return nil, database.ErrClosed
	}

	value, err := db.db.GetBytes(db.readOptions, key)
	if err != nil {
		return nil, err
	}
	if value != nil {
		return value, nil
	}
	return nil, database.ErrNotFound
}

// Put sets the value of the provided key to the provided value
func (db *Database) Put(key []byte, value []byte) error {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return database.ErrClosed
	}
	return db.db.Put(db.writeOptions, key, value)
}

// Delete removes the key from the database
func (db *Database) Delete(key []byte) error {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return database.ErrClosed
	}
	return db.db.Delete(db.writeOptions, key)
}

// NewBatch creates a write/delete-only buffer that is atomically committed to
// the database when write is called
func (db *Database) NewBatch() database.Batch {
	b := grocksdb.NewWriteBatch()
	runtime.SetFinalizer(b, func(b *grocksdb.WriteBatch) {
		b.Destroy()
	})
	return &batch{
		batch: b,
		db:    db,
	}
}

// Inner returns itself
func (b *batch) Inner() database.Batch { return b }

// NewIterator creates a lexicographically ordered iterator over the database
func (db *Database) NewIterator() database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return &nodb.Iterator{Err: database.ErrClosed}
	}

	it := db.db.NewIterator(db.iteratorOptions)
	if it == nil {
		return &nodb.Iterator{Err: errFailedToCreateIterator}
	}
	it.Seek(nil)
	return &iterator{
		it: it,
		db: db,
	}
}

// NewIteratorWithStart creates a lexicographically ordered iterator over the
// database starting at the provided key
func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return &nodb.Iterator{Err: database.ErrClosed}
	}

	it := db.db.NewIterator(db.iteratorOptions)
	if it == nil {
		return &nodb.Iterator{Err: errFailedToCreateIterator}
	}
	it.Seek(start)
	return &iterator{
		it: it,
		db: db,
	}
}

// NewIteratorWithPrefix creates a lexicographically ordered iterator over the
// database ignoring keys that do not start with the provided prefix
func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return &nodb.Iterator{Err: database.ErrClosed}
	}

	it := db.db.NewIterator(db.iteratorOptions)
	if it == nil {
		return &nodb.Iterator{Err: errFailedToCreateIterator}
	}
	it.Seek(prefix)
	return &iterator{
		it:     it,
		db:     db,
		prefix: prefix,
	}
}

// NewIteratorWithStartAndPrefix creates a lexicographically ordered iterator
// over the database starting at start and ignoring keys that do not start with
// the provided prefix
func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return &nodb.Iterator{Err: database.ErrClosed}
	}

	it := db.db.NewIterator(db.iteratorOptions)
	if it == nil {
		return &nodb.Iterator{Err: errFailedToCreateIterator}
	}
	if bytes.Compare(start, prefix) == 1 {
		it.Seek(start)
	} else {
		it.Seek(prefix)
	}
	return &iterator{
		it:     it,
		db:     db,
		prefix: prefix,
	}
}

// Stat returns a particular internal stat of the database.
func (db *Database) Stat(property string) (string, error) {
	return "", database.ErrNotFound
}

// Compact the underlying DB for the given key range.
// Specifically, deleted and overwritten versions are discarded,
// and the data is rearranged to reduce the cost of operations
// needed to access the data. This operation should typically only
// be invoked by users who understand the underlying implementation.
//
// A nil start is treated as a key before all keys in the DB.
// And a nil limit is treated as a key after all keys in the DB.
// Therefore if both are nil then it will compact entire DB.
func (db *Database) Compact(start []byte, limit []byte) error {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return database.ErrClosed
	}

	db.db.CompactRange(grocksdb.Range{Start: start, Limit: limit})
	return nil
}

// Close implements the Database interface
func (db *Database) Close() error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.db == nil {
		return database.ErrClosed
	}

	db.readOptions.Destroy()
	db.iteratorOptions.Destroy()
	db.writeOptions.Destroy()
	db.db.Close()

	db.db = nil
	return nil
}

func (db *Database) isClosed() bool {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.db == nil
}

// batch is a wrapper around a levelDB batch to contain sizes.
type batch struct {
	batch *grocksdb.WriteBatch
	db    *Database
	size  int
}

// Put the value into the batch for later writing
func (b *batch) Put(key, value []byte) error {
	b.batch.Put(key, value)
	b.size += len(key) + len(value) + rocksDBByteOverhead
	return nil
}

// Delete the key during writing
func (b *batch) Delete(key []byte) error {
	b.batch.Delete(key)
	b.size += len(key) + rocksDBByteOverhead
	return nil
}

// Size retrieves the amount of data queued up for writing.
func (b *batch) Size() int { return b.size }

// Write flushes any accumulated data to disk.
func (b *batch) Write() error {
	b.db.lock.RLock()
	defer b.db.lock.RUnlock()

	if b.db.db == nil {
		return database.ErrClosed
	}

	return b.db.db.Write(b.db.writeOptions, b.batch)
}

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	b.batch.Clear()
	b.size = 0
}

// Replay the batch contents.
func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	it := b.batch.NewIterator()
	for it.Next() {
		rec := it.Record()
		switch rec.Type {
		case
			grocksdb.WriteBatchDeletionRecord,
			grocksdb.WriteBatchSingleDeletionRecord:
			if err := w.Delete(rec.Key); err != nil {
				return err
			}
		case grocksdb.WriteBatchValueRecord:
			if err := w.Put(rec.Key, rec.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

type iterator struct {
	it      *grocksdb.Iterator
	db      *Database
	prefix  []byte
	started bool
	key     []byte
	value   []byte
	err     error
}

// Error implements the Iterator interface
func (it *iterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.it.Err()
}

// Key implements the Iterator interface
func (it *iterator) Key() []byte {
	return utils.CopyBytes(it.key)
}

// Value implements the Iterator interface
func (it *iterator) Value() []byte {
	return utils.CopyBytes(it.value)
}

func (it *iterator) Release() {
	it.db.lock.RLock()
	defer it.db.lock.RUnlock()

	if it.db.db != nil {
		it.it.Close()
	}
}

func (it *iterator) Next() bool {
	if it.db.isClosed() {
		it.key = nil
		it.value = nil
		it.err = database.ErrClosed
		return false
	}
	if it.started {
		it.it.Next()
	}
	it.started = true

	if valid := it.it.Valid(); !valid {
		it.key = nil
		it.value = nil
		return false
	}

	it.key = it.it.Key().Data()
	it.value = it.it.Value().Data()

	if !bytes.HasPrefix(it.key, it.prefix) {
		it.key = nil
		it.value = nil
		return false
	}
	return true
}
