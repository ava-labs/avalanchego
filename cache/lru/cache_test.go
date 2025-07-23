// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lru

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/cache/cachetest"
	"github.com/ava-labs/avalanchego/ids"
)

func TestCache(t *testing.T) {
	c := NewCache[ids.ID, int64](1)
	cachetest.Basic(t, c)
}

func TestCacheEviction(t *testing.T) {
	c := NewCache[ids.ID, int64](2)
	cachetest.Eviction(t, c)
}

func TestCacheOnEvict(t *testing.T) {
	tests := []struct {
		name                  string
		cacheSize             int
		operations            func(*Cache[int, int])
		expectedEvictedKeys   []int
		expectedEvictedValues []int
	}{
		{
			name:      "OnEvict on Put with size limit",
			cacheSize: 1,
			operations: func(c *Cache[int, int]) {
				// Put first item
				c.Put(1, 1)
				// Put second item, should evict first
				c.Put(2, 2)
			},
			expectedEvictedKeys:   []int{1},
			expectedEvictedValues: []int{1}, // first item evicted
		},
		{
			name:      "OnEvict on explicit Evict",
			cacheSize: 2,
			operations: func(c *Cache[int, int]) {
				// Put two items
				c.Put(1, 1)
				c.Put(2, 2)
				// Explicitly evict one
				c.Evict(1)
			},
			expectedEvictedKeys:   []int{1},
			expectedEvictedValues: []int{1}, // explicitly evicted
		},
		{
			name:      "OnEvict on Flush",
			cacheSize: 2,
			operations: func(c *Cache[int, int]) {
				// Put two items
				c.Put(1, 1)
				c.Put(2, 2)
				// Flush should evict both
				c.Flush()
			},
			expectedEvictedKeys:   []int{1, 2},
			expectedEvictedValues: []int{1, 2}, // both evicted in order
		},
		{
			name:      "OnEvict on multiple operations",
			cacheSize: 2,
			operations: func(c *Cache[int, int]) {
				// Put three items, should evict first
				c.Put(1, 1)
				c.Put(2, 2)
				c.Put(3, 3)
				// Evict one more
				c.Evict(2)
				// Flush remaining
				c.Flush()
			},
			expectedEvictedKeys:   []int{1, 2, 3},
			expectedEvictedValues: []int{1, 2, 3}, // evicted in order: 1 (by Put), 2 (by Evict), 3 (by Flush)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evictedKeys := make([]int, 0)
			evictedValues := make([]int, 0)
			c := NewCacheWithCallback(tt.cacheSize, func(key, value int) {
				evictedKeys = append(evictedKeys, key)
				evictedValues = append(evictedValues, value)
			})

			tt.operations(c)

			// Validate keys and values match expected evictions
			require.Equal(t, tt.expectedEvictedKeys, evictedKeys)
			require.Equal(t, tt.expectedEvictedValues, evictedValues)
		})
	}
}
