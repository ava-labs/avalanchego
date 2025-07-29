// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"errors"
	"sync"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"

	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/libevm/log"
)

const discardedTxsCacheSize = 50

var ErrNoGasUsed = errors.New("no gas used")

type Txs struct {
	ctx     *snow.Context
	metrics *metrics
	// maxSize is the maximum number of transactions allowed to be kept in mempool
	maxSize int

	lock sync.RWMutex

	// currentTxs is the set of transactions about to be added to a block.
	currentTxs map[ids.ID]*atomic.Tx
	// issuedTxs is the set of transactions that have been issued into a new block
	issuedTxs map[ids.ID]*atomic.Tx
	// discardedTxs is an LRU Cache of transactions that have been discarded after failing
	// verification.
	discardedTxs *lru.Cache[ids.ID, *atomic.Tx]
	// pending is a channel of length one, which the mempool ensures has an item on
	// it as long as there is an unissued transaction remaining in [txs]
	pending chan struct{}
	// txHeap is a sorted record of all txs in the mempool by [gasPrice]
	// NOTE: [txHeap] ONLY contains pending txs
	txHeap *txHeap
	// utxoSpenders maps utxoIDs to the transaction consuming them in the mempool
	utxoSpenders map[ids.ID]*atomic.Tx
}

func NewTxs(ctx *snow.Context, maxSize int) *Txs {
	return &Txs{
		ctx:          ctx,
		metrics:      newMetrics(),
		maxSize:      maxSize,
		currentTxs:   make(map[ids.ID]*atomic.Tx),
		issuedTxs:    make(map[ids.ID]*atomic.Tx),
		discardedTxs: lru.NewCache[ids.ID, *atomic.Tx](discardedTxsCacheSize),
		pending:      make(chan struct{}, 1),
		txHeap:       newTxHeap(maxSize),
		utxoSpenders: make(map[ids.ID]*atomic.Tx),
	}
}

// PendingLen returns the number of pending transactions
func (t *Txs) PendingLen() int {
	return t.Len()
}

// Len returns the number of transactions
func (t *Txs) Len() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.length()
}

// assumes the lock is held
func (t *Txs) length() int {
	return t.txHeap.Len() + len(t.issuedTxs)
}

// atomicTxGasPrice is the [gasPrice] paid by a transaction to burn a given
// amount of [AVAXAssetID] given the value of [gasUsed].
func (t *Txs) atomicTxGasPrice(tx *atomic.Tx) (uint64, error) {
	gasUsed, err := tx.GasUsed(true)
	if err != nil {
		return 0, err
	}
	if gasUsed == 0 {
		return 0, ErrNoGasUsed
	}
	burned, err := tx.Burned(t.ctx.AVAXAssetID)
	if err != nil {
		return 0, err
	}
	return burned / gasUsed, nil
}

func (t *Txs) Iterate(f func(tx *atomic.Tx) bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	for _, item := range t.txHeap.maxHeap.items {
		if !f(item.tx) {
			return
		}
	}
}

// NextTx returns a transaction to be issued from the mempool.
func (t *Txs) NextTx() (*atomic.Tx, bool) {
	t.lock.Lock()
	defer t.lock.Unlock()

	// We include atomic transactions in blocks sorted by the [gasPrice] they
	// pay.
	if t.txHeap.Len() > 0 {
		tx := t.txHeap.PopMax()
		t.currentTxs[tx.ID()] = tx
		t.metrics.pendingTxs.Update(int64(t.txHeap.Len()))
		t.metrics.currentTxs.Update(int64(len(t.currentTxs)))
		return tx, true
	}

	return nil, false
}

// GetPendingTx returns the transaction [txID] and true if it is
// currently in the [txHeap] waiting to be issued into a block.
// Returns nil, false otherwise.
func (t *Txs) GetPendingTx(txID ids.ID) (*atomic.Tx, bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.txHeap.Get(txID)
}

// GetTx returns the transaction [txID] if it was issued
// by this node and returns whether it was dropped and whether
// it exists.
func (t *Txs) GetTx(txID ids.ID) (*atomic.Tx, bool, bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	if tx, ok := t.txHeap.Get(txID); ok {
		return tx, false, true
	}
	if tx, ok := t.issuedTxs[txID]; ok {
		return tx, false, true
	}
	if tx, ok := t.currentTxs[txID]; ok {
		return tx, false, true
	}
	if tx, exists := t.discardedTxs.Get(txID); exists {
		return tx, true, true
	}

	return nil, false, false
}

// Has returns true if the mempool contains [txID] or it was issued.
func (t *Txs) Has(txID ids.ID) bool {
	_, dropped, found := t.GetTx(txID)
	return found && !dropped
}

// IssueCurrentTx marks [currentTx] as issued if there is one
func (t *Txs) IssueCurrentTxs() {
	t.lock.Lock()
	defer t.lock.Unlock()

	for txID := range t.currentTxs {
		t.issuedTxs[txID] = t.currentTxs[txID]
		delete(t.currentTxs, txID)
	}
	t.metrics.issuedTxs.Update(int64(len(t.issuedTxs)))
	t.metrics.currentTxs.Update(int64(len(t.currentTxs)))

	// If there are more transactions to be issued, add an item
	// to Pending.
	if t.txHeap.Len() > 0 {
		t.addPending()
	}
}

