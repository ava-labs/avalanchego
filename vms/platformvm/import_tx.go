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
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
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
	vm *VM

	// Metadata, inputs and outputs
	CommonTx `serialize:"true"`

	// Account that this transaction is being sent by. This is needed to ensure the Credentials are replay safe.
	Account ids.ShortID `serialize:"true"`
}

// UnsignedBytes returns the byte representation of this unsigned tx
func (tx *UnsignedImportTx) UnsignedBytes() []byte {
	return tx.unsignedBytes
}

// ImportTx imports funds from the AVM
type ImportTx struct {
	UnsignedImportTx `serialize:"true"`

	// Credentials that authorize the inputs to spend the corresponding outputs
	Creds []verify.Verifiable `serialize:"true"`
}

func (tx *ImportTx) initialize(vm *VM) error {
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

// ID of this transaction
func (tx *ImportTx) ID() ids.ID { return tx.id }

// UnsignedBytes returns the unsigned byte representation of an ImportTx
func (tx *ImportTx) UnsignedBytes() []byte { return tx.unsignedBytes }

// Bytes returns the byte representation of an ImportTx
func (tx *ImportTx) Bytes() []byte { return tx.bytes }

// InputUTXOs returns an empty set
func (tx *ImportTx) InputUTXOs() ids.Set {
	set := ids.Set{}
	for _, in := range tx.Ins {
		set.Add(in.InputID())
	}
	return set
}

// SyntacticVerify this transaction is well-formed
// TODO only syntacticVerify once
func (tx *ImportTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.NetworkID != tx.vm.Ctx.NetworkID: // verify the transaction is on this network
		return errWrongNetworkID
	case tx.id.IsZero():
		return errInvalidID
	case len(tx.Ins) == 0:
		return errNoImportInputs
	case len(tx.Ins) != len(tx.Creds):
		return errWrongNumberOfCredentials
	}

	for _, in := range tx.Ins {
		if err := in.Verify(); err != nil {
			return err
		}
		if !in.AssetID().Equals(tx.vm.avaxAssetID) {
			return errUnknownAsset
		}
	}
	if !ava.IsSortedAndUniqueTransferableInputs(tx.Ins) {
		return errInputsNotSortedUnique
	}

	for _, cred := range tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	return nil
}

// SemanticVerify this transaction is valid.
// TODO make sure the ins and outs are semantically valid
func (tx *ImportTx) SemanticVerify(db database.Database) error {
	if err := tx.SyntacticVerify(); err != nil {
		return err
	}

	// Update the UTXO set
	for _, in := range tx.Ins {
		utxoID := in.InputID() // ID of the UTXO that [in] spends
		if err := tx.vm.removeUTXO(db, utxoID); err != nil {
			return tempError{fmt.Errorf("couldn't remove UTXO %s from UTXO set: %w", utxoID, err)}
		}
	}
	for _, out := range tx.Outs {
		if err := tx.vm.putUTXO(db, tx.ID(), out); err != nil {
			return tempError{fmt.Errorf("couldn't add UTXO to UTXO set: %w", err)}
		}
	}

	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(tx.vm.avm)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)

	state := ava.NewPrefixedState(smDB, Codec)

	for i, in := range tx.Ins {
		cred := tx.Creds[i]

		utxoID := in.UTXOID.InputID()
		utxo, err := state.AVMUTXO(utxoID)
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
	return nil
}

// Accept this transaction.
func (tx *ImportTx) Accept(batch database.Batch) error {
	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(tx.vm.avm)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)

	vsmDB := versiondb.New(smDB)

	state := ava.NewPrefixedState(vsmDB, Codec)
	for _, in := range tx.Ins {
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

/* TODO implement
func (vm *VM) newImportTx(nonce uint64, networkID uint32, ins []*ava.TransferableInput, from [][]*crypto.PrivateKeySECP256K1R, to *crypto.PrivateKeySECP256K1R) (*ImportTx, error) {
	ava.SortTransferableInputsWithSigners(ins, from)

	tx := &ImportTx{UnsignedImportTx: UnsignedImportTx{
		NetworkID: networkID,
		Nonce:     nonce,
		Account:   to.PublicKey().Address(),
		Ins:       ins,
	}}

	unsignedIntf := interface{}(&tx.UnsignedImportTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // Byte repr. of unsigned transaction
	if err != nil {
		return nil, err
	}

	hash := hashing.ComputeHash256(unsignedBytes)

	for _, credKeys := range from {
		cred := &secp256k1fx.Credential{}
		for _, key := range credKeys {
			sig, err := key.SignHash(hash)
			if err != nil {
				return nil, fmt.Errorf("problem creating transaction: %w", err)
			}
			fixedSig := [crypto.SECP256K1RSigLen]byte{}
			copy(fixedSig[:], sig)

			cred.Sigs = append(cred.Sigs, fixedSig)
		}
		tx.Creds = append(tx.Creds, cred)
	}

	sig, err := to.SignHash(hash)
	if err != nil {
		return nil, err
	}
	copy(tx.Sig[:], sig)

	return tx, tx.initialize(vm)
}
*/
