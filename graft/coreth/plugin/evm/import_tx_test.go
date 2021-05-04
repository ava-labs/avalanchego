// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestImportTxVerifyNil(t *testing.T) {
	var importTx *UnsignedImportTx
	if err := importTx.Verify(testXChainID, NewContext(), testTxFee, testAvaxAssetID, apricotRulesPhase1); err == nil {
		t.Fatal("Verify should have failed due to nil transaction")
	}
}

func TestImportTxVerify(t *testing.T) {
	var importAmount uint64 = 10000000
	txID := ids.ID{0xff}
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
				Amount:  importAmount - txFee,
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

	// // Sort the inputs and outputs to ensure the transaction is canonical
	avax.SortTransferableInputs(importTx.ImportedInputs)
	SortEVMOutputs(importTx.Outs)

	// Test Valid ImportTx
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase1); err != nil {
		t.Fatalf("Failed to verify ImportTx: %s", err)
	}

	importTx.NetworkID = testNetworkID + 1

	// // Test Incorrect Network ID Errors
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase1); err == nil {
		t.Fatal("ImportTx should have failed verification due to incorrect network ID")
	}

	importTx.NetworkID = testNetworkID
	importTx.BlockchainID = nonExistentID
	// // Test Incorrect Blockchain ID Errors
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase1); err == nil {
		t.Fatal("ImportTx should have failed verification due to incorrect blockchain ID")
	}

	importTx.BlockchainID = testCChainID
	importTx.SourceChain = nonExistentID
	// // Test Incorrect Destination Chain ID Errors
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase1); err == nil {
		t.Fatal("ImportTx should have failed verification due to incorrect source chain")
	}

	importTx.SourceChain = testXChainID
	importedIns := importTx.ImportedInputs
	evmOutputs := importTx.Outs
	importTx.ImportedInputs = nil
	// // Test No Imported Inputs Errors
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase1); err == nil {
		t.Fatal("ImportTx should have failed verification due to no imported inputs")
	}

	importTx.ImportedInputs = []*avax.TransferableInput{importedIns[1], importedIns[0]}
	// // Test Unsorted Imported Inputs Errors
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase1); err == nil {
		t.Fatal("ImportTx should have failed verification due to unsorted import inputs")
	}

	importTx.ImportedInputs = []*avax.TransferableInput{importedIns[0], nil}
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase1); err == nil {
		t.Fatal("ImportTx should have failed verification due to invalid input")
	}

	importTx.ImportedInputs = []*avax.TransferableInput{importedIns[0], importedIns[1]}
	importTx.Outs = []EVMOutput{evmOutputs[1], evmOutputs[0]}
	// Test unsorted EVM Outputs pass verification prior to AP1
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase0); err != nil {
		t.Fatalf("ImportTx should have passed verification prior to AP1, but failed due to %s", err)
	}
	// Test unsorted EVM Outputs fails verification after AP1
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase1); err == nil {
		t.Fatal("ImportTx should have failed verification due to unsorted EVM Outputs in AP1")
	}
	importTx.Outs = []EVMOutput{
		{
			Address: testEthAddrs[0],
			Amount:  0,
			AssetID: testAvaxAssetID,
		},
	}
	// Test ImportTx with invalid EVM Output Amount 0 fails verification
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase1); err == nil {
		t.Fatal("ImportTx should have failed verification due to 0 value amount")
	}
	importTx.Outs = []EVMOutput{evmOutputs[0], evmOutputs[0]}
	// Test non-unique EVM Outputs passes verification for AP0 and AP1
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase0); err != nil {
		t.Fatal(err)
	}
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase1); err != nil {
		t.Fatal(err)
	}
	// Test non-unique EVM Outputs fails verification for AP2
	if err := importTx.Verify(testXChainID, ctx, testTxFee, testAvaxAssetID, apricotRulesPhase2); err == nil {
		t.Fatal("ImportTx should have failed verification due to non-unique outputs in AP2")
	}
}

