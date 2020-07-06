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

	// Metadata, inputs and outputs
	CommonTx `serialize:"true"`
}

// ImportTx imports funds from the AVM
type ImportTx struct {
	UnsignedImportTx `serialize:"true"`

	// Credentials that authorize the inputs to spend the corresponding outputs
	Credentials []verify.Verifiable `serialize:"true"`
}

// Creds returns this transactions credentials
func (tx *ImportTx) Creds() []verify.Verifiable {
	return tx.Credentials
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

// InputUTXOs returns an empty set
func (tx *ImportTx) InputUTXOs() ids.Set {
	set := ids.Set{}
	for _, in := range tx.Inputs {
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
	case len(tx.Inputs) == 0:
		return errNoImportInputs
	case len(tx.Inputs) != len(tx.Credentials):
		return errWrongNumberOfCredentials
	}

	for _, in := range tx.Inputs {
		if err := in.Verify(); err != nil {
			return err
		}
		if !in.AssetID().Equals(tx.vm.avaxAssetID) {
			return errUnknownAsset
		}
	}
	if !ava.IsSortedAndUniqueTransferableInputs(tx.Inputs) {
		return errInputsNotSortedUnique
	}

	for _, cred := range tx.Credentials {
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

	// Verify inputs/outputs and update the UTXO set
	if err := tx.vm.semanticVerifySpend(db, tx); err != nil {
		return tempError{fmt.Errorf("couldn't verify tx: %w", err)}
	}

	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(tx.vm.avm)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)

	state := ava.NewPrefixedState(smDB, Codec)

	for i, in := range tx.Inputs {
		cred := tx.Credentials[i]

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
	for _, in := range tx.Inputs {
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

// TODO comment
func (vm *VM) newImportTx(
	networkID uint32,
	feeKeys []*crypto.PrivateKeySECP256K1R,
	recipientKey *crypto.PrivateKeySECP256K1R,
) (*ImportTx, error) {

	// Calculate inputs, outputs, and keys used to sign this tx
	feeInputs, feeOutputs, feeCredKeys, err := vm.spend(vm.DB, vm.txFee, feeKeys)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Address receiving the imported AVAX
	recipientAddr := recipientKey.PublicKey().Address()

	kc := secp256k1fx.NewKeychain()
	kc.Add(recipientKey)

	addrSet := ids.Set{}
	addrSet.Add(ids.NewID(hashing.ComputeHash256Array(recipientAddr.Bytes())))

	utxos, err := vm.GetAtomicUTXOs(addrSet)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving  atomic UTXOs: %w", err)
	}

	amount := uint64(0)
	now := vm.clock.Unix()

	ins := []*ava.TransferableInput{}
	keys := [][]*crypto.PrivateKeySECP256K1R{}
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
		ins = append(ins, &ava.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  ava.Asset{ID: vm.avaxAssetID},
			In:     input,
		})
		keys = append(keys, signers)
	}
	if amount == 0 {
		return nil, errNoFunds
	}
	ins = append(ins, feeInputs...)
	keys = append(keys, feeCredKeys...)
	ava.SortTransferableInputsWithSigners(ins, keys)

	outs := append(feeOutputs, &ava.TransferableOutput{
		Asset: ava.Asset{ID: vm.avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:      amount,
			Locktime: 0,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{recipientAddr},
			},
		},
	})
	ava.SortTransferableOutputs(outs, vm.codec) //sort outputs

	// Create the transaction
	tx := &ImportTx{UnsignedImportTx: UnsignedImportTx{
		CommonTx: CommonTx{
			NetworkID: vm.Ctx.NetworkID,
			Inputs:    ins,
			Outputs:   outs,
		},
	}}

	// Generate byte repr. of unsigned transaction
	if tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedImportTx)); err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedImportTx: %w", err)
	}
	hash := hashing.ComputeHash256(tx.unsignedBytes)

	for _, credKeys := range keys {
		cred := &secp256k1fx.Credential{}
		for _, key := range credKeys {
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
