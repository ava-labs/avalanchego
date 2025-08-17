// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	dataFile, localOffset, err := db.getDataFileAndOffset(indexEntry.Offset)
	require.NoError(t, err)
	blockHeaderBuf := make([]byte, sizeOfBlockEntryHeader)
	_, err = dataFile.ReadAt(blockHeaderBuf, int64(localOffset))
	require.NoError(t, err)
	var blockHeader blockEntryHeader
	err = blockHeader.UnmarshalBinary(blockHeaderBuf)
	require.NoError(t, err)

	return blockHeader.Compressed
}

func TestCompression_Enabled(t *testing.T) {
	config := DefaultConfig().WithCompressBlocks(true)
	db, cleanup := newTestDatabase(t, config)
	defer cleanup()

	// Write a block with repetitive content that should compress well
	blockData := []byte("test block data with some repetitive content that should compress well " +
		"test block data with some repetitive content that should compress well " +
		"test block data with some repetitive content that should compress well")
	headerSize := uint32(10) // First 10 bytes are header
	height := uint64(1)

	err := db.WriteBlock(height, blockData, headerSize)
	require.NoError(t, err)

	// Read the block back
	readBlock, err := db.ReadBlock(height)
	require.NoError(t, err)
	require.Equal(t, blockData, readBlock)

	// Check compression by reading the raw compressed data from file
	indexEntry, err := db.readBlockIndex(height)
	require.NoError(t, err)

	// Read the raw compressed data from the data file
	dataFile, localOffset, err := db.getDataFileAndOffset(indexEntry.Offset)
	require.NoError(t, err)
	blockHeaderBuf := make([]byte, sizeOfBlockEntryHeader)
	_, err = dataFile.ReadAt(blockHeaderBuf, int64(localOffset))
	require.NoError(t, err)
	var blockHeader blockEntryHeader
	err = blockHeader.UnmarshalBinary(blockHeaderBuf)
	require.NoError(t, err)
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
// 6. Open database
// 7. Verify all blocks are readable
func TestCompression_Recovery(t *testing.T) {
	dir := t.TempDir()
	allBlocks := make([][]byte, 10)
	for i := range 10 {
		allBlocks[i] = randomBlock(t)
	}

	// Step 1: Open database with compression enabled
	config := DefaultConfig().WithDir(dir).WithCompressBlocks(true)
	db, cleanup := newTestDatabase(t, config)

	// Step 2: Write first 5 blocks with compression enabled
	for i := range 5 {
		height := uint64(i + 1)
		headerSize := uint32(10 + i)

		err := db.WriteBlock(height, allBlocks[i], headerSize)
		require.NoError(t, err)

		// Verify compression was used
		require.True(t, checkBlockCompression(t, db, height), "Block %d should be compressed", height)
	}
	cleanup()

	// Step 3: Open database with compression disabled
	config = config.WithCompressBlocks(false)
	db, cleanup = newTestDatabase(t, config)

	// Step 4: Write next 5 blocks with compression disabled
	for i := range 5 {
		height := uint64(i + 6) // Heights 6-10
		headerSize := uint32(15 + i)

		err := db.WriteBlock(height, allBlocks[i+5], headerSize)
		require.NoError(t, err)

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
	cleanup()

	// Step 5: Delete index file
	indexPath := filepath.Join(dir, indexFileName)
	err := os.Remove(indexPath)
	require.NoError(t, err, "Failed to delete index file")

	// Step 6: Open database (this should trigger recovery)
	db, cleanup = newTestDatabase(t, config)
	defer cleanup()

	// Step 7: Verify all blocks are readable after recovery
	for i := range 10 {
		height := uint64(i + 1)
		readBlock, err := db.ReadBlock(height)
		require.NoError(t, err)
		require.Equal(t, allBlocks[i], readBlock, "Block %d should be readable after recovery", height)
	}
}
