// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/heightindexdb/dbtest"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestInterface(t *testing.T) {
	for _, test := range dbtest.Tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Test(t, func() database.HeightIndex {
				tempDir := t.TempDir()
				db, err := New(DefaultConfig().WithDir(tempDir), logging.NoLog{})
				require.NoError(t, err)
				return db
			})
		})
	}
}

func TestNew_Params(t *testing.T) {
	tempDir := t.TempDir()
	tests := []struct {
		name        string
		config      DatabaseConfig
		wantErr     error
		expectClose bool
	}{
		{
			name:   "default config",
			config: DefaultConfig().WithDir(tempDir),
		},
		{
			name: "custom config",
			config: DefaultConfig().WithDir(tempDir).
				WithMinimumHeight(100).
				WithMaxDataFileSize(1024 * 1024). // 1MB
				WithMaxDataFiles(50).
				WithCheckpointInterval(512),
		},
		{
			name:    "empty index directory",
			config:  DefaultConfig().WithDataDir(tempDir),
			wantErr: errors.New("IndexDir must be provided"),
		},
		{
			name:    "empty data directory",
			config:  DefaultConfig().WithIndexDir(tempDir),
			wantErr: errors.New("DataDir must be provided"),
		},
		{
			name:   "different index and data directories",
			config: DefaultConfig().WithIndexDir(filepath.Join(tempDir, "index")).WithDataDir(filepath.Join(tempDir, "data")),
		},
		{
			name:    "invalid config - zero checkpoint interval",
			config:  DefaultConfig().WithDir(tempDir).WithCheckpointInterval(0),
			wantErr: errors.New("CheckpointInterval cannot be 0"),
		},
		{
			name:    "invalid config - zero max data files",
			config:  DefaultConfig().WithDir(tempDir).WithMaxDataFiles(0),
			wantErr: errors.New("MaxDataFiles must be positive"),
		},
		{
			name:    "invalid config - negative max data files",
			config:  DefaultConfig().WithDir(tempDir).WithMaxDataFiles(-1),
			wantErr: errors.New("MaxDataFiles must be positive"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hdb, err := New(tt.config, nil)

			if tt.wantErr != nil {
				require.Equal(t, tt.wantErr.Error(), err.Error())
				return
			}

			require.NoError(t, err)
			require.NotNil(t, hdb)
			db, ok := hdb.(*cacheDB)
			require.True(t, ok)
			config := db.db.config

			// Verify the database was created with correct configuration
			require.Equal(t, tt.config.MinimumHeight, config.MinimumHeight)
			require.Equal(t, tt.config.MaxDataFileSize, config.MaxDataFileSize)
			require.Equal(t, tt.config.MaxDataFiles, config.MaxDataFiles)
			require.Equal(t, tt.config.CheckpointInterval, config.CheckpointInterval)
			require.Equal(t, tt.config.SyncToDisk, config.SyncToDisk)
			indexPath := filepath.Join(tt.config.IndexDir, indexFileName)
			require.FileExists(t, indexPath)

			// Test that we can close the database
			require.NoError(t, db.Close())
		})
	}
}

