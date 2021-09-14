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
}

// txHeap is used to track pending atomic transactions by [gasPrice]
type txHeap struct {
	items  []*txEntry
	lookup map[ids.ID]*txEntry
}

func newTxHeap(items int) *txHeap {
	h := &txHeap{
		items:  make([]*txEntry, 0, items),
		lookup: map[ids.ID]*txEntry{},
	}
	heap.Init(h)
	return h
}

func (th *txHeap) Len() int { return len(th.items) }

func (th *txHeap) Less(i, j int) bool {
	return th.items[i].gasPrice < th.items[j].gasPrice
}

func (th *txHeap) Swap(i, j int) {
	th.items[i], th.items[j] = th.items[j], th.items[i]
}

func (th *txHeap) Push(x interface{}) {
	entry := x.(*txEntry)
	if th.Has(entry.id) {
		return
	}
	th.items = append(th.items, entry)
	th.lookup[entry.id] = entry
}

func (th *txHeap) Pop() interface{} {
	n := len(th.items)
	item := th.items[n-1]
	th.items[n-1] = nil // avoid memory leak
	th.items = th.items[0 : n-1]
	delete(th.lookup, item.id)
	return item
}

func (th *txHeap) Drop() *txEntry {
	item := th.items[0]
	th.items[0] = nil // avoid memory leak
	th.items = th.items[1:]
	delete(th.lookup, item.id)
	return item
}

// Remove is an expensive operation (worst case O(n)) and should be done
// sparingly.
func (th *txHeap) Remove(id ids.ID) {
	_, ok := th.lookup[id]
	if !ok {
		return
	}
	delete(th.lookup, id)

	for i, item := range th.items {
		if item.id == id {
			th.items[i] = nil
			th.items = append(th.items[0:i], th.items[i+1:]...)
			return
		}
	}
}

func (th *txHeap) Get(id ids.ID) (*Tx, bool) {
	entry, ok := th.lookup[id]
	if !ok {
		return nil, false
	}
	return entry.tx, true
}

func (th *txHeap) Has(id ids.ID) bool {
	_, has := th.Get(id)
	return has
}
