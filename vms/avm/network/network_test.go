// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

var (
	testConfig = Config{
		MaxValidatorSetStaleness:                    time.Second,
		TargetGossipSize:                            1,
		PullGossipPollSize:                          1,
		PullGossipFrequency:                         time.Second,
		PullGossipThrottlingPeriod:                  time.Second,
		PullGossipThrottlingLimit:                   1,
		ExpectedBloomFilterElements:                 10,
		ExpectedBloomFilterFalsePositiveProbability: .1,
		MaxBloomFilterFalsePositiveProbability:      .5,
		LegacyPushGossipCacheSize:                   512,
	}

	errTest = errors.New("test error")
)

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

	parser, err := txs.NewParser(
		time.Time{},
		[]fxs.Fx{
			&secp256k1fx.Fx{},
		},
	)
	require.NoError(t, err)
	require.NoError(t, testTx.Initialize(parser.Codec()))

	type test struct {
		name           string
		msgBytesFunc   func() []byte
		mempoolFunc    func(*gomock.Controller) mempool.Mempool
		txVerifierFunc func(*gomock.Controller) TxVerifier
		appSenderFunc  func(*gomock.Controller) common.AppSender
	}

	tests := []test{
		{
			name: "invalid message bytes",
			msgBytesFunc: func() []byte {
				return []byte{0x00}
			},
		},
		{
			name: "invalid tx bytes",
			msgBytesFunc: func() []byte {
				msg := message.Tx{
					Tx: []byte{0x00},
				}
				msgBytes, err := message.Build(&msg)
				require.NoError(t, err)
				return msgBytes
			},
		},
		{
			name: "tx already in mempool",
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
				mempool.EXPECT().Get(gomock.Any()).Return(testTx, true)
				return mempool
			},
		},
		{
			name: "tx previously dropped",
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
		},
		{
			name: "transaction invalid",
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
				mempool.EXPECT().MarkDropped(gomock.Any(), gomock.Any())
				return mempool
			},
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executor.NewMockManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(errTest)
				return txVerifier
			},
		},
		{
			name: "happy path",
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
				mempool.EXPECT().Len().Return(0)
				mempool.EXPECT().RequestBuildBlock()
				return mempool
			},
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executor.NewMockManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				appSender := common.NewMockSender(ctrl)
				appSender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any()).Return(nil).Times(2)
				return appSender
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			parser, err := txs.NewParser(
				time.Time{},
				[]fxs.Fx{
					&secp256k1fx.Fx{},
					&nftfx.Fx{},
					&propertyfx.Fx{},
				},
			)
			require.NoError(err)

			mempoolFunc := func(ctrl *gomock.Controller) mempool.Mempool {
				return mempool.NewMockMempool(ctrl)
			}
			if tt.mempoolFunc != nil {
				mempoolFunc = tt.mempoolFunc
			}

			txVerifierFunc := func(ctrl *gomock.Controller) TxVerifier {
				return executor.NewMockManager(ctrl)
			}
			if tt.txVerifierFunc != nil {
				txVerifierFunc = tt.txVerifierFunc
			}

			appSenderFunc := func(ctrl *gomock.Controller) common.AppSender {
				return common.NewMockSender(ctrl)
			}
			if tt.appSenderFunc != nil {
				appSenderFunc = tt.appSenderFunc
			}

			n, err := New(
				&snow.Context{
					Log: logging.NoLog{},
				},
				parser,
				txVerifierFunc(ctrl),
				mempoolFunc(ctrl),
				appSenderFunc(ctrl),
				prometheus.NewRegistry(),
				testConfig,
			)
			require.NoError(err)
			require.NoError(n.AppGossip(context.Background(), ids.GenerateTestNodeID(), tt.msgBytesFunc()))
		})
	}
}

