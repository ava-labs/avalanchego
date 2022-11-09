// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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

	p.OnValidatorAdded(nodeID, 5)
	require.Zero(p.ConnectedWeight())
	require.Empty(p.PreferredPeers())

	err := p.Connected(context.Background(), nodeID, version.CurrentApp)
	require.NoError(err)
	require.EqualValues(5, p.ConnectedWeight())
	require.Contains(p.PreferredPeers(), nodeID)

	p.OnValidatorWeightChanged(nodeID, 5, 10)
	require.EqualValues(10, p.ConnectedWeight())
	require.Contains(p.PreferredPeers(), nodeID)

	p.OnValidatorRemoved(nodeID, 10)
	require.Zero(p.ConnectedWeight())
	require.Contains(p.PreferredPeers(), nodeID)

	p.OnValidatorAdded(nodeID, 5)
	require.EqualValues(5, p.ConnectedWeight())
	require.Contains(p.PreferredPeers(), nodeID)

	err = p.Disconnected(context.Background(), nodeID)
	require.NoError(err)
	require.Zero(p.ConnectedWeight())
	require.Empty(p.PreferredPeers())
}
