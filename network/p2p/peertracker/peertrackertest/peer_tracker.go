// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peertrackertest

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/peertracker"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/version"
)

// New returns a [peertracker.PeerTracker] connected to peers. Observe it
// with [peertracker.PeerTracker.TrackedPeers] and [peertracker.PeerTracker.ResponsivePeers].
func New(t *testing.T, peers ...ids.NodeID) *peertracker.PeerTracker {
	t.Helper()

	tracker, err := peertracker.New(
		loggingtest.New(t, logging.Debug),
		"",
		prometheus.NewRegistry(),
		nil,
		nil,
	)
	require.NoError(t, err, "peertracker.New()")
	for _, nodeID := range peers {
		tracker.Connected(nodeID, version.Current)
	}
	return tracker
}

// SeedResponsive marks nodeID responsive so a later de-score is a transition
// rather than a no-op. nodeID must already be connected.
func SeedResponsive(t *testing.T, tracker *peertracker.PeerTracker, nodeID ids.NodeID) {
	t.Helper()

	tracker.RegisterRequest(nodeID)
	tracker.RegisterResponse(nodeID, 1)
}
