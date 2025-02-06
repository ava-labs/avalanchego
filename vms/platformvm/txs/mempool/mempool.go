// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/lock"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/setmap"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	ErrGasCapacityExceeded = errors.New("gas capacity exceeded")
	errMissingConsumedAVAX = errors.New("missing consumed avax")
	errNoGasUsed           = errors.New("no gas used")
)

type heapTx struct {
	*txs.Tx
	GasPrice float64

	gasUsed gas.Gas
}

type Mempool struct {
	weights     gas.Dimensions
	gasCapacity gas.Gas
	avaxAssetID ids.ID

	lock               sync.RWMutex
	cond               *lock.Cond
	maxHeap            heap.Map[ids.ID, heapTx]
	minHeap            heap.Map[ids.ID, heapTx]
	consumedUTXOs      *setmap.SetMap[ids.ID, ids.ID]
	droppedTxIDs       *lru.Cache[ids.ID, error]
	currentGas         gas.Gas
	numTxsMetric       prometheus.Gauge
	gasAvailableMetric prometheus.Gauge
}

func New(
	namespace string,
	weights gas.Dimensions,
	gasCapacity gas.Gas,
	avaxAssetID ids.ID,
	registerer prometheus.Registerer,
) (*Mempool, error) {
	numTxsMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "count",
		Help:      "number of transactions in the mempool",
	})

	gasAvailableMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "gas_available",
		Help:      "amount of remaining gas available",
	})

	if err := errors.Join(
		registerer.Register(numTxsMetric),
		registerer.Register(gasAvailableMetric),
	); err != nil {
		return nil, fmt.Errorf("failed to register metrics: %w", err)
	}

	m := &Mempool{
		weights:     weights,
		gasCapacity: gasCapacity,
		avaxAssetID: avaxAssetID,
		maxHeap: heap.NewMap[ids.ID, heapTx](func(a, b heapTx) bool {
			return a.GasPrice > b.GasPrice
		}),
		minHeap: heap.NewMap[ids.ID, heapTx](func(a, b heapTx) bool {
			return a.GasPrice < b.GasPrice
		}),
		consumedUTXOs:      setmap.New[ids.ID, ids.ID](),
		droppedTxIDs:       lru.NewCache[ids.ID, error](64),
		numTxsMetric:       numTxsMetric,
		gasAvailableMetric: gasAvailableMetric,
	}

	m.cond = lock.NewCond(&m.lock)
	return m, nil
}

func (m *Mempool) Add(tx *txs.Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.consumedUTXOs.HasOverlap(tx.InputIDs()) {
		return mempool.ErrConflictsWithOtherTx
	}

	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx.Unsigned)
	if err != nil {
		return fmt.Errorf("failed to get utxos: %w", err)
	}

	consumedAVAX := uint64(0)
	for _, utxo := range ins {
		if utxo.AssetID() != m.avaxAssetID {
			continue
		}

		// The caller should verify txs but perform overflow checks anyway
		consumedAVAX, err = math.Add(consumedAVAX, utxo.In.Amount())
		if err != nil {
			return fmt.Errorf("failed to add consumed AVAX: %w", err)
		}
	}

	if consumedAVAX == 0 {
		return errMissingConsumedAVAX
	}

	for _, utxo := range outs {
		if utxo.AssetID() != m.avaxAssetID {
			continue
		}

		// The caller should verify txs but perform overflow checks anyway
		producedAVAX, err = math.Add64(producedAVAX, utxo.Out.Amount())
		if err != nil {
			return fmt.Errorf("failed to add produced AVAX: %w", err)
		}
	}

	gasUsed, err := m.meter(tx.Unsigned)
	if err != nil {
		return fmt.Errorf("failed to meter tx: %w", err)
	}

	heapTx := heapTx{
		Tx:       tx,
		gasUsed:  gasUsed,
		GasPrice: float64(consumedAVAX-producedAVAX) / float64(gasUsed),
	}

	// Try to evict lower gas priced txs if we do not have enough remaining gas
	// capacity
	if next := m.currentGas + gasUsed; next > m.gasCapacity {
		gasToFree := next - m.gasCapacity
		if err := m.tryEvictTxs(gasToFree, heapTx); err != nil {
			return err
		}
	}

	m.maxHeap.Push(tx.TxID, heapTx)
	m.minHeap.Push(tx.TxID, heapTx)
	m.consumedUTXOs.Put(tx.TxID, tx.InputIDs())
	m.droppedTxIDs.Evict(tx.TxID)
	m.currentGas += gasUsed

	m.updateMetrics()
	m.cond.Broadcast()

	return nil
}

