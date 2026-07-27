// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

type Heap interface {
	Add(tx *platform.Tx)
	Get(txID ids.ID) *platform.Tx
	List() []*platform.Tx
	Remove(txID ids.ID) *platform.Tx
	Peek() *platform.Tx
	RemoveTop() *platform.Tx
	Len() int
}

type txHeap struct {
	heap       heap.Map[ids.ID, *platform.Tx]
	currentAge int
}

func (h *txHeap) Add(tx *platform.Tx) {
	txID := tx.ID()
	if h.heap.Contains(txID) {
		return
	}
	h.currentAge++
	h.heap.Push(txID, tx)
}

func (h *txHeap) Get(txID ids.ID) *platform.Tx {
	got, _ := h.heap.Get(txID)
	return got
}

func (h *txHeap) List() []*platform.Tx {
	return heap.MapValues(h.heap)
}

func (h *txHeap) Remove(txID ids.ID) *platform.Tx {
	removed, _ := h.heap.Remove(txID)
	return removed
}

func (h *txHeap) Peek() *platform.Tx {
	_, peeked, _ := h.heap.Peek()
	return peeked
}

func (h *txHeap) RemoveTop() *platform.Tx {
	_, popped, _ := h.heap.Pop()
	return popped
}

func (h *txHeap) Len() int {
	return h.heap.Len()
}
