// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"errors"

	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/database"
)

var (
	_ ethdb.Batch         = (*ethBatchWrapper)(nil)
	_ ethdb.KeyValueStore = (*ethDBWrapper)(nil)

	ErrSnapshotNotSupported = errors.New("snapshot is not supported")
	ErrStatNotSupported     = errors.New("stat is not supported")
)

func WrapDatabase(db database.Database) ethdb.KeyValueStore { return ethDBWrapper{db} }

type ethDBWrapper struct{ database.Database }

func (ethDBWrapper) Stat(string) (string, error) { return "", ErrStatNotSupported }

func (db ethDBWrapper) NewBatch() ethdb.Batch { return ethBatchWrapper{db.Database.NewBatch()} }

// TODO: propagate size through avalanchego Database interface
func (db ethDBWrapper) NewBatchWithSize(int) ethdb.Batch {
	return ethBatchWrapper{db.Database.NewBatch()}
}

func (ethDBWrapper) NewSnapshot() (ethdb.Snapshot, error) {
	return nil, ErrSnapshotNotSupported
}

// This method assumes that the prefix is NOT part of the start, so there's
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

func (db ethDBWrapper) NewIteratorWithStart(start []byte) ethdb.Iterator {
	return db.Database.NewIteratorWithStart(start)
}

type ethBatchWrapper struct{ database.Batch }

func (batch ethBatchWrapper) ValueSize() int { return batch.Batch.Size() }

func (batch ethBatchWrapper) Replay(w ethdb.KeyValueWriter) error { return batch.Batch.Replay(w) }
