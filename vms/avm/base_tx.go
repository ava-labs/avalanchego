// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/components/verify"
)

// Max size of memo field
// Don't change without also changing platformvm.maxMemoSize
const maxMemoSize = 256

var (
	errNilTx                 = errors.New("nil tx is not valid")
	errWrongNetworkID        = errors.New("tx has wrong network ID")
	errWrongChainID          = errors.New("tx has wrong chain ID")
	errOutputsNotSorted      = errors.New("outputs not sorted")
	errInputsNotSortedUnique = errors.New("inputs not sorted and unique")
	errInputOverflow         = errors.New("inputs overflowed uint64")
	errOutputOverflow        = errors.New("outputs overflowed uint64")
	errInsufficientFunds     = errors.New("insufficient funds")
)

// BaseTx is the basis of all transactions.
// The serialized fields of this struct should be exactly the same as those of platformvm.BaseTx
// Do not change this struct's serialized fields without doing the same on platformvm.BaseTx
// TODO: Factor out this and platformvm.BaseTx
type BaseTx struct {
	avax.Metadata

	NetID uint32                    `serialize:"true" json:"networkID"`    // ID of the network this chain lives on
	BCID  ids.ID                    `serialize:"true" json:"blockchainID"` // ID of the chain on which this transaction exists (prevents replay attacks)
	Outs  []*avax.TransferableOutput `serialize:"true" json:"outputs"`      // The outputs of this transaction
	Ins   []*avax.TransferableInput  `serialize:"true" json:"inputs"`       // The inputs to this transaction
	Memo  []byte                    `serialize:"true" json:"memo"`         // Memo field contains arbitrary bytes, up to maxMemoSize
}

// InputUTXOs track which UTXOs this transaction is consuming.
func (t *BaseTx) InputUTXOs() []*avax.UTXOID {
	utxos := []*avax.UTXOID(nil)
	for _, in := range t.Ins {
		utxos = append(utxos, &in.UTXOID)
	}
	return utxos
}

// ConsumedAssetIDs returns the IDs of the assets this transaction consumes
func (t *BaseTx) ConsumedAssetIDs() ids.Set {
	assets := ids.Set{}
	for _, in := range t.Ins {
		assets.Add(in.AssetID())
	}
	return assets
}

// AssetIDs returns the IDs of the assets this transaction depends on
func (t *BaseTx) AssetIDs() ids.Set {
	return t.ConsumedAssetIDs()
}

// NumCredentials returns the number of expected credentials
func (t *BaseTx) NumCredentials() int { return len(t.Ins) }

// UTXOs returns the UTXOs transaction is producing.
func (t *BaseTx) UTXOs() []*avax.UTXO {
	txID := t.ID()
	utxos := make([]*avax.UTXO, len(t.Outs))
	for i, out := range t.Outs {
		utxos[i] = &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(i),
			},
			Asset: avax.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}
	}
	return utxos
}

// SyntacticVerify that this transaction is well-formed.
func (t *BaseTx) SyntacticVerify(
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
	}

	fc := avax.NewFlowChecker()

	// The txFee must be burned
	fc.Produce(txFeeAssetID, txFee)

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

	if err := fc.Verify(); err != nil {
		return err
	}

	return t.Metadata.Verify()
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
	for _, out := range t.Outs {
		fxIndex, err := vm.getFx(out.Out)
		if err != nil {
			return err
		}
		if assetID := out.AssetID(); !vm.verifyFxUsage(fxIndex, assetID) {
			return errIncompatibleFx
		}
	}
	return nil
}

// ExecuteWithSideEffects writes the batch with any additional side effects
func (t *BaseTx) ExecuteWithSideEffects(_ *VM, batch database.Batch) error { return batch.Write() }
