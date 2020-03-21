// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

// import (
// 	"bytes"
// 	"errors"
// 	"sort"

// 	"github.com/ava-labs/gecko/database"
// 	"github.com/ava-labs/gecko/ids"
// 	"github.com/ava-labs/gecko/snow"
// 	"github.com/ava-labs/gecko/utils"
// 	"github.com/ava-labs/gecko/vms/components/codec"
// 	"github.com/ava-labs/gecko/vms/components/verify"
// )

// // ImportInput ...
// type ImportInput struct {
// 	UTXOID ids.ID `serialize:"true"`
// 	Asset  `serialize:"true"`

// 	In FxTransferable `serialize:"true"`
// }

// // Input returns the feature extension input that this Input is using.
// func (in *ImportInput) Input() FxTransferable { return in.In }

// // Verify implements the verify.Verifiable interface
// func (in *ImportInput) Verify() error {
// 	switch {
// 	case in == nil:
// 		return errNilTransferableInput
// 	case in.UTXOID.IsZero():
// 		return errNilUTXOID
// 	case in.In == nil:
// 		return errNilTransferableFxInput
// 	default:
// 		return verify.All(&in.Asset, in.In)
// 	}
// }

// type innerSortImportInputs []*ImportInput

// func (ins innerSortImportInputs) Less(i, j int) bool {
// 	return bytes.Compare(ins[i].AssetID().Bytes(), ins[j].AssetID().Bytes()) == -1
// }
// func (ins innerSortImportInputs) Len() int      { return len(ins) }
// func (ins innerSortImportInputs) Swap(i, j int) { ins[j], ins[i] = ins[i], ins[j] }

// func sortImportInputs(ins []*ImportInput) { sort.Sort(innerSortImportInputs(ins)) }
// func isSortedAndUniqueImportInputs(ins []*ImportInput) bool {
// 	return utils.IsSortedAndUnique(innerSortImportInputs(ins))
// }

// // ImportTx is a transaction that imports an asset from another blockchain.
// type ImportTx struct {
// 	BaseTx `serialize:"true"`

// 	Outs []*TransferableOutput `serialize:"true"` // The outputs of this transaction
// 	Ins  []*ImportInput        `serialize:"true"` // The inputs to this transaction
// }

// // InputUTXOs track which UTXOs this transaction is consuming.
// func (t *ImportTx) InputUTXOs() []*UTXOID {
// 	utxos := t.BaseTx.InputUTXOs()
// 	for _, in := range t.Ins {
// 		utxos = append(utxos, &UTXOID{
// 			TxID:     in.UTXOID,
// 			symbolic: true,
// 		})
// 	}
// 	return utxos
// }

// // AssetIDs returns the IDs of the assets this transaction depends on
// func (t *ImportTx) AssetIDs() ids.Set {
// 	assets := t.BaseTx.AssetIDs()
// 	for _, in := range t.Ins {
// 		assets.Add(in.AssetID())
// 	}
// 	return assets
// }

// // UTXOs returns the UTXOs transaction is producing.
// func (t *ImportTx) UTXOs() []*UTXO {
// 	txID := t.ID()
// 	utxos := t.BaseTx.UTXOs()

// 	for _, out := range t.Outs {
// 		utxos = append(utxos, &UTXO{
// 			UTXOID: UTXOID{
// 				TxID:        txID,
// 				OutputIndex: uint32(len(utxos)),
// 			},
// 			Asset: Asset{ID: out.AssetID()},
// 			Out:   out.Out,
// 		})
// 	}

// 	return utxos
// }

// var (
// 	errNoInputs = errors.New("no import inputs")
// )

// // SyntacticVerify that this transaction is well-formed.
// func (t *ImportTx) SyntacticVerify(ctx *snow.Context, c codec.Codec, numFxs int) error {
// 	switch {
// 	case t == nil:
// 		return errNilTx
// 	case len(t.Ins) == 0:
// 		return errNoInputs
// 	}

// 	if err := t.BaseTx.SyntacticVerify(ctx, c, numFxs); err != nil {
// 		return err
// 	}

// 	fc := NewFlowChecker()
// 	for _, out := range t.Outs {
// 		if err := out.Verify(); err != nil {
// 			return err
// 		}
// 		fc.Produce(out.AssetID(), out.Output().Amount())
// 	}
// 	if !IsSortedTransferableOutputs(t.Outs, c) {
// 		return errOutputsNotSorted
// 	}

// 	for _, in := range t.Ins {
// 		if err := in.Verify(); err != nil {
// 			return err
// 		}
// 		fc.Consume(in.AssetID(), in.Input().Amount())
// 	}
// 	if !isSortedAndUniqueImportInputs(t.Ins) {
// 		return errInputsNotSortedUnique
// 	}

// 	// TODO: Add the Tx fee to the produced side

// 	if err := fc.Verify(); err != nil {
// 		return err
// 	}

// 	return nil
// }

// // SemanticVerify that this transaction is well-formed.
// func (t *ImportTx) SemanticVerify(vm *VM, uTx *UniqueTx, creds []verify.Verifiable) error {
// 	if err := t.BaseTx.SemanticVerify(vm, uTx, creds); err != nil {
// 		return err
// 	}
// 	offset := len(t.BaseTx.Ins)
// 	for i, in := range t.Ins {
// 		cred := creds[i+offset]

// 		fxIndex, err := vm.getFx(cred)
// 		if err != nil {
// 			return err
// 		}
// 		fx := vm.fxs[fxIndex].Fx

// 		utxoID := in.UTXOID
// 		utxo, err := vm.state.UTXO(utxoID)
// 		if err != nil {
// 			return err
// 		}
// 		utxoAssetID := utxo.AssetID()
// 		inAssetID := in.AssetID()
// 		if !utxoAssetID.Equals(inAssetID) {
// 			return errAssetIDMismatch
// 		}

// 		if !vm.verifyFxUsage(fxIndex, inAssetID) {
// 			return errIncompatibleFx
// 		}

// 		if err := fx.VerifyTransfer(uTx, utxo.Out, in.In, cred); err != nil {
// 			return err
// 		}
// 	}
// 	return nil
// }

// // SideEffects that this transaction has in addition to the UTXO operations.
// func (t *ImportTx) SideEffects(vm *VM) ([]database.Batch, error) { return nil, nil }
