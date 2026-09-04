// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"

	assert "github.com/stretchr/testify/require"
)

// ErrInjected is returned by [FlakyDB] once its op budget is spent.
var ErrInjected = errors.New("injected fault")

// CopyDB returns an in-memory copy of src, used to hand a fresh VM the persisted
// state of a prior one without sharing the live database.
func CopyDB(tb testing.TB, src database.Database) database.Database {
	tb.Helper()

	dst := memdb.New()
	it := src.NewIterator()
	defer it.Release()
	for it.Next() {
		assert.NoErrorf(tb, dst.Put(it.Key(), it.Value()), "%T.Put() during database copy", dst)
	}
	assert.NoErrorf(tb, it.Error(), "%T.Error() after database copy", it)
	return dst
}

// AssertEqualDBs asserts that got holds exactly the same key/value pairs as
// want.
func AssertEqualDBs(tb testing.TB, want, got database.Database, msgAndArgs ...any) {
	tb.Helper()

	assert.Equal(tb, dbEntries(tb, want), dbEntries(tb, got), msgAndArgs...)
}

type dbEntry struct {
	Key   []byte
	Value []byte
}

// dbEntries returns every key/value pair in db, in iteration order.
func dbEntries(tb testing.TB, db database.Database) []dbEntry {
	tb.Helper()

	it := db.NewIterator()
	defer it.Release()

	var out []dbEntry
	for it.Next() {
		out = append(out, dbEntry{
			Key:   slices.Clone(it.Key()),
			Value: slices.Clone(it.Value()),
		})
	}
	assert.NoErrorf(tb, it.Error(), "%T.Error()", it)
	return out
}

// FlakyDB fails every mutating op after a configured number of them. A Put, a
// Delete and a batch write each count as one, reads never fail. Safe for
// concurrent use.
type FlakyDB struct {
	database.Database

	lock      sync.Mutex
	failAfter int
	calls     int
	failed    bool
}

// NewFlakyDB returns a [FlakyDB] whose ops succeed until failAfter of them have
// been performed and fail with [ErrInjected] from then on.
func NewFlakyDB(db database.Database, failAfter int) *FlakyDB {
	return &FlakyDB{
		Database:  db,
		failAfter: failAfter,
	}
}

// SetFailAfter resets the op counter and arms the database to fail after a
// further n mutating operations.
func (f *FlakyDB) SetFailAfter(n int) {
	f.lock.Lock()
	defer f.lock.Unlock()

	f.calls = 0
	f.failAfter = n
	f.failed = false
}

// Calls returns the number of ops that have succeeded.
func (f *FlakyDB) Calls() int {
	f.lock.Lock()
	defer f.lock.Unlock()

	return f.calls
}

// Failed returns whether any op has failed.
func (f *FlakyDB) Failed() bool {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.failed
}

func (f *FlakyDB) shouldFail() error {
	f.lock.Lock()
	defer f.lock.Unlock()

	if f.calls >= f.failAfter {
		f.failed = true
		return ErrInjected
	}
	f.calls++
	return nil
}

func (f *FlakyDB) Put(key, value []byte) error {
	if err := f.shouldFail(); err != nil {
		return err
	}
	return f.Database.Put(key, value)
}

func (f *FlakyDB) Delete(key []byte) error {
	if err := f.shouldFail(); err != nil {
		return err
	}
	return f.Database.Delete(key)
}

func (f *FlakyDB) NewBatch() database.Batch {
	return &flakyBatch{Batch: f.Database.NewBatch(), db: f}
}

type flakyBatch struct {
	database.Batch
	db *FlakyDB
}

func (b *flakyBatch) Write() error {
	if err := b.db.shouldFail(); err != nil {
		return err
	}
	return b.Batch.Write()
}

// Inner returns the wrapper itself, so callers that unwrap batches still commit
// through the fault counter.
func (b *flakyBatch) Inner() database.Batch { return b }
