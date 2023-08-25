// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package archivedb

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"

	"github.com/ava-labs/avalanchego/database"
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
}

var (
	currentHeightKey = "archivedb.height"
	ErrUnknownHeight = errors.New("Unknown height")
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
	height, err := db.Get([]byte(currentHeightKey))
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			currentHeight = 0
		} else {
			return nil, err
		}
	} else {
		currentHeight = binary.BigEndian.Uint64(height[:])
	}

	return &archiveDB{
		ctx:           ctx,
		currentHeight: currentHeight,
		rawDB:         db,
	}, nil
}

// Tiny wrapper on top Get() passing the last stored height
func (db *archiveDB) GetLastBlock(key []byte) ([]byte, uint64, error) {
	return db.Get(key, db.currentHeight)
}

// Fetches the value of a given prefix at a given height.
//
// If the value does not exists or it was actually removed an error is returned.
// Otherwise a value does exists it will be returned, alongside with the height
// at which it was updated prior the requested height.
func (db *archiveDB) Get(key []byte, height uint64) ([]byte, uint64, error) {
	if height > db.currentHeight || height == 0 {
		return nil, db.currentHeight, ErrUnknownHeight
	}

	internalKey := newKey(key, height)
	iterator := db.rawDB.NewIteratorWithStart(internalKey.Bytes())

	defer iterator.Release()

	if !iterator.Next() {
		// There is no available key with the requested prefix
		return nil, 0, database.ErrNotFound
	}

	internalKey, err := parseKey(iterator.Key())
	if err != nil {
		return nil, 0, err
	}

	if !bytes.Equal(internalKey.Prefix, key) || internalKey.IsDeleted {
		// The current key has either a different prefix or the found key has a
		// deleted flag.
		//
		// The previous key that was found does has another prefix, because the
		// iterator is not aware of prefixes. If this happens it means the
		// prefix at the requested height does not exists.
		//
		// The database is append only, so when removing a record creates a new
		// record with an special flag is being created. Before returning the
		// value we check if the deleted flag is present or not.
		return nil, 0, database.ErrNotFound
	}

	return iterator.Value(), internalKey.Height, nil
}

// Creates a new batch to append database changes in a given height
func (db *archiveDB) NewBatch() batchWithHeight {
	var nextHeightBytes [8]byte

	batch := db.rawDB.NewBatch()
	nextHeight := db.currentHeight + 1
	binary.BigEndian.PutUint64(nextHeightBytes[:], nextHeight)
	batch.Put([]byte(currentHeightKey), nextHeightBytes[:])
	return batchWithHeight{
		db:     db,
		height: nextHeight,
		batch:  batch,
	}
}

func (c *batchWithHeight) Height() uint64 {
	return c.height
}

// Writes the changes to the database
func (c *batchWithHeight) Write() error {
	err := c.batch.Write()
	if err != nil {
		return err
	}
	c.db.currentHeight = c.height
	return nil
}

// Delete any previous state that may be stored in the database
func (c *batchWithHeight) Delete(key []byte) error {
	internalKey := newKey(key, c.height)
	internalKey.IsDeleted = true
	return c.batch.Put(internalKey.Bytes(), []byte{})
}

// Queues an insert for a key with a given
func (c *batchWithHeight) Put(key []byte, value []byte) error {
	internalKey := newKey(key, c.height)
	return c.batch.Put(internalKey.Bytes(), value)
}

// Returns the sizes to be committed in the database
func (c *batchWithHeight) Size() int {
	return c.batch.Size()
}

// Removed all pending writes and deletes to the database
func (c *batchWithHeight) Reset() {
	c.batch.Reset()
}
