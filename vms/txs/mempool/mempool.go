// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
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
	Size() int
}

type Metrics interface {
	Update(numTxs, bytesAvailable int)
}

type Mempool[T Tx] interface {
	Add(tx T) error
	Get(txID ids.ID) (T, bool)
	// Remove [txs] and any conflicts of [txs] from the mempool.
	Remove(txs ...T)

	// Peek returns the oldest tx in the mempool.
	Peek() (tx T, exists bool)

	// Iterate iterates over the txs until f returns false
	Iterate(f func(tx T) bool)

	// Note: dropped txs are added to droppedTxIDs but are not evicted from
	// unissued decision/staker txs. This allows previously dropped txs to be
	// possibly reissued.
	MarkDropped(txID ids.ID, reason error)
	GetDropReason(txID ids.ID) error

	// Len returns the number of txs in the mempool.
	Len() int
}

type mempool[T Tx] struct {
	lock           sync.RWMutex
	unissuedTxs    *linked.Hashmap[ids.ID, T]
	consumedUTXOs  *setmap.SetMap[ids.ID, ids.ID] // TxID -> Consumed UTXOs
	bytesAvailable int
	droppedTxIDs   *cache.LRU[ids.ID, error] // TxID -> Verification error

	metrics Metrics
}

func New[T Tx](
	metrics Metrics,
) *mempool[T] {
	m := &mempool[T]{
		unissuedTxs:    linked.NewHashmap[ids.ID, T](),
		consumedUTXOs:  setmap.New[ids.ID, ids.ID](),
		bytesAvailable: maxMempoolSize,
		droppedTxIDs:   &cache.LRU[ids.ID, error]{Size: droppedTxIDsCacheSize},
		metrics:        metrics,
	}
	m.updateMetrics()

	return m
}

func (m *mempool[T]) updateMetrics() {
	m.metrics.Update(m.unissuedTxs.Len(), m.bytesAvailable)
}

func (m *mempool[T]) Add(tx T) error {
	txID := tx.ID()

	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.unissuedTxs.Get(txID); ok {
		return fmt.Errorf("%w: %s", ErrDuplicateTx, txID)
	}

	txSize := tx.Size()
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
	m.unissuedTxs.Put(txID, tx)
	m.updateMetrics()

	// Mark these UTXOs as consumed in the mempool
	m.consumedUTXOs.Put(txID, inputs)

	// An added tx must not be marked as dropped.
	m.droppedTxIDs.Evict(txID)
	return nil
}

func (m *mempool[T]) Get(txID ids.ID) (T, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.unissuedTxs.Get(txID)
}

func (m *mempool[T]) Remove(txs ...T) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, tx := range txs {
		txID := tx.ID()
		// If the transaction is in the mempool, remove it.
		if _, ok := m.consumedUTXOs.DeleteKey(txID); ok {
			m.unissuedTxs.Delete(txID)
			m.bytesAvailable += tx.Size()
			continue
		}

		// If the transaction isn't in the mempool, remove any conflicts it has.
		inputs := tx.InputIDs()
		for _, removed := range m.consumedUTXOs.DeleteOverlapping(inputs) {
			tx, _ := m.unissuedTxs.Get(removed.Key)
			m.unissuedTxs.Delete(removed.Key)
			m.bytesAvailable += tx.Size()
		}
	}
	m.updateMetrics()
}

func (m *mempool[T]) Peek() (T, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	_, tx, exists := m.unissuedTxs.Oldest()
	return tx, exists
}

func (m *mempool[T]) Iterate(f func(T) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	it := m.unissuedTxs.NewIterator()
	for it.Next() {
		if !f(it.Value()) {
			return
		}
	}
}

func (m *mempool[_]) MarkDropped(txID ids.ID, reason error) {
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

func (m *mempool[_]) GetDropReason(txID ids.ID) error {
	err, _ := m.droppedTxIDs.Get(txID)
	return err
}

func (m *mempool[_]) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.unissuedTxs.Len()
}
