// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import "github.com/ava-labs/avalanchego/database"

// A thin wrapper on top of the database.Batch that also keeps track of the height.
//
// It takes any key and wraps it with dbKey. It is possible to construct many
// batches but at Write() time, the height of the batch to be written to the
// database must be the following height from the last written to the database,
// otherwise an error will be thrown
type dbBatchWithHeight struct {
	db     *archiveDB
	ops    map[string]database.BatchOp
	size   int
	height uint64
	batch  database.Batch
}

func newBatchWithHeight(db *archiveDB, height uint64) *dbBatchWithHeight {
	batch := db.rawDB.NewBatch()
	ops := make(map[string]database.BatchOp)
	size := 0
	return &dbBatchWithHeight{
		db,
		ops,
		size,
		height,
		batch,
	}
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

	if err := c.Replay(c.batch); err != nil {
		return err
	}
	if err := c.batch.Write(); err != nil {
		return err
	}
	c.db.currentHeight = c.height

	return nil
}

// Delete any previous state that may be stored in the database
func (c *dbBatchWithHeight) Delete(key []byte) error {
	rawKey := newDBKey(key, c.height)
	if value, exists := c.ops[string(rawKey)]; exists {
		// decrese the size if there was any previous key/value
		c.size -= len(value.Value)
		c.size -= len(value.Key)
	}
	c.ops[string(rawKey)] = database.BatchOp{
		Key:    rawKey,
		Delete: true,
	}
	c.size += len(rawKey) + 1
	return nil
}

// Queues an insert for a key-value pair
func (c *dbBatchWithHeight) Put(key []byte, value []byte) error {
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
func (c *dbBatchWithHeight) Size() int {
	return c.size
}

// Removed all pending writes and deletes to the database
func (c *dbBatchWithHeight) Reset() {
	c.ops = make(map[string]database.BatchOp)
	c.size = 0
}

// Returns the inner batch
func (c *dbBatchWithHeight) Inner() database.Batch {
	return c
}

func (c *dbBatchWithHeight) Replay(w database.KeyValueWriterDeleter) error {
	for _, op := range c.ops {
		if err := w.Put(op.Key, op.Value); err != nil {
			return err
		}
	}
	err := database.PutUInt64(w, keyHeight, c.height)
	if err != nil {
		return err
	}
	return nil
}
