// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prefixdb

import (
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		db := memdb.New()
		test(t, New([]byte("hello"), db))
		test(t, New([]byte("world"), db))
		test(t, New([]byte("wor"), New([]byte("ld"), db)))
		test(t, New([]byte("ld"), New([]byte("wor"), db)))
		test(t, NewNested([]byte("wor"), New([]byte("ld"), db)))
		test(t, NewNested([]byte("ld"), New([]byte("wor"), db)))
	}
}

func BenchmarkInterface(b *testing.B) {
	for _, bench := range database.Benchmarks {
		for _, size := range []int{32, 64, 128, 256, 512, 1024, 2048, 4096} {
			db := New([]byte("hello"), memdb.New())
			bench(b, db, "prefixdb", 1000, size)
		}
	}
	for _, bench := range database.Benchmarks {
		for _, size := range []int{32, 64, 128, 256, 512, 1024, 2048, 4096} {
			db := NewNested([]byte("hello"), memdb.New())
			bench(b, db, "prefixdb_nested", 1000, size)
		}
	}
}
