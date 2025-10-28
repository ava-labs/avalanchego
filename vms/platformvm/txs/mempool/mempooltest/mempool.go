// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempooltest

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/lock"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

var _ mempool.Mempool = (*Mempool)(nil)

// Mempool performs no verification and has no upper bound on txs
type Mempool struct {
	txs  map[ids.ID]*txs.Tx
	drop map[ids.ID]error

	mu   sync.Mutex
	cond lock.Cond
}

func (m *Mempool) initTxs() {
	if m.txs != nil {
		return
	}

	m.txs = make(map[ids.ID]*txs.Tx)
}

func (m *Mempool) Add(tx *txs.Tx) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.txs[tx.ID()] = tx
	return nil
}

func (m *Mempool) Get(txID ids.ID) (*txs.Tx, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.initTxs()

	tx, ok := m.txs[txID]
	return tx, ok
}

func (m *Mempool) GetDropReason(txID ids.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.drop == nil {
		m.drop = make(map[ids.ID]error)
	}

	err := m.drop[txID]
	return err
}

func (m *Mempool) Iterate(fn func(tx *txs.Tx) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, tx := range m.txs {
		if !fn(tx) {
			return
		}
	}
}

func (m *Mempool) Len() int {
	return len(m.txs)
}

func (m *Mempool) MarkDropped(txID ids.ID, reason error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.drop[txID] = reason
}

func (m *Mempool) Peek() (*txs.Tx, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.initTxs()

	for _, tx := range m.txs {
		return tx, true
	}

	return nil, false
}

func (m *Mempool) Remove(txID ids.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.txs, txID)
}

func (m *Mempool) RemoveConflicts(utxos set.Set[ids.ID]) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, tx := range m.txs {
		if inputs := tx.Unsigned.InputIDs(); !inputs.Overlaps(utxos) {
			continue
		}

		delete(m.txs, tx.ID())
	}
}

func (m *Mempool) WaitForEvent(ctx context.Context) (common.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for len(m.txs) == 0 {
		if err := m.cond.Wait(ctx); err != nil {
			return 0, err
		}
	}

	return common.PendingTxs, nil
}
