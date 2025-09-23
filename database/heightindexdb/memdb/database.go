// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memdb

import (
	"sync"

	"github.com/ava-labs/avalanchego/database"
)

var (
	_ database.HeightIndex = (*Database)(nil)
)

// Database is an in-memory implementation of database.HeightIndex
type Database struct {
	mu     sync.RWMutex
	data   map[uint64][]byte
	closed bool
}

// Put stores data in memory at the given height
func (d *Database) Put(height uint64, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return database.ErrClosed
	}

	if d.data == nil {
		d.data = make(map[uint64][]byte)
	}

	if len(data) == 0 {
		return database.ErrNotFound
	}

	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	d.data[height] = dataCopy

	return nil
}

// Get retrieves data at the given height
func (d *Database) Get(height uint64) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, database.ErrClosed
	}

	data, ok := d.data[height]
	if !ok {
		return nil, database.ErrNotFound
	}

	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	return dataCopy, nil
}

// Has checks if data exists at the given height
func (d *Database) Has(height uint64) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return false, database.ErrClosed
	}

	_, ok := d.data[height]
	return ok, nil
}

// Close closes the in-memory database
func (d *Database) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.closed = true
	d.data = nil
	return nil
}
