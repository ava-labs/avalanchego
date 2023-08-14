// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSizedLRU(t *testing.T) {
	cache := NewSizedLRU[ids.ID, int64](TestIntSize, TestIntSizeFunc)

	TestBasic(t, cache)
}

func TestSizedLRUEviction(t *testing.T) {
	cache := NewSizedLRU[ids.ID, int64](2*TestIntSize, TestIntSizeFunc)

	TestEviction(t, cache)
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
