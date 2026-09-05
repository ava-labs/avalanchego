// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/compression"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func TestPutGet(t *testing.T) {
	tests := []struct {
		name  string
		block []byte
		want  []byte
	}{
		{
			name:  "normal write",
			block: []byte("hello"),
			want:  []byte("hello"),
		},
		{
			name:  "empty block",
			block: []byte{},
			want:  []byte{},
		},
		{
			name:  "nil block",
			block: nil,
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newDatabase(t, DefaultConfig())
			require.NoError(t, db.Put(0, tt.block))

			got, err := db.Get(0)
			require.NoError(t, err)
			require.True(t, bytes.Equal(tt.want, got))
		})
	}
}

func TestPut_MaxHeight(t *testing.T) {
	customConfig := DefaultConfig().WithMinimumHeight(10)

	tests := []struct {
		name               string
		blockHeights       []uint64 // block heights to write, in order
		config             DatabaseConfig
		expectedMaxHeight  uint64
		syncToDisk         bool
		checkpointInterval uint64
	}{
		{
			name:              "no blocks to write",
			expectedMaxHeight: unsetHeight,
		},
		{
			name:              "single block at min height",
			blockHeights:      []uint64{0},
			expectedMaxHeight: 0,
		},
		{
			name:              "sequential blocks from min",
			blockHeights:      []uint64{0, 1, 2, 3},
			expectedMaxHeight: 3,
		},
		{
			name:              "out of order with no gaps",
			blockHeights:      []uint64{3, 1, 2, 0, 4},
			expectedMaxHeight: 4,
		},
		{
			name:              "blocks with gaps",
			blockHeights:      []uint64{0, 1, 3, 5, 6},
			expectedMaxHeight: 6,
		},
		{
			name:              "start with gap",
			blockHeights:      []uint64{5, 6},
			expectedMaxHeight: 6,
		},
		{
			name:              "overwrite same height",
			blockHeights:      []uint64{0, 1, 0}, // Write to height 0 twice
			expectedMaxHeight: 1,
		},
		{
			name:              "custom min height single block",
			blockHeights:      []uint64{10},
			config:            customConfig,
			expectedMaxHeight: 10,
		},
		{
			name:              "custom min height out of order",
			blockHeights:      []uint64{13, 11, 10, 12},
			config:            customConfig,
			expectedMaxHeight: 13,
		},
		{
			name:              "custom min height with gaps",
			blockHeights:      []uint64{10, 11, 13, 15},
			config:            customConfig,
			expectedMaxHeight: 15,
		},
		{
			name:              "custom min height start with gap",
			blockHeights:      []uint64{11, 12},
			config:            customConfig,
			expectedMaxHeight: 12,
		},
		{
			name:              "with sync to disk",
			blockHeights:      []uint64{0, 1, 2, 5},
			syncToDisk:        true,
			expectedMaxHeight: 5,
		},
		{
			name:               "custom checkpoint interval",
			blockHeights:       []uint64{0, 1, 2, 3, 4},
			checkpointInterval: 2,
			expectedMaxHeight:  4,
		},
		{
			name: "complicated gaps",
			blockHeights: []uint64{
				10, 3, 2, 9, 35, 34, 30, 1, 9, 88, 83, 4, 43, 5, 0,
			},
			expectedMaxHeight: 88,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config
			if config.CheckpointInterval == 0 {
				config = DefaultConfig()
			}
			store := newDatabase(t, config)

			blocksWritten := make(map[uint64][]byte)
			for _, h := range tt.blockHeights {
				block := randomBlock(t)
				err := store.Put(h, block)
				require.NoError(t, err, "unexpected error at height %d", h)

				blocksWritten[h] = block
			}

			checkDatabaseState(t, store, tt.expectedMaxHeight)
		})
	}
}

func TestWriteBlock_Concurrency(t *testing.T) {
	store := newDatabase(t, DefaultConfig())

	var wg sync.WaitGroup
	var errors atomic.Int32

	// Pre-generate blocks for reuse
	blocks := make([][]byte, 20)
	for i := range 20 {
		blocks[i] = randomBlock(t)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var height uint64
			block := blocks[i]

			// create gaps at heights 5 and 10 and rewrite last block
			if i == 5 || i == 10 {
				height = uint64(i - 1)
				block = blocks[i-1]
			} else {
				height = uint64(i)
			}

			err := store.Put(height, block)
			if err != nil {
				errors.Add(1)
			}
		}(i)
	}

	wg.Wait()
	require.Zero(t, errors.Load(), "concurrent writes had errors")

	// Verify that all expected heights have blocks (except 5, 10)
	for i := range 20 {
		height := uint64(i)
		block, err := store.Get(height)
		if i == 5 || i == 10 {
			require.ErrorIs(t, err, database.ErrNotFound, "expected ErrNotFound at gap height %d", height)
		} else {
			require.NoError(t, err)
			require.Equal(t, blocks[i], block, "block mismatch at height %d", height)
		}
	}
	checkDatabaseState(t, store, 19)
}

func TestPutContinuesAfterIndexWriteFailure(t *testing.T) {
	db := newDatabase(t, DefaultConfig().WithDir(t.TempDir()))
	indexPath := db.indexFile.Name()
	// Fail after the data write, when Put attempts to write the index entry.
	require.NoError(t, db.indexFile.Close())

	err := db.Put(1, []byte("failed index write"))
	require.ErrorIs(t, err, os.ErrClosed)

	indexFile, err := os.OpenFile(indexPath, os.O_RDWR, defaultFilePermissions)
	require.NoError(t, err)
	db.indexFile = indexFile
	require.NoError(t, db.Put(2, []byte("successful later write")))
	require.NoError(t, db.Close())

	db = newDatabase(t, db.config)
	_, err = db.Get(1)
	require.ErrorIs(t, err, database.ErrNotFound)
	got, err := db.Get(2)
	require.NoError(t, err)
	require.Equal(t, []byte("successful later write"), got)
	require.NoError(t, db.Close())
}

