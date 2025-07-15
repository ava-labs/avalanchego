// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestNew_Truncate(t *testing.T) {
	// Create initial database
	tempDir := t.TempDir()
	indexDir := filepath.Join(tempDir, "index")
	dataDir := filepath.Join(tempDir, "data")
	config := DefaultDatabaseConfig().WithTruncate(true)
	db, err := New(indexDir, dataDir, config, logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db)

	// Write some test data and close the database
	testBlock := []byte("test block data")
	require.NoError(t, db.WriteBlock(0, testBlock, 0))
	require.NoError(t, db.Close())

	// Reopen with truncate=true and verify data is gone
	db2, err := New(indexDir, dataDir, config, logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db2)
	defer db2.Close()
	readBlock2, err := db2.ReadBlock(1)
	require.NoError(t, err)
	require.Nil(t, readBlock2)
	_, found := db2.MaxContiguousHeight()
	require.False(t, found)
}

func TestNew_NoTruncate(t *testing.T) {
	tempDir := t.TempDir()
	indexDir := filepath.Join(tempDir, "index")
	dataDir := filepath.Join(tempDir, "data")
	config := DefaultDatabaseConfig().WithTruncate(true)
	db, err := New(indexDir, dataDir, config, logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db)

	// Write some test data and close the database
	testBlock := []byte("test block data")
	require.NoError(t, db.WriteBlock(1, testBlock, 5))
	readBlock, err := db.ReadBlock(1)
	require.NoError(t, err)
	require.Equal(t, testBlock, readBlock)
	require.NoError(t, db.Close())

	// Reopen with truncate=false and verify data is still there
	config = DefaultDatabaseConfig().WithTruncate(false)
	db2, err := New(indexDir, dataDir, config, logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db2)
	defer db2.Close()
	readBlock1, err := db2.ReadBlock(1)
	require.NoError(t, err)
	require.Equal(t, testBlock, readBlock1)

	// Verify we can write additional data
	testBlock2 := []byte("test block data 3")
	require.NoError(t, db2.WriteBlock(2, testBlock2, 0))
	readBlock2, err := db2.ReadBlock(2)
	require.NoError(t, err)
	require.Equal(t, testBlock2, readBlock2)
}

