// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/chains/atomic/atomicmock"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/metrics/metricsmock"
	"github.com/ava-labs/avalanchego/vms/avm/state/statemock"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/avm/txs/txsmock"
)

func TestBlockVerify(t *testing.T) {
	type test struct {
		name        string
		blockFunc   func(*gomock.Controller) *Block
		expectedErr error
		postVerify  func(*require.Assertions, *Block)
	}
	tests := []test{
		{
			name: "block already verified",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				b := &Block{
					Block: mockBlock,
					manager: &manager{
						backend:      defaultTestBackend(false, nil),
						blkIDToState: map[ids.ID]*blockState{},
					},
				}
				b.manager.blkIDToState[b.ID()] = &blockState{
					statelessBlock: b.Block,
				}
				return b
			},
			expectedErr: nil,
		},
		{
			name: "block timestamp too far in the future",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().MerkleRoot().Return(ids.GenerateTestID()).AnyTimes()
				return &Block{
					Block: mockBlock,
					manager: &manager{
						backend: defaultTestBackend(false, nil),
					},
				}
			},
			expectedErr: ErrUnexpectedMerkleRoot,
		},
		{
			name: "block timestamp too far in the future",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().MerkleRoot().Return(ids.Empty).AnyTimes()
				now := time.Now()
				tooFarInFutureTime := now.Add(SyncBound + 1)
				mockBlock.EXPECT().Timestamp().Return(tooFarInFutureTime).AnyTimes()
				clk := &mockable.Clock{}
				clk.Set(now)
				return &Block{
					Block: mockBlock,
					manager: &manager{
						backend: defaultTestBackend(false, nil),
						clk:     clk,
					},
				}
			},
			expectedErr: ErrTimestampBeyondSyncBound,
		},
		{
			name: "block contains no transactions",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().MerkleRoot().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().Timestamp().Return(time.Now()).AnyTimes()
				mockBlock.EXPECT().Txs().Return(nil).AnyTimes()
				return &Block{
					Block: mockBlock,
					manager: &manager{
						backend:      defaultTestBackend(false, nil),
						blkIDToState: map[ids.ID]*blockState{},
						clk:          &mockable.Clock{},
					},
				}
			},
			expectedErr: ErrEmptyBlock,
		},
		{
			name: "block transaction fails verification",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().MerkleRoot().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().Timestamp().Return(time.Now()).AnyTimes()
				mockUnsignedTx := txsmock.NewUnsignedTx(ctrl)
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(errTest)
				errTx := &txs.Tx{
					Unsigned: mockUnsignedTx,
				}
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{errTx}).AnyTimes()

				mempool, err := mempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return &Block{
					Block: mockBlock,
					manager: &manager{
						backend:      defaultTestBackend(false, nil),
						mempool:      mempool,
						metrics:      metricsmock.NewMetrics(ctrl),
						blkIDToState: map[ids.ID]*blockState{},
						clk:          &mockable.Clock{},
					},
				}
			},
			expectedErr: errTest,
		},
		{
			name: "parent doesn't exist",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().MerkleRoot().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().Timestamp().Return(time.Now()).AnyTimes()

				mockUnsignedTx := txsmock.NewUnsignedTx(ctrl)
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(nil)
				tx := &txs.Tx{
					Unsigned: mockUnsignedTx,
				}
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{tx}).AnyTimes()

				parentID := ids.GenerateTestID()
				mockBlock.EXPECT().Parent().Return(parentID).AnyTimes()

				mockState := statemock.NewState(ctrl)
				mockState.EXPECT().GetBlock(parentID).Return(nil, errTest)
				return &Block{
					Block: mockBlock,
					manager: &manager{
						backend:      defaultTestBackend(false, nil),
						state:        mockState,
						blkIDToState: map[ids.ID]*blockState{},
						clk:          &mockable.Clock{},
					},
				}
			},
			expectedErr: errTest,
		},
		{
			name: "block height isn't parent height + 1",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().MerkleRoot().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().Timestamp().Return(time.Now()).AnyTimes()
				blockHeight := uint64(1337)
				mockBlock.EXPECT().Height().Return(blockHeight).AnyTimes()

				mockUnsignedTx := txsmock.NewUnsignedTx(ctrl)
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(nil)
				tx := &txs.Tx{
					Unsigned: mockUnsignedTx,
				}
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{tx}).AnyTimes()

				parentID := ids.GenerateTestID()
				mockBlock.EXPECT().Parent().Return(parentID).AnyTimes()

				mockState := statemock.NewState(ctrl)
				mockParentBlock := block.NewMockBlock(ctrl)
				mockParentBlock.EXPECT().Height().Return(blockHeight) // Should be blockHeight - 1
				mockState.EXPECT().GetBlock(parentID).Return(mockParentBlock, nil)

				return &Block{
					Block: mockBlock,
					manager: &manager{
						backend:      defaultTestBackend(false, nil),
						state:        mockState,
						blkIDToState: map[ids.ID]*blockState{},
						clk:          &mockable.Clock{},
					},
				}
			},
			expectedErr: ErrIncorrectHeight,
		},
		{
			name: "block timestamp before parent timestamp",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().MerkleRoot().Return(ids.Empty).AnyTimes()
				blockTimestamp := time.Now()
				mockBlock.EXPECT().Timestamp().Return(blockTimestamp).AnyTimes()
				blockHeight := uint64(1337)
				mockBlock.EXPECT().Height().Return(blockHeight).AnyTimes()

				mockUnsignedTx := txsmock.NewUnsignedTx(ctrl)
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(nil)
				tx := &txs.Tx{
					Unsigned: mockUnsignedTx,
				}
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{tx}).AnyTimes()

				parentID := ids.GenerateTestID()
				mockBlock.EXPECT().Parent().Return(parentID).AnyTimes()

				mockParentBlock := block.NewMockBlock(ctrl)
				mockParentBlock.EXPECT().Height().Return(blockHeight - 1)

				mockParentState := statemock.NewDiff(ctrl)
				mockParentState.EXPECT().GetLastAccepted().Return(parentID)
				mockParentState.EXPECT().GetTimestamp().Return(blockTimestamp.Add(1))

				return &Block{
					Block: mockBlock,
					manager: &manager{
						backend: defaultTestBackend(false, nil),
						blkIDToState: map[ids.ID]*blockState{
							parentID: {
								onAcceptState:  mockParentState,
								statelessBlock: mockParentBlock,
							},
						},
						clk:          &mockable.Clock{},
						lastAccepted: parentID,
					},
				}
			},
			expectedErr: ErrChildBlockEarlierThanParent,
		},
		{
			name: "tx fails semantic verification",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().MerkleRoot().Return(ids.Empty).AnyTimes()
				blockTimestamp := time.Now()
				mockBlock.EXPECT().Timestamp().Return(blockTimestamp).AnyTimes()
				blockHeight := uint64(1337)
				mockBlock.EXPECT().Height().Return(blockHeight).AnyTimes()

				mockUnsignedTx := txsmock.NewUnsignedTx(ctrl)
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(nil).Times(1)     // Syntactic verification passes
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(errTest).Times(1) // Semantic verification fails
				tx := &txs.Tx{
					Unsigned: mockUnsignedTx,
				}
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{tx}).AnyTimes()

				parentID := ids.GenerateTestID()
				mockBlock.EXPECT().Parent().Return(parentID).AnyTimes()

				mockParentBlock := block.NewMockBlock(ctrl)
				mockParentBlock.EXPECT().Height().Return(blockHeight - 1)

				mockParentState := statemock.NewDiff(ctrl)
				mockParentState.EXPECT().GetLastAccepted().Return(parentID)
				mockParentState.EXPECT().GetTimestamp().Return(blockTimestamp)

				mempool, err := mempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return &Block{
					Block: mockBlock,
					manager: &manager{
						backend: defaultTestBackend(false, nil),
						mempool: mempool,
						metrics: metricsmock.NewMetrics(ctrl),
						blkIDToState: map[ids.ID]*blockState{
							parentID: {
								onAcceptState:  mockParentState,
								statelessBlock: mockParentBlock,
							},
						},
						clk:          &mockable.Clock{},
						lastAccepted: parentID,
					},
				}
			},
			expectedErr: errTest,
		},
		{
			name: "tx fails execution",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().MerkleRoot().Return(ids.Empty).AnyTimes()
				blockTimestamp := time.Now()
				mockBlock.EXPECT().Timestamp().Return(blockTimestamp).AnyTimes()
				blockHeight := uint64(1337)
				mockBlock.EXPECT().Height().Return(blockHeight).AnyTimes()

				mockUnsignedTx := txsmock.NewUnsignedTx(ctrl)
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(nil).Times(1)     // Syntactic verification passes
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(nil).Times(1)     // Semantic verification fails
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(errTest).Times(1) // Execution fails
				tx := &txs.Tx{
					Unsigned: mockUnsignedTx,
				}
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{tx}).AnyTimes()

				parentID := ids.GenerateTestID()
				mockBlock.EXPECT().Parent().Return(parentID).AnyTimes()

				mockParentBlock := block.NewMockBlock(ctrl)
				mockParentBlock.EXPECT().Height().Return(blockHeight - 1)

				mockParentState := statemock.NewDiff(ctrl)
				mockParentState.EXPECT().GetLastAccepted().Return(parentID)
				mockParentState.EXPECT().GetTimestamp().Return(blockTimestamp)

				mempool, err := mempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return &Block{
					Block: mockBlock,
					manager: &manager{
						mempool: mempool,
						metrics: metricsmock.NewMetrics(ctrl),
						backend: defaultTestBackend(false, nil),
						blkIDToState: map[ids.ID]*blockState{
							parentID: {
								onAcceptState:  mockParentState,
								statelessBlock: mockParentBlock,
							},
						},
						clk:          &mockable.Clock{},
						lastAccepted: parentID,
					},
				}
			},
			expectedErr: errTest,
		},
		{
			name: "tx imported inputs overlap",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().MerkleRoot().Return(ids.Empty).AnyTimes()
				blockTimestamp := time.Now()
				mockBlock.EXPECT().Timestamp().Return(blockTimestamp).AnyTimes()
				blockHeight := uint64(1337)
				mockBlock.EXPECT().Height().Return(blockHeight).AnyTimes()

				// tx1 and tx2 both consume imported input [inputID]
				inputID := ids.GenerateTestID()
				mockUnsignedTx1 := txsmock.NewUnsignedTx(ctrl)
				mockUnsignedTx1.EXPECT().Visit(gomock.Any()).Return(nil).Times(1) // Syntactic verification passes
				mockUnsignedTx1.EXPECT().Visit(gomock.Any()).Return(nil).Times(1) // Semantic verification fails
				mockUnsignedTx1.EXPECT().Visit(gomock.Any()).DoAndReturn(
					func(visitor txs.Visitor) error {
						executor, ok := visitor.(*executor.Executor)
						if !ok {
							return errors.New("wrong visitor type")
						}
						executor.Inputs.Add(inputID)
						return nil
					},
				).Times(1)
				mockUnsignedTx2 := txsmock.NewUnsignedTx(ctrl)
				mockUnsignedTx2.EXPECT().Visit(gomock.Any()).Return(nil).Times(1) // Syntactic verification passes
				mockUnsignedTx2.EXPECT().Visit(gomock.Any()).Return(nil).Times(1) // Semantic verification fails
				mockUnsignedTx2.EXPECT().Visit(gomock.Any()).DoAndReturn(
					func(visitor txs.Visitor) error {
						executor, ok := visitor.(*executor.Executor)
						if !ok {
							return errors.New("wrong visitor type")
						}
						executor.Inputs.Add(inputID)
						return nil
					},
				).Times(1)
				tx1 := &txs.Tx{
					Unsigned: mockUnsignedTx1,
				}
				tx2 := &txs.Tx{
					Unsigned: mockUnsignedTx2,
				}
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{tx1, tx2}).AnyTimes()

				parentID := ids.GenerateTestID()
				mockBlock.EXPECT().Parent().Return(parentID).AnyTimes()

				mockParentBlock := block.NewMockBlock(ctrl)
				mockParentBlock.EXPECT().Height().Return(blockHeight - 1)

				mockParentState := statemock.NewDiff(ctrl)
				mockParentState.EXPECT().GetLastAccepted().Return(parentID)
				mockParentState.EXPECT().GetTimestamp().Return(blockTimestamp)

				mempool, err := mempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return &Block{
					Block: mockBlock,
					manager: &manager{
						mempool: mempool,
						metrics: metricsmock.NewMetrics(ctrl),
						backend: defaultTestBackend(false, nil),
						blkIDToState: map[ids.ID]*blockState{
							parentID: {
								onAcceptState:  mockParentState,
								statelessBlock: mockParentBlock,
							},
						},
						clk:          &mockable.Clock{},
						lastAccepted: parentID,
					},
				}
			},
			expectedErr: ErrConflictingBlockTxs,
		},
		{
			name: "tx input overlaps with other tx",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().MerkleRoot().Return(ids.Empty).AnyTimes()
				blockTimestamp := time.Now()
				mockBlock.EXPECT().Timestamp().Return(blockTimestamp).AnyTimes()
				blockHeight := uint64(1337)
				mockBlock.EXPECT().Height().Return(blockHeight).AnyTimes()

				// tx1 and parent block both consume [inputID]
				inputID := ids.GenerateTestID()
				mockUnsignedTx := txsmock.NewUnsignedTx(ctrl)
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(nil).Times(1) // Syntactic verification passes
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(nil).Times(1) // Semantic verification fails
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).DoAndReturn(
					func(visitor txs.Visitor) error {
						executor, ok := visitor.(*executor.Executor)
						if !ok {
							return errors.New("wrong visitor type")
						}
						executor.Inputs.Add(inputID)
						return nil
					},
				).Times(1)
				tx := &txs.Tx{
					Unsigned: mockUnsignedTx,
				}
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{tx}).AnyTimes()

				parentID := ids.GenerateTestID()
				mockBlock.EXPECT().Parent().Return(parentID).AnyTimes()

				mockParentBlock := block.NewMockBlock(ctrl)
				mockParentBlock.EXPECT().Height().Return(blockHeight - 1)

				mockParentState := statemock.NewDiff(ctrl)
				mockParentState.EXPECT().GetLastAccepted().Return(parentID)
				mockParentState.EXPECT().GetTimestamp().Return(blockTimestamp)

				return &Block{
					Block: mockBlock,
					manager: &manager{
						backend: defaultTestBackend(false, nil),
						blkIDToState: map[ids.ID]*blockState{
							parentID: {
								onAcceptState:  mockParentState,
								statelessBlock: mockParentBlock,
								importedInputs: set.Of(inputID),
							},
						},
						clk:          &mockable.Clock{},
						lastAccepted: parentID,
					},
				}
			},
			expectedErr: ErrConflictingParentTxs,
		},
		{
			name: "happy path",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.Empty).AnyTimes()
				mockBlock.EXPECT().MerkleRoot().Return(ids.Empty).AnyTimes()
				blockTimestamp := time.Now()
				mockBlock.EXPECT().Timestamp().Return(blockTimestamp).AnyTimes()
				blockHeight := uint64(1337)
				mockBlock.EXPECT().Height().Return(blockHeight).AnyTimes()

				mockUnsignedTx := txsmock.NewUnsignedTx(ctrl)
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(nil).Times(1) // Syntactic verification passes
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(nil).Times(1) // Semantic verification fails
				mockUnsignedTx.EXPECT().Visit(gomock.Any()).Return(nil).Times(1) // Execution passes
				mockUnsignedTx.EXPECT().InputIDs().AnyTimes()
				tx := &txs.Tx{
					Unsigned: mockUnsignedTx,
				}
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{tx}).AnyTimes()

				parentID := ids.GenerateTestID()
				mockBlock.EXPECT().Parent().Return(parentID).AnyTimes()

				mockParentBlock := block.NewMockBlock(ctrl)
				mockParentBlock.EXPECT().Height().Return(blockHeight - 1)

				mockParentState := statemock.NewDiff(ctrl)
				mockParentState.EXPECT().GetLastAccepted().Return(parentID)
				mockParentState.EXPECT().GetTimestamp().Return(blockTimestamp)

				mempool, err := mempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)

				return &Block{
					Block: mockBlock,
					manager: &manager{
						mempool: mempool,
						metrics: metricsmock.NewMetrics(ctrl),
						backend: defaultTestBackend(false, nil),
						blkIDToState: map[ids.ID]*blockState{
							parentID: {
								onAcceptState:  mockParentState,
								statelessBlock: mockParentBlock,
							},
						},
						clk:          &mockable.Clock{},
						lastAccepted: parentID,
					},
				}
			},
			expectedErr: nil,
			postVerify: func(require *require.Assertions, b *Block) {
				// Assert block is in the cache
				blockState, ok := b.manager.blkIDToState[b.ID()]
				require.True(ok)
				require.Equal(b.Block, blockState.statelessBlock)

				// Assert block is added to on accept state
				persistedBlock, err := blockState.onAcceptState.GetBlock(b.ID())
				require.NoError(err)
				require.Equal(b.Block, persistedBlock)

				// Assert block is set to last accepted
				lastAccepted := b.ID()
				require.Equal(lastAccepted, blockState.onAcceptState.GetLastAccepted())

				// Assert txs are added to on accept state
				blockTxs := b.Txs()
				for _, tx := range blockTxs {
					_, err := blockState.onAcceptState.GetTx(tx.ID())
					require.NoError(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			b := tt.blockFunc(ctrl)
			err := b.Verify(t.Context())
			require.ErrorIs(err, tt.expectedErr)
			if tt.postVerify != nil {
				tt.postVerify(require, b)
			}
		})
	}
}

