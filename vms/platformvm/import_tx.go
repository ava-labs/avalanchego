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
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/components/avax"
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

	_ UnsignedAtomicTx = &UnsignedImportTx{}
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
	c codec.Codec,
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

	if err := tx.BaseTx.Verify(ctx, c); err != nil {
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

	allIns := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
	copy(allIns, tx.Ins)
	copy(allIns[len(tx.Ins):], tx.ImportedInputs)
	if err := syntacticVerifySpend(allIns, tx.Outs, nil, 0, feeAmount, feeAssetID); err != nil {
		return err
	}

	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedImportTx) SemanticVerify(
	vm *VM,
	db database.Database,
	stx *Tx,
) TxError {
	if err := tx.Verify(vm.avm, vm.Ctx, vm.codec, vm.txFee, vm.avaxAssetID); err != nil {
		return permError{err}
	}

	// Verify (but don't spend) imported inputs
	smDB := vm.Ctx.SharedMemory.GetDatabase(tx.SourceChain)
	defer vm.Ctx.SharedMemory.ReleaseDatabase(tx.SourceChain)

	utxos := make([]*avax.UTXO, len(tx.Ins)+len(tx.ImportedInputs))
	for index, input := range tx.Ins {
		utxoID := input.UTXOID.InputID()
		utxo, err := vm.getUTXO(db, utxoID)
		if err != nil {
			return tempError{err}
		}
		utxos[index] = utxo
	}

	state := avax.NewPrefixedState(smDB, vm.codec, vm.Ctx.ChainID, tx.SourceChain)
	for index, input := range tx.ImportedInputs {
		utxoID := input.UTXOID.InputID()
		utxo, err := state.UTXO(utxoID)
		if err != nil { // Get the UTXO
			return tempError{err}
		}
		utxos[index+len(tx.Ins)] = utxo
	}

	ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
	copy(ins, tx.Ins)
	copy(ins[len(tx.Ins):], tx.ImportedInputs)

	// Verify the flowcheck
	if err := vm.semanticVerifySpendUTXOs(tx, utxos, ins, tx.Outs, stx.Creds); err != nil {
		return err
	}

	txID := tx.ID()

	// Consume the UTXOS
	if err := vm.consumeInputs(db, tx.Ins); err != nil {
		return tempError{err}
	}
	// Produce the UTXOS
	if err := vm.produceOutputs(db, txID, tx.Outs); err != nil {
		return tempError{err}
	}

	return nil
}

// Accept this transaction and spend imported inputs
// We spend imported UTXOs here rather than in semanticVerify because
// we don't want to remove an imported UTXO in semanticVerify
// only to have the transaction not be Accepted. This would be inconsistent.
// Recall that imported UTXOs are not kept in a versionDB.
func (tx *UnsignedImportTx) Accept(ctx *snow.Context, batch database.Batch) error {
	smDB := ctx.SharedMemory.GetDatabase(tx.SourceChain)
	defer ctx.SharedMemory.ReleaseDatabase(tx.SourceChain)

	vsmDB := versiondb.New(smDB)
	state := avax.NewPrefixedState(vsmDB, Codec, ctx.ChainID, tx.SourceChain)

	// Spend imported UTXOs
	for _, in := range tx.ImportedInputs {
		utxoID := in.InputID()
		if err := state.SpendUTXO(utxoID); err != nil {
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
	chainID ids.ID, // chain to import from
	to ids.ShortID, // Address of recipient
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

	ins := []*avax.TransferableInput{}
	outs := []*avax.TransferableOutput{}
	if importedAmount < vm.txFee { // imported amount goes toward paying tx fee
		var baseSigners [][]*crypto.PrivateKeySECP256K1R
		ins, outs, _, baseSigners, err = vm.spend(vm.DB, keys, 0, vm.txFee-importedAmount)
		if err != nil {
			return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
		}
		signers = append(baseSigners, signers...)
	} else if importedAmount > vm.txFee {
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: vm.avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: importedAmount - vm.txFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		})
	}

	// Create the transaction
	utx := &UnsignedImportTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		SourceChain:    chainID,
		ImportedInputs: importedInputs,
	}
	tx := &Tx{UnsignedTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.avm, vm.Ctx, vm.codec, vm.txFee, vm.avaxAssetID)
}
