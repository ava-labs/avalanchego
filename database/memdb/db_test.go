// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memdb

import (
	"testing"

	"github.com/ava-labs/avalanchego/database"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		test(t, New())
	}
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			db := New()
			bench(b, db, "memdb", keys, values)
		}
	}
}
