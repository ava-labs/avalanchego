// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
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
	parentOnAcceptState := state.NewMockDiff(ctrl)
	timestamp := time.Now()
	// One call for each of onCommitState and onAbortState.
	parentOnAcceptState.EXPECT().GetTimestamp().Return(timestamp).Times(2)

	backend := &backend{
		lastAccepted: parentID,
		blkIDToState: map[ids.ID]*blockState{
			parentID: {
				statelessBlock: parentStatelessBlk,
				onAcceptState:  parentOnAcceptState,
			},
		},
		Mempool: mempool,
		state:   s,
		ctx: &snow.Context{
			Log: logging.NoLog{},
		},
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				BanffTime: mockable.MaxTime, // banff is not activated
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}
	manager := &manager{
		backend:  backend,
		verifier: verifier,
	}

	blkTx := txs.NewMockUnsignedTx(ctrl)
	blkTx.EXPECT().Visit(gomock.AssignableToTypeOf(&executor.ProposalTxExecutor{})).Return(nil).Times(1)

	// We can't serialize [blkTx] because it isn't
	// registered with the blocks.Codec.
	// Serialize this block with a dummy tx
	// and replace it after creation with the mock tx.
	// TODO allow serialization of mock txs.
	apricotBlk, err := blocks.NewApricotProposalBlock(
		parentID,
		2,
		&txs.Tx{
			Unsigned: &txs.AdvanceTimeTx{},
			Creds:    []verify.Verifiable{},
		},
	)
	require.NoError(err)
	apricotBlk.Tx.Unsigned = blkTx

	// Set expectations for dependencies.
	tx := apricotBlk.Txs()[0]
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)
	mempool.EXPECT().Remove([]*txs.Tx{tx}).Times(1)

	// Visit the block
	blk := manager.NewBlock(apricotBlk)
	err = blk.Verify(context.Background())
	require.NoError(err)
	require.Contains(verifier.backend.blkIDToState, apricotBlk.ID())
	gotBlkState := verifier.backend.blkIDToState[apricotBlk.ID()]
	require.Equal(apricotBlk, gotBlkState.statelessBlock)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Assert that the expected tx statuses are set.
	_, gotStatus, err := gotBlkState.onCommitState.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, gotStatus)

	_, gotStatus, err = gotBlkState.onAbortState.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Aborted, gotStatus)

	// Visiting again should return nil without using dependencies.
	err = blk.Verify(context.Background())
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

	backend := &backend{
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
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				ApricotPhase5Time: time.Now().Add(time.Hour),
				BanffTime:         mockable.MaxTime, // banff is not activated
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}
	manager := &manager{
		backend:  backend,
		verifier: verifier,
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
	apricotBlk, err := blocks.NewApricotAtomicBlock(
		parentID,
		2,
		&txs.Tx{
			Unsigned: &txs.AdvanceTimeTx{},
			Creds:    []verify.Verifiable{},
		},
	)
	require.NoError(err)
	apricotBlk.Tx.Unsigned = blkTx

	// Set expectations for dependencies.
	timestamp := time.Now()
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)
	parentStatelessBlk.EXPECT().Parent().Return(grandparentID).Times(1)
	mempool.EXPECT().Remove([]*txs.Tx{apricotBlk.Tx}).Times(1)
	onAccept.EXPECT().AddTx(apricotBlk.Tx, status.Committed).Times(1)
	onAccept.EXPECT().GetTimestamp().Return(timestamp).Times(1)

	blk := manager.NewBlock(apricotBlk)
	err = blk.Verify(context.Background())
	require.NoError(err)

	require.Contains(verifier.backend.blkIDToState, apricotBlk.ID())
	gotBlkState := verifier.backend.blkIDToState[apricotBlk.ID()]
	require.Equal(apricotBlk, gotBlkState.statelessBlock)
	require.Equal(onAccept, gotBlkState.onAcceptState)
	require.Equal(inputs, gotBlkState.inputs)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = blk.Verify(context.Background())
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

	backend := &backend{
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
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				ApricotPhase5Time: time.Now().Add(time.Hour),
				BanffTime:         mockable.MaxTime, // banff is not activated
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}
	manager := &manager{
		backend:  backend,
		verifier: verifier,
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
	apricotBlk, err := blocks.NewApricotStandardBlock(
		parentID,
		2, /*height*/
		[]*txs.Tx{
			{
				Unsigned: &txs.AdvanceTimeTx{},
				Creds:    []verify.Verifiable{},
			},
		},
	)
	require.NoError(err)
	apricotBlk.Transactions[0].Unsigned = blkTx

	// Set expectations for dependencies.
	timestamp := time.Now()
	parentState.EXPECT().GetTimestamp().Return(timestamp).Times(1)
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)
	mempool.EXPECT().Remove(apricotBlk.Txs()).Times(1)

	blk := manager.NewBlock(apricotBlk)
	err = blk.Verify(context.Background())
	require.NoError(err)

	// Assert expected state.
	require.Contains(verifier.backend.blkIDToState, apricotBlk.ID())
	gotBlkState := verifier.backend.blkIDToState[apricotBlk.ID()]
	require.Equal(apricotBlk, gotBlkState.statelessBlock)
	require.Equal(ids.Set{}, gotBlkState.inputs)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = blk.Verify(context.Background())
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

	backend := &backend{
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
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				BanffTime: mockable.MaxTime, // banff is not activated
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}
	manager := &manager{
		backend:  backend,
		verifier: verifier,
	}

	apricotBlk, err := blocks.NewApricotCommitBlock(
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
	blk := manager.NewBlock(apricotBlk)
	err = blk.Verify(context.Background())
	require.NoError(err)

	// Assert expected state.
	require.Contains(verifier.backend.blkIDToState, apricotBlk.ID())
	gotBlkState := verifier.backend.blkIDToState[apricotBlk.ID()]
	require.Equal(parentOnAbortState, gotBlkState.onAcceptState)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = blk.Verify(context.Background())
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

	backend := &backend{
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
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				BanffTime: mockable.MaxTime, // banff is not activated
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}
	manager := &manager{
		backend:  backend,
		verifier: verifier,
	}

	apricotBlk, err := blocks.NewApricotAbortBlock(
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
	blk := manager.NewBlock(apricotBlk)
	err = blk.Verify(context.Background())
	require.NoError(err)

	// Assert expected state.
	require.Contains(verifier.backend.blkIDToState, apricotBlk.ID())
	gotBlkState := verifier.backend.blkIDToState[apricotBlk.ID()]
	require.Equal(parentOnAbortState, gotBlkState.onAcceptState)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = blk.Verify(context.Background())
	require.NoError(err)
}

// Assert that a block with an unverified parent fails verification.
func TestVerifyUnverifiedParent(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempool.NewMockMempool(ctrl)
	parentID := ids.GenerateTestID()

	backend := &backend{
		blkIDToState: map[ids.ID]*blockState{},
		Mempool:      mempool,
		state:        s,
		ctx: &snow.Context{
			Log: logging.NoLog{},
		},
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				BanffTime: mockable.MaxTime, // banff is not activated
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}

	blk, err := blocks.NewApricotAbortBlock(parentID /*not in memory or persisted state*/, 2 /*height*/)
	require.NoError(err)

	// Set expectations for dependencies.
	s.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	s.EXPECT().GetStatelessBlock(parentID).Return(nil, choices.Unknown, database.ErrNotFound).Times(1)

	// Verify the block.
	err = blk.Visit(verifier)
	require.Error(err)
}

func TestBanffAbortBlockTimestampChecks(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	now := defaultGenesisTime.Add(time.Hour)

	tests := []struct {
		description string
		parentTime  time.Time
		childTime   time.Time
		result      error
	}{
		{
			description: "abort block timestamp matching parent's one",
			parentTime:  now,
			childTime:   now,
			result:      nil,
		},
		{
			description: "abort block timestamp before parent's one",
			childTime:   now.Add(-1 * time.Second),
			parentTime:  now,
			result:      errOptionBlockTimestampNotMatchingParent,
		},
		{
			description: "abort block timestamp after parent's one",
			parentTime:  now,
			childTime:   now.Add(time.Second),
			result:      errOptionBlockTimestampNotMatchingParent,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			require := require.New(t)

			// Create mocked dependencies.
			s := state.NewMockState(ctrl)
			mempool := mempool.NewMockMempool(ctrl)
			parentID := ids.GenerateTestID()
			parentStatelessBlk := blocks.NewMockBlock(ctrl)
			parentHeight := uint64(1)

			backend := &backend{
				blkIDToState: make(map[ids.ID]*blockState),
				Mempool:      mempool,
				state:        s,
				ctx: &snow.Context{
					Log: logging.NoLog{},
				},
			}
			verifier := &verifier{
				txExecutorBackend: &executor.Backend{
					Config: &config.Config{
						BanffTime: time.Time{}, // banff is activated
					},
					Clk: &mockable.Clock{},
				},
				backend: backend,
			}

			// build and verify child block
			childHeight := parentHeight + 1
			statelessAbortBlk, err := blocks.NewBanffAbortBlock(test.childTime, parentID, childHeight)
			require.NoError(err)

			// setup parent state
			parentTime := defaultGenesisTime
			s.EXPECT().GetLastAccepted().Return(parentID).Times(2)
			s.EXPECT().GetTimestamp().Return(parentTime).Times(2)

			onCommitState, err := state.NewDiff(parentID, backend)
			require.NoError(err)
			onAbortState, err := state.NewDiff(parentID, backend)
			require.NoError(err)
			backend.blkIDToState[parentID] = &blockState{
				timestamp:      test.parentTime,
				statelessBlock: parentStatelessBlk,
				proposalBlockState: proposalBlockState{
					onCommitState: onCommitState,
					onAbortState:  onAbortState,
				},
			}

			// Set expectations for dependencies.
			parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)

			err = statelessAbortBlk.Visit(verifier)
			require.ErrorIs(err, test.result)
		})
	}
}

