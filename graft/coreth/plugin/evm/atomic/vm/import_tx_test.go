// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras/extrastest"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/avalanchego/graft/coreth/utils"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheutils "github.com/ava-labs/avalanchego/utils"
)

// createImportTxOptions adds a UTXO to shared memory and generates a list of import transactions sending this UTXO
// to each of the three test keys (conflicting transactions)
func createImportTxOptions(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) []*atomic.Tx {
	_, err := addUTXO(sharedMemory, vm.Ctx, ids.GenerateTestID(), 0, vm.Ctx.AVAXAssetID, 50000000, vmtest.TestKeys[0].Address())
	require.NoError(t, err)

	importTxs := make([]*atomic.Tx, 0, 3)
	for _, ethAddr := range vmtest.TestEthAddrs {
		importTx, err := vm.newImportTx(vm.Ctx.XChainID, ethAddr, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{vmtest.TestKeys[0]})
		require.NoError(t, err)
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
				Address: vmtest.TestEthAddrs[0],
				Amount:  importAmount - ap0.AtomicTxFee,
				AssetID: ctx.AVAXAssetID,
			},
			{
				Address: vmtest.TestEthAddrs[1],
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
			generate: func() atomic.UnsignedAtomicTx {
				var importTx *atomic.UnsignedImportTx
				return importTx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrNilTx,
		},
		"valid import tx": {
			generate: func() atomic.UnsignedAtomicTx {
				return importTx
			},
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.NoUpgrades),
		},
		"valid import tx banff": {
			generate: func() atomic.UnsignedAtomicTx {
				return importTx
			},
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.Banff),
		},
		"invalid network ID": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.NetworkID++
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrWrongNetworkID,
		},
		"invalid blockchain ID": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.BlockchainID = ids.GenerateTestID()
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrWrongChainID,
		},
		"P-chain source before AP5": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.SourceChain = constants.PlatformChainID
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrWrongChainID,
		},
		"P-chain source after AP5": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.SourceChain = constants.PlatformChainID
				return &tx
			},
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.ApricotPhase5),
		},
		"invalid source chain ID": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.SourceChain = ids.GenerateTestID()
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.ApricotPhase5),
			expectedErr: atomic.ErrWrongChainID,
		},
		"no inputs": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.ImportedInputs = nil
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrNoImportInputs,
		},
		"inputs sorted incorrectly": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.ImportedInputs = []*avax.TransferableInput{
					tx.ImportedInputs[1],
					tx.ImportedInputs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrInputsNotSortedUnique,
		},
		"invalid input": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.ImportedInputs = []*avax.TransferableInput{
					tx.ImportedInputs[0],
					nil,
				}
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: avax.ErrNilTransferableInput,
		},
		"unsorted outputs phase 0 passes verification": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					tx.Outs[1],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.NoUpgrades),
		},
		"non-unique outputs phase 0 passes verification": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					tx.Outs[0],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.NoUpgrades),
		},
		"unsorted outputs phase 1 fails verification": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					tx.Outs[1],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.ApricotPhase1),
			expectedErr: atomic.ErrOutputsNotSorted,
		},
		"non-unique outputs phase 1 passes verification": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					tx.Outs[0],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.ApricotPhase1),
		},
		"outputs not sorted and unique phase 2 fails verification": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					tx.Outs[0],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.ApricotPhase2),
			expectedErr: atomic.ErrOutputsNotSortedUnique,
		},
		"outputs not sorted phase 2 fails verification": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					tx.Outs[1],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.ApricotPhase2),
			expectedErr: atomic.ErrOutputsNotSortedUnique,
		},
		"invalid EVMOutput fails verification": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []atomic.EVMOutput{
					{
						Address: vmtest.TestEthAddrs[0],
						Amount:  0,
						AssetID: snowtest.AVAXAssetID,
					},
				}
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrNoValueOutput,
		},
		"no outputs apricot phase 3": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = nil
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.ApricotPhase3),
			expectedErr: atomic.ErrNoEVMOutputs,
		},
		"non-AVAX input Apricot Phase 6": {
			generate: func() atomic.UnsignedAtomicTx {
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
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.ApricotPhase6),
		},
		"non-AVAX output Apricot Phase 6": {
			generate: func() atomic.UnsignedAtomicTx {
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
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.ApricotPhase6),
		},
		"non-AVAX input Banff": {
			generate: func() atomic.UnsignedAtomicTx {
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
			rules:       extrastest.ForkToRules(upgradetest.Banff),
			expectedErr: atomic.ErrImportNonAVAXInputBanff,
		},
		"non-AVAX output Banff": {
			generate: func() atomic.UnsignedAtomicTx {
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
			rules:       extrastest.ForkToRules(upgradetest.Banff),
			expectedErr: atomic.ErrImportNonAVAXOutputBanff,
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
	key, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)
	ethAddress := key.EthAddress()
	// createNewImportAVAXTx adds a UTXO to shared memory and then constructs a new import transaction
	// and checks that it has the correct fee for the base fee that has been used
	createNewImportAVAXTx := func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
		txID := ids.GenerateTestID()
		_, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, vm.Ctx.AVAXAssetID, importAmount, key.Address())
		require.NoError(t, err)

		tx, err := vm.newImportTx(vm.Ctx.XChainID, ethAddress, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
		require.NoError(t, err)
		importTx := tx.UnsignedAtomicTx
		var actualFee uint64
		actualAVAXBurned, err := importTx.Burned(vm.Ctx.AVAXAssetID)
		require.NoError(t, err)
		rules := vm.CurrentRules()
		switch {
		case rules.IsApricotPhase3:
			actualCost, err := importTx.GasUsed(rules.IsApricotPhase5)
			require.NoError(t, err)
			actualFee, err = atomic.CalculateDynamicFee(actualCost, vmtest.InitialBaseFee)
			require.NoError(t, err)
		case rules.IsApricotPhase2:
			actualFee = 1000000
		default:
			actualFee = 0
		}

		require.Equalf(t, actualFee, actualAVAXBurned, "AVAX burned (%d) != actual fee (%d)", actualAVAXBurned, actualFee)

		return tx
	}
	checkState := func(t *testing.T, vm *VM) {
		blk := vm.LastAcceptedExtendedBlock()
		blockExtension, ok := blk.GetBlockExtension().(atomic.AtomicBlockContext)
		require.True(t, ok)
		txs := blockExtension.AtomicTxs()
		require.Lenf(t, txs, 1, "Expected one import tx to be in the last accepted block, but found %d", len(txs))

		tx := txs[0]
		actualAVAXBurned, err := tx.UnsignedAtomicTx.Burned(vm.Ctx.AVAXAssetID)
		require.NoError(t, err)

		// Ensure that the UTXO has been removed from shared memory within Accept
		addrSet := set.Set[ids.ShortID]{}
		addrSet.Add(key.Address())
		utxos, _, _, err := avax.GetAtomicUTXOs(vm.Ctx.SharedMemory, atomic.Codec, vm.Ctx.XChainID, addrSet, ids.ShortEmpty, ids.Empty, maxUTXOsToFetch)
		require.NoError(t, err)
		require.Emptyf(t, utxos, "Expected to find 0 UTXOs after accepting import transaction, but found %d", len(utxos))

		// Ensure that the call to EVMStateTransfer correctly updates the balance of [addr]
		statedb, err := vm.Ethereum().BlockChain().State()
		require.NoError(t, err)

		expectedRemainingBalance := new(uint256.Int).Mul(uint256.NewInt(importAmount-actualAVAXBurned), atomic.X2CRate)
		actualBalance := statedb.GetBalance(ethAddress)
		require.Zerof(t, actualBalance.Cmp(expectedRemainingBalance), "address remaining balance %s equal %s not %s", ethAddress.String(), actualBalance, expectedRemainingBalance)
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
					Address: vmtest.TestEthAddrs[0],
					Amount:  importAmount,
					AssetID: avaxAssetID,
				}},
			},
			Keys:            [][]*secp256k1.PrivateKey{{vmtest.TestKeys[0]}},
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
					Address: vmtest.TestEthAddrs[0],
					Amount:  importAmount,
					AssetID: avaxAssetID,
				}},
			},
			Keys:            [][]*secp256k1.PrivateKey{{vmtest.TestKeys[0]}},
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
					Address: vmtest.TestEthAddrs[0],
					Amount:  importAmount,
					AssetID: avaxAssetID,
				}},
			},
			Keys:            [][]*secp256k1.PrivateKey{{vmtest.TestKeys[0]}},
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
						Address: vmtest.TestEthAddrs[0],
						Amount:  importAmount,
						AssetID: antAssetID,
					},
				},
			},
			Keys:            [][]*secp256k1.PrivateKey{{vmtest.TestKeys[0]}, {vmtest.TestKeys[0]}},
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
						Address: vmtest.TestEthAddrs[0],
						Amount:  importAmount,
						AssetID: avaxAssetID,
					},
					{
						Address: vmtest.TestEthAddrs[0],
						Amount:  importAmount,
						AssetID: antAssetID,
					},
				},
			},
			Keys:            [][]*secp256k1.PrivateKey{{vmtest.TestKeys[0]}, {vmtest.TestKeys[0]}},
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
					Address: vmtest.TestEthAddrs[0],
					Amount:  importAmount,
					AssetID: avaxAssetID,
				}},
			},
			Keys:            [][]*secp256k1.PrivateKey{{vmtest.TestKeys[0], vmtest.TestKeys[1]}},
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
						Address: vmtest.TestEthAddrs[0],
						Amount:  importAmount * 10,
						AssetID: avaxAssetID,
					},
				},
			},
			Keys: [][]*secp256k1.PrivateKey{
				{vmtest.TestKeys[0]},
				{vmtest.TestKeys[0]},
				{vmtest.TestKeys[0]},
				{vmtest.TestKeys[0]},
				{vmtest.TestKeys[0]},
				{vmtest.TestKeys[0]},
				{vmtest.TestKeys[0]},
				{vmtest.TestKeys[0]},
				{vmtest.TestKeys[0]},
				{vmtest.TestKeys[0]},
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
			require.NoError(t, tx.Sign(atomic.Codec, test.Keys))

			gasUsed, err := tx.GasUsed(test.FixedFee)
			require.NoError(t, err)
			require.Equalf(t, test.ExpectedGasUsed, gasUsed, "Expected gasUsed to be %d, but found %d", test.ExpectedGasUsed, gasUsed)

			fee, err := atomic.CalculateDynamicFee(gasUsed, test.BaseFee)
			require.NoError(t, err)
			require.Equalf(t, test.ExpectedFee, fee, "Expected fee to be %d, but found %d", test.ExpectedFee, fee)
		})
	}
}

