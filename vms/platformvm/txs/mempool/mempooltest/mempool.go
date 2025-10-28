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

var _ mempool.Mempool = (*FakeMempool)(nil)

// FakeMempool performs no verification and has no upper bound on txs
type FakeMempool struct {
	txs  map[ids.ID]*txs.Tx
	drop map[ids.ID]error

	mu   sync.Mutex
	cond lock.Cond
}

func (f *FakeMempool) initTxs() {
	if f.txs != nil {
		return
	}

	f.txs = make(map[ids.ID]*txs.Tx)
}

func (f *FakeMempool) Add(tx *txs.Tx) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.txs[tx.ID()] = tx
	return nil
}

func (f *FakeMempool) Get(txID ids.ID) (*txs.Tx, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.initTxs()

	tx, ok := f.txs[txID]
	return tx, ok
}

func (f *FakeMempool) GetDropReason(txID ids.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.drop == nil {
		f.drop = make(map[ids.ID]error)
	}

	err := f.drop[txID]
	return err
}

func (f *FakeMempool) Iterate(fn func(tx *txs.Tx) bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, tx := range f.txs {
		if !fn(tx) {
			return
		}
	}
}

func (f *FakeMempool) Len() int {
	return len(f.txs)
}

func (f *FakeMempool) MarkDropped(txID ids.ID, reason error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.drop[txID] = reason
}

func (f *FakeMempool) Peek() (*txs.Tx, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.initTxs()

	for _, tx := range f.txs {
		return tx, true
	}

	return nil, false
}

func (f *FakeMempool) Remove(txID ids.ID) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.txs, txID)
}

func (f *FakeMempool) RemoveConflicts(utxos set.Set[ids.ID]) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, tx := range f.txs {
		if inputs := tx.Unsigned.InputIDs(); !inputs.Overlaps(utxos) {
			continue
		}

		delete(f.txs, tx.ID())
	}
}

func (f *FakeMempool) WaitForEvent(ctx context.Context) (common.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for len(f.txs) == 0 {
		if err := f.cond.Wait(ctx); err != nil {
			return 0, err
		}
	}

	return common.PendingTxs, nil
}
