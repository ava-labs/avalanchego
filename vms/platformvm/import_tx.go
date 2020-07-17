// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
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
	// Metadata, inputs and outputs
	// The inputs in BaseTx all consume non-imported UTXOs
	BaseTx `serialize:"true"`
	// Inputs that consume UTXOs produced on the X-Chain
	ImportedInputs []*ava.TransferableInput `serialize:"true"`
}

// ImportTx imports funds from the AVM
type ImportTx struct {
	UnsignedImportTx `serialize:"true"`
	// Credentials that authorize the inputs to spend the corresponding outputs
	// Credentials used to spend unimported inputs followed by credentials used
	// to spend imported inputs
	Creds []verify.Verifiable `serialize:"true"`
}

// Ins returns this transaction's inputs
func (tx *ImportTx) Ins() []*ava.TransferableInput {
	// We copy to a new slice so that
	// when we sort the inputs, we don't modify underlying data atructure
	ins := make([]*ava.TransferableInput, len(tx.BaseTx.Ins), len(tx.BaseTx.Ins)+len(tx.ImportedInputs))
	copy(ins, tx.BaseTx.Ins)
	copy(ins[len(tx.BaseTx.Ins):], tx.ImportedInputs)
	// Sort since syntactic verify expects sorted inputs
	ava.SortTransferableInputs(ins)
	return ins
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *ImportTx) initialize(vm *VM) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedImportTx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedImportTx: %w", err)
	}
	tx.bytes, err = Codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal ImportTx: %w", err)
	}
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return err
}

// InputUTXOs returns an empty set
func (tx *ImportTx) InputUTXOs() ids.Set {
	set := ids.Set{}
	for _, in := range tx.ImportedInputs {
		set.Add(in.InputID())
	}
	return set
}

// SyntacticVerify this transaction is well-formed
func (tx *ImportTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.NetworkID != tx.vm.Ctx.NetworkID:
		return errWrongNetworkID
	case tx.id.IsZero():
		return errInvalidID
	case len(tx.Ins()) == 0:
		return errNoInputs
	case len(tx.ImportedInputs) == 0:
		return errNoImportInputs
	case len(tx.Ins()) != len(tx.Creds):
		return errWrongNumberOfCredentials
	}
	if err := tx.BaseTx.SyntacticVerify(); err != nil {
		return err
	} else if err := syntacticVerifySpend(tx.Ins(), tx.Outs,
		tx.Creds, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *ImportTx) SemanticVerify(db database.Database) error {
	if err := tx.SyntacticVerify(); err != nil {
		return err
	} else if err := tx.vm.semanticVerifySpend(db, tx, tx.BaseTx.Ins, tx.Outs, tx.Creds[:len(tx.BaseTx.Ins)]); err != nil {
		return err // Spend unimported inputs; generate outputs
	}

	// Verify (but not spend) imported inputs
	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(tx.vm.avm)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)
	state := ava.NewPrefixedState(smDB, Codec)
	numOrdinaryInputs := len(tx.BaseTx.Ins)
	for index, in := range tx.ImportedInputs {
		cred := tx.Creds[index+numOrdinaryInputs]
		utxoID := in.UTXOID.InputID()
		if utxo, err := state.AVMUTXO(utxoID); err != nil { // Get the UTXO
			return err
		} else if !utxo.AssetID().Equals(in.AssetID()) {
			return errAssetIDMismatch
		} else if err := tx.vm.fx.VerifyTransfer(tx, in.In, cred, utxo.Out); err != nil {
			return err
		}
	}

	return nil
}

