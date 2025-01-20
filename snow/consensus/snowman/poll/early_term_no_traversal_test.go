// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

type parentGetter func(id ids.ID) (ids.ID, bool)

func (p parentGetter) GetParent(id ids.ID) (ids.ID, bool) {
	return p(id)
}

func newEarlyTermNoTraversalTestFactory(require *require.Assertions, alpha int) Factory {
	factory, err := NewEarlyTermFactory(alpha, alpha, prometheus.NewRegistry(), parentGetter(returnEmpty))
	require.NoError(err)
	return factory
}

func returnEmpty(_ ids.ID) (ids.ID, bool) {
	return ids.Empty, false
}

func TestEarlyTermNoTraversalResults(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1) // k = 1
	alpha := 1

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, blkID1)
	require.True(poll.Finished())

	result := poll.Result()
	list := result.List()
	require.Len(list, 1)
	require.Equal(blkID1, list[0])
	require.Equal(1, result.Count(blkID1))
}

func TestEarlyTermNoTraversalString(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2) // k = 2
	alpha := 2

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, blkID1)

	expected := `waiting on Bag[ids.NodeID]: (Size = 1)
    NodeID-BaMPFdqMUQ46BV8iRcwbVfsam55kMqcp: 1
received Bag[ids.ID]: (Size = 1)
    SYXsAycDPUu4z2ZksJD5fh5nTDcH3vCFHnpcVye5XuJ2jArg: 1`
	require.Equal(expected, poll.String())
}

func TestEarlyTermNoTraversalDropsDuplicatedVotes(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2) // k = 2
	alpha := 2

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, blkID1)
	require.False(poll.Finished())

	poll.Vote(vdr1, blkID1)
	require.False(poll.Finished())

	poll.Vote(vdr2, blkID1)
	require.True(poll.Finished())
}

// Tests case 2
func TestEarlyTermNoTraversalTerminatesEarlyWithoutAlphaPreference(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2, vdr3) // k = 3
	alpha := 2

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	poll := factory.New(vdrs)

	poll.Drop(vdr1)
	require.False(poll.Finished())

	poll.Drop(vdr2)
	require.True(poll.Finished())
}

// Tests case 3
func TestEarlyTermNoTraversalTerminatesEarlyWithAlphaPreference(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2, vdr3, vdr4, vdr5) // k = 5
	alphaPreference := 3
	alphaConfidence := 5

	factory, err := NewEarlyTermFactory(alphaPreference, alphaConfidence, prometheus.NewRegistry(), parentGetter(returnEmpty))
	require.NoError(err)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, blkID1)
	require.False(poll.Finished())

	poll.Vote(vdr2, blkID1)
	require.False(poll.Finished())

	poll.Vote(vdr3, blkID1)
	require.False(poll.Finished())

	poll.Drop(vdr4)
	require.True(poll.Finished())
}

// Tests case 4
func TestEarlyTermNoTraversalTerminatesEarlyWithAlphaConfidence(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2, vdr3, vdr4, vdr5) // k = 5
	alphaPreference := 3
	alphaConfidence := 3

	factory, err := NewEarlyTermFactory(alphaPreference, alphaConfidence, prometheus.NewRegistry(), parentGetter(returnEmpty))
	require.NoError(err)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, blkID1)
	require.False(poll.Finished())

	poll.Vote(vdr2, blkID1)
	require.False(poll.Finished())

	poll.Vote(vdr3, blkID1)
	require.True(poll.Finished())
}

// If validators 1-3 vote for blocks B, C, and D respectively, which all share
// the common ancestor A, then we cannot terminate early with alpha = k = 4.
//
// If the final vote is cast for any of A, B, C, or D, then A will have
// transitively received alpha = 4 votes
func TestEarlyTermNoTraversalForSharedAncestor(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2, vdr3, vdr4) // k = 4
	alpha := 4

	g := ancestryGraph{
		blkID2: blkID1,
		blkID3: blkID1,
		blkID4: blkID1,
	}

	factory, err := NewEarlyTermFactory(alpha, alpha, prometheus.NewRegistry(), g)
	require.NoError(err)

	poll := factory.New(vdrs)

	poll.Vote(vdr1, blkID2)
	require.False(poll.Finished())

	poll.Vote(vdr2, blkID3)
	require.False(poll.Finished())

	poll.Vote(vdr3, blkID4)
	require.False(poll.Finished())

	poll.Vote(vdr4, blkID1)
	require.True(poll.Finished())
}

