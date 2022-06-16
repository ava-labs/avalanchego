// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ TimedHeap = &byEndTime{}

type byEndTime struct {
	txHeap
}

func NewByEndTime() TimedHeap {
	h := &byEndTime{}
	h.initialize(h)
	return h
}

func (h *byEndTime) Less(i, j int) bool {
	iTime := h.txs[i].tx.Unsigned.(txs.StakerTx).EndTime()
	jTime := h.txs[j].tx.Unsigned.(txs.StakerTx).EndTime()
	return iTime.Before(jTime)
}

func (h *byEndTime) Timestamp() time.Time {
	return h.Peek().Unsigned.(txs.StakerTx).EndTime()
}
