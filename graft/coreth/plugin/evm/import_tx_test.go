// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestImportTxVerifyNil(t *testing.T) {
	var importTx UnsignedImportTx
	if err := importTx.Verify(testXChainID, NewContext(), testTxFee, testAvaxAssetID); err == nil {
		t.Fatal("Verify should have failed due to nil transaction")
	}
}

func TestImportTxVerify(t *testing.T) {
	var importAmount uint64 = 10000000
	txID := ids.NewID([32]byte{0xff})
	importTx := &UnsignedImportTx{
		NetworkID:    testNetworkID,
		BlockchainID: testCChainID,
		SourceChain:  testXChainID,
		ImportedInputs: []*avax.TransferableInput{
			{
				UTXOID: avax.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(0),
				},
				Asset: avax.Asset{ID: testAvaxAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: importAmount,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			},
			{
				UTXOID: avax.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(1),
				},
				Asset: avax.Asset{ID: testAvaxAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: importAmount,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			},
		},
		Outs: []EVMOutput{
			{
				Address: testEthAddrs[0],
				Amount:  importAmount,
				AssetID: testAvaxAssetID,
			},
			{
				Address: testEthAddrs[1],
				Amount:  importAmount,
				AssetID: testAvaxAssetID,
			},
		},
	}

	ctx := NewContext()

	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err != nil {
		t.Fatalf("Failed to verify ImportTx: %w", err)
	}
	// // Sort the inputs and outputs to ensure the transaction is canonical
	// avax.SortTransferableOutputs(exportTx.ExportedOutputs, Codec)
	// // Pass in a list of signers here with the appropriate length
	// // to avoid causing a nil-pointer error in the helper method
	// emptySigners := make([][]*crypto.PrivateKeySECP256K1R, 2)
	// SortEVMInputsAndSigners(exportTx.Ins, emptySigners)

	// // Test Valid Export Tx
	// if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err != nil {
	// 	t.Fatalf("Failed to verify valid ExportTx: %w", err)
	// }

	// exportTx.syntacticallyVerified = false
	// exportTx.NetworkID = testNetworkID + 1

	// // Test Incorrect Network ID Errors
	// if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err == nil {
	// 	t.Fatal("ExportTx should have failed verification due to incorrect network ID")
	// }

	// exportTx.syntacticallyVerified = false
	// exportTx.NetworkID = testNetworkID
	// exportTx.BlockchainID = nonExistentID
	// // Test Incorrect Blockchain ID Errors
	// if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err == nil {
	// 	t.Fatal("ExportTx should have failed verification due to incorrect blockchain ID")
	// }

	// exportTx.syntacticallyVerified = false
	// exportTx.BlockchainID = testCChainID
	// exportTx.DestinationChain = nonExistentID
	// // Test Incorrect Destination Chain ID Errors
	// if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err == nil {
	// 	t.Fatal("ExportTx should have failed verification due to incorrect destination chain")
	// }

	// exportTx.syntacticallyVerified = false
	// exportTx.DestinationChain = testXChainID
	// exportedOuts := exportTx.ExportedOutputs
	// exportTx.ExportedOutputs = nil
	// // Test No Exported Outputs Errors
	// if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err == nil {
	// 	t.Fatal("ExportTx should have failed verification due to no exported outputs")
	// }

	// exportTx.syntacticallyVerified = false
	// exportTx.ExportedOutputs = []*avax.TransferableOutput{exportedOuts[1], exportedOuts[0]}
	// // Test Unsorted outputs Errors
	// if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err == nil {
	// 	t.Fatal("ExportTx should have failed verification due to no exported outputs")
	// }
}
