// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ethereum/go-ethereum/ethdb"
)

var (
	_ ethdb.KeyValueStore = &ethDbWrapper{}

	ErrSnapshotNotSupported = errors.New("snapshot is not supported")
)

// ethDbWrapper implements ethdb.Database
type ethDbWrapper struct{ database.Database }

func WrapDatabase(db database.Database) ethdb.KeyValueStore { return ethDbWrapper{db} }

// Stat implements ethdb.Database
func (db ethDbWrapper) Stat(string) (string, error) { return "", database.ErrNotFound }

// NewBatch implements ethdb.Database
func (db ethDbWrapper) NewBatch() ethdb.Batch { return wrappedBatch{db.Database.NewBatch()} }

// NewBatchWithSize implements ethdb.Database
// TODO: propagate size through avalanchego Database interface
func (db ethDbWrapper) NewBatchWithSize(size int) ethdb.Batch {
	return wrappedBatch{db.Database.NewBatch()}
}

func (db ethDbWrapper) NewSnapshot() (ethdb.Snapshot, error) {
	return nil, ErrSnapshotNotSupported
}

// NewIterator implements ethdb.Database
//
// Note: This method assumes that the prefix is NOT part of the start, so there's
// no need for the caller to prepend the prefix to the start.
func (db ethDbWrapper) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	// avalanchego's database implementation assumes that the prefix is part of the
	// start, so it is added here (if it is provided).
	if len(prefix) > 0 {
		newStart := make([]byte, len(prefix)+len(start))
		copy(newStart, prefix)
		copy(newStart[len(prefix):], start)
		start = newStart
	}
	return db.Database.NewIteratorWithStartAndPrefix(start, prefix)
}

// NewIteratorWithStart implements ethdb.Database
func (db ethDbWrapper) NewIteratorWithStart(start []byte) ethdb.Iterator {
	return db.Database.NewIteratorWithStart(start)
}

// wrappedBatch implements ethdb.wrappedBatch
type wrappedBatch struct{ database.Batch }

// ValueSize implements ethdb.Batch
func (batch wrappedBatch) ValueSize() int { return batch.Batch.Size() }

// Replay implements ethdb.Batch
func (batch wrappedBatch) Replay(w ethdb.KeyValueWriter) error { return batch.Batch.Replay(w) }
