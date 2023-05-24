// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/mocks"
	"github.com/ava-labs/avalanchego/version"
)

var (
	_ block.ChainVM                      = ContextEnabledVMMock{}
	_ block.BuildBlockWithContextChainVM = ContextEnabledVMMock{}

	_ snowman.Block           = ContextEnabledBlockMock{}
	_ block.WithVerifyContext = ContextEnabledBlockMock{}

	blockContext = &block.Context{
		PChainHeight: 1,
	}

	blkID    = ids.ID{1}
	parentID = ids.ID{0}
	blkBytes = []byte{0}
)

type ContextEnabledVMMock struct {
	*mocks.MockChainVM
	*mocks.MockBuildBlockWithContextChainVM
}

type ContextEnabledBlockMock struct {
	*snowman.MockBlock
	*mocks.MockWithVerifyContext
}

func contextEnabledTestPlugin(t *testing.T, loadExpectations bool) (block.ChainVM, *gomock.Controller) {
	// test key is "contextTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ctxVM := ContextEnabledVMMock{
		MockChainVM:                      mocks.NewMockChainVM(ctrl),
		MockBuildBlockWithContextChainVM: mocks.NewMockBuildBlockWithContextChainVM(ctrl),
	}

	if loadExpectations {
		ctxBlock := ContextEnabledBlockMock{
			MockBlock:             snowman.NewMockBlock(ctrl),
			MockWithVerifyContext: mocks.NewMockWithVerifyContext(ctrl),
		}
		gomock.InOrder(
			// Initialize
			ctxVM.MockChainVM.EXPECT().Initialize(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				gomock.Any(),
			).Return(nil).Times(1),
			ctxVM.MockChainVM.EXPECT().LastAccepted(gomock.Any()).Return(preSummaryBlk.ID(), nil).Times(1),
			ctxVM.MockChainVM.EXPECT().GetBlock(gomock.Any(), gomock.Any()).Return(preSummaryBlk, nil).Times(1),

			// BuildBlockWithContext
			ctxVM.MockBuildBlockWithContextChainVM.EXPECT().BuildBlockWithContext(gomock.Any(), blockContext).Return(ctxBlock, nil).Times(1),
			ctxBlock.MockWithVerifyContext.EXPECT().ShouldVerifyWithContext(gomock.Any()).Return(true, nil).Times(1),
			ctxBlock.MockBlock.EXPECT().ID().Return(blkID).Times(1),
			ctxBlock.MockBlock.EXPECT().Parent().Return(parentID).Times(1),
			ctxBlock.MockBlock.EXPECT().Bytes().Return(blkBytes).Times(1),
			ctxBlock.MockBlock.EXPECT().Height().Return(uint64(1)).Times(1),
			ctxBlock.MockBlock.EXPECT().Timestamp().Return(time.Now()).Times(1),

			// VerifyWithContext
			ctxVM.MockChainVM.EXPECT().ParseBlock(gomock.Any(), blkBytes).Return(ctxBlock, nil).Times(1),
			ctxBlock.MockWithVerifyContext.EXPECT().VerifyWithContext(gomock.Any(), blockContext).Return(nil).Times(1),
			ctxBlock.MockBlock.EXPECT().Timestamp().Return(time.Now()).Times(1),
		)
	}

	return ctxVM, ctrl
}

func TestContextVMSummary(t *testing.T) {
	require := require.New(t)
	testKey := contextTestKey

	// Create and start the plugin
	vm, stopper := buildClientHelper(require, testKey)
	defer stopper.Stop(context.Background())

	ctx := snow.DefaultContextTest()
	dbManager := manager.NewMemDB(version.Semantic1_0_0)

	err := vm.Initialize(context.Background(), ctx, dbManager, nil, nil, nil, nil, nil, nil)
	require.NoError(err)

	blkIntf, err := vm.BuildBlockWithContext(context.Background(), blockContext)
	require.NoError(err)

	blk, ok := blkIntf.(block.WithVerifyContext)
	require.True(ok)

	shouldVerify, err := blk.ShouldVerifyWithContext(context.Background())
	require.NoError(err)
	require.True(shouldVerify)

	err = blk.VerifyWithContext(context.Background(), blockContext)
	require.NoError(err)
}
