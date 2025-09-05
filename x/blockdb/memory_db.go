// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import "sync"

// MemoryDatabase is an in-memory implementation of BlockDB
type MemoryDatabase struct {
	mu     sync.RWMutex
	blocks map[BlockHeight]BlockData
	closed bool
}

// WriteBlock stores a block in memory
func (m *MemoryDatabase) WriteBlock(height BlockHeight, block BlockData) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrDatabaseClosed
	}

	if m.blocks == nil {
		m.blocks = make(map[BlockHeight]BlockData)
	}

	if len(block) == 0 {
		return ErrBlockEmpty
	}

	blockCopy := make([]byte, len(block))
	copy(blockCopy, block)
	m.blocks[height] = blockCopy

	return nil
}

// ReadBlock retrieves the full block data for the given height
func (m *MemoryDatabase) ReadBlock(height BlockHeight) (BlockData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, ErrDatabaseClosed
	}

	block, ok := m.blocks[height]
	if !ok {
		return nil, ErrBlockNotFound
	}

	blockCopy := make([]byte, len(block))
	copy(blockCopy, block)
	return blockCopy, nil
}

// HasBlock checks if a block exists at the given height
func (m *MemoryDatabase) HasBlock(height BlockHeight) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return false, ErrDatabaseClosed
	}

	_, ok := m.blocks[height]
	return ok, nil
}

// Close closes the in-memory database
func (m *MemoryDatabase) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	m.blocks = nil
	return nil
}
