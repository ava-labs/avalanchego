// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/utils/heap"
)

var _ Heap = (*maxHeightVertexHeap)(nil)

func Less(i, j avalanche.Vertex) bool {
	statusI := i.Status()
	statusJ := j.Status()

	// Put unknown vertices at the front of the heap to ensure once we have made
	// it below a certain height in DAG traversal we do not need to reset
	if !statusI.Fetched() {
		return true
	}
	if !statusJ.Fetched() {
		return false
	}

	// Treat errors on retrieving the height as if the vertex is not fetched
	heightI, errI := i.Height()
	if errI != nil {
		return true
	}
	heightJ, errJ := j.Height()
	if errJ != nil {
		return false
	}
	return heightI > heightJ
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
func NewHeap() Heap {
	return &maxHeightVertexHeap{
		heap: heap.NewMap[ids.ID, avalanche.Vertex](Less),
	}
}

type maxHeightVertexHeap struct {
	heap heap.Map[ids.ID, avalanche.Vertex]
}

func (vh *maxHeightVertexHeap) Clear() {
	vh.heap = heap.NewMap[ids.ID, avalanche.Vertex](Less)
}

// Push adds an element to this heap. Returns true if the element was added.
// Returns false if it was already in the heap.
func (vh *maxHeightVertexHeap) Push(vtx avalanche.Vertex) bool {
	vtxID := vtx.ID()
	if ok := vh.heap.Contains(vtxID); ok {
		return false
	}

	vh.heap.Push(vtxID, vtx)
	return true
}

// If there are any vertices in this heap with status Unknown, removes one such
// vertex and returns it. Otherwise, removes and returns the vertex in this heap
// with the greatest height.
func (vh *maxHeightVertexHeap) Pop() avalanche.Vertex {
	_, vtx, _ := vh.heap.Pop()
	return vtx
}

func (vh *maxHeightVertexHeap) Len() int {
	return vh.heap.Len()
}

func (vh *maxHeightVertexHeap) Contains(vtxID ids.ID) bool {
	return vh.heap.Contains(vtxID)
}
