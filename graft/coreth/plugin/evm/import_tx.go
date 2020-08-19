// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	//"crypto/ecdsa"
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	avacrypto "github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/components/avax"
	//"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
	"github.com/ava-labs/go-ethereum/common"
	//"github.com/ava-labs/go-ethereum/crypto"
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
	errWrongChainID               = errors.New("tx has wrong chain ID")
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

	if err := tx.BaseTx.Verify(ctx); err != nil {
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
	stx *Tx,
) TxError {
	if err := tx.Verify(vm.avm, vm.ctx, vm.txFee, vm.avaxAssetID); err != nil {
		return permError{err}
	}
	// TODO: verify using avax.VerifyTx(vm.txFee, vm.avaxAssetID, tx.Ins, outs)
	// TODO: verify UTXO inputs via gRPC (with creds)
	return nil
}

// Accept this transaction and spend imported inputs
// We spend imported UTXOs here rather than in semanticVerify because
// we don't want to remove an imported UTXO in semanticVerify
// only to have the transaction not be Accepted. This would be inconsistent.
// Recall that imported UTXOs are not kept in a versionDB.
func (tx *UnsignedImportTx) Accept(batch database.Batch) error {
	// TODO: finish this function via gRPC
	return nil
}

// Create a new transaction
func (vm *VM) newImportTx(
	chainID ids.ID, // chain to import from
	to common.Address, // Address of recipient
	keys []*avacrypto.PrivateKeySECP256K1R, // Keys to import the funds
) (*Tx, error) {
	if !vm.avm.Equals(chainID) {
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
	outs := []EVMOutput{}
	if importedAmount < vm.txFee { // imported amount goes toward paying tx fee
		// TODO: spend EVM balance to compensate vm.txFee-importedAmount
	} else if importedAmount > vm.txFee {
		outs = append(outs, EVMOutput{
			Address: to,
			Amount:  importedAmount - vm.txFee,
		})
	}

	// Create the transaction
	utx := &UnsignedImportTx{
		BaseTx: BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		},
		SourceChain:    chainID,
		ImportedInputs: importedInputs,
	}
	tx := &Tx{UnsignedTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.avm, vm.ctx, vm.txFee, vm.avaxAssetID)
}
