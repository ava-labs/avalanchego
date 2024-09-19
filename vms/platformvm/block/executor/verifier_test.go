// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool/mempoolmock"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txsmock"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txstest"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func newTestVerifier(t testing.TB, s state.State) *verifier {
	require := require.New(t)

	mempool, err := mempool.New("", prometheus.NewRegistry(), nil)
	require.NoError(err)

	var (
		upgrades = upgradetest.GetConfig(upgradetest.Latest)
		ctx      = snowtest.Context(t, constants.PlatformChainID)
		clock    = &mockable.Clock{}
		fx       = &secp256k1fx.Fx{}
	)
	require.NoError(fx.InitializeVM(&secp256k1fx.TestVM{
		Clk: *clock,
		Log: logging.NoLog{},
	}))

	return &verifier{
		backend: &backend{
			Mempool:      mempool,
			lastAccepted: s.GetLastAccepted(),
			blkIDToState: make(map[ids.ID]*blockState),
			state:        s,
			ctx:          ctx,
		},
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				CreateAssetTxFee:       genesis.LocalParams.CreateAssetTxFee,
				StaticFeeConfig:        genesis.LocalParams.StaticFeeConfig,
				DynamicFeeConfig:       genesis.LocalParams.DynamicFeeConfig,
				SybilProtectionEnabled: true,
				UpgradeConfig:          upgrades,
			},
			Ctx: ctx,
			Clk: clock,
			Fx:  fx,
			FlowChecker: utxo.NewVerifier(
				ctx,
				clock,
				fx,
			),
			Bootstrapped: utils.NewAtomic(true),
		},
	}
}

func TestVerifierVisitProposalBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	s := state.NewMockState(ctrl)
	mempool := mempoolmock.NewMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := block.NewMockBlock(ctrl)
	parentOnAcceptState := state.NewMockDiff(ctrl)
	timestamp := time.Now()
	// One call for each of onCommitState and onAbortState.
	parentOnAcceptState.EXPECT().GetTimestamp().Return(timestamp).Times(2)
	parentOnAcceptState.EXPECT().GetFeeState().Return(gas.State{}).Times(2)
	parentOnAcceptState.EXPECT().GetAccruedFees().Return(uint64(0)).Times(2)

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
	manager := &manager{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				UpgradeConfig: upgradetest.GetConfig(upgradetest.ApricotPhasePost6),
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}

	blkTx := txsmock.NewUnsignedTx(ctrl)
	blkTx.EXPECT().Visit(gomock.AssignableToTypeOf(&executor.ProposalTxExecutor{})).Return(nil).Times(1)

	// We can't serialize [blkTx] because it isn't
	// registered with the blocks.Codec.
	// Serialize this block with a dummy tx
	// and replace it after creation with the mock tx.
	// TODO allow serialization of mock txs.
	apricotBlk, err := block.NewApricotProposalBlock(
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
	require.NoError(blk.Verify(context.Background()))
	require.Contains(manager.backend.blkIDToState, apricotBlk.ID())
	gotBlkState := manager.backend.blkIDToState[apricotBlk.ID()]
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
	require.NoError(blk.Verify(context.Background()))
}

func TestVerifierVisitAtomicBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempoolmock.NewMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := block.NewMockBlock(ctrl)
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
	manager := &manager{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				UpgradeConfig: upgradetest.GetConfig(upgradetest.ApricotPhasePost6),
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}

	onAccept := state.NewMockDiff(ctrl)
	blkTx := txsmock.NewUnsignedTx(ctrl)
	inputs := set.Of(ids.GenerateTestID())
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
	apricotBlk, err := block.NewApricotAtomicBlock(
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
	require.NoError(blk.Verify(context.Background()))

	require.Contains(manager.backend.blkIDToState, apricotBlk.ID())
	gotBlkState := manager.backend.blkIDToState[apricotBlk.ID()]
	require.Equal(apricotBlk, gotBlkState.statelessBlock)
	require.Equal(onAccept, gotBlkState.onAcceptState)
	require.Equal(inputs, gotBlkState.inputs)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	require.NoError(blk.Verify(context.Background()))
}

func TestVerifierVisitStandardBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempoolmock.NewMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := block.NewMockBlock(ctrl)
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
	manager := &manager{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				UpgradeConfig: upgradetest.GetConfig(upgradetest.ApricotPhasePost6),
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}

	blkTx := txsmock.NewUnsignedTx(ctrl)
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
			e.Inputs = set.Set[ids.ID]{}
			e.AtomicRequests = atomicRequests
			return nil
		},
	).Times(1)

	// We can't serialize [blkTx] because it isn't
	// registered with the blocks.Codec.
	// Serialize this block with a dummy tx
	// and replace it after creation with the mock tx.
	// TODO allow serialization of mock txs.
	apricotBlk, err := block.NewApricotStandardBlock(
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
	parentState.EXPECT().GetFeeState().Return(gas.State{}).Times(1)
	parentState.EXPECT().GetAccruedFees().Return(uint64(0)).Times(1)
	parentStatelessBlk.EXPECT().Height().Return(uint64(1)).Times(1)
	mempool.EXPECT().Remove(apricotBlk.Txs()).Times(1)

	blk := manager.NewBlock(apricotBlk)
	require.NoError(blk.Verify(context.Background()))

	// Assert expected state.
	require.Contains(manager.backend.blkIDToState, apricotBlk.ID())
	gotBlkState := manager.backend.blkIDToState[apricotBlk.ID()]
	require.Equal(apricotBlk, gotBlkState.statelessBlock)
	require.Equal(set.Set[ids.ID]{}, gotBlkState.inputs)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	require.NoError(blk.Verify(context.Background()))
}

func TestVerifierVisitCommitBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempoolmock.NewMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := block.NewMockBlock(ctrl)
	parentOnDecisionState := state.NewMockDiff(ctrl)
	parentOnCommitState := state.NewMockDiff(ctrl)
	parentOnAbortState := state.NewMockDiff(ctrl)

	backend := &backend{
		blkIDToState: map[ids.ID]*blockState{
			parentID: {
				statelessBlock: parentStatelessBlk,
				proposalBlockState: proposalBlockState{
					onDecisionState: parentOnDecisionState,
					onCommitState:   parentOnCommitState,
					onAbortState:    parentOnAbortState,
				},
			},
		},
		Mempool: mempool,
		state:   s,
		ctx: &snow.Context{
			Log: logging.NoLog{},
		},
	}
	manager := &manager{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				UpgradeConfig: upgradetest.GetConfig(upgradetest.ApricotPhasePost6),
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}

	apricotBlk, err := block.NewApricotCommitBlock(
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
	require.NoError(blk.Verify(context.Background()))

	// Assert expected state.
	require.Contains(manager.backend.blkIDToState, apricotBlk.ID())
	gotBlkState := manager.backend.blkIDToState[apricotBlk.ID()]
	require.Equal(parentOnAbortState, gotBlkState.onAcceptState)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	require.NoError(blk.Verify(context.Background()))
}

func TestVerifierVisitAbortBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempoolmock.NewMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := block.NewMockBlock(ctrl)
	parentOnDecisionState := state.NewMockDiff(ctrl)
	parentOnCommitState := state.NewMockDiff(ctrl)
	parentOnAbortState := state.NewMockDiff(ctrl)

	backend := &backend{
		blkIDToState: map[ids.ID]*blockState{
			parentID: {
				statelessBlock: parentStatelessBlk,
				proposalBlockState: proposalBlockState{
					onDecisionState: parentOnDecisionState,
					onCommitState:   parentOnCommitState,
					onAbortState:    parentOnAbortState,
				},
			},
		},
		Mempool: mempool,
		state:   s,
		ctx: &snow.Context{
			Log: logging.NoLog{},
		},
	}
	manager := &manager{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				UpgradeConfig: upgradetest.GetConfig(upgradetest.ApricotPhasePost6),
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}

	apricotBlk, err := block.NewApricotAbortBlock(
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
	require.NoError(blk.Verify(context.Background()))

	// Assert expected state.
	require.Contains(manager.backend.blkIDToState, apricotBlk.ID())
	gotBlkState := manager.backend.blkIDToState[apricotBlk.ID()]
	require.Equal(parentOnAbortState, gotBlkState.onAcceptState)
	require.Equal(timestamp, gotBlkState.timestamp)

	// Visiting again should return nil without using dependencies.
	require.NoError(blk.Verify(context.Background()))
}

