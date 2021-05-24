// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"sync"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/log"
)

const (
	discardedTxsCacheSize = 50
)

// Mempool is a simple mempool for atomic transactions
type Mempool struct {
	lock sync.RWMutex
	// maxSize is the maximum number of transactions allowed to be kept in mempool
	maxSize int
	// currentTx is the transaction about to be added to a block.
	currentTx *Tx
	// txs is the set of transactions that need to be issued into new blocks
	txs map[ids.ID]*Tx
	// issuedTxs is the set of transactions that have been issued into a new block
	issuedTxs map[ids.ID]*Tx
	// discardedTxs is an LRU Cache of transactions that have been discarded after failing
	// verification.
	discardedTxs *cache.LRU
	// Pending is a channel of length one, which the mempool ensures has an item on
	// it as long as there is an unissued transaction remaining in [txs]
	Pending chan struct{}
}

// NewMempool returns a Mempool with [maxSize]
func NewMempool(maxSize int) *Mempool {
	return &Mempool{
		txs:          make(map[ids.ID]*Tx),
		issuedTxs:    make(map[ids.ID]*Tx),
		discardedTxs: &cache.LRU{Size: discardedTxsCacheSize},
		Pending:      make(chan struct{}, 1),
		maxSize:      maxSize,
	}
}

// Len returns the number of transactions in the mempool
func (m *Mempool) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.length()
}

// assumes the lock is held
func (m *Mempool) length() int {
	return len(m.txs) + len(m.issuedTxs)
}

// Add attempts to add [tx] to the mempool and returns an error if
// it could not be addeed to the mempool.
func (m *Mempool) AddTx(tx *Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	txID := tx.ID()
	// If [txID] has already been issued or is the currentTx
	// there's no need to add it.
	if _, exists := m.issuedTxs[txID]; exists {
		return nil
	}
	if m.currentTx != nil && m.currentTx.ID() == txID {
		return nil
	}

	if m.length() >= m.maxSize {
		return errTooManyAtomicTx
	}

	if _, exists := m.txs[txID]; exists {
		return nil
	}

	// If the transaction was recently discarded, log the event and evict from
	// discarded transactions so it's not in two places within the mempool.
	// We allow the transaction to be re-issued since it may have been invalid due
	// to an atomic UTXO not being present yet.
	if _, has := m.discardedTxs.Get(txID); has {
		log.Debug("Adding recently discarded transaction %s back to the mempool", txID)
		m.discardedTxs.Evict(txID)
	}

	m.txs[txID] = tx
	// When adding [tx] to the mempool make sure that there is an item in Pending
	// to signal the VM to produce a block. Note: if the VM's buildStatus has already
	// been set to something other than [dontBuild], this will be ignored and won't be
	// reset until the engine calls BuildBlock. This case is handled in IssueCurrentTx
	// and CancelCurrentTx.
	m.addPending()
	return nil
}

// NextTx returns a transaction to be issued from the mempool.
func (m *Mempool) NextTx() (*Tx, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for txID, tx := range m.txs {
		delete(m.txs, txID)
		m.currentTx = tx
		return tx, true
	}

	return nil, false
}

// GetTx returns the transaction [txID] if it was issued
// by this node and returns whether it was dropped and whether
// it exists.
func (m *Mempool) GetTx(txID ids.ID) (*Tx, bool, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if tx, ok := m.txs[txID]; ok {
		return tx, false, true
	}
	if tx, ok := m.issuedTxs[txID]; ok {
		return tx, false, true
	}
	if m.currentTx != nil && m.currentTx.ID() == txID {
		return m.currentTx, false, true
	}
	if tx, exists := m.discardedTxs.Get(txID); exists {
		return tx.(*Tx), true, true
	}

	return nil, false, false
}

// IssueCurrentTx marks [currentTx] as issued if there is one
func (m *Mempool) IssueCurrentTx() {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.currentTx != nil {
		m.issuedTxs[m.currentTx.ID()] = m.currentTx
		m.currentTx = nil
	}

	// If there are more transactions to be issued, add an item
	// to Pending.
	if len(m.txs) > 0 {
		m.addPending()
	}
}

// CancelCurrentTx marks the attempt to issue [currentTx]
// as being aborted. If this is called after a buildBlock error
// caused by the atomic transaction, then DiscardCurrentTx should have been called
// such that this call will have no effect and should not re-issue the invalid tx.
func (m *Mempool) CancelCurrentTx() {
	m.lock.Lock()
	defer m.lock.Unlock()

	// If building a block failed, put the currentTx back in [txs]
	// if it exists.
	if m.currentTx != nil {
		m.txs[m.currentTx.ID()] = m.currentTx
		m.currentTx = nil
	}

	// If there are more transactions to be issued, add an item
	// to Pending.
	if len(m.txs) > 0 {
		m.addPending()
	}
}

// DiscardCurrentTx marks [currentTx] as invalid and aborts the attempt
// to issue it since it failed verification.
// Adding to Pending should be handled by CancelCurrentTx in this case.
func (m *Mempool) DiscardCurrentTx() {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.currentTx == nil {
		return
	}

	m.discardedTxs.Put(m.currentTx.ID(), m.currentTx)
	m.currentTx = nil
}

// RemoveTx removes [txID] from the mempool completely.
func (m *Mempool) RemoveTx(txID ids.ID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.currentTx != nil && m.currentTx.ID() == txID {
		m.currentTx = nil
	}
	delete(m.txs, txID)
	delete(m.issuedTxs, txID)
	m.discardedTxs.Evict(txID)
}

// RejectTx marks [txID] as being rejected and attempts to re-issue
// it if it was previously in the mempool.
func (m *Mempool) RejectTx(txID ids.ID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	tx, ok := m.issuedTxs[txID]
	if !ok {
		return
	}
	// If the transaction was issued by the mempool, add it back
	// to transactions pending issuance.
	delete(m.issuedTxs, txID)
	m.txs[txID] = tx
	// Add an item to Pending to ensure the VM attempts to reissue
	// [tx].
	m.addPending()
}

// addPending makes sure that an item is in the Pending channel.
func (m *Mempool) addPending() {
	select {
	case m.Pending <- struct{}{}:
	default:
	}
}
