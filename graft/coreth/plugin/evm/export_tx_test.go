// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// SemanticVerify
// SemanticVerify calls Verify - if any of those checks fail then SemanticVerify should fail as well
// catch flow check for AVAX and non-AVAX assets
// incorrect number of credentials
// catch incorrect type for verifiable - or change the type to just be the correct credential type (make sure it can't be nil either)
// ensure that the number of signatures in a credential must be exactly 1
// input Address does not match signature

// Accept
// ensure that the state transition adds the correct UTXOs with both AVAX and non-AVAX assets

// newExportTx
// Verify that newExportTx burns the correct amount for each of the tests

// EVMStateTransfer
// catches insufficient funds for both AVAX and non-AVAX assets
// Decreases balances by the correct amounts
// catch incorrect nonce for EVM Input and increases the nonce correctly

func TestExportTxSemanticVerify(t *testing.T) {
	_, vm, _, _ := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key := testKeys[0]
	addr := key.PublicKey().Address()
	ethAddr := testEthAddrs[0]

	var (
		avaxBalance           = 10 * units.Avax
		custom0Balance uint64 = 100
		custom0AssetID        = ids.ID{1, 2, 3, 4, 5}
		custom1Balance uint64 = 1000
		custom1AssetID        = ids.ID{1, 2, 3, 4, 5, 6}
	)

	validExportTx := &UnsignedExportTx{
		NetworkID:        vm.ctx.NetworkID,
		BlockchainID:     vm.ctx.ChainID,
		DestinationChain: vm.ctx.XChainID,
		Ins: []EVMInput{
			{
				Address: ethAddr,
				Amount:  avaxBalance,
				AssetID: vm.ctx.AVAXAssetID,
				Nonce:   0,
			},
			{
				Address: ethAddr,
				Amount:  custom0Balance,
				AssetID: custom0AssetID,
				Nonce:   0,
			},
			{
				Address: ethAddr,
				Amount:  custom1Balance,
				AssetID: custom1AssetID,
				Nonce:   0,
			},
		},
		ExportedOutputs: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: custom0AssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: custom0Balance,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr},
					},
				},
			},
		},
	}
	validTx := &Tx{UnsignedAtomicTx: validExportTx}

	tests := []struct {
		name      string
		tx        *Tx
		signers   [][]*crypto.PrivateKeySECP256K1R
		baseFee   *big.Int
		rules     params.Rules
		shouldErr bool
	}{
		{
			name: "validTx",
			tx:   validTx,
			signers: [][]*crypto.PrivateKeySECP256K1R{
				{key},
				{key},
				{key},
			},
			baseFee:   initialBaseFee,
			rules:     apricotRulesPhase3,
			shouldErr: false,
		},
		{
			name: "wrong destination",
			tx: func() *Tx {
				validExportTx := *validExportTx
				validExportTx.DestinationChain = ids.GenerateTestID()
				return &Tx{UnsignedAtomicTx: &validExportTx}
			}(),
			signers: [][]*crypto.PrivateKeySECP256K1R{
				{key},
				{key},
				{key},
			},
			baseFee:   initialBaseFee,
			rules:     apricotRulesPhase3,
			shouldErr: true,
		},
		{
			name: "no outputs",
			tx: func() *Tx {
				validExportTx := *validExportTx
				validExportTx.ExportedOutputs = nil
				return &Tx{UnsignedAtomicTx: &validExportTx}
			}(),
			signers: [][]*crypto.PrivateKeySECP256K1R{
				{key},
				{key},
				{key},
			},
			baseFee:   initialBaseFee,
			rules:     apricotRulesPhase3,
			shouldErr: true,
		},
		{
			name: "wrong networkID",
			tx: func() *Tx {
				validExportTx := *validExportTx
				validExportTx.NetworkID++
				return &Tx{UnsignedAtomicTx: &validExportTx}
			}(),
			signers: [][]*crypto.PrivateKeySECP256K1R{
				{key},
				{key},
				{key},
			},
			baseFee:   initialBaseFee,
			rules:     apricotRulesPhase3,
			shouldErr: true,
		},
		{
			name: "wrong chainID",
			tx: func() *Tx {
				validExportTx := *validExportTx
				validExportTx.BlockchainID = ids.GenerateTestID()
				return &Tx{UnsignedAtomicTx: &validExportTx}
			}(),
			signers: [][]*crypto.PrivateKeySECP256K1R{
				{key},
				{key},
				{key},
			},
			baseFee:   initialBaseFee,
			rules:     apricotRulesPhase3,
			shouldErr: true,
		},
		{
			name: "invalid input",
			tx: func() *Tx {
				validExportTx := *validExportTx
				validExportTx.Ins = append([]EVMInput{}, validExportTx.Ins...)
				validExportTx.Ins[2].Amount = 0
				return &Tx{UnsignedAtomicTx: &validExportTx}
			}(),
			signers: [][]*crypto.PrivateKeySECP256K1R{
				{key},
				{key},
				{key},
			},
			baseFee:   initialBaseFee,
			rules:     apricotRulesPhase3,
			shouldErr: true,
		},
		{
			name: "invalid output",
			tx: func() *Tx {
				validExportTx := *validExportTx
				validExportTx.ExportedOutputs = []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: custom0AssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: custom0Balance,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 0,
							Addrs:     []ids.ShortID{addr},
						},
					},
				}}
				return &Tx{UnsignedAtomicTx: &validExportTx}
			}(),
			signers: [][]*crypto.PrivateKeySECP256K1R{
				{key},
				{key},
				{key},
			},
			baseFee:   initialBaseFee,
			rules:     apricotRulesPhase3,
			shouldErr: true,
		},
		{
			name: "unsorted outputs",
			tx: func() *Tx {
				validExportTx := *validExportTx
				validExportTx.ExportedOutputs = []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: custom0AssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: custom0Balance/2 + 1,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{addr},
							},
						},
					},
					{
						Asset: avax.Asset{ID: custom0AssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: custom0Balance/2 - 1,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{addr},
							},
						},
					},
				}
				return &Tx{UnsignedAtomicTx: &validExportTx}
			}(),
			signers: [][]*crypto.PrivateKeySECP256K1R{
				{key},
				{key},
				{key},
			},
			baseFee:   initialBaseFee,
			rules:     apricotRulesPhase3,
			shouldErr: true,
		},
		{
			name: "not unique inputs",
			tx: func() *Tx {
				validExportTx := *validExportTx
				validExportTx.Ins = append([]EVMInput{}, validExportTx.Ins...)
				validExportTx.Ins[2] = validExportTx.Ins[1]
				return &Tx{UnsignedAtomicTx: &validExportTx}
			}(),
			signers: [][]*crypto.PrivateKeySECP256K1R{
				{key},
				{key},
				{key},
			},
			baseFee:   initialBaseFee,
			rules:     apricotRulesPhase3,
			shouldErr: true,
		},
		{
			name: "custom asset insufficient funds",
			tx: func() *Tx {
				validExportTx := *validExportTx
				validExportTx.ExportedOutputs = []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: custom0AssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: custom0Balance + 1,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{addr},
							},
						},
					},
				}
				return &Tx{UnsignedAtomicTx: &validExportTx}
			}(),
			signers: [][]*crypto.PrivateKeySECP256K1R{
				{key},
				{key},
				{key},
			},
			baseFee:   initialBaseFee,
			rules:     apricotRulesPhase3,
			shouldErr: true,
		},
		{
			name: "avax insufficient funds",
			tx: func() *Tx {
				validExportTx := *validExportTx
				validExportTx.ExportedOutputs = []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: avaxBalance, // after fees this should be too much
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{addr},
							},
						},
					},
				}
				return &Tx{UnsignedAtomicTx: &validExportTx}
			}(),
			signers: [][]*crypto.PrivateKeySECP256K1R{
				{key},
				{key},
				{key},
			},
			baseFee:   initialBaseFee,
			rules:     apricotRulesPhase3,
			shouldErr: true,
		},
	}
	for _, test := range tests {
		lastAccepted, err := vm.LastAccepted()
		if err != nil {
			t.Fatal(err)
		}

		if err := vm.SetPreference(lastAccepted); err != nil {
			t.Fatal(err)
		}

		if err := test.tx.Sign(vm.codec, test.signers); err != nil {
			t.Fatal(err)
		}

		t.Run(test.name, func(t *testing.T) {
			tx := test.tx
			exportTx := tx.UnsignedAtomicTx

			parent := vm.LastAcceptedBlockInternal().(*Block)
			err := exportTx.SemanticVerify(vm, tx, parent, test.baseFee, test.rules)
			if test.shouldErr && err == nil {
				t.Fatalf("should have errored but returned valid")
			}
			if !test.shouldErr && err != nil {
				t.Fatalf("shouldn't have errored but returned %s", err)
			}
		})
	}
}

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
		"simple export 1wei BaseFee": {
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
			ExpectedCost: 1230,
			ExpectedFee:  1,
			BaseFee:      big.NewInt(1),
		},
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
			ExpectedCost: 1230,
			ExpectedFee:  30750,
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
			ExpectedCost: 1230,
			ExpectedFee:  276750,
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
			ExpectedCost: 3366,
			ExpectedFee:  84150,
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
			ExpectedCost: 3366,
			ExpectedFee:  757350,
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

