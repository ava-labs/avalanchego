// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"

	pmempool "github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

var (
	errTest = errors.New("test error")

	testConfig = config.Network{
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
	type test struct {
		name          string
		mempool       *pmempool.Mempool
		txVerifier    testTxVerifier
		appSenderFunc func(*gomock.Controller) common.AppSender
		tx            *txs.Tx
		expectedErr   error
	}

	tests := []test{
		{
			name: "mempool has transaction",
			mempool: func() *pmempool.Mempool {
				mempool, err := pmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				require.NoError(t, mempool.Add(&txs.Tx{Unsigned: &txs.BaseTx{}}))
				return mempool
			}(),
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				return commonmock.NewSender(ctrl)
			},
			tx:          &txs.Tx{Unsigned: &txs.BaseTx{}},
			expectedErr: mempool.ErrDuplicateTx,
		},
		{
			name: "transaction marked as dropped in mempool",
			mempool: func() *pmempool.Mempool {
				mempool, err := pmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				mempool.MarkDropped(ids.Empty, errTest)
				return mempool
			}(),
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return commonmock.NewSender(ctrl)
			},
			tx:          &txs.Tx{Unsigned: &txs.BaseTx{}},
			expectedErr: errTest,
		},
		{
			name: "tx dropped",
			mempool: func() *pmempool.Mempool {
				mempool, err := pmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return mempool
			}(),
			txVerifier: testTxVerifier{err: errTest},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return commonmock.NewSender(ctrl)
			},
			tx:          &txs.Tx{Unsigned: &txs.BaseTx{}},
			expectedErr: errTest,
		},
		{
			name: "tx too big",
			mempool: func() *pmempool.Mempool {
				mempool, err := pmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return mempool
			}(),
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return commonmock.NewSender(ctrl)
			},
			tx: func() *txs.Tx {
				tx := &txs.Tx{Unsigned: &txs.BaseTx{}}
				bytes := make([]byte, mempool.MaxTxSize+1)
				tx.SetBytes(bytes, bytes)
				return tx
			}(),
			expectedErr: mempool.ErrTxTooLarge,
		},
		{
			name: "tx conflicts",
			mempool: func() *pmempool.Mempool {
				mempool, err := pmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)

				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{},
								},
							},
						},
					},
				}

				require.NoError(t, mempool.Add(tx))
				return mempool
			}(),
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return commonmock.NewSender(ctrl)
			},
			tx: func() *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{},
								},
							},
						},
					},
					TxID: ids.ID{1},
				}
				return tx
			}(),
			expectedErr: mempool.ErrConflictsWithOtherTx,
		},
		{
			name: "mempool full",
			mempool: func() *pmempool.Mempool {
				m, err := pmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)

				for i := 0; i < 1024; i++ {
					tx := &txs.Tx{Unsigned: &txs.BaseTx{}}
					bytes := make([]byte, mempool.MaxTxSize)
					tx.SetBytes(bytes, bytes)
					tx.TxID = ids.GenerateTestID()
					require.NoError(t, m.Add(tx))
				}

				return m
			}(),
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return commonmock.NewSender(ctrl)
			},
			tx: func() *txs.Tx {
				tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{}}}
				tx.SetBytes([]byte{1, 2, 3}, []byte{1, 2, 3})
				return tx
			}(),
			expectedErr: mempool.ErrMempoolFull,
		},
		{
			name: "happy path",
			mempool: func() *pmempool.Mempool {
				mempool, err := pmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return mempool
			}(),
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				appSender := commonmock.NewSender(ctrl)
				appSender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				return appSender
			},
			tx:          &txs.Tx{Unsigned: &txs.BaseTx{}},
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
				tt.mempool,
				false,
				tt.appSenderFunc(ctrl),
				nil,
				nil,
				nil,
				prometheus.NewRegistry(),
				testConfig,
			)
			require.NoError(err)

			err = n.IssueTxFromRPC(tt.tx)
			require.ErrorIs(err, tt.expectedErr)

			require.NoError(n.txPushGossiper.Gossip(context.Background()))
		})
	}
}
