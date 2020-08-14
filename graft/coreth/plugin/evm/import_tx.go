// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"crypto/ecdsa"
	"errors"
	"fmt"

	//"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database"
	//"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	avacrypto "github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
	"github.com/ava-labs/go-ethereum/common"
	"github.com/ava-labs/go-ethereum/crypto"
)

var (
	errAssetIDMismatch            = errors.New("asset IDs in the input don't match the utxo")
	errWrongNumberOfCredentials   = errors.New("should have the same number of credentials as inputs")
	errNoInputs                   = errors.New("tx has no inputs")
	errNoImportInputs             = errors.New("tx has no imported inputs")
	errInputsNotSortedUnique      = errors.New("inputs not sorted and unique")
	errPublicKeySignatureMismatch = errors.New("signature doesn't match public key")
	errUnknownAsset               = errors.New("unknown asset ID")
	errNoFunds                    = errors.New("no spendable funds were found")
)

// UnsignedImportTx is an unsigned ImportTx
type UnsignedImportTx struct {
	BaseTx `serialize:"true"`
	// Inputs that consume UTXOs produced on the X-Chain
	ImportedInputs []*ava.TransferableInput `serialize:"true" json:"importedInputs"`
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

var (
	errInputOverflow  = errors.New("inputs overflowed uint64")
	errOutputOverflow = errors.New("outputs overflowed uint64")
)

// Verify that:
// * inputs are all AVAX
// * sum(inputs{unlocked}) >= sum(outputs{unlocked}) + burnAmount{unlocked}
func syntacticVerifySpend(
	ins []*ava.TransferableInput,
	unlockedOuts []EVMOutput,
	burnedUnlocked uint64,
	avaxAssetID ids.ID,
) error {
	// AVAX consumed in this tx
	consumedUnlocked := uint64(0)
	for _, in := range ins {
		if assetID := in.AssetID(); !assetID.Equals(avaxAssetID) { // all inputs must be AVAX
			return fmt.Errorf("input has unexpected asset ID %s expected %s", assetID, avaxAssetID)
		}

		in := in.Input()
		consumed := in.Amount()
		newConsumed, err := math.Add64(consumedUnlocked, consumed)
		if err != nil {
			return errInputOverflow
		}
		consumedUnlocked = newConsumed
	}

	// AVAX produced in this tx
	producedUnlocked := burnedUnlocked
	for _, out := range unlockedOuts {
		produced := out.Amount
		newProduced, err := math.Add64(producedUnlocked, produced)
		if err != nil {
			return errOutputOverflow
		}
		producedUnlocked = newProduced
	}

	if producedUnlocked > consumedUnlocked {
		return fmt.Errorf("tx unlocked outputs (%d) + burn amount (%d) > inputs (%d)",
			producedUnlocked-burnedUnlocked, burnedUnlocked, consumedUnlocked)
	}
	return nil
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
	if err := syntacticVerifySpend(allIns, tx.Outs, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
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

	//// Verify (but don't spend) imported inputs
	//smDB := tx.vm.ctx.SharedMemory.GetDatabase(tx.vm.avm)
	//defer tx.vm.ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)

	//utxos := make([]*ava.UTXO, len(tx.Ins)+len(tx.ImportedInputs))
	//for index, input := range tx.Ins {
	//	utxoID := input.UTXOID.InputID()
	//	utxo, err := tx.vm.getUTXO(db, utxoID)
	//	if err != nil {
	//		return tempError{err}
	//	}
	//	utxos[index] = utxo
	//}

	//state := ava.NewPrefixedState(smDB, Codec)
	//for index, input := range tx.ImportedInputs {
	//	utxoID := input.UTXOID.InputID()
	//	utxo, err := state.AVMUTXO(utxoID)
	//	if err != nil { // Get the UTXO
	//		return tempError{err}
	//	}
	//	utxos[index+len(tx.Ins)] = utxo
	//}

	//ins := make([]*ava.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
	//copy(ins, tx.Ins)
	//copy(ins[len(tx.Ins):], tx.ImportedInputs)

	//// Verify the flowcheck
	//if err := tx.vm.semanticVerifySpendUTXOs(tx, utxos, ins, tx.Outs, creds); err != nil {
	//	return err
	//}

	//txID := tx.ID()

	//// Consume the UTXOS
	//if err := tx.vm.consumeInputs(db, tx.Ins); err != nil {
	//	return tempError{err}
	//}
	//// Produce the UTXOS
	//if err := tx.vm.produceOutputs(db, txID, tx.Outs); err != nil {
	//	return tempError{err}
	//}

	return nil
}

// Accept this transaction and spend imported inputs
// We spend imported UTXOs here rather than in semanticVerify because
// we don't want to remove an imported UTXO in semanticVerify
// only to have the transaction not be Accepted. This would be inconsistent.
// Recall that imported UTXOs are not kept in a versionDB.
func (tx *UnsignedImportTx) Accept(batch database.Batch) error {
	//smDB := tx.vm.ctx.SharedMemory.GetDatabase(tx.vm.avm)
	//defer tx.vm.ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)

	//vsmDB := versiondb.New(smDB)
	//state := ava.NewPrefixedState(vsmDB, Codec)

	//// Spend imported UTXOs
	//for _, in := range tx.ImportedInputs {
	//	utxoID := in.InputID()
	//	if err := state.SpendAVMUTXO(utxoID); err != nil {
	//		return err
	//	}
	//}

	//sharedBatch, err := vsmDB.CommitBatch()
	//if err != nil {
	//	return err
	//}
	//return atomic.WriteAll(batch, sharedBatch)
	return nil
}

// Create a new transaction
func (vm *VM) newImportTx(
	to common.Address, // Address of recipient
	keys []*ecdsa.PrivateKey, // Keys to import the funds
) (*AtomicTx, error) {
	kc := secp256k1fx.NewKeychain()
	factory := &avacrypto.FactorySECP256K1R{}
	for _, key := range keys {
		sk, err := factory.ToPrivateKey(crypto.FromECDSA(key))
		if err != nil {
			panic(err)
		}
		kc.Add(sk.(*avacrypto.PrivateKeySECP256K1R))
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
	signers := [][]*avacrypto.PrivateKeySECP256K1R{}

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
	outs := []EVMOutput{}
	if importedAmount < vm.txFee { // imported amount goes toward paying tx fee
		//var baseSigners [][]*avacrypto.PrivateKeySECP256K1R
		//ins, outs, _, baseSigners, err = vm.spend(vm.DB, keys, 0, vm.txFee-importedAmount)
		//if err != nil {
		//	return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
		//}
		//signers = append(baseSigners, signers...)
	} else if importedAmount > vm.txFee {
		outs = append(outs, EVMOutput{
			Address: to,
			Amount:  importedAmount - vm.txFee})
	}

	// Create the transaction
	utx := &UnsignedImportTx{
		BaseTx: BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
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
