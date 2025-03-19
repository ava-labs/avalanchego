// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Txs should be prioritized by highest gas price during after Etna
func TestMempoolOrdering(t *testing.T) {
	require := require.New(t)

	weights := gas.Dimensions{gas.Bandwidth: 1}
	m, err := New(
		&config.Internal{
			DynamicFeeConfig: gas.Config{
				Weights: gas.Dimensions{1},
			},
		},
		"",
		prometheus.NewRegistry(),
		time.Time{},
		nil,
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
						In: &secp256k1fx.TransferInput{
							Input: secp256k1fx.Input{},
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
						In: &secp256k1fx.TransferInput{},
					},
					{
						UTXOID: avax.UTXOID{
							TxID: ids.GenerateTestID(),
						},
						In: &secp256k1fx.TransferInput{},
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
