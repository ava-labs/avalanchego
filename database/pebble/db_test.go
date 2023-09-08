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

func TestPrefixBounds(t *testing.T) {
	require := require.New(t)

	type test struct {
		prefix        []byte
		expectedLower []byte
		expectedUpper []byte
	}

	tests := []test{
		{
			prefix:        nil,
			expectedLower: nil,
			expectedUpper: nil,
		},
		{
			prefix:        []byte{},
			expectedLower: []byte{},
			expectedUpper: nil,
		},
		{
			prefix:        []byte{0x00},
			expectedLower: []byte{0x00},
			expectedUpper: []byte{0x01},
		},
		{
			prefix:        []byte{0x01},
			expectedLower: []byte{0x01},
			expectedUpper: []byte{0x02},
		},
		{
			prefix:        []byte{0xFF},
			expectedLower: []byte{0xFF},
			expectedUpper: nil,
		},
		{
			prefix:        []byte{0x01, 0x02},
			expectedLower: []byte{0x01, 0x02},
			expectedUpper: []byte{0x01, 0x03},
		},
		{
			prefix:        []byte{0x01, 0x02, 0xFF},
			expectedLower: []byte{0x01, 0x02, 0xFF},
			expectedUpper: []byte{0x01, 0x03},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.prefix), func(t *testing.T) {
			itopts := prefixBounds(tt.prefix)
			require.Equal(tt.expectedLower, itopts.LowerBound)
			require.Equal(tt.expectedUpper, itopts.UpperBound)
		})
	}
}
