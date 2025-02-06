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
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
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
	m, err := New(weights, "", prometheus.NewRegistry(), nil, avaxAssetID)
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
	require.Equal(highTx, gotTx.Tx)
	m.Remove(gotTx.Tx)

	gotTx, ok = m.Peek()
	require.True(ok)
	require.Equal(lowTx, gotTx.Tx)
}

// Txs should be metered based off of their complexity
func TestMempoolMetering(t *testing.T) {
	require := require.New(t)

	avaxAssetID := ids.ID{1, 2, 3}
	weights := gas.Dimensions{gas.Bandwidth: 1}

	linearCodec := linearcodec.NewDefault()
	require.NoError(linearCodec.RegisterType(&secp256k1fx.TransferOutput{}))
	codecManager := codec.NewDefaultManager()
	require.NoError(codecManager.RegisterCodec(0, linearCodec))

	utxos, err := avax.NewUTXOState(memdb.New(), codecManager, false)

	require.NoError(err)
	m, err := New(weights, "", prometheus.NewRegistry(), nil, avaxAssetID)
	require.NoError(err)

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

	tx := &txs.Tx{
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
							Amt: 4,
						},
					},
				},
			},
		},
		TxID: ids.GenerateTestID(),
	}
	require.NoError(m.Add(tx))

	gotTx, ok := m.Peek()
	require.True(ok)
	require.Equal(tx, gotTx.Tx)

	wantComplexity, err := fee.TxComplexity(tx.Unsigned)
	require.NoError(err)
	require.Equal(wantComplexity, gotTx.Complexity)

	// fees paid = consumed avax - produced avax = 5 - 4 = 1
	feesPaid := 1
	require.NoError(err)
	wantGasUsage, err := wantComplexity.ToGas(weights)
	require.NoError(err)
	// gas price = consumed - produced / gas usage = 5 - 4  = 1
	wantHighTxGasPrice := float64(feesPaid) / float64(wantGasUsage)
	require.Equal(wantHighTxGasPrice, gotTx.GasPrice)
}
