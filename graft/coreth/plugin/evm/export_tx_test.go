// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestExportTxVerifyNil(t *testing.T) {
	var exportTx *UnsignedExportTx
	if err := exportTx.Verify(testXChainID, NewContext(), apricotRulesPhase0); err == nil {
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
					Amt: exportAmount,
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
					Amt: exportAmount,
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
	if err := exportTx.Verify(testXChainID, ctx, apricotRulesPhase1); err != nil {
		t.Fatalf("Failed to verify valid ExportTx: %s", err)
	}

	exportTx.NetworkID = testNetworkID + 1

	// Test Incorrect Network ID Errors
	if err := exportTx.Verify(testXChainID, ctx, apricotRulesPhase1); err == nil {
		t.Fatal("ExportTx should have failed verification due to incorrect network ID")
	}

	exportTx.NetworkID = testNetworkID
	exportTx.BlockchainID = nonExistentID
	// Test Incorrect Blockchain ID Errors
	if err := exportTx.Verify(testXChainID, ctx, apricotRulesPhase1); err == nil {
		t.Fatal("ExportTx should have failed verification due to incorrect blockchain ID")
	}

	exportTx.BlockchainID = testCChainID
	exportTx.DestinationChain = nonExistentID
	// Test Incorrect Destination Chain ID Errors
	if err := exportTx.Verify(testXChainID, ctx, apricotRulesPhase1); err == nil {
		t.Fatal("ExportTx should have failed verification due to incorrect destination chain")
	}

	exportTx.DestinationChain = testXChainID
	exportedOuts := exportTx.ExportedOutputs
	exportTx.ExportedOutputs = nil
	evmInputs := exportTx.Ins
	// Test No Exported Outputs Errors
	if err := exportTx.Verify(testXChainID, ctx, apricotRulesPhase1); err == nil {
		t.Fatal("ExportTx should have failed verification due to no exported outputs")
	}

	exportTx.ExportedOutputs = []*avax.TransferableOutput{exportedOuts[1], exportedOuts[0]}
	// Test Unsorted outputs Errors
	if err := exportTx.Verify(testXChainID, ctx, apricotRulesPhase1); err == nil {
		t.Fatal("ExportTx should have failed verification due to no unsorted exported outputs")
	}

	exportTx.ExportedOutputs = []*avax.TransferableOutput{exportedOuts[0], nil}
	// Test invalid exported output
	if err := exportTx.Verify(testXChainID, ctx, apricotRulesPhase1); err == nil {
		t.Fatal("ExportTx should have failed verification due to invalid output")
	}

	exportTx.ExportedOutputs = []*avax.TransferableOutput{exportedOuts[0], exportedOuts[1]}
	exportTx.Ins = []EVMInput{evmInputs[1], evmInputs[0]}
	// Test unsorted EVM Inputs passes before AP1
	if err := exportTx.Verify(testXChainID, ctx, apricotRulesPhase0); err != nil {
		t.Fatalf("ExportTx should have passed verification before AP1, but failed due to %s", err)
	}
	// Test unsorted EVM Inputs fails after AP1
	if err := exportTx.Verify(testXChainID, ctx, apricotRulesPhase1); err == nil {
		t.Fatal("ExportTx should have failed verification due to unsorted EVM Inputs")
	}
	exportTx.Ins = []EVMInput{
		{
			Address: testEthAddrs[0],
			Amount:  0,
			AssetID: testAvaxAssetID,
			Nonce:   0,
		},
	}
	// Test ExportTx with invalid EVM Input amount 0 fails verification
	if err := exportTx.Verify(testXChainID, ctx, apricotRulesPhase1); err == nil {
		t.Fatal("ExportTx should have failed verification due to 0 value amount")
	}
	exportTx.Ins = []EVMInput{evmInputs[0], evmInputs[0]}
	// Test non-unique EVM Inputs passes verification before AP1
	if err := exportTx.Verify(testXChainID, ctx, apricotRulesPhase0); err != nil {
		t.Fatalf("ExportTx with non-unique EVM Inputs should have passed verification prior to AP1, but failed due to %s", err)
	}
	exportTx.Ins = []EVMInput{evmInputs[0], evmInputs[0]}
	// Test non-unique EVM Inputs fails verification after AP1
	if err := exportTx.Verify(testXChainID, ctx, apricotRulesPhase1); err == nil {
		t.Fatal("ExportTx should have failed verification due to non-unique inputs")
	}
}

