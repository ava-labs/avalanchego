// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/plugin/evm/atomic"
	atomicvm "github.com/ava-labs/coreth/plugin/evm/atomic/vm"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/libevm/common"
	"github.com/holiman/uint256"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	avalancheutils "github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// createImportTxOptions adds a UTXO to shared memory and generates a list of import transactions sending this UTXO
// to each of the three test keys (conflicting transactions)
func createImportTxOptions(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) []*atomic.Tx {
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
		Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(50000000),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].Address()},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.Ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTxs := make([]*atomic.Tx, 0, 3)
	for _, ethAddr := range testEthAddrs {
		importTx, err := vm.NewImportTx(vm.Ctx.XChainID, ethAddr, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
		if err != nil {
			t.Fatal(err)
		}
		importTxs = append(importTxs, importTx)
	}

	return importTxs
}

func TestImportTxVerify(t *testing.T) {
	ctx := snowtest.Context(t, snowtest.CChainID)

	var importAmount uint64 = 10000000
	txID := ids.GenerateTestID()
	importTx := &atomic.UnsignedImportTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		SourceChain:  ctx.XChainID,
		ImportedInputs: []*avax.TransferableInput{
			{
				UTXOID: avax.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(0),
				},
				Asset: avax.Asset{ID: ctx.AVAXAssetID},
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
				Asset: avax.Asset{ID: ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: importAmount,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			},
		},
		Outs: []atomic.EVMOutput{
			{
				Address: testEthAddrs[0],
				Amount:  importAmount - ap0.AtomicTxFee,
				AssetID: ctx.AVAXAssetID,
			},
			{
				Address: testEthAddrs[1],
				Amount:  importAmount,
				AssetID: ctx.AVAXAssetID,
			},
		},
	}

	// Sort the inputs and outputs to ensure the transaction is canonical
	avalancheutils.Sort(importTx.ImportedInputs)
	avalancheutils.Sort(importTx.Outs)

	tests := map[string]atomicTxVerifyTest{
		"nil tx": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				var importTx *atomic.UnsignedImportTx
				return importTx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: atomic.ErrNilTx.Error(),
		},
		"valid import tx": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				return importTx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: "", // Expect this transaction to be valid in Apricot Phase 0
		},
		"valid import tx banff": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				return importTx
			},
			ctx:         ctx,
			rules:       banffRules,
			expectedErr: "", // Expect this transaction to be valid in Banff
		},
		"invalid network ID": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.NetworkID++
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: atomic.ErrWrongNetworkID.Error(),
		},
		"invalid blockchain ID": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.BlockchainID = ids.GenerateTestID()
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: atomic.ErrWrongChainID.Error(),
		},
		"P-chain source before AP5": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.SourceChain = constants.PlatformChainID
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: atomic.ErrWrongChainID.Error(),
		},
		"P-chain source after AP5": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.SourceChain = constants.PlatformChainID
				return &tx
			},
			ctx:   ctx,
			rules: apricotRulesPhase5,
		},
		"invalid source chain ID": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.SourceChain = ids.GenerateTestID()
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase5,
			expectedErr: atomic.ErrWrongChainID.Error(),
		},
		"no inputs": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.ImportedInputs = nil
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: atomic.ErrNoImportInputs.Error(),
		},
		"inputs sorted incorrectly": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.ImportedInputs = []*avax.TransferableInput{
					tx.ImportedInputs[1],
					tx.ImportedInputs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: atomic.ErrInputsNotSortedUnique.Error(),
		},
		"invalid input": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.ImportedInputs = []*avax.TransferableInput{
					tx.ImportedInputs[0],
					nil,
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: "atomic input failed verification",
		},
		"unsorted outputs phase 0 passes verification": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					tx.Outs[1],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: "",
		},
		"non-unique outputs phase 0 passes verification": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					tx.Outs[0],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: "",
		},
		"unsorted outputs phase 1 fails verification": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					tx.Outs[1],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase1,
			expectedErr: atomic.ErrOutputsNotSorted.Error(),
		},
		"non-unique outputs phase 1 passes verification": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					tx.Outs[0],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase1,
			expectedErr: "",
		},
		"outputs not sorted and unique phase 2 fails verification": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					tx.Outs[0],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase2,
			expectedErr: atomic.ErrOutputsNotSortedUnique.Error(),
		},
		"outputs not sorted phase 2 fails verification": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					tx.Outs[1],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase2,
			expectedErr: atomic.ErrOutputsNotSortedUnique.Error(),
		},
		"invalid EVMOutput fails verification": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					{
						Address: testEthAddrs[0],
						Amount:  0,
						AssetID: snowtest.AVAXAssetID,
					},
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: "EVM Output failed verification",
		},
		"no outputs apricot phase 3": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = nil
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase3,
			expectedErr: atomic.ErrNoEVMOutputs.Error(),
		},
		"non-AVAX input Apricot Phase 6": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.ImportedInputs = []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID:        txID,
							OutputIndex: uint32(0),
						},
						Asset: avax.Asset{ID: ids.GenerateTestID()},
						In: &secp256k1fx.TransferInput{
							Amt: importAmount,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase6,
			expectedErr: "",
		},
		"non-AVAX output Apricot Phase 6": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					{
						Address: importTx.Outs[0].Address,
						Amount:  importTx.Outs[0].Amount,
						AssetID: ids.GenerateTestID(),
					},
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase6,
			expectedErr: "",
		},
		"non-AVAX input Banff": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.ImportedInputs = []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID:        txID,
							OutputIndex: uint32(0),
						},
						Asset: avax.Asset{ID: ids.GenerateTestID()},
						In: &secp256k1fx.TransferInput{
							Amt: importAmount,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
				}
				return &tx
			},
			ctx:         ctx,
			rules:       banffRules,
			expectedErr: atomic.ErrImportNonAVAXInputBanff.Error(),
		},
		"non-AVAX output Banff": {
			generate: func(t *testing.T) atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					{
						Address: importTx.Outs[0].Address,
						Amount:  importTx.Outs[0].Amount,
						AssetID: ids.GenerateTestID(),
					},
				}
				return &tx
			},
			ctx:         ctx,
			rules:       banffRules,
			expectedErr: atomic.ErrImportNonAVAXOutputBanff.Error(),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			executeTxVerifyTest(t, test)
		})
	}
}

