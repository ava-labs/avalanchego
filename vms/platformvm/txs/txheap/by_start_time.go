// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ TimedHeap = (*byStartTime)(nil)

type TimedHeap interface {
	Heap

	Timestamp() time.Time
}

type byStartTime struct {
	txHeap
}

func NewByStartTime() TimedHeap {
	return &byStartTime{
		txHeap: txHeap{
			heap: heap.NewMap[ids.ID, heapTx](func(a, b heapTx) bool {
				aTime := a.tx.Unsigned.(txs.Staker).StartTime()
				bTime := b.tx.Unsigned.(txs.Staker).StartTime()
				return aTime.Before(bTime)
			}),
		},
	}
}

func (h *byStartTime) Timestamp() time.Time {
	return h.Peek().Unsigned.(txs.Staker).StartTime()
}