func (m *Mempool) tryEvictTxs(gasToFree gas.Gas, txToAdd heapTx) error {
	gasFreed := gas.Gas(0)
	toEvict := make([]ids.ID, 0)

	for _, tx := range m.minHeap.Iterator() {
		// Try to evict lower priced txs to make room for the new tx
		if tx.GasPrice >= txToAdd.GasPrice {
			continue
		}

		txID := tx.Tx.TxID
		toEvict = append(toEvict, txID)
		gasFreed += tx.gasUsed

		if gasFreed >= gasToFree {
			break
		}
	}

	if gasFreed < gasToFree {
		return ErrGasCapacityExceeded
	}

	for _, txID := range toEvict {
		m.remove(txID)
	}

	return nil
}

func (m *Mempool) updateMetrics() {
	m.numTxsMetric.Set(float64(m.maxHeap.Len()))
	m.gasAvailableMetric.Set(float64(m.gasCapacity - m.currentGas))
}

func (m *Mempool) Get(txID ids.ID) (*txs.Tx, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	tx, ok := m.maxHeap.Get(txID)
	if !ok {
		return nil, false
	}

	return tx.Tx, true
}

func (m *Mempool) Remove(txID ids.ID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.remove(txID)
}

func (m *Mempool) remove(txID ids.ID) {
	heapTx, ok := m.maxHeap.Remove(txID)
	if !ok {
		return
	}

	m.minHeap.Remove(txID)
	m.consumedUTXOs.DeleteKey(txID)

	m.currentGas -= heapTx.gasUsed

	m.updateMetrics()
}

func (m *Mempool) RemoveConflicts(utxos set.Set[ids.ID]) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, removed := range m.consumedUTXOs.DeleteOverlapping(utxos) {
		m.remove(removed.Key)
	}
}

func (m *Mempool) Peek() (*txs.Tx, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	_, tx, ok := m.maxHeap.Peek()
	return tx.Tx, ok
}

func (m *Mempool) Iterate(f func(tx *txs.Tx) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	for _, v := range m.maxHeap.Iterator() {
		if !f(v.Tx) {
			return
		}
	}
}

func (m *Mempool) MarkDropped(txID ids.ID, reason error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.maxHeap.Get(txID); ok {
		return
	}

	m.droppedTxIDs.Put(txID, reason)
}

func (m *Mempool) GetDropReason(txID ids.ID) error {
	m.lock.RLock()
	defer m.lock.RUnlock()

	err, _ := m.droppedTxIDs.Get(txID)
	return err
}

func (m *Mempool) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.maxHeap.Len()
}

func (m *Mempool) meter(tx txs.UnsignedTx) (gas.Gas, error) {
	c, err := fee.TxComplexity(tx)
	if err != nil {
		return 0, err
	}

	g, err := c.ToGas(m.weights)
	if err != nil {
		return 0, err
	}

	if g == 0 {
		return 0, errNoGasUsed
	}

	return g, nil
}

// TODO Copy a test from txmempool
func (m *Mempool) WaitForEvent(ctx context.Context) (common.Message, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for m.maxHeap.Len() == 0 {
		if err := m.cond.Wait(ctx); err != nil {
			return 0, err
		}
	}
	return common.PendingTxs, nil
}
