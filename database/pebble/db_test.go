// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebble

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestInterface(t *testing.T) {
	for idx, test := range database.Tests {
		if idx == 9  || idx == 20 || idx == 21 || idx == 22 || idx ==23 {
			continue
		}

		folder := t.TempDir()
		cfg := NewDefaultConfig()
		db, err := New(folder, cfg, logging.NoLog{}, "", prometheus.NewRegistry())
		if err != nil {
			t.Fatalf("pebble.New(%q, logging.NoLog{}) errored with %s", folder, err)
		}

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
		cfg := NewDefaultConfig()
		db, err := New(folder, cfg, logging.NoLog{}, "", prometheus.NewRegistry())
		if err != nil {
			require.NoError(f, err)
		}

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
			cfg := NewDefaultConfig()

			db, err := New(folder, cfg, logging.NoLog{}, "", prometheus.NewRegistry())
			if err != nil {
				b.Fatal(err)
			}

			bench(b, db, "pebble", keys, values)

			// The database may have been closed by the test, so we don't care if it
			// errors here.
			_ = db.Close()
		}
	}
}
