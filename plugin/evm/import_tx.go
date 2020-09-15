// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/core/state"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/go-ethereum/common"
)

// UnsignedImportTx is an unsigned ImportTx
type UnsignedImportTx struct {
	avax.Metadata
	// true iff this transaction has already passed syntactic verification
	syntacticallyVerified bool
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
	if err := tx.Verify(vm.ctx.XChainID, vm.ctx, vm.txFee, vm.ctx.AVAXAssetID); err != nil {
		return permError{err}
	}

	// do flow-checking
	fc := avax.NewFlowChecker()
	fc.Produce(vm.ctx.AVAXAssetID, vm.txFee)

	for _, out := range tx.Outs {
		fc.Produce(vm.ctx.AVAXAssetID, out.Amount)
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
		utxoIDs[i] = in.UTXOID.InputID().Bytes()
	}
	allUTXOBytes, err := vm.ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
	if err != nil {
		return tempError{err}
	}

	utxos := make([]*avax.UTXO, len(tx.ImportedInputs))
	for i, utxoBytes := range allUTXOBytes {
		utxo := &avax.UTXO{}
		if err := vm.codec.Unmarshal(utxoBytes, utxo); err != nil {
			return tempError{err}
		}
		utxos[i] = utxo
	}

	for i, in := range tx.ImportedInputs {
		utxoBytes := allUTXOBytes[i]

		utxo := &avax.UTXO{}
		if err := vm.codec.Unmarshal(utxoBytes, utxo); err != nil {
			return tempError{err}
		}

		cred := stx.Creds[i]

		utxoAssetID := utxo.AssetID()
		inAssetID := in.AssetID()
		if !utxoAssetID.Equals(inAssetID) {
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
		utxoIDs[i] = in.InputID().Bytes()
	}
	return ctx.SharedMemory.Remove(tx.SourceChain, utxoIDs)
}

// Create a new transaction
func (vm *VM) newImportTx(
	chainID ids.ID, // chain to import from
	to common.Address, // Address of recipient
	keys []*crypto.PrivateKeySECP256K1R, // Keys to import the funds
) (*Tx, error) {
	if !vm.ctx.XChainID.Equals(chainID) {
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
		if !utxo.AssetID().Equals(vm.ctx.AVAXAssetID) {
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

	nonce, err := vm.GetAcceptedNonce(to)
	if err != nil {
		return nil, err
	}

	outs := []EVMOutput{}
	if importedAmount < vm.txFee { // imported amount goes toward paying tx fee
		// TODO: spend EVM balance to compensate vm.txFee-importedAmount
		return nil, errNoFunds
	} else if importedAmount > vm.txFee {
		outs = append(outs, EVMOutput{
			Address: to,
			Amount:  importedAmount - vm.txFee,
			Nonce:   nonce,
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
	return tx, utx.Verify(vm.ctx.XChainID, vm.ctx, vm.txFee, vm.ctx.AVAXAssetID)
}

func (tx *UnsignedImportTx) EVMStateTransfer(state *state.StateDB) error {
	for _, to := range tx.Outs {
		state.AddBalance(to.Address,
			new(big.Int).Mul(
				new(big.Int).SetUint64(to.Amount), x2cRate))
		if state.GetNonce(to.Address) != to.Nonce {
			return errInvalidNonce
		}
		state.SetNonce(to.Address, to.Nonce+1)
	}
	return nil
}
