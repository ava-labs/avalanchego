// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/chains/atomic/atomicmock"
	"github.com/ava-labs/avalanchego/database/databasemock"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators/validatorstest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestAcceptorVisitProposalBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	lastAcceptedID := ids.GenerateTestID()

	blk, err := block.NewApricotProposalBlock(
		lastAcceptedID,
		1,
		&txs.Tx{
			Unsigned: &txs.AddDelegatorTx{
				// Without the line below, this function will error.
				DelegationRewardsOwner: &secp256k1fx.OutputOwners{},
			},
			Creds: []verify.Verifiable{},
		},
	)
	require.NoError(err)

	blkID := blk.ID()

	s := state.NewMockState(ctrl)
	s.EXPECT().Checksum().Return(ids.Empty).Times(1)

	acceptor := &acceptor{
		backend: &backend{
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
			blkIDToState: map[ids.ID]*blockState{
				blkID: {},
			},
			state: s,
		},
		metrics:    metrics.Noop,
		validators: validatorstest.Manager,
	}

	require.NoError(acceptor.ApricotProposalBlock(blk))

	require.Equal(blkID, acceptor.backend.lastAccepted)

	_, exists := acceptor.GetState(blkID)
	require.False(exists)

	s.EXPECT().GetLastAccepted().Return(lastAcceptedID).Times(1)

	_, exists = acceptor.GetState(lastAcceptedID)
	require.True(exists)
}

func TestAcceptorVisitAtomicBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	s := state.NewMockState(ctrl)
	sharedMemory := atomicmock.NewSharedMemory(ctrl)

	parentID := ids.GenerateTestID()
	acceptor := &acceptor{
		backend: &backend{
			lastAccepted: parentID,
			blkIDToState: make(map[ids.ID]*blockState),
			state:        s,
			ctx: &snow.Context{
				Log:          logging.NoLog{},
				SharedMemory: sharedMemory,
			},
		},
		metrics:    metrics.Noop,
		validators: validatorstest.Manager,
	}

	blk, err := block.NewApricotAtomicBlock(
		parentID,
		1,
		&txs.Tx{
			Unsigned: &txs.AddDelegatorTx{
				// Without the line below, this function will error.
				DelegationRewardsOwner: &secp256k1fx.OutputOwners{},
			},
			Creds: []verify.Verifiable{},
		},
	)
	require.NoError(err)

	err = acceptor.ApricotAtomicBlock(blk)
	require.ErrorIs(err, errMissingBlockState)

	// Set [blk]'s state in the map as though it had been verified.
	onAcceptState := state.NewMockDiff(ctrl)
	childID := ids.GenerateTestID()
	atomicRequests := make(map[ids.ID]*atomic.Requests)
	acceptor.backend.blkIDToState[blk.ID()] = &blockState{
		statelessBlock: blk,
		onAcceptState:  onAcceptState,
		atomicRequests: atomicRequests,
		metrics: metrics.Block{
			Block: blk,
		},
	}
	// Give [blk] a child.
	childOnAcceptState := state.NewMockDiff(ctrl)
	childOnAbortState := state.NewMockDiff(ctrl)
	childOnCommitState := state.NewMockDiff(ctrl)
	childState := &blockState{
		onAcceptState: childOnAcceptState,
		proposalBlockState: proposalBlockState{
			onAbortState:  childOnAbortState,
			onCommitState: childOnCommitState,
		},
	}
	acceptor.backend.blkIDToState[childID] = childState

	// Set expected calls on dependencies.
	s.EXPECT().SetLastAccepted(blk.ID()).Times(1)
	s.EXPECT().SetHeight(blk.Height()).Times(1)
	s.EXPECT().AddStatelessBlock(blk).Times(1)
	batch := databasemock.NewBatch(ctrl)
	s.EXPECT().CommitBatch().Return(batch, nil).Times(1)
	s.EXPECT().Abort().Times(1)
	onAcceptState.EXPECT().Apply(s).Times(1)
	sharedMemory.EXPECT().Apply(atomicRequests, batch).Return(nil).Times(1)
	s.EXPECT().Checksum().Return(ids.Empty).Times(1)

	require.NoError(acceptor.ApricotAtomicBlock(blk))
}

func TestAcceptorVisitStandardBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	s := state.NewMockState(ctrl)
	sharedMemory := atomicmock.NewSharedMemory(ctrl)

	parentID := ids.GenerateTestID()
	clk := &mockable.Clock{}
	acceptor := &acceptor{
		backend: &backend{
			lastAccepted: parentID,
			blkIDToState: make(map[ids.ID]*blockState),
			state:        s,
			ctx: &snow.Context{
				Log:          logging.NoLog{},
				SharedMemory: sharedMemory,
			},
		},
		metrics:    metrics.Noop,
		validators: validatorstest.Manager,
	}

	blk, err := block.NewBanffStandardBlock(
		clk.Time(),
		parentID,
		1,
		[]*txs.Tx{
			{
				Unsigned: &txs.AddDelegatorTx{
					// Without the line below, this function will error.
					DelegationRewardsOwner: &secp256k1fx.OutputOwners{},
				},
				Creds: []verify.Verifiable{},
			},
		},
	)
	require.NoError(err)

	err = acceptor.BanffStandardBlock(blk)
	require.ErrorIs(err, errMissingBlockState)

	// Set [blk]'s state in the map as though it had been verified.
	onAcceptState := state.NewMockDiff(ctrl)
	childID := ids.GenerateTestID()
	atomicRequests := make(map[ids.ID]*atomic.Requests)
	calledOnAcceptFunc := false
	acceptor.backend.blkIDToState[blk.ID()] = &blockState{
		statelessBlock: blk,
		onAcceptState:  onAcceptState,
		onAcceptFunc: func() {
			calledOnAcceptFunc = true
		},

		atomicRequests: atomicRequests,
		metrics: metrics.Block{
			Block: blk,
		},
	}
	// Give [blk] a child.
	childOnAcceptState := state.NewMockDiff(ctrl)
	childOnAbortState := state.NewMockDiff(ctrl)
	childOnCommitState := state.NewMockDiff(ctrl)
	childState := &blockState{
		onAcceptState: childOnAcceptState,
		proposalBlockState: proposalBlockState{
			onAbortState:  childOnAbortState,
			onCommitState: childOnCommitState,
		},
	}
	acceptor.backend.blkIDToState[childID] = childState

	// Set expected calls on dependencies.
	s.EXPECT().SetLastAccepted(blk.ID()).Times(1)
	s.EXPECT().SetHeight(blk.Height()).Times(1)
	s.EXPECT().AddStatelessBlock(blk).Times(1)
	batch := databasemock.NewBatch(ctrl)
	s.EXPECT().CommitBatch().Return(batch, nil).Times(1)
	s.EXPECT().Abort().Times(1)
	onAcceptState.EXPECT().Apply(s).Times(1)
	sharedMemory.EXPECT().Apply(atomicRequests, batch).Return(nil).Times(1)
	s.EXPECT().Checksum().Return(ids.Empty).Times(1)

	require.NoError(acceptor.BanffStandardBlock(blk))
	require.True(calledOnAcceptFunc)
	require.Equal(blk.ID(), acceptor.backend.lastAccepted)
}

func TestAcceptorVisitCommitBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	s := state.NewMockState(ctrl)
	sharedMemory := atomicmock.NewSharedMemory(ctrl)

	parentID := ids.GenerateTestID()
	acceptor := &acceptor{
		backend: &backend{
			lastAccepted: parentID,
			blkIDToState: make(map[ids.ID]*blockState),
			state:        s,
			ctx: &snow.Context{
				Log:          logging.NoLog{},
				SharedMemory: sharedMemory,
			},
		},
		metrics:    metrics.Noop,
		validators: validatorstest.Manager,
	}

	blk, err := block.NewApricotCommitBlock(parentID, 1 /*height*/)
	require.NoError(err)

	err = acceptor.ApricotCommitBlock(blk)
	require.ErrorIs(err, state.ErrMissingParentState)

	// Set [blk]'s parent in the state map.
	parentOnAcceptState := state.NewMockDiff(ctrl)
	parentOnAbortState := state.NewMockDiff(ctrl)
	parentOnCommitState := state.NewMockDiff(ctrl)
	parentStatelessBlk := block.NewMockBlock(ctrl)
	calledOnAcceptFunc := false
	atomicRequests := make(map[ids.ID]*atomic.Requests)
	parentState := &blockState{
		proposalBlockState: proposalBlockState{
			onAbortState:  parentOnAbortState,
			onCommitState: parentOnCommitState,
		},
		statelessBlock: parentStatelessBlk,

		onAcceptState: parentOnAcceptState,
		onAcceptFunc: func() {
			calledOnAcceptFunc = true
		},

		atomicRequests: atomicRequests,
	}
	acceptor.backend.blkIDToState[parentID] = parentState

	blkID := blk.ID()
	// Set expected calls on dependencies.
	// Make sure the parent is accepted first.
	gomock.InOrder(
		parentStatelessBlk.EXPECT().ID().Return(parentID).Times(1),
		s.EXPECT().SetLastAccepted(parentID).Times(1),
		parentStatelessBlk.EXPECT().Height().Return(blk.Height()-1).Times(1),
		s.EXPECT().SetHeight(blk.Height()-1).Times(1),
		s.EXPECT().AddStatelessBlock(parentState.statelessBlock).Times(1),
	)

	err = acceptor.ApricotCommitBlock(blk)
	require.ErrorIs(err, errMissingBlockState)

	parentOnCommitState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))

	// Set [blk]'s state in the map as though it had been verified.
	acceptor.backend.blkIDToState[parentID] = parentState
	acceptor.backend.blkIDToState[blkID] = &blockState{
		statelessBlock: blk,
		onAcceptState:  parentState.onCommitState,
		onAcceptFunc:   parentState.onAcceptFunc,

		inputs:         parentState.inputs,
		timestamp:      parentOnCommitState.GetTimestamp(),
		atomicRequests: parentState.atomicRequests,
		metrics: metrics.Block{
			Block: blk,
		},
	}

	batch := databasemock.NewBatch(ctrl)

	// Set expected calls on dependencies.
	// Make sure the parent is accepted first.
	gomock.InOrder(
		parentStatelessBlk.EXPECT().ID().Return(parentID).Times(1),
		s.EXPECT().SetLastAccepted(parentID).Times(1),
		parentStatelessBlk.EXPECT().Height().Return(blk.Height()-1).Times(1),
		s.EXPECT().SetHeight(blk.Height()-1).Times(1),
		s.EXPECT().AddStatelessBlock(parentState.statelessBlock).Times(1),

		s.EXPECT().SetLastAccepted(blkID).Times(1),
		s.EXPECT().SetHeight(blk.Height()).Times(1),
		s.EXPECT().AddStatelessBlock(blk).Times(1),

		parentOnCommitState.EXPECT().Apply(s).Times(1),
		s.EXPECT().CommitBatch().Return(batch, nil).Times(1),
		sharedMemory.EXPECT().Apply(atomicRequests, batch).Return(nil).Times(1),
		s.EXPECT().Checksum().Return(ids.Empty).Times(1),
		s.EXPECT().Abort().Times(1),
	)

	require.NoError(acceptor.ApricotCommitBlock(blk))
	require.True(calledOnAcceptFunc)
	require.Equal(blk.ID(), acceptor.backend.lastAccepted)
}

func TestAcceptorVisitAbortBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	s := state.NewMockState(ctrl)
	sharedMemory := atomicmock.NewSharedMemory(ctrl)

	parentID := ids.GenerateTestID()
	acceptor := &acceptor{
		backend: &backend{
			lastAccepted: parentID,
			blkIDToState: make(map[ids.ID]*blockState),
			state:        s,
			ctx: &snow.Context{
				Log:          logging.NoLog{},
				SharedMemory: sharedMemory,
			},
		},
		metrics:    metrics.Noop,
		validators: validatorstest.Manager,
	}

	blk, err := block.NewApricotAbortBlock(parentID, 1 /*height*/)
	require.NoError(err)

	err = acceptor.ApricotAbortBlock(blk)
	require.ErrorIs(err, state.ErrMissingParentState)

	// Set [blk]'s parent in the state map.
	parentOnAcceptState := state.NewMockDiff(ctrl)
	parentOnAbortState := state.NewMockDiff(ctrl)
	parentOnCommitState := state.NewMockDiff(ctrl)
	parentStatelessBlk := block.NewMockBlock(ctrl)
	calledOnAcceptFunc := false
	atomicRequests := make(map[ids.ID]*atomic.Requests)
	parentState := &blockState{
		proposalBlockState: proposalBlockState{
			onAbortState:  parentOnAbortState,
			onCommitState: parentOnCommitState,
		},
		statelessBlock: parentStatelessBlk,

		onAcceptState: parentOnAcceptState,
		onAcceptFunc: func() {
			calledOnAcceptFunc = true
		},

		atomicRequests: atomicRequests,
	}
	acceptor.backend.blkIDToState[parentID] = parentState

	blkID := blk.ID()
	// Set expected calls on dependencies.
	// Make sure the parent is accepted first.
	gomock.InOrder(
		parentStatelessBlk.EXPECT().ID().Return(parentID).Times(1),
		s.EXPECT().SetLastAccepted(parentID).Times(1),
		parentStatelessBlk.EXPECT().Height().Return(blk.Height()-1).Times(1),
		s.EXPECT().SetHeight(blk.Height()-1).Times(1),
		s.EXPECT().AddStatelessBlock(parentState.statelessBlock).Times(1),
	)

	err = acceptor.ApricotAbortBlock(blk)
	require.ErrorIs(err, errMissingBlockState)

	parentOnAbortState.EXPECT().GetTimestamp().Return(time.Unix(0, 0))

	// Set [blk]'s state in the map as though it had been verified.
	acceptor.backend.blkIDToState[parentID] = parentState
	acceptor.backend.blkIDToState[blkID] = &blockState{
		statelessBlock: blk,
		onAcceptState:  parentState.onAbortState,
		onAcceptFunc:   parentState.onAcceptFunc,

		inputs:         parentState.inputs,
		timestamp:      parentOnAbortState.GetTimestamp(),
		atomicRequests: parentState.atomicRequests,
		metrics: metrics.Block{
			Block: blk,
		},
	}

	batch := databasemock.NewBatch(ctrl)

	// Set expected calls on dependencies.
	// Make sure the parent is accepted first.
	gomock.InOrder(
		parentStatelessBlk.EXPECT().ID().Return(parentID).Times(1),
		s.EXPECT().SetLastAccepted(parentID).Times(1),
		parentStatelessBlk.EXPECT().Height().Return(blk.Height()-1).Times(1),
		s.EXPECT().SetHeight(blk.Height()-1).Times(1),
		s.EXPECT().AddStatelessBlock(parentState.statelessBlock).Times(1),

		s.EXPECT().SetLastAccepted(blkID).Times(1),
		s.EXPECT().SetHeight(blk.Height()).Times(1),
		s.EXPECT().AddStatelessBlock(blk).Times(1),

		parentOnAbortState.EXPECT().Apply(s).Times(1),
		s.EXPECT().CommitBatch().Return(batch, nil).Times(1),
		sharedMemory.EXPECT().Apply(atomicRequests, batch).Return(nil).Times(1),
		s.EXPECT().Checksum().Return(ids.Empty).Times(1),
		s.EXPECT().Abort().Times(1),
	)

	require.NoError(acceptor.ApricotAbortBlock(blk))
	require.True(calledOnAcceptFunc)
	require.Equal(blk.ID(), acceptor.backend.lastAccepted)
}
