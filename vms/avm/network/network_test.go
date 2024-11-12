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
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/commonmock"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/avm/block/executor/executormock"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/mempool/mempoolmock"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"

	xmempool "github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
)

var (
	testConfig = Config{
		MaxValidatorSetStaleness:                    time.Second,
		TargetGossipSize:                            1,
		PushGossipNumValidators:                     1,
		PushGossipNumPeers:                          0,
		PushRegossipNumValidators:                   1,
		PushRegossipNumPeers:                        0,
		PushGossipDiscardedCacheSize:                1,
		PushGossipMaxRegossipFrequency:              time.Second,
		PushGossipFrequency:                         time.Second,
		PullGossipPollSize:                          1,
		PullGossipFrequency:                         time.Second,
		PullGossipThrottlingPeriod:                  time.Second,
		PullGossipThrottlingLimit:                   1,
		ExpectedBloomFilterElements:                 10,
		ExpectedBloomFilterFalsePositiveProbability: .1,
		MaxBloomFilterFalsePositiveProbability:      .5,
	}

	errTest = errors.New("test error")
)

func TestNetworkIssueTxFromRPC(t *testing.T) {
	type test struct {
		name           string
		mempoolFunc    func(*gomock.Controller) xmempool.Mempool
		txVerifierFunc func(*gomock.Controller) TxVerifier
		appSenderFunc  func(*gomock.Controller) common.AppSender
		expectedErr    error
	}

	tests := []test{
		{
			name: "mempool has transaction",
			mempoolFunc: func(ctrl *gomock.Controller) xmempool.Mempool {
				mempool := mempoolmock.NewMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, true)
				return mempool
			},
			expectedErr: mempool.ErrDuplicateTx,
		},
		{
			name: "transaction marked as dropped in mempool",
			mempoolFunc: func(ctrl *gomock.Controller) xmempool.Mempool {
				mempool := mempoolmock.NewMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(errTest)
				return mempool
			},
			expectedErr: errTest,
		},
		{
			name: "transaction invalid",
			mempoolFunc: func(ctrl *gomock.Controller) xmempool.Mempool {
				mempool := mempoolmock.NewMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().MarkDropped(gomock.Any(), gomock.Any())
				return mempool
			},
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executormock.NewManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(errTest)
				return txVerifier
			},
			expectedErr: errTest,
		},
		{
			name: "can't add transaction to mempool",
			mempoolFunc: func(ctrl *gomock.Controller) xmempool.Mempool {
				mempool := mempoolmock.NewMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().Add(gomock.Any()).Return(errTest)
				mempool.EXPECT().MarkDropped(gomock.Any(), gomock.Any())
				return mempool
			},
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executormock.NewManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
			},
			expectedErr: errTest,
		},
		{
			name: "happy path",
			mempoolFunc: func(ctrl *gomock.Controller) xmempool.Mempool {
				mempool := mempoolmock.NewMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().Add(gomock.Any()).Return(nil)
				mempool.EXPECT().Len().Return(0)
				mempool.EXPECT().RequestBuildBlock()
				mempool.EXPECT().Get(gomock.Any()).Return(nil, true).Times(2)
				return mempool
			},
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executormock.NewManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				appSender := commonmock.NewSender(ctrl)
				appSender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
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
				[]fxs.Fx{
					&secp256k1fx.Fx{},
					&nftfx.Fx{},
					&propertyfx.Fx{},
				},
			)
			require.NoError(err)

			mempoolFunc := func(ctrl *gomock.Controller) xmempool.Mempool {
				return mempoolmock.NewMempool(ctrl)
			}
			if tt.mempoolFunc != nil {
				mempoolFunc = tt.mempoolFunc
			}

			txVerifierFunc := func(ctrl *gomock.Controller) TxVerifier {
				return executormock.NewManager(ctrl)
			}
			if tt.txVerifierFunc != nil {
				txVerifierFunc = tt.txVerifierFunc
			}

			appSenderFunc := func(ctrl *gomock.Controller) common.AppSender {
				return commonmock.NewSender(ctrl)
			}
			if tt.appSenderFunc != nil {
				appSenderFunc = tt.appSenderFunc
			}

			n, err := New(
				logging.NoLog{},
				ids.EmptyNodeID,
				ids.Empty,
				&validatorstest.State{
					GetCurrentHeightF: func(context.Context) (uint64, error) {
						return 0, nil
					},
					GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return nil, nil
					},
				},
				parser,
				txVerifierFunc(ctrl),
				mempoolFunc(ctrl),
				appSenderFunc(ctrl),
				prometheus.NewRegistry(),
				testConfig,
			)
			require.NoError(err)
			err = n.IssueTxFromRPC(&txs.Tx{})
			require.ErrorIs(err, tt.expectedErr)

			require.NoError(n.txPushGossiper.Gossip(context.Background()))
		})
	}
}

func TestNetworkIssueTxFromRPCWithoutVerification(t *testing.T) {
	type test struct {
		name          string
		mempoolFunc   func(*gomock.Controller) xmempool.Mempool
		appSenderFunc func(*gomock.Controller) common.AppSender
		expectedErr   error
	}

	tests := []test{
		{
			name: "can't add transaction to mempool",
			mempoolFunc: func(ctrl *gomock.Controller) xmempool.Mempool {
				mempool := mempoolmock.NewMempool(ctrl)
				mempool.EXPECT().Add(gomock.Any()).Return(errTest)
				mempool.EXPECT().MarkDropped(gomock.Any(), gomock.Any())
				return mempool
			},
			expectedErr: errTest,
		},
		{
			name: "happy path",
			mempoolFunc: func(ctrl *gomock.Controller) xmempool.Mempool {
				mempool := mempoolmock.NewMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, true).Times(2)
				mempool.EXPECT().Add(gomock.Any()).Return(nil)
				mempool.EXPECT().Len().Return(0)
				mempool.EXPECT().RequestBuildBlock()
				return mempool
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				appSender := commonmock.NewSender(ctrl)
				appSender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
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
				[]fxs.Fx{
					&secp256k1fx.Fx{},
					&nftfx.Fx{},
					&propertyfx.Fx{},
				},
			)
			require.NoError(err)

			mempoolFunc := func(ctrl *gomock.Controller) xmempool.Mempool {
				return mempoolmock.NewMempool(ctrl)
			}
			if tt.mempoolFunc != nil {
				mempoolFunc = tt.mempoolFunc
			}

			appSenderFunc := func(ctrl *gomock.Controller) common.AppSender {
				return commonmock.NewSender(ctrl)
			}
			if tt.appSenderFunc != nil {
				appSenderFunc = tt.appSenderFunc
			}

			n, err := New(
				logging.NoLog{},
				ids.EmptyNodeID,
				ids.Empty,
				&validatorstest.State{
					GetCurrentHeightF: func(context.Context) (uint64, error) {
						return 0, nil
					},
					GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return nil, nil
					},
				},
				parser,
				executormock.NewManager(ctrl), // Should never verify a tx
				mempoolFunc(ctrl),
				appSenderFunc(ctrl),
				prometheus.NewRegistry(),
				testConfig,
			)
			require.NoError(err)
			err = n.IssueTxFromRPCWithoutVerification(&txs.Tx{})
			require.ErrorIs(err, tt.expectedErr)

			require.NoError(n.txPushGossiper.Gossip(context.Background()))
		})
	}
}
