// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"container/heap"

	"github.com/ava-labs/avalanchego/ids"
)

// txEntry is used to track the [gasPrice] transactions pay to be included in
// the mempool.
type txEntry struct {
	id       ids.ID
	gasPrice uint64
	tx       *Tx
	index    int
}

// internalTxHeap is used to track pending atomic transactions by [gasPrice]
type internalTxHeap struct {
	isMinHeap bool
	items     []*txEntry
	lookup    map[ids.ID]*txEntry
}

func newInternalTxHeap(items int, isMinHeap bool) *internalTxHeap {
	return &internalTxHeap{
		isMinHeap: isMinHeap,
		items:     make([]*txEntry, 0, items),
		lookup:    map[ids.ID]*txEntry{},
	}
}

func (th internalTxHeap) Len() int { return len(th.items) }

func (th internalTxHeap) Less(i, j int) bool {
	if th.isMinHeap {
		return th.items[i].gasPrice < th.items[j].gasPrice
	}
	return th.items[i].gasPrice > th.items[j].gasPrice
}

func (th internalTxHeap) Swap(i, j int) {
	th.items[i], th.items[j] = th.items[j], th.items[i]
	th.items[i].index = i
	th.items[j].index = j
}

func (th *internalTxHeap) Push(x interface{}) {
	entry := x.(*txEntry)
	if th.Has(entry.id) {
		return
	}
	th.items = append(th.items, entry)
	th.lookup[entry.id] = entry
}

func (th *internalTxHeap) Pop() interface{} {
	n := len(th.items)
	item := th.items[n-1]
	th.items[n-1] = nil // avoid memory leak
	th.items = th.items[0 : n-1]
	delete(th.lookup, item.id)
	return item
}

func (th *internalTxHeap) Get(id ids.ID) (*txEntry, bool) {
	entry, ok := th.lookup[id]
	if !ok {
		return nil, false
	}
	return entry, true
}

func (th *internalTxHeap) Has(id ids.ID) bool {
	_, has := th.Get(id)
	return has
}

type txHeap struct {
	maxHeap *internalTxHeap
	minHeap *internalTxHeap
}

func newTxHeap(maxSize int) *txHeap {
	return &txHeap{
		maxHeap: newInternalTxHeap(maxSize, false),
		minHeap: newInternalTxHeap(maxSize, true),
	}
}

func (th *txHeap) Push(tx *Tx, gasPrice uint64) {
	txID := tx.ID()
	oldLen := th.Len()
	heap.Push(th.maxHeap, &txEntry{
		id:       txID,
		gasPrice: gasPrice,
		tx:       tx,
		index:    oldLen,
	})
	heap.Push(th.minHeap, &txEntry{
		id:       txID,
		gasPrice: gasPrice,
		tx:       tx,
		index:    oldLen,
	})
}

// Assumes there is non-zero items in [txHeap]
func (th *txHeap) PeekMax() (*Tx, uint64) {
	txEntry := th.maxHeap.items[0]
	return txEntry.tx, txEntry.gasPrice
}

// Assumes there is non-zero items in [txHeap]
func (th *txHeap) PeekMin() (*Tx, uint64) {
	txEntry := th.minHeap.items[0]
	return txEntry.tx, txEntry.gasPrice
}

// Assumes there is non-zero items in [txHeap]
func (th *txHeap) PopMax() *Tx {
	return th.Remove(th.maxHeap.items[0].id)
}

// Assumes there is non-zero items in [txHeap]
func (th *txHeap) PopMin() *Tx {
	return th.Remove(th.minHeap.items[0].id)
}

func (th *txHeap) Remove(id ids.ID) *Tx {
	maxEntry, ok := th.maxHeap.Get(id)
	if !ok {
		return nil
	}
	heap.Remove(th.maxHeap, maxEntry.index)

	minEntry, ok := th.minHeap.Get(id)
	if !ok {
		// This should never happen, as that would mean the heaps are out of
		// sync.
		return nil
	}
	return heap.Remove(th.minHeap, minEntry.index).(*txEntry).tx
}

func (th *txHeap) Len() int {
	return th.maxHeap.Len()
}

func (th *txHeap) Get(id ids.ID) (*Tx, bool) {
	txEntry, ok := th.maxHeap.Get(id)
	if !ok {
		return nil, false
	}
	return txEntry.tx, true
}

func (th *txHeap) Has(id ids.ID) bool {
	return th.maxHeap.Has(id)
}
