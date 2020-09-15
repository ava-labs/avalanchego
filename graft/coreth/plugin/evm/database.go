// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"

	"github.com/ava-labs/go-ethereum/ethdb"

	"github.com/ava-labs/avalanche-go/database"
)

var (
	errOpNotSupported = errors.New("this operation is not supported")
)

// Database implements ethdb.Database
type Database struct{ database.Database }

// HasAncient returns an error as we don't have a backing chain freezer.
func (db Database) HasAncient(kind string, number uint64) (bool, error) {
	return false, errOpNotSupported
}

// Ancient returns an error as we don't have a backing chain freezer.
func (db Database) Ancient(kind string, number uint64) ([]byte, error) { return nil, errOpNotSupported }

// Ancients returns an error as we don't have a backing chain freezer.
func (db Database) Ancients() (uint64, error) { return 0, errOpNotSupported }

// AncientSize returns an error as we don't have a backing chain freezer.
func (db Database) AncientSize(kind string) (uint64, error) { return 0, errOpNotSupported }

// AppendAncient returns an error as we don't have a backing chain freezer.
func (db Database) AppendAncient(number uint64, hash, header, body, receipts, td []byte) error {
	return errOpNotSupported
}

// TruncateAncients returns an error as we don't have a backing chain freezer.
func (db Database) TruncateAncients(items uint64) error { return errOpNotSupported }

// Sync returns an error as we don't have a backing chain freezer.
func (db Database) Sync() error { return errOpNotSupported }

// NewBatch implements ethdb.Database
func (db Database) NewBatch() ethdb.Batch { return Batch{db.Database.NewBatch()} }

// NewIterator implements ethdb.Database
func (db Database) NewIterator() ethdb.Iterator { return db.Database.NewIterator() }

// NewIteratorWithPrefix implements ethdb.Database
func (db Database) NewIteratorWithPrefix(prefix []byte) ethdb.Iterator {
	return db.NewIteratorWithPrefix(prefix)
}

// NewIteratorWithStart implements ethdb.Database
func (db Database) NewIteratorWithStart(start []byte) ethdb.Iterator {
	return db.NewIteratorWithStart(start)
}

// Batch implements ethdb.Batch
type Batch struct{ database.Batch }

// Replay implements ethdb.Batch
func (batch Batch) Replay(w ethdb.KeyValueWriter) error { return batch.Batch.Replay(w) }
