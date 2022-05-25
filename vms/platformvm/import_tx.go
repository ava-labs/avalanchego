// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"
)

var (
	_ StatefulAtomicTx = &StatefulImportTx{}

	errWrongNumberOfCredentials = errors.New("should have the same number of credentials as inputs")
)

// StatefulImportTx is an unsigned ImportTx
type StatefulImportTx struct {
	*unsigned.ImportTx `serialize:"true"`

	txID ids.ID // ID of signed create subnet tx
}

// InputUTXOs returns the UTXOIDs of the imported funds
func (tx *StatefulImportTx) InputUTXOs() ids.Set {
	set := ids.NewSet(len(tx.ImportedInputs))
	for _, in := range tx.ImportedInputs {
		set.Add(in.InputID())
	}
	return set
}

func (tx *StatefulImportTx) InputIDs() ids.Set {
	inputs := tx.BaseTx.InputIDs()
	atomicInputs := tx.InputUTXOs()
	inputs.Union(atomicInputs)
	return inputs
}

// Attempts to verify this transaction with the provided state.
func (tx *StatefulImportTx) SemanticVerify(vm *VM, parentState state.Mutable, stx *signed.Tx) error {
	_, err := tx.AtomicExecute(vm, parentState, stx)
	return err
}

// Execute this transaction.
func (tx *StatefulImportTx) Execute(
	vm *VM,
	vs state.Versioned,
	stx *signed.Tx,
) (func() error, error) {
	if err := stx.SyntacticVerify(vm.ctx); err != nil {
		return nil, err
	}

	utxosList := make([]*avax.UTXO, len(tx.Ins)+len(tx.ImportedInputs))
	for index, input := range tx.Ins {
		utxo, err := vs.GetUTXO(input.InputID())
		if err != nil {
			return nil, fmt.Errorf("failed to get UTXO %s: %w", &input.UTXOID, err)
		}
		utxosList[index] = utxo
	}

	if vm.bootstrapped.GetValue() {
		if err := verify.SameSubnet(vm.ctx, tx.SourceChain); err != nil {
			return nil, err
		}

		utxoIDs := make([][]byte, len(tx.ImportedInputs))
		for i, in := range tx.ImportedInputs {
			utxoID := in.UTXOID.InputID()
			utxoIDs[i] = utxoID[:]
		}
		allUTXOBytes, err := vm.ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to get shared memory: %w", err)
		}

		for i, utxoBytes := range allUTXOBytes {
			utxo := &avax.UTXO{}
			if _, err := Codec.Unmarshal(utxoBytes, utxo); err != nil {
				return nil, fmt.Errorf("failed to unmarshal UTXO: %w", err)
			}
			utxosList[i+len(tx.Ins)] = utxo
		}

		ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
		copy(ins, tx.Ins)
		copy(ins[len(tx.Ins):], tx.ImportedInputs)

		if err := vm.spendOps.SemanticVerifySpendUTXOs(tx, utxosList, ins, tx.Outs, stx.Creds, vm.TxFee, vm.ctx.AVAXAssetID); err != nil {
			return nil, err
		}
	}

	// Consume the UTXOS
	utxos.ConsumeInputs(vs, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(vs, tx.txID, vm.ctx.AVAXAssetID, tx.Outs)
	return nil, nil
}

// AtomicOperations returns the shared memory requests
func (tx *StatefulImportTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoID := in.InputID()
		utxoIDs[i] = utxoID[:]
	}
	return tx.SourceChain, &atomic.Requests{RemoveRequests: utxoIDs}, nil
}

// [AtomicExecute] to maintain consistency for the standard block.
func (tx *StatefulImportTx) AtomicExecute(
	vm *VM,
	parentState state.Mutable,
	stx *signed.Tx,
) (state.Versioned, error) {
	// Set up the state if this tx is committed
	newState := state.NewVersioned(
		parentState,
		parentState.CurrentStakerChainState(),
		parentState.PendingStakerChainState(),
	)
	_, err := tx.Execute(vm, newState, stx)
	return newState, err
}

// Accept this transaction and spend imported inputs
// We spend imported UTXOs here rather than in semanticVerify because
// we don't want to remove an imported UTXO in semanticVerify
// only to have the transaction not be Accepted. This would be inconsistent.
// Recall that imported UTXOs are not kept in a versionDB.
func (tx *StatefulImportTx) AtomicAccept(ctx *snow.Context, batch database.Batch) error {
	chainID, requests, err := tx.AtomicOperations()
	if err != nil {
		return err
	}
	return ctx.SharedMemory.Apply(map[ids.ID]*atomic.Requests{chainID: requests}, batch)
}
