// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

func TestGetBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	statelessBlk, err := block.NewApricotCommitBlock(ids.GenerateTestID() /*parent*/, 2 /*height*/)
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
		state.EXPECT().GetStatelessBlock(statelessBlk.ID()).Return(nil, database.ErrNotFound).Times(1)
		_, err := manager.GetBlock(statelessBlk.ID())
		require.ErrorIs(err, database.ErrNotFound)
	}
	{
		// Case: block isn't in memory but is in database.
		state.EXPECT().GetStatelessBlock(statelessBlk.ID()).Return(statelessBlk, nil).Times(1)
		gotBlk, err := manager.GetBlock(statelessBlk.ID())
		require.NoError(err)
		require.Equal(statelessBlk.ID(), gotBlk.ID())
		require.IsType(&Block{}, gotBlk)
		innerBlk := gotBlk.(*Block)
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
		require.IsType(&Block{}, gotBlk)
		innerBlk := gotBlk.(*Block)
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

func TestManagerSetPreference(t *testing.T) {
	require := require.New(t)

	initialPreference := ids.GenerateTestID()
	manager := &manager{
		preferred: initialPreference,
	}
	require.False(manager.SetPreference(initialPreference))

	newPreference := ids.GenerateTestID()
	require.True(manager.SetPreference(newPreference))
	require.False(manager.SetPreference(newPreference))
	require.True(manager.SetPreference(initialPreference))
}
