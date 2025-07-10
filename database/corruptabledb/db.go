// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package corruptabledb

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	_ database.Database = (*Database)(nil)
	_ database.Batch    = (*batch)(nil)
)

// CorruptableDB is a wrapper around Database
// it prevents any future calls in case of a corruption occurs
type Database struct {
	database.Database

	log logging.Logger
	// initialError stores the error other than "not found" or "closed" while
	// performing a db operation. If not nil, Has, Get, Put, Delete and batch
	// writes will fail with initialError.
	errorLock    sync.RWMutex
	initialError error
}

// New returns a new prefixed database
func New(db database.Database, log logging.Logger) *Database {
	return &Database{
		Database: db,
		log:      log,
	}
}

// Has returns if the key is set in the database
func (db *Database) Has(key []byte) (bool, error) {
	if err := db.corrupted(); err != nil {
		return false, err
	}
	has, err := db.Database.Has(key)
	return has, db.handleError(err)
}

// Get returns the value the key maps to in the database
func (db *Database) Get(key []byte) ([]byte, error) {
	if err := db.corrupted(); err != nil {
		return nil, err
	}
	value, err := db.Database.Get(key)
	return value, db.handleError(err)
}

// Put sets the value of the provided key to the provided value
func (db *Database) Put(key []byte, value []byte) error {
	if err := db.corrupted(); err != nil {
		return err
	}
	return db.handleError(db.Database.Put(key, value))
}

// Delete removes the key from the database
func (db *Database) Delete(key []byte) error {
	if err := db.corrupted(); err != nil {
		return err
	}
	return db.handleError(db.Database.Delete(key))
}

func (db *Database) Compact(start []byte, limit []byte) error {
	return db.handleError(db.Database.Compact(start, limit))
}

func (db *Database) Close() error {
	return db.handleError(db.Database.Close())
}

func (db *Database) HealthCheck(ctx context.Context) (interface{}, error) {
	if err := db.corrupted(); err != nil {
		return nil, err
	}
	return db.Database.HealthCheck(ctx)
}

func (db *Database) NewBatch() database.Batch {
	return &batch{
		Batch: db.Database.NewBatch(),
		db:    db,
	}
}

func (db *Database) NewIterator() database.Iterator {
	return &iterator{
		Iterator: db.Database.NewIterator(),
		db:       db,
	}
}

func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	return &iterator{
		Iterator: db.Database.NewIteratorWithStart(start),
		db:       db,
	}
}

func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return &iterator{
		Iterator: db.Database.NewIteratorWithPrefix(prefix),
		db:       db,
	}
}

func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	return &iterator{
		Iterator: db.Database.NewIteratorWithStartAndPrefix(start, prefix),
		db:       db,
	}
}

func (db *Database) corrupted() error {
	db.errorLock.RLock()
	defer db.errorLock.RUnlock()

	return db.initialError
}

func (db *Database) handleError(err error) error {
	switch err {
	case nil, database.ErrNotFound, database.ErrClosed:
	// If we get an error other than "not found" or "closed", disallow future
	// database operations to avoid possible corruption
	default:
		db.errorLock.Lock()
		defer db.errorLock.Unlock()

		// Set the initial error to the first unexpected error. Don't call
		// corrupted() here since it would deadlock.
		if db.initialError == nil {
			db.log.Error(
				"closing database to avoid possible corruption",
				zap.Error(err),
			)

			db.initialError = fmt.Errorf("closed to avoid possible corruption, init error: %w", err)
		}
	}
	return err
}

// batch is a wrapper around the batch to contain sizes.
type batch struct {
	database.Batch
	db *Database
}

// Write flushes any accumulated data to disk.
func (b *batch) Write() error {
	if err := b.db.corrupted(); err != nil {
		return err
	}
	return b.db.handleError(b.Batch.Write())
}

type iterator struct {
	database.Iterator
	db *Database
}

func (it *iterator) Next() bool {
	if err := it.db.corrupted(); err != nil {
		return false
	}
	val := it.Iterator.Next()
	_ = it.db.handleError(it.Iterator.Error())
	return val
}

func (it *iterator) Error() error {
	if err := it.db.corrupted(); err != nil {
		return err
	}
	return it.db.handleError(it.Iterator.Error())
}
