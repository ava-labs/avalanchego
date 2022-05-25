// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/timed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/txheap"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

const (
	// droppedTxIDsCacheSize is the maximum number of dropped txIDs to cache
	droppedTxIDsCacheSize = 50

	initialConsumedUTXOsSize = 512

	// maxMempoolSize is the maximum number of bytes allowed in the mempool
	maxMempoolSize = 64 * units.MiB
)

var (
	errUnknownTxType = errors.New("unknown transaction type")
	errDuplicatedTx  = errors.New("duplicated transaction")
	errConflictingTx = errors.New("conflicting transaction")
	errMempoolFull   = errors.New("mempool is full")

	_ Mempool = &mempool{}
)

type Mempool interface {
	Add(tx *signed.Tx) error
	Has(txID ids.ID) bool
	Get(txID ids.ID) *signed.Tx

	AddDecisionTx(tx *signed.Tx)
	AddProposalTx(tx *signed.Tx)

	HasDecisionTxs() bool
	HasProposalTx() bool

	RemoveDecisionTxs(txs []*signed.Tx)
	RemoveProposalTx(tx *signed.Tx)

	PopDecisionTxs(maxTxsBytes int) []*signed.Tx
	PopProposalTx() *signed.Tx

	MarkDropped(txID ids.ID)
	WasDropped(txID ids.ID) bool
}

// Transactions from clients that have not yet been put into blocks and added to
// consensus
type mempool struct {
	bytesAvailableMetric prometheus.Gauge
	bytesAvailable       int

	unissuedDecisionTxs txheap.Heap
	unissuedProposalTxs txheap.Heap
	unknownTxs          prometheus.Counter

	droppedTxIDs *cache.LRU

	consumedUTXOs ids.Set
}

func NewMempool(namespace string, registerer prometheus.Registerer) (Mempool, error) {
	bytesAvailableMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "bytes_available",
		Help:      "Number of bytes of space currently available in the mempool",
	})
	if err := registerer.Register(bytesAvailableMetric); err != nil {
		return nil, err
	}

	unissuedDecisionTxs, err := txheap.NewTxHeapWithMetrics(
		txheap.NewTxHeapByAge(),
		fmt.Sprintf("%s_decision_txs", namespace),
		registerer,
	)
	if err != nil {
		return nil, err
	}

	unissuedProposalTxs, err := txheap.NewTxHeapWithMetrics(
		txheap.NewTxHeapByStartTime(),
		fmt.Sprintf("%s_proposal_txs", namespace),
		registerer,
	)
	if err != nil {
		return nil, err
	}

	unknownTxs := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "unknown_txs_count",
		Help:      "Number of unknown tx types seen by the mempool",
	})
	if err := registerer.Register(unknownTxs); err != nil {
		return nil, err
	}

	bytesAvailableMetric.Set(maxMempoolSize)
	return &mempool{
		bytesAvailableMetric: bytesAvailableMetric,
		bytesAvailable:       maxMempoolSize,
		unissuedDecisionTxs:  unissuedDecisionTxs,
		unissuedProposalTxs:  unissuedProposalTxs,
		unknownTxs:           unknownTxs,
		droppedTxIDs:         &cache.LRU{Size: droppedTxIDsCacheSize},
		consumedUTXOs:        ids.NewSet(initialConsumedUTXOsSize),
	}, nil
}

func (m *mempool) Add(tx *signed.Tx) error {
	// Note: a previously dropped tx can be re-added
	txID := tx.ID()
	if m.Has(txID) {
		return errDuplicatedTx
	}
	if txBytes := tx.Bytes(); len(txBytes) > m.bytesAvailable {
		return errMempoolFull
	}

	inputs := tx.Unsigned.InputIDs()
	if m.consumedUTXOs.Overlaps(inputs) {
		return errConflictingTx
	}

	switch tx.Unsigned.(type) {
	case timed.Tx:
		m.AddProposalTx(tx)
	case *unsigned.CreateChainTx,
		*unsigned.CreateSubnetTx,
		*unsigned.ImportTx,
		*unsigned.ExportTx:
		m.AddDecisionTx(tx)
	default:
		m.unknownTxs.Inc()
		return fmt.Errorf("%w: %T", errUnknownTxType, tx.Unsigned)
	}

	// Mark these UTXOs as consumed in the mempool
	m.consumedUTXOs.Union(inputs)

	// ensure that a mempool tx is either dropped or available (not both)
	m.droppedTxIDs.Evict(txID)
	return nil
}

func (m *mempool) Has(txID ids.ID) bool {
	return m.Get(txID) != nil
}

func (m *mempool) Get(txID ids.ID) *signed.Tx {
	if tx := m.unissuedDecisionTxs.Get(txID); tx != nil {
		return tx
	}
	return m.unissuedProposalTxs.Get(txID)
}

func (m *mempool) AddDecisionTx(tx *signed.Tx) {
	m.unissuedDecisionTxs.Add(tx)
	m.register(tx)
}

func (m *mempool) AddProposalTx(tx *signed.Tx) {
	m.unissuedProposalTxs.Add(tx)
	m.register(tx)
}

func (m *mempool) HasDecisionTxs() bool { return m.unissuedDecisionTxs.Len() > 0 }

func (m *mempool) HasProposalTx() bool { return m.unissuedProposalTxs.Len() > 0 }

func (m *mempool) RemoveDecisionTxs(txs []*signed.Tx) {
	for _, tx := range txs {
		txID := tx.ID()
		if m.unissuedDecisionTxs.Remove(txID) != nil {
			m.deregister(tx)
		}
	}
}

func (m *mempool) RemoveProposalTx(tx *signed.Tx) {
	txID := tx.ID()
	if m.unissuedProposalTxs.Remove(txID) != nil {
		m.deregister(tx)
	}
}

func (m *mempool) PopDecisionTxs(maxTxsBytes int) []*signed.Tx {
	var txs []*signed.Tx
	for m.unissuedDecisionTxs.Len() > 0 {
		tx := m.unissuedDecisionTxs.Peek()
		txBytes := tx.Bytes()
		if len(txBytes) > maxTxsBytes {
			return txs
		}
		maxTxsBytes -= len(txBytes)

		m.unissuedDecisionTxs.RemoveTop()
		m.deregister(tx)
		txs = append(txs, tx)
	}
	return txs
}

func (m *mempool) PopProposalTx() *signed.Tx {
	tx := m.unissuedProposalTxs.RemoveTop()
	m.deregister(tx)
	return tx
}

func (m *mempool) MarkDropped(txID ids.ID) {
	m.droppedTxIDs.Put(txID, struct{}{})
}

func (m *mempool) WasDropped(txID ids.ID) bool {
	_, exist := m.droppedTxIDs.Get(txID)
	return exist
}

func (m *mempool) register(tx *signed.Tx) {
	txBytes := tx.Bytes()
	m.bytesAvailable -= len(txBytes)
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))
}

func (m *mempool) deregister(tx *signed.Tx) {
	txBytes := tx.Bytes()
	m.bytesAvailable += len(txBytes)
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

	inputs := tx.Unsigned.InputIDs()
	m.consumedUTXOs.Difference(inputs)
}
