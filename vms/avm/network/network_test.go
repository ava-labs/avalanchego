// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/avm/block/executor"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/message"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	txGossipHandlerID                        = 0
	maxValidatorSetStaleness                 = time.Second
	txGossipMaxGossipSize                    = 1
	txGossipPollSize                         = 1
	txGossipThrottlingPeriod                 = time.Second
	txGossipThrottlingLimit                  = 1
	txGossipBloomMaxElements                 = 10
	txGossipBloomFalsePositiveProbability    = 0.1
	txGossipBloomMaxFalsePositiveProbability = 0.5
)

var errTest = errors.New("test error")

func TestNetworkAppGossip(t *testing.T) {
	testTx := &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    1,
				BlockchainID: ids.GenerateTestID(),
				Ins:          []*avax.TransferableInput{},
				Outs:         []*avax.TransferableOutput{},
			},
		},
	}

	parser, err := txs.NewParser([]fxs.Fx{
		&secp256k1fx.Fx{},
	})
	require.NoError(t, err)
	require.NoError(t, testTx.Initialize(parser.Codec()))

	type test struct {
		name          string
		msgBytesFunc  func() []byte
		mempoolFunc   func(*gomock.Controller) mempool.Mempool
		verifierFunc  func(*gomock.Controller) TxVerifier
		appSenderFunc func(*gomock.Controller) common.AppSender
	}

	tests := []test{
		{
			// Shouldn't attempt to issue or gossip the tx
			name: "invalid message bytes",
			msgBytesFunc: func() []byte {
				return []byte{0x00}
			},
			mempoolFunc: func(*gomock.Controller) mempool.Mempool {
				return nil // Unused in this test
			},
			verifierFunc: func(*gomock.Controller) TxVerifier {
				return nil // Unused in this test
			},
			appSenderFunc: func(*gomock.Controller) common.AppSender {
				return nil // Unused in this test
			},
		},
		{
			// Shouldn't attempt to issue or gossip the tx
			name: "invalid tx bytes",
			msgBytesFunc: func() []byte {
				msg := message.Tx{
					Tx: []byte{0x00},
				}
				msgBytes, err := message.Build(&msg)
				require.NoError(t, err)
				return msgBytes
			},
			mempoolFunc: func(*gomock.Controller) mempool.Mempool {
				return nil // Unused in this test
			},
			verifierFunc: func(*gomock.Controller) TxVerifier {
				return nil // Unused in this test
			},
			appSenderFunc: func(*gomock.Controller) common.AppSender {
				return nil // Unused in this test
			},
		},
		{
			// The tx is added to the mempool and hasn't previously been
			// gossipped, so we gossip it both from the legacy and p2p SDK.
			name: "issuance succeeds",
			msgBytesFunc: func() []byte {
				msg := message.Tx{
					Tx: testTx.Bytes(),
				}
				msgBytes, err := message.Build(&msg)
				require.NoError(t, err)
				return msgBytes
			},
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().Add(gomock.Any()).Return(nil)
				mempool.EXPECT().RequestBuildBlock()
				return mempool
			},
			verifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executor.NewMockManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil).Times(1)
				return txVerifier
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				appSender := common.NewMockSender(ctrl)
				appSender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any()).Times(2)
				return appSender
			},
		},
		{
			// Issue returns error because tx was dropped. We shouldn't gossip
			// the tx.
			name: "issuance fails",
			msgBytesFunc: func() []byte {
				msg := message.Tx{
					Tx: testTx.Bytes(),
				}
				msgBytes, err := message.Build(&msg)
				require.NoError(t, err)
				return msgBytes
			},
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(errTest)
				return mempool
			},
			verifierFunc: func(*gomock.Controller) TxVerifier {
				return nil // Unused in this test
			},
			appSenderFunc: func(*gomock.Controller) common.AppSender {
				return nil // Unused in this test
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			parser, err := txs.NewParser([]fxs.Fx{
				&secp256k1fx.Fx{},
				&nftfx.Fx{},
				&propertyfx.Fx{},
			})
			require.NoError(err)

			n, err := New(
				&snow.Context{
					Log: logging.NoLog{},
				},
				parser,
				tt.verifierFunc(ctrl),
				tt.mempoolFunc(ctrl),
				tt.appSenderFunc(ctrl),
				prometheus.NewRegistry(),
				txGossipHandlerID,
				maxValidatorSetStaleness,
				txGossipMaxGossipSize,
				txGossipPollSize,
				txGossipThrottlingPeriod,
				txGossipThrottlingLimit,
				txGossipBloomMaxElements,
				txGossipBloomFalsePositiveProbability,
				txGossipBloomMaxFalsePositiveProbability,
			)
			require.NoError(err)
			require.NoError(n.AppGossip(context.Background(), ids.GenerateTestNodeID(), tt.msgBytesFunc()))
		})
	}
}

