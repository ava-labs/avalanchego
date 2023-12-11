// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func TestBlockBuilderAddLocalTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
		env.ctx.Lock.Unlock()
	}()

	// Create a valid transaction
	tx, err := env.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)
	txID := tx.ID()

	// Issue the transaction
	require.NoError(env.network.IssueTx(context.Background(), tx))
	require.True(env.mempool.Has(txID))

	// [BuildBlock] should build a block with the transaction
	blkIntf, err := env.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.IsType(&blockexecutor.Block{}, blkIntf)
	blk := blkIntf.(*blockexecutor.Block)
	require.Len(blk.Txs(), 1)
	require.Equal(txID, blk.Txs()[0].ID())

	// Mempool should not contain the transaction or have marked it as dropped
	require.False(env.mempool.Has(txID))
	require.NoError(env.mempool.GetDropReason(txID))
}

func TestPreviouslyDroppedTxsCanBeReAddedToMempool(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
		env.ctx.Lock.Unlock()
	}()

	// Create a valid transaction
	tx, err := env.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)
	txID := tx.ID()

	// Issue the transaction
	require.NoError(env.network.IssueTx(context.Background(), tx))
	require.True(env.mempool.Has(txID))

	// Transaction should not be marked as dropped when added to the mempool
	reason := env.mempool.GetDropReason(txID)
	require.NoError(reason)

	// Mark the transaction as dropped
	errTestingDropped := errors.New("testing dropped")
	env.mempool.MarkDropped(txID, errTestingDropped)
	reason = env.mempool.GetDropReason(txID)
	require.ErrorIs(reason, errTestingDropped)

	// Dropped transactions should still be in the mempool
	require.True(env.mempool.Has(txID))

	// Remove the transaction from the mempool
	env.mempool.Remove([]*txs.Tx{tx})

	// Issue the transaction again
	require.NoError(env.network.IssueTx(context.Background(), tx))
	require.True(env.mempool.Has(txID))

	// When issued again, the mempool should not be marked as dropped
	reason = env.mempool.GetDropReason(txID)
	require.NoError(reason)
}

func TestNoErrorOnUnexpectedSetPreferenceDuringBootstrapping(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	env.isBootstrapped.Set(false)
	defer func() {
		require.NoError(shutdownEnvironment(env))
		env.ctx.Lock.Unlock()
	}()

	require.True(env.blkManager.SetPreference(ids.GenerateTestID())) // should not panic
}

func TestGetNextStakerToReward(t *testing.T) {
	var (
		now  = time.Now()
		txID = ids.GenerateTestID()
	)

	type test struct {
		name                 string
		timestamp            time.Time
		stateF               func(*gomock.Controller) state.Chain
		expectedTxID         ids.ID
		expectedShouldReward bool
		expectedErr          error
	}

	tests := []test{
		{
			name:      "end of time",
			timestamp: mockable.MaxTime,
			stateF: func(ctrl *gomock.Controller) state.Chain {
				return state.NewMockChain(ctrl)
			},
			expectedErr: ErrEndOfTime,
		},
		{
			name:      "no stakers",
			timestamp: now,
			stateF: func(ctrl *gomock.Controller) state.Chain {
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				currentStakerIter.EXPECT().Next().Return(false)
				currentStakerIter.EXPECT().Release()

				s := state.NewMockChain(ctrl)
				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil)

				return s
			},
		},
		{
			name:      "expired subnet validator/delegator",
			timestamp: now,
			stateF: func(ctrl *gomock.Controller) state.Chain {
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(&state.Staker{
					Priority: txs.SubnetPermissionedValidatorCurrentPriority,
					EndTime:  now,
				})
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     txID,
					Priority: txs.SubnetPermissionlessDelegatorCurrentPriority,
					EndTime:  now,
				})
				currentStakerIter.EXPECT().Release()

				s := state.NewMockChain(ctrl)
				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil)

				return s
			},
			expectedTxID:         txID,
			expectedShouldReward: true,
		},
		{
			name:      "expired primary network validator after subnet expired subnet validator",
			timestamp: now,
			stateF: func(ctrl *gomock.Controller) state.Chain {
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(&state.Staker{
					Priority: txs.SubnetPermissionedValidatorCurrentPriority,
					EndTime:  now,
				})
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     txID,
					Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					EndTime:  now,
				})
				currentStakerIter.EXPECT().Release()

				s := state.NewMockChain(ctrl)
				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil)

				return s
			},
			expectedTxID:         txID,
			expectedShouldReward: true,
		},
		{
			name:      "expired primary network delegator after subnet expired subnet validator",
			timestamp: now,
			stateF: func(ctrl *gomock.Controller) state.Chain {
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(&state.Staker{
					Priority: txs.SubnetPermissionedValidatorCurrentPriority,
					EndTime:  now,
				})
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     txID,
					Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
					EndTime:  now,
				})
				currentStakerIter.EXPECT().Release()

				s := state.NewMockChain(ctrl)
				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil)

				return s
			},
			expectedTxID:         txID,
			expectedShouldReward: true,
		},
		{
			name:      "non-expired primary network delegator",
			timestamp: now,
			stateF: func(ctrl *gomock.Controller) state.Chain {
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     txID,
					Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
					EndTime:  now.Add(time.Second),
				})
				currentStakerIter.EXPECT().Release()

				s := state.NewMockChain(ctrl)
				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil)

				return s
			},
			expectedTxID:         txID,
			expectedShouldReward: false,
		},
		{
			name:      "non-expired primary network validator",
			timestamp: now,
			stateF: func(ctrl *gomock.Controller) state.Chain {
				currentStakerIter := state.NewMockStakerIterator(ctrl)

				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     txID,
					Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					EndTime:  now.Add(time.Second),
				})
				currentStakerIter.EXPECT().Release()

				s := state.NewMockChain(ctrl)
				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil)

				return s
			},
			expectedTxID:         txID,
			expectedShouldReward: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			state := tt.stateF(ctrl)
			txID, shouldReward, err := getNextStakerToReward(tt.timestamp, state)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expectedTxID, txID)
			require.Equal(tt.expectedShouldReward, shouldReward)
		})
	}
}

