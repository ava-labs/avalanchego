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
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var (
	errNoExportOutputs  = errors.New("no export outputs")
	errOutputsNotSorted = errors.New("outputs not sorted")
)

// UnsignedExportTx is an unsigned ExportTx
type UnsignedExportTx struct {
	vm *VM

	// Metadata, inputs and outputs
	CommonTx `serialize:"true"`
}

// ExportTx exports funds to the AVM
type ExportTx struct {
	UnsignedExportTx `serialize:"true"`

	// Credentials that authorize the inputs to spend the corresponding outputs
	Credentials []verify.Verifiable `serialize:"true"`
}

// Creds returns this transactions credentials
func (tx *ExportTx) Creds() []verify.Verifiable {
	return tx.Credentials
}

// initialize [tx]
// set tx.vm, tx.unsignedBytes, tx.bytes, tx.id
func (tx *ExportTx) initialize(vm *VM) error {
	tx.vm = vm
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedExportTx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedExportTx: %w", err)
	}
	tx.bytes, err = Codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal ExportTx: %w", err)
	}
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return err
}

// InputUTXOs returns an empty set
func (tx *ExportTx) InputUTXOs() ids.Set { return ids.Set{} }

// SyntacticVerify this transaction is well-formed
// TODO only syntacticVerify once
func (tx *ExportTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.NetworkID != tx.vm.Ctx.NetworkID: // verify the transaction is on this network
		return errWrongNetworkID
	case tx.id.IsZero():
		return errInvalidID
	case len(tx.Outputs) == 0:
		return errNoExportOutputs
	}

	for _, out := range tx.Outputs {
		if err := out.Verify(); err != nil {
			return err
		}
		if !out.AssetID().Equals(tx.vm.avaxAssetID) {
			return errUnknownAsset
		}
	}
	if !ava.IsSortedTransferableOutputs(tx.Outputs, Codec) {
		return errOutputsNotSorted
	}
	return nil
}

// SemanticVerify this transaction is valid.
// TODO make sure the ins and outs are semantically valid
func (tx *ExportTx) SemanticVerify(db database.Database) error {
	if err := tx.SyntacticVerify(); err != nil {
		return err
	}

	// Verify inputs/outputs and update the UTXO set
	if err := tx.vm.semanticVerifySpend(db, tx); err != nil {
		return tempError{fmt.Errorf("couldn't verify tx: %w", err)}
	}
	return nil
}

// Accept this transaction.
func (tx *ExportTx) Accept(batch database.Batch) error {
	txID := tx.ID()

	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(tx.vm.avm)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)

	vsmDB := versiondb.New(smDB)

	state := ava.NewPrefixedState(vsmDB, Codec)
	for i, out := range tx.Outputs {
		utxo := &ava.UTXO{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(i),
			},
			Asset: ava.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}
		if err := state.FundPlatformUTXO(utxo); err != nil {
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
func (vm *VM) newExportTx(
	networkID uint32,
	outs []*ava.TransferableOutput,
	keys []*crypto.PrivateKeySECP256K1R,
) (*ExportTx, error) {

	// Calculate inputs, outputs, and keys used to sign this tx
	inputs, outputs, credKeys, err := vm.spend(vm.DB, vm.txFee, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	outputs = append(outputs, outs...)

	// Create the tx
	tx := &ExportTx{
		UnsignedExportTx: UnsignedExportTx{
			CommonTx: CommonTx{
				NetworkID: networkID,
				Inputs:    inputs,
				Outputs:   outs,
			},
		},
	}
	ava.SortTransferableOutputs(outs, Codec)

	// Generate byte repr. of unsigned tx
	if tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedExportTx)); err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedExportTx: %w", err)
	}
	hash := hashing.ComputeHash256(tx.unsignedBytes)

	// Attach credentials that allow the inputs to be spent
	for _, inputKeys := range credKeys { // [inputKeys] are the keys used to authorize spend of an input
		cred := &secp256k1fx.Credential{}
		for _, key := range inputKeys {
			sig, err := key.SignHash(hash) // Sign hash(tx.unsignedBytes)
			if err != nil {
				return nil, fmt.Errorf("problem generating credential: %w", err)
			}
			sigArr := [crypto.SECP256K1RSigLen]byte{}
			copy(sigArr[:], sig)
			cred.Sigs = append(cred.Sigs, sigArr)
		}
		tx.Credentials = append(tx.Credentials, cred) // Attach credential to tx
	}

	return tx, tx.initialize(vm)
}
