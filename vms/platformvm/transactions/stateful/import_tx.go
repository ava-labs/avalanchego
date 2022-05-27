// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"
)

var _ AtomicTx = &ImportTx{}

type ImportTx struct {
	*unsigned.ImportTx

	txID  ids.ID // ID of signed add subnet validator tx
	creds []verify.Verifiable

	verifier TxVerifier
}

// Attempts to verify this transaction with the provided state.
func (tx *ImportTx) SemanticVerify(parentState state.Mutable) error {
	_, err := tx.AtomicExecute(parentState)
	return err
}

// Execute this transaction.
func (tx *ImportTx) Execute(vs state.Versioned) (func() error, error) {
	ctx := tx.verifier.Ctx()

	if err := tx.SyntacticVerify(ctx); err != nil {
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

	if tx.verifier.Bootstrapped() {
		if err := verify.SameSubnet(ctx, tx.SourceChain); err != nil {
			return nil, err
		}

		utxoIDs := make([][]byte, len(tx.ImportedInputs))
		for i, in := range tx.ImportedInputs {
			utxoID := in.UTXOID.InputID()
			utxoIDs[i] = utxoID[:]
		}
		allUTXOBytes, err := ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to get shared memory: %w", err)
		}

		for i, utxoBytes := range allUTXOBytes {
			utxo := &avax.UTXO{}
			if _, err := unsigned.Codec.Unmarshal(utxoBytes, utxo); err != nil {
				return nil, fmt.Errorf("failed to unmarshal UTXO: %w", err)
			}
			utxosList[i+len(tx.Ins)] = utxo
		}

		ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
		copy(ins, tx.Ins)
		copy(ins[len(tx.Ins):], tx.ImportedInputs)

		if err := tx.verifier.SemanticVerifySpendUTXOs(
			tx,
			utxosList,
			ins,
			tx.Outs,
			tx.creds,
			tx.verifier.PlatformConfig().TxFee,
			ctx.AVAXAssetID,
		); err != nil {
			return nil, err
		}
	}

	// Consume the UTXOS
	utxos.ConsumeInputs(vs, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(vs, tx.txID, ctx.AVAXAssetID, tx.Outs)
	return nil, nil
}

// AtomicOperations returns the shared memory requests
func (tx *ImportTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoID := in.InputID()
		utxoIDs[i] = utxoID[:]
	}
	return tx.SourceChain, &atomic.Requests{RemoveRequests: utxoIDs}, nil
}

// [AtomicExecute] to maintain consistency for the standard block.
func (tx *ImportTx) AtomicExecute(parentState state.Mutable) (state.Versioned, error) {
	// Set up the state if this tx is committed
	newState := state.NewVersioned(
		parentState,
		parentState.CurrentStakerChainState(),
		parentState.PendingStakerChainState(),
	)
	_, err := tx.Execute(newState)
	return newState, err
}

// Accept this transaction and spend imported inputs
// We spend imported UTXOs here rather than in semanticVerify because
// we don't want to remove an imported UTXO in semanticVerify
// only to have the transaction not be Accepted. This would be inconsistent.
// Recall that imported UTXOs are not kept in a versionDB.
func (tx *ImportTx) AtomicAccept(ctx *snow.Context, batch database.Batch) error {
	chainID, requests, err := tx.AtomicOperations()
	if err != nil {
		return err
	}
	return ctx.SharedMemory.Apply(map[ids.ID]*atomic.Requests{chainID: requests}, batch)
}
