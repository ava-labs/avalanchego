// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"container/heap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
)

var _ Heap = &txHeap{}

type Heap interface {
	Add(tx *signed.Tx)
	Get(txID ids.ID) *signed.Tx
	List() []*signed.Tx
	Remove(txID ids.ID) *signed.Tx
	Peek() *signed.Tx
	RemoveTop() *signed.Tx
	Len() int
}

type heapTx struct {
	tx    *signed.Tx
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

func (h *txHeap) Add(tx *signed.Tx) { heap.Push(h.self, tx) }

func (h *txHeap) Get(txID ids.ID) *signed.Tx {
	index, exists := h.txIDToIndex[txID]
	if !exists {
		return nil
	}
	return h.txs[index].tx
}

func (h *txHeap) List() []*signed.Tx {
	res := make([]*signed.Tx, 0, len(h.txs))
	for _, ht := range h.txs {
		res = append(res, ht.tx)
	}
	return res
}

func (h *txHeap) Remove(txID ids.ID) *signed.Tx {
	index, exists := h.txIDToIndex[txID]
	if !exists {
		return nil
	}
	return heap.Remove(h.self, index).(*signed.Tx)
}

func (h *txHeap) Peek() *signed.Tx { return h.txs[0].tx }

func (h *txHeap) RemoveTop() *signed.Tx { return heap.Pop(h.self).(*signed.Tx) }

func (h *txHeap) Len() int { return len(h.txs) }

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
	tx := x.(*signed.Tx)

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
	h.txs = h.txs[:newLen]

	tx := htx.tx
	txID := tx.ID()
	delete(h.txIDToIndex, txID)
	return tx
}
