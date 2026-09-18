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

// NewTrackerWithRegistry returns an empty [p2p.PeerTracker] publishing under
// namespace to registerer. Read its gauges back with [TrackerGauge].
func NewTrackerWithRegistry(t *testing.T, namespace string, registerer prometheus.Registerer) *p2p.PeerTracker {
	t.Helper()

	tracker, err := p2p.NewPeerTracker(loggingtest.New(t, logging.Debug), namespace, registerer, nil, nil)
	require.NoError(t, err, "p2p.NewPeerTracker()")
	return tracker
}

// NewTracker returns an empty [p2p.PeerTracker] whose metrics nothing reads,
// for a test that needs a [p2p.TrackingClient] but does not assert on scoring.
func NewTracker(t *testing.T) *p2p.PeerTracker {
	t.Helper()

	return NewTrackerWithRegistry(t, "", prometheus.NewRegistry())
}

// TrackerGauge returns the value a [p2p.PeerTracker] published for one of its
// gauges, such as "num_responsive_peers", under the namespace it was built with.
func TrackerGauge(t *testing.T, gatherer prometheus.Gatherer, namespace, name string) float64 {
	t.Helper()

	if namespace != "" {
		name = namespace + "_" + name
	}
	mfs, err := gatherer.Gather()
	require.NoError(t, err, "prometheus.Gatherer.Gather()")

	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if m.Gauge != nil {
				return m.Gauge.GetValue()
			}
		}
	}

	t.Fatalf("gauge %q not found", name)
	return 0
}

// SeedResponsive marks nodeID responsive so a later de-score is a transition
// rather than a no-op. nodeID must already be connected.
func SeedResponsive(t *testing.T, tracker *p2p.PeerTracker, nodeID ids.NodeID) {
	t.Helper()

	tracker.RegisterRequest(nodeID)
	tracker.RegisterResponse(nodeID, 1)
}
