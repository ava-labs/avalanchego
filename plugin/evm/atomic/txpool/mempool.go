// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/libevm/log"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	_ gossip.Set[*atomic.Tx] = (*Mempool)(nil)

	ErrAlreadyKnown    = errors.New("already known")
	ErrConflict        = errors.New("conflict present")
	ErrInsufficientFee = errors.New("insufficient fee")
	ErrMempoolFull     = errors.New("mempool full")
)

// Mempool is a simple mempool for atomic transactions
type Mempool struct {
	*Txs
	// bloom is a bloom filter containing the txs in the mempool
	bloom  *gossip.BloomFilter
	verify func(tx *atomic.Tx) error
}

func NewMempool(
	txs *Txs,
	registerer prometheus.Registerer,
	verify func(tx *atomic.Tx) error,
) (*Mempool, error) {
	bloom, err := gossip.NewBloomFilter(registerer, "atomic_mempool_bloom_filter",
		config.TxGossipBloomMinTargetElements,
		config.TxGossipBloomTargetFalsePositiveRate,
		config.TxGossipBloomResetFalsePositiveRate,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize bloom filter: %w", err)
	}

	return &Mempool{
		Txs:    txs,
		bloom:  bloom,
		verify: verify,
	}, nil
}

// Add attempts to add tx to the mempool as a Remote transaction. It is assumed
// the snow context lock is not held.
func (m *Mempool) Add(tx *atomic.Tx) error {
	m.ctx.Lock.RLock()
	defer m.ctx.Lock.RUnlock()

	return m.AddRemoteTx(tx)
}

// AddRemoteTx attempts to add tx to the mempool as a Remote transaction.
//
// Remote transactions are checked for recent verification failures prior to
// performing verification. If a Remote transaction failed verification recently
// it will not be added to the mempool.
func (m *Mempool) AddRemoteTx(tx *atomic.Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	err := m.addTx(tx, false, false)
	// If the transaction is already in the mempool, marking it as Discarded,
	// could unexpectedly cause the transaction to have multiple statuses.
	if err == nil || errors.Is(err, ErrAlreadyKnown) {
		return err
	}

	// Unlike local txs, invalid remote txs are recorded as discarded so that
	// they won't be requested again
	txID := tx.ID()
	m.discardedTxs.Put(txID, tx)
	log.Debug("failed to issue remote tx to mempool",
		"txID", txID,
		"err", err,
	)
	return err
}

// AddLocalTx attempts to add tx to the mempool as a Local transaction.
//
// Local transactions are not checked for recent verification failures prior to
// performing verification. Even if a Local transaction failed verification
// recently, the mempool will attempt to re-verify it.
func (m *Mempool) AddLocalTx(tx *atomic.Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.addTx(tx, true, false)
}

// ForceAddTx forcibly adds a tx to the mempool and bypasses all verification.
func (m *Mempool) ForceAddTx(tx *atomic.Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.addTx(tx, true, true)
}

// checkConflictTx checks for any transactions in the mempool that spend the
// same input UTXOs as the provided transaction. If any conflicts are present,
// it returns the highest gas price of any conflicting transaction, the ID of
// the corresponding tx and the full list of conflicting transactions.
func (m *Mempool) checkConflictTx(tx *atomic.Tx) (uint64, ids.ID, []*atomic.Tx, error) {
	utxoSet := tx.InputUTXOs()

	var (
		highestGasPrice             uint64
		highestGasPriceConflictTxID ids.ID
		conflictingTxs              []*atomic.Tx
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

// Assumes the lock is held.
func (m *Mempool) length() int {
	return m.pendingTxs.Len() + len(m.currentTxs) + len(m.issuedTxs)
}

// addTx attempts to add tx to the mempool.
//
// Unless local, discarded transactions can not be added.
// If force, conflict checks are skipped.
//
// Assumes lock is held.
func (m *Mempool) addTx(tx *atomic.Tx, local bool, force bool) error {
	// If the tx has already been included in the mempool, there's no need to
	// add it again.
	txID := tx.ID()
	if _, exists := m.issuedTxs[txID]; exists {
		return fmt.Errorf("%w: tx %s was issued previously", ErrAlreadyKnown, txID)
	}
	if _, exists := m.currentTxs[txID]; exists {
		return fmt.Errorf("%w: tx %s is being built into a block", ErrAlreadyKnown, txID)
	}
	if _, exists := m.pendingTxs.Get(txID); exists {
		return fmt.Errorf("%w: tx %s is pending", ErrAlreadyKnown, txID)
	}
	if !local {
		if _, exists := m.discardedTxs.Get(txID); exists {
			return fmt.Errorf("%w: tx %s was discarded", ErrAlreadyKnown, txID)
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
		// If the transaction does not have a higher fee than all of its
		// conflicts, we refuse to add it to the mempool.
		if highestGasPrice >= gasPrice {
			return fmt.Errorf(
				"%w: issued tx (%s) gas price %d <= conflict tx (%s) gas price %d (%d total conflicts in mempool)",
				ErrConflict,
				txID,
				gasPrice,
				highestGasPriceConflictTxID,
				highestGasPrice,
				len(conflictingTxs),
			)
		}
		// Remove all conflicting transactions from the mempool.
		for _, conflictTx := range conflictingTxs {
			m.removeTx(conflictTx, true)
		}
	}
	// If adding this transaction would exceed the mempool's size, check if
	// there is a lower priced transaction that can be evicted from the mempool.
	//
	// Recall that the size of the mempool is denominated in transactions, so to
	// add a new transaction we only need to evict at most one other
	// transaction.
	if m.length() >= m.maxSize {
		if m.pendingTxs.Len() > 0 {
			// Get the transaction with the lowest gasPrice
			minTx, minGasPrice := m.pendingTxs.PeekMin()
			// If the lowest gasPrice >= the gasPrice of the new transaction,
			// Discard new transaction. (Prefer items already in the mempool).
			if minGasPrice >= gasPrice {
				return fmt.Errorf(
					"%w currentMin=%d provided=%d",
					ErrInsufficientFee,
					minGasPrice,
					gasPrice,
				)
			}

			m.removeTx(minTx, true)
		} else {
			// This could occur if we have used our entire size allowance on
			// transactions that are either Current or Issued.
			return ErrMempoolFull
		}
	}

	// If the transaction was recently discarded, log the event and evict from
	// discarded transactions so it's not in two places within the mempool.
	// We allow the transaction to be re-issued since it may have been invalid
	// due to an atomic UTXO not being present yet.
	if _, has := m.discardedTxs.Get(txID); has {
		log.Debug("Adding recently discarded transaction back to the mempool",
			"txID", txID,
		)
		m.discardedTxs.Evict(txID)
	}

	// Mark the transaction as Pending.
	m.pendingTxs.Push(tx, gasPrice)
	m.metrics.addedTxs.Inc(1)
	m.metrics.pendingTxs.Update(int64(m.pendingTxs.Len()))
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

		for _, pendingTx := range m.pendingTxs.minHeap.items {
			m.bloom.Add(pendingTx.tx)
		}
	}

	// When adding a transaction to the mempool, we make sure that there is an
	// item in Pending to signal the VM to produce a block.
	//
	// If the VM's buildStatus has already been set to something other than
	// dontBuild, this will be ignored and won't be reset until the engine calls
	// BuildBlock. This case is handled in [Txs.IssueCurrentTxs] and
	// [Txs.CancelCurrentTxs].
	m.addPending()

	return nil
}

func (m *Mempool) GetFilter() ([]byte, []byte) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.bloom.Marshal()
}
