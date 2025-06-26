// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDataSplitting(t *testing.T) {
	// Each data file should have enough space for 2 blocks
	config := DefaultDatabaseConfig().WithMaxDataFileSize(1024 * 2.5)
	store, cleanup := newTestDatabase(t, config)
	defer cleanup()

	// create 11 blocks, 1kb each
	numBlocks := 11
	blocks := make([][]byte, numBlocks)
	for i := range numBlocks {
		blocks[i] = make([]byte, 1024)
		blocks[i][0] = byte(i)
		require.NoError(t, store.WriteBlock(uint64(i), blocks[i], 0))
	}

	// Verify that multiple data files were created.
	files, err := os.ReadDir(store.dataDir)
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
		readBlock, err := store.ReadBlock(uint64(i))
		require.NoError(t, err)
		require.Equal(t, blocks[i], readBlock)
	}

	// reopen and verify all blocks are readable
	require.NoError(t, store.Close())
	store, err = New(filepath.Dir(store.indexFile.Name()), store.dataDir, config, store.log)
	require.NoError(t, err)
	defer store.Close()
	for i := range numBlocks {
		readBlock, err := store.ReadBlock(uint64(i))
		require.NoError(t, err)
		require.Equal(t, blocks[i], readBlock)
	}
}

func TestDataSplitting_DeletedFile(t *testing.T) {
	config := DefaultDatabaseConfig().WithMaxDataFileSize(1024 * 2.5)
	store, cleanup := newTestDatabase(t, config)
	defer cleanup()

	// create 5 blocks, 1kb each
	numBlocks := 5
	blocks := make([][]byte, numBlocks)
	for i := range numBlocks {
		blocks[i] = make([]byte, 1024)
		blocks[i][0] = byte(i)
		require.NoError(t, store.WriteBlock(uint64(i), blocks[i], 0))
	}
	store.Close()

	// Delete the first data file (blockdb_0.dat)
	firstDataFilePath := filepath.Join(store.dataDir, fmt.Sprintf(dataFileNameFormat, 0))
	require.NoError(t, os.Remove(firstDataFilePath))

	// reopen and verify the blocks
	require.NoError(t, store.Close())
	store, err := New(filepath.Dir(store.indexFile.Name()), store.dataDir, config, store.log)
	require.NoError(t, err)
	defer store.Close()
	for i := range numBlocks {
		readBlock, err := store.ReadBlock(uint64(i))
		require.NoError(t, err)
		if i < 2 {
			require.Nil(t, readBlock)
		} else {
			require.Equal(t, blocks[i], readBlock)
		}
	}
}
