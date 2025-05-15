// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestMempoolAddTx(t *testing.T) {
	require := require.New(t)
	ctx := snowtest.Context(t, snowtest.CChainID)
	m, err := NewMempool(ctx, prometheus.NewRegistry(), 5_000, nil)
	require.NoError(err)

	txs := make([]*atomic.GossipAtomicTx, 0)
	for i := 0; i < 3_000; i++ {
		tx := &atomic.GossipAtomicTx{
			Tx: &atomic.Tx{
				UnsignedAtomicTx: &atomic.TestUnsignedTx{
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

// Add should return an error if a tx is already known
func TestMempoolAdd(t *testing.T) {
	require := require.New(t)
	ctx := snowtest.Context(t, snowtest.CChainID)
	m, err := NewMempool(ctx, prometheus.NewRegistry(), 5_000, nil)
	require.NoError(err)

	tx := &atomic.GossipAtomicTx{
		Tx: &atomic.Tx{
			UnsignedAtomicTx: &atomic.TestUnsignedTx{
				IDV: ids.GenerateTestID(),
			},
		},
	}

	require.NoError(m.Add(tx))
	err = m.Add(tx)
	require.ErrorIs(err, errTxAlreadyKnown)
}

func TestAtomicMempoolIterate(t *testing.T) {
	txs := []*atomic.GossipAtomicTx{
		{
			Tx: &atomic.Tx{
				UnsignedAtomicTx: &atomic.TestUnsignedTx{
					IDV: ids.GenerateTestID(),
				},
			},
		},
		{
			Tx: &atomic.Tx{
				UnsignedAtomicTx: &atomic.TestUnsignedTx{
					IDV: ids.GenerateTestID(),
				},
			},
		},
	}

	tests := []struct {
		name        string
		add         []*atomic.GossipAtomicTx
		f           func(tx *atomic.GossipAtomicTx) bool
		expectedTxs []*atomic.GossipAtomicTx
	}{
		{
			name: "func matches nothing",
			add:  txs,
			f: func(*atomic.GossipAtomicTx) bool {
				return false
			},
			expectedTxs: []*atomic.GossipAtomicTx{},
		},
		{
			name: "func matches all",
			add:  txs,
			f: func(*atomic.GossipAtomicTx) bool {
				return true
			},
			expectedTxs: txs,
		},
		{
			name: "func matches subset",
			add:  txs,
			f: func(tx *atomic.GossipAtomicTx) bool {
				return tx.Tx == txs[0].Tx
			},
			expectedTxs: []*atomic.GossipAtomicTx{txs[0]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := snowtest.Context(t, snowtest.CChainID)
			m, err := NewMempool(ctx, prometheus.NewRegistry(), 10, nil)
			require.NoError(err)

			for _, add := range tt.add {
				require.NoError(m.Add(add))
			}

			matches := make([]*atomic.GossipAtomicTx, 0)
			f := func(tx *atomic.GossipAtomicTx) bool {
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
	mempool, err := NewMempool(ctx, prometheus.NewRegistry(), 1, nil)
	require.NoError(err)
	// create candidate tx (we will drop before validation)
	tx := atomic.GenerateTestImportTx()

	require.NoError(mempool.AddRemoteTx(tx))
	require.True(mempool.Has(tx.ID()))
	// promote tx to be issued
	_, ok := mempool.NextTx()
	require.True(ok)
	mempool.IssueCurrentTxs()

	// try to add one more tx
	tx2 := atomic.GenerateTestImportTx()
	require.ErrorIs(mempool.AddRemoteTx(tx2), ErrTooManyAtomicTx)
	require.False(mempool.Has(tx2.ID()))
}

// mempool will drop transaction with the lowest fee
func TestMempoolPriorityDrop(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	mempool, err := NewMempool(ctx, prometheus.NewRegistry(), 1, nil)
	require.NoError(err)

	tx1 := atomic.GenerateTestImportTxWithGas(1, 2) // lower fee
	require.NoError(mempool.AddRemoteTx(tx1))
	require.True(mempool.Has(tx1.ID()))

	tx2 := atomic.GenerateTestImportTxWithGas(1, 2) // lower fee
	require.ErrorIs(mempool.AddRemoteTx(tx2), ErrInsufficientAtomicTxFee)
	require.True(mempool.Has(tx1.ID()))
	require.False(mempool.Has(tx2.ID()))

	tx3 := atomic.GenerateTestImportTxWithGas(1, 5) // higher fee
	require.NoError(mempool.AddRemoteTx(tx3))
	require.False(mempool.Has(tx1.ID()))
	require.False(mempool.Has(tx2.ID()))
	require.True(mempool.Has(tx3.ID()))
}
