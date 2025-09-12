// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memdb

import (
	"sync"

	"github.com/ava-labs/avalanchego/x/blockdb"
)

// Database is an in-memory implementation of BlockDB
type Database struct {
	mu     sync.RWMutex
	blocks map[blockdb.BlockHeight]blockdb.BlockData
	closed bool
}

// WriteBlock stores a block in memory
func (m *Database) WriteBlock(height blockdb.BlockHeight, block blockdb.BlockData) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return blockdb.ErrDatabaseClosed
	}

	if m.blocks == nil {
		m.blocks = make(map[blockdb.BlockHeight]blockdb.BlockData)
	}

	if len(block) == 0 {
		return blockdb.ErrBlockEmpty
	}

	blockCopy := make([]byte, len(block))
	copy(blockCopy, block)
	m.blocks[height] = blockCopy

	return nil
}

// ReadBlock retrieves the full block data for the given height
func (m *Database) ReadBlock(height blockdb.BlockHeight) (blockdb.BlockData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, blockdb.ErrDatabaseClosed
	}

	block, ok := m.blocks[height]
	if !ok {
		return nil, blockdb.ErrBlockNotFound
	}

	blockCopy := make([]byte, len(block))
	copy(blockCopy, block)
	return blockCopy, nil
}

// HasBlock checks if a block exists at the given height
func (m *Database) HasBlock(height blockdb.BlockHeight) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return false, blockdb.ErrDatabaseClosed
	}

	_, ok := m.blocks[height]
	return ok, nil
}

// Close closes the in-memory database
func (m *Database) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	m.blocks = nil
	return nil
}
