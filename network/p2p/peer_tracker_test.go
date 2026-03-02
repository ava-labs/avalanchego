// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
)

func TestPeerTracker(t *testing.T) {
	require := require.New(t)
	p, err := NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		nil,
	)
	require.NoError(err)

	// Connect some peers
	numExtraPeers := 10
	numPeers := desiredMinResponsivePeers + numExtraPeers
	peerIDs := make([]ids.NodeID, numPeers)
	peerVersion := &version.Application{
		Major: 1,
		Minor: 2,
		Patch: 3,
	}

	for i := range peerIDs {
		peerIDs[i] = ids.GenerateTestNodeID()
		p.Connected(peerIDs[i], peerVersion)
	}

	responsivePeers := make(map[ids.NodeID]bool)

	// Expect requests to go to new peers until we have desiredMinResponsivePeers responsive peers.
	for i := 0; i < desiredMinResponsivePeers+numExtraPeers/2; i++ {
		peer, ok := p.SelectPeer()
		require.True(ok)
		require.NotZero(peer)

		_, exists := responsivePeers[peer]
		require.Falsef(exists, "expected connecting to a new peer, but got the same peer twice: peer %s iteration %d", peer, i)
		responsivePeers[peer] = true

		p.RegisterRequest(peer) // mark the peer as having a message sent to it
	}

	// Mark some peers as responsive and others as not responsive
	i := 0
	for peer := range responsivePeers {
		if i < desiredMinResponsivePeers {
			p.RegisterResponse(peer, 10)
		} else {
			responsivePeers[peer] = false // remember which peers were not responsive
			p.RegisterFailure(peer)
		}
		i++
	}

	// Expect requests to go to responsive or new peers, so long as they are available
	numRequests := 50
	for i := 0; i < numRequests; i++ {
		peer, ok := p.SelectPeer()
		require.True(ok)
		require.NotZero(peer)

		responsive, ok := responsivePeers[peer]
		if ok {
			require.Truef(responsive, "expected connecting to a responsive peer, but got a peer that was not responsive: peer %s iteration %d", peer, i)
			p.RegisterResponse(peer, 10)
		} else {
			responsivePeers[peer] = false // remember that we connected to this peer
			p.RegisterRequest(peer)       // mark the peer as having a message sent to it
			p.RegisterFailure(peer)       // mark the peer as non-responsive
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
	peer, ok := p.SelectPeer()
	require.True(ok)
	require.NotZero(peer)

	responsive, ok := responsivePeers[peer]
	require.True(ok)
	require.Falsef(responsive, "expected connecting to a non-responsive peer, but got a peer that was responsive: peer %s", peer)
}
