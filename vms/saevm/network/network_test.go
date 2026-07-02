// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestNewTrackedIDs(t *testing.T) {
	tests := []struct {
		name       string
		trackedIDs set.Set[ids.NodeID]
	}{
		{
			name: "empty",
		},
		{
			name: "non_empty",
			trackedIDs: set.Of(
				ids.GenerateTestNodeID(),
				ids.GenerateTestNodeID(),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snowCtx := snowtest.Context(t, snowtest.CChainID)
			net, err := New(
				Config{TrackedIDs: tt.trackedIDs},
				snowCtx,
				&enginetest.Sender{},
			)
			require.NoError(t, err, "New()")

			trackers := map[string]*p2p.PeerTracker{
				"PeerTracker":          net.PeerTracker,
				"TrieDependentTracker": net.TrieDependentTracker,
			}
			for name, tracker := range trackers {
				require.Equalf(t, tt.trackedIDs.Len(), tracker.Size(), "%s.Size()", name)
			}

			// A regular P2P connection MUST be reflected by [Network.Peers]
			// regardless of config, but MUST only be selectable by the
			// trackers if no exclusive list was configured.
			peer := ids.GenerateTestNodeID()
			require.NoError(t, net.Connected(t.Context(), peer, nil), "Connected()")
			require.True(t, net.Peers.Has(peer), "Peers.Has() connected peer")

			selectable := tt.trackedIDs
			if selectable.Len() == 0 {
				selectable = set.Of(peer)
			}

			for name, tracker := range trackers {
				require.Equalf(t, selectable.Len(), tracker.Size(), "%s.Size()", name)
			}
		})
	}
}
