// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// For ease of implementation, our database's interface matches Ethereum's
// database implementation. This was to allow us to use Geth code as is for the
// EVM chain.

package database

import (
	"context"
)

// BatchWithContext is a write-only database that commits changes to its host database
// when Write is called. A batch cannot be used concurrently.
type BatchWithContext interface {
	KeyValueWriterDeleterWithContext

	// Size retrieves the amount of data queued up for writing, this includes
	// the keys, values, and deleted keys.
	Size() int

	// Write flushes any accumulated data to disk.
	Write() error

	// Reset resets the batch for reuse.
	Reset()

	// Replay replays the batch contents in the same order they were written
	// to the batch.
	Replay(w KeyValueWriterDeleter) error

	// Inner returns a Batch writing to the inner database, if one exists. If
	// this batch is already writing to the base DB, then itself should be
	// returned.
	Inner() Batch
}

// BatcherWithContext wraps the NewBatch method of a backing data store.
type BatcherWithContext interface {
	// NewBatch creates a write-only database that buffers changes to its host db
	// until a final write is called.
	NewBatch() BatchWithContext
}

type BatchOpsBatchWithContext struct {
	BatchOps
}

func (b *BatchOpsBatchWithContext) Put(_ context.Context, key, value []byte) error {
	return b.BatchOps.Put(key, value)
}

func (b *BatchOpsBatchWithContext) Delete(_ context.Context, key []byte) error {
	return b.BatchOps.Delete(key)
}

func (b *BatchOpsBatchWithContext) Size() int {
	return b.size
}

func (b *BatchOpsBatchWithContext) Reset() {
	b.Ops = b.Ops[:0]
	b.size = 0
}

func (b *BatchOpsBatchWithContext) Replay(ctx context.Context, w KeyValueWriterDeleterWithContext) error {
	for _, op := range b.Ops {
		if op.Delete {
			if err := w.Delete(ctx, op.Key); err != nil {
				return err
			}
		} else if err := w.Put(ctx, op.Key, op.Value); err != nil {
			return err
		}
	}
	return nil
}
