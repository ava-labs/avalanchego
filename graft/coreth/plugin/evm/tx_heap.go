package evm

import (
	"container/heap"

	"github.com/ava-labs/avalanchego/ids"
)

// txEntry is used to track the [gasPrice] transactions pay to be included in
// the mempool.
type txEntry struct {
	ID       ids.ID
	GasPrice uint64
	Tx       *Tx
	index    int
}

// internalTxHeap is used to track pending atomic transactions by [gasPrice]
type internalTxHeap struct {
	items  []*txEntry
	lookup map[ids.ID]*txEntry
}

func newInternalTxHeap(items int) *internalTxHeap {
	h := &internalTxHeap{
		items:  make([]*txEntry, 0, items),
		lookup: map[ids.ID]*txEntry{},
	}
	return h
}

func (th internalTxHeap) Len() int { return len(th.items) }

func (th internalTxHeap) Less(i, j int) bool {
	return th.items[i].GasPrice > th.items[j].GasPrice
}

func (th internalTxHeap) Swap(i, j int) {
	th.items[i], th.items[j] = th.items[j], th.items[i]
	th.items[i].index = i
	th.items[j].index = j
}

func (th *internalTxHeap) Push(x interface{}) {
	entry := x.(*txEntry)
	if th.Has(entry.ID) {
		return
	}
	th.items = append(th.items, entry)
	th.lookup[entry.ID] = entry
}

func (th *internalTxHeap) Pop() interface{} {
	n := len(th.items)
	item := th.items[n-1]
	th.items[n-1] = nil // avoid memory leak
	th.items = th.items[0 : n-1]
	delete(th.lookup, item.ID)
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
	internalTxHeap *internalTxHeap
}

func newTxHeap(maxSize int) *txHeap {
	th := &txHeap{
		internalTxHeap: newInternalTxHeap(maxSize),
	}
	heap.Init(th.internalTxHeap)
	return th
}

// Assumes there is non-zero items in [txHeap]
func (th *txHeap) Pop() *txEntry {
	return heap.Pop(th.internalTxHeap).(*txEntry)
}

func (th *txHeap) Push(e *txEntry) {
	heap.Push(th.internalTxHeap, e)
}

// Assumes there is non-zero items in [txHeap]
// TODO: need to remove this and replace with min-max heap (@Stephen)
func (th *txHeap) Drop() *txEntry {
	var (
		n = th.Len()
		i = 0
	)

	// Finds the minimum gas price in the heap (the "lowest" priority...where
	// [th.internalTxHeap.Less(i, j)] is true for all i and the item is j)
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 { // j1 < 0 after int overflow
			break
		}
		j := j1 // left child
		if j2 := j1 + 1; j2 < n && !th.internalTxHeap.Less(j2, j1) {
			j = j2 // = 2*i + 2  // right child
		}
		if th.internalTxHeap.Less(j, i) {
			break
		}
		i = j
	}

	// Remove and re-heap the [internalTxHeap]
	return heap.Remove(th.internalTxHeap, i).(*txEntry)
}

func (th *txHeap) Remove(id ids.ID) *txEntry {
	entry, ok := th.internalTxHeap.Get(id)
	if !ok {
		return nil
	}
	return heap.Remove(th.internalTxHeap, entry.index).(*txEntry)
}

func (th *txHeap) Len() int {
	return th.internalTxHeap.Len()
}

func (th *txHeap) Get(id ids.ID) (*txEntry, bool) {
	txEntry, ok := th.internalTxHeap.Get(id)
	if !ok {
		return nil, false
	}
	return txEntry, true
}

func (th *txHeap) Has(id ids.ID) bool {
	_, ok := th.Get(id)
	return ok
}