// TODO combine with TestApricotCommitBlockTimestampChecks
func TestBanffCommitBlockTimestampChecks(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	now := defaultGenesisTime.Add(time.Hour)

	tests := []struct {
		description string
		parentTime  time.Time
		childTime   time.Time
		result      error
	}{
		{
			description: "commit block timestamp matching parent's one",
			parentTime:  now,
			childTime:   now,
			result:      nil,
		},
		{
			description: "commit block timestamp before parent's one",
			childTime:   now.Add(-1 * time.Second),
			parentTime:  now,
			result:      errOptionBlockTimestampNotMatchingParent,
		},
		{
			description: "commit block timestamp after parent's one",
			parentTime:  now,
			childTime:   now.Add(time.Second),
			result:      errOptionBlockTimestampNotMatchingParent,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			require := require.New(t)

			// Create mocked dependencies.
			s := state.NewMockState(ctrl)
			mempool := mempool.NewMockMempool(ctrl)
			parentID := ids.GenerateTestID()
			parentStatelessBlk := blocks.NewMockBlock(ctrl)
			parentHeight := uint64(1)

			backend := &backend{
				blkIDToState: make(map[ids.ID]*blockState),
				Mempool:      mempool,
				state:        s,
				ctx: &snow.Context{
					Log: logging.NoLog{},
				},
			}
			verifier := &verifier{
				txExecutorBackend: &executor.Backend{
					Config: &config.Config{
						BanffTime: time.Time{}, // banff is activated
					},
					Clk: &mockable.Clock{},
				},
				backend: backend,
			}

			// build and verify child block
			childHeight := parentHeight + 1
			statelessCommitBlk, err := blocks.NewBanffCommitBlock(test.childTime, parentID, childHeight)
			require.NoError(err)

			// setup parent state
			parentTime := defaultGenesisTime
			s.EXPECT().GetLastAccepted().Return(parentID).Times(2)
			s.EXPECT().GetTimestamp().Return(parentTime).Times(2)

			onCommitState, err := state.NewDiff(parentID, backend)
			require.NoError(err)
			onAbortState, err := state.NewDiff(parentID, backend)
			require.NoError(err)
			backend.blkIDToState[parentID] = &blockState{
				timestamp:      test.parentTime,
				statelessBlock: parentStatelessBlk,
				proposalBlockState: proposalBlockState{
					onCommitState: onCommitState,
					onAbortState:  onAbortState,
				},
			}

			// Set expectations for dependencies.
			parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)

			err = statelessCommitBlk.Visit(verifier)
			require.ErrorIs(err, test.result)
		})
	}
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

	backend := &backend{
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
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				ApricotPhase5Time: time.Now().Add(time.Hour),
				BanffTime:         mockable.MaxTime, // banff is not activated
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
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
	// registered with the blocks.Codec.
	// Serialize this block with a dummy tx
	// and replace it after creation with the mock tx.
	// TODO allow serialization of mock txs.
	blk, err := blocks.NewApricotStandardBlock(
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
	parentStatelessBlk.EXPECT().Parent().Return(grandParentID).Times(1)

	err = verifier.ApricotStandardBlock(blk)
	require.ErrorIs(err, errConflictingParentTxs)
}

func TestVerifierVisitApricotStandardBlockWithProposalBlockParent(t *testing.T) {
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

	backend := &backend{
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
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				BanffTime: mockable.MaxTime, // banff is not activated
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}

	blk, err := blocks.NewApricotStandardBlock(
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

	err = verifier.ApricotStandardBlock(blk)
	require.ErrorIs(err, state.ErrMissingParentState)
}

func TestVerifierVisitBanffStandardBlockWithProposalBlockParent(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempool.NewMockMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	parentTime := time.Now()
	parentOnCommitState := state.NewMockDiff(ctrl)
	parentOnAbortState := state.NewMockDiff(ctrl)

	backend := &backend{
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
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				BanffTime: time.Time{}, // banff is activated
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}

	blk, err := blocks.NewBanffStandardBlock(
		parentTime.Add(time.Second),
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

	err = verifier.BanffStandardBlock(blk)
	require.ErrorIs(err, state.ErrMissingParentState)
}

func TestVerifierVisitApricotCommitBlockUnexpectedParentState(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				BanffTime: mockable.MaxTime, // banff is not activated
			},
			Clk: &mockable.Clock{},
		},
		backend: &backend{
			blkIDToState: map[ids.ID]*blockState{
				parentID: {
					statelessBlock: parentStatelessBlk,
				},
			},
			state: s,
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
		},
	}

	blk, err := blocks.NewApricotCommitBlock(
		parentID,
		2,
	)
	require.NoError(err)

	// Set expectations for dependencies.
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)

	// Verify the block.
	err = verifier.ApricotCommitBlock(blk)
	require.ErrorIs(err, state.ErrMissingParentState)
}

