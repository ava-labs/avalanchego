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
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/setmap"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"

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
	_ Mempool = (*mempool)(nil)

	ErrDuplicateTx                = errors.New("duplicate tx")
	ErrTxTooLarge                 = errors.New("tx too large")
	ErrMempoolFull                = errors.New("mempool is full")
	ErrConflictsWithOtherTx       = errors.New("tx conflicts with other tx")
	ErrCantIssueAdvanceTimeTx     = errors.New("can not issue an advance time tx")
	ErrCantIssueRewardValidatorTx = errors.New("can not issue a reward validator tx")
)

type Mempool interface {
	SetEUpgradeActive()

	Add(tx *txs.Tx, tipPercentage commonfees.TipPercentage) error
	Get(txID ids.ID) (*txs.Tx, bool)
	// Remove [txs] and any conflicts of [txs] from the mempool.
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

	// Len returns the number of txs in the mempool.
	Len() int
}

type TxAndTipPercentage struct {
	Tx            *txs.Tx
	TipPercentage commonfees.TipPercentage
}

func lessTxAndTipPercent(a TxAndTipPercentage, b TxAndTipPercentage) bool {
	switch {
	case a.TipPercentage < b.TipPercentage:
		return true
	case a.TipPercentage > b.TipPercentage:
		return false
	default:
		return a.Tx.ID().Compare(b.Tx.ID()) == -1
	}
}

// Transactions from clients that have not yet been put into blocks and added to
// consensus
type mempool struct {
	lock sync.RWMutex

	// TODO: drop [[isEUpgradeActive] once E upgrade is activated
	isEUpgradeActive utils.Atomic[bool]

	// unissued txs sorted by time they entered the mempool
	// TODO: drop [unissuedTxs] once E upgrade is activated
	unissuedTxs linkedhashmap.LinkedHashmap[ids.ID, *txs.Tx]

	// Following E upgrade activation, mempool transactions are sorted by tip percentage
	unissuedTxsByTipPercentage heap.Map[ids.ID, TxAndTipPercentage]

	consumedUTXOs  *setmap.SetMap[ids.ID, ids.ID] // TxID -> Consumed UTXOs
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
		unissuedTxs:                linkedhashmap.New[ids.ID, *txs.Tx](),
		unissuedTxsByTipPercentage: heap.NewMap[ids.ID, TxAndTipPercentage](lessTxAndTipPercent),
		consumedUTXOs:              setmap.New[ids.ID, ids.ID](),
		bytesAvailable:             maxMempoolSize,
		droppedTxIDs:               &cache.LRU[ids.ID, error]{Size: droppedTxIDsCacheSize},
		toEngine:                   toEngine,
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
	m.isEUpgradeActive.Set(false)
	m.bytesAvailableMetric.Set(maxMempoolSize)

	err := utils.Err(
		registerer.Register(m.numTxs),
		registerer.Register(m.bytesAvailableMetric),
	)
	return m, err
}

func (m *mempool) SetEUpgradeActive() {
	m.isEUpgradeActive.Set(true)
}

func (m *mempool) Add(tx *txs.Tx, tipPercentage commonfees.TipPercentage) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	switch tx.Unsigned.(type) {
	case *txs.AdvanceTimeTx:
		return ErrCantIssueAdvanceTimeTx
	case *txs.RewardValidatorTx:
		return ErrCantIssueRewardValidatorTx
	default:
	}

	// Note: a previously dropped tx can be re-added
	txID := tx.ID()
	if _, ok := m.Get(txID); ok {
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
	if m.consumedUTXOs.HasOverlap(inputs) {
		return fmt.Errorf("%w: %s", ErrConflictsWithOtherTx, txID)
	}

	m.unissuedTxs.Put(txID, tx)
	m.unissuedTxsByTipPercentage.Push(txID, TxAndTipPercentage{
		Tx:            tx,
		TipPercentage: tipPercentage,
	})
	m.numTxs.Inc()
	m.bytesAvailable -= txSize
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

	// Mark these UTXOs as consumed in the mempool
	m.consumedUTXOs.Put(txID, inputs)

	// An explicitly added tx must not be marked as dropped.
	m.droppedTxIDs.Evict(txID)

	return nil
}

func (m *mempool) Get(txID ids.ID) (*txs.Tx, bool) {
	if !m.isEUpgradeActive.Get() {
		return m.unissuedTxs.Get(txID)
	}
	v, found := m.unissuedTxsByTipPercentage.Get(txID)
	return v.Tx, found
}

func (m *mempool) Remove(txs ...*txs.Tx) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, tx := range txs {
		txID := tx.ID()
		// If the transaction is in the mempool, remove it.
		if _, ok := m.consumedUTXOs.DeleteKey(txID); ok {
			m.unissuedTxs.Delete(txID)
			m.unissuedTxsByTipPercentage.Remove(txID)
			m.bytesAvailable += len(tx.Bytes())
			continue
		}

		// If the transaction isn't in the mempool, remove any conflicts it has.
		inputs := tx.Unsigned.InputIDs()
		for _, removed := range m.consumedUTXOs.DeleteOverlapping(inputs) {
			tx, _ := m.unissuedTxs.Get(removed.Key)
			m.unissuedTxs.Delete(removed.Key)
			m.unissuedTxsByTipPercentage.Remove(removed.Key)
			m.bytesAvailable += len(tx.Bytes())
		}
	}
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))
	m.numTxs.Set(float64(m.unissuedTxs.Len()))
}

func (m *mempool) Peek() (*txs.Tx, bool) {
	var (
		tx     *txs.Tx
		exists bool
	)

	if !m.isEUpgradeActive.Get() {
		_, tx, exists = m.unissuedTxs.Oldest()
	} else {
		var v TxAndTipPercentage
		_, v, exists = m.unissuedTxsByTipPercentage.Peek()
		tx = v.Tx
	}

	return tx, exists
}

func (m *mempool) Iterate(f func(tx *txs.Tx) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	// TODO: once E upgrade gets activated, replace unissuedTxs
	// with an array containing txs. We cannot iterate [unissuedTxsByTipPercentage]
	// but order does not seems relevant for the functions [f] currently used
	itr := m.unissuedTxs.NewIterator()
	for itr.Next() {
		if !f(itr.Value()) {
			return
		}
	}
}

func (m *mempool) MarkDropped(txID ids.ID, reason error) {
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

func (m *mempool) GetDropReason(txID ids.ID) error {
	err, _ := m.droppedTxIDs.Get(txID)
	return err
}

func (m *mempool) RequestBuildBlock(emptyBlockPermitted bool) {
	if !emptyBlockPermitted && (m.unissuedTxs.Len() == 0 || m.unissuedTxsByTipPercentage.Len() == 0) {
		return
	}

	select {
	case m.toEngine <- common.PendingTxs:
	default:
	}
}

func (m *mempool) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if !m.isEUpgradeActive.Get() {
		return m.unissuedTxs.Len()
	}
	return m.unissuedTxsByTipPercentage.Len()
}
