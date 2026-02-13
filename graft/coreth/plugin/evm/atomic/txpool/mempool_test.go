// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"math"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/atomictest"
	"github.com/ava-labs/avalanchego/graft/evm/config"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/bloom"
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
		tx := atomictest.GenerateTestImportTxWithGas(1, 1)
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

	tx := atomictest.GenerateTestImportTxWithGas(1, 1)
	require.NoError(m.Add(tx))
	err = m.Add(tx)
	require.ErrorIs(err, ErrAlreadyKnown)
}

// Add should return an error if a tx doesn't consume any gas
func TestMempoolAddNoGas(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	m, err := NewMempool(
		NewTxs(ctx, 5_000),
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	tx := atomictest.GenerateTestImportTxWithGas(0, 1)
	err = m.Add(tx)
	require.ErrorIs(err, atomic.ErrNoGasUsed)
}

// Add should return an error if a tx doesn't consume any gas
func TestMempoolAddBloomReset(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	m, err := NewMempool(
		NewTxs(ctx, 2),
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	maxFeeTx := atomictest.GenerateTestImportTxWithGas(1, math.MaxUint64)
	require.NoError(m.Add(maxFeeTx))

	// Mark maxFeeTx as Current
	tx, ok := m.NextTx()
	require.True(ok)
	require.Equal(maxFeeTx, tx)

	numHashes, numEntries := bloom.OptimalParameters(
		config.TxGossipBloomMinTargetElements,
		config.TxGossipBloomTargetFalsePositiveRate,
	)
	txsToAdd := bloom.EstimateCount(
		numHashes,
		numEntries,
		config.TxGossipBloomResetFalsePositiveRate,
	)
	for fee := range txsToAdd {
		// Keep increasing the fee to evict older transactions
		tx := atomictest.GenerateTestImportTxWithGas(1, uint64(fee))
		require.NoError(m.Add(tx))
	}

	// Mark maxFeeTx as Pending
	m.CancelCurrentTxs()

	m.Iterate(func(tx *atomic.Tx) bool {
		require.True(m.bloom.Has(tx))
		return true // Iterate over the whole mempool
	})
}

func TestAtomicMempoolIterate(t *testing.T) {
	txs := []*atomic.Tx{
		atomictest.GenerateTestImportTxWithGas(1, 1),
		atomictest.GenerateTestImportTxWithGas(1, 1),
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

	lowFeeTx := atomictest.GenerateTestImportTxWithGas(1, 1)
	highFeeTx := atomictest.GenerateTestImportTxWithGas(1, 2)

	require.NoError(mempool.Add(lowFeeTx))

	// Mark the lowFeeTx as Current
	tx, ok := mempool.NextTx()
	require.True(ok)
	require.Equal(lowFeeTx, tx)

	// Because Current transactions can not be evicted, the mempool should
	// report full.
	err = mempool.Add(highFeeTx)
	require.ErrorIs(err, ErrMempoolFull)

	// Mark the lowFeeTx as Issued
	mempool.IssueCurrentTxs()

	// Issued transactions also can not be evicted.
	err = mempool.Add(highFeeTx)
	require.ErrorIs(err, ErrMempoolFull)

	// If we make space, the highFeeTx should be allowed.
	mempool.RemoveTx(lowFeeTx)
	require.NoError(mempool.Add(highFeeTx))
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

// PendingLen should only return the number of Pending transactions, not
// Current, Issued, or Discarded.
func TestMempoolPendingLen(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	mempool, err := NewMempool(
		NewTxs(ctx, 2),
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	tx1 := atomictest.GenerateTestImportTxWithGas(1, 1)
	tx2 := atomictest.GenerateTestImportTxWithGas(1, 2)

	require.NoError(mempool.AddRemoteTx(tx1))
	require.NoError(mempool.AddRemoteTx(tx2))
	require.Equal(2, mempool.PendingLen())

	nextTx, ok := mempool.NextTx()
	require.True(ok)
	require.Equal(tx2, nextTx)
	require.Equal(1, mempool.PendingLen()) // Shouldn't include Current txs

	mempool.IssueCurrentTxs()
	require.Equal(1, mempool.PendingLen()) // Shouldn't include Issued txs

	nextTx, ok = mempool.NextTx()
	require.True(ok)
	require.Equal(tx1, nextTx)
	require.Zero(mempool.PendingLen()) // Still shouldn't include Current txs

	mempool.DiscardCurrentTxs()
	require.Zero(mempool.PendingLen()) // Shouldn't include Discarded txs
}
