// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheOnMiss(t *testing.T) {
	db, _ := newTestDatabase(t, DefaultConfig())
	height := uint64(20)
	block := randomBlock(t)
	require.NoError(t, db.Put(height, block))

	// Evict the entry from cache to simulate a cache miss
	db.blockCache.Evict(height)

	// Read the block - should populate the cache on cache miss
	_, err := db.Get(height)
	require.NoError(t, err)

	_, ok := db.blockCache.Get(height)
	require.True(t, ok)
}

func TestCacheGet(t *testing.T) {
	db, _ := newTestDatabase(t, DefaultConfig())
	height := uint64(30)
	block := randomBlock(t)

	// Populate cache directly without writing to database
	db.blockCache.Put(height, block)

	// Get should return the block from cache
	data, err := db.Get(height)
	require.NoError(t, err)
	require.Equal(t, block, data)
}

func TestCacheHas(t *testing.T) {
	db, _ := newTestDatabase(t, DefaultConfig())
	height := uint64(40)
	block := randomBlock(t)

	// Populate cache directly without writing to database
	db.blockCache.Put(height, block)

	// Has should return true from cache even though block is not in database
	has, err := db.Has(height)
	require.NoError(t, err)
	require.True(t, has)
}

func TestCachePutStoresClone(t *testing.T) {
	db, _ := newTestDatabase(t, DefaultConfig())
	height := uint64(40)
	block := randomBlock(t)
	clone := slices.Clone(block)
	require.NoError(t, db.Put(height, clone))

	// Modify the original block after Put
	clone[0] = 99

	// Cache should have the original unmodified data
	cached, ok := db.blockCache.Get(height)
	require.True(t, ok)
	require.Equal(t, block, cached)
}

func TestCacheGetReturnsClone(t *testing.T) {
	db, _ := newTestDatabase(t, DefaultConfig())
	height := uint64(50)
	block := randomBlock(t)
	require.NoError(t, db.Put(height, block))

	// Get the block and modify the returned data
	data, err := db.Get(height)
	require.NoError(t, err)
	data[0] = 99

	// Cache should still have the original unmodified data
	cached, ok := db.blockCache.Get(height)
	require.True(t, ok)
	require.Equal(t, block, cached)

	// Second Get should also return original data
	data, err = db.Get(height)
	require.NoError(t, err)
	require.Equal(t, block, data)
}

func TestCachePutOverridesSameHeight(t *testing.T) {
	db, _ := newTestDatabase(t, DefaultConfig())
	height := uint64(60)
	b1 := randomBlock(t)
	require.NoError(t, db.Put(height, b1))

	// Verify first block is in cache
	cached, ok := db.blockCache.Get(height)
	require.True(t, ok)
	require.Equal(t, b1, cached)

	// Put second block at same height and verify it overrides the first one
	b2 := randomBlock(t)
	require.NoError(t, db.Put(height, b2))
	cached, ok = db.blockCache.Get(height)
	require.True(t, ok)
	require.Equal(t, b2, cached)
	require.NotEqual(t, b1, cached)

	// Get should also return the new block
	data, err := db.Get(height)
	require.NoError(t, err)
	require.Equal(t, b2, data)
}
