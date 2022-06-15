// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Create a new transaction
func (vm *VM) newImportTx(
	chainID ids.ID, // chain to import from
	to ids.ShortID, // Address of recipient
	keys []*crypto.PrivateKeySECP256K1R, // Keys to import the funds
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*txs.Tx, error) {
	kc := secp256k1fx.NewKeychain(keys...)

	atomicUTXOs, _, _, err := vm.GetAtomicUTXOs(chainID, kc.Addresses(), ids.ShortEmpty, ids.Empty, maxPageSize)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}

	importedInputs := []*avax.TransferableInput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}

	importedAmount := uint64(0)
	now := vm.clock.Unix()
	for _, utxo := range atomicUTXOs {
		if utxo.AssetID() != vm.ctx.AVAXAssetID {
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
	if importedAmount < vm.TxFee { // imported amount goes toward paying tx fee
		var baseSigners [][]*crypto.PrivateKeySECP256K1R
		ins, outs, _, baseSigners, err = vm.stake(keys, 0, vm.TxFee-importedAmount, changeAddr)
		if err != nil {
			return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
		}
		signers = append(baseSigners, signers...)
	} else if importedAmount > vm.TxFee {
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: importedAmount - vm.TxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		})
	}

	// Create the transaction
	utx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		SourceChain:    chainID,
		ImportedInputs: importedInputs,
	}
	tx, err := txs.NewSigned(utx, txs.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(vm.ctx)
}
