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
	vm   *VM
	txID ids.ID
	t    *txState
}

type txState struct {
	unique, verifiedTx, verifiedState bool
	validity                          error

	tx         *Tx
	inputs     ids.Set
	inputUTXOs []*UTXOID
	utxos      []*UTXO
	deps       []snowstorm.Tx

	status choices.Status

	onDecide func(choices.Status)
}

func (tx *UniqueTx) refresh() {
	if tx.t == nil {
		tx.t = &txState{}
	}
	if tx.t.unique {
		return
	}
	unique := tx.vm.state.UniqueTx(tx)
	prevTx := tx.t.tx
	if unique == tx {
		// If no one was in the cache, make sure that there wasn't an
		// intermediate object whose state I must reflect
		if status, err := tx.vm.state.Status(tx.ID()); err == nil {
			tx.t.status = status
			tx.t.unique = true
		}
	} else {
		// If someone is in the cache, they must be up to date

		// This ensures that every unique tx object points to the same tx state
		tx.t = unique.t
	}

	if tx.t.tx != nil {
		return
	}

	if prevTx == nil {
		if innerTx, err := tx.vm.state.Tx(tx.ID()); err == nil {
			tx.t.tx = innerTx
		}
	} else {
		tx.t.tx = prevTx
	}
}

// Evict is called when this UniqueTx will no longer be returned from a cache
// lookup
func (tx *UniqueTx) Evict() { tx.t.unique = false } // Lock is already held here

func (tx *UniqueTx) setStatus(status choices.Status) error {
	tx.refresh()
	if tx.t.status == status {
		return nil
	}
	tx.t.status = status
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

	tx.t.deps = nil // Needed to prevent a memory leak

	if tx.t.onDecide != nil {
		tx.t.onDecide(choices.Accepted)
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

	tx.t.deps = nil // Needed to prevent a memory leak

	if tx.t.onDecide != nil {
		tx.t.onDecide(choices.Rejected)
	}
}

// Status returns the current status of this transaction
func (tx *UniqueTx) Status() choices.Status {
	tx.refresh()
	return tx.t.status
}

// Dependencies returns the set of transactions this transaction builds on
func (tx *UniqueTx) Dependencies() []snowstorm.Tx {
	tx.refresh()
	if tx.t.tx == nil || len(tx.t.deps) != 0 {
		return tx.t.deps
	}

	txIDs := ids.Set{}
	for _, in := range tx.InputUTXOs() {
		txID, _ := in.InputSource()
		if !txIDs.Contains(txID) {
			txIDs.Add(txID)
			tx.t.deps = append(tx.t.deps, &UniqueTx{
				vm:   tx.vm,
				txID: txID,
			})
		}
	}
	for _, assetID := range tx.t.tx.AssetIDs().List() {
		if !txIDs.Contains(assetID) {
			txIDs.Add(assetID)
			tx.t.deps = append(tx.t.deps, &UniqueTx{
				vm:   tx.vm,
				txID: assetID,
			})
		}
	}
	return tx.t.deps
}

// InputIDs returns the set of utxoIDs this transaction consumes
func (tx *UniqueTx) InputIDs() ids.Set {
	tx.refresh()
	if tx.t.tx == nil || tx.t.inputs.Len() != 0 {
		return tx.t.inputs
	}

	for _, utxo := range tx.InputUTXOs() {
		tx.t.inputs.Add(utxo.InputID())
	}
	return tx.t.inputs
}

// InputUTXOs returns the utxos that will be consumed on tx acceptance
func (tx *UniqueTx) InputUTXOs() []*UTXOID {
	tx.refresh()
	if tx.t.tx == nil || len(tx.t.inputUTXOs) != 0 {
		return tx.t.inputUTXOs
	}
	tx.t.inputUTXOs = tx.t.tx.InputUTXOs()
	return tx.t.inputUTXOs
}

// UTXOs returns the utxos that will be added to the UTXO set on tx acceptance
func (tx *UniqueTx) UTXOs() []*UTXO {
	tx.refresh()
	if tx.t.tx == nil || len(tx.t.utxos) != 0 {
		return tx.t.utxos
	}
	tx.t.utxos = tx.t.tx.UTXOs()
	return tx.t.utxos
}

// Bytes returns the binary representation of this transaction
func (tx *UniqueTx) Bytes() []byte {
	tx.refresh()
	return tx.t.tx.Bytes()
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

	if tx.t.tx == nil {
		return errUnknownTx
	}

	if tx.t.verifiedTx {
		return tx.t.validity
	}

	tx.t.verifiedTx = true
	tx.t.validity = tx.t.tx.SyntacticVerify(tx.vm.ctx, tx.vm.codec, len(tx.vm.fxs))
	return tx.t.validity
}

// SemanticVerify the validity of this transaction
func (tx *UniqueTx) SemanticVerify() error {
	tx.SyntacticVerify()

	if tx.t.validity != nil || tx.t.verifiedState {
		return tx.t.validity
	}

	tx.t.verifiedState = true
	tx.t.validity = tx.t.tx.SemanticVerify(tx.vm, tx)

	if tx.t.validity == nil {
		tx.vm.pubsub.Publish("verified", tx.ID())
	}
	return tx.t.validity
}

// UnsignedBytes returns the unsigned bytes of the transaction
func (tx *UniqueTx) UnsignedBytes() []byte {
	b, err := tx.vm.codec.Marshal(&tx.t.tx.UnsignedTx)
	tx.vm.ctx.Log.AssertNoError(err)
	return b
}
