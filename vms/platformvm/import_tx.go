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
	errNoImportInputs             = errors.New("no import inputs")
	errInputsNotSortedUnique      = errors.New("inputs not sorted and unique")
	errPublicKeySignatureMismatch = errors.New("signature doesn't match public key")
	errUnknownAsset               = errors.New("unknown asset ID")
)

// UnsignedImportTx is an unsigned ImportTx
type UnsignedImportTx struct {
	// Metadata, inputs and outputs
	// The inputs in CommonTx all consume non-imported UTXOs
	CommonTx `serialize:"true"`
	// Inputs that consume UTXOs produced on the X-Chain
	ImportedInputs []*ava.TransferableInput `serialize:"true"`
}

// ImportTx imports funds from the AVM
type ImportTx struct {
	UnsignedImportTx `serialize:"true"`
	// Credentials that authorize the inputs to spend the corresponding outputs
	Credentials []verify.Verifiable `serialize:"true"`
}

// Ins returns this transaction's inputs
func (tx *ImportTx) Ins() []*ava.TransferableInput {
	ins := tx.CommonTx.Ins()
	ins = append(ins, tx.ImportedInputs...)
	return ins
}

// Creds returns this transactions credentials
func (tx *ImportTx) Creds() []verify.Verifiable {
	return tx.Credentials
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
	case len(tx.Inputs) == 0:
		return errNoImportInputs
	case len(tx.Inputs) != len(tx.Credentials):
		return errWrongNumberOfCredentials
	}
	if err := syntacticVerifySpend(tx, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *ImportTx) SemanticVerify(db database.Database) error {
	if err := tx.SyntacticVerify(); err != nil {
		return err
	}

	// Spend ordinary inputs (those not consuming UTXOs from X-Chain)
	for index, in := range tx.CommonTx.Inputs {
		if utxo, err := tx.vm.getUTXO(db, in.UTXOID.InputID()); err != nil {
			return err
		} else if err := tx.vm.fx.VerifyTransfer(tx, in.In, tx.Credentials[index], utxo.Out); err != nil {
			return err
		} else if err := tx.vm.removeUTXO(db, in.UTXOID.InputID()); err != nil {
			return err
		}
	}

	// Verify (but not spend) imported inputs
	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(tx.vm.avm)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)
	state := ava.NewPrefixedState(smDB, Codec)
	numOrdinaryInputs := len(tx.CommonTx.Inputs)
	for index, in := range tx.ImportedInputs {
		cred := tx.Credentials[index+numOrdinaryInputs]
		utxoID := in.UTXOID.InputID()
		utxo, err := state.AVMUTXO(utxoID) // Get the UTXO
		if err != nil {
			return err
		}
		utxoAssetID := utxo.AssetID()
		inAssetID := in.AssetID()
		if !utxoAssetID.Equals(inAssetID) {
			return errAssetIDMismatch
		} else if err := tx.vm.fx.VerifyTransfer(tx, in.In, cred, utxo.Out); err != nil {
			return err
		}
	}

	// Produce outputs
	txID := tx.ID()
	for index, out := range tx.Outs() {
		if err := tx.vm.putUTXO(db, &ava.UTXO{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(index),
			},
			Asset: ava.Asset{ID: tx.vm.avaxAssetID},
			Out:   out,
		}); err != nil {
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
	// Calculate some of the inputs, outputs, and keys used to sign this tx
	// These inputs/outputs pay the tx fee
	ins, outs, credKeys, err := vm.spend(vm.DB, vm.txFee, feeKeys)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the transaction
	tx := &ImportTx{UnsignedImportTx: UnsignedImportTx{
		CommonTx: CommonTx{
			NetworkID: vm.Ctx.NetworkID,
			Inputs:    ins, // These pay the tx fee
			Outputs:   outs,
		},
	}}

	recipientAddr := recipientKey.PublicKey().Address() // Address receiving the imported AVAX
	addrSet := ids.Set{}                                // Addresses referenced in UTXOs imported from X-Chain
	addrSet.Add(ids.NewID(hashing.ComputeHash256Array(recipientAddr.Bytes())))
	utxos, err := vm.GetAtomicUTXOs(addrSet)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}

	// Go through UTXOs imported from X-Chain
	// Find all those spendable with [recipientKey]
	// These will be spent, and their funds transferred to this chain
	kc := secp256k1fx.NewKeychain()
	kc.Add(recipientKey)
	amount := uint64(0)
	now := vm.clock.Unix()
	importedIns := []*ava.TransferableInput{}
	importedInsSigners := [][]*crypto.PrivateKeySECP256K1R{}
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
		amount, err = math.Add64(amount, input.Amount())
		if err != nil {
			return nil, err
		}
		importedIns = append(importedIns, &ava.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  ava.Asset{ID: vm.avaxAssetID},
			In:     input,
		})
		importedInsSigners = append(importedInsSigners, signers)
	}
	if amount == 0 {
		return nil, errNoFunds // No imported UTXOs were spendable
	}
	ava.SortTransferableInputsWithSigners(importedIns, importedInsSigners)
	tx.ImportedInputs = importedIns

	outs = append(outs, &ava.TransferableOutput{
		Asset: ava.Asset{ID: vm.avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:      amount, // All the value from imported UTXOs
			Locktime: 0,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{recipientAddr},
			},
		},
	})
	ava.SortTransferableOutputs(outs, vm.codec) //sort outputs

	// Generate byte repr. of unsigned transaction
	if tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedImportTx)); err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedImportTx: %w", err)
	}
	hash := hashing.ComputeHash256(tx.unsignedBytes)

	// First, append all the credentials used to spend non-imported inputs
	for _, inputKeys := range credKeys {
		cred := &secp256k1fx.Credential{}
		for _, key := range inputKeys {
			sig, err := key.SignHash(hash)
			if err != nil {
				return nil, fmt.Errorf("problem creating transaction: %w", err)
			}
			sigArr := [crypto.SECP256K1RSigLen]byte{}
			copy(sigArr[:], sig)
			cred.Sigs = append(cred.Sigs, sigArr)
		}
		tx.Credentials = append(tx.Credentials, cred)
	}
	// Then, append all the credentials used to spend imported inputs
	for _, inputKeys := range importedInsSigners {
		cred := &secp256k1fx.Credential{}
		for _, key := range inputKeys {
			sig, err := key.SignHash(hash)
			if err != nil {
				return nil, fmt.Errorf("problem creating transaction: %w", err)
			}
			sigArr := [crypto.SECP256K1RSigLen]byte{}
			copy(sigArr[:], sig)
			cred.Sigs = append(cred.Sigs, sigArr)
		}
		tx.Credentials = append(tx.Credentials, cred)
	}
	return tx, tx.initialize(vm)
}
