// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/bag"
)

func TestNoEarlyTermResults(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1) // k = 1

	factory := NewNoEarlyTermFactory()
	poll := factory.New(vdrs)

	poll.Vote(vdr1, blkID1)
	require.True(poll.Finished())

	result := poll.Result()
	list := result.List()
	require.Len(list, 1)
	require.Equal(blkID1, list[0])
	require.Equal(1, result.Count(blkID1))
}

func TestNoEarlyTermString(t *testing.T) {
	vdrs := bag.Of(vdr1, vdr2) // k = 2

	factory := NewNoEarlyTermFactory()
	poll := factory.New(vdrs)

	poll.Vote(vdr1, blkID1)

	expected := `waiting on Bag[ids.NodeID]: (Size = 1)
    NodeID-BaMPFdqMUQ46BV8iRcwbVfsam55kMqcp: 1
received Bag[ids.ID]: (Size = 1)
    SYXsAycDPUu4z2ZksJD5fh5nTDcH3vCFHnpcVye5XuJ2jArg: 1`
	require.Equal(t, expected, poll.String())
}

func TestNoEarlyTermDropsDuplicatedVotes(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2) // k = 2

	factory := NewNoEarlyTermFactory()
	poll := factory.New(vdrs)

	poll.Vote(vdr1, blkID1)
	require.False(poll.Finished())

	poll.Vote(vdr1, blkID1)
	require.False(poll.Finished())

	poll.Drop(vdr1)
	require.False(poll.Finished())

	poll.Vote(vdr2, blkID1)
	require.True(poll.Finished())
}
