// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/statesync/handlers/stats"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
)

func TestBlockRequestHandler(t *testing.T) {
	var gspec = &core.Genesis{
		Config: params.TestChainConfig,
	}
	memdb := memorydb.New()
	genesis := gspec.MustCommit(memdb)
	engine := dummy.NewETHFaker()
	blocks, _, err := core.GenerateChain(params.TestChainConfig, genesis, engine, memdb, 96, 0, func(i int, b *core.BlockGen) {})
	if err != nil {
		t.Fatal("unexpected error when generating test blockchain", err)
	}

	assert.Len(t, blocks, 96)

	// convert into map
	blocksDB := make(map[common.Hash]*types.Block, len(blocks))
	for _, blk := range blocks {
		blocksDB[blk.Hash()] = blk
	}

	codec, err := message.BuildCodec()
	if err != nil {
		t.Fatal("error building codec", err)
	}

	blockRequestHandler := NewBlockRequestHandler(func(hash common.Hash, height uint64) *types.Block {
		blk, ok := blocksDB[hash]
		if !ok || blk.NumberU64() != height {
			return nil
		}
		return blk
	}, codec, stats.NewNoopHandlerStats())

	tests := []struct {
		name string

		// starting block, specify either Index or (hash+height)
		startBlockIndex  int
		startBlockHash   common.Hash
		startBlockHeight uint64

		requestedParents  uint16
		expectedBlocks    int
		expectNilResponse bool
	}{
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
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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

			responseBytes, err := blockRequestHandler.OnBlockRequest(context.Background(), ids.GenerateTestShortID(), 1, blockRequest)
			if err != nil {
				t.Fatal("unexpected error during block request", err)
			}

			if test.expectNilResponse {
				assert.Nil(t, responseBytes)
				return
			}

			assert.NotEmpty(t, responseBytes)

			var response message.BlockResponse
			if _, err = codec.Unmarshal(responseBytes, &response); err != nil {
				t.Fatal("error unmarshalling", err)
			}
			assert.Len(t, response.Blocks, test.expectedBlocks)

			for _, blockBytes := range response.Blocks {
				block := new(types.Block)
				if err := rlp.DecodeBytes(blockBytes, block); err != nil {
					t.Fatal("could not parse block", err)
				}
				assert.GreaterOrEqual(t, test.startBlockIndex, 0)
				assert.Equal(t, blocks[test.startBlockIndex].Hash(), block.Hash())
				test.startBlockIndex--
			}
		})
	}
}

func TestBlockRequestHandlerCtxExpires(t *testing.T) {
	var gspec = &core.Genesis{
		Config: params.TestChainConfig,
	}
	memdb := memorydb.New()
	genesis := gspec.MustCommit(memdb)
	engine := dummy.NewETHFaker()
	blocks, _, err := core.GenerateChain(params.TestChainConfig, genesis, engine, memdb, 11, 0, func(i int, b *core.BlockGen) {})
	if err != nil {
		t.Fatal("unexpected error when generating test blockchain", err)
	}

	assert.Len(t, blocks, 11)

	// convert into map
	blocksDB := make(map[common.Hash]*types.Block, 11)
	for _, blk := range blocks {
		blocksDB[blk.Hash()] = blk
	}

	codec, err := message.BuildCodec()
	if err != nil {
		t.Fatal("error building codec", err)
	}

	cancelAfterNumRequests := 2
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	blockRequestCallCount := 0
	blockRequestHandler := NewBlockRequestHandler(func(hash common.Hash, height uint64) *types.Block {
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
	}, codec, stats.NewNoopHandlerStats())

	responseBytes, err := blockRequestHandler.OnBlockRequest(ctx, ids.GenerateTestShortID(), 1, message.BlockRequest{
		Hash:    blocks[10].Hash(),
		Height:  blocks[10].NumberU64(),
		Parents: uint16(8),
	})
	if err != nil {
		t.Fatal("unexpected error from BlockRequestHandler", err)
	}
	assert.NotEmpty(t, responseBytes)

	var response message.BlockResponse
	if _, err = codec.Unmarshal(responseBytes, &response); err != nil {
		t.Fatal("error unmarshalling", err)
	}
	// requested 8 blocks, received cancelAfterNumRequests because of timeout
	assert.Len(t, response.Blocks, cancelAfterNumRequests)

	for i, blockBytes := range response.Blocks {
		block := new(types.Block)
		if err := rlp.DecodeBytes(blockBytes, block); err != nil {
			t.Fatal("could not parse block", err)
		}
		assert.Equal(t, blocks[len(blocks)-i-1].Hash(), block.Hash())
	}
}
