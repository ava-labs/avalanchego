// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meterdb

import (
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/dbtest"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestInterface(t *testing.T) {
	for name, test := range dbtest.Tests {
		t.Run(name, func(t *testing.T) {
			baseDB := memdb.New()
			db, err := New(prometheus.NewRegistry(), baseDB)
			require.NoError(t, err)

			test(t, db)
		})
	}
}

func newDB(t testing.TB) database.Database {
	baseDB := memdb.New()
	db, err := New(prometheus.NewRegistry(), baseDB)
	require.NoError(t, err)
	return db
}

func FuzzKeyValue(f *testing.F) {
	dbtest.FuzzKeyValue(f, newDB(f))
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	dbtest.FuzzNewIteratorWithPrefix(f, newDB(f))
}

func FuzzNewIteratorWithStartAndPrefix(f *testing.F) {
	dbtest.FuzzNewIteratorWithStartAndPrefix(f, newDB(f))
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range dbtest.BenchmarkSizes {
		keys, values := dbtest.SetupBenchmark(b, size[0], size[1], size[2])
		for name, bench := range dbtest.Benchmarks {
			b.Run(fmt.Sprintf("meterdb_%d_pairs_%d_keys_%d_values_%s", size[0], size[1], size[2], name), func(b *testing.B) {
				bench(b, newDB(b), keys, values)
			})
		}
	}
}
