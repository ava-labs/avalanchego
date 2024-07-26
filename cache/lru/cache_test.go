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
	c := &Cache[ids.ID, int64]{Size: 1}

	cachetest.Basic(t, c)
}

func TestCacheEviction(t *testing.T) {
	c := &Cache[ids.ID, int64]{Size: 2}

	cachetest.Eviction(t, c)
}

func TestCacheResize(t *testing.T) {
	require := require.New(t)
	c := Cache[ids.ID, int64]{Size: 2}

	id1 := ids.ID{1}
	id2 := ids.ID{2}

	expectedVal1 := int64(1)
	expectedVal2 := int64(2)
	c.Put(id1, expectedVal1)
	c.Put(id2, expectedVal2)

	val, found := c.Get(id1)
	require.True(found)
	require.Equal(expectedVal1, val)

	val, found = c.Get(id2)
	require.True(found)
	require.Equal(expectedVal2, val)

	c.Size = 1
	// id1 evicted

	_, found = c.Get(id1)
	require.False(found)

	val, found = c.Get(id2)
	require.True(found)
	require.Equal(expectedVal2, val)

	c.Size = 0
	// We reset the size to 1 in resize

	_, found = c.Get(id1)
	require.False(found)

	val, found = c.Get(id2)
	require.True(found)
	require.Equal(expectedVal2, val)
}
