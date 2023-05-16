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
	"sync/atomic"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/exp/slices"
)

var (
	_ database.Database = (*Database)(nil)
	_ database.Batch    = (*batch)(nil)
	_ database.Iterator = (*iter)(nil)

	ErrInvalidOperation = errors.New("invalid operation")
)

type Database struct {
	db     *pebble.DB
	closed utils.Atomic[bool]
	// closeCh is closed when Close() is called.
	closeCh chan struct{}
}

type Config struct {
	CacheSize                   int // Byte
	BytesPerSync                int // Byte
	WALBytesPerSync             int // Byte (0 disables)
	MemTableStopWritesThreshold int // num tables
	MemTableSize                int // Byte
	MaxOpenFiles                int
}

func NewDefaultConfig() Config {
	return Config{
		CacheSize:                   units.GiB,
		BytesPerSync:                units.MiB,
		WALBytesPerSync:             units.MiB,
		MemTableStopWritesThreshold: 8,
		MemTableSize:                16 * units.MiB,
		MaxOpenFiles:                4 * units.KiB,
	}
}

func New(file string, cfg Config, log logging.Logger, _ string, _ prometheus.Registerer) (database.Database, error) {
	// These default settings are based on https://github.com/ethereum/go-ethereum/blob/master/ethdb/pebble/pebble.go

	opts := &pebble.Options{
		Cache:        pebble.NewCache(int64(cfg.CacheSize)),
		BytesPerSync: cfg.BytesPerSync,
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
	// https://github.com/cockroachdb/pebble/blob/master/cmd/pebble/db.go#L76-L86
	for i := 0; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		l.BlockSize = 64 * 1024
		l.IndexBlockSize = 256 * 1024
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		if i > 0 {
			l.TargetFileSize = opts.Levels[i-1].TargetFileSize * 2
		}
	}
	opts.Experimental.ReadSamplingMultiplier = -1 // explicitly disable seek compaction

	log.Info("creating pebbledb")

	db, err := pebble.Open(file, opts)
	if err != nil {
		return nil, err
	}

	return &Database{
		db:      db,
		closeCh: make(chan struct{}),
	}, nil
}

func (db *Database) Close() error {
	// close a db twice will trigger panic by pebble instead of error
	if db.closed.Get() {
		return database.ErrClosed
	}
	db.closed.Set(true)

	err := updateError(db.db.Close())
	if err != nil && strings.Contains(err.Error(), "leaked iterator") {
		// avalanche database support close db w/o error
		// even if there is an iterator which is not released
		return nil
	}
	return err
}

func (db *Database) HealthCheck(_ context.Context) (interface{}, error) {
	if db.closed.Get() {
		return nil, database.ErrClosed
	}
	return nil, nil
}

func (db *Database) Has(key []byte) (bool, error) {
	if db.closed.Get() {
		return false, database.ErrClosed
	}

	_, closer, err := db.db.Get(key)
	if err == pebble.ErrNotFound {
		return false, nil
	}

	if err != nil {
		return false, updateError(err)
	}
	return true, closer.Close()
}

// Get returns the value the key maps to in the database
func (db *Database) Get(key []byte) ([]byte, error) {
	if db.closed.Get() {
		return nil, database.ErrClosed
	}

	data, closer, err := db.db.Get(key)
	if err != nil {
		return nil, updateError(err)
	}
	ret := make([]byte, len(data))
	copy(ret, data)
	return ret, closer.Close()
}

// Put sets the value of the provided key to the provided value
func (db *Database) Put(key []byte, value []byte) error {
	// Put causes panic if the db has already been closed
	if db.closed.Get() {
		return database.ErrClosed
	}

	// Use of [pebble.NoSync] here means we don't wait for the [Set] to be
	// persisted to the WAL before returning. Basic benchmarking indicates that
	// waiting for the WAL to sync reduces performance by 20%.
	return updateError(db.db.Set(key, value, pebble.NoSync))
}

// Delete removes the key from the database
func (db *Database) Delete(key []byte) error {
	// Delete causes panic if the db has already been closed
	if db.closed.Get() {
		return database.ErrClosed
	}

	return updateError(db.db.Delete(key, pebble.NoSync))
}

