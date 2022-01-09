// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var (
	errAssetIDMismatch = errors.New("asset IDs in the input don't match the utxo")
	errWrongAssetID    = errors.New("asset ID must be AVAX in the atomic tx")
	errMissingUTXO     = errors.New("missing utxo")
	errUnknownTx       = errors.New("transaction is unknown")
	errRejectedTx      = errors.New("transaction is rejected")
)

var (
	_ snowstorm.Tx    = &UniqueTx{}
	_ cache.Evictable = &UniqueTx{}
)

// UniqueTx provides a de-duplication service for txs. This only provides a
// performance boost
type UniqueTx struct {
	*TxCachedState

	vm   *VM
	txID ids.ID
}

type TxCachedState struct {
	*Tx

	unique, verifiedTx, verifiedState bool
	validity                          error

	inputs     []ids.ID
	inputUTXOs []*avax.UTXOID
	utxos      []*avax.UTXO
	deps       []snowstorm.Tx

	status choices.Status
}

func (tx *UniqueTx) refresh() {
	tx.vm.numTxRefreshes.Inc()

	if tx.TxCachedState == nil {
		tx.TxCachedState = &TxCachedState{}
	}
	if tx.unique {
		return
	}
	unique := tx.vm.state.DeduplicateTx(tx)
	prevTx := tx.Tx
	if unique == tx {
		tx.vm.numTxRefreshMisses.Inc()

		// If no one was in the cache, make sure that there wasn't an
		// intermediate object whose state I must reflect
		if status, err := tx.vm.state.GetStatus(tx.ID()); err == nil {
			tx.status = status
		}
		tx.unique = true
	} else {
		tx.vm.numTxRefreshHits.Inc()

		// If someone is in the cache, they must be up to date

		// This ensures that every unique tx object points to the same tx state
		tx.TxCachedState = unique.TxCachedState
	}

	if tx.Tx != nil {
		return
	}

	if prevTx == nil {
		if innerTx, err := tx.vm.state.GetTx(tx.ID()); err == nil {
			tx.Tx = innerTx
		}
	} else {
		tx.Tx = prevTx
	}
}

// Evict is called when this UniqueTx will no longer be returned from a cache
// lookup
func (tx *UniqueTx) Evict() {
	// Lock is already held here
	tx.unique = false
	tx.deps = nil
}

func (tx *UniqueTx) setStatus(status choices.Status) error {
	tx.refresh()
	if tx.status == status {
		return nil
	}
	tx.status = status
	return tx.vm.state.PutStatus(tx.ID(), status)
}

// ID returns the wrapped txID
func (tx *UniqueTx) ID() ids.ID       { return tx.txID }
func (tx *UniqueTx) Key() interface{} { return tx.txID }

// Accept is called when the transaction was finalized as accepted by consensus
func (tx *UniqueTx) Accept() error {
	if s := tx.Status(); s != choices.Processing {
		return fmt.Errorf("transaction has invalid status: %s", s)
	}

	txID := tx.ID()
	defer tx.vm.db.Abort()

	// Fetch the input UTXOs
	inputUTXOIDs := tx.InputUTXOs()
	inputUTXOs := make([]*avax.UTXO, 0, len(inputUTXOIDs))
	for _, utxoID := range inputUTXOIDs {
		// Don't bother fetching the input UTXO if its symbolic
		if utxoID.Symbolic() {
			continue
		}

		utxo, err := tx.vm.getUTXO(utxoID)
		if err != nil {
			// should never happen because the UTXO was previously verified to
			// exist
			return fmt.Errorf("error finding UTXO %s: %w", utxoID, err)
		}
		inputUTXOs = append(inputUTXOs, utxo)
	}

	outputUTXOs := tx.UTXOs()
	// index input and output UTXOs
	if err := tx.vm.addressTxsIndexer.Accept(tx.ID(), inputUTXOs, outputUTXOs); err != nil {
		return fmt.Errorf("error indexing tx: %w", err)
	}

	// Remove spent utxos
	for _, utxo := range inputUTXOIDs {
		if utxo.Symbolic() {
			// If the UTXO is symbolic, it can't be spent
			continue
		}
		utxoID := utxo.InputID()
		if err := tx.vm.state.DeleteUTXO(utxoID); err != nil {
			return fmt.Errorf("couldn't delete UTXO %s: %w", utxoID, err)
		}
	}
	// Add new utxos
	for _, utxo := range outputUTXOs {
		utxoID := utxo.InputID()
		if err := tx.vm.state.PutUTXO(utxoID, utxo); err != nil {
			return fmt.Errorf("couldn't put UTXO %s: %w", utxoID, err)
		}
	}

	if err := tx.setStatus(choices.Accepted); err != nil {
		return fmt.Errorf("couldn't set status of tx %s: %w", txID, err)
	}

	commitBatch, err := tx.vm.db.CommitBatch()
	if err != nil {
		return fmt.Errorf("couldn't create commitBatch while processing tx %s: %w", txID, err)
	}

	if err := tx.ExecuteWithSideEffects(tx.vm, commitBatch); err != nil {
		return fmt.Errorf("ExecuteWithSideEffects errored while processing tx %s: %w", txID, err)
	}

	tx.vm.pubsub.Publish(NewPubSubFilterer(tx.Tx))
	tx.vm.walletService.decided(txID)

	tx.deps = nil // Needed to prevent a memory leak

	return nil
}

