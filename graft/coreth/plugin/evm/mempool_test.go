// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestMempoolAddTx(t *testing.T) {
	require := require.New(t)
	m, err := NewMempool(ids.Empty, 5_000)
	require.NoError(err)

	txs := make([]*GossipAtomicTx, 0)
	for i := 0; i < 3_000; i++ {
		tx := &GossipAtomicTx{
			Tx: &Tx{
				UnsignedAtomicTx: &TestUnsignedTx{
					IDV: ids.GenerateTestID(),
				},
			},
		}

		txs = append(txs, tx)
		require.NoError(m.Add(tx))
	}

	for _, tx := range txs {
		require.True(m.bloom.Has(tx))
	}
}
