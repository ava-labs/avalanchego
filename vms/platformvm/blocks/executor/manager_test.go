// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

func TestGetBlock(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	statelessBlk, err := blocks.NewCommitBlock(ids.GenerateTestID(), 2)
	assert.NoError(err)
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
		assert.Error(err)
	}
	{
		// Case: block isn't in memory but is in database.
		state.EXPECT().GetStatelessBlock(statelessBlk.ID()).Return(statelessBlk, choices.Accepted, nil).Times(1)
		gotBlk, err := manager.GetBlock(statelessBlk.ID())
		assert.NoError(err)
		assert.Equal(statelessBlk.ID(), gotBlk.ID())
		innerBlk, ok := gotBlk.(*Block)
		assert.True(ok)
		assert.Equal(statelessBlk, innerBlk.Block)
		assert.Equal(manager, innerBlk.manager)
	}
	{
		// Case: block is in memory
		manager.backend.blkIDToState[statelessBlk.ID()] = &blockState{
			statelessBlock: statelessBlk,
		}
		gotBlk, err := manager.GetBlock(statelessBlk.ID())
		assert.NoError(err)
		assert.Equal(statelessBlk.ID(), gotBlk.ID())
		innerBlk, ok := gotBlk.(*Block)
		assert.True(ok)
		assert.Equal(statelessBlk, innerBlk.Block)
		assert.Equal(manager, innerBlk.manager)
	}
}

func TestManagerLastAccepted(t *testing.T) {
	assert := assert.New(t)

	lastAcceptedID := ids.GenerateTestID()
	manager := &manager{
		backend: &backend{
			lastAccepted: lastAcceptedID,
		},
	}

	assert.Equal(lastAcceptedID, manager.LastAccepted())
}
