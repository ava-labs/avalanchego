// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/DataDog/zstd"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/compression"
)

func getCompressedBlockSize(block []byte) (uint32, error) {
	// Use the same compressor configuration as the database
	compressor, err := compression.NewZstdCompressorWithLevel(math.MaxUint32, zstd.BestSpeed)
	if err != nil {
		return 0, fmt.Errorf("failed to create compressor: %w", err)
	}

	compressed, err := compressor.Compress(block)
	if err != nil {
		return 0, fmt.Errorf("failed to compress block: %w", err)
	}

	return uint32(len(compressed)), nil
}

func TestRecovery_Success(t *testing.T) {
	// Create database with 10KB file size and 4KB blocks
	// This means each file will have 2 blocks (4KB + 26 bytes header = ~4KB per block)
	config := DefaultConfig().WithMaxDataFileSize(10 * 1024) // 10KB per file

	tests := []struct {
		name         string
		corruptIndex func(indexPath string, blocks map[uint64][]byte) error
	}{
		{
			name: "recovery from missing index file; blocks will be recovered",
			corruptIndex: func(indexPath string, _ map[uint64][]byte) error {
				return os.Remove(indexPath)
			},
		},
		{
			name: "recovery from truncated index file that only indexed the first block",
			corruptIndex: func(indexPath string, blocks map[uint64][]byte) error {
				// Remove the existing index file
				if err := os.Remove(indexPath); err != nil {
					return err
				}

				// Create a new index file with only the first block indexed
				// This simulates an unclean shutdown where the index file is behind
				indexFile, err := os.OpenFile(indexPath, os.O_RDWR|os.O_CREATE, defaultFilePermissions)
				if err != nil {
					return err
				}
				defer indexFile.Close()

				// Create a header that only knows about the first block
				// Block 0: compressed data + header
				firstBlockCompressedSize, err := getCompressedBlockSize(blocks[0])
				if err != nil {
					return err
				}
				firstBlockOffset := uint64(sizeOfBlockEntryHeader) + uint64(firstBlockCompressedSize)

				header := indexFileHeader{
					Version:             IndexFileVersion,
					MaxDataFileSize:     4 * 10 * 1024, // 10KB per file
					MinHeight:           0,
					MaxContiguousHeight: 0,
					MaxHeight:           0,
					NextWriteOffset:     firstBlockOffset,
				}

				// Write the header
				headerBytes, err := header.MarshalBinary()
				if err != nil {
					return err
				}
				if _, err := indexFile.WriteAt(headerBytes, 0); err != nil {
					return err
				}

				// Write index entry for only the first block
				indexEntry := indexEntry{
					Offset: 0,
					Size:   firstBlockCompressedSize,
				}
				entryBytes, err := indexEntry.MarshalBinary()
				if err != nil {
					return err
				}
				indexEntryOffset := sizeOfIndexFileHeader
				if _, err := indexFile.WriteAt(entryBytes, int64(indexEntryOffset)); err != nil {
					return err
				}

				return nil
			},
		},
		{
			name: "recovery from index file that is behind by one block",
			corruptIndex: func(indexPath string, blocks map[uint64][]byte) error {
				// Read the current index file to get the header
				indexFile, err := os.OpenFile(indexPath, os.O_RDWR, 0)
				if err != nil {
					return err
				}
				defer indexFile.Close()

				// Read the current header
				headerBuf := make([]byte, sizeOfIndexFileHeader)
				_, err = indexFile.ReadAt(headerBuf, 0)
				if err != nil {
					return err
				}

				// Parse the header
				var header indexFileHeader
				err = header.UnmarshalBinary(headerBuf)
				if err != nil {
					return err
				}

				// Corrupt the header by setting the NextWriteOffset to be one block behind
				lastBlockCompressedSize, err := getCompressedBlockSize(blocks[8])
				if err != nil {
					return err
				}
				blockSize := uint64(sizeOfBlockEntryHeader) + uint64(lastBlockCompressedSize)
				header.NextWriteOffset -= blockSize
				header.MaxContiguousHeight = 3
				header.MaxHeight = 8

				// Write the corrupted header back
				corruptedHeaderBytes, err := header.MarshalBinary()
				if err != nil {
					return err
				}
				_, err = indexFile.WriteAt(corruptedHeaderBytes, 0)
				return err
			},
		},
		{
			name: "recovery from inconsistent index header (offset lagging behind heights)",
			corruptIndex: func(indexPath string, blocks map[uint64][]byte) error {
				// Read the current index file to get the header
				indexFile, err := os.OpenFile(indexPath, os.O_RDWR, 0)
				if err != nil {
					return err
				}
				defer indexFile.Close()

				// Read the current header
				headerBuf := make([]byte, sizeOfIndexFileHeader)
				_, err = indexFile.ReadAt(headerBuf, 0)
				if err != nil {
					return err
				}

				// Parse the header
				var header indexFileHeader
				err = header.UnmarshalBinary(headerBuf)
				if err != nil {
					return err
				}

				// Calculate the offset after the 5th block using actual compressed sizes
				// We need to sum up the sizes of blocks 0, 1, 2, 3, 4 (5 blocks total)
				// Each block size = header + compressed data
				blocksToCompress := [][]byte{blocks[0], blocks[1], blocks[2], blocks[3], blocks[4]}
				totalCompressedSize := uint32(0)
				for _, block := range blocksToCompress {
					compressedSize, err := getCompressedBlockSize(block)
					if err != nil {
						return err
					}
					totalCompressedSize += compressedSize
				}
				totalSize := uint64(len(blocksToCompress))*uint64(sizeOfBlockEntryHeader) + uint64(totalCompressedSize)
				header.NextWriteOffset = totalSize

				// Write the corrupted header back
				corruptedHeaderBytes, err := header.MarshalBinary()
				if err != nil {
					return err
				}
				_, err = indexFile.WriteAt(corruptedHeaderBytes, 0)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, _ := newTestDatabase(t, config)

			blockHeights := []uint64{0, 1, 3, 6, 2, 8, 4}
			blocks := make(map[uint64][]byte)

			for _, height := range blockHeights {
				// Create 4KB blocks
				block := fixedSizeBlock(t, 4*1024, height)

				require.NoError(t, store.WriteBlock(height, block))
				blocks[height] = block
			}
			checkDatabaseState(t, store, 8, 4)
			require.NoError(t, store.Close())

			// Corrupt the index file according to the test case
			indexPath := store.indexFile.Name()
			require.NoError(t, tt.corruptIndex(indexPath, blocks))

			// Reopen the database and test recovery
			recoveredStore, err := New(config.WithIndexDir(store.config.IndexDir).WithDataDir(store.config.DataDir), store.log)
			require.NoError(t, err)
			defer recoveredStore.Close()

			// Verify blocks are readable
			for _, height := range blockHeights {
				readBlock, err := recoveredStore.ReadBlock(height)
				require.NoError(t, err)
				require.Equal(t, blocks[height], readBlock, "block %d should be the same", height)
			}
			checkDatabaseState(t, recoveredStore, 8, 4)
		})
	}
}

