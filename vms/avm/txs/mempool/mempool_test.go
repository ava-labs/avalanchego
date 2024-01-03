// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

func TestAdd(t *testing.T) {
	tx0 := newTx(0, 32)

	tests := []struct {
		name       string
		initialTxs []*txs.Tx
		tx         *txs.Tx
		err        error
		dropReason error
	}{
		{
			name:       "successfully add tx",
			initialTxs: nil,
			tx:         tx0,
			err:        nil,
			dropReason: nil,
		},
		{
			name:       "attempt adding duplicate tx",
			initialTxs: []*txs.Tx{tx0},
			tx:         tx0,
			err:        ErrDuplicateTx,
			dropReason: nil,
		},
		{
			name:       "attempt adding too large tx",
			initialTxs: nil,
			tx:         newTx(0, MaxTxSize+1),
			err:        ErrTxTooLarge,
			dropReason: ErrTxTooLarge,
		},
		{
			name:       "attempt adding tx when full",
			initialTxs: newTxs(maxMempoolSize/MaxTxSize, MaxTxSize),
			tx:         newTx(maxMempoolSize/MaxTxSize, MaxTxSize),
			err:        ErrMempoolFull,
			dropReason: nil,
		},
		{
			name:       "attempt adding conflicting tx",
			initialTxs: []*txs.Tx{tx0},
			tx:         newTx(0, 32),
			err:        ErrConflictsWithOtherTx,
			dropReason: ErrConflictsWithOtherTx,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			mempool, err := New(
				"mempool",
				prometheus.NewRegistry(),
				nil,
			)
			require.NoError(err)

			for _, tx := range test.initialTxs {
				require.NoError(mempool.Add(tx))
			}

			err = mempool.Add(test.tx)
			require.ErrorIs(err, test.err)

			txID := test.tx.ID()

			if err != nil {
				mempool.MarkDropped(txID, err)
			}

			err = mempool.GetDropReason(txID)
			require.ErrorIs(err, test.dropReason)
		})
	}
}

func TestGet(t *testing.T) {
	require := require.New(t)

	mempool, err := New(
		"mempool",
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	tx := newTx(0, 32)
	txID := tx.ID()

	_, exists := mempool.Get(txID)
	require.False(exists)

	require.NoError(mempool.Add(tx))

	returned, exists := mempool.Get(txID)
	require.True(exists)
	require.Equal(tx, returned)

	mempool.Remove(tx)

	_, exists = mempool.Get(txID)
	require.False(exists)
}

func TestPeek(t *testing.T) {
	require := require.New(t)

	mempool, err := New(
		"mempool",
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	_, exists := mempool.Peek()
	require.False(exists)

	tx0 := newTx(0, 32)
	tx1 := newTx(1, 32)

	require.NoError(mempool.Add(tx0))
	require.NoError(mempool.Add(tx1))

	tx, exists := mempool.Peek()
	require.True(exists)
	require.Equal(tx, tx0)

	mempool.Remove(tx0)

	tx, exists = mempool.Peek()
	require.True(exists)
	require.Equal(tx, tx1)

	mempool.Remove(tx0)

	tx, exists = mempool.Peek()
	require.True(exists)
	require.Equal(tx, tx1)

	mempool.Remove(tx1)

	_, exists = mempool.Peek()
	require.False(exists)
}

func TestRemoveConflict(t *testing.T) {
	require := require.New(t)

	mempool, err := New(
		"mempool",
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	tx := newTx(0, 32)
	txConflict := newTx(0, 32)

	require.NoError(mempool.Add(tx))

	returnedTx, exists := mempool.Peek()
	require.True(exists)
	require.Equal(returnedTx, tx)

	mempool.Remove(txConflict)

	_, exists = mempool.Peek()
	require.False(exists)
}

func TestIterate(t *testing.T) {
	require := require.New(t)

	mempool, err := New(
		"mempool",
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	var (
		iteratedTxs []*txs.Tx
		maxLen      = 2
	)
	addTxs := func(tx *txs.Tx) bool {
		iteratedTxs = append(iteratedTxs, tx)
		return len(iteratedTxs) < maxLen
	}
	mempool.Iterate(addTxs)
	require.Empty(iteratedTxs)

	tx0 := newTx(0, 32)
	require.NoError(mempool.Add(tx0))

	mempool.Iterate(addTxs)
	require.Equal([]*txs.Tx{tx0}, iteratedTxs)

	tx1 := newTx(1, 32)
	require.NoError(mempool.Add(tx1))

	iteratedTxs = nil
	mempool.Iterate(addTxs)
	require.Equal([]*txs.Tx{tx0, tx1}, iteratedTxs)

	tx2 := newTx(2, 32)
	require.NoError(mempool.Add(tx2))

	iteratedTxs = nil
	mempool.Iterate(addTxs)
	require.Equal([]*txs.Tx{tx0, tx1}, iteratedTxs)

	mempool.Remove(tx0, tx2)

	iteratedTxs = nil
	mempool.Iterate(addTxs)
	require.Equal([]*txs.Tx{tx1}, iteratedTxs)
}

func TestRequestBuildBlock(t *testing.T) {
	require := require.New(t)

	toEngine := make(chan common.Message, 1)
	mempool, err := New(
		"mempool",
		prometheus.NewRegistry(),
		toEngine,
	)
	require.NoError(err)

	mempool.RequestBuildBlock()
	select {
	case <-toEngine:
		require.FailNow("should not have sent message to engine")
	default:
	}

	tx := newTx(0, 32)
	require.NoError(mempool.Add(tx))

	mempool.RequestBuildBlock()
	mempool.RequestBuildBlock() // Must not deadlock
	select {
	case <-toEngine:
	default:
		require.FailNow("should have sent message to engine")
	}
	select {
	case <-toEngine:
		require.FailNow("should have only sent one message to engine")
	default:
	}
}

func TestDropped(t *testing.T) {
	require := require.New(t)

	mempool, err := New(
		"mempool",
		prometheus.NewRegistry(),
		nil,
	)
	require.NoError(err)

	tx := newTx(0, 32)
	txID := tx.ID()
	testErr := errors.New("test")

	mempool.MarkDropped(txID, testErr)

	err = mempool.GetDropReason(txID)
	require.ErrorIs(err, testErr)

	require.NoError(mempool.Add(tx))
	require.NoError(mempool.GetDropReason(txID))

	mempool.MarkDropped(txID, testErr)
	require.NoError(mempool.GetDropReason(txID))
}

func newTxs(num int, size int) []*txs.Tx {
	txs := make([]*txs.Tx, num)
	for i := range txs {
		txs[i] = newTx(uint32(i), size)
	}
	return txs
}

func newTx(index uint32, size int) *txs.Tx {
	tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.ID{'t', 'x', 'I', 'D'},
				OutputIndex: index,
			},
		}},
	}}}
	tx.SetBytes(utils.RandomBytes(size), utils.RandomBytes(size))
	return tx
}
