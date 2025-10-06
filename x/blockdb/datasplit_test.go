// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/compression"
)

func TestDataSplitting(t *testing.T) {
	// Each data file should have enough space for 2 blocks
	config := DefaultConfig().WithMaxDataFileSize(1024 * 2.5)
	store, cleanup := newTestDatabase(t, config)
	defer cleanup()

	// Override the compressor so we can have fixed size blocks
	store.compressor = compression.NewNoCompressor()

	// create 11 blocks, 1kb each
	numBlocks := 11
	blocks := make([][]byte, numBlocks)
	for i := range numBlocks {
		blocks[i] = fixedSizeBlock(t, 1024, uint64(i))
		require.NoError(t, store.Put(uint64(i), blocks[i]))
	}

	// Verify that multiple data files were created.
	files, err := os.ReadDir(store.config.DataDir)
	require.NoError(t, err)
	var dataFileCount int
	for _, file := range files {
		var index int
		if n, err := fmt.Sscanf(file.Name(), dataFileNameFormat, &index); n == 1 && err == nil {
			dataFileCount++
		}
	}

	// 6 data files should be created
	require.Equal(t, 6, dataFileCount)

	// Verify all blocks are readable
	for i := range numBlocks {
		readBlock, err := store.Get(uint64(i))
		require.NoError(t, err)
		require.Equal(t, blocks[i], readBlock)
	}

	// reopen and verify all blocks are readable
	require.NoError(t, store.Close())
	config = config.WithDataDir(store.config.DataDir).WithIndexDir(store.config.IndexDir)
	store, err = New(config, store.log)
	require.NoError(t, err)
	store.compressor = compression.NewNoCompressor()
	defer store.Close()
	for i := range numBlocks {
		readBlock, err := store.Get(uint64(i))
		require.NoError(t, err)
		require.Equal(t, blocks[i], readBlock)
	}
}

func TestDataSplitting_DeletedFile(t *testing.T) {
	config := DefaultConfig().WithMaxDataFileSize(1024 * 2.5)
	store, cleanup := newTestDatabase(t, config)
	defer cleanup()

	// create 5 blocks, 1kb each
	numBlocks := 5
	blocks := make([][]byte, numBlocks)
	for i := range numBlocks {
		blocks[i] = fixedSizeBlock(t, 1024, uint64(i))
		require.NoError(t, store.Put(uint64(i), blocks[i]))
	}
	require.NoError(t, store.Close())

	// Delete the first data file (blockdb_0.dat)
	firstDataFilePath := filepath.Join(store.config.DataDir, fmt.Sprintf(dataFileNameFormat, 0))
	require.NoError(t, os.Remove(firstDataFilePath))

	// reopen and verify the blocks
	config = config.WithIndexDir(store.config.IndexDir).WithDataDir(store.config.DataDir)
	_, err := New(config, store.log)
	require.ErrorIs(t, err, ErrCorrupted)
}
