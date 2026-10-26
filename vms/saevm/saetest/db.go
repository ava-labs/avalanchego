// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

// ErrInjected is returned by [FlakyDB] once its op budget is spent.
var ErrInjected = errors.New("injected fault")

// CopyDB returns an in-memory copy of src, used to hand a fresh VM the
// persisted state of a prior one without sharing the live database.
func CopyDB(tb testing.TB, src database.Database) database.Database {
	tb.Helper()

	dst, err := copyDB(src)
	require.NoErrorf(tb, err, "copying %T", src)
	return dst
}

func copyDB(src database.Database) (database.Database, error) {
	dst := memdb.New()
	it := src.NewIterator()
	defer it.Release()
	for it.Next() {
		if err := dst.Put(it.Key(), it.Value()); err != nil {
			return nil, fmt.Errorf("%T.Put() during database copy: %w", dst, err)
		}
	}
	if err := it.Error(); err != nil {
		return nil, fmt.Errorf("%T.Error() after database copy: %w", it, err)
	}
	return dst, nil
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
	require.NoErrorf(tb, it.Error(), "%T.Error()", it)
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

// CaptureDB copies the contents of the wrapped database as they were after a
// chosen number of mutating ops. Unlike [FlakyDB], every op succeeds.
// A Put, a Delete and a batch write each count as one op. Safe for concurrent
// use.
type CaptureDB struct {
	database.Database

	lock      sync.Mutex
	ops       int
	captureAt int
	captured  database.Database
	err       error
}

// NewCaptureDB returns a [CaptureDB] that copies db as it was after its first n
// mutating ops.
func NewCaptureDB(db database.Database, n int) *CaptureDB {
	return &CaptureDB{
		Database:  db,
		captureAt: n,
	}
}

// Ops returns the number of mutating ops performed.
func (c *CaptureDB) Ops() int {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.ops
}

// Captured returns the copy taken after n mutating ops, or a copy of the
// current contents if no more than n ops have been performed.
func (c *CaptureDB) Captured(tb testing.TB) database.Database {
	tb.Helper()

	c.lock.Lock()
	defer c.lock.Unlock()

	if c.captured != nil || c.err != nil {
		require.NoErrorf(tb, c.err, "%T copying database at capture point", c)
		return c.captured
	}

	captured, err := copyDB(c.Database)
	require.NoErrorf(tb, err, "%T copying database on demand", c)
	return captured
}

// mutate counts and performs a mutating op, first copying the database if
// exactly n ops have been performed. The op runs under the lock so that the
// copy contains exactly the earlier ops.
func (c *CaptureDB) mutate(op func() error) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.ops == c.captureAt {
		c.captured, c.err = copyDB(c.Database)
	}
	c.ops++
	return op()
}

func (c *CaptureDB) Put(key, value []byte) error {
	return c.mutate(func() error {
		return c.Database.Put(key, value)
	})
}

func (c *CaptureDB) Delete(key []byte) error {
	return c.mutate(func() error {
		return c.Database.Delete(key)
	})
}

func (c *CaptureDB) NewBatch() database.Batch {
	return &captureBatch{Batch: c.Database.NewBatch(), db: c}
}

type captureBatch struct {
	database.Batch
	db *CaptureDB
}

func (b *captureBatch) Write() error {
	return b.db.mutate(b.Batch.Write)
}

// Inner returns the wrapper itself, so callers that unwrap batches still commit
// through the op counter.
func (b *captureBatch) Inner() database.Batch { return b }

// UnreadableOnceDB fails the first read of one key with [ErrInjected] and
// serves every other read from the wrapped database. Safe for concurrent use.
type UnreadableOnceDB struct {
	database.Database

	key    []byte
	failed atomic.Bool
}

// NewUnreadableOnceDB returns an [UnreadableOnceDB] whose first read of key
// fails.
func NewUnreadableOnceDB(db database.Database, key []byte) *UnreadableOnceDB {
	return &UnreadableOnceDB{
		Database: db,
		key:      key,
	}
}

func (u *UnreadableOnceDB) Get(key []byte) ([]byte, error) {
	if bytes.Equal(key, u.key) && u.failed.CompareAndSwap(false, true) {
		return nil, ErrInjected
	}
	return u.Database.Get(key)
}