// Accept this transaction and spend imported inputs
// We spend imported UTXOs here rather than in semanticVerify because
// we don't want to remove an imported UTXO in semanticVerify
// only to have the transaction not be Accepted. This would be inconsistent.
// Recall that imported UTXOs are not kept in a versionDB.
func (tx *ImportTx) Accept(batch database.Batch) error {
	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(tx.vm.avm)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)
	vsmDB := versiondb.New(smDB)
	state := ava.NewPrefixedState(vsmDB, Codec)

	// Spend imported UTXOs
	for _, in := range tx.ImportedInputs {
		utxoID := in.UTXOID.InputID()
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

// Create a new transaction
func (vm *VM) newImportTx(
	feeKeys []*crypto.PrivateKeySECP256K1R, // Pay the fee
	recipientKey *crypto.PrivateKeySECP256K1R, // Keys that control the UTXOs being imported
) (*ImportTx, error) {
	if recipientKey == nil {
		return nil, errors.New("recipient key not provided")
	}

	recipientAddr := recipientKey.PublicKey().Address() // Address receiving the imported AVAX
	addrSet := ids.Set{}                                // Addresses referenced in UTXOs imported from X-Chain
	addrSet.Add(ids.NewID(hashing.ComputeHash256Array(recipientAddr.Bytes())))
	utxos, err := vm.GetAtomicUTXOs(addrSet)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}

	// Create the transaction
	tx := &ImportTx{UnsignedImportTx: UnsignedImportTx{
		BaseTx: BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
		},
	}}

	// Go through UTXOs imported from X-Chain
	// Find all those spendable with [recipientKey]
	// These will be spent, and their funds transferred to this chain
	kc := secp256k1fx.NewKeychain()
	kc.Add(recipientKey)
	importedAmount := uint64(0)
	now := vm.clock.Unix()
	importedInsKeys := [][]*crypto.PrivateKeySECP256K1R{}
	for _, utxo := range utxos {
		if !utxo.AssetID().Equals(vm.avaxAssetID) {
			continue
		}
		inputIntf, signers, err := kc.Spend(utxo.Out, now)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(ava.Transferable)
		if !ok {
			continue
		}
		importedAmount, err = math.Add64(importedAmount, input.Amount())
		if err != nil {
			return nil, err
		}
		tx.ImportedInputs = append(tx.ImportedInputs, &ava.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  ava.Asset{ID: vm.avaxAssetID},
			In:     input,
		})
		importedInsKeys = append(importedInsKeys, signers)
	}
	ava.SortTransferableInputsWithSigners(tx.ImportedInputs, importedInsKeys)
	if importedAmount == 0 {
		return nil, errNoFunds // No imported UTXOs were spendable
	}

	var unimportedInsKeys [][]*crypto.PrivateKeySECP256K1R
	if importedAmount < vm.txFee { // imported amount goes toward paying tx fee; the rest is covered by [feeKeys]
		if len(feeKeys) == 0 {
			return nil, fmt.Errorf("tx fee is %d but only have %d", vm.txFee, importedAmount)
		}
		changeSpend := &spend{
			Threshold: 1,
			Locktime:  0,
			Addrs:     []ids.ShortID{feeKeys[0].PublicKey().Address()},
		}
		tx.BaseTx.Ins, tx.Outs, unimportedInsKeys, err = vm.spend(vm.DB, feeKeys, nil, changeSpend, vm.txFee-importedAmount)
		if err != nil {
			return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
		}
		ava.SortTransferableInputsWithSigners(tx.BaseTx.Ins, unimportedInsKeys)
	} else { // The imported amount pays the entire tx fee
		tx.Outs = append(tx.Outs, &ava.TransferableOutput{
			Asset: ava.Asset{ID: vm.avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: importedAmount - vm.txFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{recipientAddr},
				},
			},
		})
	}
	ava.SortTransferableOutputs(tx.Outs, vm.codec) //sort outputs

	// Generate byte repr. of unsigned transaction
	if tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedImportTx)); err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedImportTx: %w", err)
	}
	hash := hashing.ComputeHash256(tx.unsignedBytes)

	// First, append all the credentials used to spend non-imported inputs
	allKeys := append(unimportedInsKeys, importedInsKeys...)
	tx.Creds = make([]verify.Verifiable, len(allKeys))
	for i, credKeys := range allKeys {
		cred := &secp256k1fx.Credential{Sigs: make([][crypto.SECP256K1RSigLen]byte, len(credKeys))}
		for j, key := range credKeys {
			sig, err := key.SignHash(hash)
			if err != nil {
				return nil, fmt.Errorf("problem creating transaction: %w", err)
			}
			copy(cred.Sigs[j][:], sig)
		}
		tx.Creds[i] = cred
	}
	return tx, tx.initialize(vm)
}
