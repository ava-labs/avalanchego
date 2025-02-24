// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"

	txmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	ErrCantIssueAdvanceTimeTx     = errors.New("can not issue an advance time tx")
	ErrCantIssueRewardValidatorTx = errors.New("can not issue a reward validator tx")

	_ meterer = (*noMeter)(nil)
)

type Tx struct {
	*txs.Tx
	Complexity gas.Dimensions
	Gas        gas.Gas
}

type meterer interface {
	Meter(tx txs.UnsignedTx) (gas.Dimensions, gas.Gas, error)
}

type noMeter struct{}

func (noMeter) Meter(txs.UnsignedTx) (gas.Dimensions, gas.Gas, error) {
	return gas.Dimensions{}, 0, nil
}

type dynamicMeter struct {
	weights gas.Dimensions
}

func (d dynamicMeter) Meter(tx txs.UnsignedTx) (gas.Dimensions, gas.Gas, error) {
	c, err := fee.TxComplexity(tx)
	if err != nil {
		return gas.Dimensions{}, 0, err
	}

	g, err := c.ToGas(d.weights)
	if err != nil {
		return gas.Dimensions{}, 0, err
	}

	return c, g, nil
}

type Mempool struct {
	mempool  txmempool.Mempool[*txs.Tx]
	toEngine chan<- common.Message

	meterer meterer
	lock    sync.Mutex
	heap    heap.Map[ids.ID, Tx]
}

func New(
	cfg *config.Internal,
	namespace string,
	registerer prometheus.Registerer,
	timestamp time.Time,
	toEngine chan<- common.Message,
) (*Mempool, error) {
	metrics, err := txmempool.NewMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}
	pool := txmempool.New[*txs.Tx](
		metrics,
	)

	var meterer meterer = noMeter{}
	if cfg.UpgradeConfig.IsEtnaActivated(timestamp) {
		meterer = dynamicMeter{
			weights: cfg.DynamicFeeConfig.Weights,
		}
	}

	return &Mempool{
		mempool: pool,
		meterer: meterer,
		heap: heap.NewMap[ids.ID, Tx](func(a, b Tx) bool {
			return a.Gas > b.Gas
		}),
		toEngine: toEngine,
	}, nil
}

func (m *Mempool) Add(tx *txs.Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	switch tx.Unsigned.(type) {
	case *txs.AdvanceTimeTx:
		return ErrCantIssueAdvanceTimeTx
	case *txs.RewardValidatorTx:
		return ErrCantIssueRewardValidatorTx
	default:
	}

	complexity, gas, err := m.meterer.Meter(tx.Unsigned)
	if err != nil {
		return err
	}

	if err := m.mempool.Add(tx); err != nil {
		return fmt.Errorf("failed to add tx to mempool: %w", err)
	}

	heapTx := Tx{
		Tx:         tx,
		Complexity: complexity,
		Gas:        gas,
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

	m.mempool.Iterate(func(tx *txs.Tx) bool {
		// The gas heap is guaranteed to be in-sync with the mempool
		heapTx, _ := m.heap.Get(tx.ID())

		return f(heapTx)
	})
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
