// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
)

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

// SeedResponsive marks nodeID responsive so that a later de-score shows up as a
// transition rather than a no-op. nodeID must already be connected, since
// [p2p.PeerTracker.RegisterRequest] ignores peers the tracker does not know.
func SeedResponsive(t *testing.T, tracker *p2p.PeerTracker, nodeID ids.NodeID) {
	t.Helper()

	tracker.RegisterRequest(nodeID)
	tracker.RegisterResponse(nodeID, 1)
}
