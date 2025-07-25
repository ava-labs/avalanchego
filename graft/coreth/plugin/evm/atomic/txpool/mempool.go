// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/coreth/plugin/evm/atomic"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/metrics"
)

const (
	discardedTxsCacheSize = 50
)

var (
	_ gossip.Set[*atomic.Tx] = (*Mempool)(nil)

	errTxAlreadyKnown          = errors.New("tx already known")
	errNoGasUsed               = errors.New("no gas used")
	ErrConflictingAtomicTx     = errors.New("conflicting atomic tx present")
	ErrInsufficientAtomicTxFee = errors.New("atomic tx fee too low for atomic mempool")
	ErrTooManyAtomicTx         = errors.New("too many atomic tx")
)

// mempoolMetrics defines the metrics for the atomic mempool
type mempoolMetrics struct {
	pendingTxs metrics.Gauge // Gauge of currently pending transactions in the txHeap
	currentTxs metrics.Gauge // Gauge of current transactions to be issued into a block
	issuedTxs  metrics.Gauge // Gauge of transactions that have been issued into a block

	addedTxs     metrics.Counter // Count of all transactions added to the mempool
	discardedTxs metrics.Counter // Count of all discarded transactions
}

// newMempoolMetrics constructs metrics for the atomic mempool
func newMempoolMetrics() *mempoolMetrics {
	return &mempoolMetrics{
		pendingTxs:   metrics.GetOrRegisterGauge("atomic_mempool_pending_txs", nil),
		currentTxs:   metrics.GetOrRegisterGauge("atomic_mempool_current_txs", nil),
		issuedTxs:    metrics.GetOrRegisterGauge("atomic_mempool_issued_txs", nil),
		addedTxs:     metrics.GetOrRegisterCounter("atomic_mempool_added_txs", nil),
		discardedTxs: metrics.GetOrRegisterCounter("atomic_mempool_discarded_txs", nil),
	}
}

// Mempool is a simple mempool for atomic transactions
type Mempool struct {
	lock sync.RWMutex

	ctx *snow.Context
	// maxSize is the maximum number of transactions allowed to be kept in mempool
	maxSize int
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
	// bloom is a bloom filter containing the txs in the mempool
	bloom *gossip.BloomFilter

	metrics *mempoolMetrics

	verify func(tx *atomic.Tx) error
}

// Initialize initializes the Mempool with `maxSize`
func (m *Mempool) Initialize(ctx *snow.Context, registerer prometheus.Registerer, maxSize int, verify func(tx *atomic.Tx) error) error {
	bloom, err := gossip.NewBloomFilter(registerer, "atomic_mempool_bloom_filter",
		config.TxGossipBloomMinTargetElements,
		config.TxGossipBloomTargetFalsePositiveRate,
		config.TxGossipBloomResetFalsePositiveRate,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize bloom filter: %w", err)
	}

	m.ctx = ctx
	m.issuedTxs = make(map[ids.ID]*atomic.Tx)
	m.discardedTxs = lru.NewCache[ids.ID, *atomic.Tx](discardedTxsCacheSize)
	m.currentTxs = make(map[ids.ID]*atomic.Tx)
	m.pending = make(chan struct{}, 1)
	m.txHeap = newTxHeap(maxSize)
	m.maxSize = maxSize
	m.utxoSpenders = make(map[ids.ID]*atomic.Tx)
	m.bloom = bloom
	m.metrics = newMempoolMetrics()
	m.verify = verify
	return nil
}

// PendingLen returns the number of pending transactions in the mempool
func (m *Mempool) PendingLen() int {
	return m.Len()
}

// Len returns the number of transactions in the mempool
func (m *Mempool) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.length()
}

// assumes the lock is held
func (m *Mempool) length() int {
	return m.txHeap.Len() + len(m.issuedTxs)
}