func TestNewImportTx(t *testing.T) {
	importAmount := uint64(5000000)
	// createNewImportAVAXTx adds a UTXO to shared memory and then constructs a new import transaction
	// and checks that it has the correct fee for the base fee that has been used
	createNewImportAVAXTx := func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
		txID := ids.GenerateTestID()
		_, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, vm.Ctx.AVAXAssetID, importAmount, testShortIDAddrs[0])
		if err != nil {
			t.Fatal(err)
		}

		tx, err := vm.NewImportTx(vm.Ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
		if err != nil {
			t.Fatal(err)
		}
		importTx := tx.UnsignedAtomicTx
		var actualFee uint64
		actualAVAXBurned, err := importTx.Burned(vm.Ctx.AVAXAssetID)
		if err != nil {
			t.Fatal(err)
		}
		rules := vm.CurrentRules()
		switch {
		case rules.IsApricotPhase3:
			actualCost, err := importTx.GasUsed(rules.IsApricotPhase5)
			if err != nil {
				t.Fatal(err)
			}
			actualFee, err = atomic.CalculateDynamicFee(actualCost, initialBaseFee)
			if err != nil {
				t.Fatal(err)
			}
		case rules.IsApricotPhase2:
			actualFee = 1000000
		default:
			actualFee = 0
		}

		if actualAVAXBurned != actualFee {
			t.Fatalf("AVAX burned (%d) != actual fee (%d)", actualAVAXBurned, actualFee)
		}

		return tx
	}
	checkState := func(t *testing.T, vm *atomicvm.VM) {
		txs := vm.LastAcceptedExtendedBlock().GetBlockExtension().(atomic.AtomicBlockContext).AtomicTxs()
		if len(txs) != 1 {
			t.Fatalf("Expected one import tx to be in the last accepted block, but found %d", len(txs))
		}

		tx := txs[0]
		actualAVAXBurned, err := tx.UnsignedAtomicTx.Burned(vm.Ctx.AVAXAssetID)
		if err != nil {
			t.Fatal(err)
		}

		// Ensure that the UTXO has been removed from shared memory within Accept
		addrSet := set.Set[ids.ShortID]{}
		addrSet.Add(testShortIDAddrs[0])
		utxos, _, _, err := avax.GetAtomicUTXOs(vm.Ctx.SharedMemory, atomic.Codec, vm.Ctx.XChainID, addrSet, ids.ShortEmpty, ids.Empty, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(utxos) != 0 {
			t.Fatalf("Expected to find 0 UTXOs after accepting import transaction, but found %d", len(utxos))
		}

		// Ensure that the call to EVMStateTransfer correctly updates the balance of [addr]
		sdb, err := vm.Blockchain().State()
		if err != nil {
			t.Fatal(err)
		}

		expectedRemainingBalance := new(uint256.Int).Mul(
			uint256.NewInt(importAmount-actualAVAXBurned), atomic.X2CRate)
		addr := testKeys[0].EthAddress()
		if actualBalance := sdb.GetBalance(addr); actualBalance.Cmp(expectedRemainingBalance) != 0 {
			t.Fatalf("address remaining balance %s equal %s not %s", addr.String(), actualBalance, expectedRemainingBalance)
		}
	}
	tests2 := map[string]atomicTxTest{
		"apricot phase 0": {
			setup:      createNewImportAVAXTx,
			checkState: checkState,
			fork:       upgradetest.NoUpgrades,
		},
		"apricot phase 1": {
			setup:      createNewImportAVAXTx,
			checkState: checkState,
			fork:       upgradetest.ApricotPhase1,
		},
		"apricot phase 2": {
			setup:      createNewImportAVAXTx,
			checkState: checkState,
			fork:       upgradetest.ApricotPhase2,
		},
		"apricot phase 3": {
			setup:      createNewImportAVAXTx,
			checkState: checkState,
			fork:       upgradetest.ApricotPhase3,
		},
	}

	for name, test := range tests2 {
		t.Run(name, func(t *testing.T) {
			executeTxTest(t, test)
		})
	}
}

