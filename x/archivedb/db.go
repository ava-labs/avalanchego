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
	// Meta key that stores the current height of the database. This height is
	// being incremented everytime a new batch is inserted
	currentHeightKey = []byte("archivedb:height")
	// A secondary index is built to keep track of all the known keys. This
	// index cna be used to iterate and get all the defined keys by a given
	// prefix. The alternative, would be to read a key, then read the next key
	// by leveraging the natural ordering of keys, but that would imply random
	// reads instead of a cheap sequential read of all keys
	secondaryKeysIndexPrefix = []byte("archivedb:keys:")
	// The requested height is not yet defined therefore it is unknown to the
	// database
	ErrUnknownHeight = errors.New("unknown height")
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
	height, err := db.Get(currentHeightKey)
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

// Tiny wrapper on top Get() passing the last stored height
func (db *archiveDB) GetLastBlock(key []byte) ([]byte, uint64, error) {
	return db.Get(key, db.currentHeight)
}

// Returns an iterator with all keys defined under a given prefix. The iterator
// will return the key with the value being the height at which the key has been
// defined for the first time
func (db *archiveDB) GetKeysByPrefix(prefix []byte) keysIterator {
	uniqueKeyIndexKey := make([]byte, 0, len(secondaryKeysIndexPrefix)+len(prefix))
	uniqueKeyIndexKey = append(uniqueKeyIndexKey, secondaryKeysIndexPrefix...)
	uniqueKeyIndexKey = append(uniqueKeyIndexKey, prefix...)
	return keysIterator{
		iterator: db.rawDB.NewIteratorWithPrefix(uniqueKeyIndexKey),
	}
}

// Returns all the keys defined at a given height.
//
// The result is an iteration that will list all the defined keys at a given
// height, their value and the height at which those values were set
func (db *archiveDB) GetAllAtHeight(prefix []byte, height uint64) allKeysAtHeightIterator {
	return allKeysAtHeightIterator{
		db:          db,
		keyIterator: db.GetKeysByPrefix(prefix),
		height:      height,
		lastKey:     []byte{},
		lastValue:   []byte{},
	}
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

	internalKey := newInternalKey(key, height)
	iterator := db.rawDB.NewIteratorWithStart(internalKey.Bytes())
	keyLength := len(key)

	defer iterator.Release()

	for {
		if !iterator.Next() {
			// There is no available key with the requested prefix
			return nil, 0, database.ErrNotFound
		}

		parsedKey, err := parseKey(iterator.Key())
		if err != nil {
			return nil, 0, err
		}

		if !bytes.Equal(parsedKey.key, key) {
			if keyLength < len(parsedKey.key) {
				// The current key is a longer than the requested key, now check
				// if they match at the same length as `key`, if that is the
				// case we should continue to the next key, until the exact
				// requested key is found or anothe prefix is found and by that
				// point it would exit
				if bytes.Equal(parsedKey.key[0:keyLength], key) {
					// Same prefix, read the next key until the prefix is
					// different or the exact requested key is found
					continue
				}
			}
			// The previous key that was found does has another prefix, because the
			// iterator is not aware of prefixes. If this happens it means the
			// prefix at the requested height does not exists.
			return nil, 0, database.ErrNotFound
		}

		if parsedKey.isDeleted {
			// The database is append only, so when removing a record creates a new
			// record with an special flag is being created. Before returning the
			// value we check if the deleted flag is present or not.
			return nil, 0, database.ErrNotFound
		}

		return iterator.Value(), parsedKey.height, nil
	}
}

// Creates a new batch to append database changes in a given height
func (db *archiveDB) NewBatch() (batchWithHeight, error) {
	var nextHeightBytes [8]byte

	batch := db.rawDB.NewBatch()
	nextHeight := db.currentHeight + 1
	binary.BigEndian.PutUint64(nextHeightBytes[:], nextHeight)
	err := batch.Put(currentHeightKey, nextHeightBytes[:])
	if err != nil {
		return batchWithHeight{}, err
	}
	return batchWithHeight{
		db:     db,
		height: nextHeight,
		batch:  batch,
	}, nil
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
	internalKey := newInternalKey(key, c.height)
	internalKey.isDeleted = true
	return c.batch.Put(internalKey.Bytes(), []byte{})
}

// Queues an insert for a key with a given
func (c *batchWithHeight) Put(key []byte, value []byte) error {
	uniqueKeyIndexKey := make([]byte, 0, len(secondaryKeysIndexPrefix)+len(key))
	uniqueKeyIndexKey = append(uniqueKeyIndexKey, secondaryKeysIndexPrefix...)
	uniqueKeyIndexKey = append(uniqueKeyIndexKey, key...)

	keyAlreadyExists, err := c.db.rawDB.Has(uniqueKeyIndexKey)
	if err != nil {
		return err
	}
	if !keyAlreadyExists {
		// The key has not being seeing before
		//
		// It must be stored in the database with a special prefix. This is sort
		// of a secondary index that will allow to iterate and fetch all keys
		// given a prefix, and get all their values defined at a given height.
		//
		// This section of the code should happen once for each new key. Keys
		// updating their values should not trigger this section of the code,
		// since the sole responsibility of this secondary index is to get a
		// list of all known keys, to query by a given prefix later
		var heightBytes [8]byte
		binary.BigEndian.PutUint64(heightBytes[:], c.height)
		if err := c.batch.Put(uniqueKeyIndexKey, heightBytes[:]); err != nil {
			return err
		}
	}
	internalKey := newInternalKey(key, c.height)
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
