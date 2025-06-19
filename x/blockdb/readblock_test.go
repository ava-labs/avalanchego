package blockdb

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
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
			},
			wantErr: ErrInvalidBlockHeight,
		},
		{
			name:       "height causes overflow",
			readHeight: math.MaxUint64,
			wantErr:    ErrInvalidBlockHeight,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config
			if config == nil {
				defaultConfig := DefaultDatabaseConfig()
				config = &defaultConfig
			}

			store, cleanup := newTestDatabase(t, false, tt.config)
			defer cleanup()

			// Seed database with blocks based on config
			seededBlocks := make(map[uint64][]byte)
			if tt.wantErr == nil {
				minHeight := config.MinimumHeight
				maxHeight := minHeight + 50 // Always write 51 blocks
				gapHeight := minHeight + 40 // Gap at relative position 40

				for i := minHeight; i <= maxHeight; i++ {
					if i == gapHeight {
						continue // Create gap
					}

					block := randomBlock(t)
					err := store.WriteBlock(i, block, uint16(i-minHeight))
					require.NoError(t, err)
					seededBlocks[i] = block
				}
			}

			if tt.setup != nil {
				tt.setup(store)
			}
			readBlock, err := store.ReadBlock(tt.readHeight)
			readHeader, err := store.ReadHeader(tt.readHeight)
			readBody, err := store.ReadBody(tt.readHeight)

			if tt.wantErr != nil {
				require.Error(t, err)
				require.True(t, errors.Is(err, tt.wantErr))
				return
			}

			// Handle success cases
			require.NoError(t, err)
			if tt.noBlock {
				require.Nil(t, readBlock)
				require.Nil(t, readHeader)
				require.Nil(t, readBody)
			} else {
				require.NotNil(t, readBlock)
				expectedBlock := seededBlocks[tt.readHeight]
				headerSize := uint16(tt.readHeight - config.MinimumHeight)
				var expectHeader []byte
				if headerSize > 0 {
					expectHeader = expectedBlock[:headerSize]
				}
				assert.Equal(t, expectedBlock, readBlock)
				assert.Equal(t, expectHeader, readHeader)
				assert.Equal(t, expectedBlock[headerSize:], readBody)
			}
		})
	}
}

func TestReadOperations_Concurrency(t *testing.T) {
	store, cleanup := newTestDatabase(t, false, nil)
	defer cleanup()

	// Pre-generate blocks and write them
	numBlocks := 50
	blocks := make([][]byte, numBlocks)
	headerSizes := make([]uint16, numBlocks)
	gapHeights := map[uint64]bool{
		10: true,
		20: true,
	}

	for i := range numBlocks {
		if gapHeights[uint64(i)] {
			continue
		}

		blocks[i] = randomBlock(t)
		headerSizes[i] = uint16(i * 10) // Varying header sizes
		if headerSizes[i] > uint16(len(blocks[i])) {
			headerSizes[i] = uint16(len(blocks[i])) / 2
		}

		err := store.WriteBlock(uint64(i), blocks[i], headerSizes[i])
		require.NoError(t, err)
	}

	var wg sync.WaitGroup
	var errors atomic.Int32
	for i := range numBlocks + 10 {
		wg.Add(3) // One for each read operation

		go func(height int) {
			defer wg.Done()
			block, err := store.ReadBlock(uint64(height))
			if err != nil {
				t.Errorf("ReadBlock failed for height %d: %v", height, err)
				errors.Add(1)
				return
			}
			if gapHeights[uint64(height)] || height >= numBlocks {
				if block != nil {
					t.Errorf("Expected nil block for height %d", height)
					errors.Add(1)
				}
			} else {
				if !assert.Equal(t, blocks[height], block) {
					t.Errorf("ReadBlock data mismatch at height %d", height)
					errors.Add(1)
				}
			}
		}(i)

		go func(height int) {
			defer wg.Done()
			header, err := store.ReadHeader(uint64(height))
			if err != nil {
				t.Errorf("ReadHeader failed for height %d: %v", height, err)
				errors.Add(1)
				return
			}
			if gapHeights[uint64(height)] || height >= numBlocks {
				if header != nil {
					t.Errorf("Expected nil header for height %d", height)
					errors.Add(1)
				}
			} else {
				expectedHeader := blocks[height][:headerSizes[height]]
				if headerSizes[height] == 0 {
					expectedHeader = nil
				}
				if !assert.Equal(t, expectedHeader, header) {
					t.Errorf("ReadHeader data mismatch at height %d", height)
					errors.Add(1)
				}
			}
		}(i)

		go func(height int) {
			defer wg.Done()
			body, err := store.ReadBody(uint64(height))
			if err != nil {
				t.Errorf("ReadBody failed for height %d: %v", height, err)
				errors.Add(1)
				return
			}
			if gapHeights[uint64(height)] || height >= numBlocks {
				if body != nil {
					t.Errorf("Expected nil body for height %d", height)
					errors.Add(1)
				}
			} else {
				expectedBody := blocks[height][headerSizes[height]:]
				if !assert.Equal(t, expectedBody, body) {
					t.Errorf("ReadBody data mismatch at height %d", height)
					errors.Add(1)
				}
			}
		}(i)
	}
	wg.Wait()
	require.Zero(t, errors.Load(), "concurrent read operations had errors")
}
