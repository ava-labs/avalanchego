// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memdb

import (
	"slices"
	"sync"

	"github.com/ava-labs/avalanchego/database"
)

var _ database.HeightIndex = (*Database)(nil)

// Database is an in-memory implementation of database.HeightIndex
type Database struct {
	mu     sync.RWMutex
	data   map[uint64][]byte
	closed bool
}

// Put stores data in memory at the given height
func (db *Database) Put(height uint64, data []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	if db.data == nil {
		db.data = make(map[uint64][]byte)
	}

	if len(data) == 0 {
		// don't save empty slice if data is nil or empty
		db.data[height] = nil
	} else {
		db.data[height] = slices.Clone(data)
	}

	return nil
}

// Get retrieves data at the given height
func (db *Database) Get(height uint64) ([]byte, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}

	data, ok := db.data[height]
	if !ok {
		return nil, database.ErrNotFound
	}

	return slices.Clone(data), nil
}

// Has checks if data exists at the given height
func (db *Database) Has(height uint64) (bool, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if db.closed {
		return false, database.ErrClosed
	}

	_, ok := db.data[height]
	return ok, nil
}

// Close closes the in-memory database
func (db *Database) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	db.closed = true
	db.data = nil
	return nil
}
