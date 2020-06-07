package avalanche

import (
	"container/heap"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
)

// A vertexItem is a Vertex managed by the priority queue.
type vertexItem struct {
	vertex avalanche.Vertex
	index  int // The index of the item in the heap.
}

// A priorityQueue implements heap.Interface and holds vertexItems.
type priorityQueue []*vertexItem

func (pq priorityQueue) Len() int { return len(pq) }

// Returns true if the vertex at index i has greater height than the vertex at
// index j.
func (pq priorityQueue) Less(i, j int) bool {
	statusI := pq[i].vertex.Status()
	statusJ := pq[j].vertex.Status()

	// Put unknown vertices at the front of the heap to ensure once we have made
	// it below a certain height in DAG traversal we do not need to reset
	if !statusI.Fetched() {
		return true
	}
	if !statusJ.Fetched() {
		return false
	}
	return pq[i].vertex.Height() > pq[j].vertex.Height()
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

// Push adds an item to this priority queue. x must have type *vertexItem
func (pq *priorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*vertexItem)
	item.index = n
	*pq = append(*pq, item)
}

// Pop returns the last item in this priorityQueue
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

// vertexHeap defines the functionality of a heap of vertices
// with unique VertexIDs ordered by height
type vertexHeap interface {
	Clear()
	Push(avalanche.Vertex)
	Pop() avalanche.Vertex // Requires that there be at least one element
	Contains(avalanche.Vertex) bool
	Len() int
}

type maxHeightVertexHeap struct {
	heap       *priorityQueue
	elementIDs ids.Set
}

func newMaxVertexHeap() *maxHeightVertexHeap {
	return &maxHeightVertexHeap{
		heap:       &priorityQueue{},
		elementIDs: ids.Set{},
	}
}

func (vh *maxHeightVertexHeap) Clear() {
	vh.heap = &priorityQueue{}
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
	item := &vertexItem{
		vertex: vtx,
	}
	heap.Push(vh.heap, item)
	return true
}

// If there are any vertices in this heap with status Unknown, removes one such
// vertex and returns it. Otherwise, removes and returns the vertex in this heap
// with the greatest height.
func (vh *maxHeightVertexHeap) Pop() avalanche.Vertex {
	vtx := heap.Pop(vh.heap).(*vertexItem).vertex
	vh.elementIDs.Remove(vtx.ID())
	return vtx
}

func (vh *maxHeightVertexHeap) Len() int { return vh.heap.Len() }

func (vh *maxHeightVertexHeap) Contains(vtxID ids.ID) bool { return vh.elementIDs.Contains(vtxID) }
