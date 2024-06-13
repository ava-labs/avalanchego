// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/bag"
)

func newEarlyTermNoTraversalTestFactory(require *require.Assertions, alpha int) Factory {
	factory, err := NewEarlyTermNoTraversalFactory(alpha, alpha, prometheus.NewRegistry())
	require.NoError(err)
	return factory
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

	factory, err := NewEarlyTermNoTraversalFactory(alphaPreference, alphaConfidence, prometheus.NewRegistry())
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

	factory, err := NewEarlyTermNoTraversalFactory(alphaPreference, alphaConfidence, prometheus.NewRegistry())
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

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
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