func TestVerifierVisitBanffCommitBlockUnexpectedParentState(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	timestamp := time.Unix(12345, 0)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				BanffTime: time.Time{}, // banff is activated
			},
			Clk: &mockable.Clock{},
		},
		backend: &backend{
			blkIDToState: map[ids.ID]*blockState{
				parentID: {
					statelessBlock: parentStatelessBlk,
					timestamp:      timestamp,
				},
			},
			state: s,
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
		},
	}

	blk, err := blocks.NewBanffCommitBlock(
		timestamp,
		parentID,
		2,
	)
	require.NoError(err)

	// Set expectations for dependencies.
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)

	// Verify the block.
	err = verifier.BanffCommitBlock(blk)
	require.ErrorIs(err, state.ErrMissingParentState)
}

func TestVerifierVisitApricotAbortBlockUnexpectedParentState(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				BanffTime: mockable.MaxTime, // banff is not activated
			},
			Clk: &mockable.Clock{},
		},
		backend: &backend{
			blkIDToState: map[ids.ID]*blockState{
				parentID: {
					statelessBlock: parentStatelessBlk,
				},
			},
			state: s,
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
		},
	}

	blk, err := blocks.NewApricotAbortBlock(
		parentID,
		2,
	)
	require.NoError(err)

	// Set expectations for dependencies.
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)

	// Verify the block.
	err = verifier.ApricotAbortBlock(blk)
	require.ErrorIs(err, state.ErrMissingParentState)
}

func TestVerifierVisitBanffAbortBlockUnexpectedParentState(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	timestamp := time.Unix(12345, 0)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				BanffTime: time.Time{}, // banff is activated
			},
			Clk: &mockable.Clock{},
		},
		backend: &backend{
			blkIDToState: map[ids.ID]*blockState{
				parentID: {
					statelessBlock: parentStatelessBlk,
					timestamp:      timestamp,
				},
			},
			state: s,
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
		},
	}

	blk, err := blocks.NewBanffAbortBlock(
		timestamp,
		parentID,
		2,
	)
	require.NoError(err)

	// Set expectations for dependencies.
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)

	// Verify the block.
	err = verifier.BanffAbortBlock(blk)
	require.ErrorIs(err, state.ErrMissingParentState)
}
