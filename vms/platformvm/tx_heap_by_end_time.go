// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import "time"

var _ TimedTxHeap = &txHeapByEndTime{}

type txHeapByEndTime struct {
	txHeap
}

func newTxHeapByEndTime() *txHeapByEndTime {
	h := &txHeapByEndTime{}
	h.initialize(h)
	return h
}

func (h *txHeapByEndTime) Less(i, j int) bool {
	iTime := h.txs[i].tx.UnsignedTx.(TimedTx).EndTime()
	jTime := h.txs[j].tx.UnsignedTx.(TimedTx).EndTime()
	return iTime.Before(jTime)
}

func (h *txHeapByEndTime) Timestamp() time.Time {
	return h.Peek().UnsignedTx.(TimedTx).EndTime()
}
