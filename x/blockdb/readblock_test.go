// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"bytes"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
)

func TestReadOperations(t *testing.T) {
	tests := []struct {
		name       string
		readHeight uint64
		noBlock    bool
		config     *DatabaseConfig
		setup      func(db *Database)
		wantErr    error
	}{
		{
			name:       "read first block",
			readHeight: 0,
		},
		{
			name:       "read max height block",
			readHeight: 50,
		},
		{
			name:       "read height with no block",
			readHeight: 40,
			noBlock:    true,
		},
		{
			name:       "read block higher than max height",
			readHeight: 100,
			noBlock:    true,
		},
		{
			name:       "read valid block with non-zero minimum height",
			readHeight: 25,
			config: &DatabaseConfig{
				MinimumHeight:      20,
				MaxDataFileSize:    DefaultMaxDataFileSize,
				CheckpointInterval: 1024,
				MaxDataFiles:       DefaultMaxDataFileSize,
				BlockCacheSize:     DefaultBlockCacheSize,
			},
		},
		{
			name:       "database closed",
			readHeight: 1,
			setup: func(db *Database) {
				db.Close()
			},
			wantErr: database.ErrClosed,
		},
		{
			name:       "height below minimum",
			readHeight: 5,
			config: &DatabaseConfig{
				MinimumHeight:      10,
				MaxDataFileSize:    DefaultMaxDataFileSize,
				CheckpointInterval: 1024,
				MaxDataFiles:       DefaultMaxDataFileSize,
				BlockCacheSize:     DefaultBlockCacheSize,
			},
			wantErr: ErrInvalidBlockHeight,
		},
		{
			name:       "block is past max height",
			readHeight: 51,
			wantErr:    database.ErrNotFound,
		},
		{
			name:       "block height is max height",
			readHeight: math.MaxUint64,
			wantErr:    database.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config
			if config == nil {
				defaultConfig := DefaultConfig()
				config = &defaultConfig
			}

			store := newDatabase(t, *config)

			// Seed database with blocks based on config (unless skipSeed is true)
			seededBlocks := make(map[uint64][]byte)
			minHeight := config.MinimumHeight
			maxHeight := minHeight + 50 // Always write 51 blocks
			gapHeight := minHeight + 40 // Gap at relative position 40

			for i := minHeight; i <= maxHeight; i++ {
				if i == gapHeight {
					continue // Create gap
				}

				block := randomBlock(t)
				require.NoError(t, store.Put(i, block))
				seededBlocks[i] = block
			}

			if tt.setup != nil {
				tt.setup(store)
			}

			if tt.wantErr != nil {
				_, err := store.Get(tt.readHeight)
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			// Handle success cases
			if tt.noBlock {
				_, err := store.Get(tt.readHeight)
				require.ErrorIs(t, err, database.ErrNotFound)
			} else {
				readBlock, err := store.Get(tt.readHeight)
				require.NoError(t, err)
				require.NotNil(t, readBlock)
				expectedBlock := seededBlocks[tt.readHeight]
				require.Equal(t, expectedBlock, readBlock)
			}
		})
	}
}

func TestReadOperations_Concurrency(t *testing.T) {
	store := newDatabase(t, DefaultConfig())

	// Pre-generate blocks and write them
	numBlocks := 50
	blocks := make([][]byte, numBlocks)
	gapHeights := map[uint64]bool{
		10: true,
		20: true,
	}

	for i := range numBlocks {
		if gapHeights[uint64(i)] {
			continue
		}

		blocks[i] = randomBlock(t)
		require.NoError(t, store.Put(uint64(i), blocks[i]))
	}

	var wg sync.WaitGroup
	var errorCount atomic.Int32
	var blockErrors atomic.Int32

	for i := range numBlocks + 10 {
		wg.Add(3) // One for each read operation

		go func(height int) {
			defer wg.Done()
			block, err := store.Get(uint64(height))
			if gapHeights[uint64(height)] || height >= numBlocks {
				if err == nil || !errors.Is(err, database.ErrNotFound) {
					errorCount.Add(1)
				}
			} else {
				if err != nil {
					errorCount.Add(1)
					return
				}
				if !bytes.Equal(blocks[height], block) {
					blockErrors.Add(1)
				}
			}
		}(i)

		go func(height int) {
			defer wg.Done()
			_, err := store.Get(uint64(height))
			if gapHeights[uint64(height)] || height >= numBlocks {
				if err == nil || !errors.Is(err, database.ErrNotFound) {
					errorCount.Add(1)
				}
			} else {
				if err != nil {
					errorCount.Add(1)
					return
				}
			}
		}(i)

		go func(height int) {
			defer wg.Done()
			_, err := store.Get(uint64(height))
			if gapHeights[uint64(height)] || height >= numBlocks {
				if err == nil || !errors.Is(err, database.ErrNotFound) {
					errorCount.Add(1)
				}
			} else {
				if err != nil {
					errorCount.Add(1)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	require.Zero(t, errorCount.Load(), "concurrent read operations had errors")
	require.Zero(t, blockErrors.Load(), "block data mismatches detected")
}

func TestHasBlock(t *testing.T) {
	minHeight := uint64(10)
	blocksCount := uint64(10)
	gapHeight := minHeight + 5

	testCases := []struct {
		name     string
		height   uint64
		expected bool
		wantErr  error
		dbClosed bool
	}{
		{
			name:     "has_height",
			height:   12,
			expected: true,
		},
		{
			name:     "below_minimum_height",
			height:   0,
			expected: false,
		},
		{
			name:     "above_max_height",
			height:   minHeight + blocksCount + 1,
			expected: false,
		},
		{
			name:     "at_max_height",
			height:   minHeight + blocksCount,
			expected: true,
		},
		{
			name:     "at_min_height",
			height:   minHeight,
			expected: true,
		},
		{
			name:     "no_block",
			height:   gapHeight,
			expected: false,
		},
		{
			name:     "db_closed",
			dbClosed: true,
			wantErr:  database.ErrClosed,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := newDatabase(t, DefaultConfig().WithMinimumHeight(minHeight))

			for i := minHeight; i <= minHeight+blocksCount; i++ {
				if i == gapHeight {
					continue
				}
				require.NoError(t, store.Put(i, randomBlock(t)))
			}

			if tc.dbClosed {
				require.NoError(t, store.Close())
			}

			has, err := store.Has(tc.height)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, has)
			}
		})
	}
}
