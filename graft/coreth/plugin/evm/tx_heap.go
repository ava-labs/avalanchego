package evm

import (
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
	lookup map[ids.ID]int
}

func newTxHeap(items int) *txHeap {
	return &txHeap{
		items:  make([]*txEntry, 0, items),
		lookup: map[ids.ID]int{},
	}
}

func (th *txHeap) Len() int { return len(th.items) }

func (th *txHeap) Less(i, j int) bool {
	return th.items[i].gasPrice < th.items[j].gasPrice
}

func (th *txHeap) Swap(i, j int) {
	th.items[i], th.items[j] = th.items[j], th.items[i]
	th.lookup[th.items[i].id] = i
	th.lookup[th.items[j].id] = j
}

func (th *txHeap) Push(x interface{}) {
	n := len(th.items)
	rec := x.(*txEntry)
	th.items = append(th.items, rec)
	th.lookup[rec.id] = n
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

func (th *txHeap) Remove(id ids.ID) {
	index, ok := th.lookup[id]
	if !ok {
		return
	}
	delete(th.lookup, id)
	th.items = append(th.items[0:index], th.items[index+1:]...)
}

func (th *txHeap) Get(id ids.ID) (*Tx, bool) {
	index, ok := th.lookup[id]
	if !ok {
		return nil, false
	}
	return th.items[index].tx, true
}

func (th *txHeap) Has(id ids.ID) bool {
	_, has := th.Get(id)
	return has
}
