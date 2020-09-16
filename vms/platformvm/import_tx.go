// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/codec"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errAssetIDMismatch          = errors.New("asset IDs in the input don't match the utxo")
	errWrongNumberOfCredentials = errors.New("should have the same number of credentials as inputs")
	errNoImportInputs           = errors.New("tx has no imported inputs")
	errInputsNotSortedUnique    = errors.New("inputs not sorted and unique")

	_ UnsignedAtomicTx = &UnsignedImportTx{}
)

// UnsignedImportTx is an unsigned ImportTx
type UnsignedImportTx struct {
	BaseTx `serialize:"true"`

	// Which chain to consume the funds from
	SourceChain ids.ID `serialize:"true" json:"sourceChain"`

	// Inputs that consume UTXOs produced on the chain
	ImportedInputs []*avax.TransferableInput `serialize:"true" json:"importedInputs"`
}

// InputUTXOs returns the UTXOIDs of the imported funds
func (tx *UnsignedImportTx) InputUTXOs() ids.Set {
	set := ids.Set{}
	for _, in := range tx.ImportedInputs {
		set.Add(in.InputID())
	}
	return set
}

// Verify this transaction is well-formed
func (tx *UnsignedImportTx) Verify(
	avmID ids.ID,
	ctx *snow.Context,
	c codec.Codec,
	feeAmount uint64,
	feeAssetID ids.ID,
) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.SourceChain.IsZero():
		return errWrongChainID
	case !tx.SourceChain.Equals(avmID):
		// TODO: remove this check if we allow for P->C swaps
		return errWrongChainID
	case len(tx.ImportedInputs) == 0:
		return errNoImportInputs
	}

	if err := tx.BaseTx.Verify(ctx, c); err != nil {
		return err
	}

	for _, in := range tx.ImportedInputs {
		if err := in.Verify(); err != nil {
			return err
		}
	}
	if !avax.IsSortedAndUniqueTransferableInputs(tx.ImportedInputs) {
		return errInputsNotSortedUnique
	}

	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedImportTx) SemanticVerify(
	vm *VM,
	db database.Database,
	stx *Tx,
) TxError {
	if err := tx.Verify(vm.Ctx.XChainID, vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID); err != nil {
		return permError{err}
	}

	utxos := make([]*avax.UTXO, len(tx.Ins)+len(tx.ImportedInputs))
	for index, input := range tx.Ins {
		utxoID := input.UTXOID.InputID()
		utxo, err := vm.getUTXO(db, utxoID)
		if err != nil {
			return tempError{err}
		}
		utxos[index] = utxo
	}

	txID := tx.ID()

	// Consume the UTXOS
	if err := vm.consumeInputs(db, tx.Ins); err != nil {
		return tempError{err}
	}
	// Produce the UTXOS
	if err := vm.produceOutputs(db, txID, tx.Outs); err != nil {
		return tempError{err}
	}

	if !vm.bootstrapped {
		return nil
	}

	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoIDs[i] = in.UTXOID.InputID().Bytes()
	}
	allUTXOBytes, err := vm.Ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
	if err != nil {
		return tempError{err}
	}

	for i, utxoBytes := range allUTXOBytes {
		utxo := &avax.UTXO{}
		if err := vm.codec.Unmarshal(utxoBytes, utxo); err != nil {
			return tempError{err}
		}
		utxos[i+len(tx.Ins)] = utxo
	}

	ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
	copy(ins, tx.Ins)
	copy(ins[len(tx.Ins):], tx.ImportedInputs)

	return vm.semanticVerifySpendUTXOs(tx, utxos, ins, tx.Outs, stx.Creds, vm.txFee, vm.Ctx.AVAXAssetID)
}

// Accept this transaction and spend imported inputs
// We spend imported UTXOs here rather than in semanticVerify because
// we don't want to remove an imported UTXO in semanticVerify
// only to have the transaction not be Accepted. This would be inconsistent.
// Recall that imported UTXOs are not kept in a versionDB.
func (tx *UnsignedImportTx) Accept(ctx *snow.Context, batch database.Batch) error {
	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoIDs[i] = in.InputID().Bytes()
	}
	return ctx.SharedMemory.Remove(tx.SourceChain, utxoIDs, batch)
}

// Create a new transaction
func (vm *VM) newImportTx(
	chainID ids.ID, // chain to import from
	to ids.ShortID, // Address of recipient
	keys []*crypto.PrivateKeySECP256K1R, // Keys to import the funds
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*Tx, error) {
	if !vm.Ctx.XChainID.Equals(chainID) {
		return nil, errWrongChainID
	}

	kc := secp256k1fx.NewKeychain()
	for _, key := range keys {
		kc.Add(key)
	}

	atomicUTXOs, _, _, err := vm.GetAtomicUTXOs(chainID, kc.Addresses(), ids.ShortEmpty, ids.Empty, -1)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}

	importedInputs := []*avax.TransferableInput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}

	importedAmount := uint64(0)
	now := vm.clock.Unix()
	for _, utxo := range atomicUTXOs {
		if !utxo.AssetID().Equals(vm.Ctx.AVAXAssetID) {
			continue
		}
		inputIntf, utxoSigners, err := kc.Spend(utxo.Out, now)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			continue
		}
		importedAmount, err = math.Add64(importedAmount, input.Amount())
		if err != nil {
			return nil, err
		}
		importedInputs = append(importedInputs, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In:     input,
		})
		signers = append(signers, utxoSigners)
	}
	avax.SortTransferableInputsWithSigners(importedInputs, signers)

	if importedAmount == 0 {
		return nil, errNoFunds // No imported UTXOs were spendable
	}

	ins := []*avax.TransferableInput{}
	outs := []*avax.TransferableOutput{}
	if importedAmount < vm.txFee { // imported amount goes toward paying tx fee
		var baseSigners [][]*crypto.PrivateKeySECP256K1R
		ins, outs, _, baseSigners, err = vm.stake(vm.DB, keys, 0, vm.txFee-importedAmount, changeAddr)
		if err != nil {
			return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
		}
		signers = append(baseSigners, signers...)
	} else if importedAmount > vm.txFee {
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: importedAmount - vm.txFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		})
	}

	// Create the transaction
	utx := &UnsignedImportTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		SourceChain:    chainID,
		ImportedInputs: importedInputs,
	}
	tx := &Tx{UnsignedTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.Ctx.XChainID, vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID)
}
