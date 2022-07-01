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
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

var (
	_ stateless.Metrics = &Metrics{}

	errUnknownBlockType = errors.New("unknown block type")
)

type Metrics struct {
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

	numAddDelegatorTxs,
	numAddSubnetValidatorTxs,
	numAddValidatorTxs,
	numAdvanceTimeTxs,
	numCreateChainTxs,
	numCreateSubnetTxs,
	numExportTxs,
	numImportTxs,
	numRewardValidatorTxs prometheus.Counter

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

func newTxMetrics(namespace string, name string) prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_txs_accepted", name),
		Help:      fmt.Sprintf("Number of %s transactions accepted", name),
	})
}

// Initialize platformvm metrics
func (m *Metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
	whitelistedSubnets ids.Set,
) error {
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

	m.numAddDelegatorTxs = newTxMetrics(namespace, "add_delegator")
	m.numAddSubnetValidatorTxs = newTxMetrics(namespace, "add_subnet_validator")
	m.numAddValidatorTxs = newTxMetrics(namespace, "add_validator")
	m.numAdvanceTimeTxs = newTxMetrics(namespace, "advance_time")
	m.numCreateChainTxs = newTxMetrics(namespace, "create_chain")
	m.numCreateSubnetTxs = newTxMetrics(namespace, "create_subnet")
	m.numExportTxs = newTxMetrics(namespace, "export")
	m.numImportTxs = newTxMetrics(namespace, "import")
	m.numRewardValidatorTxs = newTxMetrics(namespace, "reward_validator")

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

	apiRequestMetrics, err := metric.NewAPIInterceptor(namespace, registerer)
	m.APIRequestMetrics = apiRequestMetrics
	errs := wrappers.Errs{}
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

		registerer.Register(m.numAddDelegatorTxs),
		registerer.Register(m.numAddSubnetValidatorTxs),
		registerer.Register(m.numAddValidatorTxs),
		registerer.Register(m.numAdvanceTimeTxs),
		registerer.Register(m.numCreateChainTxs),
		registerer.Register(m.numCreateSubnetTxs),
		registerer.Register(m.numExportTxs),
		registerer.Register(m.numImportTxs),
		registerer.Register(m.numRewardValidatorTxs),

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
		return m.acceptTx(b.Tx)
	case *stateless.CommitBlock:
		m.numCommitBlocks.Inc()
	case *stateless.ProposalBlock:
		m.numProposalBlocks.Inc()
		return m.acceptTx(b.Tx)
	case *stateless.StandardBlock:
		m.numStandardBlocks.Inc()
		for _, tx := range b.Txs {
			if err := m.acceptTx(tx); err != nil {
				return err
			}
		}
	default:
		return errUnknownBlockType
	}
	return nil
}

func (m *Metrics) acceptTx(tx *txs.Tx) error {
	switch tx.Unsigned.(type) {
	case *txs.AddDelegatorTx:
		m.numAddDelegatorTxs.Inc()
	case *txs.AddSubnetValidatorTx:
		m.numAddSubnetValidatorTxs.Inc()
	case *txs.AddValidatorTx:
		m.numAddValidatorTxs.Inc()
	case *txs.AdvanceTimeTx:
		m.numAdvanceTimeTxs.Inc()
	case *txs.CreateChainTx:
		m.numCreateChainTxs.Inc()
	case *txs.CreateSubnetTx:
		m.numCreateSubnetTxs.Inc()
	case *txs.ImportTx:
		m.numImportTxs.Inc()
	case *txs.ExportTx:
		m.numExportTxs.Inc()
	case *txs.RewardValidatorTx:
		m.numRewardValidatorTxs.Inc()
	default:
		return fmt.Errorf("%w: %T", mempool.ErrUnknownTxType, tx.Unsigned)
	}
	return nil
}
