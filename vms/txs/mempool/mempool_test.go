// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ Tx = (*dummyTx)(nil)

type dummyTx struct {
	size     int
	id       ids.ID
	inputIDs []ids.ID
}

func (tx *dummyTx) Size() int {
	return tx.size
}

func (tx *dummyTx) ID() ids.ID {
	return tx.id
}

func (tx *dummyTx) InputIDs() set.Set[ids.ID] {
	return set.Of(tx.inputIDs...)
}

type noMetrics struct{}

func (*noMetrics) Update(int, int) {}

func newMempool() *mempool[*dummyTx] {
	return New[*dummyTx](&noMetrics{})
}

func TestAdd(t *testing.T) {
	tx0 := newTx(0, 32)

	tests := []struct {
		name       string
		initialTxs []*dummyTx
		tx         *dummyTx
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
			initialTxs: []*dummyTx{tx0},
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
			initialTxs: []*dummyTx{tx0},
			tx:         newTx(0, 32),
			err:        ErrConflictsWithOtherTx,
			dropReason: ErrConflictsWithOtherTx,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			mempool := newMempool()

			for _, tx := range test.initialTxs {
				require.NoError(mempool.Add(tx))
			}

			err := mempool.Add(test.tx)
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

	mempool := newMempool()

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

	mempool := newMempool()

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

	mempool := newMempool()

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

	mempool := newMempool()

	var (
		iteratedTxs []*dummyTx
		maxLen      = 2
	)
	addTxs := func(tx *dummyTx) bool {
		iteratedTxs = append(iteratedTxs, tx)
		return len(iteratedTxs) < maxLen
	}
	mempool.Iterate(addTxs)
	require.Empty(iteratedTxs)

	tx0 := newTx(0, 32)
	require.NoError(mempool.Add(tx0))

	mempool.Iterate(addTxs)
	require.Equal([]*dummyTx{tx0}, iteratedTxs)

	tx1 := newTx(1, 32)
	require.NoError(mempool.Add(tx1))

	iteratedTxs = nil
	mempool.Iterate(addTxs)
	require.Equal([]*dummyTx{tx0, tx1}, iteratedTxs)

	tx2 := newTx(2, 32)
	require.NoError(mempool.Add(tx2))

	iteratedTxs = nil
	mempool.Iterate(addTxs)
	require.Equal([]*dummyTx{tx0, tx1}, iteratedTxs)

	mempool.Remove(tx0, tx2)

	iteratedTxs = nil
	mempool.Iterate(addTxs)
	require.Equal([]*dummyTx{tx1}, iteratedTxs)
}

func TestDropped(t *testing.T) {
	require := require.New(t)

	mempool := newMempool()

	tx := newTx(0, 32)
	txID := tx.ID()
	testErr := errors.New("test")

	mempool.MarkDropped(txID, testErr)

	err := mempool.GetDropReason(txID)
	require.ErrorIs(err, testErr)

	require.NoError(mempool.Add(tx))
	require.NoError(mempool.GetDropReason(txID))

	mempool.MarkDropped(txID, testErr)
	require.NoError(mempool.GetDropReason(txID))
}

func newTxs(num int, size int) []*dummyTx {
	txs := make([]*dummyTx, num)
	for i := range txs {
		txs[i] = newTx(uint64(i), size)
	}
	return txs
}

func newTx(index uint64, size int) *dummyTx {
	return &dummyTx{
		size:     size,
		id:       ids.GenerateTestID(),
		inputIDs: []ids.ID{ids.Empty.Prefix(index)},
	}
}

// shows that valid tx is not added to mempool if this would exceed its maximum
// size
func TestBlockBuilderMaxMempoolSizeHandling(t *testing.T) {
	require := require.New(t)

	mpool := newMempool()

	tx := newTx(0, 32)

	// shortcut to simulated almost filled mempool
	mpool.bytesAvailable = tx.Size() - 1

	err := mpool.Add(tx)
	require.ErrorIs(err, ErrMempoolFull)

	// tx should not be marked as dropped if the mempool is full
	txID := tx.ID()
	mpool.MarkDropped(txID, err)
	require.NoError(mpool.GetDropReason(txID))

	// shortcut to simulated almost filled mempool
	mpool.bytesAvailable = tx.Size()

	err = mpool.Add(tx)
	require.NoError(err, "should have added tx to mempool")
}

func TestWaitForEventCancelled(t *testing.T) {
	m := newMempool()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.WaitForEvent(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWaitForEventWithTx(t *testing.T) {
	require := require.New(t)

	m := newMempool()
	go func() {
		tx := newTx(0, 32)
		require.NoError(m.Add(tx))
	}()

	msg, err := m.WaitForEvent(context.Background())
	require.NoError(err)
	require.Equal(common.PendingTxs, msg)
}