// atomicTxGasPrice is the [gasPrice] paid by a transaction to burn a given
// amount of [AVAXAssetID] given the value of [gasUsed].
func (m *Mempool) atomicTxGasPrice(tx *atomic.Tx) (uint64, error) {
	gasUsed, err := tx.GasUsed(true)
	if err != nil {
		return 0, err
	}
	if gasUsed == 0 {
		return 0, errNoGasUsed
	}
	burned, err := tx.Burned(m.ctx.AVAXAssetID)
	if err != nil {
		return 0, err
	}
	return burned / gasUsed, nil
}

func (m *Mempool) Add(tx *atomic.Tx) error {
	m.ctx.Lock.RLock()
	defer m.ctx.Lock.RUnlock()

	return m.AddRemoteTx(tx)
}

// AddRemoteTx attempts to add [tx] to the mempool and returns an error if
// it could not be added to the mempool.
func (m *Mempool) AddRemoteTx(tx *atomic.Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	err := m.addTx(tx, false, false)
	// Do not attempt to discard the tx if it was already known
	if errors.Is(err, errTxAlreadyKnown) {
		return err
	}

	if err != nil {
		// unlike local txs, invalid remote txs are recorded as discarded
		// so that they won't be requested again
		txID := tx.ID()
		m.discardedTxs.Put(tx.ID(), tx)
		log.Debug("failed to issue remote tx to mempool",
			"txID", txID,
			"err", err,
		)
	}
	return err
}

func (m *Mempool) AddLocalTx(tx *atomic.Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	err := m.addTx(tx, true, false)
	if errors.Is(err, errTxAlreadyKnown) {
		return nil
	}

	return err
}

// ForceAddTx forcibly adds a *atomic.Tx to the mempool and bypasses all verification.
func (m *Mempool) ForceAddTx(tx *atomic.Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.addTx(tx, true, true)
}

// checkConflictTx checks for any transactions in the mempool that spend the same input UTXOs as [tx].
// If any conflicts are present, it returns the highest gas price of any conflicting transaction, the
// txID of the corresponding tx and the full list of transactions that conflict with [tx].
func (m *Mempool) checkConflictTx(tx *atomic.Tx) (uint64, ids.ID, []*atomic.Tx, error) {
	utxoSet := tx.InputUTXOs()

	var (
		highestGasPrice             uint64
		conflictingTxs              []*atomic.Tx
		highestGasPriceConflictTxID ids.ID
	)
	for utxoID := range utxoSet {
		// Get current gas price of the existing tx in the mempool
		conflictTx, ok := m.utxoSpenders[utxoID]
		if !ok {
			continue
		}
		conflictTxID := conflictTx.ID()
		conflictTxGasPrice, err := m.atomicTxGasPrice(conflictTx)
		// Should never error to calculate the gas price of a transaction already in the mempool
		if err != nil {
			return 0, ids.ID{}, conflictingTxs, fmt.Errorf("failed to re-calculate gas price for conflict tx due to: %w", err)
		}
		if highestGasPrice < conflictTxGasPrice {
			highestGasPrice = conflictTxGasPrice
			highestGasPriceConflictTxID = conflictTxID
		}
		conflictingTxs = append(conflictingTxs, conflictTx)
	}
	return highestGasPrice, highestGasPriceConflictTxID, conflictingTxs, nil
}

