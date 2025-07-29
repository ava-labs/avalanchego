// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/atomic/atomictest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestMempoolAddTx(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	m, err := NewMempool(
		NewTxs(ctx, 5_000),
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	txs := make([]*atomic.Tx, 0)
	for i := 0; i < 3_000; i++ {
		tx := &atomic.Tx{
			UnsignedAtomicTx: &atomictest.TestUnsignedTx{
				IDV: ids.GenerateTestID(),
			},
		}

		txs = append(txs, tx)
		require.NoError(m.Add(tx))
	}

	for _, tx := range txs {
		require.True(m.bloom.Has(tx))
	}
}

// Add should return an error if a tx is already known
func TestMempoolAdd(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	m, err := NewMempool(
		NewTxs(ctx, 5_000),
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	tx := &atomic.Tx{
		UnsignedAtomicTx: &atomictest.TestUnsignedTx{
			IDV: ids.GenerateTestID(),
		},
	}

	require.NoError(m.Add(tx))
	err = m.Add(tx)
	require.ErrorIs(err, ErrAlreadyKnown)
}

func TestAtomicMempoolIterate(t *testing.T) {
	txs := []*atomic.Tx{
		{
			UnsignedAtomicTx: &atomictest.TestUnsignedTx{
				IDV: ids.GenerateTestID(),
			},
		},
		{
			UnsignedAtomicTx: &atomictest.TestUnsignedTx{
				IDV: ids.GenerateTestID(),
			},
		},
	}

	tests := []struct {
		name        string
		add         []*atomic.Tx
		f           func(tx *atomic.Tx) bool
		expectedTxs []*atomic.Tx
	}{
		{
			name: "func matches nothing",
			add:  txs,
			f: func(*atomic.Tx) bool {
				return false
			},
			expectedTxs: []*atomic.Tx{},
		},
		{
			name: "func matches all",
			add:  txs,
			f: func(*atomic.Tx) bool {
				return true
			},
			expectedTxs: txs,
		},
		{
			name: "func matches subset",
			add:  txs,
			f: func(tx *atomic.Tx) bool {
				return tx == txs[0]
			},
			expectedTxs: []*atomic.Tx{txs[0]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			ctx := snowtest.Context(t, snowtest.CChainID)
			m, err := NewMempool(
				NewTxs(ctx, 10),
				prometheus.NewRegistry(),
				nil,
			)
			require.NoError(err)

			for _, add := range tt.add {
				require.NoError(m.Add(add))
			}

			matches := make([]*atomic.Tx, 0)
			f := func(tx *atomic.Tx) bool {
				match := tt.f(tx)

				if match {
					matches = append(matches, tx)
				}

				return match
			}

			m.Iterate(f)

			require.ElementsMatch(tt.expectedTxs, matches)
		})
	}
}

// a valid tx shouldn't be added to the mempool if this would exceed the
// mempool's max size
func TestMempoolMaxSizeHandling(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	mempool, err := NewMempool(
		NewTxs(ctx, 1),
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)
	// create candidate tx (we will drop before validation)
	tx := atomictest.GenerateTestImportTx()

	require.NoError(mempool.AddRemoteTx(tx))
	require.True(mempool.Has(tx.ID()))
	// promote tx to be issued
	_, ok := mempool.NextTx()
	require.True(ok)
	mempool.IssueCurrentTxs()

	// try to add one more tx
	tx2 := atomictest.GenerateTestImportTx()
	err = mempool.AddRemoteTx(tx2)
	require.ErrorIs(err, ErrMempoolFull)
	require.False(mempool.Has(tx2.ID()))
}

// mempool will drop transaction with the lowest fee
func TestMempoolPriorityDrop(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	mempool, err := NewMempool(
		NewTxs(ctx, 1),
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	tx1 := atomictest.GenerateTestImportTxWithGas(1, 2) // lower fee
	require.NoError(mempool.AddRemoteTx(tx1))
	require.True(mempool.Has(tx1.ID()))

	tx2 := atomictest.GenerateTestImportTxWithGas(1, 2) // lower fee
	err = mempool.AddRemoteTx(tx2)
	require.ErrorIs(err, ErrInsufficientFee)
	require.True(mempool.Has(tx1.ID()))
	require.False(mempool.Has(tx2.ID()))

	tx3 := atomictest.GenerateTestImportTxWithGas(1, 5) // higher fee
	require.NoError(mempool.AddRemoteTx(tx3))
	require.False(mempool.Has(tx1.ID()))
	require.False(mempool.Has(tx2.ID()))
	require.True(mempool.Has(tx3.ID()))
}
