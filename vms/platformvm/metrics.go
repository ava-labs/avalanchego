// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/metricutils"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

var errUnknownBlockType = errors.New("unknown block type")

type metrics struct {
	percentConnected prometheus.Gauge
	totalStake       prometheus.Gauge

	numAbortBlocks,
	numAtomicBlocks,
	numCommitBlocks,
	numProposalBlocks,
	numStandardBlocks prometheus.Counter

	numAddDelegatorTxs,
	numAddSubnetValidatorTxs,
	numAddValidatorTxs,
	numAdvanceTimeTxs,
	numCreateChainTxs,
	numCreateSubnetTxs,
	numExportTxs,
	numImportTxs,
	numRewardValidatorTxs prometheus.Counter

	apiRequestMetrics metricutils.APIRequestMetrics
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
func (m *metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.percentConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "percent_connected",
		Help:      "Percent of connected stake",
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

	m.numAddDelegatorTxs = newTxMetrics(namespace, "add_delegator")
	m.numAddSubnetValidatorTxs = newTxMetrics(namespace, "add_subnet_validator")
	m.numAddValidatorTxs = newTxMetrics(namespace, "add_validator")
	m.numAdvanceTimeTxs = newTxMetrics(namespace, "advance_time")
	m.numCreateChainTxs = newTxMetrics(namespace, "create_chain")
	m.numCreateSubnetTxs = newTxMetrics(namespace, "create_subnet")
	m.numExportTxs = newTxMetrics(namespace, "export")
	m.numImportTxs = newTxMetrics(namespace, "import")
	m.numRewardValidatorTxs = newTxMetrics(namespace, "reward_validator")

	m.apiRequestMetrics = metricutils.NewAPIMetrics(namespace)

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.percentConnected),
		registerer.Register(m.totalStake),

		registerer.Register(m.numAbortBlocks),
		registerer.Register(m.numAtomicBlocks),
		registerer.Register(m.numCommitBlocks),
		registerer.Register(m.numProposalBlocks),
		registerer.Register(m.numStandardBlocks),

		registerer.Register(m.numAddDelegatorTxs),
		registerer.Register(m.numAddSubnetValidatorTxs),
		registerer.Register(m.numAddValidatorTxs),
		registerer.Register(m.numAdvanceTimeTxs),
		registerer.Register(m.numCreateChainTxs),
		registerer.Register(m.numCreateSubnetTxs),
		registerer.Register(m.numExportTxs),
		registerer.Register(m.numImportTxs),
		registerer.Register(m.numRewardValidatorTxs),

		m.apiRequestMetrics.Register(registerer),
	)
	return errs.Err
}

func (m *metrics) AcceptBlock(b snowman.Block) error {
	switch b := b.(type) {
	case *AbortBlock:
		m.numAbortBlocks.Inc()
	case *AtomicBlock:
		m.numAtomicBlocks.Inc()
		return m.AcceptTx(&b.Tx)
	case *CommitBlock:
		m.numCommitBlocks.Inc()
	case *ProposalBlock:
		m.numProposalBlocks.Inc()
		return m.AcceptTx(&b.Tx)
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

func (m *metrics) AcceptTx(tx *Tx) error {
	switch tx.UnsignedTx.(type) {
	case *UnsignedAddDelegatorTx:
		m.numAddDelegatorTxs.Inc()
	case *UnsignedAddSubnetValidatorTx:
		m.numAddSubnetValidatorTxs.Inc()
	case *UnsignedAddValidatorTx:
		m.numAddValidatorTxs.Inc()
	case *UnsignedAdvanceTimeTx:
		m.numAdvanceTimeTxs.Inc()
	case *UnsignedCreateChainTx:
		m.numCreateChainTxs.Inc()
	case *UnsignedCreateSubnetTx:
		m.numCreateSubnetTxs.Inc()
	case *UnsignedImportTx:
		m.numImportTxs.Inc()
	case *UnsignedExportTx:
		m.numExportTxs.Inc()
	case *UnsignedRewardValidatorTx:
		m.numRewardValidatorTxs.Inc()
	default:
		return errUnknownTxType
	}
	return nil
}
