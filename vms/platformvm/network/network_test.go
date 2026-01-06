// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
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
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
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
		PullGossipFrequency:                         time.Second,
		PullGossipThrottlingPeriod:                  time.Second,
		PullGossipRequestsPerValidator:              1,
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
				mempool, err := pmempool.New(
					"",
					gas.Dimensions{1, 1, 1, 1},
					1_000_000,
					snowtest.AVAXAssetID,
					prometheus.NewRegistry(),
				)
				require.NoError(t, err)
				require.NoError(t, mempool.Add(&txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									Asset: avax.Asset{
										ID: snowtest.AVAXAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
				}))
				return mempool
			}(),
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				return commonmock.NewSender(ctrl)
			},
			tx: &txs.Tx{
				Unsigned: &txs.BaseTx{
					BaseTx: avax.BaseTx{
						Ins: []*avax.TransferableInput{
							{
								Asset: avax.Asset{
									ID: snowtest.AVAXAssetID,
								},
								In: &secp256k1fx.TransferInput{
									Amt: 1,
								},
							},
						},
					},
				},
			},
			expectedErr: mempool.ErrDuplicateTx,
		},
		{
			name: "transaction marked as dropped in mempool",
			mempool: func() *pmempool.Mempool {
				mempool, err := pmempool.New(
					"",
					gas.Dimensions{1, 1, 1, 1},
					1_000_000,
					snowtest.AVAXAssetID,
					prometheus.NewRegistry(),
				)
				require.NoError(t, err)
				mempool.MarkDropped(ids.Empty, errTest)
				return mempool
			}(),
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return commonmock.NewSender(ctrl)
			},
			tx: &txs.Tx{
				Unsigned: &txs.BaseTx{
					BaseTx: avax.BaseTx{
						Ins: []*avax.TransferableInput{
							{
								Asset: avax.Asset{
									ID: snowtest.AVAXAssetID,
								},
								In: &secp256k1fx.TransferInput{
									Amt: 1,
								},
							},
						},
					},
				},
			},
			expectedErr: errTest,
		},
		{
			name: "tx dropped",
			mempool: func() *pmempool.Mempool {
				mempool, err := pmempool.New(
					"",
					gas.Dimensions{1, 1, 1, 1},
					1_000_000,
					snowtest.AVAXAssetID,
					prometheus.NewRegistry(),
				)
				require.NoError(t, err)
				return mempool
			}(),
			txVerifier: testTxVerifier{err: errTest},
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return commonmock.NewSender(ctrl)
			},
			tx: &txs.Tx{
				Unsigned: &txs.BaseTx{
					BaseTx: avax.BaseTx{
						Ins: []*avax.TransferableInput{
							{
								Asset: avax.Asset{
									ID: snowtest.AVAXAssetID,
								},
								In: &secp256k1fx.TransferInput{
									Amt: 1,
								},
							},
						},
					},
				},
			},
			expectedErr: errTest,
		},
		{
			name: "tx conflicts",
			mempool: func() *pmempool.Mempool {
				mempool, err := pmempool.New(
					"",
					gas.Dimensions{1, 1, 1, 1},
					1_000_000,
					snowtest.AVAXAssetID,
					prometheus.NewRegistry(),
				)
				require.NoError(t, err)

				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									Asset: avax.Asset{
										ID: snowtest.AVAXAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
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
									Asset: avax.Asset{
										ID: snowtest.AVAXAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
					TxID: ids.GenerateTestID(),
				}
				return tx
			}(),
			expectedErr: mempool.ErrConflictsWithOtherTx,
		},
		{
			name: "mempool gas capacity exceeded",
			mempool: func() *pmempool.Mempool {
				m, err := pmempool.New(
					"",
					gas.Dimensions{1, 1, 1, 1},
					0,
					snowtest.AVAXAssetID,
					prometheus.NewRegistry(),
				)
				require.NoError(t, err)

				return m
			}(),
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				// Shouldn't gossip the tx
				return commonmock.NewSender(ctrl)
			},
			tx: func() *txs.Tx {
				return &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									Asset: avax.Asset{
										ID: snowtest.AVAXAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
				}
			}(),
			expectedErr: pmempool.ErrNotEnoughGas,
		},
		{
			name: "happy path",
			mempool: func() *pmempool.Mempool {
				mempool, err := pmempool.New(
					"",
					gas.Dimensions{1, 1, 1, 1},
					1_000_000,
					snowtest.AVAXAssetID,
					prometheus.NewRegistry(),
				)
				require.NoError(t, err)
				return mempool
			}(),
			appSenderFunc: func(ctrl *gomock.Controller) common.AppSender {
				appSender := commonmock.NewSender(ctrl)
				appSender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				return appSender
			},
			tx: &txs.Tx{
				Unsigned: &txs.BaseTx{
					BaseTx: avax.BaseTx{
						Ins: []*avax.TransferableInput{
							{
								Asset: avax.Asset{
									ID: snowtest.AVAXAssetID,
								},
								In: &secp256k1fx.TransferInput{
									Amt: 1,
								},
							},
						},
					},
				},
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

			require.NoError(n.txPushGossiper.Gossip(t.Context()))
		})
	}
}