// Reject is called when the transaction was finalized as rejected by consensus
func (tx *UniqueTx) Reject() error {
	defer tx.vm.db.Abort()

	if err := tx.setStatus(choices.Rejected); err != nil {
		tx.vm.ctx.Log.Error("Failed to reject tx %s due to %s", tx.txID, err)
		return err
	}

	txID := tx.ID()
	tx.vm.ctx.Log.Debug("Rejecting Tx: %s", txID)

	if err := tx.vm.db.Commit(); err != nil {
		tx.vm.ctx.Log.Error("Failed to commit reject %s due to %s", tx.txID, err)
		return err
	}

	tx.vm.walletService.decided(txID)

	tx.deps = nil // Needed to prevent a memory leak

	return nil
}

// Status returns the current status of this transaction
func (tx *UniqueTx) Status() choices.Status {
	tx.refresh()
	return tx.status
}

// Dependencies returns the set of transactions this transaction builds on
func (tx *UniqueTx) Dependencies() ([]snowstorm.Tx, error) {
	tx.refresh()
	if tx.Tx == nil || len(tx.deps) != 0 {
		return tx.deps, nil
	}

	txIDs := ids.Set{}
	for _, in := range tx.InputUTXOs() {
		if in.Symbolic() {
			continue
		}
		txID, _ := in.InputSource()
		if txIDs.Contains(txID) {
			continue
		}
		txIDs.Add(txID)
		tx.deps = append(tx.deps, &UniqueTx{
			vm:   tx.vm,
			txID: txID,
		})
	}
	consumedIDs := tx.Tx.ConsumedAssetIDs()
	for assetID := range tx.Tx.AssetIDs() {
		if consumedIDs.Contains(assetID) || txIDs.Contains(assetID) {
			continue
		}
		txIDs.Add(assetID)
		tx.deps = append(tx.deps, &UniqueTx{
			vm:   tx.vm,
			txID: assetID,
		})
	}
	return tx.deps, nil
}

// InputIDs returns the set of utxoIDs this transaction consumes
func (tx *UniqueTx) InputIDs() []ids.ID {
	tx.refresh()
	if tx.Tx == nil || len(tx.inputs) != 0 {
		return tx.inputs
	}

	inputUTXOs := tx.InputUTXOs()
	tx.inputs = make([]ids.ID, len(inputUTXOs))
	for i, utxo := range inputUTXOs {
		tx.inputs[i] = utxo.InputID()
	}
	return tx.inputs
}

// Whitelist is not supported by this transaction type, so [false] is returned.
func (tx *UniqueTx) Whitelist() (ids.Set, bool, error) {
	return nil, false, nil
}

// InputUTXOs returns the utxos that will be consumed on tx acceptance
func (tx *UniqueTx) InputUTXOs() []*avax.UTXOID {
	tx.refresh()
	if tx.Tx == nil || len(tx.inputUTXOs) != 0 {
		return tx.inputUTXOs
	}
	tx.inputUTXOs = tx.Tx.InputUTXOs()
	return tx.inputUTXOs
}

// UTXOs returns the utxos that will be added to the UTXO set on tx acceptance
func (tx *UniqueTx) UTXOs() []*avax.UTXO {
	tx.refresh()
	if tx.Tx == nil || len(tx.utxos) != 0 {
		return tx.utxos
	}
	tx.utxos = tx.Tx.UTXOs()
	return tx.utxos
}

// Bytes returns the binary representation of this transaction
func (tx *UniqueTx) Bytes() []byte {
	tx.refresh()
	return tx.Tx.Bytes()
}

func (tx *UniqueTx) verifyWithoutCacheWrites() error {
	switch status := tx.Status(); status {
	case choices.Unknown:
		return errUnknownTx
	case choices.Accepted:
		return nil
	case choices.Rejected:
		return errRejectedTx
	default:
		return tx.SemanticVerify()
	}
}

// Verify the validity of this transaction
func (tx *UniqueTx) Verify() error {
	if err := tx.verifyWithoutCacheWrites(); err != nil {
		return err
	}

	tx.verifiedState = true
	return nil
}

// SyntacticVerify verifies that this transaction is well formed
func (tx *UniqueTx) SyntacticVerify() error {
	tx.refresh()

	if tx.Tx == nil {
		return errUnknownTx
	}

	if tx.verifiedTx {
		return tx.validity
	}

	tx.verifiedTx = true
	tx.validity = tx.Tx.SyntacticVerify(
		tx.vm.ctx,
		tx.vm.codec,
		tx.vm.feeAssetID,
		tx.vm.TxFee,
		tx.vm.CreateAssetTxFee,
		len(tx.vm.fxs),
	)
	return tx.validity
}

// SemanticVerify the validity of this transaction
func (tx *UniqueTx) SemanticVerify() error {
	// SyntacticVerify sets the error on validity and is checked in the next
	// statement
	_ = tx.SyntacticVerify()

	if tx.validity != nil || tx.verifiedState {
		return tx.validity
	}

	return tx.Tx.SemanticVerify(tx.vm, tx.UnsignedTx)
}
