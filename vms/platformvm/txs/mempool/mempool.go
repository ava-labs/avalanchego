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

	txmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	ErrNotEnoughGas = errors.New("not enough gas")
	errNoGasUsed    = errors.New("no gas used")
	errAVAXMinted   = errors.New("AVAX minted")
)

type Mempool interface {
	// Add adds `tx` to the mempool and clears its dropped status.
	Add(tx *txs.Tx) error
	// Get returns the tx corresponding to `txID` and if it was present
	Get(txID ids.ID) (*txs.Tx, bool)
	// GetDropReason returns why `txID` was dropped
	GetDropReason(txID ids.ID) error
	// Iterate calls `f` over each tx in the mempool
	Iterate(f func(tx *txs.Tx) bool)
	// len returns the number of txs in the mempool
	Len() int
	// MarkDropped marks `txID` as dropped
	MarkDropped(txID ids.ID, reason error)
	// Peek returns a tx in the mempool and if it was present
	Peek() (*txs.Tx, bool)
	// Remove removes `txID` from the mempool
	Remove(txID ids.ID)
	// RemoveConflicts removes all txs conflicting with `utxos`
	RemoveConflicts(utxos set.Set[ids.ID])
	// WaitForEvent blocks until the mempool has txs that are ready to build into
	// a block.
	WaitForEvent(ctx context.Context) (common.Message, error)
}

type meteredTx struct {
	*txs.Tx
	// gasPrice is the amount of AVAX burned per unit of gas used by this tx
	gasPrice float64
	gasUsed  gas.Gas
}

type mempool struct {
	weights     gas.Dimensions
	avaxAssetID ids.ID

	lock               sync.RWMutex
	cond               *lock.Cond
	tree               *btree.BTreeG[meteredTx]
	txs                map[ids.ID]meteredTx
	consumedUTXOs      *setmap.SetMap[ids.ID, ids.ID]
	droppedTxIDs       *lru.Cache[ids.ID, error]
	gasAvailable       gas.Gas
	numTxsMetric       prometheus.Gauge
	gasAvailableMetric prometheus.Gauge
}

func New(
	namespace string,
	weights gas.Dimensions,
	gasCapacity gas.Gas,
	avaxAssetID ids.ID,
	registerer prometheus.Registerer,
) (Mempool, error) {
	numTxsMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "count",
		Help:      "number of transactions in the mempool",
	})

	gasAvailableMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "gas_available",
		Help:      "amount of gas available",
	})

	if err := errors.Join(
		registerer.Register(numTxsMetric),
		registerer.Register(gasAvailableMetric),
	); err != nil {
		return nil, fmt.Errorf("failed to register metrics: %w", err)
	}

	m := &mempool{
		weights:     weights,
		avaxAssetID: avaxAssetID,
		tree: btree.NewG[meteredTx](2, func(a, b meteredTx) bool {
			if a.gasPrice != b.gasPrice {
				return a.gasPrice < b.gasPrice
			}

			// Break ties with txID
			return a.TxID.Compare(b.TxID) < 0
		}),
		txs:                make(map[ids.ID]meteredTx),
		consumedUTXOs:      setmap.New[ids.ID, ids.ID](),
		droppedTxIDs:       lru.NewCache[ids.ID, error](64),
		gasAvailable:       gasCapacity,
		numTxsMetric:       numTxsMetric,
		gasAvailableMetric: gasAvailableMetric,
	}

	m.cond = lock.NewCond(&m.lock)
	return m, nil
}

func (m *mempool) Add(tx *txs.Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.consumedUTXOs.HasOverlap(tx.InputIDs()) {
		return txmempool.ErrConflictsWithOtherTx
	}

	meteredTx, err := m.meter(tx)
	if err != nil {
		return fmt.Errorf("failed to meter tx: %w", err)
	}

	// Try to evict lower gas priced txs if we do not have enough remaining gas
	// capacity
	if err := m.allocateSpace(meteredTx); err != nil {
		return err
	}

	m.tree.ReplaceOrInsert(meteredTx)
	m.txs[meteredTx.TxID] = meteredTx
	m.consumedUTXOs.Put(meteredTx.TxID, meteredTx.InputIDs())
	m.droppedTxIDs.Evict(meteredTx.TxID)
	m.gasAvailable -= meteredTx.gasUsed

	m.updateMetrics()
	m.cond.Broadcast()

	return nil
}

