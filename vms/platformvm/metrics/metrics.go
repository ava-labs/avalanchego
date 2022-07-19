// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/rpc/v2"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

type Metrics interface {
	// Mark that an option vote that we initially preferred was accepted.
	MarkOptionVoteWon()
	// Mark that an option vote that we initially preferred was rejected.
	MarkOptionVoteLost()
	// Mark that the given block was accepted.
	MarkAccepted(stateless.Block) error
	// Returns an interceptor function to use for API requests
	// to track API metrics.
	InterceptRequestFunc() func(*rpc.RequestInfo) *http.Request
	// Returns an AfterRequest function to use for API requests
	// to track API metrics.
	AfterRequestFunc() func(*rpc.RequestInfo)
	// Mark that a validator set was created.
	IncValidatorSetsCreated()
	// Mark that a validator set was cached.
	IncValidatorSetsCached()
	// Mark that we spent the given time computing validator diffs.
	AddValidatorSetsDuration(time.Duration)
	// Mark that we computed a validator diff at a height with the given
	// difference from the top.
	AddValidatorSetsHeightDiff(float64)
	// Mark that this much stake is staked on the node.
	SetLocalStake(float64)
	// Mark that this much stake is staked in the network.
	SetTotalStake(float64)
	// Mark that this node is connected to this
	// percent of the subnet's stake.
	SetSubnetPercentConnected(subnetIDStr string, percent float64)
	// Mark that this node is connected to this percent
	// of the Primary network's stake.
	SetPercentConnected(percent float64)
}

func New(
	namespace string,
	registerer prometheus.Registerer,
	whitelistedSubnets ids.Set,
) (Metrics, error) {
	m := &metrics{}
	txMetrics, err := newTxMetrics(namespace, registerer)
	m.txMetrics = txMetrics
	m.percentConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "percent_connected",
		Help:      "Percent of connected stake",
	})
	m.subnetPercentConnected = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "percent_connected_subnet",
			Help:      "Percent of connected subnet weight",
		},
		[]string{"subnetID"},
	)
	m.localStake = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "local_staked",
		Help:      "Total amount of AVAX on this node staked",
	})
	m.totalStake = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "total_staked",
		Help:      "Total amount of AVAX staked",
	})

	m.numAbortBlocks = newBlockMetrics(namespace, "abort")
	m.numAtomicBlocks = newBlockMetrics(namespace, "atomic")
	m.numCommitBlocks = newBlockMetrics(namespace, "commit")
	m.numProposalBlocks = newBlockMetrics(namespace, "proposal")
	m.numStandardBlocks = newBlockMetrics(namespace, "standard")

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

	m.validatorSetsCached = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "validator_sets_cached",
		Help:      "Total number of validator sets cached",
	})
	m.validatorSetsCreated = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "validator_sets_created",
		Help:      "Total number of validator sets created from applying difflayers",
	})
	m.validatorSetsHeightDiff = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "validator_sets_height_diff_sum",
		Help:      "Total number of validator sets diffs applied for generating validator sets",
	})
	m.validatorSetsDuration = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "validator_sets_duration_sum",
		Help:      "Total amount of time generating validator sets in nanoseconds",
	})

	errs := wrappers.Errs{Err: err}
	apiRequestMetrics, err := metric.NewAPIInterceptor(namespace, registerer)
	m.apiRequestMetrics = apiRequestMetrics
	errs.Add(
		err,

		registerer.Register(m.percentConnected),
		registerer.Register(m.subnetPercentConnected),
		registerer.Register(m.localStake),
		registerer.Register(m.totalStake),

		registerer.Register(m.numAbortBlocks),
		registerer.Register(m.numAtomicBlocks),
		registerer.Register(m.numCommitBlocks),
		registerer.Register(m.numProposalBlocks),
		registerer.Register(m.numStandardBlocks),

		registerer.Register(m.numVotesWon),
		registerer.Register(m.numVotesLost),

		registerer.Register(m.validatorSetsCreated),
		registerer.Register(m.validatorSetsCached),
		registerer.Register(m.validatorSetsHeightDiff),
		registerer.Register(m.validatorSetsDuration),
	)

	// init subnet tracker metrics with whitelisted subnets
	for subnetID := range whitelistedSubnets {
		// initialize to 0
		m.subnetPercentConnected.WithLabelValues(subnetID.String()).Set(0)
	}
	return m, errs.Err
}

