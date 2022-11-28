// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txheap"
)

const (
	// targetTxSize is the maximum number of bytes a transaction can use to be
	// allowed into the mempool.
	targetTxSize = 64 * units.KiB

	// droppedTxIDsCacheSize is the maximum number of dropped txIDs to cache
	droppedTxIDsCacheSize = 64

	initialConsumedUTXOsSize = 512

	// maxMempoolSize is the maximum number of bytes allowed in the mempool
	maxMempoolSize = 64 * units.MiB
)

var (
	_ Mempool = (*mempool)(nil)

	errMempoolFull = errors.New("mempool is full")
)

type BlockTimer interface {
	// ResetBlockTimer schedules a timer to notify the consensus engine once
	// there is a block ready to be built. If a block is ready to be built when
	// this function is called, the engine will be notified directly.
	ResetBlockTimer()
}

type Mempool interface {
	// we may want to be able to stop valid transactions
	// from entering the mempool, e.g. during blocks creation
	EnableAdding()
	DisableAdding()

	Add(tx *txs.Tx) error
	Has(txID ids.ID) bool
	Get(txID ids.ID) *txs.Tx
	Remove(txs []*txs.Tx)

	// Following Banff activation, all mempool transactions,
	// (both decision and staker) are included into Standard blocks.
	// HasTxs allow to check for availability of any mempool transaction.
	HasTxs() bool
	// PeekTxs returns the next txs for Banff blocks
	// up to maxTxsBytes without removing them from the mempool.
	PeekTxs(maxTxsBytes int) []*txs.Tx

	HasStakerTx() bool
	// PeekStakerTx returns the next stakerTx without removing it from mempool.
	// It returns nil if !HasStakerTx().
	// It's guaranteed that the returned tx, if not nil, is a StakerTx.
	PeekStakerTx() *txs.Tx

	// Note: dropped txs are added to droppedTxIDs but not
	// not evicted from unissued decision/staker txs.
	// This allows previously dropped txs to be possibly
	// reissued.
	MarkDropped(txID ids.ID, reason string)
	GetDropReason(txID ids.ID) (string, bool)
}

// Transactions from clients that have not yet been put into blocks and added to
// consensus
type mempool struct {
	// If true, drop transactions added to the mempool via Add.
	dropIncoming bool

	bytesAvailableMetric prometheus.Gauge
	bytesAvailable       int

	unissuedDecisionTxs txheap.Heap
	unissuedStakerTxs   txheap.Heap

	// Key: Tx ID
	// Value: String repr. of the verification error
	droppedTxIDs *cache.LRU

	consumedUTXOs set.Set[ids.ID]

	blkTimer BlockTimer
}

func NewMempool(
	namespace string,
	registerer prometheus.Registerer,
	blkTimer BlockTimer,
) (Mempool, error) {
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

	unissuedStakerTxs, err := txheap.NewWithMetrics(
		txheap.NewByStartTime(),
		fmt.Sprintf("%s_staker_txs", namespace),
		registerer,
	)
	if err != nil {
		return nil, err
	}

	bytesAvailableMetric.Set(maxMempoolSize)
	return &mempool{
		bytesAvailableMetric: bytesAvailableMetric,
		bytesAvailable:       maxMempoolSize,
		unissuedDecisionTxs:  unissuedDecisionTxs,
		unissuedStakerTxs:    unissuedStakerTxs,
		droppedTxIDs:         &cache.LRU{Size: droppedTxIDsCacheSize},
		consumedUTXOs:        set.NewSet[ids.ID](initialConsumedUTXOsSize),
		dropIncoming:         false, // enable tx adding by default
		blkTimer:             blkTimer,
	}, nil
}

func (m *mempool) EnableAdding() {
	m.dropIncoming = false
}

func (m *mempool) DisableAdding() {
	m.dropIncoming = true
}

func (m *mempool) Add(tx *txs.Tx) error {
	if m.dropIncoming {
		return fmt.Errorf("tx %s not added because mempool is closed", tx.ID())
	}

	// Note: a previously dropped tx can be re-added
	txID := tx.ID()
	if m.Has(txID) {
		return fmt.Errorf("duplicate tx %s", txID)
	}

	txBytes := tx.Bytes()
	if len(txBytes) > targetTxSize {
		return fmt.Errorf("tx %s size (%d) > target size (%d)", txID, len(txBytes), targetTxSize)
	}
	if len(txBytes) > m.bytesAvailable {
		return fmt.Errorf("%w, tx %s size (%d) exceeds available space (%d)",
			errMempoolFull,
			txID,
			len(txBytes),
			m.bytesAvailable,
		)
	}

	inputs := tx.Unsigned.InputIDs()
	if m.consumedUTXOs.Overlaps(inputs) {
		return fmt.Errorf("tx %s conflicts with a transaction in the mempool", txID)
	}

	if err := tx.Unsigned.Visit(&issuer{
		m:  m,
		tx: tx,
	}); err != nil {
		return err
	}

	// Mark these UTXOs as consumed in the mempool
	m.consumedUTXOs.Union(inputs)

	// An explicitly added tx must not be marked as dropped.
	m.droppedTxIDs.Evict(txID)

	m.blkTimer.ResetBlockTimer()
	return nil
}

func (m *mempool) Has(txID ids.ID) bool {
	return m.Get(txID) != nil
}

func (m *mempool) Get(txID ids.ID) *txs.Tx {
	if tx := m.unissuedDecisionTxs.Get(txID); tx != nil {
		return tx
	}
	return m.unissuedStakerTxs.Get(txID)
}

func (m *mempool) Remove(txsToRemove []*txs.Tx) {
	remover := &remover{
		m: m,
	}

	for _, tx := range txsToRemove {
		remover.tx = tx
		_ = tx.Unsigned.Visit(remover)
	}
}

func (m *mempool) HasTxs() bool {
	return m.unissuedDecisionTxs.Len() > 0 || m.unissuedStakerTxs.Len() > 0
}

func (m *mempool) PeekTxs(maxTxsBytes int) []*txs.Tx {
	txs := m.unissuedDecisionTxs.List()
	txs = append(txs, m.unissuedStakerTxs.List()...)

	size := 0
	for i, tx := range txs {
		size += len(tx.Bytes())
		if size > maxTxsBytes {
			return txs[:i]
		}
	}
	return txs
}

func (m *mempool) addDecisionTx(tx *txs.Tx) {
	m.unissuedDecisionTxs.Add(tx)
	m.register(tx)
}

func (m *mempool) addStakerTx(tx *txs.Tx) {
	m.unissuedStakerTxs.Add(tx)
	m.register(tx)
}

func (m *mempool) HasStakerTx() bool {
	return m.unissuedStakerTxs.Len() > 0
}

func (m *mempool) removeDecisionTxs(txs []*txs.Tx) {
	for _, tx := range txs {
		txID := tx.ID()
		if m.unissuedDecisionTxs.Remove(txID) != nil {
			m.deregister(tx)
		}
	}
}

func (m *mempool) removeStakerTx(tx *txs.Tx) {
	txID := tx.ID()
	if m.unissuedStakerTxs.Remove(txID) != nil {
		m.deregister(tx)
	}
}

func (m *mempool) PeekStakerTx() *txs.Tx {
	if m.unissuedStakerTxs.Len() == 0 {
		return nil
	}

	return m.unissuedStakerTxs.Peek()
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
