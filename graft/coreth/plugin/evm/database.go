// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/coreth/ethdb"

	"github.com/ava-labs/avalanchego/database"
)

// Database implements ethdb.Database
type Database struct{ database.Database }

// NewBatch implements ethdb.Database
func (db Database) NewBatch() ethdb.Batch { return Batch{db.Database.NewBatch()} }

// NewIterator implements ethdb.Database
//
// Note: This method assumes that the prefix is NOT part of the start, so there's
// no need for the caller to prepend the prefix to the start.
func (db Database) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
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
func (db Database) NewIteratorWithStart(start []byte) ethdb.Iterator {
	return db.Database.NewIteratorWithStart(start)
}

// Batch implements ethdb.Batch
type Batch struct{ database.Batch }

// ValueSize implements ethdb.Batch
func (batch Batch) ValueSize() int { return batch.Batch.Size() }

// Replay implements ethdb.Batch
func (batch Batch) Replay(w ethdb.KeyValueWriter) error { return batch.Batch.Replay(w) }
