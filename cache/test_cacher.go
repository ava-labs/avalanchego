// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

// CacherTests is a list of all Cacher tests
var CacherTests = []struct {
	Size int
	Func func(t *testing.T, c Cacher[ids.ID, int])
}{
	{Size: 1, Func: TestBasic},
	{Size: 2, Func: TestEviction},
}

func TestBasic(t *testing.T, cache Cacher[ids.ID, int]) {
	require := require.New(t)

	id1 := ids.ID{1}
	_, found := cache.Get(id1)
	require.False(found)

	expectedValue1 := 1
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

	expectedValue2 := 2
	cache.Put(id2, expectedValue2)

	_, found = cache.Get(id1)
	require.False(found)

	value, found = cache.Get(id2)
	require.True(found)
	require.Equal(expectedValue2, value)
}

func TestEviction(t *testing.T, cache Cacher[ids.ID, int]) {
	require := require.New(t)

	id1 := ids.ID{1}
	id2 := ids.ID{2}
	id3 := ids.ID{3}

	cache.Put(id1, 1)
	cache.Put(id2, 2)

	val, found := cache.Get(id1)
	require.True(found)
	require.Equal(1, val)
	val, found = cache.Get(id2)
	require.True(found)
	require.Equal(2, val)
	_, found = cache.Get(id3)
	require.False(found)

	cache.Put(id3, 3)

	_, found = cache.Get(id1)
	require.False(found)
	val, found = cache.Get(id2)
	require.True(found)
	require.Equal(2, val)
	val, found = cache.Get(id3)
	require.True(found)
	require.Equal(3, val)

	cache.Get(id2)
	cache.Put(id1, 1)

	val, found = cache.Get(id1)
	require.True(found)
	require.Equal(1, val)
	val, found = cache.Get(id2)
	require.True(found)
	require.Equal(2, val)
	_, found = cache.Get(id3)
	require.False(found)

	cache.Evict(id2)
	cache.Put(id3, 3)

	val, found = cache.Get(id1)
	require.True(found)
	require.Equal(1, val)
	_, found = cache.Get(id2)
	require.False(found)
	val, found = cache.Get(id3)
	require.True(found)
	require.Equal(3, val)

	cache.Flush()

	_, found = cache.Get(id1)
	require.False(found)
	_, found = cache.Get(id2)
	require.False(found)
	_, found = cache.Get(id3)
	require.False(found)
}
