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
	"github.com/ava-labs/avalanchego/utils/setmap"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	ErrGasCapacityExceeded = errors.New("gas capacity exceeded")

	_ txs.Visitor = (*utxoGetter)(nil)
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

	_, gasUsed, err := m.meter(tx.Unsigned)
	if err != nil {
		return fmt.Errorf("failed to meter tx: %w", err)
	}

	consumedAVAX, producedAVAX, err := getUTXOs(m.avaxAssetID, tx.Unsigned)
	if err != nil {
		return fmt.Errorf("failed to get utxos: %w", err)
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
		return nil
	}

	// Check if evicting the lowest paying tx would give us enough capacity to fit
	// this tx
	if low.GasPrice < tx.GasPrice && next-low.gasUsed <= m.gasCapacity {
		// Check to see if this conflicts with any remaining txs
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

// TODO remove variadic param
// TODO support removal by inputs
// TODO remove conflicts of tx
func (m *Mempool) Remove(txs ...*txs.Tx) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, tx := range txs {
		m.remove(tx.ID())
	}
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

	return c, g, nil
}

func getUTXOs(avaxAssetID ids.ID, tx txs.UnsignedTx) (uint64, uint64, error) {
	u := utxoGetter{AVAXAssetID: avaxAssetID}

	if err := tx.Visit(&u); err != nil {
		return 0, 0, err
	}

	return u.Consumed, u.Produced, nil
}

type utxoGetter struct {
	AVAXAssetID ids.ID
	Consumed    uint64
	Produced    uint64
}

func (*utxoGetter) AddValidatorTx(*txs.AddValidatorTx) error {
	return errors.New("unsupported tx type")
}

func (u *utxoGetter) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	u.getUTXOs(tx.BaseTx)

	return nil
}

func (*utxoGetter) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return errors.New("unsupported tx type")
}

func (u *utxoGetter) CreateChainTx(tx *txs.CreateChainTx) error {
	u.getUTXOs(tx.BaseTx)

	return nil
}

func (u *utxoGetter) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	u.getUTXOs(tx.BaseTx)

	return nil
}

func (u *utxoGetter) ImportTx(tx *txs.ImportTx) error {
	for _, i := range tx.ImportedInputs {
		if i.AssetID() != u.AVAXAssetID {
			continue
		}

		u.Consumed += i.In.Amount()
	}

	u.getUTXOs(tx.BaseTx)

	return nil
}

func (u *utxoGetter) ExportTx(tx *txs.ExportTx) error {
	for _, i := range tx.ExportedOutputs {
		if i.AssetID() != u.AVAXAssetID {
			continue
		}

		u.Produced += i.Out.Amount()
	}

	u.getUTXOs(tx.BaseTx)

	return nil
}

func (*utxoGetter) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return errors.New("unsupported tx type")
}

func (*utxoGetter) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return errors.New("unsupported tx type")
}

func (u *utxoGetter) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	u.getUTXOs(tx.BaseTx)

	return nil
}

func (*utxoGetter) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return errors.New("unsupported tx type")
}

func (u *utxoGetter) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	u.getUTXOs(tx.BaseTx)

	return nil
}

func (u *utxoGetter) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	u.getUTXOs(tx.BaseTx)

	return nil
}

func (u *utxoGetter) TransferSubnetOwnershipTx(tx *txs.TransferSubnetOwnershipTx) error {
	u.getUTXOs(tx.BaseTx)

	return nil
}

func (u *utxoGetter) BaseTx(tx *txs.BaseTx) error {
	u.getUTXOs(*tx)

	return nil
}

func (u *utxoGetter) ConvertSubnetToL1Tx(tx *txs.ConvertSubnetToL1Tx) error {
	u.getUTXOs(tx.BaseTx)

	return nil
}

func (u *utxoGetter) RegisterL1ValidatorTx(tx *txs.RegisterL1ValidatorTx) error {
	u.getUTXOs(tx.BaseTx)

	return nil
}

func (u *utxoGetter) SetL1ValidatorWeightTx(tx *txs.SetL1ValidatorWeightTx) error {
	u.getUTXOs(tx.BaseTx)

	return nil
}

func (u *utxoGetter) IncreaseL1ValidatorBalanceTx(tx *txs.IncreaseL1ValidatorBalanceTx) error {
	u.getUTXOs(tx.BaseTx)

	return nil
}

func (u *utxoGetter) DisableL1ValidatorTx(tx *txs.DisableL1ValidatorTx) error {
	u.getUTXOs(tx.BaseTx)

	return nil
}

func (u *utxoGetter) getUTXOs(tx txs.BaseTx) {
	for _, i := range tx.Ins {
		if i.AssetID() != u.AVAXAssetID {
			continue
		}

		u.Consumed += i.In.Amount()
	}

	for _, i := range tx.Outs {
		if i.AssetID() != u.AVAXAssetID {
			continue
		}

		u.Produced += i.Out.Amount()
	}
}
