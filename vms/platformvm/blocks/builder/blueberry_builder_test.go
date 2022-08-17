// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	txbuilder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

func TestBlueberryPickingOrder(t *testing.T) {
	require := require.New(t)

	// mock ResetBlockTimer to control timing of block formation
	env := newEnvironment(t, true /*mockResetBlockTimer*/)
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.config.BlueberryTime = time.Time{}

	chainTime := env.state.GetTimestamp()
	now := chainTime.Add(time.Second)
	env.clk.Set(now)

	nextChainTime := chainTime.Add(env.config.MinStakeDuration).Add(time.Hour)

	// create validator
	validatorStartTime := now.Add(time.Second)
	validatorTx, err := createTestValidatorTx(env, validatorStartTime, nextChainTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
	require.NoError(err)

	onAbortState, err := state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
	require.NoError(err)

	// accept validator as pending
	txExecutor := txexecutor.ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            validatorTx,
	}
	require.NoError(validatorTx.Unsigned.Visit(&txExecutor))
	txExecutor.OnCommitState.AddTx(validatorTx, status.Committed)
	txExecutor.OnCommitState.Apply(env.state)
	require.NoError(env.state.Commit())

	// promote validator to current
	// Reset onCommitState and onAbortState
	txExecutor.OnCommitState, err = state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
	require.NoError(err)
	txExecutor.OnAbortState, err = state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
	require.NoError(err)
	advanceTime, err := env.txBuilder.NewAdvanceTimeTx(validatorStartTime)
	require.NoError(err)
	txExecutor.Tx = advanceTime
	require.NoError(advanceTime.Unsigned.Visit(&txExecutor))
	txExecutor.OnCommitState.Apply(env.state)
	require.NoError(env.state.Commit())

	// move chain time to current validator's
	// end of staking time, so that it may be rewarded
	env.state.SetTimestamp(nextChainTime)
	now = nextChainTime
	env.clk.Set(now)

	// add decisionTx and stakerTxs to mempool
	decisionTxs, err := createTestDecisionTxes(2)
	require.NoError(err)
	for _, dt := range decisionTxs {
		require.NoError(env.mempool.Add(dt))
	}

	// start time is beyond maximal distance from chain time
	starkerTxStartTime := nextChainTime.Add(txexecutor.MaxFutureStartTime).Add(time.Second)
	stakerTx, err := createTestValidatorTx(env, starkerTxStartTime, starkerTxStartTime.Add(time.Hour))
	require.NoError(err)
	require.NoError(env.mempool.Add(stakerTx))

	// test: decisionTxs must be picked first
	blk, err := env.Builder.BuildBlock()
	require.NoError(err)
	require.True(blk.Timestamp().Equal(nextChainTime))
	stdBlk, ok := blk.(*blockexecutor.Block)
	require.True(ok)
	_, ok = stdBlk.Block.(*blocks.BlueberryStandardBlock)
	require.True(ok)
	require.True(len(decisionTxs) == len(stdBlk.Txs()))
	for i, tx := range stdBlk.Txs() {
		require.Equal(decisionTxs[i].ID(), tx.ID())
	}

	require.False(env.mempool.HasDecisionTxs())

	// test: reward validator blocks must follow, one per endingValidator
	blk, err = env.Builder.BuildBlock()
	require.NoError(err)
	require.True(blk.Timestamp().Equal(nextChainTime))
	rewardBlk, ok := blk.(*blockexecutor.Block)
	require.True(ok)
	_, ok = rewardBlk.Block.(*blocks.BlueberryProposalBlock)
	require.True(ok)
	rewardTx, ok := rewardBlk.Txs()[0].Unsigned.(*txs.RewardValidatorTx)
	require.True(ok)
	require.Equal(validatorTx.ID(), rewardTx.TxID)

	// accept reward validator tx so that current validator is removed
	require.NoError(blk.Verify())
	require.NoError(blk.Accept())
	options, err := rewardBlk.Options()
	require.NoError(err)
	commitBlk := options[0]
	require.NoError(commitBlk.Verify())
	require.NoError(commitBlk.Accept())
	env.Builder.SetPreference(commitBlk.ID())

	// mempool proposal tx is too far in the future. A
	// proposal block including mempool proposalTx
	// will be issued to advance time and
	now = nextChainTime.Add(txexecutor.MaxFutureStartTime / 2)
	env.clk.Set(now)
	blk, err = env.Builder.BuildBlock()
	require.NoError(err)
	require.True(blk.Timestamp().Equal(now))
	proposalBlk, ok := blk.(*blockexecutor.Block)
	require.True(ok)
	_, ok = proposalBlk.Block.(*blocks.BlueberryProposalBlock)
	require.True(ok)
	require.Equal(stakerTx.ID(), proposalBlk.Txs()[0].ID())

	// Finally an empty standard block can be issued to advance time
	// if no mempool txs are available
	now, err = txexecutor.GetNextStakerChangeTime(env.state)
	require.NoError(err)
	env.clk.Set(now)

	// finally mempool addValidatorTx must be picked
	blk, err = env.Builder.BuildBlock()
	require.NoError(err)
	require.True(blk.Timestamp().Equal(now))
	emptyStdBlk, ok := blk.(*blockexecutor.Block)
	require.True(ok)
	_, ok = emptyStdBlk.Block.(*blocks.BlueberryStandardBlock)
	require.True(ok)
	require.True(len(emptyStdBlk.Txs()) == 0)
}