type metrics struct {
	txMetrics *txMetrics

	percentConnected       prometheus.Gauge
	subnetPercentConnected *prometheus.GaugeVec
	localStake             prometheus.Gauge
	totalStake             prometheus.Gauge

	numAbortBlocks,
	numAtomicBlocks,
	numCommitBlocks,
	numProposalBlocks,
	numStandardBlocks prometheus.Counter

	numVotesWon, numVotesLost prometheus.Counter

	validatorSetsCached     prometheus.Counter
	validatorSetsCreated    prometheus.Counter
	validatorSetsHeightDiff prometheus.Gauge
	validatorSetsDuration   prometheus.Gauge

	apiRequestMetrics metric.APIInterceptor
}

func newBlockMetrics(namespace string, name string) prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_blks_accepted", name),
		Help:      fmt.Sprintf("Number of %s blocks accepted", name),
	})
}

func (m *metrics) MarkOptionVoteWon() {
	m.numVotesWon.Inc()
}

func (m *metrics) MarkOptionVoteLost() {
	m.numVotesLost.Inc()
}

// TODO: use a visitor here
func (m *metrics) MarkAccepted(b stateless.Block) error {
	switch b := b.(type) {
	case *stateless.AbortBlock:
		m.numAbortBlocks.Inc()
	case *stateless.AtomicBlock:
		m.numAtomicBlocks.Inc()
		return m.AcceptTx(b.Tx)
	case *stateless.CommitBlock:
		m.numCommitBlocks.Inc()
	case *stateless.ProposalBlock:
		m.numProposalBlocks.Inc()
		return m.AcceptTx(b.Tx)
	case *stateless.StandardBlock:
		m.numStandardBlocks.Inc()
		for _, tx := range b.Txs {
			if err := m.AcceptTx(tx); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("got unexpected block type %T", b)
	}
	return nil
}

func (m *metrics) InterceptRequestFunc() func(*rpc.RequestInfo) *http.Request {
	return m.apiRequestMetrics.InterceptRequest
}

func (m *metrics) AfterRequestFunc() func(*rpc.RequestInfo) {
	return m.apiRequestMetrics.AfterRequest
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

func (m *metrics) AddValidatorSetsHeightDiff(d float64) {
	m.validatorSetsHeightDiff.Add(d)
}

func (m *metrics) SetLocalStake(s float64) {
	m.localStake.Set(s)
}

func (m *metrics) SetTotalStake(s float64) {
	m.totalStake.Set(s)
}

func (m *metrics) SetSubnetPercentConnected(label string, percent float64) {
	m.subnetPercentConnected.WithLabelValues(label).Set(percent)
}

func (m *metrics) SetPercentConnected(percent float64) {
	m.percentConnected.Set(percent)
}

func (m *metrics) AcceptTx(tx *txs.Tx) error {
	return tx.Unsigned.Visit(m.txMetrics)
}

type noopMetrics struct{}

func NewNoopMetrics() Metrics {
	return &noopMetrics{}
}

func (m *noopMetrics) MarkOptionVoteWon() {}

func (m *noopMetrics) MarkOptionVoteLost() {}

func (m *noopMetrics) MarkAccepted(stateless.Block) error { return nil }

func (m *noopMetrics) InterceptRequestFunc() func(*rpc.RequestInfo) *http.Request {
	return func(*rpc.RequestInfo) *http.Request { return nil }
}

func (m *noopMetrics) AfterRequestFunc() func(*rpc.RequestInfo) {
	return func(ri *rpc.RequestInfo) {}
}

func (m *noopMetrics) IncValidatorSetsCreated() {}

func (m *noopMetrics) IncValidatorSetsCached() {}

func (m *noopMetrics) AddValidatorSetsDuration(time.Duration) {}

func (m *noopMetrics) AddValidatorSetsHeightDiff(float64) {}

func (m *noopMetrics) SetLocalStake(float64) {}

func (m *noopMetrics) SetTotalStake(float64) {}

func (m *noopMetrics) SetSubnetPercentConnected(label string, percent float64) {}

func (m *noopMetrics) SetPercentConnected(percent float64) {}
