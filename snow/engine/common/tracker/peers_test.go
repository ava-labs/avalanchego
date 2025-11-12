// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
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

	p.OnValidatorAdded(nodeID, nil, ids.Empty, 5)
	require.Zero(p.ConnectedWeight())

	require.NoError(p.Connected(t.Context(), nodeID, version.Current))
	require.Equal(uint64(5), p.ConnectedWeight())

	p.OnValidatorWeightChanged(nodeID, 5, 10)
	require.Equal(uint64(10), p.ConnectedWeight())

	p.OnValidatorRemoved(nodeID, 10)
	require.Zero(p.ConnectedWeight())

	p.OnValidatorAdded(nodeID, nil, ids.Empty, 5)
	require.Equal(uint64(5), p.ConnectedWeight())

	require.NoError(p.Disconnected(t.Context(), nodeID))
	require.Zero(p.ConnectedWeight())
}
