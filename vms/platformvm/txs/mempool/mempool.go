// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/heap"
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

	lock               sync.Mutex
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

	return &Mempool{
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
	}, nil
}

func (m *Mempool) Add(tx *txs.Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx.Unsigned)
	if err != nil {
		return fmt.Errorf("failed to get utxos: %w", err)
	}

	consumedAVAX := uint64(0)
	for _, utxo := range ins {
		if utxo.AssetID() != m.avaxAssetID {
			continue
		}

		consumedAVAX += utxo.In.Amount()
	}

	if consumedAVAX == 0 {
		return errMissingConsumedAVAX
	}

	for _, utxo := range outs {
		if utxo.AssetID() != m.avaxAssetID {
			continue
		}

		producedAVAX += utxo.Out.Amount()
	}

	_, gasUsed, err := m.meter(tx.Unsigned)
	if err != nil {
		return fmt.Errorf("failed to meter tx: %w", err)
	}

	heapTx := heapTx{
		Tx:       tx,
		gasUsed:  gasUsed,
		GasPrice: float64(consumedAVAX-producedAVAX) / float64(gasUsed),
	}

	// Try to evict a lower paying tx if we do not have enough remaining gas
	// capacity
	if next := m.currentGas + gasUsed; next > m.gasCapacity {
		if err := m.tryEvictTx(next, heapTx); err != nil {
			return err
		}
	}

	if m.consumedUTXOs.HasOverlap(tx.InputIDs()) {
		return mempool.ErrConflictsWithOtherTx
	}

	m.maxHeap.Push(tx.TxID, heapTx)
	m.minHeap.Push(tx.TxID, heapTx)
	m.consumedUTXOs.Put(tx.TxID, tx.InputIDs())
	m.droppedTxIDs.Evict(tx.TxID)
	m.currentGas += gasUsed

	m.updateMetrics()

	return nil
}

// Evict a tx if it is lower paying and the resulting total gas is within
// capacity
func (m *Mempool) tryEvictTx(next gas.Gas, tx heapTx) error {
	_, low, ok := m.minHeap.Peek()
	if !ok {
		return ErrGasCapacityExceeded
	}

	// Check if evicting the lowest paying tx would give us enough capacity to fit
	// this tx
	if low.GasPrice < tx.GasPrice && next-low.gasUsed <= m.gasCapacity {
		// Check to see if this conflicts with any remaining txs after we evict
		evictedUTXOs, _ := m.consumedUTXOs.DeleteKey(low.TxID)
		if m.consumedUTXOs.HasOverlap(tx.InputIDs()) {
			m.consumedUTXOs.Put(low.TxID, evictedUTXOs)
			return mempool.ErrConflictsWithOtherTx
		}

		evictedTxID, _, _ := m.minHeap.Pop()
		m.remove(evictedTxID)

		return nil
	}

	return ErrGasCapacityExceeded
}

func (m *Mempool) updateMetrics() {
	m.numTxsMetric.Set(float64(m.maxHeap.Len()))
	m.gasAvailableMetric.Set(float64(m.gasCapacity - m.currentGas))
}

func (m *Mempool) Get(txID ids.ID) (*txs.Tx, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

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
	m.lock.Lock()
	defer m.lock.Unlock()

	_, tx, ok := m.maxHeap.Peek()
	return tx.Tx, ok
}

func (m *Mempool) Iterate(f func(tx *txs.Tx) bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, v := range m.maxHeap.UnsortedIterator() {
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
	m.lock.Lock()
	defer m.lock.Unlock()

	err, _ := m.droppedTxIDs.Get(txID)
	return err
}

func (m *Mempool) Len() int {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.maxHeap.Len()
}

func (m *Mempool) meter(tx txs.UnsignedTx) (gas.Dimensions, gas.Gas, error) {
	c, err := fee.TxComplexity(tx)
	if err != nil {
		return gas.Dimensions{}, 0, err
	}

	g, err := c.ToGas(m.weights)
	if err != nil {
		return gas.Dimensions{}, 0, err
	}

	if g == 0 {
		return gas.Dimensions{}, 0, errNoGasUsed
	}

	return c, g, nil
}
