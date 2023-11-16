// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/heap"
)

func NewByAge() Heap {
	return &txHeap{
		heap: heap.NewMap[ids.ID, heapTx](func(a, b heapTx) bool {
			return a.age < b.age
		}),
	}
}
