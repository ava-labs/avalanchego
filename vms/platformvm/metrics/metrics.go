// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ stateless.Metrics = &Metrics{}

	errUnknownBlockType = errors.New("unknown block type")
)

type Metrics struct {
	txMetrics *txMetrics

	PercentConnected       prometheus.Gauge
	SubnetPercentConnected *prometheus.GaugeVec
	LocalStake             prometheus.Gauge
	TotalStake             prometheus.Gauge

	numAbortBlocks,
	numAtomicBlocks,
	numCommitBlocks,
	numProposalBlocks,
	numStandardBlocks prometheus.Counter

	numVotesWon, numVotesLost prometheus.Counter

	ValidatorSetsCached     prometheus.Counter
	ValidatorSetsCreated    prometheus.Counter
	ValidatorSetsHeightDiff prometheus.Gauge
	ValidatorSetsDuration   prometheus.Gauge

	APIRequestMetrics metric.APIInterceptor
}

func NewMetrics(
	namespace string,
	registerer prometheus.Registerer,
	whitelistedSubnets ids.Set,
) (*Metrics, error) {
	res := &Metrics{}

	txMetrics, err := newTxMetrics(namespace, registerer)
	res.txMetrics = txMetrics
	errs := wrappers.Errs{Err: err}

	res.PercentConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "percent_connected",
		Help:      "Percent of connected stake",
	})
	res.SubnetPercentConnected = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "percent_connected_subnet",
			Help:      "Percent of connected subnet weight",
		},
		[]string{"subnetID"},
	)
	res.LocalStake = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "local_staked",
		Help:      "Total amount of AVAX on this node staked",
	})
	res.TotalStake = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "total_staked",
		Help:      "Total amount of AVAX staked",
	})

	res.numAbortBlocks = newBlockMetrics(namespace, "abort")
	res.numAtomicBlocks = newBlockMetrics(namespace, "atomic")
	res.numCommitBlocks = newBlockMetrics(namespace, "commit")
	res.numProposalBlocks = newBlockMetrics(namespace, "proposal")
	res.numStandardBlocks = newBlockMetrics(namespace, "standard")

	res.numVotesWon = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "votes_won",
		Help:      "Total number of votes this node has won",
	})
	res.numVotesLost = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "votes_lost",
		Help:      "Total number of votes this node has lost",
	})

	res.ValidatorSetsCached = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "validator_sets_cached",
		Help:      "Total number of validator sets cached",
	})
	res.ValidatorSetsCreated = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "validator_sets_created",
		Help:      "Total number of validator sets created from applying difflayers",
	})
	res.ValidatorSetsHeightDiff = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "validator_sets_height_diff_sum",
		Help:      "Total number of validator sets diffs applied for generating validator sets",
	})
	res.ValidatorSetsDuration = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "validator_sets_duration_sum",
		Help:      "Total amount of time generating validator sets in nanoseconds",
	})

	apiRequestMetrics, err := metric.NewAPIInterceptor(namespace, registerer)
	res.APIRequestMetrics = apiRequestMetrics
	errs.Add(
		err,

		registerer.Register(res.PercentConnected),
		registerer.Register(res.SubnetPercentConnected),
		registerer.Register(res.LocalStake),
		registerer.Register(res.TotalStake),

		registerer.Register(res.numAbortBlocks),
		registerer.Register(res.numAtomicBlocks),
		registerer.Register(res.numCommitBlocks),
		registerer.Register(res.numProposalBlocks),
		registerer.Register(res.numStandardBlocks),

		registerer.Register(res.numVotesWon),
		registerer.Register(res.numVotesLost),

		registerer.Register(res.ValidatorSetsCreated),
		registerer.Register(res.ValidatorSetsCached),
		registerer.Register(res.ValidatorSetsHeightDiff),
		registerer.Register(res.ValidatorSetsDuration),
	)

	// init subnet tracker metrics with whitelisted subnets
	for subnetID := range whitelistedSubnets {
		// initialize to 0
		res.SubnetPercentConnected.WithLabelValues(subnetID.String()).Set(0)
	}
	return res, errs.Err
}

func newBlockMetrics(namespace string, name string) prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_blks_accepted", name),
		Help:      fmt.Sprintf("Number of %s blocks accepted", name),
	})
}

func (m *Metrics) MarkAcceptedOptionVote() { m.numVotesWon.Inc() }
func (m *Metrics) MarkRejectedOptionVote() { m.numVotesLost.Inc() }

func (m *Metrics) MarkAccepted(b stateless.CommonBlockIntf) error {
	switch b := b.(type) {
	case stateless.AtomicBlockIntf:
		m.numAtomicBlocks.Inc()
		return m.AcceptTx(b.AtomicTx())

	case stateless.ProposalBlockIntf:
		m.numProposalBlocks.Inc()
		return m.AcceptTx(b.ProposalTx())

	case stateless.StandardBlockIntf:
		m.numStandardBlocks.Inc()
		for _, tx := range b.DecisionTxs() {
			if err := m.AcceptTx(tx); err != nil {
				return err
			}
		}
		return nil

	case stateless.OptionBlock:
		switch b.(type) {
		case *stateless.AbortBlock:
			m.numAbortBlocks.Inc()
			return nil
		case *stateless.CommitBlock:
			m.numCommitBlocks.Inc()
			return nil
		default:
			return errUnknownBlockType
		}

	default:
		return errUnknownBlockType
	}
}

func (m *Metrics) AcceptTx(tx *txs.Tx) error {
	return tx.Unsigned.Visit(m.txMetrics)
}
