// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"
)

// Txs should be prioritized by highest gas price during after Etna
func TestMempoolOrdering(t *testing.T) {
	require := require.New(t)

	avaxAssetID := ids.GenerateTestID()
	weights := gas.Dimensions{gas.Bandwidth: 1}

	linearCodec := linearcodec.NewDefault()
	require.NoError(linearCodec.RegisterType(&secp256k1fx.TransferOutput{}))
	codecManager := codec.NewDefaultManager()
	require.NoError(codecManager.RegisterCodec(0, linearCodec))

	m, err := New(
		"",
		weights,
		1_000_000,
		avaxAssetID,
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	lowTx := &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID: ids.GenerateTestID(),
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

	highTx := &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID: ids.GenerateTestID(),
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
	m.Remove(gotTx.ID())

	gotTx, ok = m.Peek()
	require.True(ok)
	require.Equal(lowTx, gotTx)
}

func TestMempoolAdd(t *testing.T) {
	avaxAssetID := ids.GenerateTestID()

	tests := []struct {
		name           string
		weights        gas.Dimensions
		maxGasCapacity gas.Gas
		prevTxs        []*txs.Tx
		tx             *txs.Tx
		wantErr        error
		wantTxIDs      []ids.ID
	}{
		{
			name: "dropped - AdvanceTimeTx",
			tx: &txs.Tx{
				Unsigned: &txs.AdvanceTimeTx{},
			},
			wantErr: utxo.ErrUnsupportedTxType,
		},
		{
			name: "dropped - RewardValidatorTx",
			tx: &txs.Tx{
				Unsigned: &txs.RewardValidatorTx{},
			},
			wantErr: utxo.ErrUnsupportedTxType,
		},
		{
			name: "dropped - no input AVAX",
			tx: &txs.Tx{
				Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{}},
			},
			wantErr: errMissingConsumedAVAX,
		},
		{
			name:    "dropped - no gas",
			weights: gas.Dimensions{},
			tx: &txs.Tx{
				Unsigned: &txs.BaseTx{
					BaseTx: avax.BaseTx{
						Ins: []*avax.TransferableInput{
							{
								UTXOID: avax.UTXOID{
									TxID: ids.GenerateTestID(),
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
				TxID: ids.GenerateTestID(),
			},
			wantErr: errNoGasUsed,
		},
		{
			name: "conflict - lower paying tx is not added",
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
				TxID: ids.GenerateTestID(),
			},
			wantErr: ErrGasCapacityExceeded,
			wantTxIDs: []ids.ID{
				{0},
			},
		},
		{
			name: "conflict - equal paying tx is not added",
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
				TxID: ids.GenerateTestID(),
			},
			wantErr: ErrGasCapacityExceeded,
			wantTxIDs: []ids.ID{
				{0},
			},
		},
		{
			name: "conflict - higher paying tx is added",
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
									TxID: ids.ID{1, 2, 3},
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
			name: "evict - higher paying tx without conflicts is added",
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
										TxID: ids.GenerateTestID(),
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
									TxID: ids.GenerateTestID(),
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
				tt.maxGasCapacity,
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

func TestMempool_Remove(t *testing.T) {
	avaxAssetID := ids.GenerateTestID()

	tests := []struct {
		name         string
		txs          []*txs.Tx
		txIDToRemove ids.ID
		wantRemove   bool
	}{
		{
			name:         "remove tx not in mempool - empty",
			txIDToRemove: ids.GenerateTestID(),
		},
		{
			name: "remove tx not in mempool - populated",
			txs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.GenerateTestID(),
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
					TxID: ids.GenerateTestID(),
				},
			},
			txIDToRemove: ids.GenerateTestID(),
		},
		{
			name: "remove tx in mempool",
			txs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.GenerateTestID(),
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
					TxID: ids.ID{1, 2, 3},
				},
			},
			txIDToRemove: ids.ID{1, 2, 3},
			wantRemove:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			m, err := New(
				"",
				gas.Dimensions{1, 1, 1, 1},
				1_000_000,
				avaxAssetID,
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			for _, tx := range tt.txs {
				require.NoError(m.Add(tx))
			}

			m.Remove(tt.txIDToRemove)
			_, gotOk := m.Get(tt.txIDToRemove)
			require.False(gotOk)
		})
	}
}

func TestMempool_RemoveConflicts(t *testing.T) {
	avaxAssetID := ids.GenerateTestID()

	tests := []struct {
		name              string
		txs               []*txs.Tx
		conflictsToRemove set.Set[ids.ID]
		wantTxs           []ids.ID
	}{
		{
			name: "remove conflicts not in mempool - empty",
		},
		{
			name: "remove conflicts not in mempool - populated",
			txs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{1},
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
					TxID: ids.ID{2},
				},
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{3},
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
					TxID: ids.ID{4},
				},
			},
			conflictsToRemove: set.Of[ids.ID](
				ids.ID{1}.Prefix(0),
				ids.ID{3}.Prefix(0),
			),
		},
		{
			name: "remove conflicts not in mempool - populated",
			txs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{1},
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
					TxID: ids.ID{2},
				},
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{3},
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
					TxID: ids.ID{4},
				},
			},
			conflictsToRemove: set.Of[ids.ID](ids.ID{1}.Prefix(0)),
			wantTxs:           []ids.ID{{4}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			m, err := New(
				"",
				gas.Dimensions{1, 1, 1, 1},
				1_000_000,
				avaxAssetID,
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			for _, tx := range tt.txs {
				require.NoError(m.Add(tx))
			}

			m.RemoveConflicts(tt.conflictsToRemove)

			require.Equal(len(tt.wantTxs), m.Len())
			for _, wantTxID := range tt.wantTxs {
				_, ok := m.Get(wantTxID)
				require.True(ok)
			}
		})
	}
}