// Note: this is a brittle test to ensure that the gas cost of a transaction does
// not change
func TestExportTxGasCost(t *testing.T) {
	avaxAssetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()
	xChainID := ids.GenerateTestID()
	networkID := uint32(5)
	exportAmount := uint64(5000000)

	tests := map[string]struct {
		UnsignedExportTx *UnsignedExportTx
		Keys             [][]*crypto.PrivateKeySECP256K1R

		BaseFee      *big.Int
		ExpectedCost uint64
		ExpectedFee  uint64
	}{
		"simple export 25Gwei BaseFee": {
			UnsignedExportTx: &UnsignedExportTx{
				NetworkID:        networkID,
				BlockchainID:     chainID,
				DestinationChain: xChainID,
				Ins: []EVMInput{
					{
						Address: testEthAddrs[0],
						Amount:  exportAmount,
						AssetID: avaxAssetID,
						Nonce:   0,
					},
				},
				ExportedOutputs: []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: avaxAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: exportAmount,
							OutputOwners: secp256k1fx.OutputOwners{
								Locktime:  0,
								Threshold: 1,
								Addrs:     []ids.ShortID{testShortIDAddrs[0]},
							},
						},
					},
				},
			},
			Keys:         [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}},
			ExpectedCost: 1307,
			ExpectedFee:  32675,
			BaseFee:      big.NewInt(25 * params.GWei),
		},
		"simple export 225Gwei BaseFee": {
			UnsignedExportTx: &UnsignedExportTx{
				NetworkID:        networkID,
				BlockchainID:     chainID,
				DestinationChain: xChainID,
				Ins: []EVMInput{
					{
						Address: testEthAddrs[0],
						Amount:  exportAmount,
						AssetID: avaxAssetID,
						Nonce:   0,
					},
				},
				ExportedOutputs: []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: avaxAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: exportAmount,
							OutputOwners: secp256k1fx.OutputOwners{
								Locktime:  0,
								Threshold: 1,
								Addrs:     []ids.ShortID{testShortIDAddrs[0]},
							},
						},
					},
				},
			},
			Keys:         [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}},
			ExpectedCost: 1307,
			ExpectedFee:  294075,
			BaseFee:      big.NewInt(225 * params.GWei),
		},
		"complex export 25Gwei BaseFee": {
			UnsignedExportTx: &UnsignedExportTx{
				NetworkID:        networkID,
				BlockchainID:     chainID,
				DestinationChain: xChainID,
				Ins: []EVMInput{
					{
						Address: testEthAddrs[0],
						Amount:  exportAmount,
						AssetID: avaxAssetID,
						Nonce:   0,
					},
					{
						Address: testEthAddrs[1],
						Amount:  exportAmount,
						AssetID: avaxAssetID,
						Nonce:   0,
					},
					{
						Address: testEthAddrs[2],
						Amount:  exportAmount,
						AssetID: avaxAssetID,
						Nonce:   0,
					},
				},
				ExportedOutputs: []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: avaxAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: exportAmount * 3,
							OutputOwners: secp256k1fx.OutputOwners{
								Locktime:  0,
								Threshold: 1,
								Addrs:     []ids.ShortID{testShortIDAddrs[0]},
							},
						},
					},
				},
			},
			Keys:         [][]*crypto.PrivateKeySECP256K1R{{testKeys[0], testKeys[0], testKeys[0]}},
			ExpectedCost: 3573,
			ExpectedFee:  89325,
			BaseFee:      big.NewInt(25 * params.GWei),
		},
		"complex export 225Gwei BaseFee": {
			UnsignedExportTx: &UnsignedExportTx{
				NetworkID:        networkID,
				BlockchainID:     chainID,
				DestinationChain: xChainID,
				Ins: []EVMInput{
					{
						Address: testEthAddrs[0],
						Amount:  exportAmount,
						AssetID: avaxAssetID,
						Nonce:   0,
					},
					{
						Address: testEthAddrs[1],
						Amount:  exportAmount,
						AssetID: avaxAssetID,
						Nonce:   0,
					},
					{
						Address: testEthAddrs[2],
						Amount:  exportAmount,
						AssetID: avaxAssetID,
						Nonce:   0,
					},
				},
				ExportedOutputs: []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: avaxAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: exportAmount * 3,
							OutputOwners: secp256k1fx.OutputOwners{
								Locktime:  0,
								Threshold: 1,
								Addrs:     []ids.ShortID{testShortIDAddrs[0]},
							},
						},
					},
				},
			},
			Keys:         [][]*crypto.PrivateKeySECP256K1R{{testKeys[0], testKeys[0], testKeys[0]}},
			ExpectedCost: 3573,
			ExpectedFee:  803925,
			BaseFee:      big.NewInt(225 * params.GWei),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			tx := &Tx{UnsignedAtomicTx: test.UnsignedExportTx}

			// Sign with the correct key
			if err := tx.Sign(Codec, test.Keys); err != nil {
				t.Fatal(err)
			}

			cost, err := tx.Cost()
			if err != nil {
				t.Fatal(err)
			}
			if cost != test.ExpectedCost {
				t.Fatalf("Expected cost to be %d, but found %d", test.ExpectedCost, cost)
			}

			fee, err := calculateDynamicFee(cost, test.BaseFee)
			if err != nil {
				t.Fatal(err)
			}
			if fee != test.ExpectedFee {
				t.Fatalf("Expected fee to be %d, but found %d", test.ExpectedFee, fee)
			}
		})
	}
}
