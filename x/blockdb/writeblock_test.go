// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/compression"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func TestWriteBlock_Basic(t *testing.T) {
	customConfig := DefaultConfig().WithMinimumHeight(10)

	tests := []struct {
		name               string
		blockHeights       []uint64 // block heights to write, in order
		config             DatabaseConfig
		expectedMCH        uint64 // expected max contiguous height
		expectedMaxHeight  uint64
		syncToDisk         bool
		checkpointInterval uint64
	}{
		{
			name:              "no blocks to write",
			expectedMCH:       unsetHeight,
			expectedMaxHeight: unsetHeight,
		},
		{
			name:              "single block at min height",
			blockHeights:      []uint64{0},
			expectedMCH:       0,
			expectedMaxHeight: 0,
		},
		{
			name:              "sequential blocks from min",
			blockHeights:      []uint64{0, 1, 2, 3},
			expectedMCH:       3,
			expectedMaxHeight: 3,
		},
		{
			name:              "out of order with no gaps",
			blockHeights:      []uint64{3, 1, 2, 0, 4},
			expectedMCH:       4,
			expectedMaxHeight: 4,
		},
		{
			name:              "blocks with gaps",
			blockHeights:      []uint64{0, 1, 3, 5, 6},
			expectedMCH:       1,
			expectedMaxHeight: 6,
		},
		{
			name:              "start with gap",
			blockHeights:      []uint64{5, 6},
			expectedMCH:       unsetHeight,
			expectedMaxHeight: 6,
		},
		{
			name:              "overwrite same height",
			blockHeights:      []uint64{0, 1, 0}, // Write to height 0 twice
			expectedMCH:       1,
			expectedMaxHeight: 1,
		},
		{
			name:              "custom min height single block",
			blockHeights:      []uint64{10},
			config:            customConfig,
			expectedMCH:       10,
			expectedMaxHeight: 10,
		},
		{
			name:              "custom min height out of order",
			blockHeights:      []uint64{13, 11, 10, 12},
			config:            customConfig,
			expectedMCH:       13,
			expectedMaxHeight: 13,
		},
		{
			name:              "custom min height with gaps",
			blockHeights:      []uint64{10, 11, 13, 15},
			config:            customConfig,
			expectedMCH:       11,
			expectedMaxHeight: 15,
		},
		{
			name:              "custom min height start with gap",
			blockHeights:      []uint64{11, 12},
			config:            customConfig,
			expectedMCH:       unsetHeight,
			expectedMaxHeight: 12,
		},
		{
			name:              "with sync to disk",
			blockHeights:      []uint64{0, 1, 2, 5},
			syncToDisk:        true,
			expectedMCH:       2,
			expectedMaxHeight: 5,
		},
		{
			name:               "custom checkpoint interval",
			blockHeights:       []uint64{0, 1, 2, 3, 4},
			checkpointInterval: 2,
			expectedMCH:        4,
			expectedMaxHeight:  4,
		},
		{
			name: "complicated gaps",
			blockHeights: []uint64{
				10, 3, 2, 9, 35, 34, 30, 1, 9, 88, 83, 4, 43, 5, 0,
			},
			expectedMCH:       5,
			expectedMaxHeight: 88,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config
			if config.CheckpointInterval == 0 {
				config = DefaultConfig()
			}

			store, cleanup := newTestDatabase(t, config)
			defer cleanup()

			blocksWritten := make(map[uint64][]byte)
			for _, h := range tt.blockHeights {
				block := randomBlock(t)
				err := store.Put(h, block)
				require.NoError(t, err, "unexpected error at height %d", h)

				blocksWritten[h] = block
			}

			// Verify all written blocks are readable and data is correct
			for h, expectedBlock := range blocksWritten {
				readBlock, err := store.Get(h)
				require.NoError(t, err, "Get failed at height %d", h)
				require.Equal(t, expectedBlock, readBlock)
			}

			checkDatabaseState(t, store, tt.expectedMaxHeight, tt.expectedMCH)
		})
	}
}

func TestWriteBlock_Concurrency(t *testing.T) {
	store, cleanup := newTestDatabase(t, DefaultConfig())
	defer cleanup()

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
	checkDatabaseState(t, store, 19, 4)
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
				db.nextDataWriteOffset.Store(math.MaxUint64 - 50)
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:   "writeBlockAt - failed to get data file",
			height: 0,
			block:  make([]byte, 100),
			setup: func(db *Database) {
				// Change file permissions to read-only
				file, err := db.getOrOpenDataFile(0)
				require.NoError(t, err)
				filePath := file.Name()
				file.Close()
				require.NoError(t, os.Chmod(filePath, 0o444))
			},
			wantErrMsg: "failed to get data file for writing block",
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

			store, cleanup := newTestDatabase(t, config)
			if tt.disableCompression {
				store.compressor = compression.NewNoCompressor()
			}
			defer cleanup()

			if tt.setup != nil {
				tt.setup(store)
			}

			err := store.Put(tt.height, tt.block)
			if tt.wantErrMsg != "" {
				require.True(t, strings.HasPrefix(err.Error(), tt.wantErrMsg), "expected error message to start with %s, got %s", tt.wantErrMsg, err.Error())
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
			checkDatabaseState(t, store, unsetHeight, unsetHeight)
		})
	}
}
