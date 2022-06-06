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
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ StatefulAtomicTx = &StatefulExportTx{}

	errOverflowExport = errors.New("overflow when computing export amount + txFee")
)

// StatefulExportTx is an unsigned ExportTx
type StatefulExportTx struct {
	*unsigned.ExportTx `serialize:"true"`

	txID ids.ID // ID of signed export tx
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
	if err := stx.SyntacticVerify(vm.ctx); err != nil {
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
	produceOutputs(vs, tx.txID, vm.ctx.AVAXAssetID, tx.Outs)
	return nil, nil
}

// AtomicOperations returns the shared memory requests
func (tx *StatefulExportTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
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

// Create a new transaction
func (vm *VM) newExportTx(
	amount uint64, // Amount of tokens to export
	chainID ids.ID, // Chain to send the UTXOs to
	to ids.ShortID, // Address of chain recipient
	keys []*crypto.PrivateKeySECP256K1R, // Pay the fee and provide the tokens
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*signed.Tx, error) {
	toBurn, err := math.Add64(amount, vm.TxFee)
	if err != nil {
		return nil, errOverflowExport
	}
	ins, outs, _, signers, err := vm.stake(keys, 0, toBurn, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the transaction
	utx := &unsigned.ExportTx{
		BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Ins:          ins,
			Outs:         outs, // Non-exported outputs
		}},
		DestinationChain: chainID,
		ExportedOutputs: []*avax.TransferableOutput{{ // Exported to X-Chain
			Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		}},
	}
	tx, err := signed.NewSigned(utx, unsigned.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(vm.ctx)
}
