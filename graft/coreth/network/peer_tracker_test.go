// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestPeerTracker(t *testing.T) {
	require := require.New(t)
	p := NewPeerTracker()

	// Connect some peers
	numExtraPeers := 10
	numPeers := desiredMinResponsivePeers + numExtraPeers
	peerIDs := make([]ids.NodeID, numPeers)

	for i := range peerIDs {
		peerIDs[i] = ids.GenerateTestNodeID()
		p.Connected(peerIDs[i], defaultPeerVersion)
	}

	responsivePeers := make(map[ids.NodeID]bool)

	// Expect requests to go to new peers until we have desiredMinResponsivePeers responsive peers.
	for i := 0; i < desiredMinResponsivePeers+numExtraPeers/2; i++ {
		peer, ok, err := p.GetAnyPeer(nil)
		require.NoError(err)
		require.True(ok)
		require.NotNil(peer)

		_, exists := responsivePeers[peer]
		require.Falsef(exists, "expected connecting to a new peer, but got the same peer twice: peer %s iteration %d", peer, i)
		responsivePeers[peer] = true

		p.TrackPeer(peer) // mark the peer as having a message sent to it
	}

	// Mark some peers as responsive and others as not responsive
	i := 0
	for peer := range responsivePeers {
		if i < desiredMinResponsivePeers {
			p.TrackBandwidth(peer, 10)
		} else {
			responsivePeers[peer] = false // remember which peers were not responsive
			p.TrackBandwidth(peer, 0)
		}
		i++
	}

	// Expect requests to go to responsive or new peers, so long as they are available
	numRequests := 50
	for i := 0; i < numRequests; i++ {
		peer, ok, err := p.GetAnyPeer(nil)
		require.NoError(err)
		require.True(ok)
		require.NotNil(peer)

		responsive, ok := responsivePeers[peer]
		if ok {
			require.Truef(responsive, "expected connecting to a responsive peer, but got a peer that was not responsive: peer %s iteration %d", peer, i)
			p.TrackBandwidth(peer, 10)
		} else {
			responsivePeers[peer] = false // remember that we connected to this peer
			p.TrackPeer(peer)             // mark the peer as having a message sent to it
			p.TrackBandwidth(peer, 0)     // mark the peer as non-responsive
		}
	}

	// Disconnect from peers that were previously responsive and ones we didn't connect to yet.
	for _, peer := range peerIDs {
		responsive, ok := responsivePeers[peer]
		if ok && responsive || !ok {
			p.Disconnected(peer)
		}
	}

	// Requests should fall back on non-responsive peers when no other choice is left
	peer, ok, err := p.GetAnyPeer(nil)
	require.NoError(err)
	require.True(ok)
	require.NotNil(peer)

	responsive, ok := responsivePeers[peer]
	require.True(ok)
	require.Falsef(responsive, "expected connecting to a non-responsive peer, but got a peer that was responsive: peer %s", peer)
}
