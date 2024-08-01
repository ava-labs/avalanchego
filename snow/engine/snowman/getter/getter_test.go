// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package getter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

var errUnknownBlock = errors.New("unknown block")

type StateSyncEnabledMock struct {
	*blocktest.TestVM
	*block.MockStateSyncableVM
}

func newTest(t *testing.T) (common.AllGetsServer, StateSyncEnabledMock, *enginetest.SenderTest) {
	ctrl := gomock.NewController(t)

	vm := StateSyncEnabledMock{
		TestVM:              &blocktest.TestVM{},
		MockStateSyncableVM: block.NewMockStateSyncableVM(ctrl),
	}

	sender := &enginetest.SenderTest{
		T: t,
	}
	sender.Default(true)

	bs, err := New(
		vm,
		sender,
		logging.NoLog{},
		time.Second,
		2000,
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)

	return bs, vm, sender
}

func TestAcceptedFrontier(t *testing.T) {
	require := require.New(t)
	bs, vm, sender := newTest(t)

	blkID := ids.GenerateTestID()
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return blkID, nil
	}

	var accepted ids.ID
	sender.SendAcceptedFrontierF = func(_ context.Context, _ ids.NodeID, _ uint32, containerID ids.ID) {
		accepted = containerID
	}

	require.NoError(bs.GetAcceptedFrontier(context.Background(), ids.EmptyNodeID, 0))
	require.Equal(blkID, accepted)
}

func TestFilterAccepted(t *testing.T) {
	require := require.New(t)
	bs, vm, sender := newTest(t)

	acceptedBlk := snowmantest.BuildChild(snowmantest.Genesis)
	require.NoError(acceptedBlk.Accept(context.Background()))

	var (
		allBlocks = []*snowmantest.Block{
			snowmantest.Genesis,
			acceptedBlk,
		}
		unknownBlkID = ids.GenerateTestID()
	)

	vm.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(allBlocks)
	vm.GetBlockIDAtHeightF = snowmantest.MakeGetBlockIDAtHeightF(allBlocks)
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		for _, blk := range allBlocks {
			if blk.ID() == blkID {
				return blk, nil
			}
		}

		require.Equal(unknownBlkID, blkID)
		return nil, errUnknownBlock
	}

	var accepted []ids.ID
	sender.SendAcceptedF = func(_ context.Context, _ ids.NodeID, _ uint32, frontier []ids.ID) {
		accepted = frontier
	}

	blkIDs := set.Of(snowmantest.GenesisID, acceptedBlk.ID(), unknownBlkID)
	require.NoError(bs.GetAccepted(context.Background(), ids.EmptyNodeID, 0, blkIDs))

	require.Len(accepted, 2)
	require.Contains(accepted, snowmantest.GenesisID)
	require.Contains(accepted, acceptedBlk.ID())
	require.NotContains(accepted, unknownBlkID)
}
