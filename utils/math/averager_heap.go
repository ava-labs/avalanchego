// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/heap"
)

var _ AveragerHeap = (*averagerHeap)(nil)

// TODO replace this interface with utils/heap
// AveragerHeap maintains a heap of the averagers.
type AveragerHeap interface {
	// Add the average to the heap. If [nodeID] is already in the heap, the
	// average will be replaced and the old average will be returned. If there
	// was not an old average, false will be returned.
	Add(nodeID ids.NodeID, averager Averager) (Averager, bool)
	// Remove attempts to remove the average that was added with the provided
	// [nodeID], if none is contained in the heap, [false] will be returned.
	Remove(nodeID ids.NodeID) (Averager, bool)
	// Pop attempts to remove the node with either the largest or smallest
	// average, depending on if this is a max heap or a min heap, respectively.
	Pop() (ids.NodeID, Averager, bool)
	// Peek attempts to return the node with either the largest or smallest
	// average, depending on if this is a max heap or a min heap, respectively.
	Peek() (ids.NodeID, Averager, bool)
	// Len returns the number of nodes that are currently in the heap.
	Len() int
}

type averagerHeap struct {
	heap heap.Map[ids.NodeID, Averager]
}

// NewMaxAveragerHeap returns a new empty max heap. The returned heap is not
// thread safe.
func NewMaxAveragerHeap() AveragerHeap {
	return averagerHeap{
		heap: heap.NewMap[ids.NodeID, Averager](func(a, b Averager) bool {
			return a.Read() > b.Read()
		}),
	}
}

func (h averagerHeap) Add(nodeID ids.NodeID, averager Averager) (Averager, bool) {
	return h.heap.Push(nodeID, averager)
}

func (h averagerHeap) Remove(nodeID ids.NodeID) (Averager, bool) {
	return h.heap.Remove(nodeID)
}

func (h averagerHeap) Pop() (ids.NodeID, Averager, bool) {
	return h.heap.Pop()
}

func (h averagerHeap) Peek() (ids.NodeID, Averager, bool) {
	return h.heap.Peek()
}

func (h averagerHeap) Len() int {
	return h.heap.Len()
}
