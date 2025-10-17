// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntryCache(t *testing.T) {
	store, cleanup := newTestDatabase(t, DefaultConfig())
	defer cleanup()

	height := uint64(10)
	expectedBlock := randomBlock(t)

	// Write the block - this should populate the cache
	require.NoError(t, store.Put(height, expectedBlock))

	// First read should come from cache
	block, err := store.Get(height)
	require.NoError(t, err)
	require.Equal(t, expectedBlock, block)

	// Close all data files to ensure the next read cannot come from disk
	store.fileCache.Flush()

	// Read again - should still succeed because it comes from cache
	cachedBlock, err := store.Get(height)
	require.NoError(t, err)
	require.Equal(t, expectedBlock, cachedBlock, "block should be retrieved from cache after files are closed")
}

func TestEntryCache_GetPopulatesCache(t *testing.T) {
	store, cleanup := newTestDatabase(t, DefaultConfig())
	defer cleanup()

	height := uint64(20)
	expectedBlock := randomBlock(t)

	// Write the block
	require.NoError(t, store.Put(height, expectedBlock))

	// Evict the entry from cache to simulate a cache miss
	store.entryCache.Evict(height)

	// Verify it's not in cache
	_, ok := store.entryCache.Get(height)
	require.False(t, ok, "entry should not be in cache after eviction")

	// Read the block - this should read from disk and populate the cache
	block, err := store.Get(height)
	require.NoError(t, err)
	require.Equal(t, expectedBlock, block)

	// Verify it's now in cache
	cachedBlock, ok := store.entryCache.Get(height)
	require.True(t, ok, "entry should be in cache after Get")
	require.Equal(t, expectedBlock, cachedBlock)
}

func TestEntryCacheHas(t *testing.T) {
	store, cleanup := newTestDatabase(t, DefaultConfig())
	defer cleanup()

	height := uint64(30)
	expectedBlock := randomBlock(t)

	// Write the block - this populates the cache
	require.NoError(t, store.Put(height, expectedBlock))

	// Close all data files to ensure Has cannot read from disk
	store.fileCache.Flush()

	// Has should still return true because it checks cache first
	has, err := store.Has(height)
	require.NoError(t, err)
	require.True(t, has, "Has should return true when block is in cache")
}
