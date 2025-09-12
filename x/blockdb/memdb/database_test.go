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

	// All operations should fail after close
	err := db.WriteBlock(height, blockData)
	require.ErrorIs(t, err, blockdb.ErrDatabaseClosed)

	_, err = db.ReadBlock(height)
	require.ErrorIs(t, err, blockdb.ErrDatabaseClosed)

	_, err = db.HasBlock(height)
	require.ErrorIs(t, err, blockdb.ErrDatabaseClosed)
}

func TestWriteReadAndHasBlock(t *testing.T) {
	db := &Database{}

	height := blockdb.BlockHeight(1)
	blockData := blockdb.BlockData("test block data")

	// Write block
	require.NoError(t, db.WriteBlock(height, blockData))

	// Verify HasBlock returns true
	exists, err := db.HasBlock(height)
	require.NoError(t, err)
	require.True(t, exists)

	// Read block back
	retrievedBlock, err := db.ReadBlock(height)
	require.NoError(t, err)
	require.Equal(t, blockData, retrievedBlock)

	// Verify non-existent block
	nonExistentHeight := blockdb.BlockHeight(999)
	exists, err = db.HasBlock(nonExistentHeight)
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
