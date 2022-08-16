// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/assert"

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
	assert := assert.New(t)
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
	parentOnAcceptState.EXPECT().GetCurrentSupply().Return(uint64(10000)).Times(2)

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
		cfg: &config.Config{BlueberryTime: mockable.MaxTime}, // blueberry is not activated
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{},
		backend:           backend,
		forkChecker: &forkChecker{
			backend: backend,
		},
	}

	blkTx := txs.NewMockUnsignedTx(ctrl)
	blkTx.EXPECT().Visit(gomock.AssignableToTypeOf(&executor.ProposalTxExecutor{})).Return(nil).Times(1)

	// We can't serialize [blkTx] because it isn't
	// registered with the blocks.Codec.
	// Serialize this block with a dummy tx
	// and replace it after creation with the mock tx.
	// TODO allow serialization of mock txs.
	blk, err := blocks.NewApricotProposalBlock(
		parentID,
		2,
		&txs.Tx{
			Unsigned: &txs.AdvanceTimeTx{},
			Creds:    []verify.Verifiable{},
		},
	)
	assert.NoError(err)
	blk.Tx.Unsigned = blkTx

	// Set expectations for dependencies.
	tx := blk.Txs()[0]
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)
	mempool.EXPECT().RemoveProposalTx(tx).Times(1)

	// Visit the block
	err = verifier.ApricotProposalBlock(blk)
	assert.NoError(err)
	assert.Contains(verifier.backend.blkIDToState, blk.ID())
	gotBlkState := verifier.backend.blkIDToState[blk.ID()]
	assert.Equal(blk, gotBlkState.statelessBlock)
	assert.Equal(timestamp, gotBlkState.timestamp)

	// Assert that the expected tx statuses are set.
	_, gotStatus, err := gotBlkState.onCommitState.GetTx(tx.ID())
	assert.NoError(err)
	assert.Equal(status.Committed, gotStatus)

	_, gotStatus, err = gotBlkState.onAbortState.GetTx(tx.ID())
	assert.NoError(err)
	assert.Equal(status.Aborted, gotStatus)

	// Visiting again should return nil without using dependencies.
	err = verifier.ApricotProposalBlock(blk)
	assert.NoError(err)
}

func TestVerifierVisitAtomicBlock(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempool.NewMockMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := blocks.NewMockBlock(ctrl)
	grandparentID := ids.GenerateTestID()
	parentState := state.NewMockDiff(ctrl)

	config := &config.Config{
		ApricotPhase5Time: time.Now().Add(time.Hour),
		BlueberryTime:     mockable.MaxTime, // blueberry is not activated
	}
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
		cfg: config,
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: config,
		},
		backend: backend,
		forkChecker: &forkChecker{
			backend: backend,
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
	blk, err := blocks.NewApricotAtomicBlock(
		parentID,
		2,
		&txs.Tx{
			Unsigned: &txs.AdvanceTimeTx{},
			Creds:    []verify.Verifiable{},
		},
	)
	assert.NoError(err)
	blk.Tx.Unsigned = blkTx

	// Set expectations for dependencies.
	timestamp := time.Now()
	parentState.EXPECT().GetTimestamp().Return(timestamp).Times(1)
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)
	parentStatelessBlk.EXPECT().Parent().Return(grandparentID).Times(1)
	mempool.EXPECT().RemoveDecisionTxs([]*txs.Tx{blk.Tx}).Times(1)
	onAccept.EXPECT().AddTx(blk.Tx, status.Committed).Times(1)
	onAccept.EXPECT().GetTimestamp().Return(timestamp).Times(1)

	err = verifier.ApricotAtomicBlock(blk)
	assert.NoError(err)

	assert.Contains(verifier.backend.blkIDToState, blk.ID())
	gotBlkState := verifier.backend.blkIDToState[blk.ID()]
	assert.Equal(blk, gotBlkState.statelessBlock)
	assert.Equal(onAccept, gotBlkState.onAcceptState)
	assert.Equal(inputs, gotBlkState.inputs)
	assert.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = verifier.ApricotAtomicBlock(blk)
	assert.NoError(err)
}