func TestFailedPutDoesNotAdvanceCheckpoint(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	config := DefaultConfig().
		WithIndexDir(filepath.Join(dir, "index")).
		WithDataDir(dataDir)
	db := newDatabase(t, config)
	blocks := [][]byte{
		[]byte("first block"),
		[]byte("second block"),
	}
	for height, block := range blocks {
		require.NoError(t, db.Put(uint64(height), block))
	}
	// Make the next Put reserve an offset, then fail reopening the unavailable data file.
	db.fileCache.Flush()

	movedDataDir := filepath.Join(dir, "moved-data")
	require.NoError(t, os.Rename(dataDir, movedDataDir))
	err := db.Put(uint64(len(blocks)), []byte("failed data write"))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, os.Rename(movedDataDir, dataDir))
	require.NoError(t, db.Close())

	db = newDatabase(t, config)
	for height, block := range blocks {
		got, err := db.Get(uint64(height))
		require.NoError(t, err)
		require.Equal(t, block, got)
	}
	_, err = db.Get(uint64(len(blocks)))
	require.ErrorIs(t, err, database.ErrNotFound)
	require.NoError(t, db.Close())
}

func TestPutContinuesAfterDataWriteFailure(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	config := DefaultConfig().
		WithIndexDir(filepath.Join(dir, "index")).
		WithDataDir(dataDir).
		WithMaxDataFileSize(100)
	db := newDatabase(t, config)
	db.compressor = compression.NewNoCompressor()
	require.NoError(t, db.Put(0, make([]byte, 40)))
	wantLaterOffset := db.nextDataReservationOffset.Load()

	// Force the failed Put to reserve in the next data file, then fail opening it.
	db.fileCache.Flush()
	movedDataDir := filepath.Join(dir, "moved-data")
	require.NoError(t, os.Rename(dataDir, movedDataDir))
	err := db.Put(1, make([]byte, 20))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, os.Rename(movedDataDir, dataDir))

	laterBlock := []byte("later")
	require.NoError(t, db.Put(2, laterBlock))
	laterEntry, err := db.readIndexEntry(2)
	require.NoError(t, err)
	require.Equal(t, wantLaterOffset, laterEntry.Offset)
	got, err := db.Get(2)
	require.NoError(t, err)
	require.Equal(t, laterBlock, got)
	require.NoError(t, db.Close())

	db = newDatabase(t, config)
	db.compressor = compression.NewNoCompressor()
	_, err = db.Get(1)
	require.ErrorIs(t, err, database.ErrNotFound)
	got, err = db.Get(2)
	require.NoError(t, err)
	require.Equal(t, laterBlock, got)
	require.NoError(t, db.Close())
}

func TestWriteBlock_Errors(t *testing.T) {
	tests := []struct {
		name               string
		height             uint64
		block              []byte
		setup              func(db *Database)
		config             DatabaseConfig
		disableCompression bool
		wantErr            error
		wantErrMsg         string
	}{
		{
			name:    "height below custom minimum",
			height:  5,
			block:   randomBlock(t),
			config:  DefaultConfig().WithMinimumHeight(10),
			wantErr: ErrInvalidBlockHeight,
		},
		{
			name:    "height causes overflow",
			height:  math.MaxUint64,
			block:   randomBlock(t),
			wantErr: ErrInvalidBlockHeight,
		},
		{
			name:   "database closed",
			height: 0,
			block:  randomBlock(t),
			setup: func(db *Database) {
				db.Close()
			},
			wantErr: database.ErrClosed,
		},
		{
			name:               "exceed max data file size",
			height:             0,
			disableCompression: true,
			block:              make([]byte, 1003), // Block + header will exceed 1024 limit (1003 + 26 = 1029 > 1024)
			config:             DefaultConfig().WithMaxDataFileSize(1024),
			wantErr:            ErrBlockTooLarge,
		},
		{
			name:               "data file offset overflow",
			height:             0,
			block:              make([]byte, 100),
			disableCompression: true,
			config:             DefaultConfig(),
			setup: func(db *Database) {
				// Set the next write offset to near max to trigger overflow
				db.nextDataReservationOffset.Store(math.MaxUint64 - 50)
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:   "writeBlockAt - failed to get data file",
			height: 0,
			block:  make([]byte, 100),
			setup: func(db *Database) {
				// Change file permissions to read-only
				file, err := db.getDataFile(0, os.O_RDWR|os.O_CREATE)
				require.NoError(t, err)
				filePath := file.Name()
				file.Close()
				require.NoError(t, os.Chmod(filePath, 0o444))
			},
			wantErrMsg: "failed to write block to data file at offset",
		},
		{
			name:   "writeIndexEntryAt - index file write failure",
			height: 0,
			block:  make([]byte, 100),
			setup: func(db *Database) {
				db.indexFile.Close()
			},
			wantErrMsg: "failed to write index entry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config
			if config.CheckpointInterval == 0 {
				config = DefaultConfig()
			}

			store := newDatabase(t, config)
			if tt.disableCompression {
				store.compressor = compression.NewNoCompressor()
			}

			if tt.setup != nil {
				tt.setup(store)
			}

			err := store.Put(tt.height, tt.block)
			if tt.wantErrMsg != "" {
				require.True(t, strings.HasPrefix(err.Error(), tt.wantErrMsg), "expected error message to start with %s, got %s", tt.wantErrMsg, err.Error())
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
			checkDatabaseState(t, store, unsetHeight)
		})
	}
}
