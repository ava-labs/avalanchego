// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/message"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	txbuilder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var errTestingDropped = errors.New("testing dropped")

// shows that a locally generated CreateChainTx can be added to mempool and then
// removed by inclusion in a block
func TestBlockBuilderAddLocalTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.Ctx.Lock.Lock()
	defer func() {
		env.Ctx.Lock.Unlock()
		require.NoError(env.Shutdown())
	}()

	// add a tx to it
	tx := env.getValidTx(t)
	txID := tx.ID()

	env.sender.SendAppGossipF = func(context.Context, []byte) error {
		return nil
	}

	err := env.Builder.AddUnverifiedTx(tx)
	require.NoError(err)

	has := env.mempool.Has(txID)
	require.True(has)

	// show that build block include that tx and removes it from mempool
	blkIntf, err := env.Builder.BuildBlock(context.Background())
	require.NoError(err)

	blk, ok := blkIntf.(*blockexecutor.Block)
	require.True(ok)
	require.Len(blk.Txs(), 1)
	require.Equal(txID, blk.Txs()[0].ID())

	has = env.mempool.Has(txID)
	require.False(has)
}

func TestPreviouslyDroppedTxsCanBeReAddedToMempool(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.Ctx.Lock.Lock()
	defer func() {
		env.Ctx.Lock.Unlock()
		require.NoError(env.Shutdown())
	}()

	env.sender.SendAppGossipF = func(_ context.Context, b []byte) error {
		return nil
	}

	// create candidate tx
	tx := env.getValidTx(t)
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
	require.NoError(env.Builder.AddUnverifiedTx(tx))

	require.True(env.mempool.Has(txID))
	reason = env.mempool.GetDropReason(txID)
	require.NoError(reason)
}

func TestNoErrorOnUnexpectedSetPreferenceDuringBootstrapping(t *testing.T) {
	env := newEnvironment(t)
	env.isBootstrapped.Set(false)
	env.Ctx.Lock.Lock()
	env.Ctx.Log = logging.NoWarn{}
	defer func() {
		env.Ctx.Lock.Unlock()
		require.NoError(t, env.Shutdown())
	}()

	env.Builder.SetPreference(ids.GenerateTestID()) // should not panic
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
			expectedErr: errEndOfTime,
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
			defer ctrl.Finish()

			state := tt.stateF(ctrl)
			txID, shouldReward, err := getNextStakerToReward(tt.timestamp, state)
			if tt.expectedErr != nil {
				require.Equal(tt.expectedErr, err)
				return
			}
			require.NoError(err)
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
			expectedBlkF: func(*require.Assertions) blocks.Block {
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
			require.EqualValues(tt.expectedBlkF(require), gotBlk)
		})
	}
}

func TestAtomicTxImports(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.Ctx.Lock.Lock()

	defer func() {
		env.Ctx.Lock.Unlock()
		require.NoError(env.Shutdown())
	}()

	utxoID := avax.UTXOID{
		TxID:        ids.Empty.Prefix(1),
		OutputIndex: 1,
	}
	amount := uint64(70000)
	recipientKey := preFundedKeys[1]

	m := atomic.NewMemory(prefixdb.New([]byte{5}, env.baseDB))

	env.msm.SharedMemory = m.NewSharedMemory(env.Ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(env.Ctx.XChainID)
	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := txs.Codec.Marshal(txs.Version, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	err = peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		env.Ctx.ChainID: {PutRequests: []*atomic.Element{{
			Key:   inputID[:],
			Value: utxoBytes,
			Traits: [][]byte{
				recipientKey.PublicKey().Address().Bytes(),
			},
		}}},
	})
	require.NoError(err)

	tx, err := env.txBuilder.NewImportTx(
		env.Ctx.XChainID,
		recipientKey.PublicKey().Address(),
		[]*secp256k1.PrivateKey{recipientKey},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	require.NoError(env.Builder.Add(tx))
	b, err := env.Builder.BuildBlock(context.Background())
	require.NoError(err)
	// Test multiple verify calls work
	require.NoError(b.Verify(context.Background()))
	require.NoError(b.Accept(context.Background()))
	_, txStatus, err := env.state.GetTx(tx.ID())
	require.NoError(err)
	// Ensure transaction is in the committed state
	require.Equal(txStatus, status.Committed)
}

func TestMempoolValidTxIsAddedToMempool(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.Ctx.Lock.Lock()
	defer func() {
		env.Ctx.Lock.Unlock()
		require.NoError(env.Shutdown())
	}()

	var gossipedBytes []byte
	env.sender.SendAppGossipF = func(_ context.Context, b []byte) error {
		gossipedBytes = b
		return nil
	}

	// create a tx
	tx := env.getValidTx(t)
	txID := tx.ID()

	// show that unknown tx is added to mempool
	err := env.AddUnverifiedTx(tx)
	require.NoError(err)

	require.True(env.Builder.Has(txID))

	// and gossiped if it has just been discovered
	require.True(gossipedBytes != nil)

	// show gossiped bytes can be decoded to the original tx
	replyIntf, err := message.Parse(gossipedBytes)
	require.NoError(err)

	reply := replyIntf.(*message.TxGossip)
	retrivedTx, err := txs.Parse(txs.Codec, reply.Tx)
	require.NoError(err)

	require.Equal(txID, retrivedTx.ID())
}

// show that locally generated txs are gossiped
func TestMempoolNewLocalTxIsGossiped(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)

	env.Ctx.Lock.Lock()
	defer func() {
		env.Ctx.Lock.Unlock()
		require.NoError(env.Shutdown())
	}()

	var gossipedBytes []byte
	env.sender.SendAppGossipF = func(_ context.Context, b []byte) error {
		gossipedBytes = b
		return nil
	}

	// add a tx to the mempool and show it gets gossiped
	tx := env.getValidTx(t)
	txID := tx.ID()

	err := env.Builder.AddUnverifiedTx(tx)
	require.NoError(err)
	require.True(gossipedBytes != nil)

	// show gossiped bytes can be decoded to the original tx
	replyIntf, err := message.Parse(gossipedBytes)
	require.NoError(err)

	reply := replyIntf.(*message.TxGossip)
	retrivedTx, err := txs.Parse(txs.Codec, reply.Tx)
	require.NoError(err)

	require.Equal(txID, retrivedTx.ID())

	// show that transaction is not re-gossiped is recently added to mempool
	gossipedBytes = nil
	env.Builder.Remove([]*txs.Tx{tx})
	err = env.Builder.Add(tx)
	require.NoError(err)

	require.True(gossipedBytes == nil)
}
