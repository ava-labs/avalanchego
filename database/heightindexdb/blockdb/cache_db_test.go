// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
)

func TestCacheOnMiss(t *testing.T) {
	db := newCacheDatabase(t, DefaultConfig())
	height := uint64(20)
	block := randomBlock(t)
	require.NoError(t, db.Put(height, block))

	// Evict the entry from cache to simulate a cache miss
	db.cache.Evict(height)

	// Read the block - should populate the cache on cache miss
	_, err := db.Get(height)
	require.NoError(t, err)

	_, ok := db.cache.Get(height)
	require.True(t, ok)
}

func TestCacheGet(t *testing.T) {
	db := newCacheDatabase(t, DefaultConfig())
	height := uint64(30)
	block := randomBlock(t)

	// Populate cache directly without writing to database
	db.cache.Put(height, block)

	// Get should return the block from cache
	data, err := db.Get(height)
	require.NoError(t, err)
	require.Equal(t, block, data)
}

func TestCacheHas(t *testing.T) {
	db := newCacheDatabase(t, DefaultConfig())
	height := uint64(40)
	block := randomBlock(t)

	// Populate cache directly without writing to database
	db.cache.Put(height, block)

	// Has should return true from cache even though block is not in database
	has, err := db.Has(height)
	require.NoError(t, err)
	require.True(t, has)
}

func TestCachePutOverridesSameHeight(t *testing.T) {
	db := newCacheDatabase(t, DefaultConfig())
	height := uint64(60)
	b1 := randomBlock(t)
	require.NoError(t, db.Put(height, b1))

	// Verify first block is in cache
	cached, ok := db.cache.Get(height)
	require.True(t, ok)
	require.Equal(t, b1, cached)

	// Put second block at same height and verify it overrides the first one
	b2 := randomBlock(t)
	require.NoError(t, db.Put(height, b2))
	cached, ok = db.cache.Get(height)
	require.True(t, ok)
	require.Equal(t, b2, cached)

	// Get should also return the new block
	data, err := db.Get(height)
	require.NoError(t, err)
	require.Equal(t, b2, data)
}

func TestCacheClose(t *testing.T) {
	db := newCacheDatabase(t, DefaultConfig())
	height := uint64(70)
	block := randomBlock(t)
	require.NoError(t, db.Put(height, block))

	_, ok := db.cache.Get(height)
	require.True(t, ok)
	require.NoError(t, db.Close())

	// cache is flushed
	require.Zero(t, db.cache.Len())

	// db operations now fails
	_, err := db.Get(height)
	require.ErrorIs(t, err, database.ErrClosed)
	_, err = db.Has(height)
	require.ErrorIs(t, err, database.ErrClosed)
	err = db.Put(height+1, block)
	require.ErrorIs(t, err, database.ErrClosed)
	err = db.Close()
	require.ErrorIs(t, err, database.ErrClosed)
}
