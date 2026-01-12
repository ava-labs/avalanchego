// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

type txHeap struct {
	heap       heap.Map[ids.ID, *txs.Tx]
	currentAge int
}

func (h *txHeap) Add(tx *txs.Tx) {
	txID := tx.ID()
	if h.heap.Contains(txID) {
		return
	}
	h.currentAge++
	h.heap.Push(txID, tx)
}

func (h *txHeap) Get(txID ids.ID) *txs.Tx {
	got, _ := h.heap.Get(txID)
	return got
}

func (h *txHeap) List() []*txs.Tx {
	return heap.MapValues(h.heap)
}

func (h *txHeap) Remove(txID ids.ID) *txs.Tx {
	removed, _ := h.heap.Remove(txID)
	return removed
}

func (h *txHeap) Peek() *txs.Tx {
	_, peeked, _ := h.heap.Peek()
	return peeked
}

func (h *txHeap) RemoveTop() *txs.Tx {
	_, popped, _ := h.heap.Pop()
	return popped
}

func (h *txHeap) Len() int {
	return h.heap.Len()
}
