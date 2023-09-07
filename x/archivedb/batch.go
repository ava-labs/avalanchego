// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/database"
)

// A thin wrapper on top of the database.Batch which keeps track of the height.
//
// It takes any key and wraps it with dbKey. It is possible to construct many
// batches but at Write() time, the height of the batch to be written to the
// database must be the following height from the last written to the database,
// otherwise an error will be thrown
type dbBatchWithHeight struct {
	db     *archiveDB
	height uint64
	batch  database.Batch
}

// Returns the height for this Batch
func (c *dbBatchWithHeight) Height() uint64 {
	return c.height
}

// Writes the changes to the database
func (c *dbBatchWithHeight) Write() error {
	c.db.lock.Lock()
	defer c.db.lock.Unlock()

	if c.db.currentHeight+1 != c.height {
		return ErrInvalidBatchHeight
	}

	err := database.PutUInt64(c.batch, newMetaKey(dbHeight).Bytes(), c.height)
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
func (c *dbBatchWithHeight) Delete(key []byte) error {
	return c.batch.Put(newKey(key, c.height).Bytes(), []byte{1})
}

// Queues an insert for a key with a given
func (c *dbBatchWithHeight) Put(key []byte, value []byte) error {
	value = append(value, 0) // not deleted element
	return c.batch.Put(newKey(key, c.height).Bytes(), value)
}

// Returns the sizes to be committed in the database
func (c *dbBatchWithHeight) Size() int {
	return c.batch.Size()
}

// Removed all pending writes and deletes to the database
func (c *dbBatchWithHeight) Reset() {
	c.batch.Reset()
}

// Returns the inner batch
func (c *dbBatchWithHeight) Inner() database.Batch {
	return c.batch
}

func (c *dbBatchWithHeight) Replay(w database.KeyValueWriterDeleter) error {
	return c.batch.Replay(w)
}
