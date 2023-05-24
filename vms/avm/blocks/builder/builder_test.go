// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/avm/blocks"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/metrics"
	"github.com/ava-labs/avalanchego/vms/avm/states"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	blkexecutor "github.com/ava-labs/avalanchego/vms/avm/blocks/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/avm/txs/executor"
)

var (
	errTest = errors.New("test error")
	chainID = ids.GenerateTestID()
	keys    = secp256k1.TestKeys()
)

func TestBuilderBuildBlock(t *testing.T) {
	type test struct {
		name        string
		builderFunc func(*gomock.Controller) Builder
		expectedErr error
	}

	tests := []test{
		{
			name: "can't get stateless block",
			builderFunc: func(ctrl *gomock.Controller) Builder {
				preferredID := ids.GenerateTestID()
				manager := blkexecutor.NewMockManager(ctrl)
				manager.EXPECT().Preferred().Return(preferredID)
				manager.EXPECT().GetStatelessBlock(preferredID).Return(nil, errTest)

				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().RequestBuildBlock()

				return New(
					&txexecutor.Backend{
						Ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
					manager,
					&mockable.Clock{},
					mempool,
				)
			},
			expectedErr: errTest,
		},
		{
			name: "can't get preferred diff",
			builderFunc: func(ctrl *gomock.Controller) Builder {
				preferredID := ids.GenerateTestID()
				preferredHeight := uint64(1337)
				preferredTimestamp := time.Now()
				preferredBlock := blocks.NewMockBlock(ctrl)
				preferredBlock.EXPECT().Height().Return(preferredHeight)
				preferredBlock.EXPECT().Timestamp().Return(preferredTimestamp)

				manager := blkexecutor.NewMockManager(ctrl)
				manager.EXPECT().Preferred().Return(preferredID)
				manager.EXPECT().GetStatelessBlock(preferredID).Return(preferredBlock, nil)
				manager.EXPECT().GetState(preferredID).Return(nil, false)

				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().RequestBuildBlock()

				return New(
					&txexecutor.Backend{
						Ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
					manager,
					&mockable.Clock{},
					mempool,
				)
			},
			expectedErr: states.ErrMissingParentState,
		},
		{
			name: "tx fails semantic verification",
			builderFunc: func(ctrl *gomock.Controller) Builder {
				preferredID := ids.GenerateTestID()
				preferredHeight := uint64(1337)
				preferredTimestamp := time.Now()
				preferredBlock := blocks.NewMockBlock(ctrl)
				preferredBlock.EXPECT().Height().Return(preferredHeight)
				preferredBlock.EXPECT().Timestamp().Return(preferredTimestamp)

				preferredState := states.NewMockChain(ctrl)
				preferredState.EXPECT().GetLastAccepted().Return(preferredID)
				preferredState.EXPECT().GetTimestamp().Return(preferredTimestamp)

				manager := blkexecutor.NewMockManager(ctrl)
				manager.EXPECT().Preferred().Return(preferredID)
				manager.EXPECT().GetStatelessBlock(preferredID).Return(preferredBlock, nil)
				manager.EXPECT().GetState(preferredID).Return(preferredState, true)

				unsignedTx := txs.NewMockUnsignedTx(ctrl)
				unsignedTx.EXPECT().Visit(gomock.Any()).Return(errTest) // Fail semantic verification
				tx := &txs.Tx{Unsigned: unsignedTx}

				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Peek(gomock.Any()).Return(tx)
				mempool.EXPECT().Remove([]*txs.Tx{tx})
				mempool.EXPECT().MarkDropped(tx.ID(), errTest)
				// Second loop iteration
				mempool.EXPECT().Peek(gomock.Any()).Return(nil)
				mempool.EXPECT().RequestBuildBlock()

				return New(
					&txexecutor.Backend{
						Ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
					manager,
					&mockable.Clock{},
					mempool,
				)
			},
			expectedErr: ErrNoTransactions, // The only tx was invalid
		},
		{
			name: "tx fails execution",
			builderFunc: func(ctrl *gomock.Controller) Builder {
				preferredID := ids.GenerateTestID()
				preferredHeight := uint64(1337)
				preferredTimestamp := time.Now()
				preferredBlock := blocks.NewMockBlock(ctrl)
				preferredBlock.EXPECT().Height().Return(preferredHeight)
				preferredBlock.EXPECT().Timestamp().Return(preferredTimestamp)

				preferredState := states.NewMockChain(ctrl)
				preferredState.EXPECT().GetLastAccepted().Return(preferredID)
				preferredState.EXPECT().GetTimestamp().Return(preferredTimestamp)

				manager := blkexecutor.NewMockManager(ctrl)
				manager.EXPECT().Preferred().Return(preferredID)
				manager.EXPECT().GetStatelessBlock(preferredID).Return(preferredBlock, nil)
				manager.EXPECT().GetState(preferredID).Return(preferredState, true)

				unsignedTx := txs.NewMockUnsignedTx(ctrl)
				unsignedTx.EXPECT().Visit(gomock.Any()).Return(nil)     // Pass semantic verification
				unsignedTx.EXPECT().Visit(gomock.Any()).Return(errTest) // Fail execution
				tx := &txs.Tx{Unsigned: unsignedTx}

				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Peek(gomock.Any()).Return(tx)
				mempool.EXPECT().Remove([]*txs.Tx{tx})
				mempool.EXPECT().MarkDropped(tx.ID(), errTest)
				// Second loop iteration
				mempool.EXPECT().Peek(gomock.Any()).Return(nil)
				mempool.EXPECT().RequestBuildBlock()

				return New(
					&txexecutor.Backend{
						Ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
					manager,
					&mockable.Clock{},
					mempool,
				)
			},
			expectedErr: ErrNoTransactions, // The only tx was invalid
		},
		{
			name: "tx has non-unique inputs",
			builderFunc: func(ctrl *gomock.Controller) Builder {
				preferredID := ids.GenerateTestID()
				preferredHeight := uint64(1337)
				preferredTimestamp := time.Now()
				preferredBlock := blocks.NewMockBlock(ctrl)
				preferredBlock.EXPECT().Height().Return(preferredHeight)
				preferredBlock.EXPECT().Timestamp().Return(preferredTimestamp)

				preferredState := states.NewMockChain(ctrl)
				preferredState.EXPECT().GetLastAccepted().Return(preferredID)
				preferredState.EXPECT().GetTimestamp().Return(preferredTimestamp)

				manager := blkexecutor.NewMockManager(ctrl)
				manager.EXPECT().Preferred().Return(preferredID)
				manager.EXPECT().GetStatelessBlock(preferredID).Return(preferredBlock, nil)
				manager.EXPECT().GetState(preferredID).Return(preferredState, true)
				manager.EXPECT().VerifyUniqueInputs(preferredID, gomock.Any()).Return(errTest)

				unsignedTx := txs.NewMockUnsignedTx(ctrl)
				unsignedTx.EXPECT().Visit(gomock.Any()).Return(nil) // Pass semantic verification
				unsignedTx.EXPECT().Visit(gomock.Any()).Return(nil) // Pass execution
				tx := &txs.Tx{Unsigned: unsignedTx}

				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Peek(gomock.Any()).Return(tx)
				mempool.EXPECT().Remove([]*txs.Tx{tx})
				mempool.EXPECT().MarkDropped(tx.ID(), errTest)
				// Second loop iteration
				mempool.EXPECT().Peek(gomock.Any()).Return(nil)
				mempool.EXPECT().RequestBuildBlock()

				return New(
					&txexecutor.Backend{
						Ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
					manager,
					&mockable.Clock{},
					mempool,
				)
			},
			expectedErr: ErrNoTransactions, // The only tx was invalid
		},
		{
			name: "txs consume same input",
			builderFunc: func(ctrl *gomock.Controller) Builder {
				preferredID := ids.GenerateTestID()
				preferredHeight := uint64(1337)
				preferredTimestamp := time.Now()
				preferredBlock := blocks.NewMockBlock(ctrl)
				preferredBlock.EXPECT().Height().Return(preferredHeight)
				preferredBlock.EXPECT().Timestamp().Return(preferredTimestamp)

				preferredState := states.NewMockChain(ctrl)
				preferredState.EXPECT().GetLastAccepted().Return(preferredID)
				preferredState.EXPECT().GetTimestamp().Return(preferredTimestamp)

				// tx1 and tx2 both consume [inputID].
				// tx1 is added to the block first, so tx2 should be dropped.
				inputID := ids.GenerateTestID()
				unsignedTx1 := txs.NewMockUnsignedTx(ctrl)
				unsignedTx1.EXPECT().Visit(gomock.Any()).Return(nil)  // Pass semantic verification
				unsignedTx1.EXPECT().Visit(gomock.Any()).DoAndReturn( // Pass execution
					func(visitor txs.Visitor) error {
						executor, ok := visitor.(*txexecutor.Executor)
						require.True(t, ok)
						executor.Inputs.Add(inputID)
						return nil
					},
				)
				unsignedTx1.EXPECT().SetBytes(gomock.Any()).AnyTimes()
				tx1 := &txs.Tx{Unsigned: unsignedTx1}
				// Set the bytes of tx1 to something other than nil
				// so we can check that the remainingSize is updated
				tx1Bytes := []byte{1, 2, 3}
				tx1.SetBytes(nil, tx1Bytes)

				unsignedTx2 := txs.NewMockUnsignedTx(ctrl)
				unsignedTx2.EXPECT().Visit(gomock.Any()).Return(nil)  // Pass semantic verification
				unsignedTx2.EXPECT().Visit(gomock.Any()).DoAndReturn( // Pass execution
					func(visitor txs.Visitor) error {
						executor, ok := visitor.(*txexecutor.Executor)
						require.True(t, ok)
						executor.Inputs.Add(inputID)
						return nil
					},
				)
				tx2 := &txs.Tx{Unsigned: unsignedTx2}

				manager := blkexecutor.NewMockManager(ctrl)
				manager.EXPECT().Preferred().Return(preferredID)
				manager.EXPECT().GetStatelessBlock(preferredID).Return(preferredBlock, nil)
				manager.EXPECT().GetState(preferredID).Return(preferredState, true)
				manager.EXPECT().VerifyUniqueInputs(preferredID, gomock.Any()).Return(nil)
				// Assert created block has one tx, tx1,
				// and other fields are set correctly.
				manager.EXPECT().NewBlock(gomock.Any()).DoAndReturn(
					func(block *blocks.StandardBlock) snowman.Block {
						require.Len(t, block.Transactions, 1)
						require.Equal(t, tx1, block.Transactions[0])
						require.Equal(t, preferredHeight+1, block.Height())
						require.Equal(t, preferredID, block.Parent())
						return nil
					},
				)

				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Peek(targetBlockSize).Return(tx1)
				mempool.EXPECT().Remove([]*txs.Tx{tx1})
				// Second loop iteration
				mempool.EXPECT().Peek(targetBlockSize - len(tx1Bytes)).Return(tx2)
				mempool.EXPECT().Remove([]*txs.Tx{tx2})
				mempool.EXPECT().MarkDropped(tx2.ID(), blkexecutor.ErrConflictingBlockTxs)
				// Third loop iteration
				mempool.EXPECT().Peek(targetBlockSize - len(tx1Bytes)).Return(nil)
				mempool.EXPECT().RequestBuildBlock()

				// To marshal the tx/block
				codec := codec.NewMockManager(ctrl)
				codec.EXPECT().Marshal(gomock.Any(), gomock.Any()).Return([]byte{1, 2, 3}, nil).AnyTimes()
				codec.EXPECT().Size(gomock.Any(), gomock.Any()).Return(2, nil).AnyTimes()

				return New(
					&txexecutor.Backend{
						Codec: codec,
						Ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
					manager,
					&mockable.Clock{},
					mempool,
				)
			},
			expectedErr: nil,
		},
		{
			name: "preferred timestamp after now",
			builderFunc: func(ctrl *gomock.Controller) Builder {
				preferredID := ids.GenerateTestID()
				preferredHeight := uint64(1337)
				preferredTimestamp := time.Now()
				preferredBlock := blocks.NewMockBlock(ctrl)
				preferredBlock.EXPECT().Height().Return(preferredHeight)
				preferredBlock.EXPECT().Timestamp().Return(preferredTimestamp)

				// Clock reads just before the preferred timestamp.
				// Created block should have the preferred timestamp since it's later.
				clock := &mockable.Clock{}
				clock.Set(preferredTimestamp.Add(-2 * time.Second))

				preferredState := states.NewMockChain(ctrl)
				preferredState.EXPECT().GetLastAccepted().Return(preferredID)
				preferredState.EXPECT().GetTimestamp().Return(preferredTimestamp)

				manager := blkexecutor.NewMockManager(ctrl)
				manager.EXPECT().Preferred().Return(preferredID)
				manager.EXPECT().GetStatelessBlock(preferredID).Return(preferredBlock, nil)
				manager.EXPECT().GetState(preferredID).Return(preferredState, true)
				manager.EXPECT().VerifyUniqueInputs(preferredID, gomock.Any()).Return(nil)
				// Assert that the created block has the right timestamp
				manager.EXPECT().NewBlock(gomock.Any()).DoAndReturn(
					func(block *blocks.StandardBlock) snowman.Block {
						require.Equal(t, preferredTimestamp.Unix(), block.Timestamp().Unix())
						return nil
					},
				)

				inputID := ids.GenerateTestID()
				unsignedTx := txs.NewMockUnsignedTx(ctrl)
				unsignedTx.EXPECT().Visit(gomock.Any()).Return(nil)  // Pass semantic verification
				unsignedTx.EXPECT().Visit(gomock.Any()).DoAndReturn( // Pass execution
					func(visitor txs.Visitor) error {
						executor, ok := visitor.(*txexecutor.Executor)
						require.True(t, ok)
						executor.Inputs.Add(inputID)
						return nil
					},
				)
				unsignedTx.EXPECT().SetBytes(gomock.Any()).AnyTimes()
				tx := &txs.Tx{Unsigned: unsignedTx}

				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Peek(gomock.Any()).Return(tx)
				mempool.EXPECT().Remove([]*txs.Tx{tx})
				// Second loop iteration
				mempool.EXPECT().Peek(gomock.Any()).Return(nil)
				mempool.EXPECT().RequestBuildBlock()

				// To marshal the tx/block
				codec := codec.NewMockManager(ctrl)
				codec.EXPECT().Marshal(gomock.Any(), gomock.Any()).Return([]byte{1, 2, 3}, nil).AnyTimes()
				codec.EXPECT().Size(gomock.Any(), gomock.Any()).Return(2, nil).AnyTimes()

				return New(
					&txexecutor.Backend{
						Codec: codec,
						Ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
					manager,
					clock,
					mempool,
				)
			},
			expectedErr: nil,
		},
		{
			name: "preferred timestamp before now",
			builderFunc: func(ctrl *gomock.Controller) Builder {
				preferredID := ids.GenerateTestID()
				preferredHeight := uint64(1337)
				// preferred block's timestamp is after the time reported by clock
				now := time.Now()
				preferredTimestamp := now.Add(-2 * time.Second)
				preferredBlock := blocks.NewMockBlock(ctrl)
				preferredBlock.EXPECT().Height().Return(preferredHeight)
				preferredBlock.EXPECT().Timestamp().Return(preferredTimestamp)

				// Clock reads after the preferred timestamp.
				// Created block should have [now] timestamp since it's later.
				clock := &mockable.Clock{}
				clock.Set(now)

				preferredState := states.NewMockChain(ctrl)
				preferredState.EXPECT().GetLastAccepted().Return(preferredID)
				preferredState.EXPECT().GetTimestamp().Return(preferredTimestamp)

				manager := blkexecutor.NewMockManager(ctrl)
				manager.EXPECT().Preferred().Return(preferredID)
				manager.EXPECT().GetStatelessBlock(preferredID).Return(preferredBlock, nil)
				manager.EXPECT().GetState(preferredID).Return(preferredState, true)
				manager.EXPECT().VerifyUniqueInputs(preferredID, gomock.Any()).Return(nil)
				// Assert that the created block has the right timestamp
				manager.EXPECT().NewBlock(gomock.Any()).DoAndReturn(
					func(block *blocks.StandardBlock) snowman.Block {
						require.Equal(t, now.Unix(), block.Timestamp().Unix())
						return nil
					},
				)

				inputID := ids.GenerateTestID()
				unsignedTx := txs.NewMockUnsignedTx(ctrl)
				unsignedTx.EXPECT().Visit(gomock.Any()).Return(nil)  // Pass semantic verification
				unsignedTx.EXPECT().Visit(gomock.Any()).DoAndReturn( // Pass execution
					func(visitor txs.Visitor) error {
						executor, ok := visitor.(*txexecutor.Executor)
						require.True(t, ok)
						executor.Inputs.Add(inputID)
						return nil
					},
				)
				unsignedTx.EXPECT().SetBytes(gomock.Any()).AnyTimes()
				tx := &txs.Tx{Unsigned: unsignedTx}

				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Peek(gomock.Any()).Return(tx)
				mempool.EXPECT().Remove([]*txs.Tx{tx})
				// Second loop iteration
				mempool.EXPECT().Peek(gomock.Any()).Return(nil)
				mempool.EXPECT().RequestBuildBlock()

				// To marshal the tx/block
				codec := codec.NewMockManager(ctrl)
				codec.EXPECT().Marshal(gomock.Any(), gomock.Any()).Return([]byte{1, 2, 3}, nil).AnyTimes()
				codec.EXPECT().Size(gomock.Any(), gomock.Any()).Return(2, nil).AnyTimes()

				return New(
					&txexecutor.Backend{
						Codec: codec,
						Ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
					manager,
					clock,
					mempool,
				)
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			builder := tt.builderFunc(ctrl)
			_, err := builder.BuildBlock(context.Background())
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestBlockBuilderAddLocalTx(t *testing.T) {
	transactions := createTxs()

	require := require.New(t)

	registerer := prometheus.NewRegistry()
	toEngine := make(chan common.Message, 100)
	mempool, err := mempool.New("mempool", registerer, toEngine)
	require.NoError(err)
	// add a tx to the mempool
	tx := transactions[0]
	txID := tx.ID()
	err = mempool.Add(tx)
	require.NoError(err)

	has := mempool.Has(txID)
	require.True(has)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	parser, err := blocks.NewParser([]fxs.Fx{
		&secp256k1fx.Fx{},
	})
	require.NoError(err)

	backend := &txexecutor.Backend{
		Ctx: &snow.Context{
			Log: logging.NoLog{},
		},
		Codec: parser.Codec(),
	}

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	baseDB := versiondb.New(baseDBManager.Current().Database)

	state, err := states.New(baseDB, parser, registerer)
	require.NoError(err)

	clk := &mockable.Clock{}
	onAccept := func(*txs.Tx) error { return nil }
	now := time.Now()
	parentTimestamp := now.Add(-2 * time.Second)
	parentID := ids.GenerateTestID()
	cm := parser.Codec()
	txs, err := createParentTxs(cm)
	require.NoError(err)
	parentBlk, err := blocks.NewStandardBlock(parentID, 0, parentTimestamp, txs, cm)
	require.NoError(err)
	state.AddBlock(parentBlk)
	state.SetLastAccepted(parentBlk.ID())

	metrics, err := metrics.New("", registerer)
	require.NoError(err)

	manager := blkexecutor.NewManager(mempool, metrics, state, backend, clk, onAccept)

	manager.SetPreference(parentBlk.ID())

	builder := New(backend, manager, clk, mempool)

	// show that build block fails if tx is invalid
	_, err = builder.BuildBlock(context.Background())
	require.ErrorIs(err, ErrNoTransactions)
}

func createTxs() []*txs.Tx {
	return []*txs.Tx{{
		Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: ids.GenerateTestID(),
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: ids.GenerateTestID()},
				Out: &secp256k1fx.TransferOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Addrs: []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
			}},
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        ids.ID{'t', 'x', 'I', 'D'},
					OutputIndex: 1,
				},
				Asset: avax.Asset{ID: ids.GenerateTestID()},
				In: &secp256k1fx.TransferInput{
					Amt: uint64(54321),
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			}},
			Memo: []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}},
		Creds: []*fxs.FxCredential{
			{
				Verifiable: &secp256k1fx.Credential{},
			},
		},
	}}
}

func createParentTxs(cm codec.Manager) ([]*txs.Tx, error) {
	countTxs := 1
	testTxs := make([]*txs.Tx, 0, countTxs)
	for i := 0; i < countTxs; i++ {
		// Create the tx
		tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: ids.ID{1, 2, 3}},
				Out: &secp256k1fx.TransferOutput{
					Amt: uint64(12345),
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			}},
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        ids.ID{'t', 'x', 'p', 'a', 'r', 'e', 'n', 't'},
					OutputIndex: 1,
				},
				Asset: avax.Asset{ID: ids.ID{1, 2, 3}},
				In: &secp256k1fx.TransferInput{
					Amt: uint64(54321),
					Input: secp256k1fx.Input{
						SigIndices: []uint32{2},
					},
				},
			}},
			Memo: []byte{1, 2, 9, 4, 5, 6, 7, 8},
		}}}
		if err := tx.SignSECP256K1Fx(cm, [][]*secp256k1.PrivateKey{{keys[0]}}); err != nil {
			return nil, err
		}
		testTxs = append(testTxs, tx)
	}
	return testTxs, nil
}
