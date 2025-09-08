// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
			},
		},
		{
			name:       "database closed",
			readHeight: 1,
			setup: func(db *Database) {
				db.Close()
			},
			wantErr: ErrDatabaseClosed,
		},
		{
			name:       "height below minimum",
			readHeight: 5,
			config: &DatabaseConfig{
				MinimumHeight:      10,
				MaxDataFileSize:    DefaultMaxDataFileSize,
				CheckpointInterval: 1024,
				MaxDataFiles:       DefaultMaxDataFileSize,
			},
			wantErr: ErrInvalidBlockHeight,
		},
		{
			name:       "block is past max height",
			readHeight: 51,
			wantErr:    ErrBlockNotFound,
		},
		{
			name:       "block height is max height",
			readHeight: math.MaxUint64,
			wantErr:    ErrBlockNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config
			if config == nil {
				defaultConfig := DefaultConfig()
				config = &defaultConfig
			}

			store, cleanup := newTestDatabase(t, *config)
			defer cleanup()

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
				require.NoError(t, store.WriteBlock(i, block))
				seededBlocks[i] = block
			}

			if tt.setup != nil {
				tt.setup(store)
			}

			if tt.wantErr != nil {
				_, err := store.ReadBlock(tt.readHeight)
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			// Handle success cases
			if tt.noBlock {
				_, err := store.ReadBlock(tt.readHeight)
				require.ErrorIs(t, err, ErrBlockNotFound)
			} else {
				readBlock, err := store.ReadBlock(tt.readHeight)
				require.NoError(t, err)
				require.NotNil(t, readBlock)
				expectedBlock := seededBlocks[tt.readHeight]
				require.Equal(t, expectedBlock, readBlock)
			}
		})
	}
}

func TestReadOperations_Concurrency(t *testing.T) {
	store, cleanup := newTestDatabase(t, DefaultConfig())
	defer cleanup()

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
		require.NoError(t, store.WriteBlock(uint64(i), blocks[i]))
	}

	var wg sync.WaitGroup
	var errorCount atomic.Int32
	var blockErrors atomic.Int32

	for i := range numBlocks + 10 {
		wg.Add(3) // One for each read operation

		go func(height int) {
			defer wg.Done()
			block, err := store.ReadBlock(uint64(height))
			if gapHeights[uint64(height)] || height >= numBlocks {
				if err == nil || !errors.Is(err, ErrBlockNotFound) {
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
			_, err := store.ReadBlock(uint64(height))
			if gapHeights[uint64(height)] || height >= numBlocks {
				if err == nil || !errors.Is(err, ErrBlockNotFound) {
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
			_, err := store.ReadBlock(uint64(height))
			if gapHeights[uint64(height)] || height >= numBlocks {
				if err == nil || !errors.Is(err, ErrBlockNotFound) {
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
