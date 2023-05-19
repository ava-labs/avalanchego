// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var (
	errMissingUTXO = errors.New("missing utxo")
	errUnknownTx   = errors.New("transaction is unknown")
	errRejectedTx  = errors.New("transaction is rejected")
)

var (
	_ snowstorm.Tx            = (*UniqueTx)(nil)
	_ cache.Evictable[ids.ID] = (*UniqueTx)(nil)
)

// UniqueTx provides a de-duplication service for txs. This only provides a
// performance boost
type UniqueTx struct {
	*TxCachedState

	vm   *VM
	txID ids.ID
}

type TxCachedState struct {
	*txs.Tx

	unique, verifiedTx, verifiedState bool
	validity                          error

	inputs     []ids.ID
	inputUTXOs []*avax.UTXOID
	utxos      []*avax.UTXO
	deps       []snowstorm.Tx

	status choices.Status
}

func (tx *UniqueTx) refresh() {
	tx.vm.metrics.IncTxRefreshes()

	if tx.TxCachedState == nil {
		tx.TxCachedState = &TxCachedState{}
	}
	if tx.unique {
		return
	}
	unique := tx.vm.DeduplicateTx(tx)
	prevTx := tx.Tx
	if unique == tx {
		tx.vm.metrics.IncTxRefreshMisses()

		// If no one was in the cache, make sure that there wasn't an
		// intermediate object whose state I must reflect
		if status, err := tx.vm.state.GetStatus(tx.ID()); err == nil {
			tx.status = status
		}
		tx.unique = true
	} else {
		tx.vm.metrics.IncTxRefreshHits()

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

func (tx *UniqueTx) setStatus(status choices.Status) {
	tx.refresh()
	if tx.status != status {
		tx.status = status
		tx.vm.state.AddStatus(tx.ID(), status)
	}
}

// ID returns the wrapped txID
func (tx *UniqueTx) ID() ids.ID {
	return tx.txID
}

func (tx *UniqueTx) Key() ids.ID {
	return tx.txID
}

// Accept is called when the transaction was finalized as accepted by consensus
func (tx *UniqueTx) Accept(context.Context) error {
	if s := tx.Status(); s != choices.Processing {
		return fmt.Errorf("transaction has invalid status: %s", s)
	}

	if err := tx.vm.onAccept(tx.Tx); err != nil {
		return err
	}

	executor := &executor.Executor{
		Codec: tx.vm.txBackend.Codec,
		State: tx.vm.state,
		Tx:    tx.Tx,
	}
	err := tx.Tx.Unsigned.Visit(executor)
	if err != nil {
		return fmt.Errorf("error staging accepted state changes: %w", err)
	}

	tx.setStatus(choices.Accepted)

	commitBatch, err := tx.vm.state.CommitBatch()
	if err != nil {
		txID := tx.ID()
		return fmt.Errorf("couldn't create commitBatch while processing tx %s: %w", txID, err)
	}

	defer tx.vm.state.Abort()
	err = tx.vm.ctx.SharedMemory.Apply(
		executor.AtomicRequests,
		commitBatch,
	)
	if err != nil {
		txID := tx.ID()
		return fmt.Errorf("error committing accepted state changes while processing tx %s: %w", txID, err)
	}

	tx.deps = nil // Needed to prevent a memory leak
	return tx.vm.metrics.MarkTxAccepted(tx.Tx)
}

// Reject is called when the transaction was finalized as rejected by consensus
func (tx *UniqueTx) Reject(context.Context) error {
	tx.setStatus(choices.Rejected)

	txID := tx.ID()
	tx.vm.ctx.Log.Debug("rejecting tx",
		zap.Stringer("txID", txID),
	)

	if err := tx.vm.state.Commit(); err != nil {
		tx.vm.ctx.Log.Error("failed to commit reject",
			zap.Stringer("txID", tx.txID),
			zap.Error(err),
		)
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

	txIDs := set.Set[ids.ID]{}
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
	consumedIDs := tx.Tx.Unsigned.ConsumedAssetIDs()
	for assetID := range tx.Tx.Unsigned.AssetIDs() {
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

// InputUTXOs returns the utxos that will be consumed on tx acceptance
func (tx *UniqueTx) InputUTXOs() []*avax.UTXOID {
	tx.refresh()
	if tx.Tx == nil || len(tx.inputUTXOs) != 0 {
		return tx.inputUTXOs
	}
	tx.inputUTXOs = tx.Tx.Unsigned.InputUTXOs()
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
func (tx *UniqueTx) Verify(context.Context) error {
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
	tx.validity = tx.Tx.Unsigned.Visit(&executor.SyntacticVerifier{
		Backend: tx.vm.txBackend,
		Tx:      tx.Tx,
	})
	return tx.validity
}

// SemanticVerify the validity of this transaction
func (tx *UniqueTx) SemanticVerify() error {
	if err := tx.SyntacticVerify(); err != nil {
		return err
	}

	if tx.validity != nil || tx.verifiedState {
		return tx.validity
	}

	return tx.Unsigned.Visit(&executor.SemanticVerifier{
		Backend: tx.vm.txBackend,
		State:   tx.vm.dagState,
		Tx:      tx.Tx,
	})
}