func TestNetworkIssueTx(t *testing.T) {
	type test struct {
		name           string
		mempoolFunc    func(*gomock.Controller) mempool.Mempool
		txVerifierFunc func(*gomock.Controller) TxVerifier
		appSenderFunc  func(*gomock.Controller) common.AppSender
		expectedErr    error
	}

	tests := []test{
		{
			name: "mempool has transaction",
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, true)
				return mempool
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
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executor.NewMockManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(errTest)
				return txVerifier
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
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executor.NewMockManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
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
				mempool.EXPECT().Len().Return(0)
				mempool.EXPECT().RequestBuildBlock()
				return mempool
			},
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executor.NewMockManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
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

			parser, err := txs.NewParser(
				time.Time{},
				[]fxs.Fx{
					&secp256k1fx.Fx{},
					&nftfx.Fx{},
					&propertyfx.Fx{},
				},
			)
			require.NoError(err)

			mempoolFunc := func(ctrl *gomock.Controller) mempool.Mempool {
				return mempool.NewMockMempool(ctrl)
			}
			if tt.mempoolFunc != nil {
				mempoolFunc = tt.mempoolFunc
			}

			txVerifierFunc := func(ctrl *gomock.Controller) TxVerifier {
				return executor.NewMockManager(ctrl)
			}
			if tt.txVerifierFunc != nil {
				txVerifierFunc = tt.txVerifierFunc
			}

			appSenderFunc := func(ctrl *gomock.Controller) common.AppSender {
				return common.NewMockSender(ctrl)
			}
			if tt.appSenderFunc != nil {
				appSenderFunc = tt.appSenderFunc
			}

			n, err := New(
				&snow.Context{
					Log: logging.NoLog{},
				},
				parser,
				txVerifierFunc(ctrl),
				mempoolFunc(ctrl),
				appSenderFunc(ctrl),
				prometheus.NewRegistry(),
				testConfig,
			)
			require.NoError(err)
			err = n.IssueTx(context.Background(), &txs.Tx{})
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestNetworkIssueVerifiedTx(t *testing.T) {
	type test struct {
		name          string
		mempoolFunc   func(*gomock.Controller) mempool.Mempool
		appSenderFunc func(*gomock.Controller) common.AppSender
		expectedErr   error
	}

	tests := []test{
		{
			name: "can't add transaction to mempool",
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Add(gomock.Any()).Return(errTest)
				mempool.EXPECT().MarkDropped(gomock.Any(), gomock.Any())
				return mempool
			},
			expectedErr: errTest,
		},
		{
			name: "happy path",
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Add(gomock.Any()).Return(nil)
				mempool.EXPECT().Len().Return(0)
				mempool.EXPECT().RequestBuildBlock()
				return mempool
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
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

			parser, err := txs.NewParser(
				time.Time{},
				[]fxs.Fx{
					&secp256k1fx.Fx{},
					&nftfx.Fx{},
					&propertyfx.Fx{},
				},
			)
			require.NoError(err)

			mempoolFunc := func(ctrl *gomock.Controller) mempool.Mempool {
				return mempool.NewMockMempool(ctrl)
			}
			if tt.mempoolFunc != nil {
				mempoolFunc = tt.mempoolFunc
			}

			appSenderFunc := func(ctrl *gomock.Controller) common.AppSender {
				return common.NewMockSender(ctrl)
			}
			if tt.appSenderFunc != nil {
				appSenderFunc = tt.appSenderFunc
			}

			n, err := New(
				&snow.Context{
					Log: logging.NoLog{},
				},
				parser,
				executor.NewMockManager(ctrl), // Should never verify a tx
				mempoolFunc(ctrl),
				appSenderFunc(ctrl),
				prometheus.NewRegistry(),
				testConfig,
			)
			require.NoError(err)
			err = n.IssueVerifiedTx(context.Background(), &txs.Tx{})
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestNetworkGossipTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	parser, err := txs.NewParser(
		time.Time{},
		[]fxs.Fx{
			&secp256k1fx.Fx{},
		},
	)
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
		testConfig,
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