func TestEarlyTermNoTraversalWithWeightedResponses(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2, vdr2) // k = 3
	alpha := 2

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr2, blkID1)
	require.True(poll.Finished())

	result := poll.Result()
	list := result.List()
	require.Len(list, 1)
	require.Equal(blkID1, list[0])
	require.Equal(2, result.Count(blkID1))
}

func TestEarlyTermNoTraversalDropWithWeightedResponses(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2, vdr2) // k = 3
	alpha := 2

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	poll := factory.New(vdrs)

	poll.Drop(vdr2)
	require.True(poll.Finished())
}

type ancestryGraph map[ids.ID]ids.ID

func (ag ancestryGraph) GetParent(id ids.ID) (ids.ID, bool) {
	parent, ok := ag[id]
	if !ok {
		return ids.Empty, false
	}
	return parent, true
}

func TestTransitiveVotesForPrefixes(t *testing.T) {
	require := require.New(t)

	g := &voteVertex{
		id: ids.ID{1},
		descendants: []*voteVertex{
			{id: ids.ID{2}},
			{id: ids.ID{4}},
		},
	}
	wireParents(g)
	getParent := getParentFunc(g)
	votes := bag.Of(ids.ID{1}, ids.ID{1}, ids.ID{2}, ids.ID{4})
	vg := buildVoteGraph(getParent, votes)
	transitiveVotes := computeTransitiveVotesForPrefixes(&vg, votes)

	var voteCount int
	for _, count := range transitiveVotes {
		voteCount = count
	}

	require.Len(transitiveVotes, 1)
	require.Equal(2, voteCount)
}

func TestEarlyTermYesTraversal(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2, vdr3, vdr4, vdr5) // k = 5
	alphaPreference := 2
	alphaConfidence := 3

	//          blkID1
	//     blkID2    blkID4
	g := ancestryGraph{
		blkID4: blkID1,
		blkID2: blkID1,
	}

	factory, err := NewEarlyTermFactory(alphaPreference, alphaConfidence, prometheus.NewRegistry(), g)
	require.NoError(err)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, blkID1)
	require.False(poll.Finished())

	poll.Vote(vdr2, blkID1)
	require.False(poll.Finished())

	poll.Vote(vdr3, blkID2)
	require.False(poll.Finished())

	poll.Vote(vdr4, blkID4)
	require.False(poll.Finished())
}

func TestEarlyTermYesTraversalII(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2, vdr3, vdr4, vdr5) // k = 5
	alphaPreference := 2
	alphaConfidence := 3

	blkID0 := ids.ID{0x00, 0x00}
	blkID1 := ids.ID{0xf0, 0xff}
	blkID2 := ids.ID{0xff, 0xf0}
	blkID3 := ids.ID{0x0f, 0xff}

	//          blkID0
	//    blkID1  blkID2  blkID3
	g := ancestryGraph{
		blkID3: blkID0,
		blkID2: blkID0,
		blkID1: blkID0,
	}

	factory, err := NewEarlyTermFactory(alphaPreference, alphaConfidence, prometheus.NewRegistry(), g)
	require.NoError(err)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, blkID2)
	require.False(poll.Finished())

	poll.Vote(vdr2, blkID3)
	require.False(poll.Finished())

	poll.Vote(vdr3, blkID3)
	require.False(poll.Finished())

	poll.Vote(vdr4, blkID3)
	require.False(poll.Finished())

	//
	//        blkID0                       blk0: {0x00, 0x00}
	//     0/       \4                     blk1: {0xf0, 0xff}
	// blkID1      {0x?f}                  blk2: {0xff, 0xf0}
	//            1/     \3                blk3: {0x0f, 0xff}
	//           blkID2  blkID3

	poll.Vote(vdr5, blkID2)
	require.True(poll.Finished())
}
