// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecovery_Success(t *testing.T) {
	// Create database with 10KB file size and 4KB blocks
	// This means each file will have 2 blocks (4KB + 24 bytes header = ~4KB per block)
	config := DefaultConfig().WithMaxDataFileSize(10 * 1024) // 10KB per file

	tests := []struct {
		name         string
		corruptIndex func(indexPath string) error
	}{
		{
			name:         "recovery from missing index file; blocks will be recovered",
			corruptIndex: os.Remove,
		},
		{
			name: "recovery from truncated index file that only indexed the first block",
			corruptIndex: func(indexPath string) error {
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
				// Block 0: 4KB data + header
				firstBlockOffset := uint64(sizeOfBlockEntryHeader) + 4*1024

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
					Offset:     0,
					Size:       4 * 1024, // 4KB
					HeaderSize: 0,
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
			corruptIndex: func(indexPath string) error {
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
				blockSize := uint64(sizeOfBlockEntryHeader) + 4*1024
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
			corruptIndex: func(indexPath string) error {
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

				// Calculate the offset after the 5th block (assuming 4KB blocks)
				// 2 files, 10KB each, 4KB block size
				blockSize := uint64(sizeOfBlockEntryHeader) + 4*1024
				header.NextWriteOffset = 10*1024*2 + blockSize

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

				require.NoError(t, store.WriteBlock(height, block, 0))
				blocks[height] = block
			}
			checkDatabaseState(t, store, 8, 4)
			require.NoError(t, store.Close())

			// Corrupt the index file according to the test case
			indexPath := store.indexFile.Name()
			require.NoError(t, tt.corruptIndex(indexPath))

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
		name            string
		blockHeights    []uint64
		minHeight       uint64
		maxDataFileSize *uint64
		blockSize       int // Optional: if set, creates fixed-size blocks instead of random
		setupCorruption func(store *Database, blocks [][]byte) error
		wantErr         error
		wantErrText     string
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
				if err := resetIndexToBlock(store, uint64(len(blocks[0])), 0); err != nil {
					return err
				}
				// Corrupt second block header with invalid data
				secondBlockOffset := int64(sizeOfBlockEntryHeader) + int64(len(blocks[0]))
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
				if err := resetIndexToBlock(store, uint64(len(blocks[0])), 0); err != nil {
					return err
				}
				secondBlockOffset := int64(sizeOfBlockEntryHeader) + int64(len(blocks[0]))
				bh := blockEntryHeader{
					Height:     1,
					Checksum:   calculateChecksum(blocks[1]),
					Size:       uint32(len(blocks[1])) + 1, // make block larger than actual
					HeaderSize: 0,
					Version:    BlockEntryVersion,
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
				if err := resetIndexToBlock(store, uint64(len(blocks[0])), 0); err != nil {
					return err
				}
				secondBlockOffset := int64(sizeOfBlockEntryHeader) + int64(len(blocks[0]))
				bh := blockEntryHeader{
					Height:     1,
					Checksum:   0xDEADBEEF, // Wrong checksum
					Size:       uint32(len(blocks[1])),
					HeaderSize: 0,
					Version:    BlockEntryVersion,
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
				truncateSize := int64(sizeOfBlockEntryHeader) + int64(len(blocks[0]))/2
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
				if err := resetIndexToBlock(store, uint64(len(blocks[0])), 10); err != nil {
					return err
				}
				secondBlockOffset := int64(sizeOfBlockEntryHeader) + int64(len(blocks[0]))
				bh := blockEntryHeader{
					Height:     5, // Invalid height because its below the minimum height of 10
					Checksum:   calculateChecksum(blocks[1]),
					Size:       uint32(len(blocks[1])),
					HeaderSize: 0,
					Version:    BlockEntryVersion,
				}
				return writeBlockHeader(store, secondBlockOffset, bh)
			},
			wantErr:     ErrCorrupted,
			wantErrText: "invalid block height in header",
		},
		{
			name:            "missing data file at index 1",
			blockHeights:    []uint64{0, 1, 2, 3, 4, 5},
			maxDataFileSize: uint64Ptr(1024), // 1KB per file to force multiple files
			blockSize:       512,             // 512 bytes per block
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
				if err := resetIndexToBlock(store, uint64(len(blocks[0])), 0); err != nil {
					return err
				}
				// Corrupt second block header version
				secondBlockOffset := int64(sizeOfBlockEntryHeader) + int64(len(blocks[0]))
				bh := blockEntryHeader{
					Height:     1,
					Checksum:   calculateChecksum(blocks[1]),
					Size:       uint32(len(blocks[1])),
					HeaderSize: 0,
					Version:    BlockEntryVersion + 1, // Invalid version
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
				if err := resetIndexToBlock(store, uint64(len(blocks[0])), 0); err != nil {
					return err
				}
				// Corrupt second block header with invalid version
				secondBlockOffset := int64(sizeOfBlockEntryHeader) + int64(len(blocks[0]))
				bh := blockEntryHeader{
					Height:     1,
					Checksum:   calculateChecksum(blocks[1]),
					Size:       uint32(len(blocks[1])),
					HeaderSize: 0,
					Version:    BlockEntryVersion + 10, // version cannot be greater than current
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
			defer cleanup()

			// Setup blocks
			blocks := make([][]byte, len(tt.blockHeights))
			for i, height := range tt.blockHeights {
				if tt.blockSize > 0 {
					blocks[i] = fixedSizeBlock(t, tt.blockSize, height)
				} else {
					blocks[i] = randomBlock(t)
				}
				require.NoError(t, store.WriteBlock(height, blocks[i], 0))
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

// Helper function to reset index to only a single block
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
