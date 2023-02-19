// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/mocks"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/chain"
)

var (
	blkBytes1 = []byte{1}
	blkBytes2 = []byte{2}

	blkID0 = ids.ID{0}
	blkID1 = ids.ID{1}
	blkID2 = ids.ID{2}

	status1 = choices.Accepted
	status2 = choices.Processing

	time1 = time.Unix(1, 0)
	time2 = time.Unix(2, 0)
)

func batchedParseBlockCachingTestPlugin(t *testing.T, loadExpectations bool) (block.ChainVM, *gomock.Controller) {
	// test key is "batchedParseBlockCachingTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	vm := mocks.NewMockChainVM(ctrl)

	if loadExpectations {
		blk1 := snowman.NewMockBlock(ctrl)
		blk2 := snowman.NewMockBlock(ctrl)
		gomock.InOrder(
			// Initialize
			vm.EXPECT().Initialize(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				gomock.Any(),
			).Return(nil).Times(1),
			vm.EXPECT().LastAccepted(gomock.Any()).Return(preSummaryBlk.ID(), nil).Times(1),
			vm.EXPECT().GetBlock(gomock.Any(), gomock.Any()).Return(preSummaryBlk, nil).Times(1),

			// Parse Block 1
			vm.EXPECT().ParseBlock(gomock.Any(), blkBytes1).Return(blk1, nil).Times(1),
			blk1.EXPECT().ID().Return(blkID1).Times(1),
			blk1.EXPECT().Parent().Return(blkID0).Times(1),
			blk1.EXPECT().Status().Return(status1).Times(1),
			blk1.EXPECT().Height().Return(uint64(1)).Times(1),
			blk1.EXPECT().Timestamp().Return(time1).Times(1),

			// Parse Block 2
			vm.EXPECT().ParseBlock(gomock.Any(), blkBytes2).Return(blk2, nil).Times(1),
			blk2.EXPECT().ID().Return(blkID2).Times(1),
			blk2.EXPECT().Parent().Return(blkID1).Times(1),
			blk2.EXPECT().Status().Return(status2).Times(1),
			blk2.EXPECT().Height().Return(uint64(2)).Times(1),
			blk2.EXPECT().Timestamp().Return(time2).Times(1),
		)
	}

	return vm, ctrl
}

func TestBatchedParseBlockCaching(t *testing.T) {
	require := require.New(t)
	testKey := batchedParseBlockCachingTestKey

	// Create and start the plugin
	vm, stopper := buildClientHelper(require, testKey)
	defer stopper.Stop(context.Background())

	ctx := snow.DefaultContextTest()
	dbManager := manager.NewMemDB(version.Semantic1_0_0)

	err := vm.Initialize(context.Background(), ctx, dbManager, nil, nil, nil, nil, nil, nil)
	require.NoError(err)

	// Call should parse the first block
	blk, err := vm.ParseBlock(context.Background(), blkBytes1)
	require.NoError(err)
	require.Equal(blkID1, blk.ID())

	_, typeChecked := blk.(*chain.BlockWrapper)
	require.True(typeChecked)

	// Call should cache the first block and parse the second block
	blks, err := vm.BatchedParseBlock(context.Background(), [][]byte{blkBytes1, blkBytes2})
	require.NoError(err)
	require.Len(blks, 2)
	require.Equal(blkID1, blks[0].ID())
	require.Equal(blkID2, blks[1].ID())

	_, typeChecked = blks[0].(*chain.BlockWrapper)
	require.True(typeChecked)

	_, typeChecked = blks[1].(*chain.BlockWrapper)
	require.True(typeChecked)

	// Call should be fully cached and not result in a grpc call
	blks, err = vm.BatchedParseBlock(context.Background(), [][]byte{blkBytes1, blkBytes2})
	require.NoError(err)
	require.Len(blks, 2)
	require.Equal(blkID1, blks[0].ID())
	require.Equal(blkID2, blks[1].ID())

	_, typeChecked = blks[0].(*chain.BlockWrapper)
	require.True(typeChecked)

	_, typeChecked = blks[1].(*chain.BlockWrapper)
	require.True(typeChecked)
}
