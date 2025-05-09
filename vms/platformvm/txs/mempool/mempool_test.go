// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"
)

// Txs should be prioritized by highest gas price during after Etna
func TestMempoolOrdering(t *testing.T) {
	require := require.New(t)

	avaxAssetID := ids.ID{1, 2, 3}
	weights := gas.Dimensions{gas.Bandwidth: 1}

	linearCodec := linearcodec.NewDefault()
	require.NoError(linearCodec.RegisterType(&secp256k1fx.TransferOutput{}))
	codecManager := codec.NewDefaultManager()
	require.NoError(codecManager.RegisterCodec(0, linearCodec))

	utxos, err := avax.NewUTXOState(memdb.New(), codecManager, false)

	require.NoError(err)
	m, err := New(
		"",
		weights,
		1_000_000,
		avaxAssetID,
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	require.NoError(utxos.PutUTXO(
		&avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID: ids.ID{1, 2, 3},
			},
			Asset: avax.Asset{
				ID: avaxAssetID,
			},
			Out: &secp256k1fx.TransferOutput{
				Amt:          5,
				OutputOwners: secp256k1fx.OutputOwners{},
			},
		},
	))

	lowTx := &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID: ids.ID{1, 2, 3},
						},
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: 5,
						},
					},
				},
				Outs: []*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: 4,
						},
					},
				},
			},
		},
		TxID: ids.GenerateTestID(),
	}

	require.NoError(m.Add(lowTx))

	require.NoError(utxos.PutUTXO(
		&avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID: ids.ID{4, 5, 6},
			},
			Asset: avax.Asset{
				ID: avaxAssetID,
			},
			Out: &secp256k1fx.TransferOutput{
				Amt:          5,
				OutputOwners: secp256k1fx.OutputOwners{},
			},
		},
	))

	highTx := &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID: ids.ID{4, 5, 6},
						},
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: 5,
						},
					},
				},
				Outs: []*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
			},
		},
		TxID: ids.GenerateTestID(),
	}
	require.NoError(m.Add(highTx))

	gotTx, ok := m.Peek()
	require.True(ok)
	require.Equal(highTx, gotTx)
	m.Remove(gotTx)

	gotTx, ok = m.Peek()
	require.True(ok)
	require.Equal(lowTx, gotTx)
}

