// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lru

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/cache/cachetest"
	"github.com/ava-labs/avalanchego/ids"
)

func TestSizedCache(t *testing.T) {
	c := NewSizedCache[ids.ID, int64](cachetest.IntSize, cachetest.IntSizeFunc)
	cachetest.Basic(t, c)
}

func TestSizedCacheEviction(t *testing.T) {
	c := NewSizedCache[ids.ID, int64](2*cachetest.IntSize, cachetest.IntSizeFunc)
	cachetest.Eviction(t, c)
}

func TestSizedCacheWrongKeyEvictionRegression(t *testing.T) {
	require := require.New(t)

	cache := NewSizedCache[string, struct{}](
		3,
		func(key string, _ struct{}) int {
			return len(key)
		},
	)

	cache.Put("a", struct{}{})
	cache.Put("b", struct{}{})
	cache.Put("c", struct{}{})
	cache.Put("dd", struct{}{})

	_, ok := cache.Get("a")
	require.False(ok)

	_, ok = cache.Get("b")
	require.False(ok)

	_, ok = cache.Get("c")
	require.True(ok)

	_, ok = cache.Get("dd")
	require.True(ok)
}

func TestSizedLRUSizeAlteringRegression(t *testing.T) {
	require := require.New(t)

	cache := NewSizedCache[string, *string](
		5,
		func(key string, val *string) int {
			if val != nil {
				return len(key) + len(*val)
			}

			return len(key)
		},
	)

	// put first value
	expectedPortionFilled := 0.6
	valueA := "ab"
	cache.Put("a", &valueA)

	require.InDelta(expectedPortionFilled, cache.PortionFilled(), 0)

	// mutate first value
	valueA = "abcd"
	require.InDelta(expectedPortionFilled, cache.PortionFilled(), 0, "after value A mutation, portion filled should be the same")

	// put second value
	expectedPortionFilled = 0.8
	valueB := "bcd"
	cache.Put("b", &valueB)

	require.InDelta(expectedPortionFilled, cache.PortionFilled(), 0)

	_, ok := cache.Get("a")
	require.False(ok, "key a shouldn't exist after b is put")

	_, ok = cache.Get("b")
	require.True(ok, "key b should exist")
}
