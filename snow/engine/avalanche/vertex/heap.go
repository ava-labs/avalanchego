// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"container/heap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
)

var (
	_ Heap           = &maxHeightVertexHeap{}
	_ heap.Interface = &priorityQueue{}
)

// A priorityQueue implements heap.Interface and holds vertexItems.
type priorityQueue []avalanche.Vertex

func (pq priorityQueue) Len() int { return len(pq) }

// Returns true if the vertex at index i has greater height than the vertex at
// index j.
func (pq priorityQueue) Less(i, j int) bool {
	statusI := pq[i].Status()
	statusJ := pq[j].Status()

	// Put unknown vertices at the front of the heap to ensure once we have made
	// it below a certain height in DAG traversal we do not need to reset
	if !statusI.Fetched() {
		return true
	}
	if !statusJ.Fetched() {
		return false
	}

	// Treat errors on retrieving the height as if the vertex is not fetched
	heightI, errI := pq[i].Height()
	if errI != nil {
		return true
	}
	heightJ, errJ := pq[j].Height()
	if errJ != nil {
		return false
	}
	return heightI > heightJ
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

// Push adds an item to this priority queue. x must have type *vertexItem
func (pq *priorityQueue) Push(x interface{}) {
	item := x.(avalanche.Vertex)
	*pq = append(*pq, item)
}

// Pop returns the last item in this priorityQueue
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*pq = old[0 : n-1]
	return item
}

// Heap defines the functionality of a heap of vertices with unique VertexIDs
// ordered by height
type Heap interface {
	// Empty the heap.
	Clear()

	// Add the provided vertex to the heap. Vertices are de-duplicated, returns
	// true if the vertex was added, false if it was dropped.
	Push(avalanche.Vertex) bool

	// Remove the top vertex. Assumes that there is at least one element.
	Pop() avalanche.Vertex

	// Returns if a vertex with the provided ID is currently in the heap.
	Contains(ids.ID) bool

	// Returns the number of vertices in the heap.
	Len() int
}

// NewHeap returns an empty Heap
func NewHeap() Heap { return &maxHeightVertexHeap{} }

type maxHeightVertexHeap struct {
	heap       priorityQueue
	elementIDs ids.Set
}

func (vh *maxHeightVertexHeap) Clear() {
	vh.heap = priorityQueue{}
	vh.elementIDs.Clear()
}

// Push adds an element to this heap. Returns true if the element was added.
// Returns false if it was already in the heap.
func (vh *maxHeightVertexHeap) Push(vtx avalanche.Vertex) bool {
	vtxID := vtx.ID()
	if vh.elementIDs.Contains(vtxID) {
		return false
	}

	vh.elementIDs.Add(vtxID)
	heap.Push(&vh.heap, vtx)
	return true
}

// If there are any vertices in this heap with status Unknown, removes one such
// vertex and returns it. Otherwise, removes and returns the vertex in this heap
// with the greatest height.
func (vh *maxHeightVertexHeap) Pop() avalanche.Vertex {
	vtx := heap.Pop(&vh.heap).(avalanche.Vertex)
	vh.elementIDs.Remove(vtx.ID())
	return vtx
}

func (vh *maxHeightVertexHeap) Len() int { return vh.heap.Len() }

func (vh *maxHeightVertexHeap) Contains(vtxID ids.ID) bool { return vh.elementIDs.Contains(vtxID) }
