// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"sync"

	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

const discardedTxsCacheSize = 50

// Txs stores the transactions inside of the mempool.
//
// Transactions in the mempool can be in 1 of 4 statuses:
//
//   - Pending: Pending transactions are eligible for the block builder to
//     attempt to include in the next block being built.
//   - Current: Current transactions are included inside of a block currently
//     being built.
//   - Issued: Issued transactions were included inside of a block built by this
//     node.
//   - Discarded: Discarded transactions were previously in the the mempool, but
//     were then deemed to be invalid. To prevent additional future work, these
//     transactions may be assumed to be invalid in the future.
type Txs struct {
	ctx     *snow.Context
	metrics *metrics
	// maxSize is the maximum number of transactions allowed to be kept in
	// mempool.
	maxSize int
	// pending is a channel of length one, which the mempool uses to awake the
	// block builder when a new transaction is added.
	pending chan struct{}

	lock sync.RWMutex

	// pendingTxs is the collection of transactions available to be included
	// into a block, sorted by gasPrice.
	pendingTxs *txHeap
	// currentTxs is the set of transactions that have been included into a
	// block that is currently being built.
	currentTxs map[ids.ID]*atomic.Tx
	// issuedTxs is the set of transactions that have been included into a
	// block that was previously built.
	issuedTxs map[ids.ID]*atomic.Tx

	// utxoSpenders maps utxoIDs to the Pending, Current, or Issued transaction
	// consuming them in the mempool. This map does not store references to
	// Discarded transactions.
	utxoSpenders map[ids.ID]*atomic.Tx

	// discardedTxs is an LRU Cache of transactions that have been discarded
	// after failing verification.
	discardedTxs *lru.Cache[ids.ID, *atomic.Tx]
}

func NewTxs(ctx *snow.Context, maxSize int) *Txs {
	return &Txs{
		ctx:          ctx,
		metrics:      newMetrics(),
		maxSize:      maxSize,
		pending:      make(chan struct{}, 1),
		pendingTxs:   newTxHeap(maxSize),
		currentTxs:   make(map[ids.ID]*atomic.Tx),
		issuedTxs:    make(map[ids.ID]*atomic.Tx),
		utxoSpenders: make(map[ids.ID]*atomic.Tx),
		discardedTxs: lru.NewCache[ids.ID, *atomic.Tx](discardedTxsCacheSize),
	}
}

// PendingLen returns the number of pending transactions.
func (t *Txs) PendingLen() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.pendingTxs.Len()
}

// Iterate applies f to all Pending transactions. If f returns false, the
// iteration stops early.
func (t *Txs) Iterate(f func(tx *atomic.Tx) bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	for _, item := range t.pendingTxs.maxHeap.items {
		if !f(item.tx) {
			return
		}
	}
}

// NextTx returns the highest paying Pending transaction from the mempool and
// marks it as Current.
func (t *Txs) NextTx() (*atomic.Tx, bool) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if t.pendingTxs.Len() == 0 {
		return nil, false
	}

	tx := t.pendingTxs.PopMax()
	t.currentTxs[tx.ID()] = tx
	t.metrics.pendingTxs.Update(int64(t.pendingTxs.Len()))
	t.metrics.currentTxs.Update(int64(len(t.currentTxs)))
	return tx, true
}

// GetPendingTx returns the transaction if it is Pending.
func (t *Txs) GetPendingTx(txID ids.ID) (*atomic.Tx, bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.pendingTxs.Get(txID)
}

// GetTx returns the transaction along with if it is Discarded.
func (t *Txs) GetTx(txID ids.ID) (tx *atomic.Tx, discarded bool, found bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	if tx, ok := t.pendingTxs.Get(txID); ok {
		return tx, false, true
	}
	if tx, ok := t.issuedTxs[txID]; ok {
		return tx, false, true
	}
	if tx, ok := t.currentTxs[txID]; ok {
		return tx, false, true
	}
	if tx, ok := t.discardedTxs.Get(txID); ok {
		return tx, true, true
	}
	return nil, false, false
}

// Has returns true if the mempool contains the transaction in either the
// Pending, Current, or Issued state.
func (t *Txs) Has(txID ids.ID) bool {
	_, dropped, found := t.GetTx(txID)
	return found && !dropped
}

// IssueCurrentTxs marks all Current transactions as Issued.
func (t *Txs) IssueCurrentTxs() {
	t.lock.Lock()
	defer t.lock.Unlock()

	for txID, tx := range t.currentTxs {
		t.issuedTxs[txID] = tx
		delete(t.currentTxs, txID)
	}
	t.metrics.issuedTxs.Update(int64(len(t.issuedTxs)))
	t.metrics.currentTxs.Update(0)
}

