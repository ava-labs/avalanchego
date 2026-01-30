// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/state/statemock"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/txsmock"
)

var (
	errTest                    = errors.New("test error")
	errTestSyntacticVerifyFail = errors.New("test error")
	errTestSemanticVerifyFail  = errors.New("test error")
	errTestExecutionFail       = errors.New("test error")
)

func TestManagerGetStatelessBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := statemock.NewState(ctrl)
	m := &manager{
		state:        state,
		blkIDToState: map[ids.ID]*blockState{},
	}

	// Case: block is in memory
	{
		statelessBlk := block.NewMockBlock(ctrl)
		blkID := ids.GenerateTestID()
		blk := &blockState{
			statelessBlock: statelessBlk,
		}
		m.blkIDToState[blkID] = blk
		gotBlk, err := m.GetStatelessBlock(blkID)
		require.NoError(err)
		require.Equal(statelessBlk, gotBlk)
	}

	// Case: block isn't in memory
	{
		blkID := ids.GenerateTestID()
		blk := block.NewMockBlock(ctrl)
		state.EXPECT().GetBlock(blkID).Return(blk, nil)
		gotBlk, err := m.GetStatelessBlock(blkID)
		require.NoError(err)
		require.Equal(blk, gotBlk)
	}

	// Case: error while getting block from state
	{
		blkID := ids.GenerateTestID()
		state.EXPECT().GetBlock(blkID).Return(nil, errTest)
		_, err := m.GetStatelessBlock(blkID)
		require.ErrorIs(err, errTest)
	}
}

func TestManagerGetState(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	s := statemock.NewState(ctrl)
	m := &manager{
		state:        s,
		blkIDToState: map[ids.ID]*blockState{},
		lastAccepted: ids.GenerateTestID(),
	}

	// Case: Block is in memory
	{
		diff := statemock.NewDiff(ctrl)
		blkID := ids.GenerateTestID()
		m.blkIDToState[blkID] = &blockState{
			onAcceptState: diff,
		}
		gotState, ok := m.GetState(blkID)
		require.True(ok)
		require.Equal(diff, gotState)
	}

	// Case: Block isn't in memory; block isn't last accepted
	{
		blkID := ids.GenerateTestID()
		gotState, ok := m.GetState(blkID)
		require.False(ok)
		require.Equal(s, gotState)
	}

	// Case: Block isn't in memory; block is last accepted
	{
		gotState, ok := m.GetState(m.lastAccepted)
		require.True(ok)
		require.Equal(s, gotState)
	}
}

