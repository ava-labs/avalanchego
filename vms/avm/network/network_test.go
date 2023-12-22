// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"testing"

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
		managerFunc   func(*gomock.Controller) executor.Manager
		appSenderFunc func(*gomock.Controller) common.AppSender
	}

	tests := []test{
		{
			name: "invalid message bytes",
			msgBytesFunc: func() []byte {
				return []byte{0x00}
			},
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				return mempool.NewMockMempool(ctrl)
			},
			managerFunc: func(ctrl *gomock.Controller) executor.Manager {
				return executor.NewMockManager(ctrl)
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				return common.NewMockSender(ctrl)
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
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				return mempool.NewMockMempool(ctrl)
			},
			managerFunc: func(ctrl *gomock.Controller) executor.Manager {
				return executor.NewMockManager(ctrl)
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				return common.NewMockSender(ctrl)
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
			managerFunc: func(ctrl *gomock.Controller) executor.Manager {
				return executor.NewMockManager(ctrl)
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				return common.NewMockSender(ctrl)
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
			managerFunc: func(ctrl *gomock.Controller) executor.Manager {
				return executor.NewMockManager(ctrl)
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				return common.NewMockSender(ctrl)
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
			managerFunc: func(ctrl *gomock.Controller) executor.Manager {
				manager := executor.NewMockManager(ctrl)
				manager.EXPECT().VerifyTx(gomock.Any()).Return(errTest)
				return manager
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				return common.NewMockSender(ctrl)
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
				mempool.EXPECT().RequestBuildBlock()
				return mempool
			},
			managerFunc: func(ctrl *gomock.Controller) executor.Manager {
				manager := executor.NewMockManager(ctrl)
				manager.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return manager
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				appSender := common.NewMockSender(ctrl)
				appSender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any()).Return(nil)
				return appSender
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

			n := New(
				&snow.Context{
					Log: logging.NoLog{},
				},
				parser,
				tt.managerFunc(ctrl),
				tt.mempoolFunc(ctrl),
				tt.appSenderFunc(ctrl),
			)
			require.NoError(n.AppGossip(context.Background(), ids.GenerateTestNodeID(), tt.msgBytesFunc()))
		})
	}
}

func TestNetworkIssueTx(t *testing.T) {
	type test struct {
		name          string
		mempoolFunc   func(*gomock.Controller) mempool.Mempool
		managerFunc   func(*gomock.Controller) executor.Manager
		appSenderFunc func(*gomock.Controller) common.AppSender
		expectedErr   error
	}

	tests := []test{
		{
			name: "mempool has transaction",
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, true)
				return mempool
			},
			managerFunc: func(ctrl *gomock.Controller) executor.Manager {
				return executor.NewMockManager(ctrl)
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				return common.NewMockSender(ctrl)
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
			managerFunc: func(ctrl *gomock.Controller) executor.Manager {
				return executor.NewMockManager(ctrl)
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				return common.NewMockSender(ctrl)
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
			managerFunc: func(ctrl *gomock.Controller) executor.Manager {
				manager := executor.NewMockManager(ctrl)
				manager.EXPECT().VerifyTx(gomock.Any()).Return(errTest)
				return manager
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
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
			managerFunc: func(ctrl *gomock.Controller) executor.Manager {
				manager := executor.NewMockManager(ctrl)
				manager.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return manager
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
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
			managerFunc: func(ctrl *gomock.Controller) executor.Manager {
				manager := executor.NewMockManager(ctrl)
				manager.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return manager
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				appSender := common.NewMockSender(ctrl)
				appSender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any()).Return(nil)
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

			n := New(
				&snow.Context{
					Log: logging.NoLog{},
				},
				parser,
				tt.managerFunc(ctrl),
				tt.mempoolFunc(ctrl),
				tt.appSenderFunc(ctrl),
			)
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
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				return common.NewMockSender(ctrl)
			},
			expectedErr: errTest,
		},
		{
			name: "happy path",
			mempoolFunc: func(ctrl *gomock.Controller) mempool.Mempool {
				mempool := mempool.NewMockMempool(ctrl)
				mempool.EXPECT().Add(gomock.Any()).Return(nil)
				mempool.EXPECT().RequestBuildBlock()
				return mempool
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				appSender := common.NewMockSender(ctrl)
				appSender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any()).Return(nil)
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

			n := New(
				&snow.Context{
					Log: logging.NoLog{},
				},
				parser,
				executor.NewMockManager(ctrl), // Should never verify a tx
				tt.mempoolFunc(ctrl),
				tt.appSenderFunc(ctrl),
			)
			err = n.IssueVerifiedTx(context.Background(), &txs.Tx{})
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

	nIntf := New(
		&snow.Context{
			Log: logging.NoLog{},
		},
		parser,
		executor.NewMockManager(ctrl),
		mempool.NewMockMempool(ctrl),
		appSender,
	)
	require.IsType(&network{}, nIntf)
	n := nIntf.(*network)

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
