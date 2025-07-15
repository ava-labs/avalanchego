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

func TestCacheFlushWithOnEvict(t *testing.T) {
	c := NewCache[ids.ID, int64](2)

	// Track which elements were evicted
	evicted := make(map[ids.ID]int64)
	c.SetOnEvict(func(key ids.ID, value int64) {
		evicted[key] = value
	})

	cachetest.Eviction(t, c)
	require.Zero(t, c.Len())
	require.Len(t, evicted, 3)
}

func TestCachePutWithOnEvict(t *testing.T) {
	c := NewCache[ids.ID, int64](1)

	evicted := make(map[ids.ID]int64)
	c.SetOnEvict(func(key ids.ID, value int64) {
		evicted[key] = value
	})

	cachetest.Basic(t, c)
	require.Len(t, evicted, 1)
	require.Equal(t, evicted[ids.ID{1}], int64(1))
}
