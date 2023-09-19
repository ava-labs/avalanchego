// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"bytes"

	"github.com/ava-labs/avalanchego/database"
)

var _ database.Batch = (*batch)(nil)

// A thin wrapper on top of the database.Batch that also keeps track of the height.
//
// It takes any key and wraps it with dbKey. It is possible to construct many
// batches but at Write() time, the height of the batch to be written to the
// database must be the following height from the last written to the database,
// otherwise an error will be thrown
type batch struct {
	db     *archiveDB
	ops    []database.BatchOp
	size   int
	height uint64
	inner  database.Batch
}

// newBatchWithHeight returns a batch struct which implements database.Batch
//
// This batch will write keys onto ArchiveDb associating it the current height.
func newBatchWithHeight(db *archiveDB, height uint64) *batch {
	return &batch{
		db:     db,
		size:   0,
		height: height,
		ops:    make([]database.BatchOp, 0),
		inner:  db.rawDB.NewBatch(),
	}
}

// Height returns the height for this Batch
func (c *batch) Height() uint64 {
	return c.height
}

// Writes the changes to the database
func (c *batch) Write() error {
	c.db.lock.Lock()
	defer c.db.lock.Unlock()

	if c.height == 0 || c.db.currentHeight != c.height && c.db.currentHeight+1 != c.height {
		return ErrInvalidBatchHeight
	}

	for _, op := range c.ops {
		if err := c.inner.Put(op.Key, op.Value); err != nil {
			return err
		}
	}
	if err := database.PutUInt64(c.inner, keyHeight, c.height); err != nil {
		return err
	}

	if err := c.inner.Write(); err != nil {
		return err
	}
	c.db.currentHeight = c.height

	return nil
}

// Delete any previous state that may be stored in the database
func (c *batch) Delete(key []byte) error {
	rawKey := newDBKey(key, c.height)
	c.ops = append(c.ops, database.BatchOp{
		Key:    rawKey,
		Value:  []byte{1},
		Delete: true,
	})
	c.size += len(rawKey) + 1
	return nil
}

// Queues an insert for a key-value pair
func (c *batch) Put(key []byte, value []byte) error {
	length := len(value)
	valueWithDeleteFlag := make([]byte, length+1)
	copy(valueWithDeleteFlag, value)
	valueWithDeleteFlag[length] = 0 // not deleted element

	rawKey := newDBKey(key, c.height)
	c.ops = append(c.ops, database.BatchOp{
		Key:   rawKey,
		Value: valueWithDeleteFlag,
	})
	c.size += len(rawKey) + len(valueWithDeleteFlag)
	return nil
}

// Returns the sizes to be committed in the database
func (c *batch) Size() int {
	return c.size
}

// Removed all pending writes and deletes to the database
func (c *batch) Reset() {
	c.ops = make([]database.BatchOp, 0)
	c.size = 0
}

// Returns the inner batch
func (c *batch) Inner() database.Batch {
	return c
}

func (c *batch) Replay(w database.KeyValueWriterDeleter) error {
	for _, op := range c.ops {
		rawKey, _, _ := parseDBKey(op.Key)
		if bytes.Equal(op.Value, []byte{1}) {
			if err := w.Delete(rawKey); err != nil {
				return err
			}
		} else {
			if err := w.Put(rawKey, op.Value[0:len(op.Value)-1]); err != nil {
				return err
			}
		}
	}
	return nil
}
