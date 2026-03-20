// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"iter"
	"slices"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/setmap"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
)

// Txs stores the transactions inside of the mempool.
type Txs struct {
	lock sync.RWMutex
	// txs is the collection of transactions available to be included into a
	// block, sorted by gasPrice.
	txs heap.Map[ids.ID, *Tx]
	// utxos maps a txID to the set of utxoIDs it consumes.
	utxos *setmap.SetMap[ids.ID, ids.ID]
}

func NewTxs() *Txs {
	return &Txs{
		txs: heap.NewMap[ids.ID, *Tx](func(a, b *Tx) bool {
			return a.GasPrice.Lt(&b.GasPrice) // Txs is a min-heap
		}),
		utxos: setmap.New[ids.ID, ids.ID](),
	}
}

// Iter returns an iterator of all transactions sorted by decreasing gas price.
func (t *Txs) Iter() iter.Seq[*Tx] {
	t.lock.RLock()
	values := heap.MapValues(t.txs)
	t.lock.RUnlock()

	// Sort by decreasing gas price
	slices.SortFunc(values, func(a, b *Tx) int {
		return -a.GasPrice.Cmp(&b.GasPrice)
	})

	return slices.Values(values)
}

// Iterate applies f to all transactions. If f returns false, the iteration
// stops early.
func (t *Txs) Iterate(f func(tx *tx.Tx) bool) {
	for tx := range t.Iter() {
		if !f(tx.Tx) {
			return
		}
	}
}

// Len returns the number of transactions in the mempool.
func (t *Txs) Len() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.txs.Len()
}

// Has returns true if the mempool contains the transaction.
func (t *Txs) Has(txID ids.ID) bool {
	t.lock.RLock()
	defer t.lock.RUnlock()

	_, ok := t.txs.Get(txID)
	return ok
}

// GetTx returns the transaction if it is in the mempool.
func (t *Txs) Get(txID ids.ID) (*tx.Tx, bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	tx, ok := t.txs.Get(txID)
	return tx.Tx, ok
}

// RemoveConflicts removes all transactions consuming any of the given UTXOs.
func (t *Txs) RemoveConflicts(utxos set.Set[ids.ID]) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.removeConflicts(utxos)
}

func (t *Txs) removeConflicts(utxos set.Set[ids.ID]) {
	for _, removed := range t.utxos.DeleteOverlapping(utxos) {
		t.txs.Remove(removed.Key)
	}
}
