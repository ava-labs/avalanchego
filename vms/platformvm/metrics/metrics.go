// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
)

type Metrics struct {
	blockMetrics *blockMetrics

	PercentConnected       prometheus.Gauge
	SubnetPercentConnected *prometheus.GaugeVec
	LocalStake             prometheus.Gauge
	TotalStake             prometheus.Gauge

	numVotesWon, numVotesLost prometheus.Counter

	ValidatorSetsCached     prometheus.Counter
	ValidatorSetsCreated    prometheus.Counter
	ValidatorSetsHeightDiff prometheus.Gauge
	ValidatorSetsDuration   prometheus.Gauge

	APIRequestMetrics metric.APIInterceptor
}

// Initialize platformvm metrics
func (m *Metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
	whitelistedSubnets ids.Set,
) error {
	blockMetrics, err := newBlockMetrics(namespace, registerer)
	m.blockMetrics = blockMetrics

	m.PercentConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "percent_connected",
		Help:      "Percent of connected stake",
	})
	m.SubnetPercentConnected = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "percent_connected_subnet",
			Help:      "Percent of connected subnet weight",
		},
		[]string{"subnetID"},
	)
	m.LocalStake = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "local_staked",
		Help:      "Total amount of AVAX on this node staked",
	})
	m.TotalStake = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "total_staked",
		Help:      "Total amount of AVAX staked",
	})

	m.numVotesWon = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "votes_won",
		Help:      "Total number of votes this node has won",
	})
	m.numVotesLost = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "votes_lost",
		Help:      "Total number of votes this node has lost",
	})

	m.ValidatorSetsCached = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "validator_sets_cached",
		Help:      "Total number of validator sets cached",
	})
	m.ValidatorSetsCreated = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "validator_sets_created",
		Help:      "Total number of validator sets created from applying difflayers",
	})
	m.ValidatorSetsHeightDiff = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "validator_sets_height_diff_sum",
		Help:      "Total number of validator sets diffs applied for generating validator sets",
	})
	m.ValidatorSetsDuration = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "validator_sets_duration_sum",
		Help:      "Total amount of time generating validator sets in nanoseconds",
	})

	errs := wrappers.Errs{Err: err}
	apiRequestMetrics, err := metric.NewAPIInterceptor(namespace, registerer)
	m.APIRequestMetrics = apiRequestMetrics
	errs.Add(
		err,

		registerer.Register(m.PercentConnected),
		registerer.Register(m.SubnetPercentConnected),
		registerer.Register(m.LocalStake),
		registerer.Register(m.TotalStake),

		registerer.Register(m.numVotesWon),
		registerer.Register(m.numVotesLost),

		registerer.Register(m.ValidatorSetsCreated),
		registerer.Register(m.ValidatorSetsCached),
		registerer.Register(m.ValidatorSetsHeightDiff),
		registerer.Register(m.ValidatorSetsDuration),
	)

	// init subnet tracker metrics with whitelisted subnets
	for subnetID := range whitelistedSubnets {
		// initialize to 0
		m.SubnetPercentConnected.WithLabelValues(subnetID.String()).Set(0)
	}
	return errs.Err
}

func (m *Metrics) MarkVoteWon() {
	m.numVotesWon.Inc()
}

func (m *Metrics) MarkVoteLost() {
	m.numVotesLost.Inc()
}

func (m *Metrics) MarkAccepted(b blocks.Block) error {
	return b.Visit(m.blockMetrics)
}
