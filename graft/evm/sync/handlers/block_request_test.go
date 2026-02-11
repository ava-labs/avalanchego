// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/message/messagetest"
	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers/stats"
	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers/stats/statstest"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
)

type blockRequestTest struct {
	name string

	// starting block, specify either Index or (hash+height)
	startBlockIndex  int
	startBlockHash   common.Hash
	startBlockHeight uint64

	requestedParents  uint16
	expectedBlocks    int
	expectNilResponse bool
	requireResponse   func(t testing.TB, stats *statstest.TestHandlerStats, b []byte)
}

func executeBlockRequestTest(t testing.TB, test blockRequestTest, blocks []*types.Block, c codec.Manager) {
	testHandlerStats := &statstest.TestHandlerStats{}

	// convert into map
	blocksDB := blockMap(blocks)
	blockProvider := &TestBlockProvider{
		GetBlockFn: func(hash common.Hash, height uint64) *types.Block {
			blk, ok := blocksDB[hash]
			if !ok || blk.NumberU64() != height {
				return nil
			}
			return blk
		},
	}
	blockRequestHandler := NewBlockRequestHandler(blockProvider, c, testHandlerStats)

	var blockRequest message.BlockRequest
	if test.startBlockHash != (common.Hash{}) {
		blockRequest.Hash = test.startBlockHash
		blockRequest.Height = test.startBlockHeight
	} else {
		startingBlock := blocks[test.startBlockIndex]
		blockRequest.Hash = startingBlock.Hash()
		blockRequest.Height = startingBlock.NumberU64()
	}
	blockRequest.Parents = test.requestedParents

	responseBytes, err := blockRequestHandler.OnBlockRequest(t.Context(), ids.GenerateTestNodeID(), 1, blockRequest)
	require.NoError(t, err)
	if test.requireResponse != nil {
		test.requireResponse(t, testHandlerStats, responseBytes)
	}

	if test.expectNilResponse {
		require.Nil(t, responseBytes)
		return
	}

	require.NotEmpty(t, responseBytes)

	var response message.BlockResponse
	_, err = c.Unmarshal(responseBytes, &response)
	require.NoError(t, err)
	require.Len(t, response.Blocks, test.expectedBlocks)

	for _, blockBytes := range response.Blocks {
		block := new(types.Block)
		require.NoError(t, rlp.DecodeBytes(blockBytes, block))
		require.GreaterOrEqual(t, test.startBlockIndex, 0)
		require.Equal(t, blocks[test.startBlockIndex].Hash(), block.Hash())
		test.startBlockIndex--
	}
	testHandlerStats.Reset()
}

func TestBlockRequestHandler(t *testing.T) {
	// Generate 97 blocks (genesis + 96).
	blocks := synctest.GenerateTestBlocks(t, 96, nil)
	require.Len(t, blocks, 97)

	tests := []blockRequestTest{
		{
			name:             "handler_returns_blocks_as_requested",
			startBlockIndex:  64,
			requestedParents: 32,
			expectedBlocks:   32,
		},
		{
			name:             "handler_caps_blocks_parent_limit",
			startBlockIndex:  95,
			requestedParents: 96,
			expectedBlocks:   64,
		},
		{
			name:             "handler_handles_genesis",
			startBlockIndex:  0,
			requestedParents: 64,
			expectedBlocks:   1,
		},
		{
			name:              "handler_unknown_block",
			startBlockHash:    common.BytesToHash([]byte("some block pls k thx bye")),
			startBlockHeight:  1_000_000,
			requestedParents:  64,
			expectNilResponse: true,
			requireResponse: func(t testing.TB, testHandlerStats *statstest.TestHandlerStats, _ []byte) {
				require.Equal(t, uint32(1), testHandlerStats.MissingBlockHashCount)
			},
		},
	}
	for _, test := range tests {
		messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()
				executeBlockRequestTest(t, test, blocks, c)
			})
		})
	}
}

func TestBlockRequestHandlerLargeBlocks(t *testing.T) {
	// Generate blocks with varying sizes
	// blocks 1-32: 1MB tx data, blocks 33+: 64KB tx data
	blocks := synctest.GenerateTestBlocks(t, 96, &synctest.BlockGeneratorConfig{
		GasLimit: 500_000_000, // Large gas limit for big txs
		TxDataSizeFunc: func(blockIndex int) int {
			if blockIndex == 0 {
				return 0 // genesis has no tx
			}
			if blockIndex <= 32 {
				return int(units.MiB)
			}
			return int(units.MiB / 16)
		},
	})
	require.Len(t, blocks, 97)

	tests := []blockRequestTest{
		{
			name:             "handler_returns_blocks_as_requested",
			startBlockIndex:  64,
			requestedParents: 10,
			expectedBlocks:   10,
		},
		{
			name:             "handler_caps_blocks_size_limit",
			startBlockIndex:  64,
			requestedParents: 16,
			expectedBlocks:   15,
		},
		{
			name:             "handler_caps_blocks_size_limit_on_first_block",
			startBlockIndex:  32,
			requestedParents: 10,
			expectedBlocks:   1,
		},
	}
	for _, test := range tests {
		messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()
				executeBlockRequestTest(t, test, blocks, c)
			})
		})
	}
}

func TestBlockRequestHandlerCtxExpires(t *testing.T) {
	t.Parallel()

	// Generate 12 blocks (genesis + 11)
	blocks := synctest.GenerateTestBlocks(t, 11, nil)
	require.Len(t, blocks, 12)

	// convert into map
	blocksDB := blockMap(blocks)

	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		cancelAfterNumRequests := 2
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		blockRequestCallCount := 0
		blockProvider := &TestBlockProvider{
			GetBlockFn: func(hash common.Hash, height uint64) *types.Block {
				blockRequestCallCount++
				// cancel ctx after the 2nd call to simulate ctx expiring due to deadline exceeding
				if blockRequestCallCount >= cancelAfterNumRequests {
					cancel()
				}
				blk, ok := blocksDB[hash]
				if !ok || blk.NumberU64() != height {
					return nil
				}
				return blk
			},
		}
		blockRequestHandler := NewBlockRequestHandler(blockProvider, c, stats.NewNoopHandlerStats())

		lastBlock := blocks[len(blocks)-1]
		responseBytes, err := blockRequestHandler.OnBlockRequest(ctx, ids.GenerateTestNodeID(), 1, message.BlockRequest{
			Hash:    lastBlock.Hash(),
			Height:  lastBlock.NumberU64(),
			Parents: uint16(8),
		})
		require.NoError(t, err)
		require.NotEmpty(t, responseBytes)

		var response message.BlockResponse
		_, err = c.Unmarshal(responseBytes, &response)
		require.NoError(t, err)
		// requested 8 blocks, received cancelAfterNumRequests because of timeout
		require.Len(t, response.Blocks, cancelAfterNumRequests)

		for i, blockBytes := range response.Blocks {
			block := new(types.Block)
			require.NoError(t, rlp.DecodeBytes(blockBytes, block))
			require.Equal(t, blocks[len(blocks)-i-1].Hash(), block.Hash())
		}
	})
}

// blockMap creates a map from block hash to block for quick lookups.
func blockMap(blocks []*types.Block) map[common.Hash]*types.Block {
	m := make(map[common.Hash]*types.Block, len(blocks))
	for _, block := range blocks {
		m[block.Hash()] = block
	}
	return m
}
