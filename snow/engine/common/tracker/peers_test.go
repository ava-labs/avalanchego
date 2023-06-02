// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"
)

func TestPeers(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()

	p := NewPeers()

	require.Zero(p.ConnectedWeight())
	require.Empty(p.PreferredPeers())

	p.OnValidatorAdded(nodeID, nil, ids.Empty, 5)
	require.Zero(p.ConnectedWeight())
	require.Empty(p.PreferredPeers())

	require.NoError(p.Connected(context.Background(), nodeID, version.CurrentApp))
	require.Equal(uint64(5), p.ConnectedWeight())
	require.Contains(p.PreferredPeers(), nodeID)

	p.OnValidatorWeightChanged(nodeID, 5, 10)
	require.Equal(uint64(10), p.ConnectedWeight())
	require.Contains(p.PreferredPeers(), nodeID)

	p.OnValidatorRemoved(nodeID, 10)
	require.Zero(p.ConnectedWeight())
	require.Contains(p.PreferredPeers(), nodeID)

	p.OnValidatorAdded(nodeID, nil, ids.Empty, 5)
	require.Equal(uint64(5), p.ConnectedWeight())
	require.Contains(p.PreferredPeers(), nodeID)

	require.NoError(p.Disconnected(context.Background(), nodeID))
	require.Zero(p.ConnectedWeight())
	require.Empty(p.PreferredPeers())
}
