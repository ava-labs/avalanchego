// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

type Heap interface {
	Add(tx *txs.Tx)
	Get(txID ids.ID) *txs.Tx
	List() []*txs.Tx
	Remove(txID ids.ID) *txs.Tx
	Peek() *txs.Tx
	RemoveTop() *txs.Tx
	Len() int
}

type heapTx struct {
	tx  *txs.Tx
	age int
}

type txHeap struct {
	heap       heap.Map[ids.ID, heapTx]
	currentAge int
}

func (h *txHeap) Add(tx *txs.Tx) {
	txID := tx.ID()
	if h.heap.Contains(txID) {
		return
	}
	htx := heapTx{
		tx:  tx,
		age: h.currentAge,
	}
	h.currentAge++
	h.heap.Push(txID, htx)
}

func (h *txHeap) Get(txID ids.ID) *txs.Tx {
	got, _ := h.heap.Get(txID)
	return got.tx
}

func (h *txHeap) List() []*txs.Tx {
	heapTxs := heap.MapValues(h.heap)
	res := make([]*txs.Tx, 0, len(heapTxs))
	for _, tx := range heapTxs {
		res = append(res, tx.tx)
	}
	return res
}

func (h *txHeap) Remove(txID ids.ID) *txs.Tx {
	removed, _ := h.heap.Remove(txID)
	return removed.tx
}

func (h *txHeap) Peek() *txs.Tx {
	_, peeked, _ := h.heap.Peek()
	return peeked.tx
}

func (h *txHeap) RemoveTop() *txs.Tx {
	_, popped, _ := h.heap.Pop()
	return popped.tx
}

func (h *txHeap) Len() int {
	return h.heap.Len()
}