func TestNetworkIssueTx(t *testing.T) {
	type test struct {
		name          string
		mempoolFunc   func(*gomock.Controller) mempool.Mempool
		txVerifier    func(*gomock.Controller) TxVerifier
		appSenderFunc func(*gomock.Controller) common.AppSender
		expectedErr   error
	}

	tests := []test{
		{
			name: "mempool already has transaction",
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, true)
				return mempool
			},
			txVerifier: func(*gomock.Controller) TxVerifier {
				return nil // Unused in this test
			},
			appSenderFunc: func(*gomock.Controller) common.AppSender {
				return nil // Unused in this test
			},
			expectedErr: mempool.ErrDuplicateTx,
		},
		{
			name: "transaction marked as dropped in mempool",
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(errTest)
				return mempool
			},
			txVerifier: func(*gomock.Controller) TxVerifier {
				return nil // Unused in this test
			},
			appSenderFunc: func(*gomock.Controller) common.AppSender {
				return nil // Unused in this test
			},
			expectedErr: errTest,
		},
		{
			name: "transaction invalid",
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().MarkDropped(gomock.Any(), gomock.Any())
				return mempool
			},
			txVerifier: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executor.NewMockManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(errTest)
				return txVerifier
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return common.NewMockSender(ctrl)
			},
			expectedErr: errTest,
		},
		{
			name: "can't add transaction to mempool",
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().Add(gomock.Any()).Return(errTest)
				mempool.EXPECT().MarkDropped(gomock.Any(), gomock.Any())
				return mempool
			},
			txVerifier: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executor.NewMockManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return common.NewMockSender(ctrl)
			},
			expectedErr: errTest,
		},
		{
			name: "happy path",
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().Add(gomock.Any()).Return(nil)
				mempool.EXPECT().RequestBuildBlock()
				return mempool
			},
			txVerifier: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executor.NewMockManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Should gossip the tx
				appSender := common.NewMockSender(ctrl)
				appSender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any()).Return(nil).Times(2)
				return appSender
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			parser, err := txs.NewParser([]fxs.Fx{
				&secp256k1fx.Fx{},
				&nftfx.Fx{},
				&propertyfx.Fx{},
			})
			require.NoError(err)

			n, err := New(
				&snow.Context{
					Log: logging.NoLog{},
				},
				parser,
				tt.txVerifier(ctrl),
				tt.mempoolFunc(ctrl),
				tt.appSenderFunc(ctrl),
				prometheus.NewRegistry(),
				txGossipHandlerID,
				maxValidatorSetStaleness,
				txGossipMaxGossipSize,
				txGossipPollSize,
				txGossipThrottlingPeriod,
				txGossipThrottlingLimit,
				txGossipBloomMaxElements,
				txGossipBloomFalsePositiveProbability,
				txGossipBloomMaxFalsePositiveProbability,
			)
			require.NoError(err)
			err = n.IssueTx(context.Background(), &txs.Tx{})
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestNetworkGossipTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	parser, err := txs.NewParser([]fxs.Fx{
		&secp256k1fx.Fx{},
	})
	require.NoError(err)

	appSender := common.NewMockSender(ctrl)

	n, err := New(
		&snow.Context{
			Log: logging.NoLog{},
		},
		parser,
		executor.NewMockManager(ctrl),
		mempool.NewMockMempool(ctrl),
		appSender,
		prometheus.NewRegistry(),
		txGossipHandlerID,
		maxValidatorSetStaleness,
		txGossipMaxGossipSize,
		txGossipPollSize,
		txGossipThrottlingPeriod,
		txGossipThrottlingLimit,
		txGossipBloomMaxElements,
		txGossipBloomFalsePositiveProbability,
		txGossipBloomMaxFalsePositiveProbability,
	)
	require.NoError(err)

	// Case: Tx was recently gossiped
	txID := ids.GenerateTestID()
	n.recentTxs.Put(txID, struct{}{})
	n.gossipTxMessage(context.Background(), txID, []byte{})
	// Didn't make a call to SendAppGossip

	// Case: Tx was not recently gossiped
	msgBytes := []byte{1, 2, 3}
	appSender.EXPECT().SendAppGossip(gomock.Any(), msgBytes).Return(nil)
	n.gossipTxMessage(context.Background(), ids.GenerateTestID(), msgBytes)
	// Did make a call to SendAppGossip
}
