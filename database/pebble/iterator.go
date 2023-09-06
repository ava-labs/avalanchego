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
	lock sync.Mutex

	db          *Database
	iter        *pebble.Iterator
	initialized bool
	closed      bool

	valid bool
	err   error
}

// Must not be called with [db.lock] held.
func (it *iter) Next() bool {
	it.lock.Lock()
	defer it.lock.Unlock()

	if it.closed {
		return false
	}

	db := it.db

	db.lock.RLock()
	closed := db.closed
	db.lock.RUnlock()

	if closed {
		it.valid = false
		it.err = database.ErrClosed
		return false
	}

	if !it.initialized {
		it.valid = it.iter.First()
		it.initialized = true
	} else {
		it.valid = it.iter.Next()
	}
	return it.valid
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

	if !it.valid {
		return nil
	}
	return slices.Clone(it.iter.Key())
}

func (it *iter) Value() []byte {
	it.lock.Lock()
	defer it.lock.Unlock()

	if !it.valid {
		return nil
	}
	// TODO Value() is deprecated; use ValueAndErr instead.
	return slices.Clone(it.iter.Value())
}

func (it *iter) Release() {
	it.lock.Lock()
	defer it.lock.Unlock()

	if it.closed {
		return
	}

	it.db.lock.Lock()
	defer it.db.lock.Unlock()

	// Remove the iterator from the list of open iterators.
	it.db.openIterators.Remove(it)

	it.closed = true
	it.valid = false
	_ = it.iter.Close()
}
