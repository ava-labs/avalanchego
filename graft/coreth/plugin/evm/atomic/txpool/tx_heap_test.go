// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/coreth/plugin/evm/atomic"
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
		assert.Zero(t, h.Len())

		assert := assert.New(t)
		h.Push(tx0, *uint256.NewInt(5))
		assert.True(h.Has(id0))
		gTx0, gHas0 := h.Get(id0)
		assert.Equal(tx0, gTx0)
		assert.True(gHas0)
		h.Remove(id0)
		assert.False(h.Has(id0))
		assert.Zero(h.Len())
		h.Push(tx0, *uint256.NewInt(5))
		assert.True(h.Has(id0))
		assert.Equal(1, h.Len())
	})

	t.Run("add other items", func(t *testing.T) {
		h := newTxHeap(3)
		assert.Zero(t, h.Len())

		assert := assert.New(t)
		h.Push(tx1, *uint256.NewInt(10))
		assert.True(h.Has(id1))
		gTx1, gHas1 := h.Get(id1)
		assert.Equal(tx1, gTx1)
		assert.True(gHas1)

		h.Push(tx2, *uint256.NewInt(2))
		assert.True(h.Has(id2))
		gTx2, gHas2 := h.Get(id2)
		assert.Equal(tx2, gTx2)
		assert.True(gHas2)

		assert.Equal(id1, h.PopMax().ID())
		assert.Equal(id2, h.PopMax().ID())

		assert.False(h.Has(id0))
		gTx0, gHas0 := h.Get(id0)
		assert.Nil(gTx0)
		assert.False(gHas0)

		assert.False(h.Has(id1))
		gTx1, gHas1 = h.Get(id1)
		assert.Nil(gTx1)
		assert.False(gHas1)

		assert.False(h.Has(id2))
		gTx2, gHas2 = h.Get(id2)
		assert.Nil(gTx2)
		assert.False(gHas2)
	})

	verifyRemovalOrder := func(t *testing.T, h *txHeap) {
		t.Helper()

		assert := assert.New(t)
		assert.Equal(id2, h.PopMin().ID())
		assert.True(h.Has(id0))
		assert.True(h.Has(id1))
		assert.False(h.Has(id2))
		assert.Equal(id0, h.PopMin().ID())
		assert.False(h.Has(id0))
		assert.True(h.Has(id1))
		assert.False(h.Has(id2))
		assert.Equal(id1, h.PopMin().ID())
		assert.False(h.Has(id0))
		assert.False(h.Has(id1))
		assert.False(h.Has(id2))
	}

	t.Run("drop", func(t *testing.T) {
		h := newTxHeap(3)
		assert.Zero(t, h.Len())

		h.Push(tx0, *uint256.NewInt(5))
		h.Push(tx1, *uint256.NewInt(10))
		h.Push(tx2, *uint256.NewInt(2))
		verifyRemovalOrder(t, h)
	})
	t.Run("drop (alt order)", func(t *testing.T) {
		h := newTxHeap(3)
		assert.Zero(t, h.Len())

		h.Push(tx0, *uint256.NewInt(5))
		h.Push(tx2, *uint256.NewInt(2))
		h.Push(tx1, *uint256.NewInt(10))
		verifyRemovalOrder(t, h)
	})
	t.Run("drop (alt order 2)", func(t *testing.T) {
		h := newTxHeap(3)
		assert.Zero(t, h.Len())

		h.Push(tx2, *uint256.NewInt(2))
		h.Push(tx0, *uint256.NewInt(5))
		h.Push(tx1, *uint256.NewInt(10))
		verifyRemovalOrder(t, h)
	})
}
