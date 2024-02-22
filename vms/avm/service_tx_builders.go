// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func buildBaseTx(
	vm *VM,
	outs []*avax.TransferableOutput,
	memo []byte,
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, ids.ShortID, error) {
	uTx := &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Memo:         memo,
			Outs:         outs,
		},
	}

	toBurn := make(map[ids.ID]uint64) // UTXOs to move + fees
	for _, out := range outs {
		toBurn[out.AssetID()] += out.Out.Amount()
	}
	amountWithFee, err := safemath.Add64(toBurn[vm.feeAssetID], vm.TxFee)
	if err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("problem calculating required spend amount: %w", err)
	}
	toBurn[vm.feeAssetID] = amountWithFee

	ins, feeOuts, keys, err := vm.Spend(
		utxos,
		kc,
		toBurn,
		changeAddr,
	)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	uTx.Ins = ins
	uTx.Outs = append(uTx.Outs, feeOuts...)
	codec := vm.parser.Codec()
	avax.SortTransferableOutputs(uTx.Outs, codec)

	tx := &txs.Tx{Unsigned: uTx}
	return tx, changeAddr, tx.SignSECP256K1Fx(codec, keys)
}

func buildOperation(
	vm *VM,
	ops []*txs.Operation,
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	uTx := &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
		}},
		Ops: ops,
	}

	toBurn := map[ids.ID]uint64{
		vm.feeAssetID: vm.TxFee,
	}
	ins, outs, keys, err := vm.Spend(
		utxos,
		kc,
		toBurn,
		changeAddr,
	)
	if err != nil {
		return nil, err
	}

	uTx.Ins = ins
	uTx.Outs = outs
	tx := &txs.Tx{Unsigned: uTx}
	return tx, tx.SignSECP256K1Fx(vm.parser.Codec(), keys)
}

func buildImportTx(
	vm *VM,
	sourceChain ids.ID,
	atomicUTXOs []*avax.UTXO,
	to ids.ShortID,
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
) (*txs.Tx, error) {
	toBurn, importInputs, importKeys, err := vm.SpendAll(atomicUTXOs, kc)
	if err != nil {
		return nil, err
	}

	uTx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
		}},
		SourceChain: sourceChain,
		ImportedIns: importInputs,
	}

	if importedAmt := toBurn[vm.feeAssetID]; importedAmt < vm.TxFee {
		toBurn[vm.feeAssetID] = vm.TxFee - importedAmt
	} else {
		toBurn[vm.feeAssetID] = importedAmt - vm.TxFee
	}
	ins, outs, keys, err := vm.Spend(
		utxos,
		kc,
		toBurn,
		to,
	)
	if err != nil {
		return nil, err
	}
	keys = append(keys, importKeys...)

	uTx.Ins = ins
	uTx.Outs = outs
	tx := &txs.Tx{Unsigned: uTx}
	return tx, tx.SignSECP256K1Fx(vm.parser.Codec(), keys)
}

func buildExportTx(
	vm *VM,
	destinationChain ids.ID,
	to ids.ShortID,
	exportedAssetID ids.ID,
	exportedAmt uint64,
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, ids.ShortID, error) {
	uTx := &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
		}},
		DestinationChain: destinationChain,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: exportedAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: exportedAmt,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		}},
	}
	toBurn := map[ids.ID]uint64{}
	if exportedAssetID == vm.feeAssetID {
		amountWithFee, err := safemath.Add64(exportedAmt, vm.TxFee)
		if err != nil {
			return nil, ids.ShortEmpty, fmt.Errorf("problem calculating required spend amount: %w", err)
		}
		toBurn[vm.feeAssetID] = amountWithFee
	} else {
		toBurn[exportedAssetID] = exportedAmt
		toBurn[vm.feeAssetID] = vm.TxFee
	}
	ins, outs, keys, err := vm.Spend(
		utxos,
		kc,
		toBurn,
		changeAddr,
	)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	codec := vm.parser.Codec()

	uTx.Ins = ins
	uTx.Outs = outs
	tx := &txs.Tx{Unsigned: uTx}
	return tx, changeAddr, tx.SignSECP256K1Fx(codec, keys)
}
