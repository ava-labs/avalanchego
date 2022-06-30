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
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txheap"
)

const (
	// droppedTxIDsCacheSize is the maximum number of dropped txIDs to cache
	droppedTxIDsCacheSize = 64

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
	Add(tx *txs.Tx) error
	Has(txID ids.ID) bool
	Get(txID ids.ID) *txs.Tx

	AddDecisionTx(tx *txs.Tx)
	AddProposalTx(tx *txs.Tx)

	HasDecisionTxs() bool
	HasProposalTx() bool

	RemoveDecisionTxs(txs []*txs.Tx)
	RemoveProposalTx(tx *txs.Tx)

	PopDecisionTxs(maxTxsBytes int) []*txs.Tx
	PopProposalTx() *txs.Tx

	// Note: dropped txs are added to droppedTxIDs but not
	// not evicted from unissued decision/proposal txs.
	// This allows previously dropped txs to be possibly
	// reissued.
	MarkDropped(txID ids.ID, reason string)
	GetDropReason(txID ids.ID) (string, bool)
}

// Transactions from clients that have not yet been put into blocks and added to
// consensus
type mempool struct {
	bytesAvailableMetric prometheus.Gauge
	bytesAvailable       int

	unissuedDecisionTxs txheap.Heap
	unissuedProposalTxs txheap.Heap
	unknownTxs          prometheus.Counter

	// Key: Tx ID
	// Value: String repr. of the verification error
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

	unissuedDecisionTxs, err := txheap.NewWithMetrics(
		txheap.NewByAge(),
		fmt.Sprintf("%s_decision_txs", namespace),
		registerer,
	)
	if err != nil {
		return nil, err
	}

	unissuedProposalTxs, err := txheap.NewWithMetrics(
		txheap.NewByStartTime(),
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

func (m *mempool) Add(tx *txs.Tx) error {
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
	case *txs.AddValidatorTx, *txs.AddDelegatorTx, *txs.AddSubnetValidatorTx:
		m.AddProposalTx(tx)
	case *txs.CreateChainTx, *txs.CreateSubnetTx, *txs.ImportTx, *txs.ExportTx:
		m.AddDecisionTx(tx)
	default:
		m.unknownTxs.Inc()
		return fmt.Errorf("%w: %T", errUnknownTxType, tx.Unsigned)
	}

	// Mark these UTXOs as consumed in the mempool
	m.consumedUTXOs.Union(inputs)

	// An explicitly added tx must not be marked as dropped.
	m.droppedTxIDs.Evict(txID)
	return nil
}

func (m *mempool) Has(txID ids.ID) bool {
	return m.Get(txID) != nil
}

func (m *mempool) Get(txID ids.ID) *txs.Tx {
	if tx := m.unissuedDecisionTxs.Get(txID); tx != nil {
		return tx
	}
	return m.unissuedProposalTxs.Get(txID)
}

func (m *mempool) AddDecisionTx(tx *txs.Tx) {
	m.unissuedDecisionTxs.Add(tx)
	m.register(tx)
}

func (m *mempool) AddProposalTx(tx *txs.Tx) {
	m.unissuedProposalTxs.Add(tx)
	m.register(tx)
}

func (m *mempool) HasDecisionTxs() bool { return m.unissuedDecisionTxs.Len() > 0 }

func (m *mempool) HasProposalTx() bool { return m.unissuedProposalTxs.Len() > 0 }

func (m *mempool) RemoveDecisionTxs(txs []*txs.Tx) {
	for _, tx := range txs {
		txID := tx.ID()
		if m.unissuedDecisionTxs.Remove(txID) != nil {
			m.deregister(tx)
		}
	}
}

func (m *mempool) RemoveProposalTx(tx *txs.Tx) {
	txID := tx.ID()
	if m.unissuedProposalTxs.Remove(txID) != nil {
		m.deregister(tx)
	}
}

func (m *mempool) PopDecisionTxs(maxTxsBytes int) []*txs.Tx {
	var txs []*txs.Tx
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

func (m *mempool) PopProposalTx() *txs.Tx {
	tx := m.unissuedProposalTxs.RemoveTop()
	m.deregister(tx)
	return tx
}

func (m *mempool) MarkDropped(txID ids.ID, reason string) {
	m.droppedTxIDs.Put(txID, reason)
}

func (m *mempool) GetDropReason(txID ids.ID) (string, bool) {
	reason, exist := m.droppedTxIDs.Get(txID)
	if !exist {
		return "", false
	}
	return reason.(string), true
}

func (m *mempool) register(tx *txs.Tx) {
	txBytes := tx.Bytes()
	m.bytesAvailable -= len(txBytes)
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))
}

func (m *mempool) deregister(tx *txs.Tx) {
	txBytes := tx.Bytes()
	m.bytesAvailable += len(txBytes)
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

	inputs := tx.Unsigned.InputIDs()
	m.consumedUTXOs.Difference(inputs)
}
