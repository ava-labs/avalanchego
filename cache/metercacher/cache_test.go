// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
)

func TestInterface(t *testing.T) {
	type scenario struct {
		description string
		setup       func(size int) cache.Cacher[ids.ID, cache.TestSizedInt]
	}

	scenarios := []scenario{
		{
			description: "cache LRU",
			setup: func(size int) cache.Cacher[ids.ID, cache.TestSizedInt] {
				return &cache.LRU[ids.ID, cache.TestSizedInt]{Size: size}
			},
		},
		{
			description: "sized cache LRU",
			setup: func(size int) cache.Cacher[ids.ID, cache.TestSizedInt] {
				return cache.NewSizedLRU[ids.ID, cache.TestSizedInt](size * cache.TestSizedIntSize)
			},
		},
	}

	for _, scenario := range scenarios {
		for _, test := range cache.CacherTests {
			baseCache := scenario.setup(test.Size)
			c, err := New("", prometheus.NewRegistry(), baseCache)
			require.NoError(t, err)
			test.Func(t, c)
		}
	}
}