// Assert that a block with an unverified parent fails verification.
func TestVerifyUnverifiedParent(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempoolmock.NewMempool(ctrl)
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
				UpgradeConfig: upgradetest.GetConfig(upgradetest.ApricotPhasePost6),
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}

	blk, err := block.NewApricotAbortBlock(parentID /*not in memory or persisted state*/, 2 /*height*/)
	require.NoError(err)

	// Set expectations for dependencies.
	s.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	s.EXPECT().GetStatelessBlock(parentID).Return(nil, database.ErrNotFound).Times(1)

	// Verify the block.
	err = blk.Visit(verifier)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestBanffAbortBlockTimestampChecks(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := genesistest.DefaultValidatorStartTime.Add(time.Hour)

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
			mempool := mempoolmock.NewMempool(ctrl)
			parentID := ids.GenerateTestID()
			parentStatelessBlk := block.NewMockBlock(ctrl)
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
						UpgradeConfig: upgradetest.GetConfig(upgradetest.Banff),
					},
					Clk: &mockable.Clock{},
				},
				backend: backend,
			}

			// build and verify child block
			childHeight := parentHeight + 1
			statelessAbortBlk, err := block.NewBanffAbortBlock(test.childTime, parentID, childHeight)
			require.NoError(err)

			// setup parent state
			parentTime := genesistest.DefaultValidatorStartTime
			s.EXPECT().GetLastAccepted().Return(parentID).Times(3)
			s.EXPECT().GetTimestamp().Return(parentTime).Times(3)
			s.EXPECT().GetFeeState().Return(gas.State{}).Times(3)
			s.EXPECT().GetAccruedFees().Return(uint64(0)).Times(3)

			onDecisionState, err := state.NewDiff(parentID, backend)
			require.NoError(err)
			onCommitState, err := state.NewDiff(parentID, backend)
			require.NoError(err)
			onAbortState, err := state.NewDiff(parentID, backend)
			require.NoError(err)
			backend.blkIDToState[parentID] = &blockState{
				timestamp:      test.parentTime,
				statelessBlock: parentStatelessBlk,
				proposalBlockState: proposalBlockState{
					onDecisionState: onDecisionState,
					onCommitState:   onCommitState,
					onAbortState:    onAbortState,
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

	now := genesistest.DefaultValidatorStartTime.Add(time.Hour)

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
			mempool := mempoolmock.NewMempool(ctrl)
			parentID := ids.GenerateTestID()
			parentStatelessBlk := block.NewMockBlock(ctrl)
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
						UpgradeConfig: upgradetest.GetConfig(upgradetest.Banff),
					},
					Clk: &mockable.Clock{},
				},
				backend: backend,
			}

			// build and verify child block
			childHeight := parentHeight + 1
			statelessCommitBlk, err := block.NewBanffCommitBlock(test.childTime, parentID, childHeight)
			require.NoError(err)

			// setup parent state
			parentTime := genesistest.DefaultValidatorStartTime
			s.EXPECT().GetLastAccepted().Return(parentID).Times(3)
			s.EXPECT().GetTimestamp().Return(parentTime).Times(3)
			s.EXPECT().GetFeeState().Return(gas.State{}).Times(3)
			s.EXPECT().GetAccruedFees().Return(uint64(0)).Times(3)

			onDecisionState, err := state.NewDiff(parentID, backend)
			require.NoError(err)
			onCommitState, err := state.NewDiff(parentID, backend)
			require.NoError(err)
			onAbortState, err := state.NewDiff(parentID, backend)
			require.NoError(err)
			backend.blkIDToState[parentID] = &blockState{
				timestamp:      test.parentTime,
				statelessBlock: parentStatelessBlk,
				proposalBlockState: proposalBlockState{
					onDecisionState: onDecisionState,
					onCommitState:   onCommitState,
					onAbortState:    onAbortState,
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

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempoolmock.NewMempool(ctrl)

	grandParentID := ids.GenerateTestID()
	grandParentStatelessBlk := block.NewMockBlock(ctrl)
	grandParentState := state.NewMockDiff(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := block.NewMockBlock(ctrl)
	parentState := state.NewMockDiff(ctrl)
	atomicInputs := set.Of(ids.GenerateTestID())

	backend := &backend{
		blkIDToState: map[ids.ID]*blockState{
			grandParentID: {
				statelessBlock: grandParentStatelessBlk,
				onAcceptState:  grandParentState,
				inputs:         atomicInputs,
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
				UpgradeConfig: upgradetest.GetConfig(upgradetest.ApricotPhasePost6),
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}

	blkTx := txsmock.NewUnsignedTx(ctrl)
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
	blk, err := block.NewApricotStandardBlock(
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
	parentState.EXPECT().GetFeeState().Return(gas.State{}).Times(1)
	parentState.EXPECT().GetAccruedFees().Return(uint64(0)).Times(1)
	parentStatelessBlk.EXPECT().Parent().Return(grandParentID).Times(1)

	err = verifier.ApricotStandardBlock(blk)
	require.ErrorIs(err, errConflictingParentTxs)
}

func TestVerifierVisitApricotStandardBlockWithProposalBlockParent(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempoolmock.NewMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := block.NewMockBlock(ctrl)
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
				UpgradeConfig: upgradetest.GetConfig(upgradetest.ApricotPhasePost6),
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}

	blk, err := block.NewApricotStandardBlock(
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

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	mempool := mempoolmock.NewMempool(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := block.NewMockBlock(ctrl)
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
				UpgradeConfig: upgradetest.GetConfig(upgradetest.Banff),
			},
			Clk: &mockable.Clock{},
		},
		backend: backend,
	}

	blk, err := block.NewBanffStandardBlock(
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

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := block.NewMockBlock(ctrl)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				UpgradeConfig: upgradetest.GetConfig(upgradetest.ApricotPhasePost6),
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

	blk, err := block.NewApricotCommitBlock(
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

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := block.NewMockBlock(ctrl)
	timestamp := time.Unix(12345, 0)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				UpgradeConfig: upgradetest.GetConfig(upgradetest.Banff),
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

	blk, err := block.NewBanffCommitBlock(
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

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := block.NewMockBlock(ctrl)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				UpgradeConfig: upgradetest.GetConfig(upgradetest.ApricotPhasePost6),
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

	blk, err := block.NewApricotAbortBlock(
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

	// Create mocked dependencies.
	s := state.NewMockState(ctrl)
	parentID := ids.GenerateTestID()
	parentStatelessBlk := block.NewMockBlock(ctrl)
	timestamp := time.Unix(12345, 0)
	verifier := &verifier{
		txExecutorBackend: &executor.Backend{
			Config: &config.Config{
				UpgradeConfig: upgradetest.GetConfig(upgradetest.Banff),
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

	blk, err := block.NewBanffAbortBlock(
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

func TestBlockExecutionWithComplexity(t *testing.T) {
	s := statetest.New(t, statetest.Config{})
	verifier := newTestVerifier(t, s)
	wallet := txstest.NewWallet(
		t,
		verifier.ctx,
		verifier.txExecutorBackend.Config,
		s,
		secp256k1fx.NewKeychain(genesis.EWOQKey),
		nil, // subnetIDs
		nil, // chainIDs
	)

	baseTx0, err := wallet.IssueBaseTx([]*avax.TransferableOutput{})
	require.NoError(t, err)
	baseTx1, err := wallet.IssueBaseTx([]*avax.TransferableOutput{})
	require.NoError(t, err)

	blockComplexity, err := fee.TxComplexity(baseTx0.Unsigned, baseTx1.Unsigned)
	require.NoError(t, err)
	blockGas, err := blockComplexity.ToGas(verifier.txExecutorBackend.Config.DynamicFeeConfig.Weights)
	require.NoError(t, err)

	tests := []struct {
		name             string
		timestamp        time.Time
		expectedErr      error
		expectedFeeState gas.State
	}{
		{
			name:        "no capacity",
			timestamp:   genesistest.DefaultValidatorStartTime,
			expectedErr: gas.ErrInsufficientCapacity,
		},
		{
			name:      "updates fee state",
			timestamp: genesistest.DefaultValidatorStartTime.Add(10 * time.Second),
			expectedFeeState: gas.State{
				Capacity: gas.Gas(0).AddPerSecond(verifier.txExecutorBackend.Config.DynamicFeeConfig.MaxPerSecond, 10) - blockGas,
				Excess:   blockGas,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Clear the state to prevent prior tests from impacting this test.
			clear(verifier.blkIDToState)

			verifier.txExecutorBackend.Clk.Set(test.timestamp)
			timestamp, _, err := state.NextBlockTime(s, verifier.txExecutorBackend.Clk)
			require.NoError(err)

			lastAcceptedID := s.GetLastAccepted()
			lastAccepted, err := s.GetStatelessBlock(lastAcceptedID)
			require.NoError(err)

			blk, err := block.NewBanffStandardBlock(
				timestamp,
				lastAcceptedID,
				lastAccepted.Height()+1,
				[]*txs.Tx{
					baseTx0,
					baseTx1,
				},
			)
			require.NoError(err)

			blkID := blk.ID()
			err = blk.Visit(verifier)
			require.ErrorIs(err, test.expectedErr)
			if err != nil {
				require.NotContains(verifier.blkIDToState, blkID)
				return
			}

			require.Contains(verifier.blkIDToState, blkID)
			onAcceptState := verifier.blkIDToState[blkID].onAcceptState
			require.Equal(test.expectedFeeState, onAcceptState.GetFeeState())
		})
	}
}
