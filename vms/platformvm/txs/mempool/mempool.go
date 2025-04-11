// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	txmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

type Tx struct {
	*txs.Tx
	Complexity gas.Dimensions
	GasPrice   *big.Rat
}

type Mempool struct {
	avaxAssetID ids.ID
	utxos       avax.UTXOState
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
	utxos avax.UTXOState,
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
		utxos:       utxos,
		weights:     weights,
		mempool:     pool,
		heap: heap.NewMap[ids.ID, Tx](
			func(a, b Tx) bool {
				return a.GasPrice.Cmp(b.GasPrice) == 1
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

	totalConsumedAvax := &big.Int{}
	for utxoID := range tx.InputIDs() {
		utxo, err := m.utxos.GetUTXO(utxoID)
		if err != nil {
			return fmt.Errorf("failed to get utxo: %w", err)
		}

		if utxo.AssetID() != m.avaxAssetID {
			continue
		}

		output, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			return errors.New("unexpected output type")
		}

		totalConsumedAvax.Add(totalConsumedAvax, big.NewInt(int64(output.Amt)))
	}

	totalProducedAvax := &big.Int{}
	for _, utxo := range tx.UTXOs() {
		if utxo.AssetID() != m.avaxAssetID {
			continue
		}

		output, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			return errors.New("unexpected output type")
		}

		totalProducedAvax.Add(totalProducedAvax, big.NewInt(int64(output.Amt)))
	}

	feesPaid := &big.Int{}
	feesPaid.Sub(totalConsumedAvax, totalProducedAvax)

	if feesPaid.Cmp(big.NewInt(0)) <= 0 {
		return errors.New("fees must be greater than zero")
	}

	gasPrice := &big.Rat{}
	gasPrice.SetFrac(feesPaid, big.NewInt(int64(gasUsed)))

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
