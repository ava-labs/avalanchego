// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocksync

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers"

	syncclient "github.com/ava-labs/coreth/sync/client"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	ethparams "github.com/ava-labs/libevm/params"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	params.RegisterExtras()
	os.Exit(m.Run())
}

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
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnvironment(t, tt.numBlocks)
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
	}
}

func TestBlockSyncer_ContextCancellation(t *testing.T) {
	env := newTestEnvironment(t, 10)
	syncer, err := env.createSyncer(5, 3)
	require.NoError(t, err)

	// Immediately cancel the context to simulate cancellation.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = syncer.Sync(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// testEnvironment provides an abstraction for setting up block syncer tests
type testEnvironment struct {
	chainDB ethdb.Database
	client  *syncclient.TestClient
	blocks  []*types.Block
}

// newTestEnvironment creates a new test environment with generated blocks
func newTestEnvironment(t *testing.T, numBlocks int) *testEnvironment {
	t.Helper()

	var (
		key, _         = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		addr           = crypto.PubkeyToAddress(key.PublicKey)
		genesisBalance = big.NewInt(1000000000)
		signer         = types.HomesteadSigner{}
	)

	// Ensure that key has some funds in the genesis block.
	gspec := &core.Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr: {Balance: genesisBalance}},
	}
	engine := dummy.NewETHFaker()

	_, blocks, _, err := core.GenerateChainWithGenesis(gspec, engine, numBlocks, 0, func(_ int, gen *core.BlockGen) {
		// Generate a transaction to create a unique block
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr), addr, big.NewInt(10), ethparams.TxGas, nil, nil), signer, key)
		gen.AddTx(tx)
	})
	require.NoError(t, err)

	// The genesis block is not include in the blocks slice, so we need to prepend it
	blocks = append([]*types.Block{gspec.ToBlock()}, blocks...)

	blockProvider := &handlers.TestBlockProvider{GetBlockFn: func(hash common.Hash, height uint64) *types.Block {
		if height >= uint64(len(blocks)) {
			return nil
		}
		block := blocks[height]
		if block.Hash() != hash {
			return nil
		}
		return block
	}}

	blockHandler := handlers.NewBlockRequestHandler(
		blockProvider,
		message.Codec,
		handlerstats.NewNoopHandlerStats(),
	)

	return &testEnvironment{
		chainDB: rawdb.NewMemoryDatabase(),
		blocks:  blocks,
		client: syncclient.NewTestClient(
			message.Codec,
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
		if height > len(e.blocks) {
			continue
		}
		block := e.blocks[height]
		dbBlock := rawdb.ReadBlock(e.chainDB, block.Hash(), block.NumberU64())
		require.NotNil(t, dbBlock, "Block %d should be in database", height)
		require.Equal(t, block.Hash(), dbBlock.Hash(), "Block %d hash mismatch", height)
	}
}
