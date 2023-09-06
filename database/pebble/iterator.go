// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebble

import (
	"sync"

	"github.com/cockroachdb/pebble"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/database"
)

var _ database.Iterator = (*iter)(nil)

type iter struct {
	// [lock] ensures that only one goroutine can access [iter] at a time.
	// Note that [Database.Close] calls [iter.Release] so we need [lock] to ensure
	// that the user and [Database.Close] don't execute [iter.Release] concurrently.
	// Invariant: [Datbase.lock] is never grabbed while holding [lock].
	lock sync.Mutex

	db          *Database
	iter        *pebble.Iterator
	initialized bool
	closed      bool

	hasNext bool
	nextKey []byte
	nextVal []byte

	err error
}

// Must not be called with [db.lock] held.
func (it *iter) Next() bool {
	it.db.lock.RLock()
	dbClosed := it.db.closed
	it.db.lock.RUnlock()

	it.lock.Lock()
	defer it.lock.Unlock()

	if it.closed || dbClosed {
		it.hasNext = false
		it.err = database.ErrClosed
		return false
	}

	if !it.initialized {
		it.hasNext = it.iter.First()
		it.initialized = true
	} else {
		it.hasNext = it.iter.Next()
	}

	if it.hasNext {
		it.nextKey = it.iter.Key()
		// TODO Value() is deprecated; use ValueAndErr instead.
		it.nextVal = it.iter.Value()
	}
	return it.hasNext
}

func (it *iter) Error() error {
	it.lock.Lock()
	defer it.lock.Unlock()

	if it.err != nil {
		return it.err
	}
	if it.closed {
		return nil
	}
	return updateError(it.iter.Error())
}

func (it *iter) Key() []byte {
	it.lock.Lock()
	defer it.lock.Unlock()

	if !it.hasNext {
		return nil
	}
	return slices.Clone(it.nextKey)
}

func (it *iter) Value() []byte {
	it.lock.Lock()
	defer it.lock.Unlock()

	if !it.hasNext {
		return nil
	}
	return slices.Clone(it.nextVal)
}

func (it *iter) Release() {
	it.db.lock.Lock()
	defer it.db.lock.Unlock()

	it.lock.Lock()
	defer it.lock.Unlock()

	it.release()

}

// Assumes [it.lock] and [it.db.lock] are held.
func (it *iter) release() {
	if it.closed {
		return
	}

	// Remove the iterator from the list of open iterators.
	it.db.openIterators.Remove(it)

	it.closed = true
	_ = it.iter.Close()
}
