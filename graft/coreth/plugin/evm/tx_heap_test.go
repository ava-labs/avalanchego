package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/stretchr/testify/assert"
)

func TestTxHeap(t *testing.T) {
	var (
		id0 = ids.ID{0}
		tx0 = &Tx{
			UnsignedAtomicTx: &UnsignedImportTx{
				NetworkID: 0,
			},
		}

		id1 = ids.ID{1}
		tx1 = &Tx{
			UnsignedAtomicTx: &UnsignedImportTx{
				NetworkID: 1,
			},
		}

		id2 = ids.ID{2}
		tx2 = &Tx{
			UnsignedAtomicTx: &UnsignedImportTx{
				NetworkID: 2,
			},
		}
	)

	assert := assert.New(t)
	h := newTxHeap(3)
	assert.Zero(h.Len())

	t.Run("add/remove single entry", func(t *testing.T) {
		h.Push(&txEntry{
			id:       id0,
			gasPrice: 5,
			tx:       tx0,
		})
		assert.True(h.Has(id0))
		gTx0, gHas0 := h.Get(id0)
		assert.Equal(tx0, gTx0)
		assert.True(gHas0)
		h.Remove(id0)
		assert.False(h.Has(id0))
		h.Push(&txEntry{
			id:       id0,
			gasPrice: 5,
			tx:       tx0,
		})
		assert.True(h.Has(id0))
	})

	t.Run("add other items", func(t *testing.T) {
		h.Push(&txEntry{
			id:       id1,
			gasPrice: 10,
			tx:       tx1,
		})
		assert.True(h.Has(id1))
		gTx1, gHas1 := h.Get(id1)
		assert.Equal(tx1, gTx1)
		assert.True(gHas1)

		h.Push(&txEntry{
			id:       id2,
			gasPrice: 2,
			tx:       tx2,
		})
		assert.True(h.Has(id2))
		gTx2, gHas2 := h.Get(id2)
		assert.Equal(tx2, gTx2)
		assert.True(gHas2)

		assert.Equal(id1, h.Pop().(*txEntry).id)
		assert.Equal(id0, h.Pop().(*txEntry).id)
		assert.Equal(id2, h.Pop().(*txEntry).id)

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

	t.Run("drop", func(t *testing.T) {
		h.Push(&txEntry{
			id:       id0,
			gasPrice: 5,
			tx:       tx0,
		})
		h.Push(&txEntry{
			id:       id1,
			gasPrice: 10,
			tx:       tx1,
		})
		h.Push(&txEntry{
			id:       id2,
			gasPrice: 2,
			tx:       tx2,
		})
		assert.Equal(id2, h.Drop().id)
		assert.True(h.Has(id0))
		assert.True(h.Has(id1))
		assert.False(h.Has(id2))
		assert.Equal(id0, h.Drop().id)
		assert.False(h.Has(id0))
		assert.True(h.Has(id1))
		assert.False(h.Has(id2))
		assert.Equal(id1, h.Drop().id)
		assert.False(h.Has(id0))
		assert.False(h.Has(id1))
		assert.False(h.Has(id2))
	})
}