// Try to evict transactions until there is enough capacity for the tx.
// Returns if any txs were evicted.
func (m *mempool) allocateSpace(txToAdd meteredTx) error {
	// We have enough space for this tx
	if txToAdd.gasUsed <= m.gasAvailable {
		return nil
	}

	gasToFree := txToAdd.gasUsed - m.gasAvailable
	gasFreed := gas.Gas(0)
	var toEvict []ids.ID

	m.tree.Ascend(func(item meteredTx) bool {
		// Try to evict lower priced txs to make room for the new tx
		if item.gasPrice >= txToAdd.gasPrice {
			return false
		}

		txID := item.Tx.TxID
		toEvict = append(toEvict, txID)
		gasFreed += item.gasUsed

		return gasFreed < gasToFree
	})

	// We do not have enough space for this tx
	if gasFreed < gasToFree {
		return ErrNotEnoughGas
	}

	for _, txID := range toEvict {
		m.remove(txID)
	}

	return nil
}

func (m *mempool) meter(tx *txs.Tx) (meteredTx, error) {
	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx.Unsigned)
	if err != nil {
		return meteredTx{}, fmt.Errorf("getting utxos %w", err)
	}

	consumedAVAX := uint64(0)
	for _, utxo := range ins {
		if utxo.AssetID() != m.avaxAssetID {
			continue
		}

		// The caller should verify txs but perform overflow checks anyway
		consumedAVAX, err = math.Add(consumedAVAX, utxo.In.Amount())
		if err != nil {
			return meteredTx{}, fmt.Errorf("failed to add consumed AVAX: %w", err)
		}
	}

	for _, utxo := range outs {
		if utxo.AssetID() != m.avaxAssetID {
			continue
		}

		// The caller should verify txs but perform overflow checks anyway
		producedAVAX, err = math.Add(producedAVAX, utxo.Out.Amount())
		if err != nil {
			return meteredTx{}, fmt.Errorf("failed to add produced AVAX: %w", err)
		}
	}

	// The caller should verify txs but perform this check anyway
	if consumedAVAX < producedAVAX {
		return meteredTx{}, errAVAXMinted
	}

	c, err := fee.TxComplexity(tx.Unsigned)
	if err != nil {
		return meteredTx{}, err
	}

	gasUsed, err := c.ToGas(m.weights)
	if err != nil {
		return meteredTx{}, err
	}

	if gasUsed == 0 {
		return meteredTx{}, errNoGasUsed
	}

	return meteredTx{
		Tx:       tx,
		gasUsed:  gasUsed,
		gasPrice: float64(consumedAVAX-producedAVAX) / float64(gasUsed),
	}, nil
}

func (m *mempool) updateMetrics() {
	m.numTxsMetric.Set(float64(m.tree.Len()))
	m.gasAvailableMetric.Set(float64(m.gasAvailable))
}

func (m *mempool) Get(txID ids.ID) (*txs.Tx, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	tx, ok := m.txs[txID]
	return tx.Tx, ok
}

func (m *mempool) Remove(txID ids.ID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.remove(txID)
}

func (m *mempool) remove(txID ids.ID) {
	removedTx, ok := m.txs[txID]
	if !ok {
		return
	}

	delete(m.txs, txID)
	m.tree.Delete(removedTx)
	m.consumedUTXOs.DeleteKey(txID)

	m.gasAvailable += removedTx.gasUsed

	m.updateMetrics()
}

func (m *mempool) RemoveConflicts(utxos set.Set[ids.ID]) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, removed := range m.consumedUTXOs.DeleteOverlapping(utxos) {
		m.remove(removed.Key)
	}
}

func (m *mempool) Peek() (*txs.Tx, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	tx, ok := m.tree.Max()
	return tx.Tx, ok
}

func (m *mempool) Iterate(f func(tx *txs.Tx) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	m.tree.Descend(func(item meteredTx) bool {
		return f(item.Tx)
	})
}

func (m *mempool) MarkDropped(txID ids.ID, reason error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.txs[txID]; ok {
		return
	}

	m.droppedTxIDs.Put(txID, reason)
}

func (m *mempool) GetDropReason(txID ids.ID) error {
	m.lock.RLock()
	defer m.lock.RUnlock()

	err, _ := m.droppedTxIDs.Get(txID)
	return err
}

func (m *mempool) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.tree.Len()
}

func (m *mempool) WaitForEvent(ctx context.Context) (common.Message, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	// TODO block until the mempool has a gas price greater than or equal to the
	// chain's minimum gas price
	for m.tree.Len() == 0 {
		if err := m.cond.Wait(ctx); err != nil {
			return 0, err
		}
	}

	return common.PendingTxs, nil
}
