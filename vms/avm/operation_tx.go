// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errOperationsNotSortedUnique = errors.New("operations not sorted and unique")
	errNoOperations              = errors.New("an operationTx must have at least one operation")

	errDoubleSpend = errors.New("inputs attempt to double spend an input")
)

// OperationTx is a transaction with no credentials.
type OperationTx struct {
	BaseTx `serialize:"true"`
	Ops    []*Operation `serialize:"true"`
}

// Operations track which ops this transaction is performing. The returned array
// should not be modified.
func (t *OperationTx) Operations() []*Operation { return t.Ops }

// InputUTXOs track which UTXOs this transaction is consuming.
func (t *OperationTx) InputUTXOs() []*UTXOID {
	utxos := t.BaseTx.InputUTXOs()
	for _, op := range t.Ops {
		for _, in := range op.Ins {
			utxos = append(utxos, &in.UTXOID)
		}
	}
	return utxos
}

// AssetIDs returns the IDs of the assets this transaction depends on
func (t *OperationTx) AssetIDs() ids.Set {
	assets := t.BaseTx.AssetIDs()
	for _, op := range t.Ops {
		assets.Add(op.AssetID())
	}
	return assets
}

// UTXOs returns the UTXOs transaction is producing.
func (t *OperationTx) UTXOs() []*UTXO {
	txID := t.ID()
	utxos := t.BaseTx.UTXOs()

	for _, op := range t.Ops {
		asset := op.AssetID()
		for _, out := range op.Outs {
			utxos = append(utxos, &UTXO{
				UTXOID: UTXOID{
					TxID:        txID,
					OutputIndex: uint32(len(utxos)),
				},
				Asset: Asset{ID: asset},
				Out:   out,
			})
		}
	}

	return utxos
}

// SyntacticVerify that this transaction is well-formed.
func (t *OperationTx) SyntacticVerify(ctx *snow.Context, c codec.Codec, numFxs int) error {
	switch {
	case t == nil:
		return errNilTx
	case len(t.Ops) == 0:
		return errNoOperations
	}

	if err := t.BaseTx.SyntacticVerify(ctx, c, numFxs); err != nil {
		return err
	}

	inputs := ids.Set{}
	for _, in := range t.Ins {
		inputs.Add(in.InputID())
	}

	for _, op := range t.Ops {
		if err := op.Verify(c); err != nil {
			return err
		}
		for _, in := range op.Ins {
			inputID := in.InputID()
			if inputs.Contains(inputID) {
				return errDoubleSpend
			}
			inputs.Add(inputID)
		}
	}
	if !isSortedAndUniqueOperations(t.Ops, c) {
		return errOperationsNotSortedUnique
	}
	return nil
}

// SemanticVerify that this transaction is well-formed.
func (t *OperationTx) SemanticVerify(vm *VM, uTx *UniqueTx, creds []verify.Verifiable) error {
	if err := t.BaseTx.SemanticVerify(vm, uTx, creds); err != nil {
		return err
	}
	offset := len(t.BaseTx.Ins)
	for _, op := range t.Ops {
		opAssetID := op.AssetID()

		utxos := []interface{}{}
		ins := []interface{}{}
		credIntfs := []interface{}{}
		outs := []interface{}{}

		for i, in := range op.Ins {
			ins = append(ins, in.In)

			cred := creds[i+offset]
			credIntfs = append(credIntfs, cred)

			utxo, err := vm.getUTXO(&in.UTXOID)
			if err != nil {
				return err
			}

			utxoAssetID := utxo.AssetID()
			if !utxoAssetID.Equals(opAssetID) {
				return errAssetIDMismatch
			}
			utxos = append(utxos, utxo.Out)
		}
		offset += len(op.Ins)
		for _, out := range op.Outs {
			outs = append(outs, out)
		}

		var fxObj interface{}
		switch {
		case len(ins) > 0:
			fxObj = ins[0]
		case len(outs) > 0:
			fxObj = outs[0]
		}

		fxIndex, err := vm.getFx(fxObj)
		if err != nil {
			return err
		}
		fx := vm.fxs[fxIndex].Fx

		if !vm.verifyFxUsage(fxIndex, opAssetID) {
			return errIncompatibleFx
		}

		err = fx.VerifyOperation(uTx, utxos, ins, credIntfs, outs)
		if err != nil {
			return err
		}
	}
	return nil
}
