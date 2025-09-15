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
			name: "WriteBlock",
			fn: func() error {
				return db.WriteBlock(height, blockData)
			},
		},
		{
			name: "ReadBlock",
			fn: func() error {
				_, err := db.ReadBlock(height)
				return err
			},
		},
		{
			name: "HasBlock",
			fn: func() error {
				_, err := db.HasBlock(height)
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

func TestWriteAndReadBlock(t *testing.T) {
	db := &Database{}

	height := blockdb.BlockHeight(1)
	blockData := blockdb.BlockData("test block data")

	// Write block
	require.NoError(t, db.WriteBlock(height, blockData))

	// Read block back
	retrievedBlock, err := db.ReadBlock(height)
	require.NoError(t, err)
	require.Equal(t, blockData, retrievedBlock)
}

func TestHasBlockForExistingBlock(t *testing.T) {
	db := &Database{}

	height := blockdb.BlockHeight(1)
	blockData := blockdb.BlockData("test block data")

	// Write block
	require.NoError(t, db.WriteBlock(height, blockData))

	// Verify HasBlock returns true
	exists, err := db.HasBlock(height)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestHasBlockForNonExistentBlock(t *testing.T) {
	db := &Database{}

	// Verify non-existent block returns false
	nonExistentHeight := blockdb.BlockHeight(999)
	exists, err := db.HasBlock(nonExistentHeight)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestOverwritingBlockUpdatesData(t *testing.T) {
	db := &Database{}

	height := blockdb.BlockHeight(1)
	originalData := blockdb.BlockData("original data")
	updatedData := blockdb.BlockData("updated data")

	// Write original block
	require.NoError(t, db.WriteBlock(height, originalData))

	// Overwrite with new data
	require.NoError(t, db.WriteBlock(height, updatedData))

	// Verify updated data
	retrievedBlock, err := db.ReadBlock(height)
	require.NoError(t, err)
	require.Equal(t, updatedData, retrievedBlock)
}
