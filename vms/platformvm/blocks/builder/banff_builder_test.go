// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	txbuilder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
)

func TestBanffFork(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	chainTime := env.state.GetTimestamp()
	env.clk.Set(chainTime)

	apricotTimes := []time.Time{
		chainTime.Add(1 * executor.SyncBound),
		chainTime.Add(2 * executor.SyncBound),
	}
	lastApricotTime := apricotTimes[len(apricotTimes)-1]
	env.config.BanffTime = lastApricotTime.Add(time.Second)

	for i, nextValidatorStartTime := range apricotTimes {
		// add a validator with the right start time
		// so that we can then advance chain time to it
		tx, err := env.txBuilder.NewAddValidatorTx(
			env.config.MinValidatorStake,
			uint64(nextValidatorStartTime.Unix()),
			uint64(defaultValidateEndTime.Unix()),
			ids.GenerateTestNodeID(),
			ids.GenerateTestShortID(),
			reward.PercentDenominator,
			[]*crypto.PrivateKeySECP256K1R{preFundedKeys[i]},
			ids.ShortEmpty,
		)
		require.NoError(err)
		require.NoError(env.mempool.Add(tx))

		proposalBlk, err := env.Builder.BuildBlock()
		require.NoError(err)
		require.NoError(proposalBlk.Verify())
		require.NoError(proposalBlk.Accept())
		require.NoError(env.state.Commit())

		options, err := proposalBlk.(snowman.OracleBlock).Options()
		require.NoError(err)
		commitBlk := options[0]
		require.NoError(commitBlk.Verify())
		require.NoError(commitBlk.Accept())
		require.NoError(env.state.Commit())
		env.Builder.SetPreference(commitBlk.ID())

		// advance chain time
		env.clk.Set(nextValidatorStartTime)
		advanceTimeBlk, err := env.Builder.BuildBlock()
		require.NoError(err)
		require.NoError(advanceTimeBlk.Verify())
		require.NoError(advanceTimeBlk.Accept())
		require.NoError(env.state.Commit())

		options, err = advanceTimeBlk.(snowman.OracleBlock).Options()
		require.NoError(err)
		commitBlk = options[0]
		require.NoError(commitBlk.Verify())
		require.NoError(commitBlk.Accept())
		require.NoError(env.state.Commit())
		env.Builder.SetPreference(commitBlk.ID())
	}

	// set local clock at banff time, so to try and build a banff block
	localTime := env.config.BanffTime
	env.clk.Set(localTime)

	createChainTx, err := env.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)
	require.NoError(env.mempool.Add(createChainTx))

	proposalBlk, err := env.Builder.BuildBlock()
	require.NoError(err)
	require.NoError(proposalBlk.Verify())
	require.NoError(proposalBlk.Accept())
	require.NoError(env.state.Commit())

	// check Banff fork is activated
	require.True(env.state.GetTimestamp().Equal(env.config.BanffTime))
}

func TestBuildBanffBlock(t *testing.T) {
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
				Validator: validator.Validator{
					// Shouldn't be dropped
					Start: uint64(now.Add(2 * executor.SyncBound).Unix()),
				},
				StakeOuts: []*avax.TransferableOutput{output},
				RewardsOwner: &secp256k1fx.OutputOwners{
					Addrs: []ids.ShortID{ids.GenerateTestShortID()},
				},
			},
			Creds: []verify.Verifiable{
				&secp256k1fx.Credential{
					Sigs: [][crypto.SECP256K1RSigLen]byte{{1, 3, 3, 7}},
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
		expectedBlkF     func(*require.Assertions) blocks.Block
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
			expectedBlkF: func(require *require.Assertions) blocks.Block {
				expectedBlk, err := blocks.NewBanffProposalBlock(
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

				// There are txs.
				mempool.EXPECT().HasStakerTx().Return(false)
				mempool.EXPECT().HasTxs().Return(true)
				mempool.EXPECT().PeekTxs(targetBlockSize).Return(transactions)
				return &builder{
					Mempool: mempool,
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
			expectedBlkF: func(require *require.Assertions) blocks.Block {
				expectedBlk, err := blocks.NewBanffStandardBlock(
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

				// There are no txs.
				mempool.EXPECT().HasStakerTx().Return(false)
				mempool.EXPECT().HasTxs().Return(false)

				clk := &mockable.Clock{}
				clk.Set(now)
				return &builder{
					Mempool: mempool,
					txExecutorBackend: &executor.Backend{
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
			expectedBlkF: func(require *require.Assertions) blocks.Block {
				return nil
			},
			expectedErr: errNoPendingBlocks,
		},
		{
			name: "should advance time",
			builderF: func(ctrl *gomock.Controller) *builder {
				mempool := mempool.NewMockMempool(ctrl)

				// There are no txs.
				mempool.EXPECT().HasStakerTx().Return(false)
				mempool.EXPECT().HasTxs().Return(false)
				mempool.EXPECT().PeekTxs(targetBlockSize).Return(nil)

				clk := &mockable.Clock{}
				clk.Set(now)
				return &builder{
					Mempool: mempool,
					txExecutorBackend: &executor.Backend{
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
			expectedBlkF: func(require *require.Assertions) blocks.Block {
				expectedBlk, err := blocks.NewBanffStandardBlock(
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

				// There is a tx.
				mempool.EXPECT().HasStakerTx().Return(false)
				mempool.EXPECT().HasTxs().Return(true)
				mempool.EXPECT().PeekTxs(targetBlockSize).Return([]*txs.Tx{transactions[0]})

				clk := &mockable.Clock{}
				clk.Set(now)
				return &builder{
					Mempool: mempool,
					txExecutorBackend: &executor.Backend{
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
			expectedBlkF: func(require *require.Assertions) blocks.Block {
				expectedBlk, err := blocks.NewBanffStandardBlock(
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

				// There are no decision txs
				// There is a staker tx.
				mempool.EXPECT().HasStakerTx().Return(false)
				mempool.EXPECT().HasTxs().Return(true)
				mempool.EXPECT().PeekTxs(targetBlockSize).Return([]*txs.Tx{transactions[0]})

				clk := &mockable.Clock{}
				clk.Set(now)
				return &builder{
					Mempool: mempool,
					txExecutorBackend: &executor.Backend{
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
			expectedBlkF: func(require *require.Assertions) blocks.Block {
				expectedBlk, err := blocks.NewBanffStandardBlock(
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
			defer ctrl.Finish()

			gotBlk, err := buildBanffBlock(
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
			require.EqualValues(tt.expectedBlkF(require), gotBlk)
		})
	}
}