// CancelCurrentTx attempts to mark the Current transaction as Pending.
//
// This should be called after [Txs.NextTx] returned the transaction and it
// couldn't be included in the block, but should not be discarded. For example,
// CancelCurrentTx should be called if including the transaction will put the
// block above the atomic tx gas limit.
func (t *Txs) CancelCurrentTx(txID ids.ID) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if tx, ok := t.currentTxs[txID]; ok {
		t.cancelTx(tx)
	}
}

// CancelCurrentTxs attempts to mark all Current transactions as Pending.
//
// This should be called after building a block failed due to an error unrelated
// to the transactions.
func (t *Txs) CancelCurrentTxs() {
	t.lock.Lock()
	defer t.lock.Unlock()

	for _, tx := range t.currentTxs {
		t.cancelTx(tx)
	}
}

// cancelTx attempts to mark the Current transaction as Pending. If the tx can
// not be marked as Pending, it will be Discarded.
//
// Assumes the lock is held.
func (t *Txs) cancelTx(tx *atomic.Tx) {
	txID := tx.ID()
	gasPrice, err := atomic.EffectiveGasPrice(tx, t.ctx.AVAXAssetID, true)
	// Should never error to calculate the gas price of a transaction already in
	// the mempool
	if err != nil {
		log.Error("failed to calculate atomic tx gas price while canceling current tx",
			"txID", txID,
			"err", err,
		)
		t.discardCurrentTx(tx)
		return
	}

	delete(t.currentTxs, txID)
	t.metrics.currentTxs.Update(int64(len(t.currentTxs)))

	t.pendingTxs.Push(tx, gasPrice)
	t.metrics.pendingTxs.Update(int64(t.pendingTxs.Len()))
}

// DiscardCurrentTx marks the Current transaction as Discarded.
//
// This should be called after [Txs.NextTx] returned the transaction and it
// failed verification. For example, DiscardCurrentTx should be called if
// including the transaction would produce a conflict with an ancestor block.
func (t *Txs) DiscardCurrentTx(txID ids.ID) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if tx, ok := t.currentTxs[txID]; ok {
		t.discardCurrentTx(tx)
	}
}

// DiscardCurrentTxs marks all Current transactions as Discarded.
//
// This should be called after building a block failed due to an error related
// to the transactions.
func (t *Txs) DiscardCurrentTxs() {
	t.lock.Lock()
	defer t.lock.Unlock()

	for _, tx := range t.currentTxs {
		t.discardCurrentTx(tx)
	}
}

// discardCurrentTx marks the Current transaction as Discarded.
//
// Assumes the lock is held.
func (t *Txs) discardCurrentTx(tx *atomic.Tx) {
	txID := tx.ID()
	delete(t.currentTxs, txID)
	t.metrics.currentTxs.Update(int64(len(t.currentTxs)))

	t.removeSpenders(tx)

	t.discardedTxs.Put(txID, tx)
	t.metrics.discardedTxs.Inc(1)
}

// removeTx removes the transaction from the mempool. If discard is set, the
// transaction will be kept as Discarded, otherwise it will no longer have any
// status.
//
// Note: removeTx will delete all UTXO entries from utxoSpenders. This means
// that when replacing a conflicting tx, removeTx must be called for all
// conflicts before overwriting the utxoSpenders map.
//
// Assumes lock is held.
func (t *Txs) removeTx(tx *atomic.Tx, discard bool) {
	txID := tx.ID()
	t.pendingTxs.Remove(txID)
	delete(t.currentTxs, txID)
	delete(t.issuedTxs, txID)
	t.metrics.pendingTxs.Update(int64(t.pendingTxs.Len()))
	t.metrics.currentTxs.Update(int64(len(t.currentTxs)))
	t.metrics.issuedTxs.Update(int64(len(t.issuedTxs)))

	t.removeSpenders(tx)

	if discard {
		t.discardedTxs.Put(txID, tx)
		t.metrics.discardedTxs.Inc(1)
	} else {
		t.discardedTxs.Evict(txID)
	}
}

// removeSpenders deletes the entries for all input UTXOs of the transaction
// from the utxoSpenders map.
//
// Assumes the lock is held.
func (t *Txs) removeSpenders(tx *atomic.Tx) {
	for utxoID := range tx.InputUTXOs() {
		delete(t.utxoSpenders, utxoID)
	}
}

// RemoveTx removes the transaction from the mempool, including removal of the
// Discarded status.
func (t *Txs) RemoveTx(tx *atomic.Tx) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.removeTx(tx, false)
}

// SubscribePendingTxs returns a channel that signals when there is a
// transaction added to the mempool.
func (t *Txs) SubscribePendingTxs() <-chan struct{} {
	return t.pending
}
