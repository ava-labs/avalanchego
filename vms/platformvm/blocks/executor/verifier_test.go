// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

func TestVerifierVisitProposalBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	s := state.NewMockState(ctrl)
	mempool := mempool.NewMockMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{},
		backend: &backend{
			lastAccepted: parentID,
			blkIDToState: map[ids.ID]*blockState{
				parentID: {
					statelessBlock: parentStatelessBlk,
				},
			},
			Mempool: mempool,
			state:   s,
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
		},
	}

	onCommitState := state.NewMockDiff(ctrl)
	onAbortState := state.NewMockDiff(ctrl)
	blkTx := txs.NewMockUnsignedTx(ctrl)
	blkTx.EXPECT().Visit(gomock.AssignableToTypeOf(&executor.ProposalTxExecutor{})).DoAndReturn(
		func(e *executor.ProposalTxExecutor) error {
			e.OnCommit = onCommitState
			e.OnAbort = onAbortState
			return nil
		},
	).Times(1)

	// We can't serialize [blkTx] because it isn't
	// registered with the blocks.Codec.
	// Serialize this block with a dummy tx
	// and replace it after creation with the mock tx.
	// TODO allow serialization of mock txs.
	blk, err := blocks.NewProposalBlock(
		parentID,
		2,
		&txs.Tx{
			Unsigned: &txs.AdvanceTimeTx{},
			Creds:    []verify.Verifiable{},
		},
	)
	require.NoError(err)
	blk.Tx.Unsigned = blkTx

	// Set expectations for dependencies.
	timestamp := time.Now()
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)
	mempool.EXPECT().RemoveProposalTx(blk.Tx).Times(1)
	onCommitState.EXPECT().AddTx(blk.Tx, status.Committed).Times(1)
	onAbortState.EXPECT().AddTx(blk.Tx, status.Aborted).Times(1)
	onAbortState.EXPECT().GetTimestamp().Return(timestamp).Times(1)

	// Visit the block
	err = verifier.ProposalBlock(blk)
	require.NoError(err)
	require.Contains(verifier.backend.blkIDToState, blk.ID())
	gotBlkState := verifier.backend.blkIDToState[blk.ID()]
	require.Equal(blk, gotBlkState.statelessBlock)
	require.Equal(onCommitState, gotBlkState.onCommitState)
	require.Equal(onAbortState, gotBlkState.onAbortState)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = verifier.ProposalBlock(blk)
	require.NoError(err)
}

func TestVerifierVisitAtomicBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempool.NewMockMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	grandparentID := ids.GenerateTestID()
	parentState := state.NewMockDiff(ctrl)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				ApricotPhase5Time: time.Now().Add(time.Hour),
			},
		},
		backend: &backend{
			blkIDToState: map[ids.ID]*blockState{
				parentID: {
					statelessBlock: parentStatelessBlk,
					onAcceptState:  parentState,
				},
			},
			Mempool: mempool,
			state:   s,
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
		},
	}

	onAccept := state.NewMockDiff(ctrl)
	blkTx := txs.NewMockUnsignedTx(ctrl)
	inputs := ids.Set{ids.GenerateTestID(): struct{}{}}
	blkTx.EXPECT().Visit(gomock.AssignableToTypeOf(&executor.AtomicTxExecutor{})).DoAndReturn(
		func(e *executor.AtomicTxExecutor) error {
			e.OnAccept = onAccept
			e.Inputs = inputs
			return nil
		},
	).Times(1)

	// We can't serialize [blkTx] because it isn't registered with blocks.Codec.
	// Serialize this block with a dummy tx and replace it after creation with
	// the mock tx.
	// TODO allow serialization of mock txs.
	blk, err := blocks.NewAtomicBlock(
		parentID,
		2,
		&txs.Tx{
			Unsigned: &txs.AdvanceTimeTx{},
			Creds:    []verify.Verifiable{},
		},
	)
	require.NoError(err)
	blk.Tx.Unsigned = blkTx

	// Set expectations for dependencies.
	timestamp := time.Now()
	parentState.EXPECT().GetTimestamp().Return(timestamp).Times(1)
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)
	parentStatelessBlk.EXPECT().Parent().Return(grandparentID).Times(1)
	mempool.EXPECT().RemoveDecisionTxs([]*txs.Tx{blk.Tx}).Times(1)
	onAccept.EXPECT().AddTx(blk.Tx, status.Committed).Times(1)
	onAccept.EXPECT().GetTimestamp().Return(timestamp).Times(1)

	err = verifier.AtomicBlock(blk)
	require.NoError(err)

	require.Contains(verifier.backend.blkIDToState, blk.ID())
	gotBlkState := verifier.backend.blkIDToState[blk.ID()]
	require.Equal(blk, gotBlkState.statelessBlock)
	require.Equal(onAccept, gotBlkState.onAcceptState)
	require.Equal(inputs, gotBlkState.inputs)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = verifier.AtomicBlock(blk)
	require.NoError(err)
}

func TestVerifierVisitStandardBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempool.NewMockMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	parentState := state.NewMockDiff(ctrl)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				ApricotPhase5Time: time.Now().Add(time.Hour),
			},
		},
		backend: &backend{
			blkIDToState: map[ids.ID]*blockState{
				parentID: {
					statelessBlock: parentStatelessBlk,
					onAcceptState:  parentState,
				},
			},
			Mempool: mempool,
			state:   s,
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
		},
	}

	blkTx := txs.NewMockUnsignedTx(ctrl)
	atomicRequests := map[ids.ID]*atomic.Requests{
		ids.GenerateTestID(): {
			RemoveRequests: [][]byte{{1}, {2}},
			PutRequests: []*atomic.Element{
				{
					Key:    []byte{3},
					Value:  []byte{4},
					Traits: [][]byte{{5}, {6}},
				},
			},
		},
	}
	blkTx.EXPECT().Visit(gomock.AssignableToTypeOf(&executor.StandardTxExecutor{})).DoAndReturn(
		func(e *executor.StandardTxExecutor) error {
			e.OnAccept = func() {}
			e.Inputs = ids.Set{}
			e.AtomicRequests = atomicRequests
			return nil
		},
	).Times(1)

	// We can't serialize [blkTx] because it isn't
	// registered with the blocks.Codec.
	// Serialize this block with a dummy tx
	// and replace it after creation with the mock tx.
	// TODO allow serialization of mock txs.
	blk, err := blocks.NewStandardBlock(
		parentID,
		2,
		[]*txs.Tx{
			{
				Unsigned: &txs.AdvanceTimeTx{},
				Creds:    []verify.Verifiable{},
			},
		},
	)
	require.NoError(err)
	blk.Transactions[0].Unsigned = blkTx

	// Set expectations for dependencies.
	timestamp := time.Now()
	parentState.EXPECT().GetTimestamp().Return(timestamp).Times(1)
	parentState.EXPECT().GetCurrentSupply().Return(uint64(10000)).Times(1)
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)
	mempool.EXPECT().RemoveDecisionTxs(blk.Transactions).Times(1)

	err = verifier.StandardBlock(blk)
	require.NoError(err)

	// Assert expected state.
	require.Contains(verifier.backend.blkIDToState, blk.ID())
	gotBlkState := verifier.backend.blkIDToState[blk.ID()]
	require.Equal(blk, gotBlkState.statelessBlock)
	require.Equal(ids.Set{}, gotBlkState.inputs)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = verifier.StandardBlock(blk)
	require.NoError(err)
}

func TestVerifierVisitCommitBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempool.NewMockMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	parentOnCommitState := state.NewMockDiff(ctrl)
	parentOnAbortState := state.NewMockDiff(ctrl)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{},
		backend: &backend{
			blkIDToState: map[ids.ID]*blockState{
				parentID: {
					statelessBlock: parentStatelessBlk,
					proposalBlockState: proposalBlockState{
						onCommitState: parentOnCommitState,
						onAbortState:  parentOnAbortState,
					},
					standardBlockState: standardBlockState{},
				},
			},
			Mempool: mempool,
			state:   s,
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
		},
	}

	blk, err := blocks.NewCommitBlock(
		parentID,
		2,
	)
	require.NoError(err)

	// Set expectations for dependencies.
	timestamp := time.Now()
	gomock.InOrder(
		parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1),
		parentOnCommitState.EXPECT().GetTimestamp().Return(timestamp).Times(1),
	)

	// Verify the block.
	err = verifier.CommitBlock(blk)
	require.NoError(err)

	// Assert expected state.
	require.Contains(verifier.backend.blkIDToState, blk.ID())
	gotBlkState := verifier.backend.blkIDToState[blk.ID()]
	require.Equal(parentOnAbortState, gotBlkState.onAcceptState)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = verifier.CommitBlock(blk)
	require.NoError(err)
}

func TestVerifierVisitAbortBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempool.NewMockMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	parentOnCommitState := state.NewMockDiff(ctrl)
	parentOnAbortState := state.NewMockDiff(ctrl)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{},
		backend: &backend{
			blkIDToState: map[ids.ID]*blockState{
				parentID: {
					statelessBlock: parentStatelessBlk,
					proposalBlockState: proposalBlockState{
						onCommitState: parentOnCommitState,
						onAbortState:  parentOnAbortState,
					},
					standardBlockState: standardBlockState{},
				},
			},
			Mempool: mempool,
			state:   s,
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
		},
	}

	blk, err := blocks.NewAbortBlock(
		parentID,
		2,
	)
	require.NoError(err)

	// Set expectations for dependencies.
	timestamp := time.Now()
	gomock.InOrder(
		parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1),
		parentOnAbortState.EXPECT().GetTimestamp().Return(timestamp).Times(1),
	)

	// Verify the block.
	err = verifier.AbortBlock(blk)
	require.NoError(err)

	// Assert expected state.
	require.Contains(verifier.backend.blkIDToState, blk.ID())
	gotBlkState := verifier.backend.blkIDToState[blk.ID()]
	require.Equal(parentOnAbortState, gotBlkState.onAcceptState)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = verifier.AbortBlock(blk)
	require.NoError(err)
}

func TestVerifierVisitStandardBlockWithDuplicateInputs(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempool.NewMockMempool(ctrl)
	grandParentID := ids.GenerateTestID()
	grandParentStatelessBlk := blocks.NewMockBlock(ctrl)
	grandParentState := state.NewMockDiff(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	parentState := state.NewMockDiff(ctrl)
	atomicInputs := ids.Set{
		ids.GenerateTestID(): struct{}{},
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				ApricotPhase5Time: time.Now().Add(time.Hour),
			},
		},
		backend: &backend{
			blkIDToState: map[ids.ID]*blockState{
				grandParentID: {
					standardBlockState: standardBlockState{
						inputs: atomicInputs,
					},
					statelessBlock: grandParentStatelessBlk,
					onAcceptState:  grandParentState,
				},
				parentID: {
					statelessBlock: parentStatelessBlk,
					onAcceptState:  parentState,
				},
			},
			Mempool: mempool,
			state:   s,
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
		},
	}

	blkTx := txs.NewMockUnsignedTx(ctrl)
	atomicRequests := map[ids.ID]*atomic.Requests{
		ids.GenerateTestID(): {
			RemoveRequests: [][]byte{{1}, {2}},
			PutRequests: []*atomic.Element{
				{
					Key:    []byte{3},
					Value:  []byte{4},
					Traits: [][]byte{{5}, {6}},
				},
			},
		},
	}
	blkTx.EXPECT().Visit(gomock.AssignableToTypeOf(&executor.StandardTxExecutor{})).DoAndReturn(
		func(e *executor.StandardTxExecutor) error {
			e.OnAccept = func() {}
			e.Inputs = atomicInputs
			e.AtomicRequests = atomicRequests
			return nil
		},
	).Times(1)

	// We can't serialize [blkTx] because it isn't
	// regiestered with the blocks.Codec.
	// Serialize this block with a dummy tx
	// and replace it after creation with the mock tx.
	// TODO allow serialization of mock txs.
	blk, err := blocks.NewStandardBlock(
		parentID,
		2,
		[]*txs.Tx{
			{
				Unsigned: &txs.AdvanceTimeTx{},
				Creds:    []verify.Verifiable{},
			},
		},
	)
	require.NoError(err)
	blk.Transactions[0].Unsigned = blkTx

	// Set expectations for dependencies.
	timestamp := time.Now()
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)
	parentState.EXPECT().GetTimestamp().Return(timestamp).Times(1)
	parentState.EXPECT().GetCurrentSupply().Return(uint64(10000)).Times(1)
	parentStatelessBlk.EXPECT().Parent().Return(grandParentID).Times(1)

	err = verifier.StandardBlock(blk)
	require.ErrorIs(err, errConflictingParentTxs)
}

func TestVerifierVisitStandardBlockWithProposalBlockParent(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempool.NewMockMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	parentOnCommitState := state.NewMockDiff(ctrl)
	parentOnAbortState := state.NewMockDiff(ctrl)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{},
		backend: &backend{
			blkIDToState: map[ids.ID]*blockState{
				parentID: {
					statelessBlock: parentStatelessBlk,
					proposalBlockState: proposalBlockState{
						onCommitState: parentOnCommitState,
						onAbortState:  parentOnAbortState,
					},
					standardBlockState: standardBlockState{},
				},
			},
			Mempool: mempool,
			state:   s,
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
		},
	}

	blk, err := blocks.NewStandardBlock(
		parentID,
		2,
		[]*txs.Tx{
			{
				Unsigned: &txs.AdvanceTimeTx{},
				Creds:    []verify.Verifiable{},
			},
		},
	)
	require.NoError(err)

	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)

	err = verifier.StandardBlock(blk)
	require.ErrorIs(err, state.ErrMissingParentState)
}
