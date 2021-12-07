// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package corruptabledb

import (
	"sync/atomic"

	"github.com/ava-labs/avalanchego/database"
)

var (
	_ database.Database = &Database{}
	_ database.Batch    = &batch{}
)

// CorruptableDB is a wrapper around Database
// it prevents any future calls in case of a corruption occurs
type Database struct {
	database.Database
	// 1 if there was previously an error other than "not found" or "closed"
	// while performing a db operation. If [errored] == 1, Has, Get, Put,
	// Delete and batch writes fail with ErrAvoidCorruption.
	errored uint64
}

// New returns a new prefixed database
func New(db database.Database) *Database {
	return &Database{Database: db}
}

// Has returns if the key is set in the database
func (db *Database) Has(key []byte) (bool, error) {
	if db.corrupted() {
		return false, database.ErrAvoidCorruption
	}
	has, err := db.Database.Has(key)
	return has, db.handleError(err)
}

// Get returns the value the key maps to in the database
func (db *Database) Get(key []byte) ([]byte, error) {
	if db.corrupted() {
		return nil, database.ErrAvoidCorruption
	}
	value, err := db.Database.Get(key)
	return value, db.handleError(err)
}

// Put sets the value of the provided key to the provided value
func (db *Database) Put(key []byte, value []byte) error {
	if db.corrupted() {
		return database.ErrAvoidCorruption
	}
	return db.handleError(db.Database.Put(key, value))
}

// Delete removes the key from the database
func (db *Database) Delete(key []byte) error {
	if db.corrupted() {
		return database.ErrAvoidCorruption
	}
	return db.handleError(db.Database.Delete(key))
}

// Stat returns a particular internal stat of the database.
func (db *Database) Stat(property string) (string, error) {
	stat, err := db.Database.Stat(property)
	return stat, db.handleError(err)
}

func (db *Database) Compact(start []byte, limit []byte) error {
	return db.handleError(db.Database.Compact(start, limit))
}

func (db *Database) Close() error { return db.handleError(db.Database.Close()) }

func (db *Database) NewBatch() database.Batch {
	return &batch{
		Batch: db.Database.NewBatch(),
		db:    db,
	}
}

func (db *Database) corrupted() bool {
	return atomic.LoadUint64(&db.errored) == 1
}

func (db *Database) handleError(err error) error {
	switch err {
	case nil, database.ErrNotFound, database.ErrClosed:
	// If we get an error other than "not found" or "closed", disallow future
	// database operations to avoid possible corruption
	default:
		atomic.StoreUint64(&db.errored, 1)
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
	if b.db.corrupted() {
		return database.ErrAvoidCorruption
	}
	return b.db.handleError(b.Batch.Write())
}
