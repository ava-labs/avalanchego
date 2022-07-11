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

	ErrUnknownBlockType = errors.New("unknown block type")
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

func newBlockMetrics(namespace string, name string) prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_blks_accepted", name),
		Help:      fmt.Sprintf("Number of %s blocks accepted", name),
	})
}

// Initialize platformvm metrics
func (m *Metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
	whitelistedSubnets ids.Set,
) error {
	txMetrics, err := newTxMetrics(namespace, registerer)
	m.txMetrics = txMetrics
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

		registerer.Register(m.numAbortBlocks),
		registerer.Register(m.numAtomicBlocks),
		registerer.Register(m.numCommitBlocks),
		registerer.Register(m.numProposalBlocks),
		registerer.Register(m.numStandardBlocks),

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

func (m *Metrics) MarkAcceptedOptionVote() { m.numVotesWon.Inc() }
func (m *Metrics) MarkRejectedOptionVote() { m.numVotesLost.Inc() }

func (m *Metrics) MarkAccepted(b stateless.Block) error {
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
		return ErrUnknownBlockType
	}
	return nil
}

func (m *Metrics) AcceptTx(tx *txs.Tx) error {
	return tx.Unsigned.Visit(m.txMetrics)
}
