// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leveldb

import (
	"bytes"
	"sync/atomic"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	// Name is the name of this database for database switches
	Name = "level"

	// BlockCacheSize is the number of bytes to use for block caching in
	// leveldb.
	BlockCacheSize = 12 * opt.MiB

	// WriteBufferSize is the number of bytes to use for buffers in leveldb.
	WriteBufferSize = 12 * opt.MiB

	// HandleCap is the number of files descriptors to cap levelDB to use.
	HandleCap = 64

	// BitsPerKey is the number of bits to add to the bloom filter per key.
	BitsPerKey = 10

	// levelDBByteOverhead is the number of bytes of constant overhead that
	// should be added to a batch size per operation.
	levelDBByteOverhead = 8
)

// Database is a persistent key-value store. Apart from basic data storage
// functionality it also supports batch writes and iterating over the keyspace
// in binary-alphabetical order.
type Database struct {
	*leveldb.DB
	log logging.Logger

	// 1 if there was previously an error other than "not found" or "closed"
	// while performing a db operation. If [errored] == 1, Has, Get, Put,
	// Delete and batch writes fail with ErrAvoidCorruption.
	errored uint64
}

// New returns a wrapped LevelDB object.
func New(file string, log logging.Logger) (*Database, error) {
	// Open the db and recover any potential corruptions
	db, err := leveldb.OpenFile(file, &opt.Options{
		OpenFilesCacheCapacity: HandleCap,
		BlockCacheCapacity:     BlockCacheSize,
		// There are two buffers of size WriteBuffer used.
		WriteBuffer: WriteBufferSize / 2,
		Filter:      filter.NewBloomFilter(BitsPerKey),
	})
	if _, corrupted := err.(*errors.ErrCorrupted); corrupted {
		db, err = leveldb.RecoverFile(file, nil)
	}
	if err != nil {
		return nil, err
	}
	return &Database{
		DB:  db,
		log: log,
	}, nil
}

// Has returns if the key is set in the database
func (db *Database) Has(key []byte) (bool, error) {
	if db.corrupted() {
		return false, database.ErrAvoidCorruption
	}
	has, err := db.DB.Has(key, nil)
	return has, db.handleError(err)
}

// Get returns the value the key maps to in the database
func (db *Database) Get(key []byte) ([]byte, error) {
	if db.corrupted() {
		return nil, database.ErrAvoidCorruption
	}
	value, err := db.DB.Get(key, nil)
	return value, db.handleError(err)
}

// Put sets the value of the provided key to the provided value
func (db *Database) Put(key []byte, value []byte) error {
	if db.corrupted() {
		return database.ErrAvoidCorruption
	}
	return db.handleError(db.DB.Put(key, value, nil))
}

// Delete removes the key from the database
func (db *Database) Delete(key []byte) error {
	if db.corrupted() {
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

func (db *Database) corrupted() bool {
	return atomic.LoadUint64(&db.errored) == 1
}

func (db *Database) handleError(err error) error {
	err = updateError(err)
	switch err {
	case nil, database.ErrNotFound, database.ErrClosed:
	// If we get an error other than "not found" or "closed", disallow future
	// database operations to avoid possible corruption
	default:
		db.log.Fatal("leveldb error: %s", err)
		atomic.StoreUint64(&db.errored, 1)
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
	if b.db.corrupted() {
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
		return err
	}
	return replay.err
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
