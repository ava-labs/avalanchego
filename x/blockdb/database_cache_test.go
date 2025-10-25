// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntryCachePutGet(t *testing.T) {
	store, cleanup := newTestDatabase(t, DefaultConfig())
	defer cleanup()

	height := uint64(10)
	expectedBlock := randomBlock(t)

	// Write the block and verify it's in cache
	require.NoError(t, store.Put(height, expectedBlock))
	cachedBlock, ok := store.entryCache.Get(height)
	require.True(t, ok, "entry should be in cache after Get")
	require.Equal(t, expectedBlock, cachedBlock)

	// Close all data files to ensure the next read cannot come from disk
	store.fileCache.Flush()

	// Read the block - should come from cache
	block, err := store.Get(height)
	require.NoError(t, err)
	require.Equal(t, expectedBlock, block)
}

func TestEntryCacheGet(t *testing.T) {
	store, cleanup := newTestDatabase(t, DefaultConfig())
	defer cleanup()

	height := uint64(20)
	expectedBlock := randomBlock(t)
	require.NoError(t, store.Put(height, expectedBlock))

	// Evict the entry from cache to simulate a cache miss
	store.entryCache.Evict(height)

	// Read the block - should come from disk
	block, err := store.Get(height)
	require.NoError(t, err)
	require.Equal(t, expectedBlock, block)

	// Verify block is now in cache
	cachedBlock, ok := store.entryCache.Get(height)
	require.True(t, ok, "entry should be in cache after Get")
	require.Equal(t, expectedBlock, cachedBlock)
}

func TestEntryCacheHas(t *testing.T) {
	store, cleanup := newTestDatabase(t, DefaultConfig())
	defer cleanup()

	height := uint64(30)
	expectedBlock := randomBlock(t)
	require.NoError(t, store.Put(height, expectedBlock))

	// Close all data files to ensure Has cannot read from disk
	store.fileCache.Flush()

	// Has should still return true because block is in cache
	has, err := store.Has(height)
	require.NoError(t, err)
	require.True(t, has, "Has should return true when block is in cache")
}
