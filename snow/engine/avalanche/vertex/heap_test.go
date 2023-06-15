// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
)

// This example inserts several ints into an IntHeap, checks the minimum,
// and removes them in order of priority.
func TestUniqueVertexHeapReturnsOrdered(t *testing.T) {
	require := require.New(t)

	h := NewHeap()

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 0,
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 1,
	}
	vtx2 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 1,
	}
	vtx3 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 3,
	}
	vtx4 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		HeightV: 0,
	}

	vts := []avalanche.Vertex{vtx0, vtx1, vtx2, vtx3, vtx4}

	for _, vtx := range vts {
		h.Push(vtx)
	}

	vtxZ := h.Pop()
	require.Equal(vtx4.ID(), vtxZ.ID())

	vtxA := h.Pop()
	height, err := vtxA.Height()
	require.NoError(err)
	require.Equal(uint64(3), height)
	require.Equal(vtx3.ID(), vtxA.ID())

	vtxB := h.Pop()
	height, err = vtxB.Height()
	require.NoError(err)
	require.Equal(uint64(1), height)
	require.Contains([]ids.ID{vtx1.ID(), vtx2.ID()}, vtxB.ID())

	vtxC := h.Pop()
	height, err = vtxC.Height()
	require.NoError(err)
	require.Equal(uint64(1), height)
	require.Contains([]ids.ID{vtx1.ID(), vtx2.ID()}, vtxC.ID())

	require.NotEqual(vtxB.ID(), vtxC.ID())

	vtxD := h.Pop()
	height, err = vtxD.Height()
	require.NoError(err)
	require.Zero(height)
	require.Equal(vtx0.ID(), vtxD.ID())

	require.Zero(h.Len())
}

func TestUniqueVertexHeapRemainsUnique(t *testing.T) {
	require := require.New(t)

	h := NewHeap()

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 0,
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 1,
	}

	sharedID := ids.GenerateTestID()
	vtx2 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     sharedID,
			StatusV: choices.Processing,
		},
		HeightV: 1,
	}
	vtx3 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     sharedID,
			StatusV: choices.Processing,
		},
		HeightV: 2,
	}

	require.True(h.Push(vtx0))
	require.True(h.Push(vtx1))
	require.True(h.Push(vtx2))
	require.False(h.Push(vtx3))
	require.Equal(3, h.Len())
}
