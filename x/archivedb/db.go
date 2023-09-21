// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"context"
	"errors"
	"sync"

	"github.com/ava-labs/avalanchego/database"
)

var (
	ErrNotImplemented = errors.New("feature not implemented")
	ErrUnknownHeight  = errors.New("unknown height")
	ErrInvalidValue   = errors.New("invalid data value")
)

// ArchiveDb
//
// Creates a thin database layer on top of database.Database. ArchiveDb is an
// append only database which stores all state changes happening at every block
// height. Each record is stored in such way to perform both fast inserts and selects.
//
// Currently its API is quite simple, it has two main functions, one to create a
// Batch write with a given block height, inside this batch entries can be added
// with a given value or they can be deleted. It also provides a Get function
// that takes a given key and a height.
//
//	The way it works is as follows:
//		- Height: 10
//			Set(foo, "foo's value is bar")
//			Set(bar, "bar's value is bar")
//		- Height: 100
//			Set(foo, "updatedfoo's value is bar")
//		- Height: 1000
//			Set(bar, "updated bar's value is bar")
//			Delete(foo)
//
// When requesting `Get(foo, 9)` it will return an errNotFound error because foo
// was not defined at block height 9, it was defined later. When calling
// `Get(foo, 99)` it will return a tuple `("foo's value is bar", 10)` returning
// the value of `foo` at height 99 (which was set at height 10). If requesting
// `Get(foo, 2000)` it will return an error because `foo` was deleted at height
// 1000.
type archiveDB struct {
	currentHeight uint64

	inner database.Database

	// When using the default iterator the defaultKeyLength is being used.
	//
	// The key format is,
	//
	//		[key length][raw key][height]
	//
	// Because the key length is needed, in order to iterate efficiently, a
	// default length must be given alongside with a prefix.
	//
	// This is currently not implemented, but will be implemented in a following
	// PR
	defaultKeyLength uint64

	// Must be held when reading/writing metadata such as the current database
	// height, because we allow constructing a batch for any height, but we
	// only allow to commit the next height
	lock sync.RWMutex
}

func NewArchiveDB(db database.Database) (*archiveDB, error) {
	height, err := database.GetUInt64(db, keyHeight)
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			return nil, err
		}
		height = 1
	}

	return &archiveDB{
		currentHeight:    height,
		defaultKeyLength: 1, // currently not used
		inner:            db,
	}, nil
}

// GetHeightReader returns an object which implements the
// database.KeyValueReader trait for all keys defined at the given height.
func (db *archiveDB) GetHeightReader(height uint64) (dbHeightReader, error) {
	if height > db.currentHeight || height == 0 {
		return dbHeightReader{}, ErrUnknownHeight
	}
	return dbHeightReader{
		db:     db,
		height: height,
	}, nil
}

// Get searches if a key exists at the the last known height.
//
// Use `GetHeightReader` to query at any specific height
func (db *archiveDB) Get(key []byte) ([]byte, error) {
	reader, err := db.GetHeightReader(db.currentHeight)
	if err != nil {
		return nil, err
	}

	value, _, err := reader.getValueAndHeight(key)
	return value, err
}

// GetHeight returns the last height where the given key has been updated
func (db *archiveDB) GetHeight(key []byte) (uint64, error) {
	reader, err := db.GetHeightReader(db.currentHeight)
	if err != nil {
		return 0, err
	}
	return reader.GetHeight(key)
}

// Has checks if a given key exists at the last known height.
//
// Use `GetHeightReader` to query at any specific height
func (db *archiveDB) Has(key []byte) (bool, error) {
	reader, err := db.GetHeightReader(db.currentHeight)
	if err != nil {
		return false, err
	}
	return reader.Has(key)
}

// Deletes a key at the last known height
func (db *archiveDB) Delete(key []byte) error {
	batch := db.NewBatch()
	err := batch.Delete(key)
	if err != nil {
		return err
	}
	return batch.Write()
}

func (db *archiveDB) Put(key, value []byte) error {
	batch := db.NewBatch()
	err := batch.Put(key, value)
	if err != nil {
		return err
	}
	return batch.Write()
}

// NewBatch returns a new Batch that will set or delete keys using the current height.
//
// After this batch is being used, the last height is not updated.
func (db *archiveDB) NewBatch() database.Batch {
	return newBatchWithHeight(db, db.currentHeight)
}

// NewBatchAtHeight creates a new batch to append database changes at a given height
func (db *archiveDB) NewBatchAtHeight(height uint64) *batch {
	return newBatchWithHeight(db, height)
}

func (db *archiveDB) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

func (db *archiveDB) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

func (db *archiveDB) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

func (db *archiveDB) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	return placeHolderIterator(db, db.currentHeight, db.defaultKeyLength, start, prefix)
}

func (db *archiveDB) Compact(start []byte, limit []byte) error {
	return db.inner.Compact(start, limit)
}

func (db *archiveDB) HealthCheck(ctx context.Context) (interface{}, error) {
	return db.inner.HealthCheck(ctx)
}

func (db *archiveDB) Close() error {
	return db.inner.Close()
}