func TestNew_Params(t *testing.T) {
	tempDir := t.TempDir()
	tests := []struct {
		name        string
		indexDir    string
		dataDir     string
		config      DatabaseConfig
		log         logging.Logger
		wantErr     error
		expectClose bool
	}{
		{
			name:     "default config",
			indexDir: tempDir,
			dataDir:  tempDir,
			config:   DefaultDatabaseConfig(),
		},
		{
			name:     "custom config",
			indexDir: tempDir,
			dataDir:  tempDir,
			config: DefaultDatabaseConfig().
				WithMinimumHeight(100).
				WithMaxDataFileSize(1024 * 1024). // 1MB
				WithCheckpointInterval(512),
		},
		{
			name:     "empty index directory",
			indexDir: "",
			dataDir:  tempDir,
			config:   DefaultDatabaseConfig(),
			wantErr:  errors.New("both indexDir and dataDir must be provided"),
		},
		{
			name:     "empty data directory",
			indexDir: tempDir,
			dataDir:  "",
			config:   DefaultDatabaseConfig(),
			wantErr:  errors.New("both indexDir and dataDir must be provided"),
		},
		{
			name:     "both directories empty",
			indexDir: "",
			config:   DefaultDatabaseConfig(),
			dataDir:  "",
			wantErr:  errors.New("both indexDir and dataDir must be provided"),
		},
		{
			name:     "invalid config - zero checkpoint interval",
			indexDir: tempDir,
			dataDir:  tempDir,
			config:   DefaultDatabaseConfig().WithCheckpointInterval(0),
			wantErr:  errors.New("CheckpointInterval cannot be 0"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := New(tt.indexDir, tt.dataDir, tt.config, tt.log)

			if tt.wantErr != nil {
				require.Equal(t, tt.wantErr.Error(), err.Error())
				return
			}

			require.NoError(t, err)
			require.NotNil(t, db)

			// Verify the database was created with correct configuration
			require.Equal(t, tt.config.MinimumHeight, db.options.MinimumHeight)
			require.Equal(t, tt.config.MaxDataFileSize, db.options.MaxDataFileSize)
			require.Equal(t, tt.config.CheckpointInterval, db.options.CheckpointInterval)
			require.Equal(t, tt.config.SyncToDisk, db.options.SyncToDisk)

			indexPath := filepath.Join(tt.indexDir, indexFileName)
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
					Version:             999, // Wrong version
					MinHeight:           0,
					MaxDataFileSize:     DefaultMaxDataFileSize,
					MaxHeight:           unsetHeight,
					MaxContiguousHeight: unsetHeight,
					NextWriteOffset:     0,
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

			_, err := New(indexDir, dataDir, DefaultDatabaseConfig(), logging.NoLog{})
			require.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestIndexFileHeaderAlignment(t *testing.T) {
	require.Equal(t, uint64(0), sizeOfIndexFileHeader%sizeOfIndexEntry,
		"sizeOfIndexFileHeader (%d) is not a multiple of sizeOfIndexEntry (%d)",
		sizeOfIndexFileHeader, sizeOfIndexEntry)
}

func TestNew_IndexFileConfigPrecedence(t *testing.T) {
	// set up db
	initialConfig := DefaultDatabaseConfig().WithMinimumHeight(100).WithMaxDataFileSize(1024 * 1024)
	tempDir := t.TempDir()
	db, err := New(tempDir, tempDir, initialConfig, logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db)

	// Write a block at height 100 and close db
	testBlock := []byte("test block data")
	require.NoError(t, db.WriteBlock(100, testBlock, 0))
	readBlock, err := db.ReadBlock(100)
	require.NoError(t, err)
	require.Equal(t, testBlock, readBlock)
	require.NoError(t, db.Close())

	// Reopen with different config that has minimum height of 200 and smaller max data file size
	differentConfig := DefaultDatabaseConfig().WithMinimumHeight(200).WithMaxDataFileSize(512 * 1024)
	db2, err := New(tempDir, tempDir, differentConfig, logging.NoLog{})
	require.NoError(t, err)
	require.NotNil(t, db2)
	defer db2.Close()

	// The database should still accept blocks between 100 and 200
	testBlock2 := []byte("test block data 2")
	require.NoError(t, db2.WriteBlock(150, testBlock2, 0))
	readBlock2, err := db2.ReadBlock(150)
	require.NoError(t, err)
	require.Equal(t, testBlock2, readBlock2)

	// Verify that writing below initial minimum height fails
	err = db2.WriteBlock(50, []byte("invalid block"), 0)
	require.ErrorIs(t, err, ErrInvalidBlockHeight)

	// Write a large block that would exceed the new config's 512KB limit
	// but should succeed because we use the original 1MB limit from index file
	largeBlock := make([]byte, 768*1024) // 768KB block
	require.NoError(t, db2.WriteBlock(200, largeBlock, 0))
	readLargeBlock, err := db2.ReadBlock(200)
	require.NoError(t, err)
	require.Equal(t, largeBlock, readLargeBlock)
}

func TestFileCache_Eviction(t *testing.T) {
	// Create a database with a small max data file size to force multiple files
	// each file should have enough for 2 blocks (0.5kb * 2)
	config := DefaultDatabaseConfig().WithMaxDataFileSize(1024 * 1.5)
	store, cleanup := newTestDatabase(t, config)
	defer cleanup()

	// Override the file cache with a smaller size to force evictions
	evictionCount := atomic.Int32{}
	evictionMu := sync.Mutex{}
	smallCache := lru.NewCache[int, *os.File](3) // Only 3 files in cache
	smallCache.SetOnEvict(func(_ int, file *os.File) {
		evictionMu.Lock()
		defer evictionMu.Unlock()
		evictionCount.Add(1)
		if file != nil {
			file.Close()
		}
	})
	store.fileCache = smallCache

	const numBlocks = 20 // 20 blocks will create 10 files
	const numGoroutines = 4
	var wg sync.WaitGroup
	var writeErrors atomic.Int32

	// Create blocks of 0.5kb each
	blocks := make([][]byte, numBlocks)
	for i := range blocks {
		blocks[i] = fixedSizeBlock(t, 512, uint64(i))
	}

	// Each goroutine writes all block heights 0-(numBlocks-1)
	for g := range numGoroutines {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := range numBlocks {
				height := uint64((i + goroutineID) % numBlocks)
				err := store.WriteBlock(height, blocks[height], 0)
				if err != nil {
					writeErrors.Add(1)
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify no write errors
	require.Zero(t, writeErrors.Load(), "concurrent writes had errors")

	// Verify we had some evictions
	require.Positive(t, evictionCount.Load(), "should have had some cache evictions")

	// Verify all blocks are readable
	for i := range numBlocks {
		block, err := store.ReadBlock(uint64(i))
		require.NoError(t, err, "failed to read block at height %d", i)
		require.Equal(t, blocks[i], block, "block data mismatch at height %d", i)
	}
}
