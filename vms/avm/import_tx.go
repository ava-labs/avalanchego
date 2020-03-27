// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"

	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/ava-labs/gecko/vms/components/verify"
)

// ImportTx is a transaction that imports an asset from another blockchain.
type ImportTx struct {
	BaseTx `serialize:"true"`

	Ins []*ava.TransferableInput `serialize:"true"` // The inputs to this transaction
}

// InputUTXOs track which UTXOs this transaction is consuming.
func (t *ImportTx) InputUTXOs() []*ava.UTXOID {
	utxos := t.BaseTx.InputUTXOs()
	for _, in := range t.Ins {
		in.Symbol = true
		utxos = append(utxos, &in.UTXOID)
	}
	return utxos
}

// AssetIDs returns the IDs of the assets this transaction depends on
func (t *ImportTx) AssetIDs() ids.Set {
	assets := t.BaseTx.AssetIDs()
	for _, in := range t.Ins {
		assets.Add(in.AssetID())
	}
	return assets
}

var (
	errNoImportInputs = errors.New("no import inputs")
)

// SyntacticVerify that this transaction is well-formed.
func (t *ImportTx) SyntacticVerify(ctx *snow.Context, c codec.Codec, numFxs int) error {
	switch {
	case t == nil:
		return errNilTx
	case t.NetID != ctx.NetworkID:
		return errWrongNetworkID
	case !t.BCID.Equals(ctx.ChainID):
		return errWrongChainID
	case len(t.Ins) == 0:
		return errNoImportInputs
	}

	fc := ava.NewFlowChecker()
	for _, out := range t.Outs {
		if err := out.Verify(); err != nil {
			return err
		}
		fc.Produce(out.AssetID(), out.Output().Amount())
	}
	if !ava.IsSortedTransferableOutputs(t.Outs, c) {
		return errOutputsNotSorted
	}

	for _, in := range t.BaseTx.Ins {
		if err := in.Verify(); err != nil {
			return err
		}
		fc.Consume(in.AssetID(), in.Input().Amount())
	}
	if !ava.IsSortedAndUniqueTransferableInputs(t.BaseTx.Ins) {
		return errInputsNotSortedUnique
	}

	for _, in := range t.Ins {
		if err := in.Verify(); err != nil {
			return err
		}
		fc.Consume(in.AssetID(), in.Input().Amount())
	}
	if !ava.IsSortedAndUniqueTransferableInputs(t.Ins) {
		return errInputsNotSortedUnique
	}

	// TODO: Add the Tx fee to the produced side

	return fc.Verify()
}

// SemanticVerify that this transaction is well-formed.
func (t *ImportTx) SemanticVerify(vm *VM, uTx *UniqueTx, creds []verify.Verifiable) error {
	if err := t.BaseTx.SemanticVerify(vm, uTx, creds); err != nil {
		return err
	}

	smDB := vm.ctx.SharedMemory.GetDatabase(vm.platform)
	defer vm.ctx.SharedMemory.ReleaseDatabase(vm.platform)

	state := ava.NewPrefixedState(smDB, vm.codec)

	offset := len(t.BaseTx.Ins)
	for i, in := range t.Ins {
		cred := creds[i+offset]

		fxIndex, err := vm.getFx(cred)
		if err != nil {
			return err
		}
		fx := vm.fxs[fxIndex].Fx

		utxoID := in.UTXOID.InputID()
		utxo, err := state.PlatformUTXO(utxoID)
		if err != nil {
			return err
		}
		utxoAssetID := utxo.AssetID()
		inAssetID := in.AssetID()
		if !utxoAssetID.Equals(inAssetID) {
			return errAssetIDMismatch
		}
		if !utxoAssetID.Equals(vm.ava) {
			return errWrongAssetID
		}

		if !vm.verifyFxUsage(fxIndex, inAssetID) {
			return errIncompatibleFx
		}

		if err := fx.VerifyTransfer(uTx, utxo.Out, in.In, cred); err != nil {
			return err
		}
	}
	return nil
}

// ExecuteWithSideEffects writes the batch with any additional side effects
func (t *ImportTx) ExecuteWithSideEffects(vm *VM, batch database.Batch) error {
	smDB := vm.ctx.SharedMemory.GetDatabase(vm.platform)
	defer vm.ctx.SharedMemory.ReleaseDatabase(vm.platform)

	vsmDB := versiondb.New(smDB)

	state := ava.NewPrefixedState(vsmDB, vm.codec)
	for _, in := range t.Ins {
		utxoID := in.UTXOID.InputID()
		if err := state.SpendPlatformUTXO(utxoID); err != nil {
			return err
		}
	}

	sharedBatch, err := vsmDB.CommitBatch()
	if err != nil {
		return err
	}

	return atomic.WriteAll(batch, sharedBatch)
}
