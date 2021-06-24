// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var errNilTx = errors.New("nil tx is not valid")

// BaseTx is the basis of all transactions.
type BaseTx struct {
	avax.BaseTx `serialize:"true"`
}

// SyntacticVerify that this transaction is well-formed.
func (t *BaseTx) SyntacticVerify(
	ctx *snow.Context,
	c codec.Manager,
	txFeeAssetID ids.ID,
	txFee uint64,
	_ uint64,
	_ int,
) error {
	if t == nil {
		return errNilTx
	}
	if err := t.MetadataVerify(ctx); err != nil {
		return err
	}

	return avax.VerifyTx(
		txFee,
		txFeeAssetID,
		[][]*avax.TransferableInput{t.Ins},
		[][]*avax.TransferableOutput{t.Outs},
		c,
	)
}

// SemanticVerify that this transaction is valid to be spent.
func (t *BaseTx) SemanticVerify(vm *VM, tx UnsignedTx, creds []verify.Verifiable) error {
	for i, in := range t.Ins {
		cred := creds[i]
		if err := vm.verifyTransfer(tx, in, cred); err != nil {
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

	// index output utxos
	for _, utxo := range t.UTXOs() {
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			vm.ctx.Log.Debug("Skipping utxo %s for export indexing because it is not of secp256k1fx.TransferOutput", utxo.InputID().String())
			continue
		}

		IndexTransferOutput(vm, utxo.AssetID(), out)
	}
	return nil
}

// ExecuteWithSideEffects writes the batch with any additional side effects
func (t *BaseTx) ExecuteWithSideEffects(_ *VM, batch database.Batch) error { return batch.Write() }
