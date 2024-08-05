// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
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
	*block.MockChainVM
	*block.MockBuildBlockWithContextChainVM
}

type ContextEnabledBlockMock struct {
	*snowmantest.MockBlock
	*block.MockWithVerifyContext
}

func contextEnabledTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "contextTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ctxVM := ContextEnabledVMMock{
		MockChainVM:                      block.NewMockChainVM(ctrl),
		MockBuildBlockWithContextChainVM: block.NewMockBuildBlockWithContextChainVM(ctrl),
	}

	if loadExpectations {
		ctxBlock := ContextEnabledBlockMock{
			MockBlock:             snowmantest.NewMockBlock(ctrl),
			MockWithVerifyContext: block.NewMockWithVerifyContext(ctrl),
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

	return ctxVM
}

func TestContextVMSummary(t *testing.T) {
	require := require.New(t)
	testKey := contextTestKey

	// Create and start the plugin
	vm := buildClientHelper(require, testKey)
	defer vm.runtime.Stop(context.Background())

	ctx := snowtest.Context(t, snowtest.CChainID)

	require.NoError(vm.Initialize(context.Background(), ctx, memdb.New(), nil, nil, nil, nil, nil, nil))

	blkIntf, err := vm.BuildBlockWithContext(context.Background(), blockContext)
	require.NoError(err)

	blk, ok := blkIntf.(block.WithVerifyContext)
	require.True(ok)

	shouldVerify, err := blk.ShouldVerifyWithContext(context.Background())
	require.NoError(err)
	require.True(shouldVerify)

	require.NoError(blk.VerifyWithContext(context.Background(), blockContext))
}
