// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestExportTxVerifyNil(t *testing.T) {
	var exportTx *UnsignedExportTx
	if err := exportTx.Verify(testXChainID, NewContext(), testTxFee, testAvaxAssetID); err == nil {
		t.Fatal("Verify should have failed due to nil transaction")
	}
}

func TestExportTxVerify(t *testing.T) {
	var exportAmount uint64 = 10000000
	exportTx := &UnsignedExportTx{
		NetworkID:        testNetworkID,
		BlockchainID:     testCChainID,
		DestinationChain: testXChainID,
		Ins: []EVMInput{
			{
				Address: testEthAddrs[0],
				Amount:  exportAmount,
				AssetID: testAvaxAssetID,
				Nonce:   0,
			},
			{
				Address: testEthAddrs[2],
				Amount:  exportAmount,
				AssetID: testAvaxAssetID,
				Nonce:   0,
			},
		},
		ExportedOutputs: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: testAvaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: exportAmount - testTxFee,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{testShortIDAddrs[0]},
					},
				},
			},
			{
				Asset: avax.Asset{ID: testAvaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: exportAmount, // only subtract fee from one output
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{testShortIDAddrs[1]},
					},
				},
			},
		},
	}

	// Sort the inputs and outputs to ensure the transaction is canonical
	avax.SortTransferableOutputs(exportTx.ExportedOutputs, Codec)
	// Pass in a list of signers here with the appropriate length
	// to avoid causing a nil-pointer error in the helper method
	emptySigners := make([][]*crypto.PrivateKeySECP256K1R, 2)
	SortEVMInputsAndSigners(exportTx.Ins, emptySigners)

	ctx := NewContext()

	// Test Valid Export Tx
	if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err != nil {
		t.Fatalf("Failed to verify valid ExportTx: %s", err)
	}

	exportTx.syntacticallyVerified = false
	exportTx.NetworkID = testNetworkID + 1

	// Test Incorrect Network ID Errors
	if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err == nil {
		t.Fatal("ExportTx should have failed verification due to incorrect network ID")
	}

	exportTx.syntacticallyVerified = false
	exportTx.NetworkID = testNetworkID
	exportTx.BlockchainID = nonExistentID
	// Test Incorrect Blockchain ID Errors
	if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err == nil {
		t.Fatal("ExportTx should have failed verification due to incorrect blockchain ID")
	}

	exportTx.syntacticallyVerified = false
	exportTx.BlockchainID = testCChainID
	exportTx.DestinationChain = nonExistentID
	// Test Incorrect Destination Chain ID Errors
	if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err == nil {
		t.Fatal("ExportTx should have failed verification due to incorrect destination chain")
	}

	exportTx.syntacticallyVerified = false
	exportTx.DestinationChain = testXChainID
	exportedOuts := exportTx.ExportedOutputs
	exportTx.ExportedOutputs = nil
	// Test No Exported Outputs Errors
	if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err == nil {
		t.Fatal("ExportTx should have failed verification due to no exported outputs")
	}

	exportTx.syntacticallyVerified = false
	exportTx.ExportedOutputs = []*avax.TransferableOutput{exportedOuts[1], exportedOuts[0]}
	// Test Unsorted outputs Errors
	if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err == nil {
		t.Fatal("ExportTx should have failed verification due to no unsorted exported outputs")
	}

	exportTx.syntacticallyVerified = false
	exportTx.ExportedOutputs = []*avax.TransferableOutput{exportedOuts[0], nil}
	// Test invalid exported output
	if err := exportTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID); err == nil {
		t.Fatal("ExportTx should have failed verification due to invalid output")
	}
}
