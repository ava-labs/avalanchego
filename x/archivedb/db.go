// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database"
)

var (
	_ database.Batch          = (*batchWithHeight)(nil)
	_ database.KeyValueReader = (*dbHeightReader)(nil)
	_ dbKey                   = (*keyInternal)(nil)
	_ dbKey                   = (*dbMetaKey)(nil)
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
	// height, because we allow our to construct a batch for any height, but we
	// only allow to commit the next height
	lock sync.RWMutex
}

var (
	dbHeight              = []byte("archivedb.height")
	ErrUnknownHeight      = errors.New("unknown height")
	ErrInvalidBatchHeight = errors.New("invalid batch height")
)

type batchWithHeight struct {
	db     *archiveDB
	height uint64
	batch  database.Batch
}

func NewArchiveDB(
	ctx context.Context,
	db database.Database,
) (*archiveDB, error) {
	var currentHeight uint64
	height, err := db.Get(newMetaKey(dbHeight).Bytes())
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			return nil, err
		}
		currentHeight = 0
	} else {
		currentHeight = binary.BigEndian.Uint64(height)
	}

	return &archiveDB{
		ctx:           ctx,
		currentHeight: currentHeight,
		rawDB:         db,
	}, nil
}

func (db *archiveDB) GetMetadata(key []byte) ([]byte, error) {
	fmt.Printf("%v %v\n", key, newMetaKey(key).Bytes())
	return db.rawDB.Get(newMetaKey(key).Bytes())
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
func (db *archiveDB) NewBatch(height uint64) (batchWithHeight, error) {
	batch := db.rawDB.NewBatch()
	return batchWithHeight{
		db:     db,
		height: height,
		batch:  batch,
	}, nil
}

func (c *batchWithHeight) Height() uint64 {
	return c.height
}

// Writes the changes to the database
func (c *batchWithHeight) Write() error {
	c.db.lock.Lock()
	defer c.db.lock.Unlock()

	if c.db.currentHeight+1 != c.height {
		return ErrInvalidBatchHeight
	}

	var newHeightBytes [8]byte
	binary.BigEndian.PutUint64(newHeightBytes[:], c.height)
	err := c.batch.Put(newMetaKey(dbHeight).Bytes(), newHeightBytes[:])
	if err != nil {
		return err
	}

	err = c.batch.Write()
	if err != nil {
		return err
	}
	c.db.currentHeight = c.height
	return nil
}

// Delete any previous state that may be stored in the database
func (c *batchWithHeight) Delete(key []byte) error {
	return c.batch.Put(newKey(key, c.height).Bytes(), []byte{1})
}

// Queues an insert for a key with a given
func (c *batchWithHeight) Put(key []byte, value []byte) error {
	value = append(value, 0) // not deleted element
	return c.batch.Put(newKey(key, c.height).Bytes(), value)
}

// Returns the sizes to be committed in the database
func (c *batchWithHeight) Size() int {
	return c.batch.Size()
}

// Removed all pending writes and deletes to the database
func (c *batchWithHeight) Reset() {
	c.batch.Reset()
}

func (c *batchWithHeight) Inner() database.Batch {
	return c.batch
}

func (c *batchWithHeight) Replay(w database.KeyValueWriterDeleter) error {
	return c.batch.Replay(w)
}
