// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	for _, bench := range database.Benchmarks {
		for _, size := range []int{32, 64, 128, 256, 512, 1024, 2048, 4096} {
			db := New()
			bench(b, db, "memdb", 1000, size)
		}
	}
}
