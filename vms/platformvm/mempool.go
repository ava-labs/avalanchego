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
	Add(tx *Tx) error
	Has(txID ids.ID) bool
	Get(txID ids.ID) *Tx

	AddDecisionTx(tx *Tx)
	AddProposalTx(tx *Tx)

	HasDecisionTxs() bool
	HasProposalTx() bool

	RemoveDecisionTxs(txs []*Tx)
	RemoveProposalTx(tx *Tx)

	PopDecisionTxs(numTxs int) []*Tx
	PopProposalTx() *Tx

	MarkDropped(txID ids.ID)
	WasDropped(txID ids.ID) bool
}

// Transactions from clients that have not yet been put into blocks and added to
// consensus
type mempool struct {
	bytesAvailableMetric prometheus.Gauge
	bytesAvailable       int

	unissuedDecisionTxs TxHeap
	unissuedProposalTxs TxHeap
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

	unissuedDecisionTxs, err := NewTxHeapWithMetrics(
		NewTxHeapByAge(),
		fmt.Sprintf("%s_decision_txs", namespace),
		registerer,
	)
	if err != nil {
		return nil, err
	}

	unissuedProposalTxs, err := NewTxHeapWithMetrics(
		NewTxHeapByStartTime(),
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

func (m *mempool) Add(tx *Tx) error {
	// Note: a previously dropped tx can be re-added
	txID := tx.ID()
	if m.Has(txID) {
		return errDuplicatedTx
	}
	if txBytes := tx.Bytes(); len(txBytes) > m.bytesAvailable {
		return errMempoolFull
	}

	inputs := tx.InputIDs()
	if m.consumedUTXOs.Overlaps(inputs) {
		return errConflictingTx
	}

	switch tx.UnsignedTx.(type) {
	case TimedTx:
		m.AddProposalTx(tx)
	case UnsignedDecisionTx:
		m.AddDecisionTx(tx)
	default:
		m.unknownTxs.Inc()
		return errUnknownTxType
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

func (m *mempool) Get(txID ids.ID) *Tx {
	if tx := m.unissuedDecisionTxs.Get(txID); tx != nil {
		return tx
	}
	return m.unissuedProposalTxs.Get(txID)
}

func (m *mempool) AddDecisionTx(tx *Tx) {
	m.unissuedDecisionTxs.Add(tx)
	m.register(tx)
}

func (m *mempool) AddProposalTx(tx *Tx) {
	m.unissuedProposalTxs.Add(tx)
	m.register(tx)
}

func (m *mempool) HasDecisionTxs() bool { return m.unissuedDecisionTxs.Len() > 0 }

func (m *mempool) HasProposalTx() bool { return m.unissuedProposalTxs.Len() > 0 }

func (m *mempool) RemoveDecisionTxs(txs []*Tx) {
	for _, tx := range txs {
		txID := tx.ID()
		if m.unissuedDecisionTxs.Remove(txID) != nil {
			m.deregister(tx)
		}
	}
}

func (m *mempool) RemoveProposalTx(tx *Tx) {
	txID := tx.ID()
	if m.unissuedProposalTxs.Remove(txID) != nil {
		m.deregister(tx)
	}
}

func (m *mempool) PopDecisionTxs(numTxs int) []*Tx {
	if maxLen := m.unissuedDecisionTxs.Len(); numTxs > maxLen {
		numTxs = maxLen
	}

	txs := make([]*Tx, numTxs)
	for i := range txs {
		tx := m.unissuedDecisionTxs.RemoveTop()
		m.deregister(tx)
		txs[i] = tx
	}
	return txs
}

func (m *mempool) PopProposalTx() *Tx {
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

func (m *mempool) register(tx *Tx) {
	txBytes := tx.Bytes()
	m.bytesAvailable -= len(txBytes)
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))
}

func (m *mempool) deregister(tx *Tx) {
	txBytes := tx.Bytes()
	m.bytesAvailable += len(txBytes)
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

	inputs := tx.InputIDs()
	m.consumedUTXOs.Difference(inputs)
}
