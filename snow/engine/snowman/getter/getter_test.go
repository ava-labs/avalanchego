// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package getter

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/mocks"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
)

var errUnknownBlock = errors.New("unknown block")

type StateSyncEnabledMock struct {
	*block.TestVM
	*mocks.MockStateSyncableVM
}

func testSetup(
	t *testing.T,
	ctrl *gomock.Controller,
) (StateSyncEnabledMock, *common.SenderTest, common.Config) {
	ctx := snow.DefaultConsensusContextTest()

	peers := validators.NewSet()
	sender := &common.SenderTest{}
	vm := StateSyncEnabledMock{
		TestVM:              &block.TestVM{},
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	sender.T = t

	sender.Default(true)

	isBootstrapped := false
	bootstrapTracker := &common.BootstrapTrackerTest{
		T: t,
		IsBootstrappedF: func() bool {
			return isBootstrapped
		},
		BootstrappedF: func(ids.ID) {
			isBootstrapped = true
		},
	}

	sender.CantSendGetAcceptedFrontier = false

	peer := ids.GenerateTestNodeID()
	require.NoError(t, peers.Add(peer, nil, ids.Empty, 1))

	commonConfig := common.Config{
		Ctx:                            ctx,
		Beacons:                        peers,
		SampleK:                        peers.Len(),
		Alpha:                          peers.Weight()/2 + 1,
		Sender:                         sender,
		BootstrapTracker:               bootstrapTracker,
		Timer:                          &common.TimerTest{},
		AncestorsMaxContainersSent:     2000,
		AncestorsMaxContainersReceived: 2000,
		SharedCfg:                      &common.SharedConfig{},
	}

	return vm, sender, commonConfig
}

func TestAcceptedFrontier(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	vm, sender, config := testSetup(t, ctrl)

	blkID := ids.GenerateTestID()

	dummyBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  []byte{1, 2, 3},
	}
	vm.CantLastAccepted = false
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return blkID, nil
	}
	vm.GetBlockF = func(_ context.Context, bID ids.ID) (snowman.Block, error) {
		require.Equal(blkID, bID)
		return dummyBlk, nil
	}

	bsIntf, err := New(vm, config)
	require.NoError(err)
	require.IsType(&getter{}, bsIntf)
	bs := bsIntf.(*getter)

	var accepted ids.ID
	sender.SendAcceptedFrontierF = func(_ context.Context, _ ids.NodeID, _ uint32, containerID ids.ID) {
		accepted = containerID
	}

	require.NoError(bs.GetAcceptedFrontier(context.Background(), ids.EmptyNodeID, 0))
	require.Equal(blkID, accepted)
}

func TestFilterAccepted(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	vm, sender, config := testSetup(t, ctrl)

	blkID0 := ids.GenerateTestID()
	blkID1 := ids.GenerateTestID()
	blkID2 := ids.GenerateTestID()

	blk0 := &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     blkID0,
		StatusV: choices.Accepted,
	}}
	blk1 := &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     blkID1,
		StatusV: choices.Accepted,
	}}

	vm.CantLastAccepted = false
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return blk1.ID(), nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(blk1.ID(), blkID)
		return blk1, nil
	}

	bsIntf, err := New(vm, config)
	require.NoError(err)
	require.IsType(&getter{}, bsIntf)
	bs := bsIntf.(*getter)

	blkIDs := []ids.ID{blkID0, blkID1, blkID2}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			return blk1, nil
		case blkID2:
			return nil, errUnknownBlock
		}
		require.FailNow(errUnknownBlock.Error())
		return nil, errUnknownBlock
	}

	var accepted []ids.ID
	sender.SendAcceptedF = func(_ context.Context, _ ids.NodeID, _ uint32, frontier []ids.ID) {
		accepted = frontier
	}

	require.NoError(bs.GetAccepted(context.Background(), ids.EmptyNodeID, 0, blkIDs))

	acceptedSet := set.Set[ids.ID]{}
	acceptedSet.Add(accepted...)

	require.Len(acceptedSet, 2)
	require.Contains(acceptedSet, blkID0)
	require.Contains(acceptedSet, blkID1)
	require.NotContains(acceptedSet, blkID2)
}