// If start and limit are the same based on the compare function in pebble
// pebble returns error but we like to return nil
func (db *Database) Compact(start []byte, limit []byte) error {
	// Compact causes panic if the db has already been closed
	if db.closed.Get() {
		return database.ErrClosed
	}

	err := updateError(db.db.Compact(start, limit, true))
	if err != nil && bytes.Equal(start, limit) {
		// The default compare function of pebble is bytes.Compare.
		// Use this default compare function since we don't setup
		// the compare function when we create pebble db.
		// It's impossible to retrieve this default
		// compare function from the pebble package since
		// it's defined in internal.
		// We use bytes.Equal instead of bytes.Compare
		// because golangci_lint recommends

		// Don't return error if it is caused
		// by the same of start and limit
		// Do nothing just ignore the error
		// since there is no need to compact
		err = nil
	}
	return err
}

// batch is a wrapper around a pebbleDB batch to contain sizes.
type batch struct {
	batch *pebble.Batch
	db    *Database
	size  int

	// Support batch rewrite
	applied atomic.Bool
}

// NewBatch creates a write/delete-only buffer that is atomically committed to
// the database when write is called
func (db *Database) NewBatch() database.Batch {
	return &batch{
		db:    db,
		batch: db.db.NewBatch(),
	}
}

// Put the value into the batch for later writing
func (b *batch) Put(key, value []byte) error {
	b.size += len(key) + len(value) + 8 // TODO: find byte overhead
	return b.batch.Set(key, value, pebble.NoSync)
}

// Delete the key during writing
func (b *batch) Delete(key []byte) error {
	b.size += len(key) + 8 // TODO: find byte overhead
	return b.batch.Delete(key, pebble.NoSync)
}

// Size retrieves the amount of data queued up for writing.
func (b *batch) Size() int { return b.size }

// Write flushes any accumulated data to disk.
func (b *batch) Write() error {
	// write causes panic if the db has already been closed
	if b.db.closed.Get() {
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
		newbatch := &batch{db: b.db, batch: b.db.db.NewBatch()}

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

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	b.batch.Reset()
	b.size = 0
}

// Replay the batch contents.
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

// Inner returns itself
func (b *batch) Inner() database.Batch { return b }

type iter struct {
	db       *Database
	iter     *pebble.Iterator
	setFirst bool

	valid bool
	err   error
}

// NewIterator creates a lexicographically ordered iterator over the database
func (db *Database) NewIterator() database.Iterator {
	if db.closed.Get() {
		return &iter{
			db: db,
		}
	}

	return &iter{
		db:   db,
		iter: db.db.NewIter(&pebble.IterOptions{}),
	}
}

// NewIteratorWithStart creates a lexicographically ordered iterator over the
// database starting at the provided key
func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	if db.closed.Get() {
		return &iter{
			db: db,
		}
	}

	return &iter{
		db:   db,
		iter: db.db.NewIter(&pebble.IterOptions{LowerBound: start}),
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

// NewIteratorWithPrefix creates a lexicographically ordered iterator over the
// database ignoring keys that do not start with the provided prefix
func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	if db.closed.Get() {
		return &iter{
			db: db,
		}
	}

	return &iter{
		db:   db,
		iter: db.db.NewIter(bytesPrefix(prefix)),
	}
}

// NewIteratorWithStartAndPrefix creates a lexicographically ordered iterator
// over the database starting at start and ignoring keys that do not start with
// the provided prefix.
//
// Prefix should be some key contained within [start] or else the lower bound
// of the iteration will be overwritten with [start].
func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	if db.closed.Get() {
		return &iter{
			db: db,
		}
	}

	iterRange := bytesPrefix(prefix)
	if bytes.Compare(start, prefix) == 1 {
		iterRange.LowerBound = start
	}
	return &iter{
		db:   db,
		iter: db.db.NewIter(iterRange),
	}
}

func (it *iter) Next() bool {
	// Short-circuit and set an error if the underlying database has been closed.
	db := it.db
	if db.closed.Get() {
		it.valid = false
		it.err = database.ErrClosed
		return false
	}

	var hasNext bool
	if !it.setFirst {
		hasNext = it.iter.First()
		it.setFirst = true
	} else {
		hasNext = it.iter.Next()
	}
	it.valid = hasNext
	return hasNext
}

func (it *iter) Error() error {
	if it.err != nil {
		return it.err
	}
	return updateError(it.iter.Error())
}

func (it *iter) Key() []byte {
	if !it.valid {
		return nil
	}
	return slices.Clone(it.iter.Key())
}

func (it *iter) Value() []byte {
	if !it.valid {
		return nil
	}
	return slices.Clone(it.iter.Value())
}

func (it *iter) Release() {
	if it.db.closed.Get() {
		return
	}

	it.iter.Close()
}

// updateError casts pebble-specific errors to errors that Avalanche VMs expect
// to see (they do not know which type of db may be provided).
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
