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
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmanmock"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blockmock"
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
	*blockmock.ChainVM
	*blockmock.BuildBlockWithContextChainVM
}

type ContextEnabledBlockMock struct {
	*snowmanmock.Block
	*blockmock.WithVerifyContext
}

func contextEnabledTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "contextTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ctxVM := ContextEnabledVMMock{
		ChainVM:                      blockmock.NewChainVM(ctrl),
		BuildBlockWithContextChainVM: blockmock.NewBuildBlockWithContextChainVM(ctrl),
	}

	if loadExpectations {
		ctxBlock := ContextEnabledBlockMock{
			Block:             snowmanmock.NewBlock(ctrl),
			WithVerifyContext: blockmock.NewWithVerifyContext(ctrl),
		}
		gomock.InOrder(
			// Initialize
			ctxVM.ChainVM.EXPECT().Initialize(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			).Return(nil).Times(1),
			ctxVM.ChainVM.EXPECT().LastAccepted(gomock.Any()).Return(preSummaryBlk.ID(), nil).Times(1),
			ctxVM.ChainVM.EXPECT().GetBlock(gomock.Any(), gomock.Any()).Return(preSummaryBlk, nil).Times(1),

			// BuildBlockWithContext
			ctxVM.BuildBlockWithContextChainVM.EXPECT().BuildBlockWithContext(gomock.Any(), blockContext).Return(ctxBlock, nil).Times(1),
			ctxBlock.WithVerifyContext.EXPECT().ShouldVerifyWithContext(gomock.Any()).Return(true, nil).Times(1),
			ctxBlock.Block.EXPECT().ID().Return(blkID).Times(1),
			ctxBlock.Block.EXPECT().Parent().Return(parentID).Times(1),
			ctxBlock.Block.EXPECT().Bytes().Return(blkBytes).Times(1),
			ctxBlock.Block.EXPECT().Height().Return(uint64(1)).Times(1),
			ctxBlock.Block.EXPECT().Timestamp().Return(time.Now()).Times(1),

			// VerifyWithContext
			ctxVM.ChainVM.EXPECT().ParseBlock(gomock.Any(), blkBytes).Return(ctxBlock, nil).Times(1),
			ctxBlock.WithVerifyContext.EXPECT().VerifyWithContext(gomock.Any(), blockContext).Return(nil).Times(1),
			ctxBlock.Block.EXPECT().Timestamp().Return(time.Now()).Times(1),
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

	require.NoError(vm.Initialize(context.Background(), ctx, memdb.New(), nil, nil, nil, nil, nil))

	blkIntf, err := vm.BuildBlockWithContext(context.Background(), blockContext)
	require.NoError(err)

	blk, ok := blkIntf.(block.WithVerifyContext)
	require.True(ok)

	shouldVerify, err := blk.ShouldVerifyWithContext(context.Background())
	require.NoError(err)
	require.True(shouldVerify)

	require.NoError(blk.VerifyWithContext(context.Background(), blockContext))
}
