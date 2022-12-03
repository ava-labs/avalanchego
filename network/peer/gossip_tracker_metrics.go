// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type gossipTrackerMetrics struct {
	trackedPeersSize prometheus.Gauge
	validatorsSize   prometheus.Gauge
}

func newGossipTrackerMetrics(registerer prometheus.Registerer, namespace string) (gossipTrackerMetrics, error) {
	m := gossipTrackerMetrics{
		trackedPeersSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "tracked_peers_size",
				Help:      "amount of peers that are being tracked",
			},
		),
		validatorsSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "validators_size",
				Help:      "number of validators this node is tracking",
			},
		),
	}

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.trackedPeersSize),
		registerer.Register(m.validatorsSize),
	)

	return m, errs.Err
}
