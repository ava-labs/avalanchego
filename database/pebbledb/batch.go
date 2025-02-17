// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebbledb

import (
	"fmt"

	"github.com/cockroachdb/pebble"

	"github.com/ava-labs/avalanchego/database"
)

var _ database.Batch = (*batch)(nil)

// Not safe for concurrent use.
type batch struct {
	batch *pebble.Batch
	db    *Database
	size  int

	// True iff [batch] has been written to the database
	// since the last time [Reset] was called.
	written bool
}

func (db *Database) NewBatch() database.Batch {
	return &batch{
		db:    db,
		batch: db.pebbleDB.NewBatch(),
	}
}

func (b *batch) Put(key, value []byte) error {
	b.size += len(key) + len(value) + pebbleByteOverHead
	return b.batch.Set(key, value, b.db.writeOptions)
}

func (b *batch) Delete(key []byte) error {
	b.size += len(key) + pebbleByteOverHead
	return b.batch.Delete(key, b.db.writeOptions)
}

func (b *batch) Size() int {
	return b.size
}

// Assumes [b.db.lock] is not held.
func (b *batch) Write() error {
	b.db.lock.RLock()
	defer b.db.lock.RUnlock()

	// Committing to a closed database makes pebble panic
	// so make sure [b.db] isn't closed.
	if b.db.closed {
		return database.ErrClosed
	}

	if b.written {
		// pebble doesn't support writing a batch twice so we have to clone the
		// batch before writing it.
		newBatch := b.db.pebbleDB.NewBatch()
		if err := newBatch.Apply(b.batch, nil); err != nil {
			return err
		}
		b.batch = newBatch
	}

	b.written = true
	return updateError(b.batch.Commit(b.db.writeOptions))
}

func (b *batch) Reset() {
	b.batch.Reset()
	b.written = false
	b.size = 0
}

func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	reader := b.batch.Reader()
	for {
		kind, k, v, ok := reader.Next()
		if !ok {
			return nil
		}
		switch kind {
		case pebble.InternalKeyKindSet:
			if err := w.Put(k, v); err != nil {
				return err
			}
		case pebble.InternalKeyKindDelete:
			if err := w.Delete(k); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: %v", errInvalidOperation, kind)
		}
	}
}

func (b *batch) Inner() database.Batch {
	return b
}
