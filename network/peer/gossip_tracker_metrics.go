// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type gossipTrackerMetrics struct {
	validators              prometheus.Gauge
	txsToValidatorsSize     prometheus.Gauge
	validatorsToIndicesSize prometheus.Gauge
	trackedPeersSize        prometheus.Gauge
}

func newGossipTrackerMetrics(registerer prometheus.Registerer, namespace string) (gossipTrackerMetrics, error) {
	m := gossipTrackerMetrics{
		validators: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "validators__size",
				Help:      "amount of validators this node is tracking in validators",
			},
		),
		validatorsToIndicesSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "validators_to_indices_size",
				Help:      "amount of validators this node is tracking in validatorsToIndices",
			},
		),
		txsToValidatorsSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "txs_to_validators_size",
				Help:      "amount of txs this node is tracking in txsToValidators",
			},
		),
		trackedPeersSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "tracked_peers_size",
				Help:      "amount of peers that are being tracked",
			},
		),
	}

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.validators),
		registerer.Register(m.validatorsToIndicesSize),
		registerer.Register(m.txsToValidatorsSize),
		registerer.Register(m.trackedPeersSize),
	)

	return m, errs.Err
}
