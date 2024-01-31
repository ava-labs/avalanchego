// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memdb

import (
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/database"
)

func TestInterface(t *testing.T) {
	for name, test := range database.Tests {
		t.Run(name, func(t *testing.T) {
			test(t, New())
		})
	}
}

func FuzzKeyValue(f *testing.F) {
	database.FuzzKeyValue(f, New())
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	database.FuzzNewIteratorWithPrefix(f, New())
}

func FuzzNewIteratorWithStartAndPrefix(f *testing.F) {
	database.FuzzNewIteratorWithStartAndPrefix(f, New())
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for name, bench := range database.Benchmarks {
			b.Run(fmt.Sprintf("memdb_%d_pairs_%d_keys_%d_values_%s", size[0], size[1], size[2], name), func(b *testing.B) {
				db := New()
				bench(b, db, keys, values)
			})
		}
	}
}
