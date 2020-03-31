// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
)

var (
	errAssetIDMismatch = errors.New("asset IDs in the input don't match the utxo")
	errMissingUTXO     = errors.New("missing utxo")
	errUnknownTx       = errors.New("transaction is unknown")
	errRejectedTx      = errors.New("transaction is rejected")
)

// UniqueTx provides a de-duplication service for txs. This only provides a
// performance boost
type UniqueTx struct {
	*TxState
	vm   *VM
	txID ids.ID
}

// TxState ...
type TxState struct {
	*Tx

	unique, verifiedTx, verifiedState bool
	validity                          error

	inputs     ids.Set
	inputUTXOs []*UTXOID
	utxos      []*UTXO
	deps       []snowstorm.Tx

	status choices.Status

	onDecide func(choices.Status)
}

func (tx *UniqueTx) refresh() {
	if tx.TxState == nil {
		tx.TxState = &TxState{}
	}
	if tx.unique {
		return
	}
	unique := tx.vm.state.UniqueTx(tx)
	prevTx := tx.Tx
	if unique == tx {
		// If no one was in the cache, make sure that there wasn't an
		// intermediate object whose state I must reflect
		if status, err := tx.vm.state.Status(tx.ID()); err == nil {
			tx.status = status
			tx.unique = true
		}
	} else {
		// If someone is in the cache, they must be up to date

		// This ensures that every unique tx object points to the same tx state
		tx.TxState = unique.TxState
	}

	if tx.Tx != nil {
		return
	}

	if prevTx == nil {
		if innerTx, err := tx.vm.state.Tx(tx.ID()); err == nil {
			tx.Tx = innerTx
		}
	} else {
		tx.Tx = prevTx
	}
}

// Evict is called when this UniqueTx will no longer be returned from a cache
// lookup
func (tx *UniqueTx) Evict() { tx.unique = false } // Lock is already held here

func (tx *UniqueTx) setStatus(status choices.Status) error {
	tx.refresh()
	if tx.status == status {
		return nil
	}
	tx.status = status
	return tx.vm.state.SetStatus(tx.ID(), status)
}

// ID returns the wrapped txID
func (tx *UniqueTx) ID() ids.ID { return tx.txID }

// Accept is called when the transaction was finalized as accepted by consensus
func (tx *UniqueTx) Accept() {
	if err := tx.setStatus(choices.Accepted); err != nil {
		tx.vm.ctx.Log.Error("Failed to accept tx %s due to %s", tx.txID, err)
		return
	}

	// Remove spent utxos
	for _, utxoID := range tx.InputIDs().List() {
		if err := tx.vm.state.SpendUTXO(utxoID); err != nil {
			tx.vm.ctx.Log.Error("Failed to spend utxo %s due to %s", utxoID, err)
			return
		}
	}

	// Add new utxos
	for _, utxo := range tx.UTXOs() {
		if err := tx.vm.state.FundUTXO(utxo); err != nil {
			tx.vm.ctx.Log.Error("Failed to fund utxo %s due to %s", utxoID, err)
			return
		}
	}

	txID := tx.ID()
	tx.vm.ctx.Log.Verbo("Accepting Tx: %s", txID)

	if err := tx.vm.db.Commit(); err != nil {
		tx.vm.ctx.Log.Error("Failed to commit accept %s due to %s", tx.txID, err)
	}

	tx.vm.pubsub.Publish("accepted", txID)

	tx.deps = nil // Needed to prevent a memory leak

	if tx.onDecide != nil {
		tx.onDecide(choices.Accepted)
	}
}

// Reject is called when the transaction was finalized as rejected by consensus
func (tx *UniqueTx) Reject() {
	if err := tx.setStatus(choices.Rejected); err != nil {
		tx.vm.ctx.Log.Error("Failed to reject tx %s due to %s", tx.txID, err)
		return
	}

	txID := tx.ID()
	tx.vm.ctx.Log.Debug("Rejecting Tx: %s", txID)

	if err := tx.vm.db.Commit(); err != nil {
		tx.vm.ctx.Log.Error("Failed to commit reject %s due to %s", tx.txID, err)
	}

	tx.vm.pubsub.Publish("rejected", txID)

	tx.deps = nil // Needed to prevent a memory leak

	if tx.onDecide != nil {
		tx.onDecide(choices.Rejected)
	}
}

// Status returns the current status of this transaction
func (tx *UniqueTx) Status() choices.Status {
	tx.refresh()
	return tx.status
}

// Dependencies returns the set of transactions this transaction builds on
func (tx *UniqueTx) Dependencies() []snowstorm.Tx {
	tx.refresh()
	if tx.Tx == nil || len(tx.deps) != 0 {
		return tx.deps
	}

	txIDs := ids.Set{}
	for _, in := range tx.InputUTXOs() {
		txID, _ := in.InputSource()
		if !txIDs.Contains(txID) {
			txIDs.Add(txID)
			tx.deps = append(tx.deps, &UniqueTx{
				vm:   tx.vm,
				txID: txID,
			})
		}
	}
	for _, assetID := range tx.Tx.AssetIDs().List() {
		if !txIDs.Contains(assetID) {
			txIDs.Add(assetID)
			tx.deps = append(tx.deps, &UniqueTx{
				vm:   tx.vm,
				txID: assetID,
			})
		}
	}
	return tx.deps
}

// InputIDs returns the set of utxoIDs this transaction consumes
func (tx *UniqueTx) InputIDs() ids.Set {
	tx.refresh()
	if tx.Tx == nil || tx.inputs.Len() != 0 {
		return tx.inputs
	}

	for _, utxo := range tx.InputUTXOs() {
		tx.inputs.Add(utxo.InputID())
	}
	return tx.inputs
}

// InputUTXOs returns the utxos that will be consumed on tx acceptance
func (tx *UniqueTx) InputUTXOs() []*UTXOID {
	tx.refresh()
	if tx.Tx == nil || len(tx.inputUTXOs) != 0 {
		return tx.inputUTXOs
	}
	tx.inputUTXOs = tx.Tx.InputUTXOs()
	return tx.inputUTXOs
}

// UTXOs returns the utxos that will be added to the UTXO set on tx acceptance
func (tx *UniqueTx) UTXOs() []*UTXO {
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

// Verify the validity of this transaction
func (tx *UniqueTx) Verify() error {
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
	tx.validity = tx.Tx.SyntacticVerify(tx.vm.ctx, tx.vm.codec, len(tx.vm.fxs))
	return tx.validity
}

// SemanticVerify the validity of this transaction
func (tx *UniqueTx) SemanticVerify() error {
	tx.SyntacticVerify()

	if tx.validity != nil || tx.verifiedState {
		return tx.validity
	}

	tx.verifiedState = true
	tx.validity = tx.Tx.SemanticVerify(tx.vm, tx)

	if tx.validity == nil {
		tx.vm.pubsub.Publish("verified", tx.ID())
	}
	return tx.validity
}

// UnsignedBytes returns the unsigned bytes of the transaction
func (tx *UniqueTx) UnsignedBytes() []byte {
	b, err := tx.vm.codec.Marshal(&tx.Tx.UnsignedTx)
	tx.vm.ctx.Log.AssertNoError(err)
	return b
}
