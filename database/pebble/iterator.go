// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebble

import (
	"github.com/cockroachdb/pebble"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/database"
)

type iter struct {
	db          *Database
	iter        *pebble.Iterator
	initialized bool
	closed      bool

	valid bool
	err   error
}

func (it *iter) Next() bool {
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

	var hasNext bool
	if !it.initialized {
		hasNext = it.iter.First()
		it.initialized = true
	} else {
		hasNext = it.iter.Next()
	}
	it.valid = hasNext
	return hasNext
}

func (it *iter) Error() error {
	if it.err != nil {
		return it.err
	}
	if it.closed {
		return nil
	}
	return updateError(it.iter.Error())
}

func (it *iter) Key() []byte {
	if !it.valid {
		return nil
	}
	return slices.Clone(it.iter.Key())
}

func (it *iter) Value() []byte {
	if !it.valid {
		return nil
	}
	return slices.Clone(it.iter.Value())
}

func (it *iter) Release() {
	if it.closed {
		return
	}
	it.closed = true
	it.valid = false
	_ = it.iter.Close()
}