func TestImportTxSemanticVerifyApricotPhase0(t *testing.T) {
	_, vm, _, sharedMemory := GenesisVM(t, false, genesisJSONApricotPhase0)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)

	importAmount := uint64(5000000)
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

	evmOutput := EVMOutput{
		Address: testEthAddrs[0],
		Amount:  importAmount,
		AssetID: vm.ctx.AVAXAssetID,
	}
	unsignedImportTx := &UnsignedImportTx{
		NetworkID:    vm.ctx.NetworkID,
		BlockchainID: vm.ctx.ChainID,
		SourceChain:  vm.ctx.XChainID,
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: utxoID,
			Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
			In: &secp256k1fx.TransferInput{
				Amt:   importAmount,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}},
		Outs: []EVMOutput{evmOutput},
	}

	state, err := vm.chain.CurrentState()
	if err != nil {
		t.Fatalf("Failed to get last accepted stateDB due to: %s", err)
	}

	if empty := state.Empty(testEthAddrs[0]); !empty {
		t.Fatal("Expected ethereum address to have empty starting balance.")
	}

	if err := unsignedImportTx.Verify(vm.ctx.XChainID, vm.ctx, vm.txFee, vm.ctx.AVAXAssetID, apricotRulesPhase0); err != nil {
		t.Fatal(err)
	}

	tx := &Tx{UnsignedAtomicTx: unsignedImportTx}

	// Sign with the correct key
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
		t.Fatal(err)
	}

	// Check that SemanticVerify passes without the UTXO being present during bootstrapping
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase0); err != nil {
		t.Fatal(err)
	}
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}); err != nil {
		t.Fatal(err)
	}

	// Check that SemanticVerify passes when the UTXO is present during bootstrapping
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase0); err != nil {
		t.Fatal(err)
	}

	// Check that SemanticVerify does not pass if an additional output is added in
	unsignedImportTx.Outs = append(unsignedImportTx.Outs, EVMOutput{
		Address: testEthAddrs[1],
		Amount:  importAmount,
		AssetID: vm.ctx.AVAXAssetID,
	})
	// Sign the updated transaction
	tx.Creds = nil
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
		t.Fatal(err)
	}
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase0); err == nil {
		t.Fatal("Semantic verification should have failed due to insufficient funds")
	}

	unsignedImportTx.Outs = []EVMOutput{evmOutput}
	// Sign the updated transaction
	tx.Creds = nil
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
		t.Fatal(err)
	}

	if err := vm.Bootstrapping(); err != nil {
		t.Fatal(err)
	}

	if err := vm.Bootstrapped(); err != nil {
		t.Fatal(err)
	}

	vm.ctx.Bootstrapped()

	// Remove the signature
	tx.Creds = nil
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase0); err == nil {
		t.Fatal("SemanticVerify should have failed due to no signatures")
	}

	// Sign with the incorrect key
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[1]}}); err != nil {
		t.Fatal(err)
	}
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase0); err == nil {
		t.Fatal("SemanticVerify should have failed due to an invalid signature")
	}

	// Re-sign with the correct key
	tx.Creds = nil
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
		t.Fatal(err)
	}

	// Check that SemanticVerify passes when the UTXO is present after bootstrapping
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase0); err != nil {
		t.Fatal(err)
	}

	if err := unsignedImportTx.Accept(vm.ctx, nil); err != nil {
		t.Fatalf("Accept failed due to: %s", err)
	}

	if err := unsignedImportTx.EVMStateTransfer(vm, state); err != nil {
		t.Fatalf("EVM State Transfer failed due to: %s", err)
	}

	balance := state.GetBalance(testEthAddrs[0])
	if balance == nil {
		t.Fatal("Found nil balance for address receiving imported funds")
	} else if balance.Uint64() != importAmount*x2cRate.Uint64() {
		t.Fatalf("Balance was %d, but expected balance of: %d", balance.Uint64(), importAmount*x2cRate.Uint64())
	}

	// Check that SemanticVerify fails when the UTXO is not present after bootstrapping
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase0); err == nil {
		t.Fatal("Semantic verification should have failed after the UTXO removed from shared memory")
	}
}

