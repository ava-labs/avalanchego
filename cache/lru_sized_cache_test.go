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

func TestSizedLRU(t *testing.T) {
	cache := NewSizedLRU[ids.ID, int64](cachetest.IntSize, cachetest.IntSizeFunc)

	cachetest.TestBasic(t, cache)
}

func TestSizedLRUEviction(t *testing.T) {
	cache := NewSizedLRU[ids.ID, int64](2*cachetest.IntSize, cachetest.IntSizeFunc)

	cachetest.TestEviction(t, cache)
}

func TestSizedLRUWrongKeyEvictionRegression(t *testing.T) {
	require := require.New(t)

	cache := NewSizedLRU[string, struct{}](
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

	cache := NewSizedLRU[string, *string](
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

	require.Equal(expectedPortionFilled, cache.PortionFilled())

	// mutate first value
	valueA = "abcd"
	require.Equal(expectedPortionFilled, cache.PortionFilled(), "after value A mutation, portion filled should be the same")

	// put second value
	expectedPortionFilled = 0.8
	valueB := "bcd"
	cache.Put("b", &valueB)

	require.Equal(expectedPortionFilled, cache.PortionFilled())

	_, ok := cache.Get("a")
	require.False(ok, "key a shouldn't exist after b is put")

	_, ok = cache.Get("b")
	require.True(ok, "key b should exist")
}
