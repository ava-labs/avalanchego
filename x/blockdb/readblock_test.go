// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"bytes"
	"encoding/binary"
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
		setup      func(db *Database) error
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
				MaxDataFiles:       DefaultMaxDataFiles,
				BlockCacheSize:     DefaultBlockCacheSize,
			},
		},
		{
			name:       "database closed",
			readHeight: 1,
			setup: func(db *Database) error {
				return db.Close()
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
				MaxDataFiles:       DefaultMaxDataFiles,
				BlockCacheSize:     DefaultBlockCacheSize,
			},
			wantErr: ErrInvalidBlockHeight,
		},
		{
			name:       "corrupted index points to wrong block",
			readHeight: 1,
			setup: func(db *Database) error {
				entry, err := db.readIndexEntry(0)
				if err != nil {
					return err
				}
				offset, err := db.indexEntryOffset(1)
				if err != nil {
					return err
				}
				return db.writeIndexEntryAt(offset, entry.Offset, entry.CompressedSize)
			},
			wantErr: ErrCorrupted,
		},
		{
			name:       "index entry compressed size differs from block header",
			readHeight: 0,
			setup: func(db *Database) error {
				entry, err := db.readIndexEntry(0)
				if err != nil {
					return err
				}
				offset, err := db.indexEntryOffset(0)
				if err != nil {
					return err
				}
				return db.writeIndexEntryAt(offset, entry.Offset, entry.CompressedSize+1)
			},
			wantErr: ErrCorrupted,
		},
		{
			name:       "corrupted checksum in block header",
			readHeight: 0,
			setup: func(db *Database) error {
				entry, err := db.readIndexEntry(0)
				if err != nil {
					return err
				}
				dataFile, localOffset, _, err := db.getDataFileAndOffset(entry.Offset)
				if err != nil {
					return err
				}
				// 12 is the byte offset of the Checksum field within the
				// serialized blockEntryHeader (8B Height + 4B CompressedSize).
				const checksumOffset = 12
				var bad [8]byte
				binary.LittleEndian.PutUint64(bad[:], 0xDEADBEEF)
				_, err = dataFile.WriteAt(bad[:], int64(localOffset)+checksumOffset)
				return err
			},
			wantErr: ErrCorrupted,
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
				require.NoError(t, tt.setup(store))
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

	const goroutinesPerHeight = 3
	var wg sync.WaitGroup
	var errorCount atomic.Int32
	var blockErrors atomic.Int32

	for i := range numBlocks + 10 {
		for range goroutinesPerHeight {
			wg.Add(1)
			go func(height int) {
				defer wg.Done()
				block, err := store.Get(uint64(height))
				if gapHeights[uint64(height)] || height >= numBlocks {
					if !errors.Is(err, database.ErrNotFound) {
						errorCount.Add(1)
					}
					return
				}
				if err != nil {
					errorCount.Add(1)
					return
				}
				if !bytes.Equal(blocks[height], block) {
					blockErrors.Add(1)
				}
			}(i)
		}
	}
	wg.Wait()

	require.Zero(t, errorCount.Load(), "concurrent read operations had errors")
	require.Zero(t, blockErrors.Load(), "block data mismatches detected")
}

func TestHasBlock(t *testing.T) {
	minHeight := uint64(10)
	blocksCount := uint64(10)
	gapHeight := minHeight + 5

	tests := []struct {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newDatabase(t, DefaultConfig().WithMinimumHeight(minHeight))

			for i := minHeight; i <= minHeight+blocksCount; i++ {
				if i == gapHeight {
					continue
				}
				require.NoError(t, store.Put(i, randomBlock(t)))
			}

			if tt.dbClosed {
				require.NoError(t, store.Close())
			}

			has, err := store.Has(tt.height)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, has)
			}
		})
	}
}
