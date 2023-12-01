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
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	txbuilder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var errTestingDropped = errors.New("testing dropped")

// shows that a locally generated CreateChainTx can be added to mempool and then
// removed by inclusion in a block
func TestBlockBuilderAddLocalTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	// add a tx to it
	tx := getValidTx(env.txBuilder, t)
	txID := tx.ID()

	env.sender.SendAppGossipF = func(context.Context, []byte) error {
		return nil
	}
	require.NoError(env.network.IssueTx(context.Background(), tx))
	require.True(env.mempool.Has(txID))

	// show that build block include that tx and removes it from mempool
	blkIntf, err := env.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.IsType(&blockexecutor.Block{}, blkIntf)
	blk := blkIntf.(*blockexecutor.Block)
	require.Len(blk.Txs(), 1)
	require.Equal(txID, blk.Txs()[0].ID())

	require.False(env.mempool.Has(txID))
}

func TestPreviouslyDroppedTxsCanBeReAddedToMempool(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	// create candidate tx
	tx := getValidTx(env.txBuilder, t)
	txID := tx.ID()

	// A tx simply added to mempool is obviously not marked as dropped
	require.NoError(env.mempool.Add(tx))
	require.True(env.mempool.Has(txID))
	reason := env.mempool.GetDropReason(txID)
	require.NoError(reason)

	// When a tx is marked as dropped, it is still available to allow re-issuance
	env.mempool.MarkDropped(txID, errTestingDropped)
	require.True(env.mempool.Has(txID)) // still available
	reason = env.mempool.GetDropReason(txID)
	require.ErrorIs(reason, errTestingDropped)

	// A previously dropped tx, popped then re-added to mempool,
	// is not dropped anymore
	env.mempool.Remove([]*txs.Tx{tx})
	require.NoError(env.mempool.Add(tx))

	require.True(env.mempool.Has(txID))
	reason = env.mempool.GetDropReason(txID)
	require.NoError(reason)
}

func TestNoErrorOnUnexpectedSetPreferenceDuringBootstrapping(t *testing.T) {
	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	env.isBootstrapped.Set(false)
	env.ctx.Log = logging.NoWarn{}
	defer func() {
		require.NoError(t, shutdownEnvironment(env))
	}()

	require.False(t, env.blkManager.SetPreference(ids.GenerateTestID())) // should not panic
}