func TestImportTxSemanticVerifyApricotPhase2(t *testing.T) {
	_, vm, _, sharedMemory := GenesisVM(t, false, genesisJSONApricotPhase2)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)

	importAmount := uint64(5000000)
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

	evmOutput := EVMOutput{
		Address: testEthAddrs[0],
		Amount:  importAmount - txFee,
		AssetID: vm.ctx.AVAXAssetID,
	}
	unsignedImportTx := &UnsignedImportTx{
		NetworkID:    vm.ctx.NetworkID,
		BlockchainID: vm.ctx.ChainID,
		SourceChain:  vm.ctx.XChainID,
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: utxoID,
			Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
			In: &secp256k1fx.TransferInput{
				Amt:   importAmount,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}},
		Outs: []EVMOutput{evmOutput},
	}

	state, err := vm.chain.CurrentState()
	if err != nil {
		t.Fatalf("Failed to get last accepted stateDB due to: %s", err)
	}

	if empty := state.Empty(testEthAddrs[0]); !empty {
		t.Fatal("Expected ethereum address to have empty starting balance.")
	}

	if err := unsignedImportTx.Verify(vm.ctx.XChainID, vm.ctx, vm.txFee, vm.ctx.AVAXAssetID, apricotRulesPhase2); err != nil {
		t.Fatal(err)
	}

	tx := &Tx{UnsignedAtomicTx: unsignedImportTx}

	// Sign with the correct key
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
		t.Fatal(err)
	}

	// Check that SemanticVerify passes without the UTXO being present during bootstrapping
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase2); err != nil {
		t.Fatal(err)
	}
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}); err != nil {
		t.Fatal(err)
	}

	// Check that SemanticVerify passes when the UTXO is present during bootstrapping
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase2); err != nil {
		t.Fatal(err)
	}

	// Check that SemanticVerify does not pass if an additional output is added in
	unsignedImportTx.Outs = append(unsignedImportTx.Outs, EVMOutput{
		Address: testEthAddrs[1],
		Amount:  importAmount,
		AssetID: vm.ctx.AVAXAssetID,
	})
	// Sign the updated transaction
	tx.Creds = nil
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
		t.Fatal(err)
	}
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase2); err == nil {
		t.Fatal("Semantic verification should have failed due to insufficient funds")
	}

	// Check that SemanticVerify errors when the transaction fee is not paid
	unsignedImportTx.Outs = []EVMOutput{
		{
			Address: testEthAddrs[0],
			Amount:  importAmount,
			AssetID: vm.ctx.AVAXAssetID,
		},
	}
	tx.Creds = nil
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
		t.Fatal(err)
	}
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase2); err == nil {
		t.Fatal("Semantic verification should have failed due to not paying the transaction fee")
	}

	unsignedImportTx.Outs = []EVMOutput{evmOutput}
	// Sign the updated transaction
	tx.Creds = nil
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
		t.Fatal(err)
	}

	if err := vm.Bootstrapping(); err != nil {
		t.Fatal(err)
	}

	if err := vm.Bootstrapped(); err != nil {
		t.Fatal(err)
	}

	vm.ctx.Bootstrapped()

	// Remove the signature
	tx.Creds = nil
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase2); err == nil {
		t.Fatal("SemanticVerify should have failed due to no signatures")
	}

	// Sign with the incorrect key
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[1]}}); err != nil {
		t.Fatal(err)
	}
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase2); err == nil {
		t.Fatal("SemanticVerify should have failed due to an invalid signature")
	}

	// Re-sign with the correct key
	tx.Creds = nil
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
		t.Fatal(err)
	}

	// Check that SemanticVerify passes when the UTXO is present after bootstrapping
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase2); err != nil {
		t.Fatal(err)
	}

	if err := unsignedImportTx.Accept(vm.ctx, nil); err != nil {
		t.Fatalf("Accept failed due to: %s", err)
	}

	if err := unsignedImportTx.EVMStateTransfer(vm, state); err != nil {
		t.Fatalf("EVM State Transfer failed due to: %s", err)
	}

	balance := state.GetBalance(testEthAddrs[0])
	if balance == nil {
		t.Fatal("Found nil balance for address receiving imported funds")
	} else if balance.Uint64() != (importAmount-vm.txFee)*x2cRate.Uint64() {
		t.Fatalf("Balance was %d, but expected balance of: %d", balance.Uint64(), importAmount*x2cRate.Uint64())
	}

	// Check that SemanticVerify fails when the UTXO is not present after bootstrapping
	if err := unsignedImportTx.SemanticVerify(vm, tx, apricotRulesPhase2); err == nil {
		t.Fatal("Semantic verification should have failed after the UTXO removed from shared memory")
	}
}

func TestNewImportTx(t *testing.T) {
	tests := []struct {
		name    string
		genesis string
	}{
		{
			name:    "apricot phase 0",
			genesis: genesisJSONApricotPhase0,
		},
		{
			name:    "apricot phase 1",
			genesis: genesisJSONApricotPhase1,
		},
		{
			name:    "apricot phase 2",
			genesis: genesisJSONApricotPhase2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, vm, _, sharedMemory := GenesisVM(t, true, genesisJSONApricotPhase2)

			defer func() {
				if err := vm.Shutdown(); err != nil {
					t.Fatal(err)
				}
			}()

			importAmount := uint64(5000000)
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
			if err := xChainSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{{
				Key:   inputID[:],
				Value: utxoBytes,
				Traits: [][]byte{
					testKeys[0].PublicKey().Address().Bytes(),
				},
			}}); err != nil {
				t.Fatal(err)
			}

			tx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], []*crypto.PrivateKeySECP256K1R{testKeys[0]})
			if err != nil {
				t.Fatal(err)
			}

			importTx := tx.UnsignedAtomicTx

			if err := importTx.SemanticVerify(vm, tx, apricotRulesPhase2); err != nil {
				t.Fatal("newImportTx created an invalid transaction")
			}

			if err := importTx.Accept(vm.ctx, nil); err != nil {
				t.Fatalf("Failed to accept import transaction due to: %s", err)
			}
		})
	}
}