func TestBuildBlockDropExpiredStakerTxs(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
		env.ctx.Lock.Unlock()
	}()

	var (
		now                   = env.backend.Clk.Time()
		defaultValidatorStake = 100 * units.MilliAvax

		// Add a validator with StartTime in the future within [MaxFutureStartTime]
		validatorStartTime = now.Add(txexecutor.MaxFutureStartTime - 1*time.Second)
		validatorEndTime   = validatorStartTime.Add(360 * 24 * time.Hour)
	)

	tx1, err := env.txBuilder.NewAddValidatorTx(
		defaultValidatorStake,
		uint64(validatorStartTime.Unix()),
		uint64(validatorEndTime.Unix()),
		ids.GenerateTestNodeID(),
		preFundedKeys[0].PublicKey().Address(),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		preFundedKeys[0].PublicKey().Address(),
	)
	require.NoError(err)
	require.NoError(env.mempool.Add(tx1))
	tx1ID := tx1.ID()
	require.True(env.mempool.Has(tx1ID))

	// Add a validator with StartTime before current chain time
	validator2StartTime := now.Add(-5 * time.Second)
	validator2EndTime := validator2StartTime.Add(360 * 24 * time.Hour)

	tx2, err := env.txBuilder.NewAddValidatorTx(
		defaultValidatorStake,
		uint64(validator2StartTime.Unix()),
		uint64(validator2EndTime.Unix()),
		ids.GenerateTestNodeID(),
		preFundedKeys[1].PublicKey().Address(),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{preFundedKeys[1]},
		preFundedKeys[1].PublicKey().Address(),
	)
	require.NoError(err)
	require.NoError(env.mempool.Add(tx2))
	tx2ID := tx2.ID()
	require.True(env.mempool.Has(tx2ID))

	// Add a validator with StartTime in the future past [MaxFutureStartTime]
	validator3StartTime := now.Add(txexecutor.MaxFutureStartTime + 5*time.Second)
	validator3EndTime := validator2StartTime.Add(360 * 24 * time.Hour)

	tx3, err := env.txBuilder.NewAddValidatorTx(
		defaultValidatorStake,
		uint64(validator3StartTime.Unix()),
		uint64(validator3EndTime.Unix()),
		ids.GenerateTestNodeID(),
		preFundedKeys[2].PublicKey().Address(),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{preFundedKeys[2]},
		preFundedKeys[2].PublicKey().Address(),
	)
	require.NoError(err)
	require.NoError(env.mempool.Add(tx3))
	tx3ID := tx3.ID()
	require.True(env.mempool.Has(tx3ID))

	// Only tx1 should be in a built block
	blkIntf, err := env.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.IsType(&blockexecutor.Block{}, blkIntf)
	blk := blkIntf.(*blockexecutor.Block)
	require.Len(blk.Txs(), 1)
	require.Equal(tx1ID, blk.Txs()[0].ID())

	// Mempool should have none of the txs
	require.False(env.mempool.Has(tx1ID))
	require.False(env.mempool.Has(tx2ID))
	require.False(env.mempool.Has(tx3ID))

	// Only tx2 and tx3 should be dropped
	require.NoError(env.mempool.GetDropReason(tx1ID))

	tx2DropReason := env.mempool.GetDropReason(tx2ID)
	require.ErrorIs(tx2DropReason, txexecutor.ErrTimestampNotBeforeStartTime)

	tx3DropReason := env.mempool.GetDropReason(tx3ID)
	require.ErrorIs(tx3DropReason, txexecutor.ErrFutureStakeTime)
}
