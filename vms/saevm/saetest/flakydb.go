// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"errors"
	"sync"

	"github.com/ava-labs/avalanchego/database"
)

// ErrInjected is returned by a [FlakyDB] once its configured number of
// mutating operations has been reached.
var ErrInjected = errors.New("injected fault")

// FlakyDB wraps a database and fails after a configured number of mutating
// operations. Each [FlakyDB.Put], [FlakyDB.Delete], and [database.Batch.Write]
// of a batch created by [FlakyDB.NewBatch] counts as an op; reads and iteration
// are not counted and never fail. It is safe for concurrent use.
type FlakyDB struct {
	database.Database

	mu        sync.Mutex
	failAfter int
	calls     int
}

// NewFlakyDB returns a [FlakyDB] that fails with [ErrInjected] after
// failAfter mutating operations.
func NewFlakyDB(db database.Database, failAfter int) *FlakyDB {
	return &FlakyDB{
		Database:  db,
		failAfter: failAfter,
	}
}

// SetFailAfter resets the operation counter and arms the database to fail
// after a further n mutating operations.
func (f *FlakyDB) SetFailAfter(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = 0
	f.failAfter = n
}

// Calls returns the number of mutating operations performed so far.
func (f *FlakyDB) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *FlakyDB) shouldFail() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls >= f.failAfter {
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