func TestVerifierVisitStandardBlock(t *testing.T) {
	assert := assert.New(t)
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
		cfg: &config.Config{BlueberryTime: mockable.MaxTime}, // blueberry is not activated
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				ApricotPhase5Time: time.Now().Add(time.Hour),
			},
		},
		backend: backend,
		forkChecker: &forkChecker{
			backend: backend,
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
	blk, err := blocks.NewApricotStandardBlock(
		parentID,
		2, /*height*/
		[]*txs.Tx{
			{
				Unsigned: &txs.AdvanceTimeTx{},
				Creds:    []verify.Verifiable{},
			},
		},
	)
	assert.NoError(err)
	blk.Transactions[0].Unsigned = blkTx

	// Set expectations for dependencies.
	timestamp := time.Now()
	parentState.EXPECT().GetTimestamp().Return(timestamp).Times(1)
	parentState.EXPECT().GetCurrentSupply().Return(uint64(10000)).Times(1)
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)
	mempool.EXPECT().RemoveDecisionTxs(blk.Txs()).Times(1)

	err = verifier.ApricotStandardBlock(blk)
	assert.NoError(err)

	// Assert expected state.
	assert.Contains(verifier.backend.blkIDToState, blk.ID())
	gotBlkState := verifier.backend.blkIDToState[blk.ID()]
	assert.Equal(blk, gotBlkState.statelessBlock)
	assert.Equal(ids.Set{}, gotBlkState.inputs)
	assert.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = verifier.ApricotStandardBlock(blk)
	assert.NoError(err)
}

func TestVerifierVisitCommitBlock(t *testing.T) {
	assert := assert.New(t)
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
		cfg: &config.Config{BlueberryTime: mockable.MaxTime}, // blueberry is not activated
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{},
		backend:           backend,
		forkChecker: &forkChecker{
			backend: backend,
		},
	}

	blk, err := blocks.NewApricotCommitBlock(parentID, 2 /*height*/)
	assert.NoError(err)

	// Set expectations for dependencies.
	timestamp := time.Now()
	gomock.InOrder(
		parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1),
		parentOnCommitState.EXPECT().GetTimestamp().Return(timestamp).Times(1),
	)

	// Verify the block.
	err = verifier.ApricotCommitBlock(blk)
	assert.NoError(err)

	// Assert expected state.
	assert.Contains(verifier.backend.blkIDToState, blk.ID())
	gotBlkState := verifier.backend.blkIDToState[blk.ID()]
	assert.Equal(parentOnAbortState, gotBlkState.onAcceptState)
	assert.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = verifier.ApricotCommitBlock(blk)
	assert.NoError(err)
}

func TestVerifierVisitAbortBlock(t *testing.T) {
	assert := assert.New(t)
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
		cfg: &config.Config{BlueberryTime: mockable.MaxTime}, // blueberry is not activated
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{},
		backend:           backend,
		forkChecker: &forkChecker{
			backend: backend,
		},
	}

	blk, err := blocks.NewApricotAbortBlock(parentID, 2 /*height*/)
	assert.NoError(err)

	// Set expectations for dependencies.
	timestamp := time.Now()
	gomock.InOrder(
		parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1),
		parentOnAbortState.EXPECT().GetTimestamp().Return(timestamp).Times(1),
	)

	// Verify the block.
	err = verifier.ApricotAbortBlock(blk)
	assert.NoError(err)

	// Assert expected state.
	assert.Contains(verifier.backend.blkIDToState, blk.ID())
	gotBlkState := verifier.backend.blkIDToState[blk.ID()]
	assert.Equal(parentOnAbortState, gotBlkState.onAcceptState)
	assert.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	err = verifier.ApricotAbortBlock(blk)
	assert.NoError(err)
}

// Assert that a block with an unverified parent fails verification.
func TestVerifyUnverifiedParent(t *testing.T) {
	assert := assert.New(t)
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
		cfg: &config.Config{BlueberryTime: mockable.MaxTime}, // blueberry is not activated
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{},
		backend:           backend,
		forkChecker: &forkChecker{
			backend: backend,
		},
	}

	blk, err := blocks.NewApricotAbortBlock(parentID /*not in memory or persisted state*/, 2 /*height*/)
	assert.NoError(err)

	// Set expectations for dependencies.
	s.EXPECT().GetStatelessBlock(parentID).Return(nil, choices.Unknown, database.ErrNotFound).Times(1)

	// Verify the block.
	err = blk.Visit(verifier)
	assert.Error(err)
}

