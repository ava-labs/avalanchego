// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestCompression_Basic(t *testing.T) {
	// Test with compression disabled (default)
	config := DefaultConfig().WithCompressBlocks(false)
	db, cleanup := newTestDatabase(t, config)
	defer cleanup()

	// Write a block
	blockData := []byte("test block data with some repetitive content that should compress well")
	headerSize := uint32(10) // First 10 bytes are header
	height := uint64(1)

	err := db.WriteBlock(height, blockData, headerSize)
	require.NoError(t, err)

	// Read the block back
	readBlock, err := db.ReadBlock(height)
	require.NoError(t, err)
	require.Equal(t, blockData, readBlock)

	// Read header and body
	header, err := db.ReadHeader(height)
	require.NoError(t, err)
	require.Equal(t, blockData[:headerSize], header)

	body, err := db.ReadBody(height)
	require.NoError(t, err)
	require.Equal(t, blockData[headerSize:], body)
}

func TestCompression_Enabled(t *testing.T) {
	// Test with compression enabled
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

	// Read header and body
	header, err := db.ReadHeader(height)
	require.NoError(t, err)
	require.Equal(t, blockData[:headerSize], header)

	body, err := db.ReadBody(height)
	require.NoError(t, err)
	require.Equal(t, blockData[headerSize:], body)

	// Check compression ratio by reading the raw compressed data from file
	indexEntry, err := db.readBlockIndex(height)
	require.NoError(t, err)

	// Read the raw compressed data from the data file
	dataFile, localOffset, err := db.getDataFileAndOffset(indexEntry.Offset)
	require.NoError(t, err)

	// Read the block entry header to get the compressed size
	blockHeaderBuf := make([]byte, sizeOfBlockEntryHeader)
	_, err = dataFile.ReadAt(blockHeaderBuf, int64(localOffset))
	require.NoError(t, err)

	var blockHeader blockEntryHeader
	err = blockHeader.UnmarshalBinary(blockHeaderBuf)
	require.NoError(t, err)

	// Read the compressed data
	compressedData := make([]byte, blockHeader.Size)
	_, err = dataFile.ReadAt(compressedData, int64(localOffset+uint64(sizeOfBlockEntryHeader)))
	require.NoError(t, err)

	// Calculate compression ratio
	originalSize := len(blockData)
	compressedSize := len(compressedData)
	compressionRatio := float64(compressedSize) / float64(originalSize)
	spaceSaved := originalSize - compressedSize

	t.Logf("Compression stats:")
	t.Logf("  Original size: %d bytes", originalSize)
	t.Logf("  Compressed size: %d bytes", compressedSize)
	t.Logf("  Compression ratio: %.2f%%", compressionRatio*100)
	t.Logf("  Space saved: %d bytes (%.1f%%)", spaceSaved, (1-compressionRatio)*100)

	// Verify that compression actually occurred (compressed size should be smaller)
	require.Less(t, compressedSize, originalSize, "Compressed data should be smaller than original")
	require.Less(t, compressionRatio, 1.0, "Compression ratio should be less than 100%")
}

func TestCompression_MultipleBlocks(t *testing.T) {
	// Test multiple blocks with compression
	config := DefaultConfig().WithCompressBlocks(true)
	db, cleanup := newTestDatabase(t, config)
	defer cleanup()

	// Write multiple blocks
	blocks := make([][]byte, 5)
	for i := 0; i < 5; i++ {
		blocks[i] = []byte("block data with repetitive content " + string(rune('A'+i)))
		headerSize := uint32(5)
		height := uint64(i + 1)

		err := db.WriteBlock(height, blocks[i], headerSize)
		require.NoError(t, err)
	}

	// Read all blocks back
	for i := 0; i < 5; i++ {
		height := uint64(i + 1)
		readBlock, err := db.ReadBlock(height)
		require.NoError(t, err)
		require.Equal(t, blocks[i], readBlock)
	}
}

func TestCompression_Reopen(t *testing.T) {
	// Test that compression works correctly when reopening the database
	tempDir := t.TempDir()
	config := DefaultConfig().WithDir(tempDir).WithCompressBlocks(true)

	// Create first database
	db, err := New(config, logging.NoLog{})
	require.NoError(t, err)

	// Write a block
	blockData := []byte("test block data with repetitive content")
	headerSize := uint32(8)
	height := uint64(1)

	err = db.WriteBlock(height, blockData, headerSize)
	require.NoError(t, err)

	// Close first database
	err = db.Close()
	require.NoError(t, err)

	// Create second database with same config
	db2, err := New(config, logging.NoLog{})
	require.NoError(t, err)
	defer db2.Close()

	// Read the block back
	readBlock, err := db2.ReadBlock(height)
	require.NoError(t, err)
	require.Equal(t, blockData, readBlock)
}

func TestCompression_SizeComparison(t *testing.T) {
	// Test to compare compressed vs uncompressed file sizes
	blockData := []byte("test block data with some repetitive content that should compress well " +
		"test block data with some repetitive content that should compress well " +
		"test block data with some repetitive content that should compress well")
	headerSize := uint32(10)
	height := uint64(1)

	// Test with compression disabled
	configNoCompress := DefaultConfig().WithCompressBlocks(false)
	dbNoCompress, cleanup1 := newTestDatabase(t, configNoCompress)
	err := dbNoCompress.WriteBlock(height, blockData, headerSize)
	require.NoError(t, err)

	// Get uncompressed size
	indexEntryNoCompress, err := dbNoCompress.readBlockIndex(height)
	require.NoError(t, err)
	uncompressedSize := int(indexEntryNoCompress.Size)
	cleanup1()

	// Test with compression enabled
	configCompress := DefaultConfig().WithCompressBlocks(true)
	dbCompress, cleanup2 := newTestDatabase(t, configCompress)
	err = dbCompress.WriteBlock(height, blockData, headerSize)
	require.NoError(t, err)

	// Get compressed size
	indexEntryCompress, err := dbCompress.readBlockIndex(height)
	require.NoError(t, err)
	compressedSize := int(indexEntryCompress.Size)
	cleanup2()

	// Calculate savings
	spaceSaved := uncompressedSize - compressedSize
	compressionRatio := float64(compressedSize) / float64(uncompressedSize)

	t.Logf("Size comparison:")
	t.Logf("  Uncompressed size: %d bytes", uncompressedSize)
	t.Logf("  Compressed size: %d bytes", compressedSize)
	t.Logf("  Space saved: %d bytes", spaceSaved)
	t.Logf("  Compression ratio: %.2f%%", compressionRatio*100)
	t.Logf("  Space reduction: %.1f%%", (1-compressionRatio)*100)

	// Verify compression is effective
	require.Less(t, compressedSize, uncompressedSize, "Compressed size should be smaller than uncompressed")
	require.Less(t, compressionRatio, 1.0, "Compression ratio should be less than 100%")
	require.Greater(t, spaceSaved, 0, "Should save some space with compression")
}
