// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebble

import (
	"fmt"
	"sync/atomic"

	"github.com/cockroachdb/pebble"

	"github.com/ava-labs/avalanchego/database"
)

var _ database.Batch = (*batch)(nil)

type batch struct {
	batch *pebble.Batch
	db    *Database
	size  int

	// Support batch rewrite
	applied atomic.Bool
}

func (db *Database) NewBatch() database.Batch {
	return &batch{
		db:    db,
		batch: db.pebbleDB.NewBatch(),
	}
}

func (b *batch) Put(key, value []byte) error {
	b.size += len(key) + len(value) + pebbleByteOverHead
	return b.batch.Set(key, value, pebble.NoSync) // TODO is NoSync OK?
}

func (b *batch) Delete(key []byte) error {
	b.size += len(key) + pebbleByteOverHead
	return b.batch.Delete(key, pebble.NoSync) // TODO is NoSync OK?
}

func (b *batch) Size() int { return b.size }

func (b *batch) Write() error {
	b.db.lock.Lock() // TODO do we need write lock?
	defer b.db.lock.Unlock()

	if b.db.closed {
		return database.ErrClosed
	}

	// Support batch rewrite
	// The underlying pebble db doesn't support batch rewrites but panics instead
	// We have to create a new batch which is a kind of duplicate of the given
	// batch(arg b) and commit this new batch on behalf of the given batch.
	if b.applied.Load() {
		// the given batch b has already been committed
		// Don't Commit it again, got panic otherwise
		// Create a new batch to do Commit
		newbatch := &batch{
			db:    b.db,
			batch: b.db.pebbleDB.NewBatch(),
		}

		// duplicate b.batch to newbatch.batch
		if err := newbatch.batch.Apply(b.batch, nil); err != nil {
			return err
		}
		return updateError(newbatch.batch.Commit(pebble.NoSync)) // TODO is NoSync OK?
	}
	// mark it for alerady committed
	b.applied.Store(true)

	return updateError(b.batch.Commit(pebble.NoSync))
}

func (b *batch) Reset() {
	b.batch.Reset()
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
			return fmt.Errorf("%w: %v", ErrInvalidOperation, kind)
		}
	}
}

func (b *batch) Inner() database.Batch { return b }
