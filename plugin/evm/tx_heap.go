package evm

import (
	"container/heap"
	"sort"

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
	sort.Interface
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
	internalTxHeap *internalTxHeap
}

func newTxHeap(maxSize int) *txHeap {
	th := &txHeap{
		internalTxHeap: newInternalTxHeap(maxSize),
	}
	heap.Init(th.internalTxHeap)
	return th
}

func (th *txHeap) Pop() *txEntry {
	return heap.Pop(th.internalTxHeap).(*txEntry)
}

func (th *txHeap) Push(e *txEntry) {
	heap.Push(th.internalTxHeap, e)
}

func (th *txHeap) Drop() *txEntry {
	n := th.internalTxHeap.Len()
	return heap.Remove(th.internalTxHeap, n-1).(*txEntry)
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
	return txEntry.tx, true
}

func (th *txHeap) Has(id ids.ID) bool {
	_, ok := th.Get(id)
	return ok
}
