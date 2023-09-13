// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"errors"

	"github.com/ava-labs/avalanchego/database"
)

var (
	_ database.Batch = (*batch)(nil)

	ErrNotLongerAvailable = errors.New("batch is no longer available")
)

// A thin wrapper on top of the database.Batch that also keeps track of the height.
//
// It takes any key and wraps it with dbKey. It is possible to construct many
// batches but at Write() time, the height of the batch to be written to the
// database must be the following height from the last written to the database,
// otherwise an error will be thrown
type batch struct {
	db            *archiveDB
	ops           map[string]database.BatchOp
	size          int
	height        uint64
	inner         database.Batch
	noLongerValid bool
}

func newBatchWithHeight(db *archiveDB, height uint64) *batch {
	return &batch{
		db:            db,
		size:          0,
		height:        height,
		ops:           make(map[string]database.BatchOp),
		inner:         db.rawDB.NewBatch(),
		noLongerValid: false,
	}
}

// Returns the height for this Batch
func (c *batch) Height() uint64 {
	return c.height
}

// Writes the changes to the database
func (c *batch) Write() error {
	if c.noLongerValid {
		return ErrNotLongerAvailable
	}
	c.db.lock.Lock()
	defer c.db.lock.Unlock()

	if c.db.currentHeight+1 != c.height {
		return ErrInvalidBatchHeight
	}

	if err := c.Replay(c.inner); err != nil {
		return err
	}
	if err := c.inner.Write(); err != nil {
		return err
	}
	c.db.currentHeight = c.height
	c.Reset()              // release memory
	c.noLongerValid = true // flag as batch as no longer usable

	return nil
}

// Delete any previous state that may be stored in the database
func (c *batch) Delete(key []byte) error {
	if c.noLongerValid {
		return ErrNotLongerAvailable
	}
	rawKey := newDBKey(key, c.height)
	if value, exists := c.ops[string(rawKey)]; exists {
		// decrese the size if there was any previous key/value
		c.size -= len(value.Value)
		c.size -= len(value.Key)
	}
	c.ops[string(rawKey)] = database.BatchOp{
		Key:    rawKey,
		Value:  []byte{1},
		Delete: true,
	}
	c.size += len(rawKey) + 1
	return nil
}

// Queues an insert for a key-value pair
func (c *batch) Put(key []byte, value []byte) error {
	if c.noLongerValid {
		return ErrNotLongerAvailable
	}
	valueWithDeleteFlag := make([]byte, len(value)+1)
	offset := copy(valueWithDeleteFlag, value)
	valueWithDeleteFlag[offset] = 0 // not deleted element

	rawKey := newDBKey(key, c.height)
	if value, exists := c.ops[string(rawKey)]; exists {
		// decrese the size if there was any previous key/value
		c.size -= len(value.Value)
		c.size -= len(value.Key)
	}

	c.ops[string(key)] = database.BatchOp{
		Key:   rawKey,
		Value: valueWithDeleteFlag,
	}
	c.size += len(rawKey) + len(valueWithDeleteFlag)
	return nil
}

// Returns the sizes to be committed in the database
func (c *batch) Size() int {
	return c.size
}

// Removed all pending writes and deletes to the database
func (c *batch) Reset() {
	c.ops = make(map[string]database.BatchOp)
	c.size = 0
}

// Returns the inner batch
func (c *batch) Inner() database.Batch {
	return c
}

func (c *batch) Replay(w database.KeyValueWriterDeleter) error {
	for _, op := range c.ops {
		if err := w.Put(op.Key, op.Value); err != nil {
			return err
		}
	}
	return database.PutUInt64(w, keyHeight, c.height)
}
