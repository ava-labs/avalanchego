// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"container/heap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Heap = (*txHeap)(nil)

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
	tx    *txs.Tx
	index int
	age   int
}

type txHeap struct {
	self heap.Interface

	txIDToIndex map[ids.ID]int
	txs         []*heapTx
	currentAge  int
}

func (h *txHeap) initialize(self heap.Interface) {
	h.self = self
	h.txIDToIndex = make(map[ids.ID]int)
}

func (h *txHeap) Add(tx *txs.Tx) {
	heap.Push(h.self, tx)
}

func (h *txHeap) Get(txID ids.ID) *txs.Tx {
	index, exists := h.txIDToIndex[txID]
	if !exists {
		return nil
	}
	return h.txs[index].tx
}

func (h *txHeap) List() []*txs.Tx {
	res := make([]*txs.Tx, 0, len(h.txs))
	for _, tx := range h.txs {
		res = append(res, tx.tx)
	}
	return res
}

func (h *txHeap) Remove(txID ids.ID) *txs.Tx {
	index, exists := h.txIDToIndex[txID]
	if !exists {
		return nil
	}
	return heap.Remove(h.self, index).(*txs.Tx)
}

func (h *txHeap) Peek() *txs.Tx {
	return h.txs[0].tx
}

func (h *txHeap) RemoveTop() *txs.Tx {
	return heap.Pop(h.self).(*txs.Tx)
}

func (h *txHeap) Len() int {
	return len(h.txs)
}

func (h *txHeap) Swap(i, j int) {
	// The follow "i"s and "j"s are intentionally swapped to perform the actual
	// swap
	iTx := h.txs[j]
	jTx := h.txs[i]

	iTx.index = i
	jTx.index = j
	h.txs[i] = iTx
	h.txs[j] = jTx

	iTxID := iTx.tx.ID()
	jTxID := jTx.tx.ID()
	h.txIDToIndex[iTxID] = i
	h.txIDToIndex[jTxID] = j
}

func (h *txHeap) Push(x interface{}) {
	tx := x.(*txs.Tx)

	txID := tx.ID()
	_, exists := h.txIDToIndex[txID]
	if exists {
		return
	}
	htx := &heapTx{
		tx:    tx,
		index: len(h.txs),
		age:   h.currentAge,
	}
	h.currentAge++
	h.txIDToIndex[txID] = htx.index
	h.txs = append(h.txs, htx)
}

func (h *txHeap) Pop() interface{} {
	newLen := len(h.txs) - 1
	htx := h.txs[newLen]
	h.txs[newLen] = nil
	h.txs = h.txs[:newLen]

	tx := htx.tx
	txID := tx.ID()
	delete(h.txIDToIndex, txID)
	return tx
}
