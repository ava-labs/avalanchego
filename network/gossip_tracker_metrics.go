// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type gossipTrackerMetrics struct {
	localPeersSize     prometheus.Gauge
	peersToIndicesSize prometheus.Gauge
	indicesToPeersSize prometheus.Gauge
}

func newGossipTrackerMetrics(registerer prometheus.Registerer, namespace string) (gossipTrackerMetrics, error) {
	m := gossipTrackerMetrics{
		localPeersSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "local_peers_size",
				Help:      "amount of peers this node is tracking gossip for",
			},
		),
		peersToIndicesSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "peers_to_indices_size",
				Help:      "amount of peers this node is tracking in peersToIndices",
			},
		),
		indicesToPeersSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "indices_to_peers_size",
				Help:      "amount of peers this node is tracking in indicesToPeers",
			},
		),
	}

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.localPeersSize),
		registerer.Register(m.peersToIndicesSize),
		registerer.Register(m.indicesToPeersSize),
	)

	return m, errs.Err
}
