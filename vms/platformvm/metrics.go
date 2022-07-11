// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var errUnknownBlockType = errors.New("unknown block type")

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

// Initialize platformvm metrics
func (m *metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
	whitelistedSubnets ids.Set,
) error {
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
	return errs.Err
}

// TODO: use a visitor here
func (m *metrics) AcceptBlock(b Block) error {
	switch b := b.(type) {
	case *AbortBlock:
		m.numAbortBlocks.Inc()
	case *AtomicBlock:
		m.numAtomicBlocks.Inc()
		return m.AcceptTx(b.Tx)
	case *CommitBlock:
		m.numCommitBlocks.Inc()
	case *ProposalBlock:
		m.numProposalBlocks.Inc()
		return m.AcceptTx(b.Tx)
	case *StandardBlock:
		m.numStandardBlocks.Inc()
		for _, tx := range b.Txs {
			if err := m.AcceptTx(tx); err != nil {
				return err
			}
		}
	default:
		return errUnknownBlockType
	}
	return nil
}

func (m *metrics) AcceptTx(tx *txs.Tx) error {
	return tx.Unsigned.Visit(m.txMetrics)
}
