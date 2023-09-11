// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebble

import (
	"bytes"
	"context"
	"errors"
	"sync"

	"github.com/cockroachdb/pebble"

	"github.com/prometheus/client_golang/prometheus"

	"golang.org/x/exp/slices"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
)

// pebbleByteOverHead is the number of bytes of constant overhead that
// should be added to a batch size per operation.
const pebbleByteOverHead = 8

var (
	_ database.Database = (*Database)(nil)

	ErrInvalidOperation = errors.New("invalid operation")

	defaultCacheSize = uint64(512 * units.MiB)
	DefaultConfig    = Config{
		CacheSize:                   int(defaultCacheSize),
		BytesPerSync:                512 * units.KiB,
		WALBytesPerSync:             0, // Default to no background syncing.
		MemTableStopWritesThreshold: 8,
		MemTableSize:                defaultCacheSize / 4,
		MaxOpenFiles:                4096,
	}

	comparer = pebble.DefaultComparer
)

type Database struct {
	lock          sync.RWMutex
	pebbleDB      *pebble.DB
	closed        bool
	openIterators set.Set[*iter]
}

type Config struct {
	CacheSize                   int
	BytesPerSync                int
	WALBytesPerSync             int // 0 means no background syncing
	MemTableStopWritesThreshold int
	MemTableSize                uint64
	MaxOpenFiles                int
	MaxConcurrentCompactions    int
}

// TODO: Add support for adding a custom logger
// TODO: Add metrics
func New(file string, cfg Config, log logging.Logger, _ string, _ prometheus.Registerer) (*Database, error) {
	opts := &pebble.Options{
		Cache:                       pebble.NewCache(int64(cfg.CacheSize)),
		BytesPerSync:                cfg.BytesPerSync,
		Comparer:                    comparer,
		WALBytesPerSync:             cfg.WALBytesPerSync,
		MemTableStopWritesThreshold: cfg.MemTableStopWritesThreshold,
		MemTableSize:                cfg.MemTableSize,
		MaxOpenFiles:                cfg.MaxOpenFiles,
		MaxConcurrentCompactions:    func() int { return cfg.MaxConcurrentCompactions },
	}
	opts.Experimental.ReadSamplingMultiplier = -1 // explicitly disable seek compaction

	log.Info("opening pebble")
	log.Debug("config", zap.Any("config", opts))

	db, err := pebble.Open(file, opts)
	if err != nil {
		return nil, err
	}

	return &Database{
		pebbleDB:      db,
		openIterators: set.Set[*iter]{},
	}, nil
}

func (db *Database) Close() error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	db.closed = true

	for iter := range db.openIterators {
		iter.lock.Lock()
		iter.release()
		iter.lock.Unlock()
	}
	db.openIterators.Clear()

	return updateError(db.pebbleDB.Close())
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
	return slices.Clone(data), closer.Close()
}

func (db *Database) Put(key []byte, value []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	return updateError(db.pebbleDB.Set(key, value, pebble.Sync))
}

func (db *Database) Delete(key []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	return updateError(db.pebbleDB.Delete(key, pebble.Sync))
}

func (db *Database) Compact(start []byte, limit []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	switch {
	case db.closed:
		return database.ErrClosed
	case comparer.Compare(start, limit) >= 0:
		// pebble's Compact will no-op & error if start >= limit
		// according to pebble's comparer.
		return nil
	case limit != nil:
		return updateError(db.pebbleDB.Compact(start, limit, true /* parallelize */))
	}

	// The database.Database spec treats a nil [limit] as a key after all keys
	// but pebble treats a nil [limit] as a key before all keys.
	// Use the greatest key in the database as the [limit] to get the desired behavior.
	it, err := db.pebbleDB.NewIter(&pebble.IterOptions{})
	if err != nil {
		return updateError(err)
	}
	if it.Last() {
		if lastkey := it.Key(); lastkey != nil {
			return updateError(db.pebbleDB.Compact(start, lastkey, true /* parallelize */))
		}
	}

	// Either this database is empty or the only key in it is nil.
	return nil
}

func (db *Database) NewIterator() database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return &iter{
			db:     db,
			closed: true,
			err:    database.ErrClosed,
		}
	}

	innerIter, err := db.pebbleDB.NewIter(&pebble.IterOptions{})
	if err != nil {
		return &iter{
			db:     db,
			closed: true,
			err:    updateError(err),
		}
	}

	iter := &iter{
		db:   db,
		iter: innerIter,
	}
	db.openIterators.Add(iter)
	return iter
}

func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return &iter{
			db:     db,
			closed: true,
			err:    database.ErrClosed,
		}
	}

	innerIter, err := db.pebbleDB.NewIter(&pebble.IterOptions{
		LowerBound: start,
	})
	if err != nil {
		return &iter{
			db:     db,
			closed: true,
			err:    updateError(err),
		}
	}

	iter := &iter{
		db:   db,
		iter: innerIter,
	}
	db.openIterators.Add(iter)
	return iter
}

func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return &iter{
			db:     db,
			closed: true,
			err:    database.ErrClosed,
		}
	}

	innerIter, err := db.pebbleDB.NewIter(prefixBounds(prefix))
	if err != nil {
		return &iter{
			db:     db,
			closed: true,
			err:    updateError(err),
		}
	}

	iter := &iter{
		db:   db,
		iter: innerIter,
	}
	db.openIterators.Add(iter)
	return iter
}

func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return &iter{
			db:     db,
			closed: true,
			err:    database.ErrClosed,
		}
	}

	iterRange := prefixBounds(prefix)
	if bytes.Compare(start, prefix) == 1 {
		iterRange.LowerBound = start
	}

	innerIter, err := db.pebbleDB.NewIter(iterRange)
	if err != nil {
		return &iter{
			db:     db,
			closed: true,
			err:    updateError(err),
		}
	}

	iter := &iter{
		db:   db,
		iter: innerIter,
	}
	db.openIterators.Add(iter)
	return iter
}

// Converts a pebble-specific error to to its
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

// Returns a key range that covers all keys with the
// given [prefix].
// Assumes the Database uses bytes.Compare for key
// comparison and not a custom comparer.
func prefixBounds(prefix []byte) *pebble.IterOptions {
	var upperBound []byte
	for i := len(prefix) - 1; i >= 0; i-- {
		c := prefix[i]
		if c < 0xFF {
			upperBound = make([]byte, i+1)
			copy(upperBound, prefix)
			upperBound[i] = c + 1
			break
		}
	}
	return &pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upperBound,
	}
}
