// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/setmap"
	"github.com/ava-labs/avalanchego/utils/units"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
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

// TxAndTipPercentage is a helper struct to sort transactions
// by priority fee once E uprade activates
type TxAndTipPercentage[T Tx] struct {
	Tx            T
	TipPercentage commonfees.TipPercentage
}

func lessTxAndTipPercent[T Tx](a TxAndTipPercentage[T], b TxAndTipPercentage[T]) bool {
	switch {
	case a.TipPercentage < b.TipPercentage:
		return true
	case a.TipPercentage > b.TipPercentage:
		return false
	default:
		return a.Tx.ID().Compare(b.Tx.ID()) == -1
	}
}

type Mempool[T Tx] interface {
	SetEUpgradeActive()

	Add(tx T, tipPercentage commonfees.TipPercentage) error
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
	lock sync.RWMutex
	// TODO: drop [[isEUpgradeActive] once E upgrade is activated
	isEUpgradeActive utils.Atomic[bool]

	// unissued txs sorted by time they entered the mempool
	// TODO: drop [unissuedTxs] once E upgrade is activated
	unissuedTxs *linked.Hashmap[ids.ID, T]

	// Following E upgrade activation, mempool transactions are sorted by tip percentage
	unissuedTxsByTipPercentage heap.Map[ids.ID, TxAndTipPercentage[T]]

	consumedUTXOs  *setmap.SetMap[ids.ID, ids.ID] // TxID -> Consumed UTXOs
	bytesAvailable int
	droppedTxIDs   *cache.LRU[ids.ID, error] // TxID -> Verification error

	metrics Metrics
}

func New[T Tx](
	metrics Metrics,
) *mempool[T] {
	m := &mempool[T]{
		unissuedTxs:                linked.NewHashmap[ids.ID, T](),
		unissuedTxsByTipPercentage: heap.NewMap[ids.ID, TxAndTipPercentage[T]](lessTxAndTipPercent),
		consumedUTXOs:              setmap.New[ids.ID, ids.ID](),
		bytesAvailable:             maxMempoolSize,
		droppedTxIDs:               &cache.LRU[ids.ID, error]{Size: droppedTxIDsCacheSize},
		metrics:                    metrics,
	}
	m.isEUpgradeActive.Set(false)
	m.updateMetrics()

	return m
}

func (m *mempool[T]) updateMetrics() {
	m.metrics.Update(m.unissuedTxs.Len(), m.bytesAvailable)
}

func (m *mempool[T]) SetEUpgradeActive() {
	m.isEUpgradeActive.Set(true)
}

func (m *mempool[T]) Add(tx T, tipPercentage commonfees.TipPercentage) error {
	txID := tx.ID()

	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.get(txID); ok {
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
	m.unissuedTxsByTipPercentage.Push(txID, TxAndTipPercentage[T]{
		Tx:            tx,
		TipPercentage: tipPercentage,
	})
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

	return m.get(txID)
}

func (m *mempool[T]) get(txID ids.ID) (T, bool) {
	if !m.isEUpgradeActive.Get() {
		return m.unissuedTxs.Get(txID)
	}
	v, found := m.unissuedTxsByTipPercentage.Get(txID)
	return v.Tx, found
}

func (m *mempool[T]) Remove(txs ...T) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, tx := range txs {
		txID := tx.ID()
		// If the transaction is in the mempool, remove it.
		if _, ok := m.consumedUTXOs.DeleteKey(txID); ok {
			m.unissuedTxs.Delete(txID)
			m.unissuedTxsByTipPercentage.Remove(txID)
			m.bytesAvailable += tx.Size()
			continue
		}

		// If the transaction isn't in the mempool, remove any conflicts it has.
		inputs := tx.InputIDs()
		for _, removed := range m.consumedUTXOs.DeleteOverlapping(inputs) {
			tx, _ := m.unissuedTxs.Get(removed.Key)
			m.unissuedTxs.Delete(removed.Key)
			m.unissuedTxsByTipPercentage.Remove(removed.Key)
			m.bytesAvailable += tx.Size()
		}
	}
	m.updateMetrics()
}

func (m *mempool[T]) Peek() (T, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	var (
		tx     T
		exists bool
	)

	if !m.isEUpgradeActive.Get() {
		_, tx, exists = m.unissuedTxs.Oldest()
	} else {
		var v TxAndTipPercentage[T]
		_, v, exists = m.unissuedTxsByTipPercentage.Peek()
		tx = v.Tx
	}

	return tx, exists
}

func (m *mempool[T]) Iterate(f func(T) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	// TODO: once E upgrade gets activated, replace unissuedTxs
	// with an array containing txs. We cannot iterate [unissuedTxsByTipPercentage]
	// but order does not seems relevant for the functions [f] currently used
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

	if !m.isEUpgradeActive.Get() {
		if _, ok := m.unissuedTxs.Get(txID); ok {
			return
		}
	} else {
		if _, ok := m.unissuedTxsByTipPercentage.Get(txID); ok {
			return
		}
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

	if !m.isEUpgradeActive.Get() {
		return m.unissuedTxs.Len()
	}
	return m.unissuedTxsByTipPercentage.Len()
}
