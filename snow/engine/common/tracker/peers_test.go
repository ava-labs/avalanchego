// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

func TestPeers(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()

	p := NewPeers()

	require.Zero(p.ConnectedWeight())

	p.OnValidatorAdded(nodeID, nil, ids.Empty, 5)
	require.Zero(p.ConnectedWeight())

	require.NoError(p.Connected(context.Background(), nodeID, version.CurrentApp))
	require.Equal(uint64(5), p.ConnectedWeight())

	p.OnValidatorWeightChanged(nodeID, 5, 10)
	require.Equal(uint64(10), p.ConnectedWeight())

	p.OnValidatorRemoved(nodeID, 10)
	require.Zero(p.ConnectedWeight())

	p.OnValidatorAdded(nodeID, nil, ids.Empty, 5)
	require.Equal(uint64(5), p.ConnectedWeight())

	require.NoError(p.Disconnected(context.Background(), nodeID))
	require.Zero(p.ConnectedWeight())
}

func TestConnectedValidators(t *testing.T) {
	require := require.New(t)

	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()

	p := NewPeers()

	p.OnValidatorAdded(nodeID1, nil, ids.Empty, 5)
	p.OnValidatorAdded(nodeID2, nil, ids.Empty, 6)

	require.NoError(p.Connected(context.Background(), nodeID1, version.CurrentApp))
	require.Equal(uint64(5), p.ConnectedWeight())

	require.NoError(p.Connected(context.Background(), nodeID2, version.CurrentApp))
	require.Equal(uint64(11), p.ConnectedWeight())
	require.True(set.Of(ids.NodeWeight{Node: nodeID1, Weight: 5}, ids.NodeWeight{Node: nodeID2, Weight: 6}).Equals(p.GetValidators()))
	require.True(set.Of(ids.NodeWeight{Node: nodeID1, Weight: 5}, ids.NodeWeight{Node: nodeID2, Weight: 6}).Equals(p.ConnectedValidators()))

	require.NoError(p.Disconnected(context.Background(), nodeID2))
	require.True(set.Of(ids.NodeWeight{Node: nodeID1, Weight: 5}, ids.NodeWeight{Node: nodeID2, Weight: 6}).Equals(p.GetValidators()))
	require.True(set.Of(ids.NodeWeight{Node: nodeID1, Weight: 5}).Equals(p.ConnectedValidators()))
}
