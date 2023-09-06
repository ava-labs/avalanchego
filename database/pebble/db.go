// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebble

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"

	"github.com/prometheus/client_golang/prometheus"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	// pebbleByteOverHead is the number of bytes of constant overhead that
	// should be added to a batch size per operation.
	pebbleByteOverHead = 8

	blockSize      = 64 * units.KiB
	indexBlockSize = 256 * units.KiB
	filterPolicy   = bloom.FilterPolicy(10)
)

var (
	_ database.Database = (*Database)(nil)
	_ database.Batch    = (*batch)(nil)
	_ database.Iterator = (*iter)(nil)

	ErrInvalidOperation = errors.New("invalid operation")

	DefaultConfig = Config{
		CacheSize:                   units.GiB,
		BytesPerSync:                units.MiB,
		WALBytesPerSync:             units.MiB,
		MemTableStopWritesThreshold: 8,
		MemTableSize:                16 * units.MiB,
		MaxOpenFiles:                4 * units.KiB,
	}

	comparer = pebble.DefaultComparer
)

type Database struct {
	lock     sync.RWMutex
	pebbleDB *pebble.DB
	closed   bool
}

type Config struct {
	CacheSize                   int // Byte
	BytesPerSync                int // Byte
	WALBytesPerSync             int // Byte (0 disables)
	MemTableStopWritesThreshold int // num tables
	MemTableSize                int // Byte
	MaxOpenFiles                int
}

func New(file string, cfg Config, log logging.Logger, _ string, _ prometheus.Registerer) (database.Database, error) {
	// Original default settings are based on
	// https://github.com/ethereum/go-ethereum/blob/release/1.11/ethdb/pebble/pebble.go
	opts := &pebble.Options{
		Cache:        pebble.NewCache(int64(cfg.CacheSize)),
		BytesPerSync: cfg.BytesPerSync,
		Comparer:     comparer,
		// Although we use `pebble.NoSync`, we still keep the WAL enabled. Pebble
		// will fsync the WAL during shutdown and should ensure the db is
		// recoverable if shutdown correctly.
		WALBytesPerSync:             cfg.WALBytesPerSync,
		MemTableStopWritesThreshold: cfg.MemTableStopWritesThreshold,
		MemTableSize:                cfg.MemTableSize,
		MaxOpenFiles:                cfg.MaxOpenFiles,
		MaxConcurrentCompactions:    runtime.NumCPU,
		Levels:                      make([]pebble.LevelOptions, 7),
		// TODO: add support for adding a custom logger
	}

	// Default configuration sourced from:
	// https://github.com/cockroachdb/pebble/blob/crl-release-23.1/cmd/pebble/db.go
	for i := 0; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		l.BlockSize = blockSize
		l.IndexBlockSize = indexBlockSize
		l.FilterPolicy = filterPolicy
		l.FilterType = pebble.TableFilter
		if i > 0 {
			l.TargetFileSize = opts.Levels[i-1].TargetFileSize * 2
		}
	}
	opts.Experimental.ReadSamplingMultiplier = -1 // explicitly disable seek compaction

	log.Info("opening pebble")

	db, err := pebble.Open(file, opts)
	if err != nil {
		return nil, err
	}

	return &Database{
		pebbleDB: db,
	}, nil
}

func (db *Database) Close() error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	db.closed = true

	// TODO: Address the following comment from pebbledb.Close:
	// "It is not safe to close a DB until all outstanding iterators are closed or to call
	// Close concurrently with any other DB method. It is not valid to call any of a DB's
	// methods after the DB has been closed.""
	err := updateError(db.pebbleDB.Close())
	if err != nil && strings.Contains(err.Error(), "leaked iterator") {
		// avalanche database support close db w/o error
		// even if there is an iterator which is not released
		// TODO: This is a fragile way to detect "leaked iterator". Try to
		// find a better way to do it.
		return nil
	}
	return err
}

func (db *Database) HealthCheck(_ context.Context) (interface{}, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}
	return nil, nil
}

func (db *Database) Has(key []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return false, database.ErrClosed
	}

	_, closer, err := db.pebbleDB.Get(key)
	if err == pebble.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, updateError(err)
	}
	return true, closer.Close()
}

func (db *Database) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}

	data, closer, err := db.pebbleDB.Get(key)
	if err != nil {
		return nil, updateError(err)
	}
	dataClone := slices.Clone(data)
	return dataClone, closer.Close()
}

func (db *Database) Put(key []byte, value []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	// Use of [pebble.NoSync] here means we don't wait for the [Set] to be
	// persisted to the WAL before returning. Basic benchmarking indicates that
	// waiting for the WAL to sync reduces performance by 20%.
	return updateError(db.pebbleDB.Set(key, value, pebble.NoSync))
}

func (db *Database) Delete(key []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	return updateError(db.pebbleDB.Delete(key, pebble.NoSync))
}

func (db *Database) Compact(start []byte, limit []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	if comparer.Compare(start, limit) >= 0 {
		// pebble's Compact will no-op & error if start >= limit
		// according to pebble's comparer.
		return nil
	}

	if limit != nil {
		return updateError(db.pebbleDB.Compact(start, limit, true /* parallelize */))
	}

	// The database.Database spec says that a nil [limit] is treated as a key after all keys.
	// But pebble treats a nil [limit] as being the key before all keys.
	// Use the greatest key in the database as the [limit] to get the desired behavior.
	it := db.pebbleDB.NewIter(&pebble.IterOptions{})
	if it.Last() {
		if lastkey := it.Key(); lastkey != nil {
			return updateError(db.pebbleDB.Compact(start, lastkey, true /* parallelize */))
		}
	}

	// Either this database is empty or the only key in it is nil.
	return nil
}

