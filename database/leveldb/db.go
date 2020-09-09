// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leveldb

import (
	"bytes"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"

	"github.com/ava-labs/avalanche-go/database"
	"github.com/ava-labs/avalanche-go/utils"
)

const (
	// minBlockCacheSize is the minimum number of bytes to use for block caching
	// in leveldb.
	minBlockCacheSize = 8 * opt.MiB

	// minWriteBufferSize is the minimum number of bytes to use for buffers in
	// leveldb.
	minWriteBufferSize = 8 * opt.MiB

	// minHandleCap is the minimum number of files descriptors to cap levelDB to
	// use
	minHandleCap = 16
)

// Database is a persistent key-value store. Apart from basic data storage
// functionality it also supports batch writes and iterating over the keyspace
// in binary-alphabetical order.
type Database struct{ *leveldb.DB }

// New returns a wrapped LevelDB object.
func New(file string, blockCacheSize, writeBufferSize, handleCap int) (*Database, error) {
	// Enforce minimums
	if blockCacheSize < minBlockCacheSize {
		blockCacheSize = minBlockCacheSize
	}
	if writeBufferSize < minWriteBufferSize {
		writeBufferSize = minWriteBufferSize
	}
	if handleCap < minHandleCap {
		handleCap = minHandleCap
	}

	// Open the db and recover any potential corruptions
	db, err := leveldb.OpenFile(file, &opt.Options{
		OpenFilesCacheCapacity: handleCap,
		BlockCacheCapacity:     blockCacheSize,
		// There are two buffers of size WriteBuffer used.
		WriteBuffer: writeBufferSize / 2,
		Filter:      filter.NewBloomFilter(10),
	})
	if _, corrupted := err.(*errors.ErrCorrupted); corrupted {
		db, err = leveldb.RecoverFile(file, nil)
	}
	if err != nil {
		return nil, err
	}
	return &Database{DB: db}, nil
}

// Has returns if the key is set in the database
func (db *Database) Has(key []byte) (bool, error) {
	has, err := db.DB.Has(key, nil)
	return has, updateError(err)
}

// Get returns the value the key maps to in the database
func (db *Database) Get(key []byte) ([]byte, error) {
	value, err := db.DB.Get(key, nil)
	return value, updateError(err)
}

// Put sets the value of the provided key to the provided value
func (db *Database) Put(key []byte, value []byte) error {
	return updateError(db.DB.Put(key, value, nil))
}

// Delete removes the key from the database
func (db *Database) Delete(key []byte) error { return updateError(db.DB.Delete(key, nil)) }

// NewBatch creates a write/delete-only buffer that is atomically committed to
// the database when write is called
func (db *Database) NewBatch() database.Batch { return &batch{db: db.DB} }

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
	return stat, updateError(err)
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
	return updateError(db.DB.CompactRange(util.Range{Start: start, Limit: limit}))
}

// Close implements the Database interface
func (db *Database) Close() error { return updateError(db.DB.Close()) }

// batch is a wrapper around a levelDB batch to contain sizes.
type batch struct {
	leveldb.Batch

	db   *leveldb.DB
	size int
}

// Put the value into the batch for later writing
func (b *batch) Put(key, value []byte) error {
	b.Batch.Put(key, value)
	b.size += len(value)
	return nil
}

// Delete the key during writing
func (b *batch) Delete(key []byte) error {
	b.Batch.Delete(key)
	b.size++
	return nil
}

// ValueSize retrieves the amount of data queued up for writing.
func (b *batch) ValueSize() int { return b.size }

// Write flushes any accumulated data to disk.
func (b *batch) Write() error { return updateError(b.db.Write(&b.Batch, nil)) }

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
		return updateError(err)
	}
	return updateError(replay.err)
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
