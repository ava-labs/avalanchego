// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dbtest

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
)

// Tests is a list of all database tests
var Tests = []struct {
	Name string
	Test func(t *testing.T, newDB func() database.HeightIndex)
}{
	{"TestPutGet", TestPutGet},
	{"TestHas", TestHas},
	{"TestSync", TestSync},
	{"TestCloseAndPut", TestCloseAndPut},
	{"TestCloseAndGet", TestCloseAndGet},
	{"TestCloseAndHas", TestCloseAndHas},
	{"TestCloseAndSync", TestCloseAndSync},
	{"TestClose", TestClose},
}

type putArgs struct {
	height uint64
	data   []byte
}

func TestPutGet(t *testing.T, newDB func() database.HeightIndex) {
	tests := []struct {
		name        string
		puts        []putArgs
		queryHeight uint64
		want        []byte
		wantErr     error
	}{
		{
			name: "normal operation",
			puts: []putArgs{
				{1, []byte("test data 1")},
			},
			queryHeight: 1,
			want:        []byte("test data 1"),
		},
		{
			name: "not found error when getting on non-existing height",
			puts: []putArgs{
				{1, []byte("test data")},
			},
			queryHeight: 2,
			wantErr:     database.ErrNotFound,
		},
		{
			name: "overwriting data on same height",
			puts: []putArgs{
				{1, []byte("original data")},
				{1, []byte("overwritten data")},
			},
			queryHeight: 1,
			want:        []byte("overwritten data"),
		},
		{
			name: "put and get nil data",
			puts: []putArgs{
				{1, nil},
			},
			queryHeight: 1,
		},
		{
			name: "put and get empty bytes",
			puts: []putArgs{
				{1, []byte{}},
			},
			queryHeight: 1,
		},
		{
			name: "put and get large data",
			puts: []putArgs{
				{1, make([]byte, 1000)},
			},
			queryHeight: 1,
			want:        make([]byte, 1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newDB()
			defer func() {
				require.NoError(t, db.Close())
			}()

			for _, write := range tt.puts {
				require.NoError(t, db.Put(write.height, write.data))
			}

			retrievedData, err := db.Get(tt.queryHeight)
			require.ErrorIs(t, err, tt.wantErr)
			require.True(t, bytes.Equal(tt.want, retrievedData))
		})
	}
}

func TestHas(t *testing.T, newDB func() database.HeightIndex) {
	tests := []struct {
		name        string
		puts        []putArgs
		queryHeight uint64
		want        bool
	}{
		{
			name:        "non-existent item",
			queryHeight: 1,
		},
		{
			name:        "existing item with data",
			puts:        []putArgs{{1, []byte("test data")}},
			queryHeight: 1,
			want:        true,
		},
		{
			name:        "existing item with nil data",
			puts:        []putArgs{{1, nil}},
			queryHeight: 1,
			want:        true,
		},
		{
			name:        "existing item with empty bytes",
			puts:        []putArgs{{1, []byte{}}},
			queryHeight: 1,
			want:        true,
		},
		{
			name: "has returns true on overridden height",
			puts: []putArgs{
				{1, []byte("original data")},
				{1, []byte("overridden data")},
			},
			queryHeight: 1,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newDB()
			defer func() {
				require.NoError(t, db.Close())
			}()

			// Perform all puts
			for _, write := range tt.puts {
				require.NoError(t, db.Put(write.height, write.data))
			}

			ok, err := db.Has(tt.queryHeight)
			require.NoError(t, err)
			require.Equal(t, tt.want, ok)
		})
	}
}

func TestCloseAndPut(t *testing.T, newDB func() database.HeightIndex) {
	db := newDB()
	require.NoError(t, db.Close())

	// Try to put after close - should return error
	err := db.Put(1, []byte("test"))
	require.ErrorIs(t, err, database.ErrClosed)
}

func TestCloseAndGet(t *testing.T, newDB func() database.HeightIndex) {
	db := newDB()
	require.NoError(t, db.Close())

	// Try to get after close - should return error
	_, err := db.Get(1)
	require.ErrorIs(t, err, database.ErrClosed)
}

func TestCloseAndHas(t *testing.T, newDB func() database.HeightIndex) {
	db := newDB()
	require.NoError(t, db.Close())

	// Try to has after close - should return error
	_, err := db.Has(1)
	require.ErrorIs(t, err, database.ErrClosed)
}

func TestCloseAndSync(t *testing.T, newDB func() database.HeightIndex) {
	db := newDB()
	require.NoError(t, db.Close())

	// Try to sync after close - should return error
	err := db.Sync(1, 10)
	require.ErrorIs(t, err, database.ErrClosed)
}

func TestClose(t *testing.T, newDB func() database.HeightIndex) {
	db := newDB()
	require.NoError(t, db.Close())

	// Second close should return error
	err := db.Close()
	require.ErrorIs(t, err, database.ErrClosed)
}

func TestSync(t *testing.T, newDB func() database.HeightIndex) {
	tests := []struct {
		name    string
		heights []uint64
		start   uint64
		end     uint64
	}{
		{
			name: "empty range",
			end:  10,
		},
		{
			name:    "single height",
			heights: []uint64{5},
			start:   5,
			end:     5,
		},
		{
			name:    "contiguous range",
			heights: []uint64{1, 2, 3},
			start:   1,
			end:     3,
		},
		{
			name:    "partial range",
			heights: []uint64{0, 1, 2, 3, 4, 5},
			start:   2,
			end:     4,
		},
		{
			name:    "range with gaps",
			heights: []uint64{1, 3, 5},
			start:   0,
			end:     10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newDB()
			defer func() {
				require.NoError(t, db.Close())
			}()

			for _, h := range tt.heights {
				require.NoError(t, db.Put(h, []byte("data")))
			}

			require.NoError(t, db.Sync(tt.start, tt.end))

			for _, h := range tt.heights {
				has, err := db.Has(h)
				require.NoError(t, err)
				require.True(t, has)
			}
		})
	}
}