// CancelCurrentTx marks the attempt to issue [txID]
// as being aborted. This should be called after NextTx returns [txID]
// and the transaction [txID] cannot be included in the block, but should
// not be discarded. For example, CancelCurrentTx should be called if including
// the transaction will put the block above the atomic tx gas limit.
func (t *Txs) CancelCurrentTx(txID ids.ID) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if tx, ok := t.currentTxs[txID]; ok {
		t.cancelTx(tx)
	}
}

// [CancelCurrentTxs] marks the attempt to issue [currentTxs]
// as being aborted. If this is called after a buildBlock error
// caused by the atomic transaction, then DiscardCurrentTx should have been called
// such that this call will have no effect and should not re-issue the invalid tx.
func (t *Txs) CancelCurrentTxs() {
	t.lock.Lock()
	defer t.lock.Unlock()

	// If building a block failed, put the currentTx back in [txs]
	// if it exists.
	for _, tx := range t.currentTxs {
		t.cancelTx(tx)
	}

	// If there are more transactions to be issued, add an item
	// to Pending.
	if t.txHeap.Len() > 0 {
		t.addPending()
	}
}

// cancelTx removes [tx] from current transactions and moves it back into the
// tx heap.
// assumes the lock is held.
func (t *Txs) cancelTx(tx *atomic.Tx) {
	// Add tx to heap sorted by gasPrice
	gasPrice, err := t.atomicTxGasPrice(tx)
	if err == nil {
		t.txHeap.Push(tx, gasPrice)
		t.metrics.pendingTxs.Update(int64(t.txHeap.Len()))
	} else {
		// If the err is not nil, we simply discard the transaction because it is
		// invalid. This should never happen but we guard against the case it does.
		log.Error("failed to calculate atomic tx gas price while canceling current tx", "err", err)
		t.removeSpenders(tx)
		t.discardedTxs.Put(tx.ID(), tx)
		t.metrics.discardedTxs.Inc(1)
	}

	delete(t.currentTxs, tx.ID())
	t.metrics.currentTxs.Update(int64(len(t.currentTxs)))
}

// DiscardCurrentTx marks a [tx] in the [currentTxs] map as invalid and aborts the attempt
// to issue it since it failed verification.
func (t *Txs) DiscardCurrentTx(txID ids.ID) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if tx, ok := t.currentTxs[txID]; ok {
		t.discardCurrentTx(tx)
	}
}

// DiscardCurrentTxs marks all txs in [currentTxs] as discarded.
func (t *Txs) DiscardCurrentTxs() {
	t.lock.Lock()
	defer t.lock.Unlock()

	for _, tx := range t.currentTxs {
		t.discardCurrentTx(tx)
	}
}

// discardCurrentTx discards [tx] from the set of current transactions.
// Assumes the lock is held.
func (t *Txs) discardCurrentTx(tx *atomic.Tx) {
	t.removeSpenders(tx)
	t.discardedTxs.Put(tx.ID(), tx)
	delete(t.currentTxs, tx.ID())
	t.metrics.currentTxs.Update(int64(len(t.currentTxs)))
	t.metrics.discardedTxs.Inc(1)
}

// removeTx removes [txID] from the mempool.
// Note: removeTx will delete all entries from [utxoSpenders] corresponding
// to input UTXOs of [txID]. This means that when replacing a conflicting tx,
// removeTx must be called for all conflicts before overwriting the utxoSpenders
// map.
// Assumes lock is held.
func (t *Txs) removeTx(tx *atomic.Tx, discard bool) {
	txID := tx.ID()

	// Remove from [currentTxs], [txHeap], and [issuedTxs].
	delete(t.currentTxs, txID)
	t.txHeap.Remove(txID)
	delete(t.issuedTxs, txID)

	if discard {
		t.discardedTxs.Put(txID, tx)
		t.metrics.discardedTxs.Inc(1)
	} else {
		t.discardedTxs.Evict(txID)
	}
	t.metrics.pendingTxs.Update(int64(t.txHeap.Len()))
	t.metrics.currentTxs.Update(int64(len(t.currentTxs)))
	t.metrics.issuedTxs.Update(int64(len(t.issuedTxs)))

	// Remove all entries from [utxoSpenders].
	t.removeSpenders(tx)
}

// removeSpenders deletes the entries for all input UTXOs of [tx] from the
// [utxoSpenders] map.
// Assumes the lock is held.
func (t *Txs) removeSpenders(tx *atomic.Tx) {
	for utxoID := range tx.InputUTXOs() {
		delete(t.utxoSpenders, utxoID)
	}
}

// RemoveTx removes [txID] from the mempool completely.
// Evicts [tx] from the discarded cache if present.
func (t *Txs) RemoveTx(tx *atomic.Tx) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.removeTx(tx, false)
}

// addPending makes sure that an item is in the Pending channel.
func (t *Txs) addPending() {
	select {
	case t.pending <- struct{}{}:
	default:
	}
}

// SubscribePendingTxs implements the BuilderMempool interface and returns a channel
// that signals when there is at least one pending transaction in the mempool
func (t *Txs) SubscribePendingTxs() <-chan struct{} {
	return t.pending
}
