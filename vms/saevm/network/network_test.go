// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestWithAllowedTrackedPeers(t *testing.T) {
	peer := ids.GenerateTestNodeID()

	tests := []struct {
		name         string
		trackedIDs   set.Set[ids.NodeID]
		expectedSize int
	}{
		{
			name:         "empty",
			expectedSize: 1,
		},
		{
			name: "non_empty_filtered",
			trackedIDs: set.Of(
				ids.GenerateTestNodeID(),
				ids.GenerateTestNodeID(),
			),
			expectedSize: 0,
		},
		{
			name: "includeFilter",
			trackedIDs: set.Of(
				peer,
				ids.GenerateTestNodeID(),
			),
			expectedSize: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snowCtx := snowtest.Context(t, snowtest.CChainID)
			net, err := New(
				snowCtx,
				&enginetest.Sender{},
				WithAllowedTrackedPeers(tt.trackedIDs),
			)
			require.NoError(t, err, "New()")

			require.NoError(t, net.Connected(t.Context(), peer, nil), "Connected()")
			require.True(t, net.Peers.Has(peer), "Peers.Has() connected peer")

			require.Equalf(t, tt.expectedSize, net.PeerTracker.Size(), "PeerTracker.Size()")
		})
	}
}
