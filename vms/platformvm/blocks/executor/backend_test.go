// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestGetState(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var (
		mockState     = state.NewMockState(ctrl)
		onAcceptState = state.NewMockDiff(ctrl)
		blkID1        = ids.GenerateTestID()
		blkID2        = ids.GenerateTestID()
		b             = &backend{
			state: mockState,
			blkIDToState: map[ids.ID]*blockState{
				blkID1: {
					onAcceptState: onAcceptState,
				},
				blkID2: {},
			},
		}
	)

	{
		// Case: block is in the map and onAcceptState isn't nil.
		gotState, ok := b.GetState(blkID1)
		require.True(ok)
		require.Equal(onAcceptState, gotState)
	}

	{
		// Case: block is in the map and onAcceptState is nil.
		_, ok := b.GetState(blkID2)
		require.False(ok)
	}

	{
		// Case: block is not in the map and block isn't last accepted.
		mockState.EXPECT().GetLastAccepted().Return(ids.GenerateTestID())
		_, ok := b.GetState(ids.GenerateTestID())
		require.False(ok)
	}

	{
		// Case: block is not in the map and block is last accepted.
		blkID := ids.GenerateTestID()
		mockState.EXPECT().GetLastAccepted().Return(blkID)
		gotState, ok := b.GetState(blkID)
		require.True(ok)
		require.Equal(mockState, gotState)
	}
}