// addTx adds [tx] to the mempool. Assumes [m.lock] is held.
// If [force], skips conflict checks within the mempool.
func (m *Mempool) addTx(tx *atomic.Tx, local bool, force bool) error {
	txID := tx.ID()
	// If [txID] has already been issued or is in the currentTxs map
	// there's no need to add it.
	if _, exists := m.issuedTxs[txID]; exists {
		return fmt.Errorf("%w: tx %s was issued previously", errTxAlreadyKnown, tx.ID())
	}
	if _, exists := m.currentTxs[txID]; exists {
		return fmt.Errorf("%w: tx %s is being built into a block", errTxAlreadyKnown, tx.ID())
	}
	if _, exists := m.txHeap.Get(txID); exists {
		return fmt.Errorf("%w: tx %s is pending", errTxAlreadyKnown, tx.ID())
	}
	if !local {
		if _, exists := m.discardedTxs.Get(txID); exists {
			return fmt.Errorf("%w: tx %s was discarded", errTxAlreadyKnown, tx.ID())
		}
	}
	if !force && m.verify != nil {
		if err := m.verify(tx); err != nil {
			return err
		}
	}

	utxoSet := tx.InputUTXOs()
	gasPrice, _ := m.atomicTxGasPrice(tx)
	highestGasPrice, highestGasPriceConflictTxID, conflictingTxs, err := m.checkConflictTx(tx)
	if err != nil {
		return err
	}
	if len(conflictingTxs) != 0 && !force {
		// If [tx] does not have a higher fee than all of its conflicts,
		// we refuse to issue it to the mempool.
		if highestGasPrice >= gasPrice {
			return fmt.Errorf(
				"%w: issued tx (%s) gas price %d <= conflict tx (%s) gas price %d (%d total conflicts in mempool)",
				ErrConflictingAtomicTx,
				txID,
				gasPrice,
				highestGasPriceConflictTxID,
				highestGasPrice,
				len(conflictingTxs),
			)
		}
		// Remove any conflicting transactions from the mempool
		for _, conflictTx := range conflictingTxs {
			m.removeTx(conflictTx, true)
		}
	}
	// If adding this transaction would exceed the mempool's size, check if there is a lower priced
	// transaction that can be evicted from the mempool
	if m.length() >= m.maxSize {
		if m.txHeap.Len() > 0 {
			// Get the lowest price item from [txHeap]
			minTx, minGasPrice := m.txHeap.PeekMin()
			// If the [gasPrice] of the lowest item is >= the [gasPrice] of the
			// submitted item, discard the submitted item (we prefer items
			// already in the mempool).
			if minGasPrice >= gasPrice {
				return fmt.Errorf(
					"%w currentMin=%d provided=%d",
					ErrInsufficientAtomicTxFee,
					minGasPrice,
					gasPrice,
				)
			}

			m.removeTx(minTx, true)
		} else {
			// This could occur if we have used our entire size allowance on
			// transactions that are currently processing.
			return ErrTooManyAtomicTx
		}
	}

	// If the transaction was recently discarded, log the event and evict from
	// discarded transactions so it's not in two places within the mempool.
	// We allow the transaction to be re-issued since it may have been invalid
	// due to an atomic UTXO not being present yet.
	if _, has := m.discardedTxs.Get(txID); has {
		log.Debug("Adding recently discarded transaction %s back to the mempool", txID)
		m.discardedTxs.Evict(txID)
	}

	// Add the transaction to the [txHeap] so we can evaluate new entries based
	// on how their [gasPrice] compares and add to [utxoSet] to make sure we can
	// reject conflicting transactions.
	m.txHeap.Push(tx, gasPrice)
	m.metrics.addedTxs.Inc(1)
	m.metrics.pendingTxs.Update(int64(m.txHeap.Len()))
	for utxoID := range utxoSet {
		m.utxoSpenders[utxoID] = tx
	}

	m.bloom.Add(tx)
	reset, err := gossip.ResetBloomFilterIfNeeded(m.bloom, m.length()*config.TxGossipBloomChurnMultiplier)
	if err != nil {
		return err
	}

	if reset {
		log.Debug("resetting bloom filter", "reason", "reached max filled ratio")

		for _, pendingTx := range m.txHeap.minHeap.items {
			m.bloom.Add(pendingTx.tx)
		}
	}

	// When adding [tx] to the mempool make sure that there is an item in Pending
	// to signal the VM to produce a block. Note: if the VM's buildStatus has already
	// been set to something other than [dontBuild], this will be ignored and won't be
	// reset until the engine calls BuildBlock. This case is handled in IssueCurrentTx
	// and CancelCurrentTx.
	m.addPending()

	return nil
}