func TestNew_IndexFileErrors(t *testing.T) {
	tests := []struct {
		name       string
		setup      func() (string, string)
		wantErrMsg string
	}{
		{
			name: "corrupted index file",
			setup: func() (string, string) {
				tempDir := t.TempDir()
				indexDir := filepath.Join(tempDir, "index")
				dataDir := filepath.Join(tempDir, "data")
				require.NoError(t, os.MkdirAll(indexDir, 0o755))
				require.NoError(t, os.MkdirAll(dataDir, 0o755))

				// Create a corrupted index file
				indexPath := filepath.Join(indexDir, indexFileName)
				corruptedData := []byte("corrupted index file data")
				require.NoError(t, os.WriteFile(indexPath, corruptedData, defaultFilePermissions))

				return indexDir, dataDir
			},
			wantErrMsg: "failed to read index header",
		},
		{
			name: "version mismatch in existing index file",
			setup: func() (string, string) {
				tempDir := t.TempDir()
				indexDir := filepath.Join(tempDir, "index")
				dataDir := filepath.Join(tempDir, "data")

				// Create directories
				require.NoError(t, os.MkdirAll(indexDir, 0o755))
				require.NoError(t, os.MkdirAll(dataDir, 0o755))

				// Create a valid index file with wrong version
				indexPath := filepath.Join(indexDir, indexFileName)
				header := indexFileHeader{
					Version:         999, // Wrong version
					MinHeight:       0,
					MaxDataFileSize: DefaultMaxDataFileSize,
					MaxHeight:       unsetHeight,
					NextWriteOffset: 0,
				}

				headerBytes, err := header.MarshalBinary()
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(indexPath, headerBytes, defaultFilePermissions))

				return indexDir, dataDir
			},
			wantErrMsg: "mismatched index file version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexDir, dataDir := tt.setup()
			if indexDir == "" || dataDir == "" {
				t.Skip("Setup failed, skipping test")
			}

			config := DefaultConfig().WithIndexDir(indexDir).WithDataDir(dataDir)
			_, err := New(config, logging.NoLog{})
			require.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestIndexFileHeaderAlignment(t *testing.T) {
	require.Equal(t, uint64(0), sizeOfIndexFileHeader%sizeOfIndexEntry,
		"sizeOfIndexFileHeader (%d) is not a multiple of sizeOfIndexEntry (%d)",
		sizeOfIndexFileHeader, sizeOfIndexEntry)
}

func TestIndexEntrySizePowerOfTwo(t *testing.T) {
	// Check that sizeOfIndexEntry is a power of 2
	// This is important for memory alignment and performance
	require.Equal(t, uint64(0), sizeOfIndexEntry&(sizeOfIndexEntry-1),
		"sizeOfIndexEntry (%d) is not a power of 2", sizeOfIndexEntry)
}

func TestNew_IndexFileConfigPrecedence(t *testing.T) {
	// set up db
	tempDir := t.TempDir()
	initialConfig := DefaultConfig().WithDir(tempDir).WithMinimumHeight(100).WithMaxDataFileSize(1024 * 1024)
	db, err := New(initialConfig, logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db)

	// Write a block at height 100 and close db
	testBlock := []byte("test block data")
	require.NoError(t, db.Put(100, testBlock))
	readBlock, err := db.Get(100)
	require.NoError(t, err)
	require.Equal(t, testBlock, readBlock)
	require.NoError(t, db.Close())

	// Reopen with different config that has minimum height of 200 and smaller max data file size
	differentConfig := DefaultConfig().WithDir(tempDir).WithMinimumHeight(200).WithMaxDataFileSize(512 * 1024)
	db2, err := New(differentConfig, logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db2)
	defer db2.Close()

	// The database should still accept blocks between 100 and 200
	testBlock2 := []byte("test block data 2")
	require.NoError(t, db2.Put(150, testBlock2))
	readBlock2, err := db2.Get(150)
	require.NoError(t, err)
	require.Equal(t, testBlock2, readBlock2)

	// Verify that writing below initial minimum height fails
	err = db2.Put(50, []byte("invalid block"))
	require.ErrorIs(t, err, ErrInvalidBlockHeight)

	// Write a large block that would exceed the new config's 512KB limit
	// but should succeed because we use the original 1MB limit from index file
	largeBlock := make([]byte, 768*1024) // 768KB block
	require.NoError(t, db2.Put(200, largeBlock))
	readLargeBlock, err := db2.Get(200)
	require.NoError(t, err)
	require.Equal(t, largeBlock, readLargeBlock)
}

func TestFileCache_Eviction(t *testing.T) {
	tests := []struct {
		name   string
		config DatabaseConfig
	}{
		{
			name:   "data file eviction",
			config: DefaultConfig().WithMaxDataFileSize(3),
		},
		// Test a condition where the data file can be closed between getting the file
		// handler and writing/reading from it. This can happen when another process
		// evicts the file handler between the get and write/read.
		// To trigger this in the test, we will create a cache with a size of 1 and
		// small max data file size to force multiple data files.
		// When trying to write or read a block, all other file handlers will be evicted.
		{
			name:   "retry opening data file if it's evicted",
			config: DefaultConfig().WithMaxDataFiles(1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newDatabase(t, tt.config.WithMaxDataFileSize(1024*1.5))
			store.compressor = compression.NewNoCompressor()

			// Override the file cache with specified size
			evictionCount := atomic.Int32{}
			evictionMu := sync.Mutex{}
			smallCache := lru.NewCacheWithOnEvict(tt.config.MaxDataFiles, func(_ int, file *os.File) {
				evictionMu.Lock()
				defer evictionMu.Unlock()
				evictionCount.Add(1)
				if file != nil {
					file.Close()
				}
			})
			store.fileCache = smallCache

			const numGoroutines = 4
			var wg sync.WaitGroup
			var writeErrors atomic.Int32

			// Thread-safe error message collection
			var errorMu sync.Mutex
			var errorMessages []string

			// Create blocks of 0.5kb each
			const numBlocks = 20 // 20 blocks will create 10 files
			blocks := make([][]byte, numBlocks)
			for i := range blocks {
				blocks[i] = fixedSizeBlock(t, 512, uint64(i))
			}

			// each goroutine writes all blocks
			for g := range numGoroutines {
				wg.Add(1)
				go func(goroutineID int) {
					defer wg.Done()
					for i := range numBlocks {
						height := uint64((i + goroutineID) % numBlocks)
						err := store.Put(height, blocks[height])
						if err != nil {
							writeErrors.Add(1)
							errorMu.Lock()
							errorMessages = append(errorMessages, fmt.Sprintf("goroutine %d, height %d: %v", goroutineID, height, err))
							errorMu.Unlock()
							return
						}
					}
				}(g)
			}

			wg.Wait()

			// Build error message if there were errors
			var errorMsg string
			if writeErrors.Load() > 0 {
				errorMsg = fmt.Sprintf("concurrent writes had %d errors:\n", writeErrors.Load())
				for _, msg := range errorMessages {
					errorMsg += fmt.Sprintf("  %s\n", msg)
				}
			}

			// Verify no write errors and we have evictions
			require.Zero(t, writeErrors.Load(), errorMsg)
			require.Positive(t, evictionCount.Load(), "should have had some cache evictions")

			// Verify again that all blocks are readable
			for i := range numBlocks {
				block, err := store.Get(uint64(i))
				require.NoError(t, err, "failed to read block at height %d", i)
				require.Equal(t, blocks[i], block, "block data mismatch at height %d", i)
			}
		})
	}
}

func TestMaxDataFiles_CacheLimit(t *testing.T) {
	// Test that the file cache respects the MaxDataFiles limit
	// Create a small cache size to test eviction behavior
	config := DefaultConfig().
		WithMaxDataFiles(2).      // Only allow 2 files in cache
		WithMaxDataFileSize(1024) // Small file size to force multiple files

	db := newDatabase(t, config)

	// Create blocks that will span multiple data files
	// Each block is ~512 bytes, so 2 blocks per file
	numBlocks := 6 // This will create 3 files, more than our cache limit of 2
	// Write blocks to force multiple data files
	for i := range numBlocks {
		block := fixedSizeBlock(t, 512, uint64(i))
		require.NoError(t, db.Put(uint64(i), block))
	}

	// Verify all blocks are still readable despite evictions
	for i := range numBlocks {
		block, err := db.Get(uint64(i))
		require.NoError(t, err, "failed to read block at height %d after eviction", i)
		require.Len(t, block, 512, "block size mismatch at height %d", i)
	}
}

// TestStructSizes verifies that our critical data structures have the expected sizes
func TestStructSizes(t *testing.T) {
	tests := []struct {
		name                string
		memorySize          uintptr
		binarySize          int
		expectedMemorySize  uintptr
		expectedBinarySize  int
		expectedMarshalSize int
		expectedPadding     uintptr
		createInstance      func() interface{}
	}{
		{
			name:                "indexFileHeader",
			memorySize:          unsafe.Sizeof(indexFileHeader{}),
			binarySize:          binary.Size(indexFileHeader{}),
			expectedMemorySize:  64,
			expectedBinarySize:  64,
			expectedMarshalSize: 64,
			expectedPadding:     0,
			createInstance:      func() interface{} { return indexFileHeader{} },
		},
		{
			name:                "blockEntryHeader",
			memorySize:          unsafe.Sizeof(blockEntryHeader{}),
			binarySize:          binary.Size(blockEntryHeader{}),
			expectedMemorySize:  32,
			expectedBinarySize:  22,
			expectedMarshalSize: 22,
			expectedPadding:     10,
			createInstance:      func() interface{} { return blockEntryHeader{} },
		},
		{
			name:                "indexEntry",
			memorySize:          unsafe.Sizeof(indexEntry{}),
			binarySize:          binary.Size(indexEntry{}),
			expectedMemorySize:  16,
			expectedBinarySize:  16,
			expectedMarshalSize: 16,
			expectedPadding:     0,
			createInstance:      func() interface{} { return indexEntry{} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualMemorySize := tt.memorySize
			require.Equal(t, tt.expectedMemorySize, actualMemorySize,
				"%s has unexpected memory size: got %d bytes, expected %d bytes",
				tt.name, actualMemorySize, tt.expectedMemorySize)

			binarySize := tt.binarySize
			require.Equal(t, tt.expectedBinarySize, binarySize,
				"%s binary size should be compact: got %d bytes, expected %d bytes",
				tt.name, binarySize, tt.expectedBinarySize)

			instance := tt.createInstance()
			var data []byte
			var err error

			data, err = instance.(encoding.BinaryMarshaler).MarshalBinary()
			require.NoError(t, err, "%s MarshalBinary should not fail", tt.name)
			require.Len(t, data, tt.expectedMarshalSize,
				"%s MarshalBinary should produce exactly %d bytes, got %d bytes",
				tt.name, tt.expectedMarshalSize, len(data))

			padding := actualMemorySize - uintptr(binarySize)
			require.Equal(t, tt.expectedPadding, padding,
				"%s should have %d bytes of padding: memory=%d, binary=%d",
				tt.name, tt.expectedPadding, actualMemorySize, binarySize)
		})
	}
}