func TestManagerVerifyTx(t *testing.T) {
	type test struct {
		name        string
		txF         func(*gomock.Controller) *txs.Tx
		managerF    func(*gomock.Controller) *manager
		expectedErr error
	}

	tests := []test{
		{
			name: "not bootstrapped",
			txF: func(*gomock.Controller) *txs.Tx {
				return &txs.Tx{}
			},
			managerF: func(*gomock.Controller) *manager {
				return &manager{
					backend: defaultTestBackend(false, nil),
				}
			},
			expectedErr: ErrChainNotSynced,
		},
		{
			name: "fails syntactic verification",
			txF: func(ctrl *gomock.Controller) *txs.Tx {
				unsigned := txsmock.NewUnsignedTx(ctrl)
				unsigned.EXPECT().Visit(gomock.Any()).Return(errTestSyntacticVerifyFail)
				return &txs.Tx{
					Unsigned: unsigned,
				}
			},
			managerF: func(*gomock.Controller) *manager {
				return &manager{
					backend: defaultTestBackend(true, nil),
				}
			},
			expectedErr: errTestSyntacticVerifyFail,
		},
		{
			name: "fails semantic verification",
			txF: func(ctrl *gomock.Controller) *txs.Tx {
				unsigned := txsmock.NewUnsignedTx(ctrl)
				// Syntactic verification passes
				unsigned.EXPECT().Visit(gomock.Any()).Return(nil)
				// Semantic verification fails
				unsigned.EXPECT().Visit(gomock.Any()).Return(errTestSemanticVerifyFail)
				return &txs.Tx{
					Unsigned: unsigned,
				}
			},
			managerF: func(ctrl *gomock.Controller) *manager {
				lastAcceptedID := ids.GenerateTestID()

				// These values don't matter for this test
				state := statemock.NewState(ctrl)
				state.EXPECT().GetLastAccepted().Return(lastAcceptedID)
				state.EXPECT().GetTimestamp().Return(time.Time{})

				return &manager{
					backend:      defaultTestBackend(true, nil),
					state:        state,
					lastAccepted: lastAcceptedID,
				}
			},
			expectedErr: errTestSemanticVerifyFail,
		},
		{
			name: "fails execution",
			txF: func(ctrl *gomock.Controller) *txs.Tx {
				unsigned := txsmock.NewUnsignedTx(ctrl)
				// Syntactic verification passes
				unsigned.EXPECT().Visit(gomock.Any()).Return(nil)
				// Semantic verification passes
				unsigned.EXPECT().Visit(gomock.Any()).Return(nil)
				// Execution fails
				unsigned.EXPECT().Visit(gomock.Any()).Return(errTestExecutionFail)
				return &txs.Tx{
					Unsigned: unsigned,
				}
			},
			managerF: func(ctrl *gomock.Controller) *manager {
				lastAcceptedID := ids.GenerateTestID()

				// These values don't matter for this test
				state := statemock.NewState(ctrl)
				state.EXPECT().GetLastAccepted().Return(lastAcceptedID)
				state.EXPECT().GetTimestamp().Return(time.Time{})

				return &manager{
					backend:      defaultTestBackend(true, nil),
					state:        state,
					lastAccepted: lastAcceptedID,
				}
			},
			expectedErr: errTestExecutionFail,
		},
		{
			name: "happy path",
			txF: func(ctrl *gomock.Controller) *txs.Tx {
				unsigned := txsmock.NewUnsignedTx(ctrl)
				// Syntactic verification passes
				unsigned.EXPECT().Visit(gomock.Any()).Return(nil)
				// Semantic verification passes
				unsigned.EXPECT().Visit(gomock.Any()).Return(nil)
				// Execution passes
				unsigned.EXPECT().Visit(gomock.Any()).Return(nil)
				return &txs.Tx{
					Unsigned: unsigned,
				}
			},
			managerF: func(ctrl *gomock.Controller) *manager {
				lastAcceptedID := ids.GenerateTestID()

				// These values don't matter for this test
				state := statemock.NewState(ctrl)
				state.EXPECT().GetLastAccepted().Return(lastAcceptedID)
				state.EXPECT().GetTimestamp().Return(time.Time{})

				return &manager{
					backend:      defaultTestBackend(true, nil),
					state:        state,
					lastAccepted: lastAcceptedID,
				}
			},
			expectedErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			m := test.managerF(ctrl)
			tx := test.txF(ctrl)
			err := m.VerifyTx(tx)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}

func TestVerifyUniqueInputs(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	// Case: No inputs
	{
		m := &manager{}
		require.NoError(m.VerifyUniqueInputs(ids.GenerateTestID(), set.Set[ids.ID]{}))
	}

	// blk0 is blk1's parent
	blk0ID, blk1ID := ids.GenerateTestID(), ids.GenerateTestID()
	blk0, blk1 := block.NewMockBlock(ctrl), block.NewMockBlock(ctrl)
	blk1.EXPECT().Parent().Return(blk0ID).AnyTimes()
	blk0.EXPECT().Parent().Return(ids.Empty).AnyTimes() // blk0's parent is accepted

	inputID := ids.GenerateTestID()
	m := &manager{
		blkIDToState: map[ids.ID]*blockState{
			blk0ID: {
				statelessBlock: blk0,
				importedInputs: set.Of(inputID),
			},
			blk1ID: {
				statelessBlock: blk1,
				importedInputs: set.Of(ids.GenerateTestID()),
			},
		},
	}
	// [blk1]'s parent, [blk0], has [inputID] as an input
	err := m.VerifyUniqueInputs(blk1ID, set.Of(inputID))
	require.ErrorIs(err, ErrConflictingParentTxs)

	require.NoError(m.VerifyUniqueInputs(blk1ID, set.Of(ids.GenerateTestID())))
}