// batch is a wrapper around a pebbleDB batch to contain sizes.
type batch struct {
	batch *pebble.Batch
	db    *Database
	size  int

	// Support batch rewrite
	applied atomic.Bool
}

func (db *Database) NewBatch() database.Batch {
	return &batch{
		db:    db,
		batch: db.pebbleDB.NewBatch(),
	}
}

func (b *batch) Put(key, value []byte) error {
	b.size += len(key) + len(value) + pebbleByteOverHead
	return b.batch.Set(key, value, pebble.NoSync)
}

func (b *batch) Delete(key []byte) error {
	b.size += len(key) + pebbleByteOverHead
	return b.batch.Delete(key, pebble.NoSync)
}

func (b *batch) Size() int { return b.size }

func (b *batch) Write() error {
	b.db.lock.Lock() // TODO do we need write lock?
	defer b.db.lock.Unlock()

	if b.db.closed {
		return database.ErrClosed
	}

	// Support batch rewrite
	// The underlying pebble db doesn't support batch rewrites but panics instead
	// We have to create a new batch which is a kind of duplicate of the given
	// batch(arg b) and commit this new batch on behalf of the given batch.
	if b.applied.Load() {
		// the given batch b has already been committed
		// Don't Commit it again, got panic otherwise
		// Create a new batch to do Commit
		newbatch := &batch{
			db:    b.db,
			batch: b.db.pebbleDB.NewBatch(),
		}

		// duplicate b.batch to newbatch.batch
		if err := newbatch.batch.Apply(b.batch, nil); err != nil {
			return err
		}
		return updateError(newbatch.batch.Commit(pebble.NoSync))
	}
	// mark it for alerady committed
	b.applied.Store(true)

	return updateError(b.batch.Commit(pebble.NoSync))
}

func (b *batch) Reset() {
	b.batch.Reset()
	b.size = 0
}

func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	reader := b.batch.Reader()
	for {
		kind, k, v, ok := reader.Next()
		if !ok {
			return nil
		}
		switch kind {
		case pebble.InternalKeyKindSet:
			if err := w.Put(k, v); err != nil {
				return err
			}
		case pebble.InternalKeyKindDelete:
			if err := w.Delete(k); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: %v", ErrInvalidOperation, kind)
		}
	}
}

func (b *batch) Inner() database.Batch { return b }

func (db *Database) NewIterator() database.Iterator {
	db.lock.Lock() // TODO do we need write lock?
	defer db.lock.Unlock()

	if db.closed {
		return &iter{
			db:     db,
			closed: true,
			err:    database.ErrClosed,
		}
	}

	return &iter{
		db:   db,
		iter: db.pebbleDB.NewIter(&pebble.IterOptions{}),
	}
}

func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	db.lock.Lock() // TODO do we need write lock?
	defer db.lock.Unlock()

	if db.closed {
		return &iter{
			db:     db,
			closed: true,
			err:    database.ErrClosed,
		}
	}

	return &iter{
		db:   db,
		iter: db.pebbleDB.NewIter(&pebble.IterOptions{LowerBound: start}),
	}
}

// bytesPrefix returns key range that satisfy the given prefix.
// This only applicable for the standard 'bytes comparer'.
func bytesPrefix(prefix []byte) *pebble.IterOptions {
	var limit []byte
	for i := len(prefix) - 1; i >= 0; i-- {
		c := prefix[i]
		if c < 0xff {
			limit = make([]byte, i+1)
			copy(limit, prefix)
			limit[i] = c + 1
			break
		}
	}
	return &pebble.IterOptions{LowerBound: prefix, UpperBound: limit}
}

func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	db.lock.Lock() // TODO do we need write lock?
	defer db.lock.Unlock()

	if db.closed {
		return &iter{
			db:     db,
			closed: true,
			err:    database.ErrClosed,
		}
	}

	return &iter{
		db:   db,
		iter: db.pebbleDB.NewIter(bytesPrefix(prefix)),
	}
}

// NewIteratorWithStartAndPrefix creates a lexicographically ordered iterator
// over the database starting at start and ignoring keys that do not start with
// the provided prefix.
//
// Prefix should be some key contained within [start] or else the lower bound
// of the iteration will be overwritten with [start].
func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	db.lock.Lock() // TODO do we need write lock?
	defer db.lock.Unlock()

	if db.closed {
		return &iter{
			db:     db,
			closed: true,
			err:    database.ErrClosed,
		}
	}

	iterRange := bytesPrefix(prefix)
	if bytes.Compare(start, prefix) == 1 {
		iterRange.LowerBound = start
	}
	return &iter{
		db:   db,
		iter: db.pebbleDB.NewIter(iterRange),
	}
}

// updateError converts a pebble-specific error to to its
// Avalanche equivalent, if applicable.
func updateError(err error) error {
	switch err {
	case pebble.ErrClosed:
		return database.ErrClosed
	case pebble.ErrNotFound:
		return database.ErrNotFound
	default:
		return err
	}
}