func TestBlueberryAbortBlockTimestampChecks(t *testing.T) {
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
			assert := assert.New(t)

			// Create mocked dependencies.
			s := state.NewMockState(ctrl)
			mempool := mempool.NewMockMempool(ctrl)
			parentID := ids.GenerateTestID()
			parentStatelessBlk := blocks.NewMockBlock(ctrl)
			parentHeight := uint64(1)

			backend := &backend{
				blkIDToState: map[ids.ID]*blockState{
					parentID: {
						timestamp:      test.parentTime,
						statelessBlock: parentStatelessBlk,
					},
				},
				Mempool: mempool,
				state:   s,
				ctx: &snow.Context{
					Log: logging.NoLog{},
				},
				// Blueberry is activated
				cfg: &config.Config{BlueberryTime: time.Time{}},
			}
			fc := &forkChecker{
				backend: backend,
				clk:     &mockable.Clock{},
			}

			// build and verify child block
			childHeight := parentHeight + 1
			statelessAbortBlk, err := blocks.NewBlueberryAbortBlock(test.childTime, parentID, childHeight)
			assert.NoError(err)

			// Set expectations for dependencies.
			parentStatelessBlk.EXPECT().Timestamp().Return(test.parentTime).Times(1)

			err = statelessAbortBlk.Visit(fc)
			assert.ErrorIs(err, test.result)
		})
	}
}

// TODO combine with TestApricotCommitBlockTimestampChecks
func TestBlueberryCommitBlockTimestampChecks(t *testing.T) {
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
			assert := assert.New(t)

			// Create mocked dependencies.
			s := state.NewMockState(ctrl)
			mempool := mempool.NewMockMempool(ctrl)
			parentID := ids.GenerateTestID()
			parentStatelessBlk := blocks.NewMockBlock(ctrl)
			parentHeight := uint64(1)

			backend := &backend{
				blkIDToState: map[ids.ID]*blockState{
					parentID: {
						timestamp:      test.parentTime,
						statelessBlock: parentStatelessBlk,
					},
				},
				Mempool: mempool,
				state:   s,
				ctx: &snow.Context{
					Log: logging.NoLog{},
				},
				// Blueberry is activated
				cfg: &config.Config{BlueberryTime: time.Time{}},
			}
			fc := &forkChecker{
				backend: backend,
				clk:     &mockable.Clock{},
			}

			// build and verify child block
			childHeight := parentHeight + 1
			statelessCommitBlk, err := blocks.NewBlueberryCommitBlock(test.childTime, parentID, childHeight)

			// Set expectations for dependencies.
			parentStatelessBlk.EXPECT().Timestamp().Return(test.parentTime).Times(1)
			assert.NoError(err)
			err = statelessCommitBlk.Visit(fc)
			assert.ErrorIs(err, test.result)
		})
	}
}

func TestVerifierVisitStandardBlockWithDuplicateInputs(t *testing.T) {
	assert := assert.New(t)
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

	config := &config.Config{
		ApricotPhase5Time: time.Now().Add(time.Hour),
		BlueberryTime:     mockable.MaxTime,
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
		cfg: config,
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: config,
		},
		backend: backend,
		forkChecker: &forkChecker{
			backend: backend,
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
	assert.NoError(err)
	blk.Transactions[0].Unsigned = blkTx

	// Set expectations for dependencies.
	timestamp := time.Now()
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)
	parentState.EXPECT().GetTimestamp().Return(timestamp).Times(1)
	parentState.EXPECT().GetCurrentSupply().Return(uint64(10000)).Times(1)
	parentStatelessBlk.EXPECT().Parent().Return(grandParentID).Times(1)

	err = verifier.ApricotStandardBlock(blk)
	assert.ErrorIs(err, errConflictingParentTxs)
}

func TestVerifierVisitStandardBlockWithProposalBlockParent(t *testing.T) {
	assert := assert.New(t)
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
		cfg: &config.Config{
			BlueberryTime: mockable.MaxTime,
		},
	}
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{},
		backend:           backend,
		forkChecker: &forkChecker{
			backend: backend,
			clk:     &mockable.Clock{},
		},
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
	assert.NoError(err)

	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)

	err = verifier.ApricotStandardBlock(blk)
	assert.ErrorIs(err, state.ErrMissingParentState)
}
