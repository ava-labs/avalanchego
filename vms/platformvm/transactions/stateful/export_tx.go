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
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"
)

var _ AtomicTx = &ExportTx{}

type ExportTx struct {
	*unsigned.ExportTx

	txID        ids.ID // ID of signed add subnet validator tx
	signedBytes []byte // signed Tx bytes, needed to recreate signed.Tx
}

// InputUTXOs returns an empty set
func (tx *ExportTx) InputUTXOs() ids.Set { return nil }

// Attempts to verify this transaction with the provided state.
func (tx *ExportTx) SemanticVerify(
	verifier TxVerifier,
	parentState state.Mutable,
	creds []verify.Verifiable,
) error {
	_, err := tx.AtomicExecute(verifier, parentState, creds)
	return err
}

// Execute this transaction.
func (tx *ExportTx) Execute(
	verifier TxVerifier,
	vs state.Versioned,
	creds []verify.Verifiable,
) (func() error, error) {
	ctx := verifier.Ctx()

	stx := &signed.Tx{
		Unsigned: tx.ExportTx,
		Creds:    creds,
	}
	stx.Initialize(tx.UnsignedBytes(), tx.signedBytes)
	if err := stx.SyntacticVerify(ctx); err != nil {
		return nil, err
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOutputs)

	if verifier.Bootstrapped() {
		if err := verify.SameSubnet(ctx, tx.DestinationChain); err != nil {
			return nil, err
		}
	}

	// Verify the flowcheck
	if err := verifier.SemanticVerifySpend(
		vs,
		tx,
		tx.Ins,
		outs,
		creds,
		verifier.PlatformConfig().TxFee,
		ctx.AVAXAssetID,
	); err != nil {
		return nil, fmt.Errorf("failed semanticVerifySpend: %w", err)
	}

	// Consume the UTXOS
	utxos.ConsumeInputs(vs, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(vs, tx.txID, ctx.AVAXAssetID, tx.Outs)
	return nil, nil
}

// AtomicOperations returns the shared memory requests
func (tx *ExportTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	elems := make([]*atomic.Element, len(tx.ExportedOutputs))
	for i, out := range tx.ExportedOutputs {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        tx.txID,
				OutputIndex: uint32(len(tx.Outs) + i),
			},
			Asset: avax.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}

		utxoBytes, err := unsigned.Codec.Marshal(unsigned.Version, utxo)
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
func (tx *ExportTx) AtomicExecute(
	verifier TxVerifier,
	parentState state.Mutable,
	creds []verify.Verifiable,
) (state.Versioned, error) {
	// Set up the state if this tx is committed
	newState := state.NewVersioned(
		parentState,
		parentState.CurrentStakerChainState(),
		parentState.PendingStakerChainState(),
	)
	_, err := tx.Execute(verifier, newState, creds)
	return newState, err
}

// Accept this transaction.
func (tx *ExportTx) AtomicAccept(ctx *snow.Context, batch database.Batch) error {
	chainID, requests, err := tx.AtomicOperations()
	if err != nil {
		return err
	}
	return ctx.SharedMemory.Apply(map[ids.ID]*atomic.Requests{chainID: requests}, batch)
}
