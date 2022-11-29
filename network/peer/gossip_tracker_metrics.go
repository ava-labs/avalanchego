// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type gossipTrackerMetrics struct {
	trackedPeersSize        prometheus.Gauge
	validatorsToIndicesSize prometheus.Gauge
	validatorIndices        prometheus.Gauge
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
		validatorsToIndicesSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "validators_to_indices_size",
				Help:      "amount of validators this node is tracking in validatorsToIndices",
			},
		),
		validatorIndices: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "validator_indices_size",
				Help:      "amount of validators this node is tracking in validatorIndices",
			},
		),
	}

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.trackedPeersSize),
		registerer.Register(m.validatorsToIndicesSize),
		registerer.Register(m.validatorIndices),
	)

	return m, errs.Err
}
