// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/blocks"
	"github.com/ava-labs/avalanchego/vms/avm/states"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/executor"
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
	defer ctrl.Finish()

	state := states.NewMockState(ctrl)
	m := &manager{
		state:        state,
		blkIDToState: map[ids.ID]*blockState{},
	}

	// Case: block is in memory
	{
		statelessBlk := blocks.NewMockBlock(ctrl)
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
		blk := blocks.NewMockBlock(ctrl)
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
	defer ctrl.Finish()

	state := states.NewMockState(ctrl)
	m := &manager{
		state:        state,
		blkIDToState: map[ids.ID]*blockState{},
		lastAccepted: ids.GenerateTestID(),
	}

	// Case: Block is in memory
	{
		diff := states.NewMockDiff(ctrl)
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
		require.Equal(state, gotState)
	}

	// Case: Block isn't in memory; block is last accepted
	{
		gotState, ok := m.GetState(m.lastAccepted)
		require.True(ok)
		require.Equal(state, gotState)
	}
}

func TestManagerVerifyTx(t *testing.T) {
	type test struct {
		name        string
		txF         func(*gomock.Controller) *txs.Tx
		managerF    func(*gomock.Controller) *manager
		expectedErr error
	}

	inputID := ids.GenerateTestID()
	tests := []test{
		{
			name: "not bootstrapped",
			txF: func(*gomock.Controller) *txs.Tx {
				return &txs.Tx{}
			},
			managerF: func(ctrl *gomock.Controller) *manager {
				return &manager{
					backend: &executor.Backend{},
				}
			},
			expectedErr: ErrChainNotSynced,
		},
		{
			name: "fails syntactic verification",
			txF: func(ctrl *gomock.Controller) *txs.Tx {
				unsigned := txs.NewMockUnsignedTx(ctrl)
				unsigned.EXPECT().Visit(gomock.Any()).Return(errTestSyntacticVerifyFail)
				return &txs.Tx{
					Unsigned: unsigned,
				}
			},
			managerF: func(*gomock.Controller) *manager {
				return &manager{
					backend: &executor.Backend{
						Bootstrapped: true,
					},
				}
			},
			expectedErr: errTestSyntacticVerifyFail,
		},
		{
			name: "fails semantic verification",
			txF: func(ctrl *gomock.Controller) *txs.Tx {
				unsigned := txs.NewMockUnsignedTx(ctrl)
				// Syntactic verification passes
				unsigned.EXPECT().Visit(gomock.Any()).Return(nil)
				// Semantic verification fails
				unsigned.EXPECT().Visit(gomock.Any()).Return(errTestSemanticVerifyFail)
				return &txs.Tx{
					Unsigned: unsigned,
				}
			},
			managerF: func(ctrl *gomock.Controller) *manager {
				preferred := ids.GenerateTestID()

				// These values don't matter for this test
				state := states.NewMockState(ctrl)
				state.EXPECT().GetLastAccepted().Return(preferred)
				state.EXPECT().GetTimestamp().Return(time.Time{})

				return &manager{
					backend: &executor.Backend{
						Bootstrapped: true,
					},
					state:        state,
					lastAccepted: preferred,
					preferred:    preferred,
				}
			},
			expectedErr: errTestSemanticVerifyFail,
		},
		{
			name: "fails execution",
			txF: func(ctrl *gomock.Controller) *txs.Tx {
				unsigned := txs.NewMockUnsignedTx(ctrl)
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
				preferred := ids.GenerateTestID()

				// These values don't matter for this test
				state := states.NewMockState(ctrl)
				state.EXPECT().GetLastAccepted().Return(preferred)
				state.EXPECT().GetTimestamp().Return(time.Time{})

				return &manager{
					backend: &executor.Backend{
						Bootstrapped: true,
					},
					state:        state,
					lastAccepted: preferred,
					preferred:    preferred,
				}
			},
			expectedErr: errTestExecutionFail,
		},
		{
			name: "non-unique inputs",
			txF: func(ctrl *gomock.Controller) *txs.Tx {
				unsigned := txs.NewMockUnsignedTx(ctrl)
				// Syntactic verification passes
				unsigned.EXPECT().Visit(gomock.Any()).Return(nil)
				// Semantic verification passes
				unsigned.EXPECT().Visit(gomock.Any()).Return(nil)
				// Execution passes
				unsigned.EXPECT().Visit(gomock.Any()).DoAndReturn(func(e *executor.Executor) error {
					e.Inputs.Add(inputID)
					return nil
				})
				return &txs.Tx{
					Unsigned: unsigned,
				}
			},
			managerF: func(ctrl *gomock.Controller) *manager {
				lastAcceptedID := ids.GenerateTestID()

				preferredID := ids.GenerateTestID()
				preferred := blocks.NewMockBlock(ctrl)
				preferred.EXPECT().Parent().Return(lastAcceptedID).AnyTimes()

				// These values don't matter for this test
				diffState := states.NewMockDiff(ctrl)
				diffState.EXPECT().GetLastAccepted().Return(preferredID)
				diffState.EXPECT().GetTimestamp().Return(time.Time{})

				return &manager{
					backend: &executor.Backend{
						Bootstrapped: true,
					},
					blkIDToState: map[ids.ID]*blockState{
						preferredID: {
							statelessBlock: preferred,
							onAcceptState:  diffState,
							importedInputs: set.Set[ids.ID]{inputID: struct{}{}},
						},
					},
					lastAccepted: lastAcceptedID,
					preferred:    preferredID,
				}
			},
			expectedErr: ErrConflictingParentTxs,
		},
		{
			name: "happy path",
			txF: func(ctrl *gomock.Controller) *txs.Tx {
				unsigned := txs.NewMockUnsignedTx(ctrl)
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
				preferred := ids.GenerateTestID()

				// These values don't matter for this test
				state := states.NewMockState(ctrl)
				state.EXPECT().GetLastAccepted().Return(preferred)
				state.EXPECT().GetTimestamp().Return(time.Time{})

				return &manager{
					backend: &executor.Backend{
						Bootstrapped: true,
					},
					state:        state,
					lastAccepted: preferred,
					preferred:    preferred,
				}
			},
			expectedErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

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
	defer ctrl.Finish()

	// Case: No inputs
	{
		m := &manager{}
		err := m.VerifyUniqueInputs(ids.GenerateTestID(), set.Set[ids.ID]{})
		require.NoError(err)
	}

	// blk0 is blk1's parent
	blk0ID, blk1ID := ids.GenerateTestID(), ids.GenerateTestID()
	blk0, blk1 := blocks.NewMockBlock(ctrl), blocks.NewMockBlock(ctrl)
	blk1.EXPECT().Parent().Return(blk0ID).AnyTimes()
	blk0.EXPECT().Parent().Return(ids.Empty).AnyTimes() // blk0's parent is accepted

	inputID := ids.GenerateTestID()
	m := &manager{
		blkIDToState: map[ids.ID]*blockState{
			blk0ID: {
				statelessBlock: blk0,
				importedInputs: set.Set[ids.ID]{inputID: struct{}{}},
			},
			blk1ID: {
				statelessBlock: blk1,
				importedInputs: set.Set[ids.ID]{ids.GenerateTestID(): struct{}{}},
			},
		},
	}
	// [blk1]'s parent, [blk0], has [inputID] as an input
	err := m.VerifyUniqueInputs(blk1ID, set.Set[ids.ID]{inputID: struct{}{}})
	require.ErrorIs(err, ErrConflictingParentTxs)

	err = m.VerifyUniqueInputs(blk1ID, set.Set[ids.ID]{ids.GenerateTestID(): struct{}{}})
	require.NoError(err)
}