// Note: this is a brittle test to ensure that the gas cost of a transaction does
// not change
func TestImportTxGasCost(t *testing.T) {
	avaxAssetID := ids.GenerateTestID()
	antAssetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()
	xChainID := ids.GenerateTestID()
	networkID := uint32(5)
	importAmount := uint64(5000000)

	tests := map[string]struct {
		UnsignedImportTx *atomic.UnsignedImportTx
		Keys             [][]*secp256k1.PrivateKey

		ExpectedGasUsed uint64
		ExpectedFee     uint64
		BaseFee         *big.Int
		FixedFee        bool
	}{
		"simple import": {
			UnsignedImportTx: &atomic.UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  xChainID,
				ImportedInputs: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
					Asset:  avax.Asset{ID: avaxAssetID},
					In: &secp256k1fx.TransferInput{
						Amt:   importAmount,
						Input: secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				}},
				Outs: []atomic.EVMOutput{{
					Address: testEthAddrs[0],
					Amount:  importAmount,
					AssetID: avaxAssetID,
				}},
			},
			Keys:            [][]*secp256k1.PrivateKey{{testKeys[0]}},
			ExpectedGasUsed: 1230,
			ExpectedFee:     30750,
			BaseFee:         big.NewInt(25 * utils.GWei),
		},
		"simple import 1wei": {
			UnsignedImportTx: &atomic.UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  xChainID,
				ImportedInputs: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
					Asset:  avax.Asset{ID: avaxAssetID},
					In: &secp256k1fx.TransferInput{
						Amt:   importAmount,
						Input: secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				}},
				Outs: []atomic.EVMOutput{{
					Address: testEthAddrs[0],
					Amount:  importAmount,
					AssetID: avaxAssetID,
				}},
			},
			Keys:            [][]*secp256k1.PrivateKey{{testKeys[0]}},
			ExpectedGasUsed: 1230,
			ExpectedFee:     1,
			BaseFee:         big.NewInt(1),
		},
		"simple import 1wei + fixed fee": {
			UnsignedImportTx: &atomic.UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  xChainID,
				ImportedInputs: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
					Asset:  avax.Asset{ID: avaxAssetID},
					In: &secp256k1fx.TransferInput{
						Amt:   importAmount,
						Input: secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				}},
				Outs: []atomic.EVMOutput{{
					Address: testEthAddrs[0],
					Amount:  importAmount,
					AssetID: avaxAssetID,
				}},
			},
			Keys:            [][]*secp256k1.PrivateKey{{testKeys[0]}},
			ExpectedGasUsed: 11230,
			ExpectedFee:     1,
			BaseFee:         big.NewInt(1),
			FixedFee:        true,
		},
		"simple ANT import": {
			UnsignedImportTx: &atomic.UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  xChainID,
				ImportedInputs: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: antAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
				},
				Outs: []atomic.EVMOutput{
					{
						Address: testEthAddrs[0],
						Amount:  importAmount,
						AssetID: antAssetID,
					},
				},
			},
			Keys:            [][]*secp256k1.PrivateKey{{testKeys[0]}, {testKeys[0]}},
			ExpectedGasUsed: 2318,
			ExpectedFee:     57950,
			BaseFee:         big.NewInt(25 * utils.GWei),
		},
		"complex ANT import": {
			UnsignedImportTx: &atomic.UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  xChainID,
				ImportedInputs: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: antAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
				},
				Outs: []atomic.EVMOutput{
					{
						Address: testEthAddrs[0],
						Amount:  importAmount,
						AssetID: avaxAssetID,
					},
					{
						Address: testEthAddrs[0],
						Amount:  importAmount,
						AssetID: antAssetID,
					},
				},
			},
			Keys:            [][]*secp256k1.PrivateKey{{testKeys[0]}, {testKeys[0]}},
			ExpectedGasUsed: 2378,
			ExpectedFee:     59450,
			BaseFee:         big.NewInt(25 * utils.GWei),
		},
		"multisig import": {
			UnsignedImportTx: &atomic.UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  xChainID,
				ImportedInputs: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
					Asset:  avax.Asset{ID: avaxAssetID},
					In: &secp256k1fx.TransferInput{
						Amt:   importAmount,
						Input: secp256k1fx.Input{SigIndices: []uint32{0, 1}},
					},
				}},
				Outs: []atomic.EVMOutput{{
					Address: testEthAddrs[0],
					Amount:  importAmount,
					AssetID: avaxAssetID,
				}},
			},
			Keys:            [][]*secp256k1.PrivateKey{{testKeys[0], testKeys[1]}},
			ExpectedGasUsed: 2234,
			ExpectedFee:     55850,
			BaseFee:         big.NewInt(25 * utils.GWei),
		},
		"large import": {
			UnsignedImportTx: &atomic.UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  xChainID,
				ImportedInputs: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  avax.Asset{ID: avaxAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
				},
				Outs: []atomic.EVMOutput{
					{
						Address: testEthAddrs[0],
						Amount:  importAmount * 10,
						AssetID: avaxAssetID,
					},
				},
			},
			Keys: [][]*secp256k1.PrivateKey{
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
			},
			ExpectedGasUsed: 11022,
			ExpectedFee:     275550,
			BaseFee:         big.NewInt(25 * utils.GWei),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			tx := &atomic.Tx{UnsignedAtomicTx: test.UnsignedImportTx}

			// Sign with the correct key
			if err := tx.Sign(atomic.Codec, test.Keys); err != nil {
				t.Fatal(err)
			}

			gasUsed, err := tx.GasUsed(test.FixedFee)
			if err != nil {
				t.Fatal(err)
			}
			if gasUsed != test.ExpectedGasUsed {
				t.Fatalf("Expected gasUsed to be %d, but found %d", test.ExpectedGasUsed, gasUsed)
			}

			fee, err := atomic.CalculateDynamicFee(gasUsed, test.BaseFee)
			if err != nil {
				t.Fatal(err)
			}
			if fee != test.ExpectedFee {
				t.Fatalf("Expected fee to be %d, but found %d", test.ExpectedFee, fee)
			}
		})
	}
}

