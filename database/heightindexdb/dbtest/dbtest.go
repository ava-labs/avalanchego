// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
)

// Tests is a list of all database tests
var Tests = map[string]func(t *testing.T, newDB func() database.HeightIndex){
	"PutGet":              TestPutGet,
	"Has":                 TestHas,
	"CloseAndPut":         TestCloseAndPut,
	"CloseAndGet":         TestCloseAndGet,
	"CloseAndHas":         TestCloseAndHas,
	"ModifyValueAfterPut": TestModifyValueAfterPut,
	"ModifyValueAfterGet": TestModifyValueAfterGet,
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
		expected    []byte
		expectedErr error
	}{
		{
			name: "normal operation",
			puts: []putArgs{
				{1, []byte("test data 1")},
			},
			queryHeight: 1,
			expected:    []byte("test data 1"),
		},
		{
			name: "not found error when getting on non-existing height",
			puts: []putArgs{
				{1, []byte("test data")},
			},
			queryHeight: 2,
			expected:    nil,
			expectedErr: database.ErrNotFound,
		},
		{
			name: "overwriting data on same height",
			puts: []putArgs{
				{1, []byte("original data")},
				{1, []byte("overwritten data")},
			},
			queryHeight: 1,
			expected:    []byte("overwritten data"),
		},
		{
			name: "put and get nil data",
			puts: []putArgs{
				{1, nil},
			},
			queryHeight: 1,
			expected:    nil,
		},
		{
			name: "put and get empty data",
			puts: []putArgs{
				{1, []byte{}},
			},
			queryHeight: 1,
			expected:    nil,
		},
		{
			name: "put and get large data",
			puts: []putArgs{
				{1, make([]byte, 1000)},
			},
			queryHeight: 1,
			expected:    make([]byte, 1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newDB()
			defer db.Close()

			// Perform all puts
			for _, write := range tt.puts {
				require.NoError(t, db.Put(write.height, write.data))
			}

			// Query the specific height
			retrievedData, err := db.Get(tt.queryHeight)
			if tt.expectedErr != nil {
				require.Equal(t, tt.expectedErr, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, retrievedData)
			}
		})
	}
}

func TestHas(t *testing.T, newDB func() database.HeightIndex) {
	tests := []struct {
		name        string
		puts        []putArgs
		queryHeight uint64
		expected    bool
	}{
		{
			name:        "non-existent item",
			puts:        []putArgs{},
			queryHeight: 1,
			expected:    false,
		},
		{
			name:        "existing item with data",
			puts:        []putArgs{{1, []byte("test data")}},
			queryHeight: 1,
			expected:    true,
		},
		{
			name:        "existing item with nil data",
			puts:        []putArgs{{1, nil}},
			queryHeight: 1,
			expected:    true,
		},
		{
			name:        "existing item with empty bytes",
			puts:        []putArgs{{1, []byte{}}},
			queryHeight: 1,
			expected:    true,
		},
		{
			name: "has returns true on overridden height",
			puts: []putArgs{
				{1, []byte("original data")},
				{1, []byte("overridden data")},
			},
			queryHeight: 1,
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newDB()
			defer db.Close()

			// Perform all puts
			for _, write := range tt.puts {
				require.NoError(t, db.Put(write.height, write.data))
			}

			exists, err := db.Has(tt.queryHeight)
			require.NoError(t, err)
			require.Equal(t, tt.expected, exists)
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

func TestModifyValueAfterPut(t *testing.T, newDB func() database.HeightIndex) {
	db := newDB()
	defer db.Close()

	originalData := []byte("original data")
	modifiedData := []byte("modified data")

	// Put original data
	require.NoError(t, db.Put(1, originalData))

	// Modify the original data slice
	copy(originalData, modifiedData)

	// Get the data back - should still be the original data, not the modified slice
	retrievedData, err := db.Get(1)
	require.NoError(t, err)
	require.Equal(t, []byte("original data"), retrievedData)
	require.NotEqual(t, modifiedData, retrievedData)
}

func TestModifyValueAfterGet(t *testing.T, newDB func() database.HeightIndex) {
	db := newDB()
	defer db.Close()

	originalData := []byte("original data")

	// Put original data
	require.NoError(t, db.Put(1, originalData))

	// Get the data
	retrievedData, err := db.Get(1)
	require.NoError(t, err)
	require.Equal(t, originalData, retrievedData)

	// Modify the retrieved data
	copy(retrievedData, []byte("modified data"))

	// Get the data again - should still be the original data
	retrievedData2, err := db.Get(1)
	require.NoError(t, err)
	require.Equal(t, originalData, retrievedData2)
	require.NotEqual(t, retrievedData, retrievedData2)
}
