// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
)

func TestTxHeap(t *testing.T) {
	var (
		tx0 = &atomic.Tx{
			UnsignedAtomicTx: &atomic.UnsignedImportTx{
				NetworkID: 0,
			},
		}
		tx0Bytes = []byte{0}

		tx1 = &atomic.Tx{
			UnsignedAtomicTx: &atomic.UnsignedImportTx{
				NetworkID: 1,
			},
		}
		tx1Bytes = []byte{1}

		tx2 = &atomic.Tx{
			UnsignedAtomicTx: &atomic.UnsignedImportTx{
				NetworkID: 2,
			},
		}
		tx2Bytes = []byte{2}
	)
	tx0.Initialize(tx0Bytes, tx0Bytes)
	tx1.Initialize(tx1Bytes, tx1Bytes)
	tx2.Initialize(tx2Bytes, tx2Bytes)

	id0 := tx0.ID()
	id1 := tx1.ID()
	id2 := tx2.ID()

	t.Run("add/remove single entry", func(t *testing.T) {
		h := newTxHeap(3)
		require.Zero(t, h.Len())
		h.Push(tx0, *uint256.NewInt(5))
		require.True(t, h.Has(id0))
		gTx0, gHas0 := h.Get(id0)
		require.Equal(t, tx0, gTx0)
		require.True(t, gHas0)
		h.Remove(id0)
		require.False(t, h.Has(id0))
		require.Zero(t, h.Len())
		h.Push(tx0, *uint256.NewInt(5))
		require.True(t, h.Has(id0))
		require.Equal(t, 1, h.Len())
	})

	t.Run("add other items", func(t *testing.T) {
		h := newTxHeap(3)
		require.Zero(t, h.Len())
		h.Push(tx1, *uint256.NewInt(10))
		require.True(t, h.Has(id1))
		gTx1, gHas1 := h.Get(id1)
		require.Equal(t, tx1, gTx1)
		require.True(t, gHas1)

		h.Push(tx2, *uint256.NewInt(2))
		require.True(t, h.Has(id2))
		gTx2, gHas2 := h.Get(id2)
		require.Equal(t, tx2, gTx2)
		require.True(t, gHas2)

		require.Equal(t, id1, h.PopMax().ID())
		require.Equal(t, id2, h.PopMax().ID())

		require.False(t, h.Has(id0))
		gTx0, gHas0 := h.Get(id0)
		require.Nil(t, gTx0)
		require.False(t, gHas0)

		require.False(t, h.Has(id1))
		gTx1, gHas1 = h.Get(id1)
		require.Nil(t, gTx1)
		require.False(t, gHas1)

		require.False(t, h.Has(id2))
		gTx2, gHas2 = h.Get(id2)
		require.Nil(t, gTx2)
		require.False(t, gHas2)
	})

	verifyRemovalOrder := func(t *testing.T, h *txHeap) {
		t.Helper()

		require.Equal(t, id2, h.PopMin().ID())
		require.True(t, h.Has(id0))
		require.True(t, h.Has(id1))
		require.False(t, h.Has(id2))
		require.Equal(t, id0, h.PopMin().ID())
		require.False(t, h.Has(id0))
		require.True(t, h.Has(id1))
		require.False(t, h.Has(id2))
		require.Equal(t, id1, h.PopMin().ID())
		require.False(t, h.Has(id0))
		require.False(t, h.Has(id1))
		require.False(t, h.Has(id2))
	}

	t.Run("drop", func(t *testing.T) {
		h := newTxHeap(3)
		require.Zero(t, h.Len())

		h.Push(tx0, *uint256.NewInt(5))
		h.Push(tx1, *uint256.NewInt(10))
		h.Push(tx2, *uint256.NewInt(2))
		verifyRemovalOrder(t, h)
	})
	t.Run("drop (alt order)", func(t *testing.T) {
		h := newTxHeap(3)
		require.Zero(t, h.Len())

		h.Push(tx0, *uint256.NewInt(5))
		h.Push(tx2, *uint256.NewInt(2))
		h.Push(tx1, *uint256.NewInt(10))
		verifyRemovalOrder(t, h)
	})
	t.Run("drop (alt order 2)", func(t *testing.T) {
		h := newTxHeap(3)
		require.Zero(t, h.Len())

		h.Push(tx2, *uint256.NewInt(2))
		h.Push(tx0, *uint256.NewInt(5))
		h.Push(tx1, *uint256.NewInt(10))
		verifyRemovalOrder(t, h)
	})
}