func TestGetNextStakerToReward(t *testing.T) {
	type test struct {
		name                 string
		timestamp            time.Time
		stateF               func(*gomock.Controller) state.Chain
		expectedTxID         ids.ID
		expectedShouldReward bool
		expectedErr          error
	}

	var (
		now  = time.Now()
		txID = ids.GenerateTestID()
	)
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

func TestBuildBlock(t *testing.T) {
	var (
		parentID = ids.GenerateTestID()
		height   = uint64(1337)
		output   = &avax.TransferableOutput{
			Asset: avax.Asset{ID: ids.GenerateTestID()},
			Out: &secp256k1fx.TransferOutput{
				OutputOwners: secp256k1fx.OutputOwners{
					Addrs: []ids.ShortID{ids.GenerateTestShortID()},
				},
			},
		}
		now             = time.Now()
		parentTimestamp = now.Add(-2 * time.Second)
		transactions    = []*txs.Tx{{
			Unsigned: &txs.AddValidatorTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: []*avax.TransferableInput{{
						Asset: avax.Asset{ID: ids.GenerateTestID()},
						In: &secp256k1fx.TransferInput{
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					}},
					Outs: []*avax.TransferableOutput{output},
				}},
				Validator: txs.Validator{
					NodeID: ids.GenerateTestNodeID(),
					// Shouldn't be dropped
					Start: uint64(now.Add(2 * txexecutor.SyncBound).Unix()),
				},
				StakeOuts: []*avax.TransferableOutput{output},
				RewardsOwner: &secp256k1fx.OutputOwners{
					Addrs: []ids.ShortID{ids.GenerateTestShortID()},
				},
			},
			Creds: []verify.Verifiable{
				&secp256k1fx.Credential{
					Sigs: [][secp256k1.SignatureLen]byte{{1, 3, 3, 7}},
				},
			},
		}}
		stakerTxID = ids.GenerateTestID()
	)

	type test struct {
		name             string
		builderF         func(*gomock.Controller) *builder
		timestamp        time.Time
		forceAdvanceTime bool
		parentStateF     func(*gomock.Controller) state.Chain
		expectedBlkF     func(*require.Assertions) block.Block
		expectedErr      error
	}

	tests := []test{
		{
			name: "should reward",
			builderF: func(ctrl *gomock.Controller) *builder {
				mempool := mempool.NewMockMempool(ctrl)

				// The tx builder should be asked to build a reward tx
				txBuilder := txbuilder.NewMockBuilder(ctrl)
				txBuilder.EXPECT().NewRewardValidatorTx(stakerTxID).Return(transactions[0], nil)

				return &builder{
					Mempool:   mempool,
					txBuilder: txBuilder,
				}
			},
			timestamp:        parentTimestamp,
			forceAdvanceTime: false,
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)

				// add current validator that ends at [parentTimestamp]
				// i.e. it should be rewarded
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     stakerTxID,
					Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
					EndTime:  parentTimestamp,
				})
				currentStakerIter.EXPECT().Release()

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil)
				return s
			},
			expectedBlkF: func(require *require.Assertions) block.Block {
				expectedBlk, err := block.NewBanffProposalBlock(
					parentTimestamp,
					parentID,
					height,
					transactions[0],
				)
				require.NoError(err)
				return expectedBlk
			},
			expectedErr: nil,
		},
		{
			name: "has decision txs",
			builderF: func(ctrl *gomock.Controller) *builder {
				mempool := mempool.NewMockMempool(ctrl)
				manager := blockexecutor.NewMockManager(ctrl)
				chain := state.NewMockChain(ctrl)

				preferredID := ids.GenerateTestID()
				manager.EXPECT().Preferred().Return(preferredID)
				manager.EXPECT().GetState(preferredID).Return(chain, true).AnyTimes()
				chain.EXPECT().GetTimestamp().Return(parentTimestamp)

				pendingStakerIter := state.NewMockStakerIterator(ctrl)
				pendingStakerIter.EXPECT().Next().Return(false)
				pendingStakerIter.EXPECT().Release()
				chain.EXPECT().GetPendingStakerIterator().Return(pendingStakerIter, nil)

				currentStakerIter := state.NewMockStakerIterator(ctrl)
				currentStakerIter.EXPECT().Next().Return(false)
				currentStakerIter.EXPECT().Release()
				chain.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).Times(1)

				gomock.InOrder(
					mempool.EXPECT().Peek(targetBlockSize).Return(transactions[0]),
					mempool.EXPECT().Remove([]*txs.Tx{transactions[0]}),
				)

				return &builder{
					Mempool:    mempool,
					blkManager: manager,
					txExecutorBackend: &txexecutor.Backend{
						Ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
				}
			},
			timestamp:        parentTimestamp,
			forceAdvanceTime: false,
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)

				// Handle calls in [getNextStakerToReward]
				// and [GetNextStakerChangeTime].
				// Next validator change time is in the future.
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				gomock.InOrder(
					// expect calls from [getNextStakerToReward]
					currentStakerIter.EXPECT().Next().Return(true),
					currentStakerIter.EXPECT().Value().Return(&state.Staker{
						NextTime: now.Add(time.Second),
						Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
					}),
					currentStakerIter.EXPECT().Release(),
				)

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).Times(1)
				return s
			},
			expectedBlkF: func(require *require.Assertions) block.Block {
				expectedBlk, err := block.NewBanffStandardBlock(
					parentTimestamp,
					parentID,
					height,
					transactions,
				)
				require.NoError(err)
				return expectedBlk
			},
			expectedErr: nil,
		},
		{
			name: "no stakers tx",
			builderF: func(ctrl *gomock.Controller) *builder {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Peek(gomock.Any()).Return(nil)

				clk := &mockable.Clock{}
				clk.Set(now)
				return &builder{
					Mempool: mempool,
					txExecutorBackend: &txexecutor.Backend{
						Ctx: &snow.Context{
							Log: logging.NoLog{},
						},
						Clk: clk,
					},
				}
			},
			timestamp:        parentTimestamp,
			forceAdvanceTime: false,
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)

				// Handle calls in [getNextStakerToReward]
				// and [GetNextStakerChangeTime].
				// Next validator change time is in the future.
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				gomock.InOrder(
					// expect calls from [getNextStakerToReward]
					currentStakerIter.EXPECT().Next().Return(true),
					currentStakerIter.EXPECT().Value().Return(&state.Staker{
						NextTime: now.Add(time.Second),
						Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
					}),
					currentStakerIter.EXPECT().Release(),
				)

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).Times(1)
				return s
			},
			expectedBlkF: func(*require.Assertions) block.Block {
				return nil
			},
			expectedErr: ErrNoPendingBlocks,
		},
		{
			name: "should advance time",
			builderF: func(ctrl *gomock.Controller) *builder {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Peek(gomock.Any()).Return(nil)

				clk := &mockable.Clock{}
				clk.Set(now)
				return &builder{
					Mempool: mempool,
					txExecutorBackend: &txexecutor.Backend{
						Clk: clk,
					},
				}
			},
			timestamp:        now.Add(-1 * time.Second),
			forceAdvanceTime: true,
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)

				// add current validator that ends at [now] - 1 second.
				// That is, it ends in the past but after the current chain time.
				// Handle calls in [getNextStakerToReward]
				// and [GetNextStakerChangeTime]
				// when determining whether to issue a reward tx.
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				gomock.InOrder(
					// expect calls from [getNextStakerToReward]
					currentStakerIter.EXPECT().Next().Return(true),
					currentStakerIter.EXPECT().Value().Return(&state.Staker{
						NextTime: now.Add(-1 * time.Second),
						Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
					}),
					currentStakerIter.EXPECT().Release(),
				)

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).Times(1)
				return s
			},
			expectedBlkF: func(require *require.Assertions) block.Block {
				expectedBlk, err := block.NewBanffStandardBlock(
					now.Add(-1*time.Second), // note the advanced time
					parentID,
					height,
					nil, // empty block to advance time
				)
				require.NoError(err)
				return expectedBlk
			},
			expectedErr: nil,
		},
		{
			name: "has a staker tx no force",
			builderF: func(ctrl *gomock.Controller) *builder {
				mempool := mempool.NewMockMempool(ctrl)

				gomock.InOrder(
					mempool.EXPECT().Peek(targetBlockSize).Return(transactions[0]),
					mempool.EXPECT().Remove([]*txs.Tx{transactions[0]}),
					// Second loop iteration
					mempool.EXPECT().Peek(gomock.Any()).Return(nil),
				)

				clk := &mockable.Clock{}
				clk.Set(now)
				return &builder{
					Mempool: mempool,
					txExecutorBackend: &txexecutor.Backend{
						Clk: clk,
					},
				}
			},
			timestamp:        parentTimestamp,
			forceAdvanceTime: false,
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)

				// Handle calls in [getNextStakerToReward]
				// and [GetNextStakerChangeTime].
				// Next validator change time is in the future.
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				gomock.InOrder(
					// expect calls from [getNextStakerToReward]
					currentStakerIter.EXPECT().Next().Return(true),
					currentStakerIter.EXPECT().Value().Return(&state.Staker{
						NextTime: now.Add(time.Second),
						Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
					}),
					currentStakerIter.EXPECT().Release(),
				)

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).Times(1)
				return s
			},
			expectedBlkF: func(require *require.Assertions) block.Block {
				expectedBlk, err := block.NewBanffStandardBlock(
					parentTimestamp,
					parentID,
					height,
					[]*txs.Tx{transactions[0]},
				)
				require.NoError(err)
				return expectedBlk
			},
			expectedErr: nil,
		},
		{
			name: "has a staker tx with force",
			builderF: func(ctrl *gomock.Controller) *builder {
				mempool := mempool.NewMockMempool(ctrl)

				gomock.InOrder(
					mempool.EXPECT().Peek(targetBlockSize).Return(transactions[0]),
					mempool.EXPECT().Remove([]*txs.Tx{transactions[0]}),
					// Second loop iteration
					mempool.EXPECT().Peek(gomock.Any()).Return(nil),
				)

				clk := &mockable.Clock{}
				clk.Set(now)
				return &builder{
					Mempool: mempool,
					txExecutorBackend: &txexecutor.Backend{
						Clk: clk,
					},
				}
			},
			timestamp:        parentTimestamp,
			forceAdvanceTime: true,
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)

				// Handle calls in [getNextStakerToReward]
				// and [GetNextStakerChangeTime].
				// Next validator change time is in the future.
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				gomock.InOrder(
					// expect calls from [getNextStakerToReward]
					currentStakerIter.EXPECT().Next().Return(true),
					currentStakerIter.EXPECT().Value().Return(&state.Staker{
						NextTime: now.Add(time.Second),
						Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
					}),
					currentStakerIter.EXPECT().Release(),
				)

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).Times(1)
				return s
			},
			expectedBlkF: func(require *require.Assertions) block.Block {
				expectedBlk, err := block.NewBanffStandardBlock(
					parentTimestamp,
					parentID,
					height,
					[]*txs.Tx{transactions[0]},
				)
				require.NoError(err)
				return expectedBlk
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			gotBlk, err := buildBlock(
				tt.builderF(ctrl),
				parentID,
				height,
				tt.timestamp,
				tt.forceAdvanceTime,
				tt.parentStateF(ctrl),
			)
			if tt.expectedErr != nil {
				require.ErrorIs(err, tt.expectedErr)
				return
			}
			require.NoError(err)
			require.Equal(tt.expectedBlkF(require), gotBlk)
		})
	}
}

func TestBuildBlockDropExpiredStakerTxs(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	env.sender.SendAppGossipF = func(context.Context, []byte) error {
		return nil
	}

	now := env.backend.Clk.Time()

	defaultValidatorStake := 100 * units.MilliAvax

	// Add a validator with StartTime in the future within [MaxFutureStartTime]
	validatorStartTime := now.Add(txexecutor.MaxFutureStartTime - 1*time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

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

	require.NoError(env.mempool.GetDropReason(tx1ID))

	tx2DropReason := env.mempool.GetDropReason(tx2ID)
	require.ErrorIs(tx2DropReason, txexecutor.ErrTimestampNotBeforeStartTime)

	tx3DropReason := env.mempool.GetDropReason(tx3ID)
	require.ErrorIs(tx3DropReason, txexecutor.ErrFutureStakeTime)
}