func TestImportTxSemanticVerify(t *testing.T) {
	tests := map[string]atomicTxTest{
		"UTXO not present during bootstrapping": {
			setup: func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				tx := &atomic.Tx{UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
					SourceChain:  vm.Ctx.XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: avax.UTXOID{
							TxID: ids.GenerateTestID(),
						},
						Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []atomic.EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			bootstrapping: true,
		},
		"UTXO not present": {
			setup: func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				tx := &atomic.Tx{UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
					SourceChain:  vm.Ctx.XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: avax.UTXOID{
							TxID: ids.GenerateTestID(),
						},
						Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []atomic.EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: "failed to fetch import UTXOs from",
		},
		"garbage UTXO": {
			setup: func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}
				xChainSharedMemory := sharedMemory.NewSharedMemory(vm.Ctx.XChainID)
				inputID := utxoID.InputID()
				if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
					Key:   inputID[:],
					Value: []byte("hey there"),
					Traits: [][]byte{
						testShortIDAddrs[0].Bytes(),
					},
				}}}}); err != nil {
					t.Fatal(err)
				}

				tx := &atomic.Tx{UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
					SourceChain:  vm.Ctx.XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: utxoID,
						Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []atomic.EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: "failed to unmarshal UTXO",
		},
		"UTXO AssetID mismatch": {
			setup: func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				expectedAssetID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, expectedAssetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &atomic.Tx{UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
					SourceChain:  vm.Ctx.XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID}, // Use a different assetID then the actual UTXO
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []atomic.EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: atomicvm.ErrAssetIDMismatch.Error(),
		},
		"insufficient AVAX funds": {
			setup: func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, vm.Ctx.AVAXAssetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &atomic.Tx{UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
					SourceChain:  vm.Ctx.XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []atomic.EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  2, // Produce more output than is consumed by the transaction
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: "import tx flow check failed due to",
		},
		"insufficient non-AVAX funds": {
			setup: func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				assetID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, assetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &atomic.Tx{UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
					SourceChain:  vm.Ctx.XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  avax.Asset{ID: assetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []atomic.EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  2, // Produce more output than is consumed by the transaction
						AssetID: assetID,
					}},
				}}
				if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: "import tx flow check failed due to",
		},
		"no signatures": {
			setup: func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, vm.Ctx.AVAXAssetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &atomic.Tx{UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
					SourceChain:  vm.Ctx.XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []atomic.EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				if err := tx.Sign(atomic.Codec, nil); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: "import tx contained mismatched number of inputs/credentials",
		},
		"incorrect signature": {
			setup: func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, vm.Ctx.AVAXAssetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &atomic.Tx{UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
					SourceChain:  vm.Ctx.XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []atomic.EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				// Sign the transaction with the incorrect key
				if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{testKeys[1]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: "import tx transfer failed verification",
		},
		"non-unique EVM Outputs": {
			setup: func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, vm.Ctx.AVAXAssetID, 2, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &atomic.Tx{UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
					SourceChain:  vm.Ctx.XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   2,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []atomic.EVMOutput{
						{
							Address: testEthAddrs[0],
							Amount:  1,
							AssetID: vm.Ctx.AVAXAssetID,
						},
						{
							Address: testEthAddrs[0],
							Amount:  1,
							AssetID: vm.Ctx.AVAXAssetID,
						},
					},
				}}
				if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			fork:              upgradetest.ApricotPhase3,
			semanticVerifyErr: atomic.ErrOutputsNotSortedUnique.Error(),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			executeTxTest(t, test)
		})
	}
}

