// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"container/heap"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ AveragerHeap   = averagerHeap{}
	_ heap.Interface = (*averagerHeapBackend)(nil)
)

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

type averagerHeapEntry struct {
	nodeID   ids.NodeID
	averager Averager
	index    int
}

type averagerHeapBackend struct {
	isMaxHeap     bool
	nodeIDToEntry map[ids.NodeID]*averagerHeapEntry
	entries       []*averagerHeapEntry
}

type averagerHeap struct {
	b *averagerHeapBackend
}

// NewMinAveragerHeap returns a new empty min heap. The returned heap is not
// thread safe.
func NewMinAveragerHeap() AveragerHeap {
	return averagerHeap{b: &averagerHeapBackend{
		nodeIDToEntry: make(map[ids.NodeID]*averagerHeapEntry),
	}}
}

// NewMaxAveragerHeap returns a new empty max heap. The returned heap is not
// thread safe.
func NewMaxAveragerHeap() AveragerHeap {
	return averagerHeap{b: &averagerHeapBackend{
		isMaxHeap:     true,
		nodeIDToEntry: make(map[ids.NodeID]*averagerHeapEntry),
	}}
}

func (h averagerHeap) Add(nodeID ids.NodeID, averager Averager) (Averager, bool) {
	if e, exists := h.b.nodeIDToEntry[nodeID]; exists {
		oldAverager := e.averager
		e.averager = averager
		heap.Fix(h.b, e.index)
		return oldAverager, true
	}

	heap.Push(h.b, &averagerHeapEntry{
		nodeID:   nodeID,
		averager: averager,
	})
	return nil, false
}

func (h averagerHeap) Remove(nodeID ids.NodeID) (Averager, bool) {
	e, exists := h.b.nodeIDToEntry[nodeID]
	if !exists {
		return nil, false
	}
	heap.Remove(h.b, e.index)
	return e.averager, true
}

func (h averagerHeap) Pop() (ids.NodeID, Averager, bool) {
	if len(h.b.entries) == 0 {
		return ids.EmptyNodeID, nil, false
	}
	e := h.b.entries[0]
	heap.Pop(h.b)
	return e.nodeID, e.averager, true
}

func (h averagerHeap) Peek() (ids.NodeID, Averager, bool) {
	if len(h.b.entries) == 0 {
		return ids.EmptyNodeID, nil, false
	}
	e := h.b.entries[0]
	return e.nodeID, e.averager, true
}

func (h averagerHeap) Len() int {
	return len(h.b.entries)
}

func (h *averagerHeapBackend) Len() int {
	return len(h.entries)
}

func (h *averagerHeapBackend) Less(i, j int) bool {
	if h.isMaxHeap {
		return h.entries[i].averager.Read() > h.entries[j].averager.Read()
	}
	return h.entries[i].averager.Read() < h.entries[j].averager.Read()
}

func (h *averagerHeapBackend) Swap(i, j int) {
	h.entries[i], h.entries[j] = h.entries[j], h.entries[i]
	h.entries[i].index = i
	h.entries[j].index = j
}

func (h *averagerHeapBackend) Push(x interface{}) {
	e := x.(*averagerHeapEntry)
	e.index = len(h.entries)
	h.nodeIDToEntry[e.nodeID] = e
	h.entries = append(h.entries, e)
}

func (h *averagerHeapBackend) Pop() interface{} {
	newLen := len(h.entries) - 1
	e := h.entries[newLen]
	h.entries[newLen] = nil
	delete(h.nodeIDToEntry, e.nodeID)
	h.entries = h.entries[:newLen]
	return e
}
