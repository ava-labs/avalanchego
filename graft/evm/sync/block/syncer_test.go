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
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	handlerstats "github.com/ava-labs/avalanchego/graft/evm/sync/handlers/stats"
)

func TestBlockSyncer_ParameterizedTests(t *testing.T) {
	const defaultFromHeight = uint64(10)

	tests := []struct {
		name              string
		prePopulateBlocks []int
		fromHeight        uint64
		blocksToFetch     uint64
		wantBlocks        []int
	}{
		{
			name:          "normal case - all blocks retrieved from network",
			fromHeight:    5,
			blocksToFetch: 3,
			wantBlocks:    []int{3, 4, 5},
		},
		{
			name:              "all blocks already available",
			prePopulateBlocks: []int{3, 4, 5},
			fromHeight:        5,
			blocksToFetch:     3,
			wantBlocks:        []int{3, 4, 5},
		},
		{
			name:              "some blocks already available",
			prePopulateBlocks: []int{4, 5},
			fromHeight:        5,
			blocksToFetch:     3,
			wantBlocks:        []int{3, 4, 5},
		},
		{
			name:              "most recent block missing",
			prePopulateBlocks: []int{3, 4},
			fromHeight:        5,
			blocksToFetch:     3,
			wantBlocks:        []int{3, 4, 5},
		},
		{
			name:          "edge case - from height 1",
			fromHeight:    1,
			blocksToFetch: 1,
			wantBlocks:    []int{1},
		},
		{
			name:          "single block sync",
			fromHeight:    7,
			blocksToFetch: 1,
			wantBlocks:    []int{7},
		},
		{
			name:          "large sync - many blocks",
			fromHeight:    40,
			blocksToFetch: 35,
			wantBlocks:    []int{6, 10, 20, 30, 40},
		},
		{
			name:          "fetch genesis block",
			fromHeight:    10,
			blocksToFetch: 30,
			wantBlocks:    []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
	}

	for _, tt := range tests {
		messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				numBlocks := int(defaultFromHeight)
				if tt.fromHeight > defaultFromHeight {
					numBlocks = int(tt.fromHeight)
				}

				env := newTestEnvironment(t, numBlocks, c)
				require.NoError(t, env.prePopulateBlocks(tt.prePopulateBlocks))

				syncer, err := env.createSyncer(tt.fromHeight, tt.blocksToFetch)
				require.NoError(t, err)

				require.NoError(t, syncer.Sync(t.Context()))

				env.verifyBlocksInDB(t, tt.wantBlocks)
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

func TestBlockSyncer_NoNetworkRequestsWhenBlocksAlreadyOnDisk(t *testing.T) {
	t.Parallel()

	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		env := newTestEnvironment(t, 10, c)
		require.NoError(t, env.prePopulateBlocks([]int{3, 4, 5}))

		syncer, err := env.createSyncer(5, 3)
		require.NoError(t, err)

		require.NoError(t, syncer.Sync(t.Context()))
		require.Zero(t, env.client.BlocksReceived())
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
		if height < len(e.blocks) {
			// Generated test blocks are indexed by height.
			block := e.blocks[height]
			rawdb.WriteBlock(batch, block)
			rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())
		}
	}
	return batch.Write()
}

// createSyncer creates a block syncer with the given configuration
func (e *testEnvironment) createSyncer(fromHeight uint64, blocksToFetch uint64) (*Syncer, error) {
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
func (e *testEnvironment) verifyBlocksInDB(t *testing.T, wantBlockHeights []int) {
	t.Helper()

	// Verify expected blocks are present
	for _, height := range wantBlockHeights {
		if height >= len(e.blocks) {
			continue
		}
		block := e.blocks[height]
		dbBlock := rawdb.ReadBlock(e.chainDB, block.Hash(), block.NumberU64())
		require.NotNil(t, dbBlock, "Block %d should be in database", height)
		require.Equal(t, block.Hash(), dbBlock.Hash(), "Block %d hash mismatch", height)
	}
}

// testSyncTarget is a minimal message.Syncable double for UpdateTarget tests.
var _ message.Syncable = (*testSyncTarget)(nil)

type testSyncTarget struct {
	hash   common.Hash
	root   common.Hash
	height uint64
}

func (s *testSyncTarget) GetBlockHash() common.Hash                         { return s.hash }
func (s *testSyncTarget) GetBlockRoot() common.Hash                         { return s.root }
func (s *testSyncTarget) ID() ids.ID                                        { return ids.ID(s.hash) }
func (s *testSyncTarget) Height() uint64                                    { return s.height }
func (s *testSyncTarget) Bytes() []byte                                     { return s.hash.Bytes() }
func (*testSyncTarget) Accept(context.Context) (block.StateSyncMode, error) { return 0, nil }

func TestUpdateTarget_Monotonic(t *testing.T) {
	t.Parallel()

	syncer, err := NewSyncer(nil, rawdb.NewMemoryDatabase(), common.HexToHash("0x1"), 100, 10)
	require.NoError(t, err)

	// At or below fromHeight: ignored.
	require.NoError(t, syncer.UpdateTarget(&testSyncTarget{hash: common.HexToHash("0xa"), height: 100}))
	require.Nil(t, syncer.latestTarget)

	// Higher: accepted.
	require.NoError(t, syncer.UpdateTarget(&testSyncTarget{hash: common.HexToHash("0xb"), height: 200}))
	require.Equal(t, uint64(200), syncer.latestTarget.height)

	// Stale or equal: ignored.
	require.NoError(t, syncer.UpdateTarget(&testSyncTarget{hash: common.HexToHash("0xc"), height: 150}))
	require.NoError(t, syncer.UpdateTarget(&testSyncTarget{hash: common.HexToHash("0xd"), height: 200}))
	require.Equal(t, common.HexToHash("0xb"), syncer.latestTarget.hash)
}

func TestSync_CatchUpBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		fromHeight    uint64
		blocksToFetch uint64
		updateHeight  uint64
		wantBlocks    []int
		absentBlocks  []int
	}{
		{
			name:          "drift exceeds window - catch-up runs",
			fromHeight:    20,
			blocksToFetch: 5,
			updateHeight:  40,
			wantBlocks:    []int{16, 17, 18, 19, 20, 36, 37, 38, 39, 40},
		},
		{
			name:          "drift within window - no catch-up",
			fromHeight:    10,
			blocksToFetch: 5,
			updateHeight:  15,
			wantBlocks:    []int{6, 7, 8, 9, 10},
			absentBlocks:  []int{15},
		},
	}

	for _, tt := range tests {
		messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				env := newTestEnvironment(t, int(tt.updateHeight), c)
				syncer, err := env.createSyncer(tt.fromHeight, tt.blocksToFetch)
				require.NoError(t, err)

				require.NoError(t, syncer.UpdateTarget(&testSyncTarget{
					hash:   env.blocks[tt.updateHeight].Hash(),
					height: tt.updateHeight,
				}))
				require.NoError(t, syncer.Sync(t.Context()))

				env.verifyBlocksInDB(t, tt.wantBlocks)
				for _, h := range tt.absentBlocks {
					dbBlock := rawdb.ReadBlock(env.chainDB, env.blocks[h].Hash(), uint64(h))
					require.Nil(t, dbBlock, "block %d should not be fetched", h)
				}
			})
		})
	}
}
