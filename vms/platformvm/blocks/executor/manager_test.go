// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

func TestGetBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	statelessBlk, err := blocks.NewApricotCommitBlock(ids.GenerateTestID() /*parent*/, 2 /*height*/)
	require.NoError(err)
	state := state.NewMockState(ctrl)
	manager := &manager{
		backend: &backend{
			state:        state,
			blkIDToState: map[ids.ID]*blockState{},
		},
	}

	{
		// Case: block isn't in memory or database
		state.EXPECT().GetStatelessBlock(statelessBlk.ID()).Return(nil, choices.Unknown, database.ErrNotFound).Times(1)
		_, err := manager.GetBlock(statelessBlk.ID())
		require.Error(err)
	}
	{
		// Case: block isn't in memory but is in database.
		state.EXPECT().GetStatelessBlock(statelessBlk.ID()).Return(statelessBlk, choices.Accepted, nil).Times(1)
		gotBlk, err := manager.GetBlock(statelessBlk.ID())
		require.NoError(err)
		require.Equal(statelessBlk.ID(), gotBlk.ID())
		innerBlk, ok := gotBlk.(*Block)
		require.True(ok)
		require.Equal(statelessBlk, innerBlk.Block)
		require.Equal(manager, innerBlk.manager)
	}
	{
		// Case: block is in memory
		manager.backend.blkIDToState[statelessBlk.ID()] = &blockState{
			statelessBlock: statelessBlk,
		}
		gotBlk, err := manager.GetBlock(statelessBlk.ID())
		require.NoError(err)
		require.Equal(statelessBlk.ID(), gotBlk.ID())
		innerBlk, ok := gotBlk.(*Block)
		require.True(ok)
		require.Equal(statelessBlk, innerBlk.Block)
		require.Equal(manager, innerBlk.manager)
	}
}

func TestManagerLastAccepted(t *testing.T) {
	lastAcceptedID := ids.GenerateTestID()
	manager := &manager{
		backend: &backend{
			lastAccepted: lastAcceptedID,
		},
	}

	require.Equal(t, lastAcceptedID, manager.LastAccepted())
}