func TestRecovery_CorruptionDetection(t *testing.T) {
	tests := []struct {
		name               string
		blockHeights       []uint64
		minHeight          uint64
		maxDataFileSize    *uint64
		disableCompression bool
		blockSize          int // Optional: if set, creates fixed-size blocks instead of random
		setupCorruption    func(store *Database, blocks [][]byte) error
		wantErr            error
		wantErrText        string
	}{
		{
			name:         "index header claims larger offset than actual data",
			blockHeights: []uint64{0, 1, 2, 3, 4},
			setupCorruption: func(store *Database, _ [][]byte) error {
				indexPath := store.indexFile.Name()
				indexFile, err := os.OpenFile(indexPath, os.O_RDWR, 0)
				if err != nil {
					return err
				}
				defer indexFile.Close()

				// Read the current header
				headerBuf := make([]byte, sizeOfIndexFileHeader)
				_, err = indexFile.ReadAt(headerBuf, 0)
				if err != nil {
					return err
				}

				// Parse and corrupt the header by setting NextWriteOffset to be much larger than actual data
				var header indexFileHeader
				err = header.UnmarshalBinary(headerBuf)
				if err != nil {
					return err
				}
				header.NextWriteOffset = 1000000

				// Write the corrupted header back
				corruptedHeaderBytes, err := header.MarshalBinary()
				if err != nil {
					return err
				}
				_, err = indexFile.WriteAt(corruptedHeaderBytes, 0)
				return err
			},
			wantErr:     ErrCorrupted,
			wantErrText: "index header claims to have more data than is actually on disk",
		},
		{
			name:         "corrupted block header in data file",
			blockHeights: []uint64{0, 1, 3},
			setupCorruption: func(store *Database, blocks [][]byte) error {
				compressedSize, err := getCompressedBlockSize(blocks[0])
				if err != nil {
					return err
				}
				if err := resetIndexToBlock(store, uint64(compressedSize), 0); err != nil {
					return err
				}
				// Corrupt second block header with invalid data
				secondBlockOffset := int64(sizeOfBlockEntryHeader) + int64(compressedSize)
				corruptedHeader := make([]byte, sizeOfBlockEntryHeader)
				for i := range corruptedHeader {
					corruptedHeader[i] = 0xFF // Invalid header data
				}
				dataFilePath := store.dataFilePath(0)
				dataFile, err := os.OpenFile(dataFilePath, os.O_RDWR, 0)
				if err != nil {
					return err
				}
				defer dataFile.Close()
				_, err = dataFile.WriteAt(corruptedHeader, secondBlockOffset)
				return err
			},
			wantErr:     ErrCorrupted,
			wantErrText: "invalid block entry version at offset",
		},
		{
			name:         "block with invalid block size in header that reads more than total data file size",
			blockHeights: []uint64{0, 1},
			setupCorruption: func(store *Database, blocks [][]byte) error {
				compressedSize0, err := getCompressedBlockSize(blocks[0])
				if err != nil {
					return err
				}
				if err := resetIndexToBlock(store, uint64(compressedSize0), 0); err != nil {
					return err
				}
				secondBlockOffset := int64(sizeOfBlockEntryHeader) + int64(compressedSize0)
				compressedSize1, err := getCompressedBlockSize(blocks[1])
				if err != nil {
					return err
				}
				bh := blockEntryHeader{
					Height:   1,
					Checksum: calculateChecksum(blocks[1]),
					Size:     compressedSize1 + 1, // make block larger than actual compressed size
					Version:  BlockEntryVersion,
				}
				return writeBlockHeader(store, secondBlockOffset, bh)
			},
			wantErr:     ErrCorrupted,
			wantErrText: "block data out of bounds at offset ",
		},
		{
			name:         "block with checksum mismatch",
			blockHeights: []uint64{0, 1},
			setupCorruption: func(store *Database, blocks [][]byte) error {
				compressedSize0, err := getCompressedBlockSize(blocks[0])
				if err != nil {
					return err
				}
				if err := resetIndexToBlock(store, uint64(compressedSize0), 0); err != nil {
					return err
				}
				secondBlockOffset := int64(sizeOfBlockEntryHeader) + int64(compressedSize0)
				compressedSize1, err := getCompressedBlockSize(blocks[1])
				if err != nil {
					return err
				}
				bh := blockEntryHeader{
					Height:   1,
					Checksum: 0xDEADBEEF, // Wrong checksum
					Size:     compressedSize1,
					Version:  BlockEntryVersion,
				}
				return writeBlockHeader(store, secondBlockOffset, bh)
			},
			wantErr:     ErrCorrupted,
			wantErrText: "checksum mismatch for block",
		},
		{
			name:         "partial block at end of file",
			blockHeights: []uint64{0},
			setupCorruption: func(store *Database, blocks [][]byte) error {
				dataFilePath := store.dataFilePath(0)
				dataFile, err := os.OpenFile(dataFilePath, os.O_RDWR, 0)
				if err != nil {
					return err
				}
				defer dataFile.Close()

				// Truncate data file to have only partial block data
				compressedSize, err := getCompressedBlockSize(blocks[0])
				if err != nil {
					return err
				}
				truncateSize := int64(sizeOfBlockEntryHeader) + int64(compressedSize)/2
				return dataFile.Truncate(truncateSize)
			},
			wantErr:     ErrCorrupted,
			wantErrText: "index header claims to have more data than is actually on disk",
		},
		{
			name:         "block with invalid height",
			blockHeights: []uint64{10, 11},
			minHeight:    10,
			setupCorruption: func(store *Database, blocks [][]byte) error {
				compressedSize0, err := getCompressedBlockSize(blocks[0])
				if err != nil {
					return err
				}
				if err := resetIndexToBlock(store, uint64(compressedSize0), 10); err != nil {
					return err
				}
				secondBlockOffset := int64(sizeOfBlockEntryHeader) + int64(compressedSize0)
				compressedSize1, err := getCompressedBlockSize(blocks[1])
				if err != nil {
					return err
				}
				bh := blockEntryHeader{
					Height:   5, // Invalid height because its below the minimum height of 10
					Checksum: calculateChecksum(blocks[1]),
					Size:     compressedSize1,
					Version:  BlockEntryVersion,
				}
				return writeBlockHeader(store, secondBlockOffset, bh)
			},
			wantErr:     ErrCorrupted,
			wantErrText: "invalid block height in header",
		},
		{
			name:               "missing data file at index 1",
			blockHeights:       []uint64{0, 1, 2, 3, 4, 5},
			disableCompression: true,
			maxDataFileSize:    uint64Ptr(1024), // 1KB per file to force multiple files
			blockSize:          512,             // 512 bytes per block
			setupCorruption: func(store *Database, _ [][]byte) error {
				// Delete the second data file (index 1)
				dataFilePath := store.dataFilePath(1)
				return os.Remove(dataFilePath)
			},
			wantErr:     ErrCorrupted,
			wantErrText: "data file at index 1 is missing",
		},
		{
			name:            "unexpected multiple data files when MaxDataFileSize is max uint64",
			blockHeights:    []uint64{0, 1, 2},
			maxDataFileSize: uint64Ptr(math.MaxUint64), // Single file mode
			blockSize:       512,                       // 512 bytes per block
			setupCorruption: func(store *Database, _ [][]byte) error {
				// Manually create a second data file to simulate corruption
				secondDataFilePath := store.dataFilePath(1)
				secondDataFile, err := os.Create(secondDataFilePath)
				if err != nil {
					return err
				}
				defer secondDataFile.Close()

				// Write some dummy data to the second file
				dummyData := []byte("dummy data file")
				_, err = secondDataFile.Write(dummyData)
				return err
			},
			wantErr:     ErrCorrupted,
			wantErrText: "only one data file expected when MaxDataFileSize is max uint64, got 2 files with max index 1",
		},
		{
			name:         "block with invalid block entry version",
			blockHeights: []uint64{0, 1},
			setupCorruption: func(store *Database, blocks [][]byte) error {
				compressedSize0, err := getCompressedBlockSize(blocks[0])
				if err != nil {
					return err
				}
				if err := resetIndexToBlock(store, uint64(compressedSize0), 0); err != nil {
					return err
				}
				// Corrupt second block header version
				secondBlockOffset := int64(sizeOfBlockEntryHeader) + int64(compressedSize0)
				compressedSize1, err := getCompressedBlockSize(blocks[1])
				if err != nil {
					return err
				}
				bh := blockEntryHeader{
					Height:   1,
					Checksum: calculateChecksum(blocks[1]),
					Size:     compressedSize1,
					Version:  BlockEntryVersion + 1, // Invalid version
				}
				return writeBlockHeader(store, secondBlockOffset, bh)
			},
			wantErr:     ErrCorrupted,
			wantErrText: "invalid block entry version at offset",
		},
		{
			name:         "second block with invalid version among 4 blocks",
			blockHeights: []uint64{0, 3, 2, 4},
			setupCorruption: func(store *Database, blocks [][]byte) error {
				compressedSize0, err := getCompressedBlockSize(blocks[0])
				if err != nil {
					return err
				}
				if err := resetIndexToBlock(store, uint64(compressedSize0), 0); err != nil {
					return err
				}
				// Corrupt second block header with invalid version
				secondBlockOffset := int64(sizeOfBlockEntryHeader) + int64(compressedSize0)
				compressedSize1, err := getCompressedBlockSize(blocks[1])
				if err != nil {
					return err
				}
				bh := blockEntryHeader{
					Height:   1,
					Checksum: calculateChecksum(blocks[1]),
					Size:     compressedSize1,
					Version:  BlockEntryVersion + 10, // version cannot be greater than current
				}
				return writeBlockHeader(store, secondBlockOffset, bh)
			},
			wantErr:     ErrCorrupted,
			wantErrText: "invalid block entry version at offset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			if tt.minHeight > 0 {
				config = config.WithMinimumHeight(tt.minHeight)
			}
			if tt.maxDataFileSize != nil {
				config = config.WithMaxDataFileSize(*tt.maxDataFileSize)
			}

			store, cleanup := newTestDatabase(t, config)
			if tt.disableCompression {
				store.compressor = compression.NewNoCompressor()
			}
			defer cleanup()

			// Setup blocks
			blocks := make([][]byte, len(tt.blockHeights))
			for i, height := range tt.blockHeights {
				if tt.blockSize > 0 {
					blocks[i] = fixedSizeBlock(t, tt.blockSize, height)
				} else {
					blocks[i] = randomBlock(t)
				}
				require.NoError(t, store.WriteBlock(height, blocks[i]))
			}
			require.NoError(t, store.Close())

			// Apply corruption logic
			require.NoError(t, tt.setupCorruption(store, blocks))

			// Try to reopen the database - it should detect corruption
			_, err := New(config.WithIndexDir(store.config.IndexDir).WithDataDir(store.config.DataDir), store.log)
			require.ErrorIs(t, err, tt.wantErr)
			require.Contains(t, err.Error(), tt.wantErrText, "error message should contain expected text")
		})
	}
}

// Helper function to reset index file header to only a single block
func resetIndexToBlock(store *Database, blockSize uint64, minHeight uint64) error {
	indexPath := store.indexFile.Name()
	indexFile, err := os.OpenFile(indexPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer indexFile.Close()

	header := indexFileHeader{
		Version:             IndexFileVersion,
		MaxDataFileSize:     DefaultMaxDataFileSize,
		MinHeight:           minHeight,
		MaxContiguousHeight: minHeight,
		MaxHeight:           minHeight,
		NextWriteOffset:     uint64(sizeOfBlockEntryHeader) + blockSize,
	}

	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = indexFile.WriteAt(headerBytes, 0)
	return err
}

// Helper function to write a block header at a specific offset
func writeBlockHeader(store *Database, offset int64, bh blockEntryHeader) error {
	fileIndex := int(offset / int64(store.header.MaxDataFileSize))
	localOffset := offset % int64(store.header.MaxDataFileSize)
	dataFilePath := store.dataFilePath(fileIndex)
	dataFile, err := os.OpenFile(dataFilePath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer dataFile.Close()

	headerBytes, err := bh.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = dataFile.WriteAt(headerBytes, localOffset)
	return err
}
