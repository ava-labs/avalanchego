// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	crypto "github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
	"github.com/ava-labs/go-ethereum/common"
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
	avax.Metadata
	// true iff this transaction has already passed syntactic verification
	syntacticallyVerified bool
	// ID of the network on which this tx was issued
	NetworkID uint32 `serialize:"true" json:"networkID"`
	// ID of this blockchain. In practice is always the empty ID.
	// This is only here to match avm.BaseTx's format
	BlockchainID ids.ID `serialize:"true" json:"blockchainID"`
	// Outputs
	Outs []EVMOutput `serialize:"true" json:"outputs"`
	// Memo field contains arbitrary bytes, up to maxMemoSize
	Memo []byte `serialize:"true" json:"memo"`

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
		return errWrongChainID
	case len(tx.ImportedInputs) == 0:
		return errNoImportInputs
	case tx.NetworkID != ctx.NetworkID:
		return errWrongNetworkID
	case !ctx.ChainID.Equals(tx.BlockchainID):
		return errWrongBlockchainID
	}

	for _, out := range tx.Outs {
		if err := out.Verify(); err != nil {
			return err
		}
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

	// do flow-checking
	fc := avax.NewFlowChecker()
	fc.Produce(vm.avaxAssetID, vm.txFee)

	for _, out := range tx.Outs {
		fc.Produce(vm.avaxAssetID, out.Amount)
	}

	for _, in := range tx.ImportedInputs {
		fc.Consume(in.AssetID(), in.Input().Amount())
	}
	if err := fc.Verify(); err != nil {
		return permError{err}
	}

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
	keys []*crypto.PrivateKeySECP256K1R, // Keys to import the funds
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

	outs := []EVMOutput{}
	if importedAmount < vm.txFee { // imported amount goes toward paying tx fee
		// TODO: spend EVM balance to compensate vm.txFee-importedAmount
		return nil, errNoFunds
	} else if importedAmount > vm.txFee {
		outs = append(outs, EVMOutput{
			Address: to,
			Amount:  importedAmount - vm.txFee,
		})
	}

	// Create the transaction
	utx := &UnsignedImportTx{
		NetworkID:      vm.ctx.NetworkID,
		BlockchainID:   vm.ctx.ChainID,
		Outs:           outs,
		ImportedInputs: importedInputs,
		SourceChain:    chainID,
	}
	tx := &Tx{UnsignedTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.avm, vm.ctx, vm.txFee, vm.avaxAssetID)
}
