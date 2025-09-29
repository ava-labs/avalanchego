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

// NewMemoryDatabase creates a new in-memory BlockDB
func NewMemoryDatabase() *MemoryDatabase {
	return &MemoryDatabase{
		blocks: make(map[BlockHeight]BlockData),
	}
}

// Put stores a block in memory
func (m *MemoryDatabase) Put(height BlockHeight, block BlockData) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrDatabaseClosed
	}

	if len(block) == 0 {
		return ErrBlockEmpty
	}

	m.blocks[height] = block

	return nil
}

// Get retrieves the full block data for the given height
func (m *MemoryDatabase) Get(height BlockHeight) (BlockData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, ErrDatabaseClosed
	}

	block, exists := m.blocks[height]
	if !exists {
		return nil, ErrBlockNotFound
	}

	return block, nil
}

// Has checks if a block exists at the given height
func (m *MemoryDatabase) Has(height BlockHeight) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return false, ErrDatabaseClosed
	}

	_, exists := m.blocks[height]
	return exists, nil
}

// Inspect returns details about the database
func (*MemoryDatabase) Inspect() (string, error) {
	return "", nil
}

// Close closes the in-memory database
func (m *MemoryDatabase) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	m.closed = true
	m.blocks = nil
	return nil
}
