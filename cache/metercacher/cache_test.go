// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/cachetest"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
)

func TestInterface(t *testing.T) {
	scenarios := []struct {
		name  string
		setup func(size int) cache.Cacher[ids.ID, int64]
	}{
		{
			name: "cache LRU",
			setup: func(size int) cache.Cacher[ids.ID, int64] {
				return lru.NewCache[ids.ID, int64](size)
			},
		},
		{
			name: "sized cache LRU",
			setup: func(size int) cache.Cacher[ids.ID, int64] {
				return lru.NewSizedCache(size*cachetest.IntSize, cachetest.IntSizeFunc)
			},
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			for _, test := range cachetest.Tests {
				baseCache := scenario.setup(test.Size)
				c, err := New("", prometheus.NewRegistry(), baseCache)
				require.NoError(t, err)
				test.Func(t, c)
			}
		})
	}
}
