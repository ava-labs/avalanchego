// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

const (
	ResourceLabel   = "resource"
	GasLabel        = "gas"
	ValidatorsLabel = "validators"
)

var (
	gasLabels = prometheus.Labels{
		ResourceLabel: GasLabel,
	}
	validatorsLabels = prometheus.Labels{
		ResourceLabel: ValidatorsLabel,
	}
)

var _ Metrics = (*metrics)(nil)

type Block struct {
	Block block.Block

	GasConsumed gas.Gas
	GasState    gas.State
	GasPrice    gas.Price

	ActiveL1Validators   int
	ValidatorExcess      gas.Gas
	ValidatorPrice       gas.Price
	AccruedValidatorFees uint64
}

type Metrics interface {
	metric.APIInterceptor

	// Mark that the given block was accepted.
	MarkAccepted(Block) error

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

		gasConsumed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "gas_consumed",
			Help: "Cumulative amount of gas consumed by transactions",
		}),
		gasCapacity: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gas_capacity",
			Help: "Minimum amount of gas that can be consumed in the next block",
		}),
		activeL1Validators: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "active_l1_validators",
			Help: "Number of active L1 validators",
		}),
		excess: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "excess",
				Help: "Excess usage of a resource over the target usage",
			},
			[]string{ResourceLabel},
		),
		price: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "price",
				Help: "Price (in nAVAX) of a resource",
			},
			[]string{ResourceLabel},
		),
		accruedValidatorFees: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "accrued_validator_fees",
			Help: "The total cost of running an active L1 validator since Etna activation",
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

		registerer.Register(m.gasConsumed),
		registerer.Register(m.gasCapacity),
		registerer.Register(m.activeL1Validators),
		registerer.Register(m.excess),
		registerer.Register(m.price),
		registerer.Register(m.accruedValidatorFees),

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

	// Staking metrics
	timeUntilUnstake       prometheus.Gauge
	timeUntilSubnetUnstake *prometheus.GaugeVec
	localStake             prometheus.Gauge
	totalStake             prometheus.Gauge

	gasConsumed          prometheus.Counter
	gasCapacity          prometheus.Gauge
	activeL1Validators   prometheus.Gauge
	excess               *prometheus.GaugeVec
	price                *prometheus.GaugeVec
	accruedValidatorFees prometheus.Gauge

	// Validator set diff metrics
	validatorSetsCached     prometheus.Counter
	validatorSetsCreated    prometheus.Counter
	validatorSetsHeightDiff prometheus.Gauge
	validatorSetsDuration   prometheus.Gauge
}

func (m *metrics) MarkAccepted(b Block) error {
	m.gasConsumed.Add(float64(b.GasConsumed))
	m.gasCapacity.Set(float64(b.GasState.Capacity))
	m.excess.With(gasLabels).Set(float64(b.GasState.Excess))
	m.price.With(gasLabels).Set(float64(b.GasPrice))

	m.activeL1Validators.Set(float64(b.ActiveL1Validators))
	m.excess.With(validatorsLabels).Set(float64(b.ValidatorExcess))
	m.price.With(validatorsLabels).Set(float64(b.ValidatorPrice))
	m.accruedValidatorFees.Set(float64(b.AccruedValidatorFees))

	return b.Block.Visit(m.blockMetrics)
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
