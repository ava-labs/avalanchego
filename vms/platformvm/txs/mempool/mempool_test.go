// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

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

	weights := gas.Dimensions{gas.Bandwidth: 1}
	m, err := New(ids.ID{}, weights, "", prometheus.NewRegistry(), nil)
	require.NoError(err)

	lowTx := &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID: ids.ID{1, 2, 3},
						},
						Asset: avax.Asset{},
						In: &secp256k1fx.TransferInput{
							Amt: 1,
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
							TxID: ids.ID{4, 5, 6},
						},
						Asset: avax.Asset{},
						In: &secp256k1fx.TransferInput{
							Amt: 2,
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

	wantComplexity, err := fee.TxComplexity(highTx.Unsigned)
	require.NoError(err)
	require.Equal(wantComplexity, gotTx.Complexity)
	wantGas, err := wantComplexity.ToGas(weights)
	require.NoError(err)
	require.Equal(wantGas, gotTx.Gas)

	m.Remove(gotTx.Tx)

	gotTx, ok = m.Peek()
	require.True(ok)
	require.Equal(lowTx, gotTx.Tx)

	wantComplexity, err = fee.TxComplexity(lowTx.Unsigned)
	require.NoError(err)
	require.Equal(wantComplexity, gotTx.Complexity)
	wantGas, err = wantComplexity.ToGas(weights)
	require.NoError(err)
	require.Equal(wantGas, gotTx.Gas)
}
