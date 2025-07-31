// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmanmock"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blockmock"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/components/chain"
)

var (
	blkBytes1 = []byte{1}
	blkBytes2 = []byte{2}

	blkID0 = ids.ID{0}
	blkID1 = ids.ID{1}
	blkID2 = ids.ID{2}

	time1 = time.Unix(1, 0)
	time2 = time.Unix(2, 0)
)

func batchedParseBlockCachingTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "batchedParseBlockCachingTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	vm := blockmock.NewChainVM(ctrl)

	if loadExpectations {
		blk1 := snowmanmock.NewBlock(ctrl)
		blk2 := snowmanmock.NewBlock(ctrl)
		gomock.InOrder(
			// Initialize
			vm.EXPECT().Initialize(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				gomock.Any(), gomock.Any(), gomock.Any(),
				gomock.Any(),
			).Return(nil).Times(1),
			vm.EXPECT().LastAccepted(gomock.Any()).Return(preSummaryBlk.ID(), nil).Times(1),
			vm.EXPECT().GetBlock(gomock.Any(), gomock.Any()).Return(preSummaryBlk, nil).Times(1),

			// Parse Block 1
			vm.EXPECT().ParseBlock(gomock.Any(), blkBytes1).Return(blk1, nil).Times(1),
			blk1.EXPECT().ID().Return(blkID1).Times(1),
			blk1.EXPECT().Parent().Return(blkID0).Times(1),
			blk1.EXPECT().Height().Return(uint64(1)).Times(1),
			blk1.EXPECT().Timestamp().Return(time1).Times(1),

			// Parse Block 2
			vm.EXPECT().ParseBlock(gomock.Any(), blkBytes2).Return(blk2, nil).Times(1),
			blk2.EXPECT().ID().Return(blkID2).Times(1),
			blk2.EXPECT().Parent().Return(blkID1).Times(1),
			blk2.EXPECT().Height().Return(uint64(2)).Times(1),
			blk2.EXPECT().Timestamp().Return(time2).Times(1),
		)
	}

	return vm
}

func TestBatchedParseBlockCaching(t *testing.T) {
	require := require.New(t)
	testKey := batchedParseBlockCachingTestKey

	// Create and start the plugin
	vm := buildClientHelper(require, testKey)
	defer vm.runtime.Stop(context.Background())

	ctx := snowtest.Context(t, snowtest.CChainID)

	require.NoError(vm.Initialize(context.Background(), ctx, memdb.New(), nil, nil, nil, nil, nil))

	// Call should parse the first block
	blk, err := vm.ParseBlock(context.Background(), blkBytes1)
	require.NoError(err)
	require.Equal(blkID1, blk.ID())

	require.IsType(&chain.BlockWrapper{}, blk)

	// Call should cache the first block and parse the second block
	blks, err := vm.BatchedParseBlock(context.Background(), [][]byte{blkBytes1, blkBytes2})
	require.NoError(err)
	require.Len(blks, 2)
	require.Equal(blkID1, blks[0].ID())
	require.Equal(blkID2, blks[1].ID())

	require.IsType(&chain.BlockWrapper{}, blks[0])
	require.IsType(&chain.BlockWrapper{}, blks[1])

	// Call should be fully cached and not result in a grpc call
	blks, err = vm.BatchedParseBlock(context.Background(), [][]byte{blkBytes1, blkBytes2})
	require.NoError(err)
	require.Len(blks, 2)
	require.Equal(blkID1, blks[0].ID())
	require.Equal(blkID2, blks[1].ID())

	require.IsType(&chain.BlockWrapper{}, blks[0])
	require.IsType(&chain.BlockWrapper{}, blks[1])
}
