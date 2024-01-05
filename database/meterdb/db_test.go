// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meterdb

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		baseDB := memdb.New()
		db, err := New("", prometheus.NewRegistry(), baseDB)
		require.NoError(t, err)

		test(t, db)
	}
}

func newDB(t testing.TB) database.Database {
	baseDB := memdb.New()
	db, err := New("", prometheus.NewRegistry(), baseDB)
	require.NoError(t, err)
	return db
}

func FuzzKeyValue(f *testing.F) {
	database.FuzzKeyValue(f, newDB(f))
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	database.FuzzNewIteratorWithPrefix(f, newDB(f))
}

func FuzzNewIteratorWithStartAndPrefix(f *testing.F) {
	database.FuzzNewIteratorWithStartAndPrefix(f, newDB(f))
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			bench(b, newDB(b), "meterdb", keys, values)
		}
	}
}
