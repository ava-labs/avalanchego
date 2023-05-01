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
	require := require.New(t)

	for _, test := range cache.CacherTests {
		cache := &cache.LRU[ids.ID, int]{Size: test.Size}
		c, err := New[ids.ID, int]("", prometheus.NewRegistry(), cache)
		require.NoError(err)

		test.Func(t, c)
	}
}