func TestBlockAccept(t *testing.T) {
	type test struct {
		name        string
		blockFunc   func(*gomock.Controller) *Block
		expectedErr error
	}
	tests := []test{
		{
			name: "block not found",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(ids.GenerateTestID()).AnyTimes()
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{}).AnyTimes()

				mempool, err := mempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)

				return &Block{
					Block: mockBlock,
					manager: &manager{
						mempool:      mempool,
						metrics:      metricsmock.NewMetrics(ctrl),
						backend:      defaultTestBackend(false, nil),
						blkIDToState: map[ids.ID]*blockState{},
					},
				}
			},
			expectedErr: ErrBlockNotFound,
		},
		{
			name: "can't get commit batch",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				blockID := ids.GenerateTestID()
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(blockID).AnyTimes()
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{}).AnyTimes()

				mempool, err := mempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)

				mockManagerState := statemock.NewState(ctrl)
				mockManagerState.EXPECT().CommitBatch().Return(nil, errTest)
				mockManagerState.EXPECT().Abort()

				mockOnAcceptState := statemock.NewDiff(ctrl)
				mockOnAcceptState.EXPECT().Apply(mockManagerState)

				return &Block{
					Block: mockBlock,
					manager: &manager{
						state:   mockManagerState,
						mempool: mempool,
						backend: defaultTestBackend(false, nil),
						blkIDToState: map[ids.ID]*blockState{
							blockID: {
								onAcceptState: mockOnAcceptState,
							},
						},
					},
				}
			},
			expectedErr: errTest,
		},
		{
			name: "can't apply shared memory",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				blockID := ids.GenerateTestID()
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(blockID).AnyTimes()
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{}).AnyTimes()

				mempool, err := mempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)

				mockManagerState := statemock.NewState(ctrl)
				// Note the returned batch is nil but not used
				// because we mock the call to shared memory
				mockManagerState.EXPECT().CommitBatch().Return(nil, nil)
				mockManagerState.EXPECT().Abort()

				mockSharedMemory := atomicmock.NewSharedMemory(ctrl)
				mockSharedMemory.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(errTest)

				mockOnAcceptState := statemock.NewDiff(ctrl)
				mockOnAcceptState.EXPECT().Apply(mockManagerState)

				return &Block{
					Block: mockBlock,
					manager: &manager{
						state:   mockManagerState,
						mempool: mempool,
						backend: defaultTestBackend(false, mockSharedMemory),
						blkIDToState: map[ids.ID]*blockState{
							blockID: {
								onAcceptState: mockOnAcceptState,
							},
						},
					},
				}
			},
			expectedErr: errTest,
		},
		{
			name: "failed to apply metrics",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				blockID := ids.GenerateTestID()
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(blockID).AnyTimes()
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{}).AnyTimes()

				mempool, err := mempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)

				mockManagerState := statemock.NewState(ctrl)
				// Note the returned batch is nil but not used
				// because we mock the call to shared memory
				mockManagerState.EXPECT().CommitBatch().Return(nil, nil)
				mockManagerState.EXPECT().Abort()

				mockSharedMemory := atomicmock.NewSharedMemory(ctrl)
				mockSharedMemory.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(nil)

				mockOnAcceptState := statemock.NewDiff(ctrl)
				mockOnAcceptState.EXPECT().Apply(mockManagerState)

				metrics := metricsmock.NewMetrics(ctrl)
				metrics.EXPECT().MarkBlockAccepted(gomock.Any()).Return(errTest)

				return &Block{
					Block: mockBlock,
					manager: &manager{
						state:   mockManagerState,
						mempool: mempool,
						metrics: metrics,
						backend: defaultTestBackend(false, mockSharedMemory),
						blkIDToState: map[ids.ID]*blockState{
							blockID: {
								onAcceptState: mockOnAcceptState,
							},
						},
					},
				}
			},
			expectedErr: errTest,
		},
		{
			name: "no error",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				blockID := ids.GenerateTestID()
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(blockID).AnyTimes()
				mockBlock.EXPECT().Height().Return(uint64(0)).AnyTimes()
				mockBlock.EXPECT().Parent().Return(ids.GenerateTestID()).AnyTimes()
				mockBlock.EXPECT().Txs().Return([]*txs.Tx{}).AnyTimes()

				mempool, err := mempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)

				mockManagerState := statemock.NewState(ctrl)
				// Note the returned batch is nil but not used
				// because we mock the call to shared memory
				mockManagerState.EXPECT().CommitBatch().Return(nil, nil)
				mockManagerState.EXPECT().Abort()
				mockManagerState.EXPECT().Checksum().Return(ids.Empty)

				mockSharedMemory := atomicmock.NewSharedMemory(ctrl)
				mockSharedMemory.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(nil)

				mockOnAcceptState := statemock.NewDiff(ctrl)
				mockOnAcceptState.EXPECT().Apply(mockManagerState)

				metrics := metricsmock.NewMetrics(ctrl)
				metrics.EXPECT().MarkBlockAccepted(gomock.Any()).Return(nil)

				return &Block{
					Block: mockBlock,
					manager: &manager{
						state:   mockManagerState,
						mempool: mempool,
						metrics: metrics,
						backend: defaultTestBackend(false, mockSharedMemory),
						blkIDToState: map[ids.ID]*blockState{
							blockID: {
								onAcceptState: mockOnAcceptState,
							},
						},
					},
				}
			},
			expectedErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			b := tt.blockFunc(ctrl)
			err := b.Accept(t.Context())
			require.ErrorIs(err, tt.expectedErr)
			if err == nil {
				// Make sure block is removed from cache
				_, ok := b.manager.blkIDToState[b.ID()]
				require.False(ok)
			}
		})
	}
}

