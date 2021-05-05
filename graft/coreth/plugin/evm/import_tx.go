// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// UnsignedImportTx is an unsigned ImportTx
type UnsignedImportTx struct {
	avax.Metadata
	// ID of the network on which this tx was issued
	NetworkID uint32 `serialize:"true" json:"networkID"`
	// ID of this blockchain.
	BlockchainID ids.ID `serialize:"true" json:"blockchainID"`
	// Which chain to consume the funds from
	SourceChain ids.ID `serialize:"true" json:"sourceChain"`
	// Inputs that consume UTXOs produced on the chain
	ImportedInputs []*avax.TransferableInput `serialize:"true" json:"importedInputs"`
	// Outputs
	Outs []EVMOutput `serialize:"true" json:"outputs"`
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
	rules params.Rules,
) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.SourceChain != avmID:
		return errWrongChainID
	case len(tx.ImportedInputs) == 0:
		return errNoImportInputs
	case tx.NetworkID != ctx.NetworkID:
		return errWrongNetworkID
	case ctx.ChainID != tx.BlockchainID:
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

	if rules.IsApricotPhase2 {
		if !IsSortedAndUniqueEVMOutputs(tx.Outs) {
			return errOutputsNotSortedUnique
		}
	} else if rules.IsApricotPhase1 {
		if !IsSortedEVMOutputs(tx.Outs) {
			return errOutputsNotSorted
		}
	}

	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedImportTx) SemanticVerify(
	vm *VM,
	stx *Tx,
	rules params.Rules,
) TxError {
	if err := tx.Verify(vm.ctx.XChainID, vm.ctx, vm.txFee, vm.ctx.AVAXAssetID, rules); err != nil {
		return tempError{err}
	}

	if len(stx.Creds) != len(tx.ImportedInputs) {
		return permError{errSignatureInputsMismatch}
	}

	// do flow-checking
	fc := avax.NewFlowChecker()

	// Apply transaction fee to import transactions as of Apricot Phase 2
	if rules.IsApricotPhase2 {
		fc.Produce(vm.ctx.AVAXAssetID, vm.txFee)
	}

	for _, out := range tx.Outs {
		fc.Produce(out.AssetID, out.Amount)
	}

	for _, in := range tx.ImportedInputs {
		fc.Consume(in.AssetID(), in.Input().Amount())
	}
	if err := fc.Verify(); err != nil {
		return permError{err}
	}

	if !vm.ctx.IsBootstrapped() {
		// Allow for force committing during bootstrapping
		return nil
	}

	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		inputID := in.UTXOID.InputID()
		utxoIDs[i] = inputID[:]
	}
	// allUTXOBytes is guaranteed to be the same length as utxoIDs
	allUTXOBytes, err := vm.ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
	if err != nil {
		return tempError{fmt.Errorf("failed to fetch import UTXOs from %s with %w", tx.SourceChain, err)}
	}

	for i, in := range tx.ImportedInputs {
		utxoBytes := allUTXOBytes[i]

		utxo := &avax.UTXO{}
		if _, err := vm.codec.Unmarshal(utxoBytes, utxo); err != nil {
			return tempError{err}
		}

		cred := stx.Creds[i]

		utxoAssetID := utxo.AssetID()
		inAssetID := in.AssetID()
		if utxoAssetID != inAssetID {
			return permError{errAssetIDMismatch}
		}

		if err := vm.fx.VerifyTransfer(tx, in.In, cred, utxo.Out); err != nil {
			return tempError{err}
		}
	}
	return nil
}

// Accept this transaction and spend imported inputs
// We spend imported UTXOs here rather than in semanticVerify because
// we don't want to remove an imported UTXO in semanticVerify
// only to have the transaction not be Accepted. This would be inconsistent.
// Recall that imported UTXOs are not kept in a versionDB.
func (tx *UnsignedImportTx) Accept(ctx *snow.Context, _ database.Batch) error {
	// TODO: Is any batch passed in here?
	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		inputID := in.InputID()
		utxoIDs[i] = inputID[:]
	}
	return ctx.SharedMemory.Remove(tx.SourceChain, utxoIDs)
}

// newImportTx returns a new ImportTx
func (vm *VM) newImportTx(
	chainID ids.ID, // chain to import from
	to common.Address, // Address of recipient
	keys []*crypto.PrivateKeySECP256K1R, // Keys to import the funds
) (*Tx, error) {
	if vm.ctx.XChainID != chainID {
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

	importedAmount := make(map[ids.ID]uint64)
	now := vm.clock.Unix()
	for _, utxo := range atomicUTXOs {
		inputIntf, utxoSigners, err := kc.Spend(utxo.Out, now)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			continue
		}
		aid := utxo.AssetID()
		importedAmount[aid], err = math.Add64(importedAmount[aid], input.Amount())
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
	importedAVAXAmount := importedAmount[vm.ctx.AVAXAssetID]
	outs := []EVMOutput{}

	// AVAX output
	if importedAVAXAmount < vm.txFee { // imported amount goes toward paying tx fee
		return nil, errInsufficientFundsForFee
	} else if importedAVAXAmount > vm.txFee {
		outs = append(outs, EVMOutput{
			Address: to,
			Amount:  importedAVAXAmount - vm.txFee,
			AssetID: vm.ctx.AVAXAssetID,
		})
	}

	// This will create unique outputs (in the context of sorting)
	// since each output will have a unique assetID
	for assetID, amount := range importedAmount {
		// Skip the AVAX amount since it has already been included
		// and skip any input with an amount of 0
		if assetID == vm.ctx.AVAXAssetID || amount == 0 {
			continue
		}
		outs = append(outs, EVMOutput{
			Address: to,
			Amount:  amount,
			AssetID: assetID,
		})
	}

	// If no outputs are produced, return an error.
	// Note: this can happen if there is exactly enough AVAX to pay the
	// transaction fee, but no other funds to be imported.
	if len(outs) == 0 {
		return nil, errNoEVMOutputs
	}

	SortEVMOutputs(outs)

	// Create the transaction
	utx := &UnsignedImportTx{
		NetworkID:      vm.ctx.NetworkID,
		BlockchainID:   vm.ctx.ChainID,
		Outs:           outs,
		ImportedInputs: importedInputs,
		SourceChain:    chainID,
	}
	tx := &Tx{UnsignedAtomicTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.ctx.XChainID, vm.ctx, vm.txFee, vm.ctx.AVAXAssetID, vm.currentRules())
}

// EVMStateTransfer performs the state transfer to increase the balances of
// accounts accordingly with the imported EVMOutputs
func (tx *UnsignedImportTx) EVMStateTransfer(vm *VM, state *state.StateDB) error {
	for _, to := range tx.Outs {
		if to.AssetID == vm.ctx.AVAXAssetID {
			log.Debug("crosschain X->C", "addr", to.Address, "amount", to.Amount, "assetID", "AVAX")
			// If the asset is AVAX, convert the input amount in nAVAX to gWei by
			// multiplying by the x2c rate.
			amount := new(big.Int).Mul(
				new(big.Int).SetUint64(to.Amount), x2cRate)
			state.AddBalance(to.Address, amount)
		} else {
			log.Debug("crosschain X->C", "addr", to.Address, "amount", to.Amount, "assetID", to.AssetID)
			amount := new(big.Int).SetUint64(to.Amount)
			state.AddBalanceMultiCoin(to.Address, common.Hash(to.AssetID), amount)
		}
	}
	return nil
}
