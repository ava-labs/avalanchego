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

func newDB(t testing.TB) *Database {
	folder := t.TempDir()
	cfg := DefaultConfig
	db, err := New(folder, cfg, logging.NoLog{}, "pebble", prometheus.NewRegistry())
	require.NoError(t, err)
	return db
}

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		db := newDB(t)
		test(t, db)
		_ = db.Close()
	}
}

func FuzzKeyValue(f *testing.F) {
	db := newDB(f)
	database.FuzzKeyValue(f, db)
	_ = db.Close()
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	db := newDB(f)
	database.FuzzNewIteratorWithPrefix(f, db)
	_ = db.Close()
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			db := newDB(b)
			bench(b, db, "pebble", keys, values)
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
			bounds := prefixBounds(tt.prefix)
			require.Equal(tt.expectedLower, bounds.LowerBound)
			require.Equal(tt.expectedUpper, bounds.UpperBound)
		})
	}
}
