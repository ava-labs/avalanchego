// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
)

type testCase struct {
	Name string
	Test func(t *testing.T, newDB func() database.HeightIndex)
}

// Tests is a list of all database tests
var Tests = []testCase{
	{"TestPutGet", TestPutGet},
	{"TestHas", TestHas},
	{"TestCloseAndPut", TestCloseAndPut},
	{"TestCloseAndGet", TestCloseAndGet},
	{"TestCloseAndHas", TestCloseAndHas},
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
			want:        nil,
		},
		{
			name: "put and get empty bytes",
			puts: []putArgs{
				{1, []byte{}},
			},
			queryHeight: 1,
			want:        []byte{},
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

			// Perform all puts
			for _, write := range tt.puts {
				require.NoError(t, db.Put(write.height, write.data))
			}

			// modify the original value of the put data to ensure the saved
			// value won't be changed after Get
			if len(tt.puts) > int(tt.queryHeight) && tt.puts[tt.queryHeight].data != nil {
				copy(tt.puts[tt.queryHeight].data, []byte("modified data"))
			}

			// Query the specific height
			retrievedData, err := db.Get(tt.queryHeight)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, retrievedData)

			// modify the data returned from Get and ensure it won't change the
			// data from a second Get
			copy(retrievedData, []byte("modified data"))
			newData, err := db.Get(tt.queryHeight)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, newData)
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

func TestClose(t *testing.T, newDB func() database.HeightIndex) {
	db := newDB()
	require.NoError(t, db.Close())

	// Second close should return error
	err := db.Close()
	require.ErrorIs(t, err, database.ErrClosed)
}
