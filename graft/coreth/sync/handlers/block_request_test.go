// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
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
	assertResponse    func(t testing.TB, stats *stats.MockHandlerStats, b []byte)
}

func executeBlockRequestTest(t testing.TB, test blockRequestTest, blocks []*types.Block) {
	mockHandlerStats := &stats.MockHandlerStats{}

	// convert into map
	blocksDB := make(map[common.Hash]*types.Block, len(blocks))
	for _, blk := range blocks {
		blocksDB[blk.Hash()] = blk
	}
	blockProvider := &TestBlockProvider{
		GetBlockFn: func(hash common.Hash, height uint64) *types.Block {
			blk, ok := blocksDB[hash]
			if !ok || blk.NumberU64() != height {
				return nil
			}
			return blk
		},
	}
	blockRequestHandler := NewBlockRequestHandler(blockProvider, message.Codec, mockHandlerStats)

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

	responseBytes, err := blockRequestHandler.OnBlockRequest(context.Background(), ids.GenerateTestNodeID(), 1, blockRequest)
	if err != nil {
		t.Fatal("unexpected error during block request", err)
	}
	if test.assertResponse != nil {
		test.assertResponse(t, mockHandlerStats, responseBytes)
	}

	if test.expectNilResponse {
		assert.Nil(t, responseBytes)
		return
	}

	assert.NotEmpty(t, responseBytes)

	var response message.BlockResponse
	if _, err = message.Codec.Unmarshal(responseBytes, &response); err != nil {
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
	mockHandlerStats.Reset()
}

func TestBlockRequestHandler(t *testing.T) {
	var gspec = &core.Genesis{
		Config: params.TestChainConfig,
	}
	memdb := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(memdb, nil)
	genesis := gspec.MustCommit(memdb, tdb)
	engine := dummy.NewETHFaker()
	blocks, _, err := core.GenerateChain(params.TestChainConfig, genesis, engine, memdb, 96, 0, func(i int, b *core.BlockGen) {})
	if err != nil {
		t.Fatal("unexpected error when generating test blockchain", err)
	}
	assert.Len(t, blocks, 96)

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
			assertResponse: func(t testing.TB, mockHandlerStats *stats.MockHandlerStats, _ []byte) {
				assert.Equal(t, uint32(1), mockHandlerStats.MissingBlockHashCount)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executeBlockRequestTest(t, test, blocks)
		})
	}
}

func TestBlockRequestHandlerLargeBlocks(t *testing.T) {
	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		funds   = big.NewInt(1000000000000000000)
		gspec   = &core.Genesis{
			Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
			Alloc:  types.GenesisAlloc{addr1: {Balance: funds}},
		}
		signer = types.LatestSigner(gspec.Config)
	)
	memdb := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(memdb, nil)
	genesis := gspec.MustCommit(memdb, tdb)
	engine := dummy.NewETHFaker()
	blocks, _, err := core.GenerateChain(gspec.Config, genesis, engine, memdb, 96, 0, func(i int, b *core.BlockGen) {
		var data []byte
		switch {
		case i <= 32:
			data = make([]byte, units.MiB)
		default:
			data = make([]byte, units.MiB/16)
		}
		tx, err := types.SignTx(types.NewTransaction(b.TxNonce(addr1), addr1, big.NewInt(10000), 4_215_304, nil, data), signer, key1)
		if err != nil {
			t.Fatal(err)
		}
		b.AddTx(tx)
	})
	if err != nil {
		t.Fatal("unexpected error when generating test blockchain", err)
	}
	assert.Len(t, blocks, 96)

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
		t.Run(test.name, func(t *testing.T) {
			executeBlockRequestTest(t, test, blocks)
		})
	}
}

func TestBlockRequestHandlerCtxExpires(t *testing.T) {
	var gspec = &core.Genesis{
		Config: params.TestChainConfig,
	}
	memdb := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(memdb, nil)
	genesis := gspec.MustCommit(memdb, tdb)
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

	cancelAfterNumRequests := 2
	ctx, cancel := context.WithCancel(context.Background())
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
	blockRequestHandler := NewBlockRequestHandler(blockProvider, message.Codec, stats.NewNoopHandlerStats())

	responseBytes, err := blockRequestHandler.OnBlockRequest(ctx, ids.GenerateTestNodeID(), 1, message.BlockRequest{
		Hash:    blocks[10].Hash(),
		Height:  blocks[10].NumberU64(),
		Parents: uint16(8),
	})
	if err != nil {
		t.Fatal("unexpected error from BlockRequestHandler", err)
	}
	assert.NotEmpty(t, responseBytes)

	var response message.BlockResponse
	if _, err = message.Codec.Unmarshal(responseBytes, &response); err != nil {
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
