// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			baseDB := memdb.New()
			db, err := New("", prometheus.NewRegistry(), baseDB)
			if err != nil {
				b.Fatal(err)
			}
			bench(b, db, "meterdb", keys, values)
		}
	}
}
