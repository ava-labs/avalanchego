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
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/avm/block/executor/executormock"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
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
		PullGossipRequestsPerValidator:              1,
		ExpectedBloomFilterElements:                 10,
		ExpectedBloomFilterFalsePositiveProbability: .1,
		MaxBloomFilterFalsePositiveProbability:      .5,
	}

	errTest = errors.New("test error")
)

func TestNetworkIssueTxFromRPC(t *testing.T) {
	type test struct {
		name           string
		mempool        mempool.Mempool[*txs.Tx]
		txVerifierFunc func(*gomock.Controller) TxVerifier
		appSenderFunc  func(*gomock.Controller) common.AppSender
		tx             *txs.Tx
		expectedErr    error
	}

	tests := []test{
		{
			name: "mempool has transaction",
			mempool: func() mempool.Mempool[*txs.Tx] {
				mempool, err := xmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				require.NoError(t, mempool.Add(&txs.Tx{Unsigned: &txs.BaseTx{}}))
				return mempool
			}(),
			tx:          &txs.Tx{Unsigned: &txs.BaseTx{}},
			expectedErr: nil,
		},
		{
			name: "transaction marked as dropped in mempool",
			mempool: func() mempool.Mempool[*txs.Tx] {
				mempool, err := xmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				mempool.MarkDropped(ids.Empty, errTest)
				return mempool
			}(),
			tx:          &txs.Tx{Unsigned: &txs.BaseTx{}},
			expectedErr: errTest,
		},
		{
			name: "tx too big",
			mempool: func() mempool.Mempool[*txs.Tx] {
				mempool, err := xmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return mempool
			}(),
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executormock.NewManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
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
			mempool: func() mempool.Mempool[*txs.Tx] {
				mempool, err := xmempool.New("", prometheus.NewRegistry())
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
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executormock.NewManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
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
			mempool: func() mempool.Mempool[*txs.Tx] {
				m, err := xmempool.New("", prometheus.NewRegistry())
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
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executormock.NewManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
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
			mempool: func() mempool.Mempool[*txs.Tx] {
				mempool, err := xmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return mempool
			}(),
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
			tx:          &txs.Tx{Unsigned: &txs.BaseTx{}},
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
				tt.mempool,
				appSenderFunc(ctrl),
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

func TestNetworkIssueTxFromRPCWithoutVerification(t *testing.T) {
	type test struct {
		name          string
		mempool       mempool.Mempool[*txs.Tx]
		appSenderFunc func(*gomock.Controller) common.AppSender
		expectedErr   error
	}

	tests := []test{
		{
			name: "happy path",
			mempool: func() mempool.Mempool[*txs.Tx] {
				mempool, err := xmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return mempool
			}(),
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
				tt.mempool,
				appSenderFunc(ctrl),
				prometheus.NewRegistry(),
				testConfig,
			)
			require.NoError(err)
			err = n.IssueTxFromRPCWithoutVerification(&txs.Tx{Unsigned: &txs.BaseTx{}})
			require.ErrorIs(err, tt.expectedErr)

			require.NoError(n.txPushGossiper.Gossip(t.Context()))
		})
	}
}
