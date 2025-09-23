// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memdb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/x/blockdb"
)

func TestOperationsAfterCloseReturnError(t *testing.T) {
	db := &Database{}

	// Close database
	require.NoError(t, db.Close())

	height := blockdb.BlockHeight(1)
	blockData := blockdb.BlockData("test block data")

	tests := []struct {
		name string
		fn   func() error
	}{
		{
			name: "Put",
			fn: func() error {
				return db.Put(height, blockData)
			},
		},
		{
			name: "Get",
			fn: func() error {
				_, err := db.Get(height)
				return err
			},
		},
		{
			name: "Has",
			fn: func() error {
				_, err := db.Has(height)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			require.ErrorIs(t, err, blockdb.ErrDatabaseClosed)
		})
	}
}

func TestPut(t *testing.T) {
	db := &Database{}

	height := blockdb.BlockHeight(1)
	blockData := blockdb.BlockData("test block data")
	require.NoError(t, db.Put(height, blockData))
}

func TestGet(t *testing.T) {
	db := &Database{}

	height := blockdb.BlockHeight(1)
	blockData := blockdb.BlockData("test block data")
	require.NoError(t, db.Put(height, blockData))

	// Read block back
	retrievedBlock, err := db.Get(height)
	require.NoError(t, err)
	require.Equal(t, blockData, retrievedBlock)
}

func TestHas(t *testing.T) {
	t.Run("non-existent block", func(t *testing.T) {
		db := &Database{}
		exists, err := db.Has(blockdb.BlockHeight(1))
		require.NoError(t, err)
		require.False(t, exists)
	})

	t.Run("existing block", func(t *testing.T) {
		db := &Database{}
		blockData := blockdb.BlockData("test block data")
		require.NoError(t, db.Put(blockdb.BlockHeight(1), blockData))
		exists, err := db.Has(blockdb.BlockHeight(1))
		require.NoError(t, err)
		require.True(t, exists)
	})
}

func TestPut_Overwrite(t *testing.T) {
	db := &Database{}

	height := blockdb.BlockHeight(1)
	originalData := blockdb.BlockData("original data")
	updatedData := blockdb.BlockData("updated data")

	// Write original block
	require.NoError(t, db.Put(height, originalData))

	// Overwrite with new data
	require.NoError(t, db.Put(height, updatedData))

	// Verify updated data
	retrievedBlock, err := db.Get(height)
	require.NoError(t, err)
	require.Equal(t, updatedData, retrievedBlock)
}