func TestImportTxSemanticVerify(t *testing.T) {
	key, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)
	ethAddress := key.PublicKey().EthAddress()
	tests := map[string]atomicTxTest{
		"UTXO not present during bootstrapping": {
			setup: func(t *testing.T, vm *VM, _ *avalancheatomic.Memory) *atomic.Tx {
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
						Address: ethAddress,
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				require.NoError(t, tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key}}))
				return tx
			},
			bootstrapping: true,
		},
		"UTXO not present": {
			setup: func(t *testing.T, vm *VM, _ *avalancheatomic.Memory) *atomic.Tx {
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
						Address: ethAddress,
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				require.NoError(t, tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key}}))
				return tx
			},
			semanticVerifyErr: errFailedToFetchImportUTXOs,
		},
		"garbage UTXO": {
			setup: func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}
				xChainSharedMemory := sharedMemory.NewSharedMemory(vm.Ctx.XChainID)
				inputID := utxoID.InputID()
				require.NoError(t, xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
					Key:   inputID[:],
					Value: []byte("hey there"),
					Traits: [][]byte{
						key.Address().Bytes(),
					},
				}}}}))

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
						Address: ethAddress,
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				require.NoError(t, tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key}}))
				return tx
			},
			semanticVerifyErr: errFailedToUnmarshalUTXO,
		},
		"UTXO AssetID mismatch": {
			setup: func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				expectedAssetID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, expectedAssetID, 1, key.Address())
				require.NoError(t, err)

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
						Address: ethAddress,
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				require.NoError(t, tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key}}))
				return tx
			},
			semanticVerifyErr: ErrAssetIDMismatch,
		},
		"insufficient AVAX funds": {
			setup: func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, vm.Ctx.AVAXAssetID, 1, key.Address())
				require.NoError(t, err)

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
						Address: ethAddress,
						Amount:  2, // Produce more output than is consumed by the transaction
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				require.NoError(t, tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key}}))
				return tx
			},
			semanticVerifyErr: avax.ErrInsufficientFunds,
		},
		"insufficient non-AVAX funds": {
			setup: func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				assetID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, assetID, 1, key.Address())
				require.NoError(t, err)

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
						Address: ethAddress,
						Amount:  2, // Produce more output than is consumed by the transaction
						AssetID: assetID,
					}},
				}}
				require.NoError(t, tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key}}))
				return tx
			},
			semanticVerifyErr: avax.ErrInsufficientFunds,
		},
		"no signatures": {
			setup: func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, vm.Ctx.AVAXAssetID, 1, key.Address())
				require.NoError(t, err)

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
						Address: ethAddress,
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				require.NoError(t, tx.Sign(atomic.Codec, nil))
				return tx
			},
			semanticVerifyErr: errIncorrectNumCredentials,
		},
		"incorrect signature": {
			setup: func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, vm.Ctx.AVAXAssetID, 1, key.Address())
				require.NoError(t, err)

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
						Address: ethAddress,
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				incorrectKey, err := secp256k1.NewPrivateKey()
				require.NoError(t, err)
				// Sign the transaction with the incorrect key
				require.NoError(t, tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{incorrectKey}}))
				return tx
			},
			semanticVerifyErr: secp256k1fx.ErrWrongSig,
		},
		"non-unique EVM Outputs": {
			setup: func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, vm.Ctx.AVAXAssetID, 2, key.Address())
				require.NoError(t, err)

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
							Address: ethAddress,
							Amount:  1,
							AssetID: vm.Ctx.AVAXAssetID,
						},
						{
							Address: ethAddress,
							Amount:  1,
							AssetID: vm.Ctx.AVAXAssetID,
						},
					},
				}}
				require.NoError(t, tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key}}))
				return tx
			},
			fork:              upgradetest.ApricotPhase3,
			semanticVerifyErr: atomic.ErrOutputsNotSortedUnique,
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
	key, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)
	ethAddress := key.EthAddress()
	tests := map[string]atomicTxTest{
		"AVAX UTXO": {
			setup: func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, vm.Ctx.AVAXAssetID, 1, key.Address())
				require.NoError(t, err)

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
						Address: ethAddress,
						Amount:  1,
						AssetID: vm.Ctx.AVAXAssetID,
					}},
				}}
				require.NoError(t, tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key}}))
				return tx
			},
			checkState: func(t *testing.T, vm *VM) {
				lastAcceptedBlock := vm.LastAcceptedExtendedBlock()

				statedb, err := vm.Ethereum().BlockChain().StateAt(lastAcceptedBlock.GetEthBlock().Root())
				require.NoError(t, err)

				avaxBalance := statedb.GetBalance(ethAddress)
				require.Zerof(t, avaxBalance.Cmp(atomic.X2CRate), "Expected AVAX balance to be %d, found balance: %d", *atomic.X2CRate, avaxBalance)
			},
		},
		"non-AVAX UTXO": {
			setup: func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.Ctx, txID, 0, assetID, 1, key.Address())
				require.NoError(t, err)

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
						Address: ethAddress,
						Amount:  1,
						AssetID: assetID,
					}},
				}}
				require.NoError(t, tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key}}))
				return tx
			},
			checkState: func(t *testing.T, vm *VM) {
				lastAcceptedBlock := vm.LastAcceptedExtendedBlock()

				statedb, err := vm.Ethereum().BlockChain().StateAt(lastAcceptedBlock.GetEthBlock().Root())
				require.NoError(t, err)

				wrappedStateDB := extstate.New(statedb)
				assetBalance := wrappedStateDB.GetBalanceMultiCoin(ethAddress, common.Hash(assetID))
				require.Equalf(t, 0, assetBalance.Cmp(common.Big1), "Expected asset balance to be %d, found balance: %d", common.Big1, assetBalance)
				avaxBalance := wrappedStateDB.GetBalance(ethAddress)
				require.Zerof(t, avaxBalance.Cmp(uint256.NewInt(0)), "Expected AVAX balance to be 0, found balance: %d", avaxBalance)
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			executeTxTest(t, test)
		})
	}
}

