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
	keyHeight             = []byte("archivedb.height")
	ErrUnknownHeight      = errors.New("unknown height")
	ErrInvalidBatchHeight = errors.New("invalid batch height")
	ErrInvalidValue       = errors.New("invalid data value")
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
	ctx context.Context

	currentHeight uint64

	rawDB database.Database

	// Must be held when reading/writing metadata such as the current database
	// height, because we allow constructing a batch for any height, but we
	// only allow to commit the next height
	lock sync.RWMutex
}

func NewArchiveDB(
	ctx context.Context,
	db database.Database,
) (*archiveDB, error) {
	height, err := database.GetUInt64(db, keyHeight)
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			return nil, err
		}
		height = 0
	}

	return &archiveDB{
		ctx:           ctx,
		currentHeight: height,
		rawDB:         db,
	}, nil
}

// Tiny wrapper on top Get() passing the last stored height
func (db *archiveDB) GetLastBlock(key []byte) ([]byte, uint64, error) {
	return db.Get(key, db.currentHeight)
}

// Returns an object which implements the database.KeyValueReader trait for all
// keys defined at the given height.
func (db *archiveDB) GetHeightReader(height uint64) (dbHeightReader, error) {
	if height > db.currentHeight || height == 0 {
		return dbHeightReader{}, ErrUnknownHeight
	}
	return dbHeightReader{
		db:                 db,
		height:             height,
		heightLastFoundKey: 0,
	}, nil
}

// Fetches the value of a given prefix at a given height.
//
// If the value does not exists or it was actually removed an error is returned.
// Otherwise a value does exists it will be returned, alongside with the height
// at which it was updated prior the requested height.
func (db *archiveDB) Get(key []byte, height uint64) ([]byte, uint64, error) {
	reader, err := db.GetHeightReader(height)
	if err != nil {
		return nil, 0, err
	}
	value, err := reader.Get(key)
	if err != nil {
		return nil, 0, err
	}

	return value, reader.heightLastFoundKey, nil
}

// Creates a new batch to append database changes in a given height
func (db *archiveDB) NewBatch(height uint64) *batch {
	return newBatchWithHeight(db, height)
}
