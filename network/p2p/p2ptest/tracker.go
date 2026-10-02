// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
)

// NewTracker returns an empty [p2p.PeerTracker]. Observe it with
// [p2p.PeerTracker.TrackedPeers] and [p2p.PeerTracker.ResponsivePeers].
func NewTracker(t *testing.T) *p2p.PeerTracker {
	t.Helper()

	tracker, err := p2p.NewPeerTracker(loggingtest.New(t, logging.Debug), "", prometheus.NewRegistry(), nil, nil)
	require.NoError(t, err, "p2p.NewPeerTracker()")
	return tracker
}

// SeedResponsive marks nodeID responsive so a later de-score is a transition
// rather than a no-op. nodeID must already be connected.
func SeedResponsive(t *testing.T, tracker *p2p.PeerTracker, nodeID ids.NodeID) {
	t.Helper()

	tracker.RegisterRequest(nodeID)
	tracker.RegisterResponse(nodeID, 1)
}
