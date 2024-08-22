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
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool/mempoolmock"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"

	pmempool "github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

var (
	errTest = errors.New("test error")

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
)

var _ TxVerifier = (*testTxVerifier)(nil)

type testTxVerifier struct {
	err error
}

func (t testTxVerifier) VerifyTx(*txs.Tx) error {
	return t.err
}

func TestNetworkIssueTxFromRPC(t *testing.T) {
	tx := &txs.Tx{}

	type test struct {
		name                      string
		mempoolFunc               func(*gomock.Controller) pmempool.Mempool
		txVerifier                testTxVerifier
		partialSyncPrimaryNetwork bool
		appSenderFunc             func(*gomock.Controller) common.AppSender
		expectedErr               error
	}

	tests := []test{
		{
			name: "mempool has transaction",
			mempoolFunc: func(ctrl *gomock.Controller) pmempool.Mempool {
				mempool := mempoolmock.NewMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(tx, true)
				return mempool
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				return commonmock.NewSender(ctrl)
			},
			expectedErr: mempool.ErrDuplicateTx,
		},
		{
			name: "transaction marked as dropped in mempool",
			mempoolFunc: func(ctrl *gomock.Controller) pmempool.Mempool {
				mempool := mempoolmock.NewMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(errTest)
				return mempool
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return commonmock.NewSender(ctrl)
			},
			expectedErr: errTest,
		},
		{
			name: "transaction invalid",
			mempoolFunc: func(ctrl *gomock.Controller) pmempool.Mempool {
				mempool := mempoolmock.NewMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().MarkDropped(gomock.Any(), gomock.Any())
				return mempool
			},
			txVerifier: testTxVerifier{err: errTest},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return commonmock.NewSender(ctrl)
			},
			expectedErr: errTest,
		},
		{
			name: "can't add transaction to mempool",
			mempoolFunc: func(ctrl *gomock.Controller) pmempool.Mempool {
				mempool := mempoolmock.NewMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().Add(gomock.Any()).Return(errTest)
				mempool.EXPECT().MarkDropped(gomock.Any(), gomock.Any())
				return mempool
			},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return commonmock.NewSender(ctrl)
			},
			expectedErr: errTest,
		},
		{
			name: "mempool is disabled if primary network is not being fully synced",
			mempoolFunc: func(ctrl *gomock.Controller) pmempool.Mempool {
				return mempoolmock.NewMempool(ctrl)
			},
			partialSyncPrimaryNetwork: true,
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				return commonmock.NewSender(ctrl)
			},
			expectedErr: errMempoolDisabledWithPartialSync,
		},
		{
			name: "happy path",
			mempoolFunc: func(ctrl *gomock.Controller) pmempool.Mempool {
				mempool := mempoolmock.NewMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().Add(gomock.Any()).Return(nil)
				mempool.EXPECT().Len().Return(0)
				mempool.EXPECT().RequestBuildBlock(false)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, true).Times(2)
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

			snowCtx := snowtest.Context(t, ids.Empty)
			n, err := New(
				snowCtx.Log,
				snowCtx.NodeID,
				snowCtx.SubnetID,
				snowCtx.ValidatorState,
				tt.txVerifier,
				tt.mempoolFunc(ctrl),
				tt.partialSyncPrimaryNetwork,
				tt.appSenderFunc(ctrl),
				prometheus.NewRegistry(),
				testConfig,
			)
			require.NoError(err)

			err = n.IssueTxFromRPC(tx)
			require.ErrorIs(err, tt.expectedErr)

			require.NoError(n.txPushGossiper.Gossip(context.Background()))
		})
	}
}
