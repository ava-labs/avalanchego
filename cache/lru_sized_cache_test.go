// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestSizedLRU(t *testing.T) {
	cache := NewSizedLRU[ids.ID, TestSizedInt](TestSizedIntSize)

	TestBasic(t, cache)
}

func TestSizedtLRUEviction(t *testing.T) {
	cache := NewSizedLRU[ids.ID, TestSizedInt](2 * TestSizedIntSize)

	TestEviction(t, cache)
}

func TestSizedLRUResize(t *testing.T) {
	require := require.New(t)
	cacheIntf := NewSizedLRU[ids.ID, TestSizedInt](2 * TestSizedIntSize)
	cache, ok := cacheIntf.(*sizedLRU[ids.ID, TestSizedInt])
	require.True(ok)

	id1 := ids.ID{1}
	id2 := ids.ID{2}

	expectedVal1 := TestSizedInt{i: 1}
	expectedVal2 := TestSizedInt{i: 2}
	cache.Put(id1, expectedVal1)
	cache.Put(id2, expectedVal2)

	val, found := cache.Get(id1)
	require.True(found)
	require.Equal(expectedVal1, val)

	val, found = cache.Get(id2)
	require.True(found)
	require.Equal(expectedVal2, val)

	cache.maxSize = TestSizedIntSize
	// id1 evicted

	_, found = cache.Get(id1)
	require.False(found)

	val, found = cache.Get(id2)
	require.True(found)
	require.Equal(expectedVal2, val)

	cache.maxSize = 0
	// We reset the size to 1 in resize

	_, found = cache.Get(id1)
	require.False(found)

	val, found = cache.Get(id2)
	require.True(found)
	require.Equal(expectedVal2, val)
}
