// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var errTest = errors.New("test error")

func TestNewOnEvictCache(t *testing.T) {
	require := require.New(t)

	called := false
	size := func(int, int) int {
		return 1
	}
	onEviction := func(int, int) error {
		called = true
		return nil
	}
	maxSize := 10

	cache := newOnEvictCache(maxSize, size, onEviction)
	require.Equal(maxSize, cache.maxSize)
	require.NotNil(cache.fifo)
	require.Zero(cache.fifo.Len())
	// Can't test function equality directly so do this
	// to make sure it was assigned correctly
	require.NoError(cache.onEviction(0, 0))
	require.True(called)
}

// Test the functionality of the cache when the onEviction function
// never returns an error.
// Note this test assumes the internal cache is a FIFO cache
func TestOnEvictCacheNoOnEvictionError(t *testing.T) {
	require := require.New(t)

	evictedKey := []int{}
	evictedValue := []int{}
	size := func(int, int) int {
		return 1
	}
	onEviction := func(k, n int) error {
		evictedKey = append(evictedKey, k)
		evictedValue = append(evictedValue, n)
		return nil
	}
	maxSize := 3

	cache := newOnEvictCache(maxSize, size, onEviction)

	// Get non-existent key
	_, ok := cache.Get(0)
	require.False(ok)

	// Put key
	require.NoError(cache.Put(0, 0))
	require.Equal(1, cache.fifo.Len())

	// Get key
	val, ok := cache.Get(0)
	require.True(ok)
	require.Zero(val)

	// Get non-existent key
	_, ok = cache.Get(1)
	require.False(ok)

	// Fill the cache
	for i := 1; i < maxSize; i++ {
		require.NoError(cache.Put(i, i))
		require.Equal(i+1, cache.fifo.Len())
	}
	require.Empty(evictedKey)
	require.Empty(evictedValue)
	// Cache has [0,1,2]

	// Put another key. This should evict the oldest inserted key (0).
	require.NoError(cache.Put(maxSize, maxSize))
	require.Equal(maxSize, cache.fifo.Len())
	require.Len(evictedKey, 1)
	require.Zero(evictedKey[0])
	require.Len(evictedValue, 1)
	require.Zero(evictedValue[0])

	// Cache has [1,2,3]
	iter := cache.fifo.NewIterator()
	require.True(iter.Next())
	require.Equal(1, iter.Key())
	require.Equal(1, iter.Value())
	require.True(iter.Next())
	require.Equal(2, iter.Key())
	require.Equal(2, iter.Value())
	require.True(iter.Next())
	require.Equal(3, iter.Key())
	require.Equal(3, iter.Value())
	require.False(iter.Next())

	// 0 should no longer be in the cache
	_, ok = cache.Get(0)
	require.False(ok)

	// Other keys should still be in the cache
	for i := maxSize; i >= 1; i-- {
		val, ok := cache.Get(i)
		require.True(ok)
		require.Equal(i, val)
	}

	// Cache has [1,2,3]
	iter = cache.fifo.NewIterator()
	require.True(iter.Next())
	require.Equal(1, iter.Key())
	require.Equal(1, iter.Value())
	require.True(iter.Next())
	require.Equal(2, iter.Key())
	require.Equal(2, iter.Value())
	require.True(iter.Next())
	require.Equal(3, iter.Key())
	require.Equal(3, iter.Value())
	require.False(iter.Next())

	// Put another key to evict the oldest inserted key (1).
	require.NoError(cache.Put(maxSize+1, maxSize+1))
	require.Equal(maxSize, cache.fifo.Len())
	require.Len(evictedKey, 2)
	require.Equal(1, evictedKey[1])
	require.Len(evictedValue, 2)
	require.Equal(1, evictedValue[1])

	// Cache has [2,3,4]
	iter = cache.fifo.NewIterator()
	require.True(iter.Next())
	require.Equal(2, iter.Key())
	require.Equal(2, iter.Value())
	require.True(iter.Next())
	require.Equal(3, iter.Key())
	require.Equal(3, iter.Value())
	require.True(iter.Next())
	require.Equal(4, iter.Key())
	require.Equal(4, iter.Value())
	require.False(iter.Next())

	// 1 should no longer be in the cache
	_, ok = cache.Get(1)
	require.False(ok)

	require.NoError(cache.Flush())

	// Cache should be empty
	require.Zero(cache.fifo.Len())
	require.Len(evictedKey, 5)
	require.Equal([]int{0, 1, 2, 3, 4}, evictedKey)
	require.Len(evictedValue, 5)
	require.Equal([]int{0, 1, 2, 3, 4}, evictedValue)
	require.Zero(cache.fifo.Len())
	require.Equal(maxSize, cache.maxSize) // Should be unchanged
}

// Test the functionality of the cache when the onEviction function
// returns an error.
// Note this test assumes the cache is FIFO.
func TestOnEvictCacheOnEvictionError(t *testing.T) {
	var (
		require = require.New(t)
		evicted = []int{}
		size    = func(int, int) int {
			return 1
		}
		onEviction = func(_, n int) error {
			// Evicting even keys errors
			evicted = append(evicted, n)
			if n%2 == 0 {
				return errTest
			}
			return nil
		}
		maxSize = 2
	)

	cache := newOnEvictCache(maxSize, size, onEviction)

	// Fill the cache
	for i := 0; i < maxSize; i++ {
		require.NoError(cache.Put(i, i))
		require.Equal(i+1, cache.fifo.Len())
	}

	// Cache has [0,1]

	// Put another key. This should evict the first key (0)
	// and return an error since 0 is even.
	err := cache.Put(maxSize, maxSize)
	require.ErrorIs(err, errTest)

	// Cache has [1,2]
	require.Equal([]int{0}, evicted)
	require.Equal(maxSize, cache.fifo.Len())
	_, ok := cache.Get(0)
	require.False(ok)
	_, ok = cache.Get(1)
	require.True(ok)
	_, ok = cache.Get(2)
	require.True(ok)

	// Flush the cache. Should error on last element (2).
	err = cache.Flush()
	require.ErrorIs(err, errTest)

	// Should still be empty.
	require.Zero(cache.fifo.Len())
	require.Equal([]int{0, 1, 2}, evicted)
	_, ok = cache.Get(0)
	require.False(ok)
	_, ok = cache.Get(1)
	require.False(ok)
	_, ok = cache.Get(2)
	require.False(ok)
}
