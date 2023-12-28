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
	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

const (
	// MaxTxSize is the maximum number of bytes a transaction can use to be
	// allowed into the mempool.
	MaxTxSize = 64 * units.KiB

	// droppedTxIDsCacheSize is the maximum number of dropped txIDs to cache
	droppedTxIDsCacheSize = 64

	// maxMempoolSize is the maximum number of bytes allowed in the mempool
	maxMempoolSize = 64 * units.MiB
)

var (
	_ Mempool = (*mempool)(nil)

	ErrDuplicateTx          = errors.New("duplicate tx")
	ErrTxTooLarge           = errors.New("tx too large")
	ErrMempoolFull          = errors.New("mempool is full")
	ErrConflictsWithOtherTx = errors.New("tx conflicts with other tx")
)

// Mempool contains transactions that have not yet been put into a block.
type Mempool interface {
	Add(tx *txs.Tx) error
	Get(txID ids.ID) (*txs.Tx, bool)
	Remove(txs ...*txs.Tx)

	// Peek returns the oldest tx in the mempool.
	Peek() (tx *txs.Tx, exists bool)

	// Iterate over transactions from oldest to newest until the function
	// returns false or there are no more transactions.
	Iterate(f func(tx *txs.Tx) bool)

	// RequestBuildBlock notifies the consensus engine that a block should be
	// built if there is at least one transaction in the mempool.
	RequestBuildBlock()

	// Note: Dropped txs are added to droppedTxIDs but not evicted from
	// unissued. This allows previously dropped txs to be possibly reissued.
	MarkDropped(txID ids.ID, reason error)
	GetDropReason(txID ids.ID) error
}

type mempool struct {
	lock           sync.RWMutex
	unissuedTxs    linkedhashmap.LinkedHashmap[ids.ID, *txs.Tx]
	consumedUTXOs  set.Set[ids.ID]
	bytesAvailable int
	droppedTxIDs   *cache.LRU[ids.ID, error] // TxID -> Verification error

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
		bytesAvailable: maxMempoolSize,
		droppedTxIDs:   &cache.LRU[ids.ID, error]{Size: droppedTxIDsCacheSize},
		toEngine:       toEngine,
		numTxs: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "count",
			Help:      "Number of transactions in the mempool",
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
	txID := tx.ID()

	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.unissuedTxs.Get(txID); ok {
		return fmt.Errorf("%w: %s", ErrDuplicateTx, txID)
	}

	txSize := len(tx.Bytes())
	if txSize > MaxTxSize {
		return fmt.Errorf("%w: %s size (%d) > max size (%d)",
			ErrTxTooLarge,
			txID,
			txSize,
			MaxTxSize,
		)
	}
	if txSize > m.bytesAvailable {
		return fmt.Errorf("%w: %s size (%d) > available space (%d)",
			ErrMempoolFull,
			txID,
			txSize,
			m.bytesAvailable,
		)
	}

	inputs := tx.Unsigned.InputIDs()
	if m.consumedUTXOs.Overlaps(inputs) {
		return fmt.Errorf("%w: %s", ErrConflictsWithOtherTx, txID)
	}

	m.bytesAvailable -= txSize
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

	m.unissuedTxs.Put(txID, tx)
	m.numTxs.Inc()

	// Mark these UTXOs as consumed in the mempool
	m.consumedUTXOs.Union(inputs)

	// An added tx must not be marked as dropped.
	m.droppedTxIDs.Evict(txID)
	return nil
}

func (m *mempool) Get(txID ids.ID) (*txs.Tx, bool) {
	tx, ok := m.unissuedTxs.Get(txID)
	return tx, ok
}

func (m *mempool) Remove(txs ...*txs.Tx) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, tx := range txs {
		txID := tx.ID()
		if !m.unissuedTxs.Delete(txID) {
			continue
		}

		m.bytesAvailable += len(tx.Bytes())
		m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

		m.numTxs.Dec()

		inputs := tx.Unsigned.InputIDs()
		m.consumedUTXOs.Difference(inputs)
	}
}

func (m *mempool) Peek() (*txs.Tx, bool) {
	_, tx, exists := m.unissuedTxs.Oldest()
	return tx, exists
}

func (m *mempool) Iterate(f func(*txs.Tx) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	it := m.unissuedTxs.NewIterator()
	for it.Next() {
		if !f(it.Value()) {
			return
		}
	}
}

func (m *mempool) RequestBuildBlock() {
	if m.unissuedTxs.Len() == 0 {
		return
	}

	select {
	case m.toEngine <- common.PendingTxs:
	default:
	}
}

func (m *mempool) MarkDropped(txID ids.ID, reason error) {
	if errors.Is(reason, ErrMempoolFull) {
		return
	}

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
