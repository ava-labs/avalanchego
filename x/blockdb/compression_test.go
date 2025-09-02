// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// checkBlockCompression is a helper function to check if a block is compressed
func checkBlockCompression(t *testing.T, db *Database, height uint64) bool {
	indexEntry, err := db.readBlockIndex(height)
	require.NoError(t, err)

	// Read the raw data from the data file
	dataFile, localOffset, _, err := db.getDataFileAndOffset(indexEntry.Offset)
	require.NoError(t, err)
	blockHeaderBuf := make([]byte, sizeOfBlockEntryHeader)
	_, err = dataFile.ReadAt(blockHeaderBuf, int64(localOffset))
	require.NoError(t, err)
	var blockHeader blockEntryHeader
	require.NoError(t, blockHeader.UnmarshalBinary(blockHeaderBuf))

	return blockHeader.Compressed
}

func TestCompression_Enabled(t *testing.T) {
	config := DefaultConfig().WithCompression(true)
	db, closeDB := newTestDatabase(t, config)
	defer closeDB()

	// Write a block with repetitive content that should compress well
	blockData := []byte("test block data with some repetitive content that should compress well " +
		"test block data with some repetitive content that should compress well " +
		"test block data with some repetitive content that should compress well")
	height := uint64(1)

	require.NoError(t, db.WriteBlock(height, blockData))

	// Read the block back
	readBlock, err := db.ReadBlock(height)
	require.NoError(t, err)
	require.Equal(t, blockData, readBlock)

	// Check compression by reading the raw compressed data from file
	indexEntry, err := db.readBlockIndex(height)
	require.NoError(t, err)

	// Read the raw compressed data from the data file
	dataFile, localOffset, _, err := db.getDataFileAndOffset(indexEntry.Offset)
	require.NoError(t, err)
	blockHeaderBuf := make([]byte, sizeOfBlockEntryHeader)
	_, err = dataFile.ReadAt(blockHeaderBuf, int64(localOffset))
	require.NoError(t, err)
	var blockHeader blockEntryHeader
	require.NoError(t, blockHeader.UnmarshalBinary(blockHeaderBuf))
	compressedData := make([]byte, blockHeader.Size)
	_, err = dataFile.ReadAt(compressedData, int64(localOffset+uint64(sizeOfBlockEntryHeader)))
	require.NoError(t, err)

	// Verify that compression occurred
	require.Less(t, len(compressedData), len(blockData), "Compressed data should be smaller than original")
}

// TestCompression_Recovery tests the following scenario:
// 1. Open database with compression enabled
// 2. Write 5 blocks
// 3. Open database with compression disabled
// 4. Write 5 blocks
// 5. Delete index file
// 6. Open database with compression enabled (trigger recovery)
// 7. Verify all blocks are readable
// 8. Delete index file
// 9. Open database with compression disabled  (trigger recovery)
// 10. Verify all blocks are readable
func TestCompression_Recovery(t *testing.T) {
	dir := t.TempDir()
	allBlocks := make([][]byte, 10)
	for i := range 10 {
		allBlocks[i] = randomBlock(t)
	}

	// Step 1: Open database with compression enabled
	config := DefaultConfig().WithDir(dir).WithCompression(true)
	db, closeDB := newTestDatabase(t, config)

	// Step 2: Write first 5 blocks with compression enabled
	for i := range 5 {
		height := uint64(i + 1)

		require.NoError(t, db.WriteBlock(height, allBlocks[i]))

		// Verify compression was used
		require.True(t, checkBlockCompression(t, db, height), "Block %d should be compressed", height)
	}
	closeDB()

	// Step 3: Open database with compression disabled
	config = config.WithCompression(false)
	db, closeDB = newTestDatabase(t, config)

	// Step 4: Write next 5 blocks with compression disabled
	for i := range 5 {
		height := uint64(i + 6) // Heights 6-10

		require.NoError(t, db.WriteBlock(height, allBlocks[i+5]))

		// Verify compression was not used
		require.False(t, checkBlockCompression(t, db, height), "Block %d should not be compressed", height)
	}

	// Verify all 10 blocks are readable before deleting index
	for i := range 10 {
		height := uint64(i + 1)
		readBlock, err := db.ReadBlock(height)
		require.NoError(t, err)
		require.Equal(t, allBlocks[i], readBlock, "Block %d should be readable", height)
	}
	closeDB()

	// Step 5: Delete index file
	indexPath := filepath.Join(dir, indexFileName)
	require.NoError(t, os.Remove(indexPath), "Failed to delete index file")

	// Step 6: Open database with compression enabled (this should trigger recovery)
	db, closeDB = newTestDatabase(t, config)

	// Step 7: Verify all blocks are readable after recovery
	for i := range 10 {
		height := uint64(i + 1)
		readBlock, err := db.ReadBlock(height)
		require.NoError(t, err)
		require.Equal(t, allBlocks[i], readBlock, "Block %d should be readable after recovery", height)
	}
	closeDB()

	// Step 8: Delete index file
	indexPath = filepath.Join(dir, indexFileName)
	require.NoError(t, os.Remove(indexPath), "Failed to delete index file")

	// Step 9: Test recovery again but with compression disabled
	// This tests compression config does not matter for recovery
	db, closeDB = newTestDatabase(t, config.WithCompression(false))
	defer closeDB()

	// Step 10: Verify all blocks are readable after second recovery
	for i := range 10 {
		height := uint64(i + 1)
		readBlock, err := db.ReadBlock(height)
		require.NoError(t, err)
		require.Equal(t, allBlocks[i], readBlock, "Block %d should be readable after recovery", height)
	}
}
