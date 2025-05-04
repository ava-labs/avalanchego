// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cachetest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
)

const IntSize = ids.IDLen + 8

func IntSizeFunc(ids.ID, int64) int {
	return IntSize
}

// Tests is a list of all Cacher tests
var Tests = []struct {
	Size int
	Func func(t *testing.T, c cache.Cacher[ids.ID, int64])
}{
	{Size: 1, Func: Basic},
	{Size: 2, Func: Eviction},
}

func Basic(t *testing.T, cache cache.Cacher[ids.ID, int64]) {
	require := require.New(t)

	id1 := ids.ID{1}
	_, found := cache.Get(id1)
	require.False(found)

	expectedValue1 := int64(1)
	cache.Put(id1, expectedValue1)
	value, found := cache.Get(id1)
	require.True(found)
	require.Equal(expectedValue1, value)

	cache.Put(id1, expectedValue1)
	value, found = cache.Get(id1)
	require.True(found)
	require.Equal(expectedValue1, value)

	cache.Put(id1, expectedValue1)
	value, found = cache.Get(id1)
	require.True(found)
	require.Equal(expectedValue1, value)

	id2 := ids.ID{2}

	expectedValue2 := int64(2)
	cache.Put(id2, expectedValue2)
	_, found = cache.Get(id1)
	require.False(found)

	value, found = cache.Get(id2)
	require.True(found)
	require.Equal(expectedValue2, value)
}

func Eviction(t *testing.T, cache cache.Cacher[ids.ID, int64]) {
	require := require.New(t)

	id1 := ids.ID{1}
	id2 := ids.ID{2}
	id3 := ids.ID{3}

	expectedValue1 := int64(1)
	expectedValue2 := int64(2)
	expectedValue3 := int64(3)

	require.Zero(cache.Len())

	cache.Put(id1, expectedValue1)

	require.Equal(1, cache.Len())

	cache.Put(id2, expectedValue2)

	require.Equal(2, cache.Len())

	val, found := cache.Get(id1)
	require.True(found)
	require.Equal(expectedValue1, val)

	val, found = cache.Get(id2)
	require.True(found)
	require.Equal(expectedValue2, val)

	_, found = cache.Get(id3)
	require.False(found)

	cache.Put(id3, expectedValue3)
	require.Equal(2, cache.Len())

	_, found = cache.Get(id1)
	require.False(found)

	val, found = cache.Get(id2)
	require.True(found)
	require.Equal(expectedValue2, val)

	val, found = cache.Get(id3)
	require.True(found)
	require.Equal(expectedValue3, val)

	cache.Get(id2)
	cache.Put(id1, expectedValue1)

	val, found = cache.Get(id1)
	require.True(found)
	require.Equal(expectedValue1, val)

	val, found = cache.Get(id2)
	require.True(found)
	require.Equal(expectedValue2, val)

	_, found = cache.Get(id3)
	require.False(found)

	cache.Evict(id2)
	cache.Put(id3, expectedValue3)

	val, found = cache.Get(id1)
	require.True(found)
	require.Equal(expectedValue1, val)

	_, found = cache.Get(id2)
	require.False(found)

	val, found = cache.Get(id3)
	require.True(found)
	require.Equal(expectedValue3, val)

	cache.Flush()

	_, found = cache.Get(id1)
	require.False(found)
	_, found = cache.Get(id2)
	require.False(found)
	_, found = cache.Get(id3)
	require.False(found)
}
