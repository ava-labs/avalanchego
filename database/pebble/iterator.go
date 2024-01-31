// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebble

import (
	"errors"
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/database"
)

var (
	_ database.Iterator = (*iter)(nil)

	errCouldntGetValue = errors.New("couldnt get iterator value")
)

type iter struct {
	// [lock] ensures that only one goroutine can access [iter] at a time.
	// Note that [Database.Close] calls [iter.Release] so we need [lock] to ensure
	// that the user and [Database.Close] don't execute [iter.Release] concurrently.
	// Invariant: [Database.lock] is never grabbed while holding [lock].
	lock sync.Mutex

	db   *Database
	iter *pebble.Iterator

	initialized bool
	closed      bool
	err         error

	hasNext bool
	nextKey []byte
	nextVal []byte
}

// Must not be called with [db.lock] held.
func (it *iter) Next() bool {
	it.lock.Lock()
	defer it.lock.Unlock()

	switch {
	case it.err != nil:
		it.hasNext = false
		return false
	case it.closed:
		it.hasNext = false
		it.err = database.ErrClosed
		return false
	case !it.initialized:
		it.hasNext = it.iter.First()
		it.initialized = true
	default:
		it.hasNext = it.iter.Next()
	}

	if !it.hasNext {
		return false
	}

	it.nextKey = it.iter.Key()

	var err error
	it.nextVal, err = it.iter.ValueAndErr()
	if err != nil {
		it.hasNext = false
		it.err = fmt.Errorf("%w: %w", errCouldntGetValue, err)
		return false
	}

	return true
}

func (it *iter) Error() error {
	it.lock.Lock()
	defer it.lock.Unlock()

	if it.err != nil || it.closed {
		return it.err
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
	if err := it.iter.Close(); err != nil {
		it.err = updateError(err)
	}
}
