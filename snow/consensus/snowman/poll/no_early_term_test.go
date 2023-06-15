// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

func TestNoEarlyTermResults(t *testing.T) {
	require := require.New(t)

	vtxID := ids.ID{1}

	vdr1 := ids.NodeID{1} // k = 1

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(vdr1)

	factory := NewNoEarlyTermFactory()
	poll := factory.New(vdrs)

	poll.Vote(vdr1, vtxID)
	require.True(poll.Finished())

	result := poll.Result()
	list := result.List()
	require.Len(list, 1)
	require.Equal(vtxID, list[0])
	require.Equal(1, result.Count(vtxID))
}

func TestNoEarlyTermString(t *testing.T) {
	vtxID := ids.ID{1}

	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2} // k = 2

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	factory := NewNoEarlyTermFactory()
	poll := factory.New(vdrs)

	poll.Vote(vdr1, vtxID)

	expected := `waiting on Bag[ids.NodeID]: (Size = 1)
    NodeID-BaMPFdqMUQ46BV8iRcwbVfsam55kMqcp: 1
received Bag[ids.ID]: (Size = 1)
    SYXsAycDPUu4z2ZksJD5fh5nTDcH3vCFHnpcVye5XuJ2jArg: 1`
	require.Equal(t, expected, poll.String())
}

func TestNoEarlyTermDropsDuplicatedVotes(t *testing.T) {
	require := require.New(t)

	vtxID := ids.ID{1}

	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2} // k = 2

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	factory := NewNoEarlyTermFactory()
	poll := factory.New(vdrs)

	poll.Vote(vdr1, vtxID)
	require.False(poll.Finished())

	poll.Vote(vdr1, vtxID)
	require.False(poll.Finished())

	poll.Drop(vdr1)
	require.False(poll.Finished())

	poll.Vote(vdr2, vtxID)
	require.True(poll.Finished())
}
