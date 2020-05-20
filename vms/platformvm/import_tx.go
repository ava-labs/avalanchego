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
	vm *VM

	// ID of this tx
	id ids.ID

	// Byte representation of the unsigned transaction
	unsignedBytes []byte

	// Byte representation of the signed transaction (ie with Creds and ControlSigs)
	bytes []byte

	// ID of the network this blockchain exists on
	NetworkID uint32 `serialize:"true"`

	// Account that this transaction is being sent by. This is needed to ensure the Credentials are replay safe.
	Account ids.ShortID `serialize:"true"`

	// Input UTXOs
	Ins []*ava.TransferableInput `serialize:"true"`

	// Output UTXOs
	Outs []*ava.TransferableOutput `serialize:"true"`
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

// Key returns the public key of the signer of this transaction
// Precondition: tx.Verify() has been called and returned nil
func (tx *ImportTx) Key() crypto.PublicKey { return tx.key }

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
		if !in.AssetID().Equals(tx.vm.ava) {
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

	unsignedIntf := interface{}(&tx.UnsignedImportTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // byte repr of unsigned tx
	if err != nil {
		return err
	}

	key, err := tx.vm.factory.RecoverPublicKey(unsignedBytes, tx.Sig[:])
	if err != nil {
		return err
	}

	if !tx.Account.Equals(key.Address()) {
		return errPublicKeySignatureMismatch
	}

	tx.key = key
	tx.unsignedBytes = unsignedBytes
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *ImportTx) SemanticVerify(db database.Database) error {
	if err := tx.SyntacticVerify(); err != nil {
		return err
	}

	amount := uint64(0)
	for _, in := range tx.Ins {
		newAmount, err := math.Add64(in.In.Amount(), amount)
		if err != nil {
			return err
		}
		amount = newAmount
	}

	// Deduct tx fee from payer's account
	account, err := tx.vm.getAccount(db, tx.Key().Address())
	if err != nil {
		return err
	}
	account, err = account.Add(amount)
	if err != nil {
		return err
	}
	account, err = account.Remove(0, tx.Nonce)
	if err != nil {
		return err
	}
	if err := tx.vm.putAccount(db, account); err != nil {
		return err
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
		}

		if err := tx.vm.fx.VerifyTransfer(tx, in.In, cred, utxo.Out); err != nil {
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
