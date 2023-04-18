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
	frontier0 := []ids.ID{ids.GenerateTestID()}
	frontier1 := []ids.ID{ids.GenerateTestID()}

	a := NewAccepted()

	require.Empty(a.AcceptedFrontier(nodeID))

	a.SetAcceptedFrontier(nodeID, frontier0)
	require.Empty(a.AcceptedFrontier(nodeID))

	a.OnValidatorAdded(nodeID, nil, ids.GenerateTestID(), 1)

	require.Empty(a.AcceptedFrontier(nodeID))

	a.SetAcceptedFrontier(nodeID, frontier0)
	require.Equal(frontier0, a.AcceptedFrontier(nodeID))

	a.SetAcceptedFrontier(nodeID, frontier1)
	require.Equal(frontier1, a.AcceptedFrontier(nodeID))

	a.OnValidatorRemoved(nodeID, 1)

	require.Empty(a.AcceptedFrontier(nodeID))
}
