// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"

	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/math"
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
	// ID of the network this blockchain exists on
	NetworkID uint32 `serialize:"true"`

	// Next unused nonce of account paying the transaction fee for this transaction.
	// Currently unused, as there are no tx fees.
	Nonce uint64 `serialize:"true"`

	// Account that this transaction is being sent by. This is needed to ensure the Credentials are replay safe.
	Account [crypto.SECP256K1RPKLen]byte `serialize:"true"`

	Ins []*ava.TransferableInput `serialize:"true"` // The inputs to this transaction
}

// ImportTx imports funds from the AVM
type ImportTx struct {
	UnsignedImportTx `serialize:"true"`

	Sig   [crypto.SECP256K1RSigLen]byte `serialize:"true"`
	Creds []verify.Verifiable           `serialize:"true"` // The credentials of this transaction

	vm    *VM
	id    ids.ID
	key   crypto.PublicKey // public key of transaction signer
	bytes []byte
}

func (tx *ImportTx) initialize(vm *VM) error {
	tx.vm = vm
	txBytes, err := Codec.Marshal(tx) // byte repr. of the signed tx
	tx.bytes = txBytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(txBytes))
	return err
}

// ID of this transaction
func (tx *ImportTx) ID() ids.ID { return tx.id }

// Key returns the public key of the signer of this transaction
// Precondition: tx.Verify() has been called and returned nil
func (tx *ImportTx) Key() crypto.PublicKey { return tx.key }

// Bytes returns the byte representation of a CreateChainTx
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
// Also populates [tx.Key] with the public key that signed this transaction
func (tx *ImportTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.key != nil:
		return nil // Only verify the transaction once
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
		if !in.AssetID().Equals(tx.vm.AVA) {
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

	expectedPublicKey, err := tx.vm.factory.ToPublicKey(tx.Account[:])
	if err != nil {
		return err
	}

	key, err := tx.vm.factory.RecoverPublicKey(unsignedBytes, tx.Sig[:])
	if err != nil {
		return err
	}

	if !expectedPublicKey.Address().Equals(key.Address()) {
		return errPublicKeySignatureMismatch
	}

	tx.key = key
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *ImportTx) SemanticVerify(db database.Database) error {
	if err := tx.SyntacticVerify(); err != nil {
		return err
	}

	bID := ids.Empty // TODO: Needs to be set to the platform chain
	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(bID)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(bID)

	state := ava.NewPrefixedState(smDB, Codec)

	amount := uint64(0)
	for i, in := range tx.Ins {
		newAmount, err := math.Add64(in.In.Amount(), amount)
		if err != nil {
			return err
		}
		amount = newAmount

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

		if err := tx.vm.fx.VerifyTransfer(uTx, utxo.Out, in.In, cred); err != nil {
			return err
		}
	}

	// Deduct tx fee from payer's account
	account, err := tx.vm.getAccount(db, tx.Key().Address())
	if err != nil {
		return err
	}
	account, err = account.Remove(0, tx.Nonce)
	if err != nil {
		return err
	}
	account, err = account.Add(amount)
	if err != nil {
		return err
	}
	return tx.vm.putAccount(db, account)
}

// Accept this transaction.
func (tx *ImportTx) Accept(batch database.Batch) error {
	bID := ids.Empty // TODO: Needs to be set to the platform chain
	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(bID)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(bID)

	vsmDB := versiondb.New(smDB)

	state := ava.NewPrefixedState(vsmDB, Codec)
	for _, in := range tx.Ins {
		utxoID := in.UTXOID.InputID()
		if _, err := state.AVMUTXO(utxoID); err == nil {
			if err := state.SetAVMUTXO(utxoID, nil); err != nil {
				return err
			}
		} else if err := state.SetAVMStatus(utxoID, choices.Accepted); err != nil {
			return err
		}
	}

	sharedBatch, err := vsmDB.CommitBatch()
	if err != nil {
		return err
	}

	return atomic.WriteAll(batch, sharedBatch)
}

func (vm *VM) newImportTx(nonce uint64, genesisData []byte, vmID ids.ID, fxIDs []ids.ID, chainName string, networkID uint32, key *crypto.PrivateKeySECP256K1R) (*ImportTx, error) {
	tx := &CreateChainTx{
		UnsignedCreateChainTx: UnsignedCreateChainTx{
			NetworkID:   networkID,
			Nonce:       nonce,
			GenesisData: genesisData,
			VMID:        vmID,
			FxIDs:       fxIDs,
			ChainName:   chainName,
		},
	}

	unsignedIntf := interface{}(&tx.UnsignedCreateChainTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // Byte repr. of unsigned transaction
	if err != nil {
		return nil, err
	}

	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		return nil, err
	}
	copy(tx.Sig[:], sig)

	return tx, tx.initialize(vm)
}