func executeTxTest(t *testing.T, test atomicTxTest) {
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		IsSyncing: test.bootstrapping,
		Fork:      &test.fork,
	})
	rules := vm.CurrentRules()

	tx := test.setup(t, vm, tvm.AtomicMemory)

	var baseFee *big.Int
	// If ApricotPhase3 is active, use the initial base fee for the atomic transaction
	if rules.IsApricotPhase3 {
		baseFee = vmtest.InitialBaseFee
	}

	lastAcceptedBlock := vm.LastAcceptedExtendedBlock()
	backend := NewVerifierBackend(vm, rules)

	err := backend.SemanticVerify(tx, lastAcceptedBlock, baseFee)
	require.ErrorIs(t, err, test.semanticVerifyErr)
	if test.semanticVerifyErr != nil {
		// If SemanticVerify failed for the expected reason, return early
		return
	}

	// Retrieve dummy state to test that EVMStateTransfer works correctly
	statedb, err := vm.Ethereum().BlockChain().StateAt(lastAcceptedBlock.GetEthBlock().Root())
	require.NoError(t, err)
	wrappedStateDB := extstate.New(statedb)
	err = tx.UnsignedAtomicTx.EVMStateTransfer(vm.Ctx, wrappedStateDB)
	require.ErrorIs(t, err, test.evmStateTransferErr)
	if test.evmStateTransferErr != nil {
		// If EVMStateTransfer failed for the expected reason, return early
		return
	}

	require.NoError(t, vm.AtomicMempool.AddLocalTx(tx))

	if test.bootstrapping {
		return
	}
	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	// If we've reached this point, we expect to be able to build and verify the block without any errors
	blk, err := vm.BuildBlock(t.Context())
	require.NoError(t, err)

	require.NoError(t, blk.Verify(t.Context()))

	err = blk.Accept(t.Context())
	require.ErrorIs(t, err, test.acceptErr)

	if test.acceptErr != nil {
		// If Accept failed for the expected reason, return early
		return
	}

	if test.checkState != nil {
		test.checkState(t, vm)
	}
}
