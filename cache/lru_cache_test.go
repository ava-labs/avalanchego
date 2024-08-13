// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/cache/cachetest"
	"github.com/ava-labs/avalanchego/ids"

	. "github.com/ava-labs/avalanchego/cache"
)

func TestLRU(t *testing.T) {
	cache := &LRU[ids.ID, int64]{Size: 1}

	cachetest.TestBasic(t, cache)
}

func TestLRUEviction(t *testing.T) {
	cache := &LRU[ids.ID, int64]{Size: 2}

	cachetest.TestEviction(t, cache)
}

func TestLRUResize(t *testing.T) {
	require := require.New(t)
	cache := LRU[ids.ID, int64]{Size: 2}

	id1 := ids.ID{1}
	id2 := ids.ID{2}

	expectedVal1 := int64(1)
	expectedVal2 := int64(2)
	cache.Put(id1, expectedVal1)
	cache.Put(id2, expectedVal2)

	val, found := cache.Get(id1)
	require.True(found)
	require.Equal(expectedVal1, val)

	val, found = cache.Get(id2)
	require.True(found)
	require.Equal(expectedVal2, val)

	cache.Size = 1
	// id1 evicted

	_, found = cache.Get(id1)
	require.False(found)

	val, found = cache.Get(id2)
	require.True(found)
	require.Equal(expectedVal2, val)

	cache.Size = 0
	// We reset the size to 1 in resize

	_, found = cache.Get(id1)
	require.False(found)

	val, found = cache.Get(id2)
	require.True(found)
	require.Equal(expectedVal2, val)
}
