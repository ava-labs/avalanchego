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
func (th *txHeap) Drop() *txEntry {
	// TODO: find a faster way to do this
	var (
		lowestValue uint64
		lowestIndex = -1
	)
	for i, entry := range th.internalTxHeap.items {
		if entry.GasPrice < lowestValue || lowestIndex == -1 {
			lowestValue = entry.GasPrice
			lowestIndex = i
		}
	}
	return heap.Remove(th.internalTxHeap, lowestIndex).(*txEntry)
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

func (th *txHeap) Get(id ids.ID) (*Tx, bool) {
	txEntry, ok := th.internalTxHeap.Get(id)
	if !ok {
		return nil, false
	}
	return txEntry.Tx, true
}

func (th *txHeap) Has(id ids.ID) bool {
	_, ok := th.Get(id)
	return ok
}
