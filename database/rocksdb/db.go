// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leveldb

import (
	"bytes"

	"github.com/linxGnu/grocksdb"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/util"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	MemoryBudget   = 512 * 1024 * 1024 // 512 MiB
	BitsPerKey     = 10                // 10 bits
	BlockCacheSize = 8 * 1024 * 1024   // 8 MiB
	BlockSize      = 4 * 1024          // 4 KiB
)

// Database is a persistent key-value store. Apart from basic data storage
// functionality it also supports batch writes and iterating over the keyspace
// in binary-alphabetical order.
type Database struct {
	db *grocksdb.DB

	log logging.Logger

	// True if there was previously an error other than "not found" or "closed"
	// while performing a db operation. If [errored] == true, Has, Get, Put,
	// Delete and batch writes fail with ErrAvoidCorruption.
	// The node should shut down.
	errored bool
}

// New returns a wrapped RocksDB object.
func New(file string, log logging.Logger) (*Database, error) {
	filter := grocksdb.NewBloomFilter(BitsPerKey)

	blockOptions := grocksdb.NewDefaultBlockBasedTableOptions()
	blockOptions.SetBlockCache(grocksdb.NewLRUCache(BlockCacheSize))
	blockOptions.SetBlockSize(BlockSize)
	blockOptions.SetFilterPolicy(filter)

	options := grocksdb.NewDefaultOptions()
	options.SetCreateIfMissing(true)
	options.OptimizeUniversalStyleCompaction(MemoryBudget)
	options.SetBlockBasedTableFactory(blockOptions)

	db, err := grocksdb.OpenDb(options, file)
	if err != nil {
		return nil, err
	}

	return &Database{
		db:  db,
		log: log,
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
	if db.errored {
		return nil, database.ErrAvoidCorruption
	}
	value, err := db.DB.Get(key, nil)
	return value, db.handleError(err)
}

// Put sets the value of the provided key to the provided value
func (db *Database) Put(key []byte, value []byte) error {
	if db.errored {
		return database.ErrAvoidCorruption
	}
	return db.handleError(db.DB.Put(key, value, nil))
}

// Delete removes the key from the database
func (db *Database) Delete(key []byte) error {
	if db.errored {
		return database.ErrAvoidCorruption
	}
	return db.handleError(db.DB.Delete(key, nil))
}

// NewBatch creates a write/delete-only buffer that is atomically committed to
// the database when write is called
func (db *Database) NewBatch() database.Batch { return &batch{db: db} }

// NewIterator creates a lexicographically ordered iterator over the database
func (db *Database) NewIterator() database.Iterator {
	return &iter{db.DB.NewIterator(new(util.Range), nil)}
}

// NewIteratorWithStart creates a lexicographically ordered iterator over the
// database starting at the provided key
func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	return &iter{db.DB.NewIterator(&util.Range{Start: start}, nil)}
}

// NewIteratorWithPrefix creates a lexicographically ordered iterator over the
// database ignoring keys that do not start with the provided prefix
func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return &iter{db.DB.NewIterator(util.BytesPrefix(prefix), nil)}
}

// NewIteratorWithStartAndPrefix creates a lexicographically ordered iterator
// over the database starting at start and ignoring keys that do not start with
// the provided prefix
func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	iterRange := util.BytesPrefix(prefix)
	if bytes.Compare(start, prefix) == 1 {
		iterRange.Start = start
	}
	return &iter{db.DB.NewIterator(iterRange, nil)}
}

// Stat returns a particular internal stat of the database.
func (db *Database) Stat(property string) (string, error) {
	stat, err := db.DB.GetProperty(property)
	return stat, db.handleError(err)
}

// This comment is basically copy pasted from the underlying levelDB library:

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
	return db.handleError(db.DB.CompactRange(util.Range{Start: start, Limit: limit}))
}

// Close implements the Database interface
func (db *Database) Close() error { return db.handleError(db.DB.Close()) }

func (db *Database) handleError(err error) error {
	err = updateError(err)
	// If we get an error other than "not found" or "closed", disallow future
	// database operations to avoid possible corruption
	if err != nil && err != database.ErrNotFound && err != database.ErrClosed {
		db.log.Fatal("leveldb error: %w", err)
		db.errored = true
	}
	return err
}

// batch is a wrapper around a levelDB batch to contain sizes.
type batch struct {
	leveldb.Batch
	db   *Database
	size int
}

// Put the value into the batch for later writing
func (b *batch) Put(key, value []byte) error {
	b.Batch.Put(key, value)
	b.size += len(key) + len(value) + levelDBByteOverhead
	return nil
}

// Delete the key during writing
func (b *batch) Delete(key []byte) error {
	b.Batch.Delete(key)
	b.size += len(key) + levelDBByteOverhead
	return nil
}

// Size retrieves the amount of data queued up for writing.
func (b *batch) Size() int { return b.size }

// Write flushes any accumulated data to disk.
func (b *batch) Write() error {
	if b.db.errored {
		return database.ErrAvoidCorruption
	}
	return b.db.handleError(b.db.DB.Write(&b.Batch, nil))
}

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	b.Batch.Reset()
	b.size = 0
}

// Replay the batch contents.
func (b *batch) Replay(w database.KeyValueWriter) error {
	replay := &replayer{writer: w}
	if err := b.Batch.Replay(replay); err != nil {
		// Never actually returns an error, because Replay just returns nil
		return b.db.handleError(err)
	}
	return b.db.handleError(replay.err)
}

// Inner returns itself
func (b *batch) Inner() database.Batch { return b }

type replayer struct {
	writer database.KeyValueWriter
	err    error
}

func (r *replayer) Put(key, value []byte) {
	if r.err != nil {
		return
	}
	r.err = r.writer.Put(key, value)
}

func (r *replayer) Delete(key []byte) {
	if r.err != nil {
		return
	}
	r.err = r.writer.Delete(key)
}

type iter struct{ iterator.Iterator }

// Error implements the Iterator interface
func (it *iter) Error() error { return updateError(it.Iterator.Error()) }

// Key implements the Iterator interface
func (it *iter) Key() []byte { return utils.CopyBytes(it.Iterator.Key()) }

// Value implements the Iterator interface
func (it *iter) Value() []byte { return utils.CopyBytes(it.Iterator.Value()) }

func updateError(err error) error {
	switch err {
	case leveldb.ErrClosed:
		return database.ErrClosed
	case leveldb.ErrNotFound:
		return database.ErrNotFound
	default:
		return err
	}
}
