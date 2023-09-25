// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leveldb

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		folder := t.TempDir()
		db, err := New(folder, nil, logging.NoLog{}, "", prometheus.NewRegistry())
		require.NoError(t, err)

		test(t, db)

		_ = db.Close()
	}
}

func FuzzKeyValue(f *testing.F) {
	folder := f.TempDir()
	db, err := New(folder, nil, logging.NoLog{}, "", prometheus.NewRegistry())
	require.NoError(f, err)

	defer db.Close()

	database.FuzzKeyValue(f, db)
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	folder := f.TempDir()
	db, err := New(folder, nil, logging.NoLog{}, "", prometheus.NewRegistry())
	require.NoError(f, err)

	defer db.Close()

	database.FuzzNewIteratorWithPrefix(f, db)
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			folder := b.TempDir()

			db, err := New(folder, nil, logging.NoLog{}, "", prometheus.NewRegistry())
			require.NoError(b, err)

			bench(b, db, "leveldb", keys, values)

			// The database may have been closed by the test, so we don't care if it
			// errors here.
			_ = db.Close()
		}
	}
}
