// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

var _ Metrics = (*metrics)(nil)

type Metrics interface {
	metric.APIInterceptor

	// Mark that the given block was accepted.
	MarkAccepted(block.Block) error
	// Mark that a validator set was created.
	IncValidatorSetsCreated()
	// Mark that a validator set was cached.
	IncValidatorSetsCached()
	// Mark that we spent the given time computing validator diffs.
	AddValidatorSetsDuration(time.Duration)
	// Mark that we computed a validator diff at a height with the given
	// difference from the top.
	AddValidatorSetsHeightDiff(uint64)
	// Mark that this much stake is staked on the node.
	SetLocalStake(uint64)
	// Mark that this much stake is staked in the network.
	SetTotalStake(uint64)
	// Mark when this node will unstake from the Primary Network.
	SetTimeUntilUnstake(time.Duration)
	// Mark when this node will unstake from a subnet.
	SetTimeUntilSubnetUnstake(subnetID ids.ID, timeUntilUnstake time.Duration)
}

func New(registerer prometheus.Registerer) (Metrics, error) {
	blockMetrics, err := newBlockMetrics(registerer)
	m := &metrics{
		blockMetrics: blockMetrics,
		timeUntilUnstake: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "time_until_unstake",
			Help: "Time (in ns) until this node leaves the Primary Network's validator set",
		}),
		timeUntilSubnetUnstake: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "time_until_unstake_subnet",
				Help: "Time (in ns) until this node leaves the subnet's validator set",
			},
			[]string{"subnetID"},
		),
		localStake: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "local_staked",
			Help: "Amount (in nAVAX) of AVAX staked on this node",
		}),
		totalStake: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "total_staked",
			Help: "Amount (in nAVAX) of AVAX staked on the Primary Network",
		}),

		validatorSetsCached: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "validator_sets_cached",
			Help: "Total number of validator sets cached",
		}),
		validatorSetsCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "validator_sets_created",
			Help: "Total number of validator sets created from applying difflayers",
		}),
		validatorSetsHeightDiff: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "validator_sets_height_diff_sum",
			Help: "Total number of validator sets diffs applied for generating validator sets",
		}),
		validatorSetsDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "validator_sets_duration_sum",
			Help: "Total amount of time generating validator sets in nanoseconds",
		}),
	}

	errs := wrappers.Errs{Err: err}
	apiRequestMetrics, err := metric.NewAPIInterceptor(registerer)
	errs.Add(err)
	m.APIInterceptor = apiRequestMetrics
	errs.Add(
		registerer.Register(m.timeUntilUnstake),
		registerer.Register(m.timeUntilSubnetUnstake),
		registerer.Register(m.localStake),
		registerer.Register(m.totalStake),

		registerer.Register(m.validatorSetsCreated),
		registerer.Register(m.validatorSetsCached),
		registerer.Register(m.validatorSetsHeightDiff),
		registerer.Register(m.validatorSetsDuration),
	)

	return m, errs.Err
}

type metrics struct {
	metric.APIInterceptor

	blockMetrics *blockMetrics

	timeUntilUnstake       prometheus.Gauge
	timeUntilSubnetUnstake *prometheus.GaugeVec
	localStake             prometheus.Gauge
	totalStake             prometheus.Gauge

	validatorSetsCached     prometheus.Counter
	validatorSetsCreated    prometheus.Counter
	validatorSetsHeightDiff prometheus.Gauge
	validatorSetsDuration   prometheus.Gauge
}

func (m *metrics) MarkAccepted(b block.Block) error {
	return b.Visit(m.blockMetrics)
}

func (m *metrics) IncValidatorSetsCreated() {
	m.validatorSetsCreated.Inc()
}

func (m *metrics) IncValidatorSetsCached() {
	m.validatorSetsCached.Inc()
}

func (m *metrics) AddValidatorSetsDuration(d time.Duration) {
	m.validatorSetsDuration.Add(float64(d))
}

func (m *metrics) AddValidatorSetsHeightDiff(d uint64) {
	m.validatorSetsHeightDiff.Add(float64(d))
}

func (m *metrics) SetLocalStake(s uint64) {
	m.localStake.Set(float64(s))
}

func (m *metrics) SetTotalStake(s uint64) {
	m.totalStake.Set(float64(s))
}

func (m *metrics) SetTimeUntilUnstake(timeUntilUnstake time.Duration) {
	m.timeUntilUnstake.Set(float64(timeUntilUnstake))
}

func (m *metrics) SetTimeUntilSubnetUnstake(subnetID ids.ID, timeUntilUnstake time.Duration) {
	m.timeUntilSubnetUnstake.WithLabelValues(subnetID.String()).Set(float64(timeUntilUnstake))
}
