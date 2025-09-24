// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"errors"

	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/database"
)

var (
	_ ethdb.KeyValueStore = (*ethDBWrapper)(nil)

	ErrSnapshotNotSupported = errors.New("snapshot is not supported")
)

func WrapDatabase(db database.Database) ethdb.KeyValueStore { return ethDBWrapper{db} }

// ethDbWrapper implements ethdb.Database
type ethDBWrapper struct{ database.Database }

// Stat implements ethdb.Database
func (ethDBWrapper) Stat(string) (string, error) { return "", database.ErrNotFound }

// NewBatch implements ethdb.Database
func (db ethDBWrapper) NewBatch() ethdb.Batch { return wrappedBatch{db.Database.NewBatch()} }

// NewBatchWithSize implements ethdb.Database
// TODO: propagate size through avalanchego Database interface
func (db ethDBWrapper) NewBatchWithSize(int) ethdb.Batch {
	return wrappedBatch{db.Database.NewBatch()}
}

func (ethDBWrapper) NewSnapshot() (ethdb.Snapshot, error) {
	return nil, ErrSnapshotNotSupported
}

// NewIterator implements ethdb.Database
//
// Note: This method assumes that the prefix is NOT part of the start, so there's
// no need for the caller to prepend the prefix to the start.
func (db ethDBWrapper) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	// avalanchego's database implementation assumes that the prefix is part of the
	// start, so it is added here (if it is provided).
	if len(prefix) != 0 {
		newStart := make([]byte, len(prefix)+len(start))
		copy(newStart, prefix)
		copy(newStart[len(prefix):], start)
		start = newStart
	}
	return db.Database.NewIteratorWithStartAndPrefix(start, prefix)
}

// NewIteratorWithStart implements ethdb.Database
func (db ethDBWrapper) NewIteratorWithStart(start []byte) ethdb.Iterator {
	return db.Database.NewIteratorWithStart(start)
}

// wrappedBatch implements ethdb.Batch
type wrappedBatch struct{ database.Batch }

// ValueSize implements ethdb.Batch
func (batch wrappedBatch) ValueSize() int { return batch.Batch.Size() }

// Replay implements ethdb.Batch
func (batch wrappedBatch) Replay(w ethdb.KeyValueWriter) error { return batch.Batch.Replay(w) }