func TestBuildBlueberryBlock(t *testing.T) {
	var (
		parentID = ids.GenerateTestID()
		height   = uint64(1337)
		txs      = []*txs.Tx{
			{
				Unsigned: &txs.AdvanceTimeTx{
					Time: uint64(123),
				},
				Creds: []verify.Verifiable{
					&secp256k1fx.Credential{
						Sigs: [][crypto.SECP256K1RSigLen]byte{{1, 3, 3, 7}},
					},
				},
			},
		}
		now        = time.Now()
		stakerTxID = ids.GenerateTestID()
	)

	type test struct {
		name         string
		builderF     func(*gomock.Controller) *builder
		parentStateF func(*gomock.Controller) state.Chain
		expectedBlkF func() blocks.Block
		shouldErr    bool
	}

	tests := []test{
		{
			name: "has decision txs",
			builderF: func(ctrl *gomock.Controller) *builder {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().HasDecisionTxs().Return(true)
				mempool.EXPECT().PeekDecisionTxs(targetBlockSize).Return(txs)
				return &builder{
					Mempool: mempool,
				}
			},
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)
				s.EXPECT().GetTimestamp().Return(now)
				return s
			},
			expectedBlkF: func() blocks.Block {
				expectedBlk, _ := blocks.NewBlueberryStandardBlock(
					now,
					parentID,
					height,
					txs,
				)
				return expectedBlk
			},
			shouldErr: false,
		},
		{
			name: "should reward",
			builderF: func(ctrl *gomock.Controller) *builder {
				// There are no decision txs
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().HasDecisionTxs().Return(false)

				// The tx builder should be asked to build a reward tx
				txBuilder := txbuilder.NewMockBuilder(ctrl)
				txBuilder.EXPECT().NewRewardValidatorTx(stakerTxID).Return(txs[0], nil)

				return &builder{
					Mempool:   mempool,
					txBuilder: txBuilder,
				}
			},
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)

				// Once in [buildBlueberryBlock], once in [getNextStakerToReward]
				s.EXPECT().GetTimestamp().Return(now).Times(2)

				// add current validator that ends at [now]
				// i.e. it should be rewarded
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				currentStakerIter.EXPECT().Next().Return(true)
				currentStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     stakerTxID,
					Priority: state.PrimaryNetworkDelegatorCurrentPriority,
					EndTime:  now,
				})
				currentStakerIter.EXPECT().Release()

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil)

				return s
			},
			expectedBlkF: func() blocks.Block {
				expectedBlk, _ := blocks.NewBlueberryProposalBlock(
					now,
					parentID,
					height,
					txs[0],
				)
				return expectedBlk
			},
			shouldErr: false,
		},
		{
			name: "should advance time",
			builderF: func(ctrl *gomock.Controller) *builder {
				// There are no decision txs
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().HasDecisionTxs().Return(false)

				clk := &mockable.Clock{}
				clk.Set(now)
				return &builder{
					Mempool: mempool,
					txExecutorBackend: &txexecutor.Backend{
						Clk: clk,
					},
				}
			},
			parentStateF: func(ctrl *gomock.Controller) state.Chain {
				s := state.NewMockChain(ctrl)

				// Once in [buildBlueberryBlock], once in [GetNextStakerChangeTime]
				s.EXPECT().GetTimestamp().Return(now).Times(2)

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
					}),
					currentStakerIter.EXPECT().Next().Return(false),
					currentStakerIter.EXPECT().Release(),

					// expect calls from [GetNextStakerChangeTime]
					currentStakerIter.EXPECT().Next().Return(true),
					currentStakerIter.EXPECT().Value().Return(&state.Staker{
						NextTime: now.Add(-1 * time.Second),
					}),
					currentStakerIter.EXPECT().Release(),
				)

				// We also iterate over the pending stakers in [getNextStakerToReward]
				pendingStakerIter := state.NewMockStakerIterator(ctrl)
				pendingStakerIter.EXPECT().Next().Return(false)
				pendingStakerIter.EXPECT().Release()

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).Times(2)
				s.EXPECT().GetPendingStakerIterator().Return(pendingStakerIter, nil)
				return s
			},
			expectedBlkF: func() blocks.Block {
				expectedBlk, _ := blocks.NewBlueberryStandardBlock(
					now.Add(-1*time.Second),
					parentID,
					height,
					nil, // empty block to advance time
				)
				return expectedBlk
			},
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			gotBlk, err := buildBlueberryBlock(
				tt.builderF(ctrl),
				parentID,
				height,
				tt.parentStateF(ctrl),
			)
			if tt.shouldErr {
				require.Error(err)
				return
			}
			require.NoError(err)
			require.EqualValues(tt.expectedBlkF(), gotBlk)
		})
	}
}
