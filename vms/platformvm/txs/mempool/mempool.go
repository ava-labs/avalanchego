// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/btree"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
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

type meteredTx struct {
	*txs.Tx
	GasPrice float64

	gasUsed gas.Gas
}

type Mempool struct {
	weights     gas.Dimensions
	gasCapacity gas.Gas
	avaxAssetID ids.ID

	lock               sync.RWMutex
	cond *lock.Cond // TODO (?)
	tree *btree.BTreeG[meteredTx]
	txs  map[ids.ID]meteredTx
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
		tree: btree.NewG[meteredTx](2, func(a, b meteredTx) bool {
			if a.GasPrice != b.GasPrice {
				return a.GasPrice < b.GasPrice
			}

			// Break ties
			return a.TxID.Compare(b.TxID) < 0
		}),
		txs: make(map[ids.ID]meteredTx),
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
		producedAVAX, err = math.Add(producedAVAX, utxo.Out.Amount())
		if err != nil {
			return fmt.Errorf("failed to add produced AVAX: %w", err)
		}
	}

	gasUsed, err := m.meter(tx.Unsigned)
	if err != nil {
		return fmt.Errorf("failed to meter tx: %w", err)
	}

	meteredTx := meteredTx{
		Tx:       tx,
		gasUsed:  gasUsed,
		GasPrice: float64(consumedAVAX-producedAVAX) / float64(gasUsed),
	}

	// Try to evict lower gas priced txs if we do not have enough remaining gas
	// capacity
	if gasRemaining := m.gasCapacity - gasUsed; m.currentGas > gasRemaining {
		gasToFree := m.currentGas - gasRemaining
		if err := m.tryEvictTxs(gasToFree, meteredTx); err != nil {
			return err
		}
	}

	m.tree.ReplaceOrInsert(meteredTx)
	m.txs[tx.TxID] = meteredTx
	m.consumedUTXOs.Put(tx.TxID, tx.InputIDs())
	m.droppedTxIDs.Evict(tx.TxID)
	m.currentGas += gasUsed

	m.updateMetrics()
	m.cond.Broadcast()

	return nil
}

func (m *Mempool) tryEvictTxs(gasToFree gas.Gas, txToAdd meteredTx) error {
	gasFreed := gas.Gas(0)
	toEvict := make([]ids.ID, 0)

	m.tree.Ascend(func(item meteredTx) bool {
		// Try to evict lower priced txs to make room for the new tx
		if item.GasPrice >= txToAdd.GasPrice {
			return false
		}

		txID := item.Tx.TxID
		toEvict = append(toEvict, txID)
		gasFreed += item.gasUsed

		return gasFreed < gasToFree
	})

	if gasFreed < gasToFree {
		return ErrGasCapacityExceeded
	}

	for _, txID := range toEvict {
		m.remove(txID)
	}

	return nil
}

func (m *Mempool) updateMetrics() {
	m.numTxsMetric.Set(float64(m.tree.Len()))
	m.gasAvailableMetric.Set(float64(m.gasCapacity - m.currentGas))
}

func (m *Mempool) Get(txID ids.ID) (*txs.Tx, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	tx, ok := m.txs[txID]
	return tx.Tx, ok
}

func (m *Mempool) Remove(txID ids.ID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.remove(txID)
}

func (m *Mempool) remove(txID ids.ID) {
	removedTx, ok := m.txs[txID]
	if !ok {
		return
	}

	delete(m.txs, txID)
	m.tree.Delete(removedTx)
	m.consumedUTXOs.DeleteKey(txID)

	m.currentGas -= removedTx.gasUsed

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

	tx, ok := m.tree.Max()
	return tx.Tx, ok
}

func (m *Mempool) Iterate(f func(tx *txs.Tx) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	m.tree.Ascend(func(item meteredTx) bool {
		return f(item.Tx)
	})
}

func (m *Mempool) MarkDropped(txID ids.ID, reason error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.txs[txID]; ok {
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

	return m.tree.Len()
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

	if g > m.gasCapacity {
		return 0, ErrGasCapacityExceeded
	}

	if g == 0 {
		return 0, errNoGasUsed
	}

	return g, nil
}

func (m *Mempool) WaitForEvent(ctx context.Context) (common.Message, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for m.tree.Len() == 0 {
		if err := m.cond.Wait(ctx); err != nil {
			return 0, err
		}
	}
	return common.PendingTxs, nil
}
