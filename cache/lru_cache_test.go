// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestLRU(t *testing.T) {
	cache := &LRU[ids.ID, TestSizedInt]{Size: 1}

	TestBasic(t, cache)
}

func TestLRUEviction(t *testing.T) {
	cache := &LRU[ids.ID, TestSizedInt]{Size: 2}

	TestEviction(t, cache)
}

func TestLRUResize(t *testing.T) {
	require := require.New(t)
	cache := LRU[ids.ID, TestSizedInt]{Size: 2}

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
