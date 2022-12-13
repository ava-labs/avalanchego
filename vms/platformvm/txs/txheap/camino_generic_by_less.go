// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

type WithDuration interface {
	Duration() uint32
}

var _ Heap = (*byDuration)(nil)

type byDuration struct {
	txHeap
}

func NewByDuration() Heap {
	h := &byDuration{}
	h.initialize(h)
	return h
}

func (h *byDuration) Less(i, j int) bool {
	iDuration := h.txs[i].tx.Unsigned.(WithDuration).Duration()
	jDuration := h.txs[j].tx.Unsigned.(WithDuration).Duration()
	return iDuration < jDuration
}
