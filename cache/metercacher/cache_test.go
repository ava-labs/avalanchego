// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
)

func TestInterface(t *testing.T) {
	for _, test := range cache.CacherTests {
		cache := &cache.LRU[ids.ID, int]{Size: test.Size}
		c, err := New[ids.ID, int]("", prometheus.NewRegistry(), cache)
		if err != nil {
			t.Fatal(err)
		}

		test.Func(t, c)
	}
}
