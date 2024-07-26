// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lru

import (
	"crypto/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

// TODO: What is this benchmarking?
func BenchmarkCachePut(b *testing.B) {
	sizes := []int{5, 250, 10000}
	for _, size := range sizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			cache := NewCache[ids.ID, int](size)
			for n := 0; n < b.N; n++ {
				for i := 0; i < size; i++ {
					var id ids.ID
					_, err := rand.Read(id[:])
					require.NoError(b, err)
					cache.Put(id, n)
				}
				b.StopTimer()
				cache.Flush()
				b.StartTimer()
			}
		})
	}
}
