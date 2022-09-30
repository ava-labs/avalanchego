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
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	txbuilder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func TestBuildApricotBlock(t *testing.T) {
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
		blockTxs        = []*txs.Tx{{
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
					Start: uint64(now.Add(2 * txexecutor.SyncBound).Unix()),
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
		name              string
		builderF          func(*gomock.Controller) *builder
		timestamp         time.Time
		shouldAdvanceTime bool
		parentStateF      func(*gomock.Controller) state.Chain
		expectedBlkF      func(*require.Assertions) blocks.Block
		expectedErr       error
	}

	tests := []test{
		{
			name: "has decision txs",
			builderF: func(ctrl *gomock.Controller) *builder {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().HasApricotDecisionTxs().Return(true)
				mempool.EXPECT().PeekApricotDecisionTxs(targetBlockSize).Return(blockTxs)
				return &builder{
					Mempool: mempool,
				}
			},
			timestamp:         time.Time{},
			shouldAdvanceTime: false,
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				return state.NewMockChain(ctrl)
			},
			expectedBlkF: func(require *require.Assertions) blocks.Block {
				expectedBlk, err := blocks.NewApricotStandardBlock(
					parentID,
					height,
					blockTxs,
				)
				require.NoError(err)
				return expectedBlk
			},
			expectedErr: nil,
		},
		{
			name: "should reward",
			builderF: func(ctrl *gomock.Controller) *builder {
				// There are no decision txs
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().HasApricotDecisionTxs().Return(false)

				// The tx builder should be asked to build a reward tx
				txBuilder := txbuilder.NewMockBuilder(ctrl)
				txBuilder.EXPECT().NewRewardValidatorTx(stakerTxID).Return(blockTxs[0], nil)

				return &builder{
					Mempool:   mempool,
					txBuilder: txBuilder,
				}
			},
			timestamp:         time.Time{},
			shouldAdvanceTime: false,
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)

				s.EXPECT().GetTimestamp().Return(parentTimestamp)

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
				expectedBlk, err := blocks.NewApricotProposalBlock(
					parentID,
					height,
					blockTxs[0],
				)
				require.NoError(err)
				return expectedBlk
			},
			expectedErr: nil,
		},
		{
			name: "should advance time",
			builderF: func(ctrl *gomock.Controller) *builder {
				// There are no decision txs
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().HasApricotDecisionTxs().Return(false)

				// The tx builder should be asked to build an advance time tx
				advanceTimeTx := &txs.Tx{Unsigned: &txs.AdvanceTimeTx{
					Time: uint64(now.Add(-1 * time.Second).Unix()),
				}}
				txBuilder := txbuilder.NewMockBuilder(ctrl)
				txBuilder.EXPECT().NewAdvanceTimeTx(now.Add(-1*time.Second)).Return(advanceTimeTx, nil)

				clk := &mockable.Clock{}
				clk.Set(now)
				return &builder{
					Mempool:   mempool,
					txBuilder: txBuilder,
					txExecutorBackend: &txexecutor.Backend{
						Clk: clk,
					},
				}
			},
			timestamp:         now.Add(-1 * time.Second),
			shouldAdvanceTime: true,
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)

				s.EXPECT().GetTimestamp().Return(parentTimestamp)

				// add current validator that ends at [now] - 1 second.
				// Handle calls in [getNextStakerToReward]
				// and [GetNextStakerChangeTime]
				// when determining whether to issue a reward tx.
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				gomock.InOrder(
					// expect calls from [getNextStakerToReward]
					currentStakerIter.EXPECT().Next().Return(true),
					currentStakerIter.EXPECT().Value().Return(&state.Staker{
						NextTime: now.Add(-1 * time.Second),
						Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					}),
					currentStakerIter.EXPECT().Release(),
				)

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).Times(1)
				return s
			},
			expectedBlkF: func(require *require.Assertions) blocks.Block {
				expectedBlk, err := blocks.NewApricotProposalBlock(
					parentID,
					height,
					&txs.Tx{Unsigned: &txs.AdvanceTimeTx{ // advances time
						Time: uint64(now.Add(-1 * time.Second).Unix()),
					}},
				)
				require.NoError(err)
				return expectedBlk
			},
			expectedErr: nil,
		},
		{
			name: "no proposal tx",
			builderF: func(ctrl *gomock.Controller) *builder {
				// There are no decision txs
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().HasApricotDecisionTxs().Return(false)

				// There is a staker tx.
				mempool.EXPECT().HasStakerTx().Return(false).AnyTimes()

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
			timestamp:         time.Time{},
			shouldAdvanceTime: false,
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)

				s.EXPECT().GetTimestamp().Return(parentTimestamp)

				// Next validator change time is in the future.
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				gomock.InOrder(
					// expect calls from [getNextStakerToReward]
					currentStakerIter.EXPECT().Next().Return(true),
					currentStakerIter.EXPECT().Value().Return(&state.Staker{
						NextTime: now.Add(time.Second),
						Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					}),
					currentStakerIter.EXPECT().Release(),
				)

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).Times(1)
				return s
			},
			expectedBlkF: func(require *require.Assertions) blocks.Block {
				expectedBlk, err := blocks.NewApricotProposalBlock(
					parentID,
					height,
					blockTxs[0],
				)
				require.NoError(err)
				return expectedBlk
			},
			expectedErr: errNoPendingBlocks,
		},
		{
			name: "has a proposal tx",
			builderF: func(ctrl *gomock.Controller) *builder {
				// There are no decision txs
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().HasApricotDecisionTxs().Return(false)

				// There is a proposal tx.
				mempool.EXPECT().HasStakerTx().Return(true).AnyTimes()
				mempool.EXPECT().PeekStakerTx().Return(blockTxs[0]).AnyTimes()

				clk := &mockable.Clock{}
				clk.Set(now)
				return &builder{
					Mempool: mempool,
					txExecutorBackend: &txexecutor.Backend{
						Clk: clk,
					},
				}
			},
			timestamp:         time.Time{},
			shouldAdvanceTime: false,
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)

				// Once in [buildBanffBlock], once in [GetNextStakerChangeTime],
				s.EXPECT().GetTimestamp().Return(parentTimestamp).Times(2)

				// Handle calls in [getNextStakerToReward]
				// and [GetNextStakerChangeTime].
				// Next validator change time is in the future.
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				gomock.InOrder(
					// expect calls from [getNextStakerToReward]
					currentStakerIter.EXPECT().Next().Return(true),
					currentStakerIter.EXPECT().Value().Return(&state.Staker{
						NextTime: now.Add(time.Second),
						Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					}),
					currentStakerIter.EXPECT().Release(),
				)

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).Times(1)
				return s
			},
			expectedBlkF: func(require *require.Assertions) blocks.Block {
				expectedBlk, err := blocks.NewApricotProposalBlock(
					parentID,
					height,
					blockTxs[0],
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

			gotBlk, err := buildApricotBlock(
				tt.builderF(ctrl),
				parentID,
				height,
				tt.timestamp,
				tt.shouldAdvanceTime,
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