func (m *Mempool) Iterate(f func(tx *atomic.Tx) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	for _, item := range m.txHeap.maxHeap.items {
		if !f(item.tx) {
			return
		}
	}
}

func (m *Mempool) GetFilter() ([]byte, []byte) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.bloom.Marshal()
}

// NextTx returns a transaction to be issued from the mempool.
func (m *Mempool) NextTx() (*atomic.Tx, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	// We include atomic transactions in blocks sorted by the [gasPrice] they
	// pay.
	if m.txHeap.Len() > 0 {
		tx := m.txHeap.PopMax()
		m.currentTxs[tx.ID()] = tx
		m.metrics.pendingTxs.Update(int64(m.txHeap.Len()))
		m.metrics.currentTxs.Update(int64(len(m.currentTxs)))
		return tx, true
	}

	return nil, false
}

// GetPendingTx returns the transaction [txID] and true if it is
// currently in the [txHeap] waiting to be issued into a block.
// Returns nil, false otherwise.
func (m *Mempool) GetPendingTx(txID ids.ID) (*atomic.Tx, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.txHeap.Get(txID)
}

// GetTx returns the transaction [txID] if it was issued
// by this node and returns whether it was dropped and whether
// it exists.
func (m *Mempool) GetTx(txID ids.ID) (*atomic.Tx, bool, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if tx, ok := m.txHeap.Get(txID); ok {
		return tx, false, true
	}
	if tx, ok := m.issuedTxs[txID]; ok {
		return tx, false, true
	}
	if tx, ok := m.currentTxs[txID]; ok {
		return tx, false, true
	}
	if tx, exists := m.discardedTxs.Get(txID); exists {
		return tx, true, true
	}

	return nil, false, false
}

// Has returns true if the mempool contains [txID] or it was issued.
func (m *Mempool) Has(txID ids.ID) bool {
	_, dropped, found := m.GetTx(txID)
	return found && !dropped
}

// IssueCurrentTx marks [currentTx] as issued if there is one
func (m *Mempool) IssueCurrentTxs() {
	m.lock.Lock()
	defer m.lock.Unlock()

	for txID := range m.currentTxs {
		m.issuedTxs[txID] = m.currentTxs[txID]
		delete(m.currentTxs, txID)
	}
	m.metrics.issuedTxs.Update(int64(len(m.issuedTxs)))
	m.metrics.currentTxs.Update(int64(len(m.currentTxs)))

	// If there are more transactions to be issued, add an item
	// to Pending.
	if m.txHeap.Len() > 0 {
		m.addPending()
	}
}

// CancelCurrentTx marks the attempt to issue [txID]
// as being aborted. This should be called after NextTx returns [txID]
// and the transaction [txID] cannot be included in the block, but should
// not be discarded. For example, CancelCurrentTx should be called if including
// the transaction will put the block above the atomic tx gas limit.
func (m *Mempool) CancelCurrentTx(txID ids.ID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if tx, ok := m.currentTxs[txID]; ok {
		m.cancelTx(tx)
	}
}

// [CancelCurrentTxs] marks the attempt to issue [currentTxs]
// as being aborted. If this is called after a buildBlock error
// caused by the atomic transaction, then DiscardCurrentTx should have been called
// such that this call will have no effect and should not re-issue the invalid tx.
func (m *Mempool) CancelCurrentTxs() {
	m.lock.Lock()
	defer m.lock.Unlock()

	// If building a block failed, put the currentTx back in [txs]
	// if it exists.
	for _, tx := range m.currentTxs {
		m.cancelTx(tx)
	}

	// If there are more transactions to be issued, add an item
	// to Pending.
	if m.txHeap.Len() > 0 {
		m.addPending()
	}
}

