// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

func TestEarlyTermNoTraversalResults(t *testing.T) {
	require := require.New(t)

	alpha := 1

	vtxID := ids.ID{1}

	vdr1 := ids.NodeID{1} // k = 1

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(vdr1)

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, vtxID)
	require.True(poll.Finished())

	result := poll.Result()
	list := result.List()
	require.Len(list, 1)
	require.Equal(vtxID, list[0])
	require.Equal(1, result.Count(vtxID))
}

func TestEarlyTermNoTraversalString(t *testing.T) {
	alpha := 2

	vtxID := ids.ID{1}

	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2} // k = 2

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, vtxID)

	expected := `waiting on Bag[ids.NodeID]: (Size = 1)
    NodeID-BaMPFdqMUQ46BV8iRcwbVfsam55kMqcp: 1
received Bag[ids.ID]: (Size = 1)
    SYXsAycDPUu4z2ZksJD5fh5nTDcH3vCFHnpcVye5XuJ2jArg: 1`
	require.Equal(t, expected, poll.String())
}

func TestEarlyTermNoTraversalDropsDuplicatedVotes(t *testing.T) {
	require := require.New(t)

	alpha := 2

	vtxID := ids.ID{1}

	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2} // k = 2

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, vtxID)
	require.False(poll.Finished())

	poll.Vote(vdr1, vtxID)
	require.False(poll.Finished())

	poll.Vote(vdr2, vtxID)
	require.True(poll.Finished())
}

func TestEarlyTermNoTraversalTerminatesEarly(t *testing.T) {
	require := require.New(t)

	alpha := 3

	vtxID := ids.ID{1}

	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}
	vdr3 := ids.NodeID{3}
	vdr4 := ids.NodeID{4}
	vdr5 := ids.NodeID{5} // k = 5

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(
		vdr1,
		vdr2,
		vdr3,
		vdr4,
		vdr5,
	)

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, vtxID)
	require.False(poll.Finished())

	poll.Vote(vdr2, vtxID)
	require.False(poll.Finished())

	poll.Vote(vdr3, vtxID)
	require.True(poll.Finished())
}

func TestEarlyTermNoTraversalForSharedAncestor(t *testing.T) {
	require := require.New(t)

	alpha := 4

	vtxA := ids.ID{1}
	vtxB := ids.ID{2}
	vtxC := ids.ID{3}
	vtxD := ids.ID{4}

	// If validators 1-3 vote for frontier vertices
	// B, C, and D respectively, which all share the common ancestor
	// A, then we cannot terminate early with alpha = k = 4
	// If the final vote is cast for any of A, B, C, or D, then
	// vertex A will have transitively received alpha = 4 votes
	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}
	vdr3 := ids.NodeID{3}
	vdr4 := ids.NodeID{4}

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(
		vdr1,
		vdr2,
		vdr3,
		vdr4,
	)

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, vtxB)
	require.False(poll.Finished())

	poll.Vote(vdr2, vtxC)
	require.False(poll.Finished())

	poll.Vote(vdr3, vtxD)
	require.False(poll.Finished())

	poll.Vote(vdr4, vtxA)
	require.True(poll.Finished())
}

func TestEarlyTermNoTraversalWithFastDrops(t *testing.T) {
	require := require.New(t)

	alpha := 2

	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}
	vdr3 := ids.NodeID{3} // k = 3

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(
		vdr1,
		vdr2,
		vdr3,
	)

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Drop(vdr1)
	require.False(poll.Finished())

	poll.Drop(vdr2)
	require.True(poll.Finished())
}

func TestEarlyTermNoTraversalWithWeightedResponses(t *testing.T) {
	require := require.New(t)

	alpha := 2

	vtxID := ids.ID{1}

	vdr1 := ids.NodeID{2}
	vdr2 := ids.NodeID{3}

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(
		vdr1,
		vdr2,
		vdr2,
	) // k = 3

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr2, vtxID)
	require.True(poll.Finished())

	result := poll.Result()
	list := result.List()
	require.Len(list, 1)
	require.Equal(vtxID, list[0])
	require.Equal(2, result.Count(vtxID))
}

func TestEarlyTermNoTraversalDropWithWeightedResponses(t *testing.T) {
	alpha := 2

	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(
		vdr1,
		vdr2,
		vdr2,
	) // k = 3

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Drop(vdr2)
	require.True(t, poll.Finished())
}
