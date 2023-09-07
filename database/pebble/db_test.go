// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebble

import (
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		folder := t.TempDir()
		cfg := DefaultConfig
		db, err := New(folder, cfg, logging.NoLog{}, "pebble", prometheus.NewRegistry())
		require.NoError(t, err)
		defer db.Close()

		test(t, db)

		// The database may have been closed by the test, so we don't care if it
		// errors here.
		_ = db.Close()
	}
}

func FuzzInterface(f *testing.F) {
	for _, test := range database.FuzzTests {
		folder := f.TempDir()
		cfg := DefaultConfig
		db, err := New(folder, cfg, logging.NoLog{}, "", prometheus.NewRegistry())
		require.NoError(f, err)
		defer db.Close()

		test(f, db)

		// The database may have been closed by the test, so we don't care if it
		// errors here.
		_ = db.Close()
	}
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			folder := b.TempDir()
			cfg := DefaultConfig

			db, err := New(folder, cfg, logging.NoLog{}, "", prometheus.NewRegistry())
			require.NoError(b, err)
			bench(b, db, "pebble", keys, values)

			// The database may have been closed by the test, so we don't care if it
			// errors here.
			_ = db.Close()
		}
	}
}

func Test_bytesPrefix(t *testing.T) {
	require := require.New(t)

	prefs := [][]byte{
		{},
		{1},
		{1},
		{1, 2, 3},
		{1, 2, 3, 4, 5},
		{1, 2, 3, 4, 5, 8, 19, 29},
	}

	itopts := make([]*pebble.IterOptions, 0)
	for _, pref := range prefs {
		itopts = append(itopts, prefixBounds(pref))
	}

	for idx, itopt := range itopts {
		if lbLen := len(itopt.LowerBound); lbLen > 0 {
			require.Equal(prefs[idx], itopt.LowerBound)
			itopt.LowerBound[lbLen-1] += 1
			require.Equal(itopt.LowerBound, itopt.UpperBound)
		}
	}
}
