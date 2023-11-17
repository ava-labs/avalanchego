// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/mocks"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

var errUnknownBlock = errors.New("unknown block")

type StateSyncEnabledMock struct {
	*block.TestVM
	*mocks.MockStateSyncableVM
}

func newTest(t *testing.T) (common.AllGetsServer, StateSyncEnabledMock, *common.SenderTest) {
	ctrl := gomock.NewController(t)

	vm := StateSyncEnabledMock{
		TestVM:              &block.TestVM{},
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	sender := &common.SenderTest{
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

	blkIDs := set.Of(blkID0, blkID1, blkID2)
	require.NoError(bs.GetAccepted(context.Background(), ids.EmptyNodeID, 0, blkIDs))

	require.Len(accepted, 2)
	require.Contains(accepted, blkID0)
	require.Contains(accepted, blkID1)
	require.NotContains(accepted, blkID2)
}
