// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/timed"
)

var _ TimedTxHeap = &txHeapByStartTime{}

type TimedTxHeap interface {
	TxHeap

	Timestamp() time.Time
}

type txHeapByStartTime struct {
	txHeap
}

func NewTxHeapByStartTime() TimedTxHeap {
	h := &txHeapByStartTime{}
	h.initialize(h)
	return h
}

func (h *txHeapByStartTime) Less(i, j int) bool {
	iTime := h.txs[i].tx.Unsigned.(timed.Tx).StartTime()
	jTime := h.txs[j].tx.Unsigned.(timed.Tx).StartTime()
	return iTime.Before(jTime)
}

func (h *txHeapByStartTime) Timestamp() time.Time {
	return h.Peek().Unsigned.(timed.Tx).StartTime()
}