func TestNewExportTx(t *testing.T) {
	tests := []struct {
		name    string
		genesis string
		rules   params.Rules
		bal     uint64
	}{
		{
			name:    "apricot phase 0",
			genesis: genesisJSONApricotPhase0,
			rules:   apricotRulesPhase0,
			bal:     44000000,
		},
		{
			name:    "apricot phase 1",
			genesis: genesisJSONApricotPhase1,
			rules:   apricotRulesPhase1,
			bal:     44000000,
		},
		{
			name:    "apricot phase 2",
			genesis: genesisJSONApricotPhase2,
			rules:   apricotRulesPhase2,
			bal:     43000000,
		},
		{
			name:    "apricot phase 3",
			genesis: genesisJSONApricotPhase3,
			rules:   apricotRulesPhase3,
			bal:     44446500,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			issuer, vm, _, sharedMemory := GenesisVM(t, true, test.genesis, "", "")

			defer func() {
				if err := vm.Shutdown(); err != nil {
					t.Fatal(err)
				}
			}()

			parent := vm.LastAcceptedBlockInternal().(*Block)
			importAmount := uint64(50000000)
			utxoID := avax.UTXOID{
				TxID: ids.ID{
					0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
					0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
					0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
					0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
				},
			}

			utxo := &avax.UTXO{
				UTXOID: utxoID,
				Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: importAmount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{testKeys[0].PublicKey().Address()},
					},
				},
			}
			utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
			if err != nil {
				t.Fatal(err)
			}

			xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
			inputID := utxo.InputID()
			if err := xChainSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
				Key:   inputID[:],
				Value: utxoBytes,
				Traits: [][]byte{
					testKeys[0].PublicKey().Address().Bytes(),
				},
			}}}}); err != nil {
				t.Fatal(err)
			}

			tx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.issueTx(tx); err != nil {
				t.Fatal(err)
			}

			<-issuer

			blk, err := vm.BuildBlock()
			if err != nil {
				t.Fatal(err)
			}

			if err := blk.Verify(); err != nil {
				t.Fatal(err)
			}

			if err := vm.SetPreference(blk.ID()); err != nil {
				t.Fatal(err)
			}

			if err := blk.Accept(); err != nil {
				t.Fatal(err)
			}

			parent = vm.LastAcceptedBlockInternal().(*Block)
			exportAmount := uint64(5000000)

			testKeys0Addr := GetEthAddress(testKeys[0])
			exportId, err := ids.ToShortID(testKeys0Addr[:])
			if err != nil {
				t.Fatal(err)
			}

			tx, err = vm.newExportTx(vm.ctx.AVAXAssetID, exportAmount, vm.ctx.XChainID, exportId, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
			if err != nil {
				t.Fatal(err)
			}

			exportTx := tx.UnsignedAtomicTx

			if err := exportTx.SemanticVerify(vm, tx, parent, parent.ethBlock.BaseFee(), test.rules); err != nil {
				t.Fatal("newExportTx created an invalid transaction", err)
			}

			commitBatch, err := vm.db.CommitBatch()
			if err != nil {
				t.Fatalf("Failed to create commit batch for VM due to %s", err)
			}
			if err := exportTx.Accept(vm.ctx, commitBatch); err != nil {
				t.Fatalf("Failed to accept export transaction due to: %s", err)
			}

			stdb, err := vm.chain.CurrentState()
			if err != nil {
				t.Fatal(err)
			}
			err = exportTx.EVMStateTransfer(vm.ctx, stdb)
			if err != nil {
				t.Fatal(err)
			}

			addr := GetEthAddress(testKeys[0])
			if stdb.GetBalance(addr).Cmp(new(big.Int).SetUint64(test.bal*units.Avax)) != 0 {
				t.Fatalf("address balance %s equal %s not %s", addr.String(), stdb.GetBalance(addr), new(big.Int).SetUint64(test.bal*units.Avax))
			}
		})
	}
}