// cancelTx removes [tx] from current transactions and moves it back into the
// tx heap.
// assumes the lock is held.
func (m *Mempool) cancelTx(tx *atomic.Tx) {
	// Add tx to heap sorted by gasPrice
	gasPrice, err := m.atomicTxGasPrice(tx)
	if err == nil {
		m.txHeap.Push(tx, gasPrice)
		m.metrics.pendingTxs.Update(int64(m.txHeap.Len()))
	} else {
		// If the err is not nil, we simply discard the transaction because it is
		// invalid. This should never happen but we guard against the case it does.
		log.Error("failed to calculate atomic tx gas price while canceling current tx", "err", err)
		m.removeSpenders(tx)
		m.discardedTxs.Put(tx.ID(), tx)
		m.metrics.discardedTxs.Inc(1)
	}

	delete(m.currentTxs, tx.ID())
	m.metrics.currentTxs.Update(int64(len(m.currentTxs)))
}

// DiscardCurrentTx marks a [tx] in the [currentTxs] map as invalid and aborts the attempt
// to issue it since it failed verification.
func (m *Mempool) DiscardCurrentTx(txID ids.ID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if tx, ok := m.currentTxs[txID]; ok {
		m.discardCurrentTx(tx)
	}
}

// DiscardCurrentTxs marks all txs in [currentTxs] as discarded.
func (m *Mempool) DiscardCurrentTxs() {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, tx := range m.currentTxs {
		m.discardCurrentTx(tx)
	}
}

// discardCurrentTx discards [tx] from the set of current transactions.
// Assumes the lock is held.
func (m *Mempool) discardCurrentTx(tx *atomic.Tx) {
	m.removeSpenders(tx)
	m.discardedTxs.Put(tx.ID(), tx)
	delete(m.currentTxs, tx.ID())
	m.metrics.currentTxs.Update(int64(len(m.currentTxs)))
	m.metrics.discardedTxs.Inc(1)
}

// removeTx removes [txID] from the mempool.
// Note: removeTx will delete all entries from [utxoSpenders] corresponding
// to input UTXOs of [txID]. This means that when replacing a conflicting tx,
// removeTx must be called for all conflicts before overwriting the utxoSpenders
// map.
// Assumes lock is held.
func (m *Mempool) removeTx(tx *atomic.Tx, discard bool) {
	txID := tx.ID()

	// Remove from [currentTxs], [txHeap], and [issuedTxs].
	delete(m.currentTxs, txID)
	m.txHeap.Remove(txID)
	delete(m.issuedTxs, txID)

	if discard {
		m.discardedTxs.Put(txID, tx)
		m.metrics.discardedTxs.Inc(1)
	} else {
		m.discardedTxs.Evict(txID)
	}
	m.metrics.pendingTxs.Update(int64(m.txHeap.Len()))
	m.metrics.currentTxs.Update(int64(len(m.currentTxs)))
	m.metrics.issuedTxs.Update(int64(len(m.issuedTxs)))

	// Remove all entries from [utxoSpenders].
	m.removeSpenders(tx)
}

// removeSpenders deletes the entries for all input UTXOs of [tx] from the
// [utxoSpenders] map.
// Assumes the lock is held.
func (m *Mempool) removeSpenders(tx *atomic.Tx) {
	for utxoID := range tx.InputUTXOs() {
		delete(m.utxoSpenders, utxoID)
	}
}

// RemoveTx removes [txID] from the mempool completely.
// Evicts [tx] from the discarded cache if present.
func (m *Mempool) RemoveTx(tx *atomic.Tx) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.removeTx(tx, false)
}

// addPending makes sure that an item is in the Pending channel.
func (m *Mempool) addPending() {
	select {
	case m.pending <- struct{}{}:
	default:
	}
}

// SubscribePendingTxs implements the BuilderMempool interface and returns a channel
// that signals when there is at least one pending transaction in the mempool
func (m *Mempool) SubscribePendingTxs() <-chan struct{} {
	return m.pending
}