func TestMempoolAdd(t *testing.T) {
	avaxAssetID := ids.ID{1, 2, 3}

	tests := []struct {
		name           string
		weights        gas.Dimensions
		maxGasCapacity gas.Gas
		prevTxs        []*txs.Tx
		tx             *txs.Tx
		wantErr        error
		wantTxIDs      []ids.ID
	}{
		// TODO no conflicts are stopped
		{
			name: "conflict - lower paying tx conflicts",
			weights: gas.Dimensions{
				gas.Bandwidth: 1,
			},
			maxGasCapacity: 100,
			prevTxs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{1, 2, 3},
									},
									Asset: avax.Asset{
										ID: avaxAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 2,
									},
								},
							},
						},
					},
					TxID: ids.ID{0},
				},
			},
			tx: &txs.Tx{
				Unsigned: &txs.BaseTx{
					BaseTx: avax.BaseTx{
						Ins: []*avax.TransferableInput{
							{
								UTXOID: avax.UTXOID{
									TxID: ids.ID{1, 2, 3},
								},
								Asset: avax.Asset{
									ID: avaxAssetID,
								},
								In: &secp256k1fx.TransferInput{
									Amt: 1,
								},
							},
						},
					},
				},
				TxID: ids.ID{1},
			},
			wantErr: ErrGasCapacityExceeded,
			wantTxIDs: []ids.ID{
				{0},
			},
		},
		{
			name: "conflict - equal paying tx conflicts",
			weights: gas.Dimensions{
				gas.Bandwidth: 1,
			},
			maxGasCapacity: 100,
			prevTxs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{1, 2, 3},
									},
									Asset: avax.Asset{
										ID: avaxAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 5,
									},
								},
							},
						},
					},
					TxID: ids.ID{0},
				},
			},
			tx: &txs.Tx{
				Unsigned: &txs.BaseTx{
					BaseTx: avax.BaseTx{
						Ins: []*avax.TransferableInput{
							{
								UTXOID: avax.UTXOID{
									TxID: ids.ID{1, 2, 3},
								},
								Asset: avax.Asset{
									ID: avaxAssetID,
								},
								In: &secp256k1fx.TransferInput{
									Amt: 5,
								},
							},
						},
					},
				},
				TxID: ids.ID{1},
			},
			wantErr: ErrGasCapacityExceeded,
			wantTxIDs: []ids.ID{
				{0},
			},
		},
		{
			name: "evict - higher paying tx conflicts",
			weights: gas.Dimensions{
				gas.Bandwidth: 1,
			},
			maxGasCapacity: 200,
			prevTxs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{4, 5, 6},
									},
									Asset: avax.Asset{
										ID: avaxAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 5,
									},
								},
							},
						},
					},
					TxID: ids.ID{0},
				},
			},
			tx: &txs.Tx{
				Unsigned: &txs.BaseTx{
					BaseTx: avax.BaseTx{
						Ins: []*avax.TransferableInput{
							{
								UTXOID: avax.UTXOID{
									TxID: ids.ID{4, 5, 6},
								},
								Asset: avax.Asset{
									ID: avaxAssetID,
								},
								In: &secp256k1fx.TransferInput{
									Amt: 10,
								},
							},
						},
					},
				},
				TxID: ids.ID{1},
			},
			wantTxIDs: []ids.ID{
				{1},
			},
		},
		{
			name: "evict - higher paying tx conflicts with multiple txs",
			weights: gas.Dimensions{
				gas.Bandwidth: 1,
			},
			maxGasCapacity: 500,
			prevTxs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{1, 2, 3},
									},
									Asset: avax.Asset{
										ID: avaxAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
					TxID: ids.ID{0},
				},
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{4, 5, 6},
									},
									Asset: avax.Asset{
										ID: avaxAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 10,
									},
								},
							},
						},
					},
					TxID: ids.ID{1},
				},
			},
			tx: &txs.Tx{
				Unsigned: &txs.BaseTx{
					BaseTx: avax.BaseTx{
						Ins: []*avax.TransferableInput{
							{
								UTXOID: avax.UTXOID{
									TxID: ids.ID{1, 2, 3},
								},
								Asset: avax.Asset{
									ID: avaxAssetID,
								},
								In: &secp256k1fx.TransferInput{
									Amt: 2,
								},
							},
							{
								UTXOID: avax.UTXOID{
									TxID: ids.ID{4, 5, 6},
								},
								Asset: avax.Asset{
									ID: avaxAssetID,
								},
								In: &secp256k1fx.TransferInput{
									Amt: 2,
								},
							},
						},
					},
				},
				TxID: ids.ID{2},
			},
			wantErr: mempool.ErrConflictsWithOtherTx,
			wantTxIDs: []ids.ID{
				{0},
				{1},
			},
		},
		{
			name: "evict - higher paying tx has no conflicts",
			weights: gas.Dimensions{
				gas.Bandwidth: 1,
			},
			maxGasCapacity: 200,
			prevTxs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{1, 2, 3},
									},
									Asset: avax.Asset{
										ID: avaxAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 5,
									},
								},
							},
						},
					},
					TxID: ids.ID{0},
				},
			},
			tx: &txs.Tx{
				Unsigned: &txs.BaseTx{
					BaseTx: avax.BaseTx{
						Ins: []*avax.TransferableInput{
							{
								UTXOID: avax.UTXOID{
									TxID: ids.ID{4, 5, 6},
								},
								Asset: avax.Asset{
									ID: avaxAssetID,
								},
								In: &secp256k1fx.TransferInput{
									Amt: 10,
								},
							},
						},
					},
				},
				TxID: ids.ID{1},
			},
			wantTxIDs: []ids.ID{
				{1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			linearCodec := linearcodec.NewDefault()
			require.NoError(linearCodec.RegisterType(&secp256k1fx.TransferOutput{}))
			codecManager := codec.NewDefaultManager()
			require.NoError(codecManager.RegisterCodec(0, linearCodec))

			m, err := New(
				"",
				tt.weights,
				tt.maxGasCapacity, // TODO test insufficient gas
				avaxAssetID,
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			for _, tx := range tt.prevTxs {
				require.NoError(m.Add(tx))
			}

			require.ErrorIs(m.Add(tt.tx), tt.wantErr)

			for _, wantTxID := range tt.wantTxIDs {
				_, ok := m.Get(wantTxID)
				require.True(ok)
			}

			require.Equal(len(tt.wantTxIDs), m.Len())
		})
	}
}
