// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebbledb

import (
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/dbtest"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func newDB(t testing.TB) *Database {
	folder := t.TempDir()
	db, err := New(folder, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	return db.(*Database)
}

func TestInterface(t *testing.T) {
	for name, test := range dbtest.Tests {
		t.Run(name, func(t *testing.T) {
			db := newDB(t)
			test(t, db)
			_ = db.Close()
		})
	}
}

func FuzzKeyValue(f *testing.F) {
	db := newDB(f)
	dbtest.FuzzKeyValue(f, db)
	_ = db.Close()
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	db := newDB(f)
	dbtest.FuzzNewIteratorWithPrefix(f, db)
	_ = db.Close()
}

func FuzzNewIteratorWithStartAndPrefix(f *testing.F) {
	db := newDB(f)
	dbtest.FuzzNewIteratorWithStartAndPrefix(f, db)
	_ = db.Close()
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range dbtest.BenchmarkSizes {
		keys, values := dbtest.SetupBenchmark(b, size[0], size[1], size[2])
		for name, bench := range dbtest.Benchmarks {
			b.Run(fmt.Sprintf("pebble_%d_pairs_%d_keys_%d_values_%s", size[0], size[1], size[2], name), func(b *testing.B) {
				db := newDB(b)
				bench(b, db, keys, values)
				_ = db.Close()
			})
		}
	}
}

func TestKeyRange(t *testing.T) {
	type test struct {
		start         []byte
		prefix        []byte
		expectedLower []byte
		expectedUpper []byte
	}

	tests := []test{
		{
			start:         nil,
			prefix:        nil,
			expectedLower: nil,
			expectedUpper: nil,
		},
		{
			start:         nil,
			prefix:        []byte{},
			expectedLower: []byte{},
			expectedUpper: nil,
		},
		{
			start:         nil,
			prefix:        []byte{0x00},
			expectedLower: []byte{0x00},
			expectedUpper: []byte{0x01},
		},
		{
			start:         []byte{0x00, 0x02},
			prefix:        []byte{0x00},
			expectedLower: []byte{0x00, 0x02},
			expectedUpper: []byte{0x01},
		},
		{
			start:         []byte{0x01},
			prefix:        []byte{0x00},
			expectedLower: []byte{0x01},
			expectedUpper: []byte{0x01},
		},
		{
			start:         nil,
			prefix:        []byte{0x01},
			expectedLower: []byte{0x01},
			expectedUpper: []byte{0x02},
		},
		{
			start:         nil,
			prefix:        []byte{0xFF},
			expectedLower: []byte{0xFF},
			expectedUpper: nil,
		},
		{
			start:         []byte{0x00},
			prefix:        []byte{0xFF},
			expectedLower: []byte{0xFF},
			expectedUpper: nil,
		},
		{
			start:         nil,
			prefix:        []byte{0x01, 0x02},
			expectedLower: []byte{0x01, 0x02},
			expectedUpper: []byte{0x01, 0x03},
		},
		{
			start:         []byte{0x01, 0x02},
			prefix:        []byte{0x01, 0x02},
			expectedLower: []byte{0x01, 0x02},
			expectedUpper: []byte{0x01, 0x03},
		},
		{
			start:         []byte{0x01, 0x02, 0x05},
			prefix:        []byte{0x01, 0x02},
			expectedLower: []byte{0x01, 0x02, 0x05},
			expectedUpper: []byte{0x01, 0x03},
		},
		{
			start:         nil,
			prefix:        []byte{0x01, 0x02, 0xFF},
			expectedLower: []byte{0x01, 0x02, 0xFF},
			expectedUpper: []byte{0x01, 0x03},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.start)+" "+string(tt.prefix), func(t *testing.T) {
			require := require.New(t)
			bounds := keyRange(tt.start, tt.prefix)
			require.Equal(tt.expectedLower, bounds.LowerBound)
			require.Equal(tt.expectedUpper, bounds.UpperBound)
		})
	}
}
