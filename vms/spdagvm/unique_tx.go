// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
)

var (
	errInvalidUTXO = errors.New("utxo doesn't exist")
	errMissingUTXO = errors.New("missing utxo")
	errUnknownTx   = errors.New("transaction is unknown")
	errRejectedTx  = errors.New("transaction is rejected")
)

// UniqueTx provides a de-duplication service for txs. This only provides a
// performance boost
type UniqueTx struct {
	vm   *VM
	txID ids.ID
	t    *txState
}

func (tx *UniqueTx) refresh() {
	if tx.t == nil {
		tx.t = &txState{}
	}
	if !tx.t.unique {
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
			*tx = *unique
		}
		switch {
		case tx.t.tx == nil && prevTx == nil:
			if innerTx, err := tx.vm.state.Tx(tx.ID()); err == nil {
				tx.t.tx = innerTx
			}
		case tx.t.tx == nil:
			tx.t.tx = prevTx
		}
	}
}

// Evict is called when this UniqueTx will no longer be returned from a cache
// lookup
func (tx *UniqueTx) Evict() { tx.t.unique = false } // Lock is already held here

// setStatus sets the status of the UniqueTx in memory and in the database
// It will set the status in memory even if it fails to set the status in the database
func (tx *UniqueTx) setStatus(status choices.Status) error {
	tx.refresh()
	if tx.t.status != status {
		tx.t.status = status
		return tx.vm.state.SetStatus(tx.ID(), status)
	}
	return nil
}

func (tx *UniqueTx) addEvents(finalized func(choices.Status)) {
	tx.refresh()

	if finalized != nil {
		tx.t.finalized = append(tx.t.finalized, finalized)
	}
}

// ID returns the wrapped txID
func (tx *UniqueTx) ID() ids.ID { return tx.txID }

// Accept is called when the transaction was finalized as accepted by consensus
func (tx *UniqueTx) Accept() error {
	if err := tx.setStatus(choices.Accepted); err != nil {
		tx.vm.ctx.Log.Error("Failed to accept tx %s due to %s", tx.txID, err)
		return err
	}

	// Remove spent UTXOs
	for _, utxoID := range tx.InputIDs().List() {
		if err := tx.vm.state.SpendUTXO(utxoID); err != nil {
			tx.vm.ctx.Log.Error("Failed to spend utxo %s due to %s", utxoID, err)
			return err
		}
	}

	// Add new UTXOs
	for _, utxo := range tx.utxos() {
		if err := tx.vm.state.FundUTXO(utxo); err != nil {
			tx.vm.ctx.Log.Error("Failed to fund utxo %s due to %s", utxoID, err)
			return err
		}
	}

	for _, finalized := range tx.t.finalized {
		if finalized != nil {
			finalized(choices.Accepted)
		}
	}

	if err := tx.vm.db.Commit(); err != nil {
		tx.vm.ctx.Log.Error("Failed to commit accept %s due to %s", tx.txID, err)
		return err
	}

	tx.t.deps = nil // Needed to prevent a memory leak
	return nil
}

// Reject is called when the transaction was finalized as rejected by consensus
func (tx *UniqueTx) Reject() error {
	if err := tx.setStatus(choices.Rejected); err != nil {
		tx.vm.ctx.Log.Error("Failed to reject tx %s due to %s", tx.txID, err)
		return err
	}

	tx.vm.ctx.Log.Debug("Rejecting Tx: %s", tx.ID())

	for _, finalized := range tx.t.finalized {
		if finalized != nil {
			finalized(choices.Rejected)
		}
	}

	if err := tx.vm.db.Commit(); err != nil {
		tx.vm.ctx.Log.Error("Failed to commit reject %s due to %s", tx.txID, err)
		return err
	}

	tx.t.deps = nil // Needed to prevent a memory leak
	return nil
}

// Status returns the current status of this transaction
func (tx *UniqueTx) Status() choices.Status {
	tx.refresh()
	return tx.t.status
}

// Dependencies returns the set of transactions this transaction builds on
func (tx *UniqueTx) Dependencies() []snowstorm.Tx {
	tx.refresh()
	if tx.t.tx != nil && len(tx.t.deps) == 0 {
		txIDs := ids.Set{}
		for _, in := range tx.t.tx.ins {
			txID, _ := in.InputSource()
			if !txIDs.Contains(txID) {
				txIDs.Add(txID)
				tx.t.deps = append(tx.t.deps, &UniqueTx{
					vm:   tx.vm,
					txID: txID,
				})
			}
		}
	}
	return tx.t.deps
}

// InputIDs returns the set of utxoIDs this transaction consumes
func (tx *UniqueTx) InputIDs() ids.Set {
	tx.refresh()
	if tx.t.tx != nil && tx.t.inputs.Len() == 0 {
		for _, in := range tx.t.tx.ins {
			tx.t.inputs.Add(in.InputID())
		}
	}
	return tx.t.inputs
}

func (tx *UniqueTx) utxos() []*UTXO {
	tx.refresh()
	if tx.t.tx != nil && len(tx.t.tx.outs) != len(tx.t.outputs) {
		tx.t.outputs = tx.t.tx.UTXOs()
	}
	return tx.t.outputs
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
		return tx.VerifyState()
	}
}

// VerifyTx the validity of this transaction
func (tx *UniqueTx) VerifyTx() error {
	tx.refresh()

	if tx.t.verifiedTx {
		return tx.t.validity
	}

	tx.t.verifiedTx = true
	tx.t.validity = tx.t.tx.Verify(tx.vm.ctx, tx.vm.TxFee)
	return tx.t.validity
}

// VerifyState the validity of this transaction
func (tx *UniqueTx) VerifyState() error {
	// VerifyTx sets validity to be checked in the next statement, so the error is ignored here.
	_ = tx.VerifyTx()

	if tx.t.validity != nil || tx.t.verifiedState {
		return tx.t.validity
	}

	tx.t.verifiedState = true

	time := tx.vm.clock.Unix()
	for _, in := range tx.t.tx.ins {
		// Tx is spending spent/non-existent utxo
		// Tx input doesn't unlock output
		if utxo, err := tx.vm.state.UTXO(in.InputID()); err == nil {
			if err := utxo.Out().Unlock(in, time); err != nil {
				tx.t.validity = err
				break
			}
			continue
		}
		inputTx, inputIndex := in.InputSource()
		parent := &UniqueTx{
			vm:   tx.vm,
			txID: inputTx,
		}

		if err := parent.Verify(); err != nil {
			tx.t.validity = errMissingUTXO
		} else if status := parent.Status(); status.Decided() {
			tx.t.validity = errMissingUTXO
		} else if uint32(len(parent.t.tx.outs)) <= inputIndex {
			tx.t.validity = errInvalidUTXO
		} else if err := parent.t.tx.outs[int(inputIndex)].Unlock(in, time); err != nil {
			tx.t.validity = err
		} else {
			continue
		}
		break
	}
	return tx.t.validity
}

type txState struct {
	unique, verifiedTx, verifiedState bool
	validity                          error

	tx      *Tx
	inputs  ids.Set
	outputs []*UTXO
	deps    []snowstorm.Tx

	status choices.Status

	finalized []func(choices.Status)
}
