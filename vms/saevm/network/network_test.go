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
		trackedIDs []ids.NodeID
	}{
		{
			name: "empty",
		},
		{
			name: "non_empty",
			trackedIDs: []ids.NodeID{
				ids.GenerateTestNodeID(),
				ids.GenerateTestNodeID(),
			},
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

			tracked := set.Of(tt.trackedIDs...)
			trackers := map[string]*p2p.PeerTracker{
				"PeerTracker":          net.PeerTracker,
				"TrieDependentTracker": net.TrieDependentTracker,
			}
			for name, tracker := range trackers {
				require.Equalf(t, tracked.Len(), tracker.Size(), "%s.Size()", name)
			}
			requireSelectsExactly(t, trackers, tracked)

			// A regular P2P connection MUST be reflected by [Network.Peers]
			// regardless of config, but MUST only be selectable by the
			// trackers if no exclusive list was configured.
			peer := ids.GenerateTestNodeID()
			require.NoError(t, net.Connected(t.Context(), peer, nil), "Connected()")
			require.True(t, net.Peers.Has(peer), "Peers.Has() connected peer")

			selectable := tracked
			if selectable.Len() == 0 {
				selectable = set.Of(peer)
			}
			requireSelectsExactly(t, trackers, selectable)
		})
	}
}

// requireSelectsExactly asserts that repeated calls to SelectPeer() on every
// tracker only ever return nodes in [want] and, collectively, return every
// node in [want]. If [want] is empty, SelectPeer() must report no peers.
func requireSelectsExactly(t *testing.T, trackers map[string]*p2p.PeerTracker, want set.Set[ids.NodeID]) {
	t.Helper()

	for name, tracker := range trackers {
		got := set.NewSet[ids.NodeID](want.Len())
		// SelectPeer() is random; sample enough times that missing a peer in
		// [want] has negligible probability.
		for range 128 {
			id, ok := tracker.SelectPeer()
			require.Equalf(t, want.Len() > 0, ok, "%s.SelectPeer() reported a peer", name)
			if !ok {
				break
			}
			require.Containsf(t, want, id, "%s.SelectPeer() returned an untracked peer", name)
			got.Add(id)
		}
		require.Equalf(t, want, got, "%s.SelectPeer() selectable peers", name)
	}
}
