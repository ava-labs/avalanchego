package avalanche

import (
	"testing"

	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
)

// This example inserts several ints into an IntHeap, checks the minimum,
// and removes them in order of priority.
func TestUniqueVertexHeapReturnsOrdered(t *testing.T) {
	h := newMaxVertexHeap()

	vtx0 := &Vtx{
		id:     GenerateID(),
		height: 0,
		status: choices.Processing,
	}

	vtx1 := &Vtx{
		id:     GenerateID(),
		height: 1,
		status: choices.Processing,
	}

	vtx2 := &Vtx{
		id:     GenerateID(),
		height: 1,
		status: choices.Processing,
	}

	vtx3 := &Vtx{
		id:     GenerateID(),
		height: 3,
		status: choices.Processing,
	}

	vtx4 := &Vtx{
		id:     GenerateID(),
		status: choices.Unknown,
	}

	vts := []avalanche.Vertex{vtx0, vtx1, vtx2, vtx3, vtx4}

	for _, vtx := range vts {
		h.Push(vtx)
	}

	vtxZ := h.Pop()
	if !vtxZ.ID().Equals(vtx4.ID()) {
		t.Fatalf("Heap did not pop unknown element first")
	}

	vtxA := h.Pop()
	if height, err := vtxA.Height(); err != nil || height != 3 {
		t.Fatalf("First height from heap was incorrect")
	} else if !vtxA.ID().Equals(vtx3.ID()) {
		t.Fatalf("Incorrect ID on vertex popped from heap")
	}

	vtxB := h.Pop()
	if height, err := vtxB.Height(); err != nil || height != 1 {
		t.Fatalf("First height from heap was incorrect")
	} else if !vtxB.ID().Equals(vtx1.ID()) && !vtxB.ID().Equals(vtx2.ID()) {
		t.Fatalf("Incorrect ID on vertex popped from heap")
	}

	vtxC := h.Pop()
	if height, err := vtxC.Height(); err != nil || height != 1 {
		t.Fatalf("First height from heap was incorrect")
	} else if !vtxC.ID().Equals(vtx1.ID()) && !vtxC.ID().Equals(vtx2.ID()) {
		t.Fatalf("Incorrect ID on vertex popped from heap")
	}

	if vtxB.ID().Equals(vtxC.ID()) {
		t.Fatalf("Heap returned same element more than once")
	}

	vtxD := h.Pop()
	if height, err := vtxD.Height(); err != nil || height != 0 {
		t.Fatalf("Last height returned was incorrect")
	} else if !vtxD.ID().Equals(vtx0.ID()) {
		t.Fatalf("Last item from heap had incorrect ID")
	}

	if h.Len() != 0 {
		t.Fatalf("Heap was not empty after popping all of its elements")
	}
}

func TestUniqueVertexHeapRemainsUnique(t *testing.T) {
	h := newMaxVertexHeap()

	vtx0 := &Vtx{
		height: 0,
		id:     GenerateID(),
		status: choices.Processing,
	}
	vtx1 := &Vtx{
		height: 1,
		id:     GenerateID(),
		status: choices.Processing,
	}

	sharedID := GenerateID()
	vtx2 := &Vtx{
		height: 1,
		id:     sharedID,
		status: choices.Processing,
	}

	vtx3 := &Vtx{
		height: 2,
		id:     sharedID,
		status: choices.Processing,
	}

	pushed1 := h.Push(vtx0)
	pushed2 := h.Push(vtx1)
	pushed3 := h.Push(vtx2)
	pushed4 := h.Push(vtx3)
	if h.Len() != 3 {
		t.Fatalf("Unique Vertex Heap has incorrect length: %d", h.Len())
	} else if !(pushed1 && pushed2 && pushed3) {
		t.Fatalf("Failed to push a new unique element")
	} else if pushed4 {
		t.Fatalf("Pushed non-unique element to the unique vertex heap")
	}
}