func TestImportTxEVMStateTransfer(t *testing.T) {
	assetID := ids.GenerateTestID()
	tests := map[string]atomicTxTest{
		"AVAX UTXO": {
			setup: func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, vm.Ctx.AVAXAssetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &atomic.Tx{UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
					SourceChain:  vm.Ctx.XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []atomic.EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			checkState: func(t *testing.T, vm *atomicvm.VM) {
				lastAcceptedBlock := vm.LastAcceptedExtendedBlock()

				sdb, err := vm.Blockchain().StateAt(lastAcceptedBlock.GetEthBlock().Root())
				if err != nil {
					t.Fatal(err)
				}

				avaxBalance := sdb.GetBalance(testEthAddrs[0])
				if avaxBalance.Cmp(atomic.X2CRate) != 0 {
					t.Fatalf("Expected AVAX balance to be %d, found balance: %d", *atomic.X2CRate, avaxBalance)
				}
			},
		},
		"non-AVAX UTXO": {
			setup: func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, assetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &atomic.Tx{UnsignedAtomicTx: &atomic.UnsignedImportTx{
					NetworkID:    vm.Ctx.NetworkID,
					BlockchainID: vm.Ctx.ChainID,
					SourceChain:  vm.Ctx.XChainID,
					ImportedInputs: []*avax.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  avax.Asset{ID: assetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []atomic.EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: assetID,
					}},
				}}
				if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			checkState: func(t *testing.T, vm *atomicvm.VM) {
				lastAcceptedBlock := vm.LastAcceptedExtendedBlock()

				sdb, err := vm.Blockchain().StateAt(lastAcceptedBlock.GetEthBlock().Root())
				if err != nil {
					t.Fatal(err)
				}

				assetBalance := sdb.GetBalanceMultiCoin(testEthAddrs[0], common.Hash(assetID))
				if assetBalance.Cmp(common.Big1) != 0 {
					t.Fatalf("Expected asset balance to be %d, found balance: %d", common.Big1, assetBalance)
				}
				avaxBalance := sdb.GetBalance(testEthAddrs[0])
				if avaxBalance.Cmp(common.U2560) != 0 {
					t.Fatalf("Expected AVAX balance to be 0, found balance: %d", avaxBalance)
				}
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			executeTxTest(t, test)
		})
	}
}
