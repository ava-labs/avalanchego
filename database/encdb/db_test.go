// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package encdb

import (
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestInterface(t *testing.T) {
	pw := "lol totally a secure password"
	for _, test := range database.Tests {
		unencryptedDB := memdb.New()
		db, err := New([]byte(pw), unencryptedDB)
		if err != nil {
			t.Fatal(err)
		}

		test(t, db)
	}
}

func BenchmarkInterface(b *testing.B) {
	pw := "lol totally a secure password"
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size, size)
		for _, bench := range database.Benchmarks {
			unencryptedDB := memdb.New()
			db, err := New([]byte(pw), unencryptedDB)
			if err != nil {
				b.Fatal(err)
			}
			bench(b, db, "encdb", keys, values)
		}
	}
}
