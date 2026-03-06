// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
)

func TestGetState(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	s := statetest.New(t, statetest.Config{})
	var (
		onAcceptState = state.NewMockDiff(ctrl)
		blkID1        = ids.GenerateTestID()
		blkID2        = ids.GenerateTestID()
		b             = &backend{
			state: s,
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
		_, ok := b.GetState(ids.GenerateTestID())
		require.False(ok)
	}

	{
		// Case: block is not in the map and block is last accepted.
		blkID := ids.GenerateTestID()
		s.SetLastAccepted(blkID)
		gotState, ok := b.GetState(blkID)
		require.True(ok)
		require.Equal(s, gotState)
	}
}

func TestBackendGetBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	var (
		blkID1       = ids.GenerateTestID()
		statelessBlk = block.NewMockBlock(ctrl)
		state        = statetest.New(t, statetest.Config{})

		b = &backend{
			state: state,
			blkIDToState: map[ids.ID]*blockState{
				blkID1: {
					statelessBlock: statelessBlk,
				},
			},
		}
	)

	{
		// Case: block is in the map.
		gotBlk, err := b.GetBlock(blkID1)
		require.NoError(err)
		require.Equal(statelessBlk, gotBlk)
	}

	{
		// Case: block isn't in the map or database.
		blkID := ids.GenerateTestID()
		_, err := b.GetBlock(blkID)
		require.Equal(database.ErrNotFound, err)
	}

	{
		// Case: block isn't in the map and is in database.
		blkID := ids.GenerateTestID()
		statelessBlk.EXPECT().ID().Return(blkID).Times(1)
		statelessBlk.EXPECT().Height().Return(uint64(2178)).Times(1)
		state.AddStatelessBlock(statelessBlk)
		gotBlk, err := b.GetBlock(blkID)
		require.NoError(err)
		require.Equal(statelessBlk, gotBlk)
	}
}

func TestGetTimestamp(t *testing.T) {
	type test struct {
		name              string
		backendF          func() *backend
		expectedTimestamp time.Time
	}

	blkID := ids.GenerateTestID()
	tests := []test{
		{
			name: "block is in map",
			backendF: func() *backend {
				return &backend{
					blkIDToState: map[ids.ID]*blockState{
						blkID: {
							timestamp: time.Unix(1337, 0),
						},
					},
				}
			},
			expectedTimestamp: time.Unix(1337, 0),
		},
		{
			name: "block isn't map",
			backendF: func() *backend {
				state := statetest.New(t, statetest.Config{})
				state.SetTimestamp(time.Unix(1337, 0))
				return &backend{
					state: state,
				}
			},
			expectedTimestamp: time.Unix(1337, 0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := tt.backendF()
			gotTimestamp := backend.getTimestamp(blkID)
			require.Equal(t, tt.expectedTimestamp, gotTimestamp)
		})
	}
}
