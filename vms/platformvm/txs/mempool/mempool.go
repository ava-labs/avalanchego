// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"

	txmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var _ txs.Visitor = (*utxoGetter)(nil)

type Tx struct {
	*txs.Tx
	Complexity gas.Dimensions
	GasPrice   float64
}

type Mempool struct {
	avaxAssetID ids.ID
	weights     gas.Dimensions
	toEngine    chan<- common.Message

	mempool txmempool.Mempool[*txs.Tx]

	lock sync.Mutex
	heap heap.Map[ids.ID, Tx]
}

func New(
	weights gas.Dimensions,
	namespace string,
	registerer prometheus.Registerer,
	toEngine chan<- common.Message,
	avaxAssetID ids.ID,
) (*Mempool, error) {
	metrics, err := txmempool.NewMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}
	pool := txmempool.New[*txs.Tx](
		metrics,
	)

	return &Mempool{
		avaxAssetID: avaxAssetID,
		weights:     weights,
		mempool:     pool,
		heap: heap.NewMap[ids.ID, Tx](
			func(a, b Tx) bool {
				return a.GasPrice > b.GasPrice
			},
		),
		toEngine: toEngine,
	}, nil
}

func (m *Mempool) Add(tx *txs.Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	complexity, gasUsed, err := m.meter(tx.Unsigned)
	if err != nil {
		return err
	}

	consumedAVAX, producedAVAX, err := getUTXOs(m.avaxAssetID, tx.Unsigned)
	if err != nil {
		return fmt.Errorf("failed to get utxos: %w", err)
	}

	feesPaid := consumedAVAX - producedAVAX
	gasPrice := float64(0)
	if gasUsed == 0 {
		gasPrice = 0
	} else {
		gasPrice = float64(feesPaid) / float64(gasUsed)
	}

	if err := m.mempool.Add(tx); err != nil {
		return fmt.Errorf("failed to add tx to mempool: %w", err)
	}

	heapTx := Tx{
		Tx:         tx,
		Complexity: complexity,
		GasPrice:   gasPrice,
	}

	m.heap.Push(tx.TxID, heapTx)

	return nil
}

func (m *Mempool) Get(txID ids.ID) (Tx, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.heap.Get(txID)
}

func (m *Mempool) Remove(txs ...*txs.Tx) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.mempool.Remove(txs...)

	for _, tx := range txs {
		m.heap.Remove(tx.TxID)
	}
}

func (m *Mempool) Peek() (Tx, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	_, tx, ok := m.heap.Peek()

	return tx, ok
}

func (m *Mempool) Iterate(f func(tx Tx) bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.mempool.Iterate(
		func(tx *txs.Tx) bool {
			// The gas heap is guaranteed to be in-sync with the mempool
			heapTx, _ := m.heap.Get(tx.ID())

			return f(heapTx)
		},
	)
}

func (m *Mempool) MarkDropped(txID ids.ID, reason error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.mempool.MarkDropped(txID, reason)
}

func (m *Mempool) GetDropReason(txID ids.ID) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.mempool.GetDropReason(txID)
}

func (m *Mempool) Len() int {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.mempool.Len()
}

func (m *Mempool) RequestBuildBlock(emptyBlockPermitted bool) {
	if !emptyBlockPermitted && m.Len() == 0 {
		return
	}

	select {
	case m.toEngine <- common.PendingTxs:
	default:
	}
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
	u := utxoGetter{
		AVAXAssetID: avaxAssetID,
	}
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
