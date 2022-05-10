// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/timed"
)

var _ TimedTxHeap = &txHeapByEndTime{}

type txHeapByEndTime struct {
	txHeap
}

func NewTxHeapByEndTime() TimedTxHeap {
	h := &txHeapByEndTime{}
	h.initialize(h)
	return h
}

func (h *txHeapByEndTime) Less(i, j int) bool {
	iTime := h.txs[i].tx.Unsigned.(timed.Tx).EndTime()
	jTime := h.txs[j].tx.Unsigned.(timed.Tx).EndTime()
	return iTime.Before(jTime)
}

func (h *txHeapByEndTime) Timestamp() time.Time {
	return h.Peek().Unsigned.(timed.Tx).EndTime()
}
