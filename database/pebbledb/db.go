// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebbledb

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	Name = "pebbledb"

	// pebbleByteOverHead is the number of bytes of constant overhead that
	// should be added to a batch size per operation.
	pebbleByteOverHead = 8

	defaultCacheSize = 512 * units.MiB
)

var (
	_ database.Database = (*Database)(nil)

	errInvalidOperation = errors.New("invalid operation")

	DefaultConfig = Config{
		CacheSize:                   defaultCacheSize,
		BytesPerSync:                512 * units.KiB,
		WALBytesPerSync:             0, // Default to no background syncing.
		MemTableStopWritesThreshold: 8,
		MemTableSize:                defaultCacheSize / 4,
		MaxOpenFiles:                4096,
		MaxConcurrentCompactions:    1,
		Sync:                        true,
	}
)

type Database struct {
	lock          sync.RWMutex
	pebbleDB      *pebble.DB
	closed        bool
	openIterators set.Set[*iter]
	writeOptions  *pebble.WriteOptions
}

type Config struct {
	CacheSize                   int64  `json:"cacheSize"`
	BytesPerSync                int    `json:"bytesPerSync"`
	WALBytesPerSync             int    `json:"walBytesPerSync"` // 0 means no background syncing
	MemTableStopWritesThreshold int    `json:"memTableStopWritesThreshold"`
	MemTableSize                uint64 `json:"memTableSize"`
	MaxOpenFiles                int    `json:"maxOpenFiles"`
	MaxConcurrentCompactions    int    `json:"maxConcurrentCompactions"`
	Sync                        bool   `json:"sync"`
}

// TODO: Add metrics
func New(file string, configBytes []byte, log logging.Logger, _ prometheus.Registerer) (database.Database, error) {
	cfg := DefaultConfig
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &cfg); err != nil {
			return nil, err
		}
	}

	opts := &pebble.Options{
		Cache:                       pebble.NewCache(cfg.CacheSize),
		BytesPerSync:                cfg.BytesPerSync,
		Comparer:                    pebble.DefaultComparer,
		WALBytesPerSync:             cfg.WALBytesPerSync,
		MemTableStopWritesThreshold: cfg.MemTableStopWritesThreshold,
		MemTableSize:                cfg.MemTableSize,
		MaxOpenFiles:                cfg.MaxOpenFiles,
		MaxConcurrentCompactions:    func() int { return cfg.MaxConcurrentCompactions },
	}
	opts.Experimental.ReadSamplingMultiplier = -1 // Disable seek compaction

	log.Info(
		"opening pebble",
		zap.Reflect("config", cfg),
	)

	db, err := pebble.Open(file, opts)
	return &Database{
		pebbleDB:      db,
		openIterators: set.Set[*iter]{},
		writeOptions:  &pebble.WriteOptions{Sync: cfg.Sync},
	}, err
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
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return database.ErrClosed
	}

	return updateError(db.pebbleDB.Set(key, value, db.writeOptions))
}

func (db *Database) Delete(key []byte) error {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return database.ErrClosed
	}

	return updateError(db.pebbleDB.Delete(key, db.writeOptions))
}

func (db *Database) Compact(start []byte, end []byte) error {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return database.ErrClosed
	}

	if end == nil {
		// The database.Database spec treats a nil [limit] as a key after all
		// keys but pebble treats a nil [limit] as a key before all keys in
		// Compact. Use the greatest key in the database as the [limit] to get
		// the desired behavior.
		it, err := db.pebbleDB.NewIter(&pebble.IterOptions{})
		if err != nil {
			return updateError(err)
		}

		if !it.Last() {
			// The database is empty.
			return it.Close()
		}

		end = slices.Clone(it.Key())
		if err := it.Close(); err != nil {
			return err
		}
	}

	if pebble.DefaultComparer.Compare(start, end) >= 1 {
		// pebble requires [start] < [end]
		return nil
	}

	return updateError(db.pebbleDB.Compact(start, end, true /* parallelize */))
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
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return &iter{
			db:     db,
			closed: true,
			err:    database.ErrClosed,
		}
	}

	it, err := db.pebbleDB.NewIter(keyRange(start, prefix))
	if err != nil {
		return &iter{
			db:     db,
			closed: true,
			err:    updateError(err),
		}
	}

	iter := &iter{
		db:   db,
		iter: it,
	}
	db.openIterators.Add(iter)
	return iter
}

// Converts a pebble-specific error to its Avalanche equivalent, if applicable.
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

func keyRange(start, prefix []byte) *pebble.IterOptions {
	opt := &pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixToUpperBound(prefix),
	}
	if pebble.DefaultComparer.Compare(start, prefix) == 1 {
		opt.LowerBound = start
	}
	return opt
}

// Returns an upper bound that stops after all keys with the given [prefix].
// Assumes the Database uses bytes.Compare for key comparison and not a custom
// comparer.
func prefixToUpperBound(prefix []byte) []byte {
	for i := len(prefix) - 1; i >= 0; i-- {
		if prefix[i] != 0xFF {
			upperBound := make([]byte, i+1)
			copy(upperBound, prefix)
			upperBound[i]++
			return upperBound
		}
	}
	return nil
}
