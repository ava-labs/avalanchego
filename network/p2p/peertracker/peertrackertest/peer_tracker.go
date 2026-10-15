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
)

// New returns an empty [peertracker.PeerTracker]. Observe it with
// [peertracker.PeerTracker.TrackedPeers] and [peertracker.PeerTracker.ResponsivePeers].
func New(t *testing.T) *peertracker.PeerTracker {
	t.Helper()

	tracker, err := peertracker.New(
		loggingtest.New(t, logging.Debug),
		"",
		prometheus.NewRegistry(),
		nil,
		nil,
	)
	require.NoError(t, err, "peertracker.New()")
	return tracker
}

// SeedResponsive marks nodeID responsive so a later de-score is a transition
// rather than a no-op. nodeID must already be connected.
func SeedResponsive(t *testing.T, tracker *peertracker.PeerTracker, nodeID ids.NodeID) {
	t.Helper()

	tracker.RegisterRequest(nodeID)
	tracker.RegisterResponse(nodeID, 1)
}
