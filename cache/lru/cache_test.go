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
		name            string
		cacheSize       int
		operations      func(*Cache[ids.ID, int64])
		expectedEvicted map[ids.ID]int64
	}{
		{
			name:      "OnEvict on Put with size limit",
			cacheSize: 1,
			operations: func(c *Cache[ids.ID, int64]) {
				// Put first item
				c.Put(ids.ID{1}, int64(1))
				// Put second item, should evict first
				c.Put(ids.ID{2}, int64(2))
			},
			expectedEvicted: map[ids.ID]int64{
				{1}: int64(1),
			},
		},
		{
			name:      "OnEvict on explicit Evict",
			cacheSize: 2,
			operations: func(c *Cache[ids.ID, int64]) {
				// Put two items
				c.Put(ids.ID{1}, int64(1))
				c.Put(ids.ID{2}, int64(2))
				// Explicitly evict one
				c.Evict(ids.ID{1})
			},
			expectedEvicted: map[ids.ID]int64{
				{1}: int64(1),
			},
		},
		{
			name:      "OnEvict on Flush",
			cacheSize: 2,
			operations: func(c *Cache[ids.ID, int64]) {
				// Put two items
				c.Put(ids.ID{1}, int64(1))
				c.Put(ids.ID{2}, int64(2))
				// Flush should evict both
				c.Flush()
			},
			expectedEvicted: map[ids.ID]int64{
				{1}: int64(1),
				{2}: int64(2),
			},
		},
		{
			name:      "OnEvict on multiple operations",
			cacheSize: 2,
			operations: func(c *Cache[ids.ID, int64]) {
				// Put three items, should evict first
				c.Put(ids.ID{1}, int64(1))
				c.Put(ids.ID{2}, int64(2))
				c.Put(ids.ID{3}, int64(3))
				// Evict one more
				c.Evict(ids.ID{2})
				// Flush remaining
				c.Flush()
			},
			expectedEvicted: map[ids.ID]int64{
				{1}: int64(1), // evicted by Put
				{2}: int64(2), // evicted by Evict
				{3}: int64(3), // evicted by Flush
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCache[ids.ID, int64](tt.cacheSize)

			evicted := make(map[ids.ID]int64)
			c.SetOnEvict(func(key ids.ID, value int64) {
				evicted[key] = value
			})

			tt.operations(c)

			require.Equal(t, tt.expectedEvicted, evicted)
		})
	}
}
