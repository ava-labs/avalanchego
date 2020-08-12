// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errNoExportOutputs = errors.New("no export outputs")
)

// ExportTx is a transaction that exports an asset to another blockchain.
type ExportTx struct {
	BaseTx `serialize:"true"`

	DestinationChain ids.ID                     `serialize:"true" json:"destinationChain"` // Which chain to send the funds to
	Outs             []*avax.TransferableOutput `serialize:"true" json:"exportedOutputs"`  // The outputs this transaction is sending to the other chain
}

// SyntacticVerify that this transaction is well-formed.
func (t *ExportTx) SyntacticVerify(
	ctx *snow.Context,
	c codec.Codec,
	txFeeAssetID ids.ID,
	txFee uint64,
	_ int,
) error {
	switch {
	case t == nil:
		return errNilTx
	case t.NetID != ctx.NetworkID:
		return errWrongNetworkID
	case !t.BCID.Equals(ctx.ChainID):
		return errWrongChainID
	case len(t.Memo) > maxMemoSize:
		return fmt.Errorf("memo length, %d, exceeds maximum memo length, %d", len(t.Memo), maxMemoSize)
	case t.DestinationChain.IsZero():
		return errWrongBlockchainID
	case len(t.Outs) == 0:
		return errNoExportOutputs
	}

	fc := avax.NewFlowChecker()

	// The txFee must be burned
	fc.Produce(txFeeAssetID, txFee)

	for _, out := range t.BaseTx.Outs {
		if err := out.Verify(); err != nil {
			return err
		}
		fc.Produce(out.AssetID(), out.Output().Amount())
	}
	if !avax.IsSortedTransferableOutputs(t.BaseTx.Outs, c) {
		return errOutputsNotSorted
	}

	for _, out := range t.Outs {
		if err := out.Verify(); err != nil {
			return err
		}
		fc.Produce(out.AssetID(), out.Output().Amount())
	}
	if !avax.IsSortedTransferableOutputs(t.Outs, c) {
		return errOutputsNotSorted
	}

	for _, in := range t.Ins {
		if err := in.Verify(); err != nil {
			return err
		}
		fc.Consume(in.AssetID(), in.Input().Amount())
	}
	if !avax.IsSortedAndUniqueTransferableInputs(t.Ins) {
		return errInputsNotSortedUnique
	}

	return verify.All(fc, &t.Metadata)
}

// SemanticVerify that this transaction is valid to be spent.
func (t *ExportTx) SemanticVerify(vm *VM, uTx *UniqueTx, creds []verify.Verifiable) error {
	subnetID, err := vm.ctx.SNLookup.SubnetID(t.DestinationChain)
	if err != nil {
		return err
	}
	if !vm.ctx.SubnetID.Equals(subnetID) || t.DestinationChain.Equals(vm.ctx.ChainID) {
		return errWrongBlockchainID
	}

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

		if err := fx.VerifyTransfer(uTx, in.In, cred, utxo.Out); err != nil {
			return err
		}
	}

	for _, out := range t.BaseTx.Outs {
		fxIndex, err := vm.getFx(out.Out)
		if err != nil {
			return err
		}
		if assetID := out.AssetID(); !vm.verifyFxUsage(fxIndex, assetID) {
			return errIncompatibleFx
		}
	}

	for _, out := range t.Outs {
		fxIndex, err := vm.getFx(out.Out)
		if err != nil {
			return err
		}
		assetID := out.AssetID()
		if !out.AssetID().Equals(vm.avax) {
			return errWrongAssetID
		}
		if !vm.verifyFxUsage(fxIndex, assetID) {
			return errIncompatibleFx
		}
	}

	return nil
}

// ExecuteWithSideEffects writes the batch with any additional side effects
func (t *ExportTx) ExecuteWithSideEffects(vm *VM, batch database.Batch) error {
	txID := t.ID()

	smDB := vm.ctx.SharedMemory.GetDatabase(t.DestinationChain)
	defer vm.ctx.SharedMemory.ReleaseDatabase(t.DestinationChain)

	vsmDB := versiondb.New(smDB)

	state := avax.NewPrefixedState(vsmDB, vm.codec)
	for i, out := range t.Outs {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(t.BaseTx.Outs) + i),
			},
			Asset: avax.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}
		if err := state.FundAVMUTXO(utxo); err != nil {
			return err
		}
	}

	sharedBatch, err := vsmDB.CommitBatch()
	if err != nil {
		return err
	}

	return atomic.WriteAll(batch, sharedBatch)
}
