// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/heap"
)

var _ AveragerHeap = averagerHeap{}

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
	heap heap.Map[ids.NodeID, *Averager]
}

// NewMinAveragerHeap returns a new empty min heap. The returned heap is not
// thread safe.
func NewMinAveragerHeap() AveragerHeap {
	return averagerHeap{
		heap: heap.NewMap[ids.NodeID, *Averager](func(a, b *Averager) bool {
			return (*a).Read() < (*b).Read()
		}),
	}
}

// NewMaxAveragerHeap returns a new empty max heap. The returned heap is not
// thread safe.
func NewMaxAveragerHeap() AveragerHeap {
	return averagerHeap{
		heap: heap.NewMap[ids.NodeID, *Averager](func(a, b *Averager) bool {
			return (*a).Read() > (*b).Read()
		}),
	}
}

func (h averagerHeap) Add(nodeID ids.NodeID, averager Averager) (Averager, bool) {
	if i, exists := h.heap.Index()[nodeID]; exists {
		_, averagerPtr := h.heap.Get(i)
		oldAverager := *averagerPtr
		*averagerPtr = averager
		h.heap.Fix(i)
		return oldAverager, true
	}

	h.heap.Push(nodeID, &averager)
	return nil, false
}

func (h averagerHeap) Remove(nodeID ids.NodeID) (Averager, bool) {
	i, exists := h.heap.Index()[nodeID]
	if !exists {
		return nil, false
	}
	_, averager := h.heap.Remove(i)
	return *averager, true
}

func (h averagerHeap) Pop() (ids.NodeID, Averager, bool) {
	if h.heap.Len() == 0 {
		return ids.EmptyNodeID, nil, false
	}

	nodeID, averagerPtr, _ := h.heap.Pop()
	return nodeID, *averagerPtr, true
}

func (h averagerHeap) Peek() (ids.NodeID, Averager, bool) {
	if h.heap.Len() == 0 {
		return ids.EmptyNodeID, nil, false
	}

	nodeID, averagerPtr, _ := h.heap.Peek()
	return nodeID, *averagerPtr, true
}

func (h averagerHeap) Len() int {
	return h.heap.Len()
}
