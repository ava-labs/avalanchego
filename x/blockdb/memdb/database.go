// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memdb

import (
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/x/blockdb"
)

var (
	_ database.HeightIndex = (*Database)(nil)
)

// Database is an in-memory implementation of BlockDB
type Database struct {
	mu     sync.RWMutex
	blocks map[blockdb.BlockHeight]blockdb.BlockData
	closed bool
}

// Put stores data in memory at the given height
func (d *Database) Put(height blockdb.BlockHeight, block blockdb.BlockData) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return blockdb.ErrDatabaseClosed
	}

	if d.blocks == nil {
		d.blocks = make(map[blockdb.BlockHeight]blockdb.BlockData)
	}

	if len(block) == 0 {
		return blockdb.ErrBlockEmpty
	}

	blockCopy := make([]byte, len(block))
	copy(blockCopy, block)
	d.blocks[height] = blockCopy

	return nil
}

// Get retrieves data at the given height
func (d *Database) Get(height blockdb.BlockHeight) (blockdb.BlockData, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, blockdb.ErrDatabaseClosed
	}

	block, ok := d.blocks[height]
	if !ok {
		return nil, blockdb.ErrBlockNotFound
	}

	blockCopy := make([]byte, len(block))
	copy(blockCopy, block)
	return blockCopy, nil
}

// Has checks if data exists at the given height
func (d *Database) Has(height blockdb.BlockHeight) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return false, blockdb.ErrDatabaseClosed
	}

	_, ok := d.blocks[height]
	return ok, nil
}

// Close closes the in-memory database
func (d *Database) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.closed = true
	d.blocks = nil
	return nil
}
