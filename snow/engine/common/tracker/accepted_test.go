// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestAccepted(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	blkID0 := ids.GenerateTestID()
	blkID1 := ids.GenerateTestID()

	a := NewAccepted()

	_, ok := a.LastAccepted(nodeID)
	require.False(ok)

	a.SetLastAccepted(nodeID, blkID0)
	_, ok = a.LastAccepted(nodeID)
	require.False(ok)

	a.OnValidatorAdded(nodeID, nil, ids.GenerateTestID(), 1)

	_, ok = a.LastAccepted(nodeID)
	require.False(ok)

	a.SetLastAccepted(nodeID, blkID0)
	blkID, ok := a.LastAccepted(nodeID)
	require.True(ok)
	require.Equal(blkID0, blkID)

	a.SetLastAccepted(nodeID, blkID1)
	blkID, ok = a.LastAccepted(nodeID)
	require.True(ok)
	require.Equal(blkID1, blkID)

	a.OnValidatorRemoved(nodeID, 1)

	_, ok = a.LastAccepted(nodeID)
	require.False(ok)
}
