// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"context"
	"errors"
	"io"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/database"
)

var (
	ErrNotImplemented = errors.New("feature not implemented")
	ErrInvalidValue   = errors.New("invalid data value")

	_ database.Compacter = (*Database)(nil)
	_ health.Checker     = (*Database)(nil)
	_ io.Closer          = (*Database)(nil)
)

// Database implements an ArchiveDB on top of a database.Database. An ArchiveDB
// is an append only database which stores all state changes happening at every
// height. Each record is stored in such way to perform both fast insertions and
// lookups.
//
// The API is quite simple, it has two main functions, one to create a Batch
// write with a given height, inside this batch entries can be added with a
// given value or they can be deleted.
//
//	The way it works is as follows:
//		- NewBatch(10)
//			batch.Put(foo, "foo's value is bar")
//			batch.Put(bar, "bar's value is bar")
//		- NewBatch(100)
//			batch.Put(foo, "updatedfoo's value is bar")
//		- NewBatch(1000)
//			batch.Put(bar, "updated bar's value is bar")
//			batch.Delete(foo)
//
// The other primary function is to read data at a given height.
//
//	The way it works is as follows:
//		- Open(10)
//			reader.Get(foo)
//			reader.Get(bar)
//		- Open(99)
//			reader.GetHeight(foo)
//		- Open(100)
//			reader.Get(foo)
//		- Open(1000)
//			reader.Get(foo)
//
// Requesting `reader.Get(foo)` at height 1000 will return ErrNotFound because
// foo was deleted at height 1000. When calling `reader.GetHeight(foo)` at
// height 99 it will return a tuple `("foo's value is bar", 10)` returning the
// value of `foo` at height 99 (which was set at height 10).
type Database struct {
	db database.Database
}

func New(db database.Database) *Database {
	return &Database{
		db: db,
	}
}

// Height returns the last written height.
func (db *Database) Height() (uint64, error) {
	return database.GetUInt64(db.db, heightKey)
}

// Open returns a reader for the state at the given height.
func (db *Database) Open(height uint64) *Reader {
	return &Reader{
		db:     db,
		height: height,
	}
}

// NewBatch creates a write batch to perform changes at a given height.
//
// Note: Committing multiple batches at the same height, or at a lower height
// than the currently committed height will not error. It is left up to the
// caller to enforce any guarantees they need around height consistency.
func (db *Database) NewBatch(height uint64) *batch {
	return &batch{
		db:     db,
		height: height,
	}
}

func (db *Database) Compact(start []byte, limit []byte) error {
	return db.db.Compact(start, limit)
}

func (db *Database) HealthCheck(ctx context.Context) (interface{}, error) {
	return db.db.HealthCheck(ctx)
}

func (db *Database) Close() error {
	return db.db.Close()
}
