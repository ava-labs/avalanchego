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
)

var (
	_ StatefulAtomicTx = &StatefulExportTx{}

	errOverflowExport = errors.New("overflow when computing export amount + txFee")
)

// StatefulExportTx is an unsigned ExportTx
type StatefulExportTx struct {
	*unsigned.ExportTx `serialize:"true"`
}

// InputUTXOs returns an empty set
func (tx *StatefulExportTx) InputUTXOs() ids.Set { return nil }

// Attempts to verify this transaction with the provided state.
func (tx *StatefulExportTx) SemanticVerify(vm *VM, parentState state.Mutable, stx *signed.Tx) error {
	_, err := tx.AtomicExecute(vm, parentState, stx)
	return err
}

// Execute this transaction.
func (tx *StatefulExportTx) Execute(
	vm *VM,
	vs state.Versioned,
	stx *signed.Tx,
) (func() error, error) {
	if err := tx.SyntacticVerify(vm.ctx); err != nil {
		return nil, err
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOutputs)

	if vm.bootstrapped.GetValue() {
		if err := verify.SameSubnet(vm.ctx, tx.DestinationChain); err != nil {
			return nil, err
		}
	}

	// Verify the flowcheck
	if err := vm.semanticVerifySpend(
		vs,
		tx.ExportTx,
		tx.Ins,
		outs,
		stx.Creds,
		vm.TxFee,
		vm.ctx.AVAXAssetID,
	); err != nil {
		return nil, fmt.Errorf("failed semanticVerifySpend: %w", err)
	}

	// Consume the UTXOS
	consumeInputs(vs, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	produceOutputs(vs, txID, vm.ctx.AVAXAssetID, tx.Outs)
	return nil, nil
}

// AtomicOperations returns the shared memory requests
func (tx *StatefulExportTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	txID := tx.ID()

	elems := make([]*atomic.Element, len(tx.ExportedOutputs))
	for i, out := range tx.ExportedOutputs {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(tx.Outs) + i),
			},
			Asset: avax.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}

		utxoBytes, err := Codec.Marshal(CodecVersion, utxo)
		if err != nil {
			return ids.ID{}, nil, fmt.Errorf("failed to marshal UTXO: %w", err)
		}
		utxoID := utxo.InputID()
		elem := &atomic.Element{
			Key:   utxoID[:],
			Value: utxoBytes,
		}
		if out, ok := utxo.Out.(avax.Addressable); ok {
			elem.Traits = out.Addresses()
		}

		elems[i] = elem
	}
	return tx.DestinationChain, &atomic.Requests{PutRequests: elems}, nil
}

// Execute this transaction and return the versioned state.
func (tx *StatefulExportTx) AtomicExecute(
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

// Accept this transaction.
func (tx *StatefulExportTx) AtomicAccept(ctx *snow.Context, batch database.Batch) error {
	chainID, requests, err := tx.AtomicOperations()
	if err != nil {
		return err
	}
	return ctx.SharedMemory.Apply(map[ids.ID]*atomic.Requests{chainID: requests}, batch)
}
