// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/setmap"
	"github.com/ava-labs/avalanchego/utils/units"
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
	ErrDuplicateTx          = errors.New("duplicate tx")
	ErrTxTooLarge           = errors.New("tx too large")
	ErrMempoolFull          = errors.New("mempool is full")
	ErrConflictsWithOtherTx = errors.New("tx conflicts with other tx")
)

type Tx interface {
	InputIDs() set.Set[ids.ID]
	ID() ids.ID
	Bytes() []byte
}

type Mempool[T Tx] struct {
	lock           sync.RWMutex
	unissuedTxs    *linked.Hashmap[ids.ID, T]
	consumedUTXOs  *setmap.SetMap[ids.ID, ids.ID] // TxID -> Consumed UTXOs
	bytesAvailable int
	droppedTxIDs   *cache.LRU[ids.ID, error] // TxID -> Verification error

	toEngine chan<- common.Message

	numTxs               prometheus.Gauge
	bytesAvailableMetric prometheus.Gauge
}

func New[T Tx](
	namespace string,
	registerer prometheus.Registerer,
	toEngine chan<- common.Message,
	numTxsGaugeName string,
	numTxsGaugeHelp string,
) (*Mempool[T], error) {
	m := &Mempool[T]{
		unissuedTxs:    linked.NewHashmap[ids.ID, T](),
		consumedUTXOs:  setmap.New[ids.ID, ids.ID](),
		bytesAvailable: maxMempoolSize,
		droppedTxIDs:   &cache.LRU[ids.ID, error]{Size: droppedTxIDsCacheSize},
		toEngine:       toEngine,
		numTxs: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      numTxsGaugeName,
			Help:      numTxsGaugeHelp,
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

func (m *Mempool[T]) Add(tx T) error {
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

	inputs := tx.InputIDs()
	if m.consumedUTXOs.HasOverlap(inputs) {
		return fmt.Errorf("%w: %s", ErrConflictsWithOtherTx, txID)
	}

	m.bytesAvailable -= txSize
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

	m.unissuedTxs.Put(txID, tx)
	m.numTxs.Inc()

	// Mark these UTXOs as consumed in the mempool
	m.consumedUTXOs.Put(txID, inputs)

	// An added tx must not be marked as dropped.
	m.droppedTxIDs.Evict(txID)
	return nil
}

func (m *Mempool[T]) Get(txID ids.ID) (T, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.unissuedTxs.Get(txID)
}

func (m *Mempool[T]) Remove(txs ...T) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, tx := range txs {
		txID := tx.ID()
		// If the transaction is in the mempool, remove it.
		if _, ok := m.consumedUTXOs.DeleteKey(txID); ok {
			m.unissuedTxs.Delete(txID)
			m.bytesAvailable += len(tx.Bytes())
			continue
		}

		// If the transaction isn't in the mempool, remove any conflicts it has.
		inputs := tx.InputIDs()
		for _, removed := range m.consumedUTXOs.DeleteOverlapping(inputs) {
			tx, _ := m.unissuedTxs.Get(removed.Key)
			m.unissuedTxs.Delete(removed.Key)
			m.bytesAvailable += len(tx.Bytes())
		}
	}
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))
	m.numTxs.Set(float64(m.unissuedTxs.Len()))
}

func (m *Mempool[T]) Peek() (T, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	_, tx, exists := m.unissuedTxs.Oldest()
	return tx, exists
}

func (m *Mempool[T]) Iterate(f func(T) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	it := m.unissuedTxs.NewIterator()
	for it.Next() {
		if !f(it.Value()) {
			return
		}
	}
}

func (m *Mempool[_]) RequestBuildBlock(emptyBlockPermitted bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if !emptyBlockPermitted && m.unissuedTxs.Len() == 0 {
		return
	}

	select {
	case m.toEngine <- common.PendingTxs:
	default:
	}
}

func (m *Mempool[_]) MarkDropped(txID ids.ID, reason error) {
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

func (m *Mempool[_]) GetDropReason(txID ids.ID) error {
	err, _ := m.droppedTxIDs.Get(txID)
	return err
}

func (m *Mempool[_]) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.unissuedTxs.Len()
}
