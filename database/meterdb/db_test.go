// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meterdb

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		baseDB := memdb.New()
		db, err := New("", prometheus.NewRegistry(), baseDB)
		if err != nil {
			t.Fatal(err)
		}

		test(t, db)
	}
}

func BenchmarkInterface(b *testing.B) {
	for _, bench := range database.Benchmarks {
		baseDB := memdb.New()
		db, err := New("", prometheus.NewRegistry(), baseDB)
		if err != nil {
			b.Fatal(err)
		}

		for _, size := range []int{1, 10, 100, 1000, 10000, 100000} {
			bench(b, db, "meterdb", size)
		}
	}
}
