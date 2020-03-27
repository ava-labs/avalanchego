// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/ava-labs/gecko/vms/components/verify"
)

// ExportTx is the basis of all transactions.
type ExportTx struct {
	BaseTx `serialize:"true"`

	ExportOuts []*ava.TransferableOutput `serialize:"true"` // The outputs this transaction is sending to the other chain
}

// SyntacticVerify that this transaction is well-formed.
func (t *ExportTx) SyntacticVerify(ctx *snow.Context, c codec.Codec, _ int) error {
	switch {
	case t == nil:
		return errNilTx
	case t.NetID != ctx.NetworkID:
		return errWrongNetworkID
	case !t.BCID.Equals(ctx.ChainID):
		return errWrongChainID
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

	for _, out := range t.ExportOuts {
		if err := out.Verify(); err != nil {
			return err
		}
		fc.Produce(out.AssetID(), out.Output().Amount())
	}
	if !ava.IsSortedTransferableOutputs(t.ExportOuts, c) {
		return errOutputsNotSorted
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

	if err := fc.Verify(); err != nil {
		return err
	}

	return t.Metadata.Verify()
}

// SemanticVerify that this transaction is valid to be spent.
func (t *ExportTx) SemanticVerify(vm *VM, uTx *UniqueTx, creds []verify.Verifiable) error {
	for i, in := range t.Ins {
		cred := creds[i]

		fxIndex, err := vm.getFx(cred)
		if err != nil {
			return err
		}
		fx := vm.fxs[fxIndex].Fx

		utxo, err := vm.getUTXO(&in.UTXOID)
		if err != nil {
			return err
		}

		utxoAssetID := utxo.AssetID()
		inAssetID := in.AssetID()
		if !utxoAssetID.Equals(inAssetID) {
			return errAssetIDMismatch
		}

		if !vm.verifyFxUsage(fxIndex, inAssetID) {
			return errIncompatibleFx
		}

		if err := fx.VerifyTransfer(uTx, utxo.Out, in.In, cred); err != nil {
			return err
		}
	}

	for _, out := range t.ExportOuts {
		if !out.AssetID().Equals(vm.ava) {
			return errWrongAssetID
		}
	}

	return nil
}

// ExecuteWithSideEffects writes the batch with any additional side effects
func (t *ExportTx) ExecuteWithSideEffects(vm *VM, batch database.Batch) error {
	txID := t.ID()

	smDB := vm.ctx.SharedMemory.GetDatabase(vm.platform)
	defer vm.ctx.SharedMemory.ReleaseDatabase(vm.platform)

	vsmDB := versiondb.New(smDB)

	state := ava.NewPrefixedState(vsmDB, vm.codec)
	for i, out := range t.ExportOuts {
		utxo := &ava.UTXO{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(t.Outs) + i),
			},
			Asset: ava.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}

		utxoID := utxo.InputID()
		if _, err := state.AVMStatus(utxoID); err == nil {
			if err := state.SetAVMStatus(utxoID, choices.Unknown); err != nil {
				return err
			}
		} else if err := state.SetAVMUTXO(utxoID, utxo); err != nil {
			return err
		}
	}

	sharedBatch, err := vsmDB.CommitBatch()
	if err != nil {
		return err
	}

	return atomic.WriteAll(batch, sharedBatch)
}
