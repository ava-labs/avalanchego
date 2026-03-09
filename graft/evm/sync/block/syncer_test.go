// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/message/messagetest"
	"github.com/ava-labs/avalanchego/graft/evm/sync/client"
	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"

	handlerstats "github.com/ava-labs/avalanchego/graft/evm/sync/handlers/stats"
)

func TestBlockSyncer_ParameterizedTests(t *testing.T) {
	tests := []struct {
		name                     string
		numBlocks                int
		prePopulateBlocks        []int
		fromHeight               uint64
		blocksToFetch            uint64
		expectedBlocks           []int
		verifyZeroBlocksReceived bool
	}{
		{
			name:           "normal case - all blocks retrieved from network",
			numBlocks:      10,
			fromHeight:     5,
			blocksToFetch:  3,
			expectedBlocks: []int{3, 4, 5},
		},
		{
			name:                     "all blocks already available",
			numBlocks:                10,
			prePopulateBlocks:        []int{3, 4, 5},
			fromHeight:               5,
			blocksToFetch:            3,
			expectedBlocks:           []int{3, 4, 5},
			verifyZeroBlocksReceived: true,
		},
		{
			name:              "some blocks already available",
			numBlocks:         10,
			prePopulateBlocks: []int{4, 5},
			fromHeight:        5,
			blocksToFetch:     3,
			expectedBlocks:    []int{3, 4, 5},
		},
		{
			name:              "most recent block missing",
			numBlocks:         10,
			prePopulateBlocks: []int{3, 4},
			fromHeight:        5,
			blocksToFetch:     3,
			expectedBlocks:    []int{3, 4, 5},
		},
		{
			name:           "edge case - from height 1",
			numBlocks:      10,
			fromHeight:     1,
			blocksToFetch:  1,
			expectedBlocks: []int{1},
		},
		{
			name:           "single block sync",
			numBlocks:      10,
			fromHeight:     7,
			blocksToFetch:  1,
			expectedBlocks: []int{7},
		},
		{
			name:           "large sync - many blocks",
			numBlocks:      50,
			fromHeight:     40,
			blocksToFetch:  35,
			expectedBlocks: []int{6, 10, 20, 30, 40},
		},
		{
			name:           "fetch genesis block",
			numBlocks:      10,
			fromHeight:     10,
			blocksToFetch:  30,
			expectedBlocks: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
	}

	for _, tt := range tests {
		messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				env := newTestEnvironment(t, tt.numBlocks, c)
				require.NoError(t, env.prePopulateBlocks(tt.prePopulateBlocks))

				syncer, err := env.createSyncer(tt.fromHeight, tt.blocksToFetch)
				require.NoError(t, err)

				require.NoError(t, syncer.Sync(t.Context()))

				env.verifyBlocksInDB(t, tt.expectedBlocks)

				if tt.verifyZeroBlocksReceived {
					// Client should not have received any block requests since all blocks were on disk
					require.Zero(t, env.client.BlocksReceived())
				}
			})
		})
	}
}

func TestBlockSyncer_ContextCancellation(t *testing.T) {
	t.Parallel()

	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		env := newTestEnvironment(t, 10, c)
		syncer, err := env.createSyncer(5, 3)
		require.NoError(t, err)

		// Immediately cancel the context to simulate cancellation.
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err = syncer.Sync(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})
}

// testEnvironment provides an abstraction for setting up block syncer tests
type testEnvironment struct {
	chainDB ethdb.Database
	client  *client.TestClient
	blocks  []*types.Block
}

// newTestEnvironment creates a new test environment with generated blocks
func newTestEnvironment(t *testing.T, numBlocks int, c codec.Manager) *testEnvironment {
	t.Helper()

	blocks := synctest.GenerateTestBlocks(t, numBlocks, nil)

	blockProvider := &handlers.TestBlockProvider{
		GetBlockFn: func(hash common.Hash, height uint64) *types.Block {
			if height >= uint64(len(blocks)) {
				return nil
			}
			block := blocks[height]
			if block.Hash() != hash {
				return nil
			}
			return block
		},
	}

	blockHandler := handlers.NewBlockRequestHandler(
		blockProvider,
		c,
		handlerstats.NewNoopHandlerStats(),
	)

	return &testEnvironment{
		chainDB: rawdb.NewMemoryDatabase(),
		blocks:  blocks,
		client: client.NewTestClient(
			c,
			nil,
			nil,
			blockHandler,
		),
	}
}

// prePopulateBlocks writes some blocks to the database before syncing (by block height)
func (e *testEnvironment) prePopulateBlocks(blockHeights []int) error {
	batch := e.chainDB.NewBatch()
	for _, height := range blockHeights {
		if height <= len(e.blocks) {
			// blocks[0] is block number 1, blocks[1] is block number 2, etc.
			block := e.blocks[height]
			rawdb.WriteBlock(batch, block)
			rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())
		}
	}
	return batch.Write()
}

// createSyncer creates a block syncer with the given configuration
func (e *testEnvironment) createSyncer(fromHeight uint64, blocksToFetch uint64) (*BlockSyncer, error) {
	if fromHeight > uint64(len(e.blocks)) {
		return nil, fmt.Errorf("fromHeight %d exceeds available blocks %d", fromHeight, len(e.blocks))
	}

	return NewSyncer(
		e.client,
		e.chainDB,
		e.blocks[fromHeight].Hash(),
		fromHeight,
		blocksToFetch,
	)
}

// verifyBlocksInDB checks that the expected blocks are present in the database (by block height)
func (e *testEnvironment) verifyBlocksInDB(t *testing.T, expectedBlockHeights []int) {
	t.Helper()

	// Verify expected blocks are present
	for _, height := range expectedBlockHeights {
		if height >= len(e.blocks) {
			continue
		}
		block := e.blocks[height]
		dbBlock := rawdb.ReadBlock(e.chainDB, block.Hash(), block.NumberU64())
		require.NotNil(t, dbBlock, "Block %d should be in database", height)
		require.Equal(t, block.Hash(), dbBlock.Hash(), "Block %d hash mismatch", height)
	}
}
