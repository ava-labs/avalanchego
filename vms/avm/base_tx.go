// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errNilTx          = errors.New("nil tx is not valid")
	errWrongNetworkID = errors.New("tx has wrong network ID")
	errWrongChainID   = errors.New("tx has wrong chain ID")

	errOutputsNotSorted      = errors.New("outputs not sorted")
	errInputsNotSortedUnique = errors.New("inputs not sorted and unique")

	errInputOverflow     = errors.New("inputs overflowed uint64")
	errOutputOverflow    = errors.New("outputs overflowed uint64")
	errInsufficientFunds = errors.New("insufficient funds")
)

// BaseTx is the basis of all transactions.
type BaseTx struct {
	metadata

	NetID uint32                `serialize:"true"` // ID of the network this chain lives on
	BCID  ids.ID                `serialize:"true"` // ID of the chain on which this transaction exists (prevents replay attacks)
	Outs  []*TransferableOutput `serialize:"true"` // The outputs of this transaction
	Ins   []*TransferableInput  `serialize:"true"` // The inputs to this transaction
}

// NetworkID is the ID of the network on which this transaction exists
func (t *BaseTx) NetworkID() uint32 { return t.NetID }

// ChainID is the ID of the chain on which this transaction exists
func (t *BaseTx) ChainID() ids.ID { return t.BCID }

// Outputs track which outputs this transaction is producing. The returned array
// should not be modified.
func (t *BaseTx) Outputs() []*TransferableOutput { return t.Outs }

// Inputs track which UTXOs this transaction is consuming. The returned array
// should not be modified.
func (t *BaseTx) Inputs() []*TransferableInput { return t.Ins }

// InputUTXOs track which UTXOs this transaction is consuming.
func (t *BaseTx) InputUTXOs() []*UTXOID {
	utxos := []*UTXOID(nil)
	for _, in := range t.Ins {
		utxos = append(utxos, &in.UTXOID)
	}
	return utxos
}

// AssetIDs returns the IDs of the assets this transaction depends on
func (t *BaseTx) AssetIDs() ids.Set {
	assets := ids.Set{}
	for _, in := range t.Ins {
		assets.Add(in.AssetID())
	}
	return assets
}

// UTXOs returns the UTXOs transaction is producing.
func (t *BaseTx) UTXOs() []*UTXO {
	txID := t.ID()
	utxos := make([]*UTXO, len(t.Outs))
	for i, out := range t.Outs {
		utxos[i] = &UTXO{
			UTXOID: UTXOID{
				TxID:        txID,
				OutputIndex: uint32(i),
			},
			Asset: Asset{
				ID: out.AssetID(),
			},
			Out: out.Out,
		}
	}
	return utxos
}

// SyntacticVerify that this transaction is well-formed.
func (t *BaseTx) SyntacticVerify(ctx *snow.Context, c codec.Codec, _ int) error {
	switch {
	case t == nil:
		return errNilTx
	case t.NetID != ctx.NetworkID:
		return errWrongNetworkID
	case !t.BCID.Equals(ctx.ChainID):
		return errWrongChainID
	}

	for _, out := range t.Outs {
		if err := out.Verify(); err != nil {
			return err
		}
	}
	if !IsSortedTransferableOutputs(t.Outs, c) {
		return errOutputsNotSorted
	}

	for _, in := range t.Ins {
		if err := in.Verify(); err != nil {
			return err
		}
	}
	if !isSortedAndUniqueTransferableInputs(t.Ins) {
		return errInputsNotSortedUnique
	}

	consumedFunds := map[[32]byte]uint64{}
	for _, in := range t.Ins {
		assetID := in.AssetID()
		amount := in.Input().Amount()

		var err error
		assetIDKey := assetID.Key()
		consumedFunds[assetIDKey], err = math.Add64(consumedFunds[assetIDKey], amount)

		if err != nil {
			return errInputOverflow
		}
	}
	producedFunds := map[[32]byte]uint64{}
	for _, out := range t.Outs {
		assetID := out.AssetID()
		amount := out.Output().Amount()

		var err error
		assetIDKey := assetID.Key()
		producedFunds[assetIDKey], err = math.Add64(producedFunds[assetIDKey], amount)

		if err != nil {
			return errOutputOverflow
		}
	}

	// TODO: Add the Tx fee to the producedFunds

	for assetID, producedAssetAmount := range producedFunds {
		consumedAssetAmount := consumedFunds[assetID]
		if producedAssetAmount > consumedAssetAmount {
			return errInsufficientFunds
		}
	}

	return t.metadata.Verify()
}

// SemanticVerify that this transaction is valid to be spent.
func (t *BaseTx) SemanticVerify(vm *VM, uTx *UniqueTx, creds []verify.Verifiable) error {
	for i, in := range t.Ins {
		cred := creds[i]

		fxIndex, err := vm.getFx(cred)
		if err != nil {
			return err
		}
		fx := vm.fxs[fxIndex].Fx

		utxoID := in.InputID()
		utxo, err := vm.state.UTXO(utxoID)
		if err == nil {
			utxoAssetID := utxo.AssetID()
			inAssetID := in.AssetID()
			if !utxoAssetID.Equals(inAssetID) {
				return errAssetIDMismatch
			}

			if !vm.verifyFxUsage(fxIndex, inAssetID) {
				return errIncompatibleFx
			}

			err = fx.VerifyTransfer(uTx, utxo.Out, in.In, cred)
			if err == nil {
				continue
			}
			return err
		}

		inputTx, inputIndex := in.InputSource()
		parent := UniqueTx{
			vm:   vm,
			txID: inputTx,
		}

		if err := parent.Verify(); err != nil {
			return errMissingUTXO
		} else if status := parent.Status(); status.Decided() {
			return errMissingUTXO
		}

		utxos := parent.UTXOs()

		if uint32(len(utxos)) <= inputIndex || int(inputIndex) < 0 {
			return errInvalidUTXO
		}

		utxo = utxos[int(inputIndex)]

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
	return nil
}