func TestBlockReject(t *testing.T) {
	type test struct {
		name      string
		blockFunc func(*gomock.Controller) *Block
	}
	tests := []test{
		{
			name: "one tx passes verification; one fails syntactic verification; one fails semantic verification; one fails execution",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				blockID := ids.GenerateTestID()
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(blockID).AnyTimes()
				mockBlock.EXPECT().Height().Return(uint64(0)).AnyTimes()
				mockBlock.EXPECT().Parent().Return(ids.GenerateTestID()).AnyTimes()

				unsignedValidTx := txsmock.NewUnsignedTx(ctrl)
				unsignedValidTx.EXPECT().SetBytes(gomock.Any())
				unsignedValidTx.EXPECT().Visit(gomock.Any()).Return(nil).AnyTimes() // Passes verification and execution
				unsignedValidTx.EXPECT().InputIDs().Return(nil)

				unsignedSyntacticallyInvalidTx := txsmock.NewUnsignedTx(ctrl)
				unsignedSyntacticallyInvalidTx.EXPECT().SetBytes(gomock.Any())
				unsignedSyntacticallyInvalidTx.EXPECT().Visit(gomock.Any()).Return(errTest) // Fails syntactic verification

				unsignedSemanticallyInvalidTx := txsmock.NewUnsignedTx(ctrl)
				unsignedSemanticallyInvalidTx.EXPECT().SetBytes(gomock.Any())
				unsignedSemanticallyInvalidTx.EXPECT().Visit(gomock.Any()).Return(nil)     // Passes syntactic verification
				unsignedSemanticallyInvalidTx.EXPECT().Visit(gomock.Any()).Return(errTest) // Fails semantic verification

				unsignedExecutionFailsTx := txsmock.NewUnsignedTx(ctrl)
				unsignedExecutionFailsTx.EXPECT().SetBytes(gomock.Any())
				unsignedExecutionFailsTx.EXPECT().Visit(gomock.Any()).Return(nil)     // Passes syntactic verification
				unsignedExecutionFailsTx.EXPECT().Visit(gomock.Any()).Return(nil)     // Passes semantic verification
				unsignedExecutionFailsTx.EXPECT().Visit(gomock.Any()).Return(errTest) // Fails execution

				// Give each tx a unique ID
				validTx := &txs.Tx{Unsigned: unsignedValidTx}
				validTx.SetBytes(utils.RandomBytes(16), utils.RandomBytes(16))
				syntacticallyInvalidTx := &txs.Tx{Unsigned: unsignedSyntacticallyInvalidTx}
				syntacticallyInvalidTx.SetBytes(utils.RandomBytes(16), utils.RandomBytes(16))
				semanticallyInvalidTx := &txs.Tx{Unsigned: unsignedSemanticallyInvalidTx}
				semanticallyInvalidTx.SetBytes(utils.RandomBytes(16), utils.RandomBytes(16))
				executionFailsTx := &txs.Tx{Unsigned: unsignedExecutionFailsTx}
				executionFailsTx.SetBytes(utils.RandomBytes(16), utils.RandomBytes(16))

				mockBlock.EXPECT().Txs().Return([]*txs.Tx{
					validTx,
					syntacticallyInvalidTx,
					semanticallyInvalidTx,
					executionFailsTx,
				})

				mempool, err := mempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)

				lastAcceptedID := ids.GenerateTestID()
				mockState := statemock.NewState(ctrl)
				mockState.EXPECT().GetLastAccepted().Return(lastAcceptedID).AnyTimes()
				mockState.EXPECT().GetTimestamp().Return(time.Now()).AnyTimes()

				return &Block{
					Block: mockBlock,
					manager: &manager{
						lastAccepted: lastAcceptedID,
						mempool:      mempool,
						metrics:      metricsmock.NewMetrics(ctrl),
						backend:      defaultTestBackend(true, nil),
						state:        mockState,
						blkIDToState: map[ids.ID]*blockState{
							blockID: {},
						},
					},
				}
			},
		},
		{
			name: "all txs valid",
			blockFunc: func(ctrl *gomock.Controller) *Block {
				blockID := ids.GenerateTestID()
				mockBlock := block.NewMockBlock(ctrl)
				mockBlock.EXPECT().ID().Return(blockID).AnyTimes()
				mockBlock.EXPECT().Height().Return(uint64(0)).AnyTimes()
				mockBlock.EXPECT().Parent().Return(ids.GenerateTestID()).AnyTimes()

				unsignedTx1 := txsmock.NewUnsignedTx(ctrl)
				unsignedTx1.EXPECT().SetBytes(gomock.Any())
				unsignedTx1.EXPECT().Visit(gomock.Any()).Return(nil).AnyTimes() // Passes verification and execution
				unsignedTx1.EXPECT().InputIDs().Return(nil)

				unsignedTx2 := txsmock.NewUnsignedTx(ctrl)
				unsignedTx2.EXPECT().SetBytes(gomock.Any())
				unsignedTx2.EXPECT().Visit(gomock.Any()).Return(nil).AnyTimes() // Passes verification and execution
				unsignedTx2.EXPECT().InputIDs().Return(nil)

				// Give each tx a unique ID
				tx1 := &txs.Tx{Unsigned: unsignedTx1}
				tx1.SetBytes(utils.RandomBytes(16), utils.RandomBytes(16))
				tx2 := &txs.Tx{Unsigned: unsignedTx2}
				tx2.SetBytes(utils.RandomBytes(16), utils.RandomBytes(16))

				mockBlock.EXPECT().Txs().Return([]*txs.Tx{
					tx1,
					tx2,
				})

				mempool, err := mempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)

				lastAcceptedID := ids.GenerateTestID()
				mockState := statemock.NewState(ctrl)
				mockState.EXPECT().GetLastAccepted().Return(lastAcceptedID).AnyTimes()
				mockState.EXPECT().GetTimestamp().Return(time.Now()).AnyTimes()

				return &Block{
					Block: mockBlock,
					manager: &manager{
						lastAccepted: lastAcceptedID,
						mempool:      mempool,
						metrics:      metricsmock.NewMetrics(ctrl),
						backend:      defaultTestBackend(true, nil),
						state:        mockState,
						blkIDToState: map[ids.ID]*blockState{
							blockID: {},
						},
					},
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			b := tt.blockFunc(ctrl)
			require.NoError(b.Reject(t.Context()))
			_, ok := b.manager.blkIDToState[b.ID()]
			require.False(ok)
		})
	}
}

func defaultTestBackend(bootstrapped bool, sharedMemory atomic.SharedMemory) *executor.Backend {
	return &executor.Backend{
		Bootstrapped: bootstrapped,
		Ctx: &snow.Context{
			SharedMemory: sharedMemory,
			Log:          logging.NoLog{},
		},
		Config: &config.Config{
			Upgrades:         upgradetest.GetConfig(upgradetest.Durango),
			TxFee:            0,
			CreateAssetTxFee: 0,
		},
	}
}
