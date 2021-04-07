// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package versionabledb

import (
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type Database struct {
	lock sync.RWMutex

	versionEnabled bool
	db             database.Database
	vdb            *versiondb.Database
}

// New returns a new prefixed database
func New(db database.Database) *Database {
	return &Database{
		db:  db,
		vdb: versiondb.New(db),
	}
}

// Has implements the database.Database interface
func (db *Database) Has(key []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.versionEnabled {
		return db.vdb.Has(key)
	}
	return db.db.Has(key)
}

// Get implements the database.Database interface
func (db *Database) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.versionEnabled {
		return db.vdb.Get(key)
	}
	return db.db.Get(key)
}

// Put implements the database.Database interface
func (db *Database) Put(key, value []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.versionEnabled {
		return db.vdb.Put(key, value)
	}
	return db.db.Put(key, value)
}

// Delete implements the database.Database interface
func (db *Database) Delete(key []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.versionEnabled {
		return db.vdb.Delete(key)
	}
	return db.db.Delete(key)
}

// NewBatch implements the database.Database interface
func (db *Database) NewBatch() database.Batch {
	return &batch{
		db:    db,
		Batch: db.db.NewBatch(),
	}
}

// NewIterator implements the database.Database interface
func (db *Database) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

// NewIteratorWithStart implements the database.Database interface
func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

// NewIteratorWithPrefix implements the database.Database interface
func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

// NewIteratorWithStartAndPrefix implements the database.Database interface
func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.versionEnabled {
		return db.vdb.NewIteratorWithStartAndPrefix(start, prefix)
	}
	return db.db.NewIteratorWithStartAndPrefix(start, prefix)
}

// Stat implements the database.Database interface
func (db *Database) Stat(stat string) (string, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	// Note: versiondb passes through to the underlying db, so we skip
	// checking the [versionEnabled] flag here.
	return db.db.Stat(stat)
}

// Compact implements the database.Database interface
func (db *Database) Compact(start, limit []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	// Note: versiondb passes through to the underlying db, so we skip
	// checking the [versionEnabled] flag here.
	return db.db.Compact(start, limit)
}

// StartCommit sets the [versionEnabled] flag to true, so that
// all operations are performed on top of the versiondb instead
// of the underlying database.
func (db *Database) StartCommit() {
	db.lock.Lock()
	defer db.lock.Unlock()

	db.versionEnabled = true
}

// EndCommit sets the [versionEnabled] flag back to false and calls
// Abort() on the versiondb.
func (db *Database) AbortCommit() {
	db.lock.Lock()
	defer db.lock.Unlock()

	db.versionEnabled = false
	db.vdb.Abort()
}

// EndCommit sets the [versionEnabled] flag back to false and calls
// Abort() on the versiondb.
func (db *Database) EndCommit() {
	db.versionEnabled = false
	db.vdb.Abort()
	db.lock.Unlock()
}

// CommitBatch returns a batch that contains all uncommitted puts/deletes.
// Calling Write() on the returned batch causes the puts/deletes to be
// written to the underlying database. CommitBatch holds onto the lock,
// blocking all other database operations until EndCommit() is called.
func (db *Database) CommitBatch() (database.Batch, error) {
	db.lock.Lock()

	return db.vdb.CommitBatch()
}

// Close implements the database.Database interface
func (db *Database) Close() error {
	db.lock.Lock()
	defer db.lock.Unlock()

	errs := wrappers.Errs{}
	errs.Add(
		db.vdb.Close(),
		db.db.Close(),
	)
	return errs.Err
}

type batch struct {
	db *Database
	database.Batch
}

// Write implements the Database interface
func (b *batch) Write() error {
	b.db.lock.Lock()
	defer b.db.lock.Unlock()

	if b.db.versionEnabled {
		return b.Batch.Replay(b.db.vdb)
	}

	return b.Batch.Write()
}

// Inner returns itself
func (b *batch) Inner() database.Batch { return b.Batch }
