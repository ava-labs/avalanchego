// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

var _ Heap = (*byAge)(nil)

type byAge struct {
	txHeap
}

func NewByAge() Heap {
	h := &byAge{}
	h.initialize(h)
	return h
}

func (h *byAge) Less(i, j int) bool {
	return h.txs[i].age < h.txs[j].age
}
