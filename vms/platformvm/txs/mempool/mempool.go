// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const (
	// MaxTxSize is the maximum number of bytes a transaction can use to be
	// allowed into the mempool.
	MaxTxSize = 64 * units.KiB

	// droppedTxIDsCacheSize is the maximum number of dropped txIDs to cache
	droppedTxIDsCacheSize = 64

	initialConsumedUTXOsSize = 512

	// maxMempoolSize is the maximum number of bytes allowed in the mempool
	maxMempoolSize = 64 * units.MiB
)

var (
	_ Mempool = (*mempool)(nil)

	errDuplicateTx                = errors.New("duplicate tx")
	errTxTooLarge                 = errors.New("tx too large")
	errMempoolFull                = errors.New("mempool is full")
	errConflictsWithOtherTx       = errors.New("tx conflicts with other tx")
	errCantIssueAdvanceTimeTx     = errors.New("can not issue an advance time tx")
	errCantIssueRewardValidatorTx = errors.New("can not issue a reward validator tx")
)

type Mempool interface {
	Add(tx *txs.Tx) error
	Get(txID ids.ID) (*txs.Tx, bool)
	Remove(txs ...*txs.Tx)

	// Peek returns the oldest tx in the mempool.
	Peek() (tx *txs.Tx, exists bool)

	// Iterate iterates over the txs until f returns false
	Iterate(f func(tx *txs.Tx) bool)

	// RequestBuildBlock notifies the consensus engine that a block should be
	// built. If [emptyBlockPermitted] is true, the notification will be sent
	// regardless of whether there are no transactions in the mempool. If not,
	// a notification will only be sent if there is at least one transaction in
	// the mempool.
	RequestBuildBlock(emptyBlockPermitted bool)

	// Note: dropped txs are added to droppedTxIDs but are not evicted from
	// unissued decision/staker txs. This allows previously dropped txs to be
	// possibly reissued.
	MarkDropped(txID ids.ID, reason error)
	GetDropReason(txID ids.ID) error
}

// Transactions from clients that have not yet been put into blocks and added to
// consensus
type mempool struct {
	lock           sync.RWMutex
	unissuedTxs    linkedhashmap.LinkedHashmap[ids.ID, *txs.Tx]
	consumedUTXOs  set.Set[ids.ID]
	bytesAvailable int
	droppedTxIDs   *cache.LRU[ids.ID, error] // TxID -> verification error

	toEngine chan<- common.Message

	numTxs               prometheus.Gauge
	bytesAvailableMetric prometheus.Gauge
}

func New(
	namespace string,
	registerer prometheus.Registerer,
	toEngine chan<- common.Message,
) (Mempool, error) {
	m := &mempool{
		unissuedTxs:    linkedhashmap.New[ids.ID, *txs.Tx](),
		consumedUTXOs:  set.NewSet[ids.ID](initialConsumedUTXOsSize),
		bytesAvailable: maxMempoolSize,
		droppedTxIDs:   &cache.LRU[ids.ID, error]{Size: droppedTxIDsCacheSize},
		toEngine:       toEngine,
		numTxs: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "txs",
			Help:      "Number of decision/staker transactions in the mempool",
		}),
		bytesAvailableMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "bytes_available",
			Help:      "Number of bytes of space currently available in the mempool",
		}),
	}
	m.bytesAvailableMetric.Set(maxMempoolSize)

	err := utils.Err(
		registerer.Register(m.numTxs),
		registerer.Register(m.bytesAvailableMetric),
	)
	return m, err
}

func (m *mempool) Add(tx *txs.Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	switch tx.Unsigned.(type) {
	case *txs.AdvanceTimeTx:
		return errCantIssueAdvanceTimeTx
	case *txs.RewardValidatorTx:
		return errCantIssueRewardValidatorTx
	default:
	}

	// Note: a previously dropped tx can be re-added
	txID := tx.ID()
	if _, ok := m.unissuedTxs.Get(txID); ok {
		return fmt.Errorf("%w: %s", errDuplicateTx, txID)
	}

	txSize := len(tx.Bytes())
	if txSize > MaxTxSize {
		return fmt.Errorf("%w: %s size (%d) > max size (%d)",
			errTxTooLarge,
			txID,
			txSize,
			MaxTxSize,
		)
	}
	if txSize > m.bytesAvailable {
		return fmt.Errorf("%w: %s size (%d) > available space (%d)",
			errMempoolFull,
			txID,
			txSize,
			m.bytesAvailable,
		)
	}

	inputs := tx.Unsigned.InputIDs()
	if m.consumedUTXOs.Overlaps(inputs) {
		return fmt.Errorf("%w: %s", errConflictsWithOtherTx, txID)
	}

	m.unissuedTxs.Put(tx.ID(), tx)
	m.numTxs.Inc()
	m.bytesAvailable -= txSize
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

	// Mark these UTXOs as consumed in the mempool
	m.consumedUTXOs.Union(inputs)

	// An explicitly added tx must not be marked as dropped.
	m.droppedTxIDs.Evict(txID)

	return nil
}

func (m *mempool) Get(txID ids.ID) (*txs.Tx, bool) {
	return m.unissuedTxs.Get(txID)
}

func (m *mempool) Remove(txs ...*txs.Tx) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, tx := range txs {
		txID := tx.ID()
		if !m.unissuedTxs.Delete(txID) {
			continue
		}
		m.numTxs.Dec()

		m.bytesAvailable += len(tx.Bytes())
		m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

		inputs := tx.Unsigned.InputIDs()
		m.consumedUTXOs.Difference(inputs)
	}
}

func (m *mempool) Peek() (*txs.Tx, bool) {
	_, tx, exists := m.unissuedTxs.Oldest()
	return tx, exists
}

func (m *mempool) Iterate(f func(tx *txs.Tx) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	itr := m.unissuedTxs.NewIterator()
	for itr.Next() {
		if !f(itr.Value()) {
			return
		}
	}
}

func (m *mempool) MarkDropped(txID ids.ID, reason error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if _, ok := m.unissuedTxs.Get(txID); ok {
		return
	}

	m.droppedTxIDs.Put(txID, reason)
}

func (m *mempool) GetDropReason(txID ids.ID) error {
	err, _ := m.droppedTxIDs.Get(txID)
	return err
}

func (m *mempool) RequestBuildBlock(emptyBlockPermitted bool) {
	if !emptyBlockPermitted && m.unissuedTxs.Len() == 0 {
		return
	}

	select {
	case m.toEngine <- common.PendingTxs:
	default:
	}
}
