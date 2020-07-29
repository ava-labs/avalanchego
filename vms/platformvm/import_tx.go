// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var (
	errAssetIDMismatch            = errors.New("asset IDs in the input don't match the utxo")
	errWrongNumberOfCredentials   = errors.New("should have the same number of credentials as inputs")
	errNoInputs                   = errors.New("tx has no inputs")
	errNoImportInputs             = errors.New("tx has no imported inputs")
	errInputsNotSortedUnique      = errors.New("inputs not sorted and unique")
	errPublicKeySignatureMismatch = errors.New("signature doesn't match public key")
	errUnknownAsset               = errors.New("unknown asset ID")
)

// UnsignedImportTx is an unsigned ImportTx
type UnsignedImportTx struct {
	BaseTx `serialize:"true"`
	// Inputs that consume UTXOs produced on the X-Chain
	ImportedInputs []*ava.TransferableInput `serialize:"true"`
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *UnsignedImportTx) initialize(vm *VM, bytes []byte) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	tx.bytes = bytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(bytes))
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedImportTx: %w", err)
	}
	return nil
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
func (tx *UnsignedImportTx) Verify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case len(tx.ImportedInputs) == 0:
		return errNoImportInputs
	}

	if err := tx.BaseTx.Verify(); err != nil {
		return err
	}

	for _, in := range tx.ImportedInputs {
		if err := in.Verify(); err != nil {
			return err
		}
	}
	if !ava.IsSortedAndUniqueTransferableInputs(tx.ImportedInputs) {
		return errInputsNotSortedUnique
	}

	allIns := make([]*ava.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
	copy(allIns, tx.Ins)
	copy(allIns[len(tx.Ins):], tx.ImportedInputs)
	if err := syntacticVerifySpend(allIns, tx.Outs, tx.vm.txFee, 0, tx.vm.avaxAssetID); err != nil {
		return err
	}

	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedImportTx) SemanticVerify(db database.Database, creds []verify.Verifiable) TxError {
	if err := tx.Verify(); err != nil {
		return permError{err}
	}

	baseTxCredsLen := len(tx.BaseTx.Ins)
	baseTxCreds := creds[:baseTxCredsLen]
	importCreds := creds[baseTxCredsLen:]

	// Spend un-imported inputs; generate outputs
	if err := tx.vm.semanticVerifySpend(db, tx, tx.BaseTx.Ins, tx.Outs, baseTxCreds); err != nil {
		return err
	}

	// Verify (but don't spend) imported inputs
	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(tx.vm.avm)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)

	state := ava.NewPrefixedState(smDB, Codec)
	for i, in := range tx.ImportedInputs {
		cred := importCreds[i]
		utxoID := in.UTXOID.InputID()
		if utxo, err := state.AVMUTXO(utxoID); err != nil { // Get the UTXO
			return tempError{err}
		} else if !utxo.AssetID().Equals(in.AssetID()) {
			return permError{errAssetIDMismatch}
		} else if err := tx.vm.fx.VerifyTransfer(tx, in.In, cred, utxo.Out); err != nil {
			return permError{err}
		}
	}

	return nil
}

// Accept this transaction and spend imported inputs
// We spend imported UTXOs here rather than in semanticVerify because
// we don't want to remove an imported UTXO in semanticVerify
// only to have the transaction not be Accepted. This would be inconsistent.
// Recall that imported UTXOs are not kept in a versionDB.
func (tx *UnsignedImportTx) Accept(batch database.Batch) error {
	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(tx.vm.avm)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)

	vsmDB := versiondb.New(smDB)
	state := ava.NewPrefixedState(vsmDB, Codec)

	// Spend imported UTXOs
	for _, in := range tx.ImportedInputs {
		utxoID := in.InputID()
		if err := state.SpendAVMUTXO(utxoID); err != nil {
			return err
		}
	}

	sharedBatch, err := vsmDB.CommitBatch()
	if err != nil {
		return err
	}
	return atomic.WriteAll(batch, sharedBatch)
}

// MarshalJSON marshals [tx] to JSON
func (tx *UnsignedImportTx) MarshalJSON() ([]byte, error) {
	// Marshal the base tx
	baseTxJSON, err := json.Marshal(tx.BaseTx)
	if err != nil {
		return nil, err
	}
	importedInsJSON, err := json.Marshal(tx.ImportedInputs)
	if err != nil {
		return nil, err
	}
	buffer := bytes.NewBufferString("{")
	buffer.WriteString(fmt.Sprintf("\"baseTx\":%s,", string(baseTxJSON)))
	buffer.WriteString(fmt.Sprintf("\"importedInputs\":%s", string(importedInsJSON)))
	buffer.WriteString("}")
	return buffer.Bytes(), nil
}

// Create a new transaction
func (vm *VM) newImportTx(
	to ids.ShortID, // Address of recipient
	keys []*crypto.PrivateKeySECP256K1R, // Keys to import the funds
) (*AtomicTx, error) {
	kc := secp256k1fx.NewKeychain()
	for _, key := range keys {
		kc.Add(key)
	}

	addrSet := ids.Set{}
	for _, addr := range kc.Addresses().List() {
		addrSet.Add(ids.NewID(hashing.ComputeHash256Array(addr.Bytes())))
	}
	atomicUTXOs, err := vm.GetAtomicUTXOs(addrSet)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}

	importedInputs := []*ava.TransferableInput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}

	importedAmount := uint64(0)
	now := vm.clock.Unix()
	for _, utxo := range atomicUTXOs {
		if !utxo.AssetID().Equals(vm.avaxAssetID) {
			continue
		}
		inputIntf, utxoSigners, err := kc.Spend(utxo.Out, now)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(ava.TransferableIn)
		if !ok {
			continue
		}
		importedAmount, err = math.Add64(importedAmount, input.Amount())
		if err != nil {
			return nil, err
		}
		importedInputs = append(importedInputs, &ava.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In:     input,
		})
		signers = append(signers, utxoSigners)
	}
	ava.SortTransferableInputsWithSigners(importedInputs, signers)

	if importedAmount == 0 {
		return nil, errNoFunds // No imported UTXOs were spendable
	}

	ins := []*ava.TransferableInput{}
	outs := []*ava.TransferableOutput{}
	if importedAmount < vm.txFee { // imported amount goes toward paying tx fee
		var baseSigners [][]*crypto.PrivateKeySECP256K1R
		ins, outs, baseSigners, err = vm.burn(vm.DB, keys, vm.txFee-importedAmount, 0)
		if err != nil {
			return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
		}
		signers = append(baseSigners, signers...)
	} else if importedAmount > vm.txFee {
		outs = append(outs, &ava.TransferableOutput{
			Asset: ava.Asset{ID: vm.avaxAssetID},
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
		BaseTx: BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		},
		ImportedInputs: importedInputs,
	}
	tx := &AtomicTx{UnsignedAtomicTx: utx}
	if err := vm.signAtomicTx(tx, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify()
}
