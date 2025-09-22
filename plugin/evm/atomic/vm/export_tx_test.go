// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/libevm/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/core/extstate"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/coreth/utils"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheutils "github.com/ava-labs/avalanchego/utils"
)

// createExportTxOptions adds funds to shared memory, imports them, and returns a list of export transactions
// that attempt to send the funds to each of the test keys (list of length 3).
func createExportTxOptions(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) []*atomic.Tx {
	key, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)
	ethAddr := key.EthAddress()

	// Add a UTXO to shared memory
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
		Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(50000000),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{key.Address()},
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
			key.Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	// Import the funds
	importTx, err := vm.newImportTx(vm.Ctx.XChainID, ethAddr, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	msg, err := vm.WaitForEvent(context.Background())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	statedb, err := vm.Ethereum().BlockChain().State()
	if err != nil {
		t.Fatal(err)
	}

	// Use the funds to create 3 conflicting export transactions sending the funds to each of the test addresses
	exportTxs := make([]*atomic.Tx, 0, 3)
	wrappedStateDB := extstate.New(statedb)
	for _, addr := range vmtest.TestShortIDAddrs {
		exportTx, err := atomic.NewExportTx(vm.Ctx, vm.CurrentRules(), wrappedStateDB, vm.Ctx.AVAXAssetID, uint64(5000000), vm.Ctx.XChainID, addr, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{vmtest.TestKeys[0]})
		if err != nil {
			t.Fatal(err)
		}
		exportTxs = append(exportTxs, exportTx)
	}

	return exportTxs
}

func TestExportTxEVMStateTransfer(t *testing.T) {
	key, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)
	addr := key.Address()
	ethAddr := key.EthAddress()

	avaxAmount := 50 * units.MilliAvax
	avaxUTXOID := avax.UTXOID{
		OutputIndex: 0,
	}
	avaxInputID := avaxUTXOID.InputID()

	customAmount := uint64(100)
	customAssetID := ids.ID{1, 2, 3, 4, 5, 7}
	customUTXOID := avax.UTXOID{
		OutputIndex: 1,
	}
	customInputID := customUTXOID.InputID()

	customUTXO := &avax.UTXO{
		UTXOID: customUTXOID,
		Asset:  avax.Asset{ID: customAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: customAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		},
	}

	tests := []struct {
		name          string
		tx            []atomic.EVMInput
		avaxBalance   *uint256.Int
		balances      map[ids.ID]*big.Int
		expectedNonce uint64
		shouldErr     bool
	}{
		{
			name:        "no transfers",
			tx:          nil,
			avaxBalance: uint256.NewInt(avaxAmount * atomic.X2CRateUint64),
			balances: map[ids.ID]*big.Int{
				customAssetID: big.NewInt(int64(customAmount)),
			},
			expectedNonce: 0,
			shouldErr:     false,
		},
		{
			name: "spend half AVAX",
			tx: []atomic.EVMInput{
				{
					Address: ethAddr,
					Amount:  avaxAmount / 2,
					AssetID: snowtest.AVAXAssetID,
					Nonce:   0,
				},
			},
			avaxBalance: uint256.NewInt(avaxAmount / 2 * atomic.X2CRateUint64),
			balances: map[ids.ID]*big.Int{
				customAssetID: big.NewInt(int64(customAmount)),
			},
			expectedNonce: 1,
			shouldErr:     false,
		},
		{
			name: "spend all AVAX",
			tx: []atomic.EVMInput{
				{
					Address: ethAddr,
					Amount:  avaxAmount,
					AssetID: snowtest.AVAXAssetID,
					Nonce:   0,
				},
			},
			avaxBalance: uint256.NewInt(0),
			balances: map[ids.ID]*big.Int{
				customAssetID: big.NewInt(int64(customAmount)),
			},
			expectedNonce: 1,
			shouldErr:     false,
		},
		{
			name: "spend too much AVAX",
			tx: []atomic.EVMInput{
				{
					Address: ethAddr,
					Amount:  avaxAmount + 1,
					AssetID: snowtest.AVAXAssetID,
					Nonce:   0,
				},
			},
			avaxBalance: uint256.NewInt(0),
			balances: map[ids.ID]*big.Int{
				customAssetID: big.NewInt(int64(customAmount)),
			},
			expectedNonce: 1,
			shouldErr:     true,
		},
		{
			name: "spend half custom",
			tx: []atomic.EVMInput{
				{
					Address: ethAddr,
					Amount:  customAmount / 2,
					AssetID: customAssetID,
					Nonce:   0,
				},
			},
			avaxBalance: uint256.NewInt(avaxAmount * atomic.X2CRateUint64),
			balances: map[ids.ID]*big.Int{
				customAssetID: big.NewInt(int64(customAmount / 2)),
			},
			expectedNonce: 1,
			shouldErr:     false,
		},
		{
			name: "spend all custom",
			tx: []atomic.EVMInput{
				{
					Address: ethAddr,
					Amount:  customAmount,
					AssetID: customAssetID,
					Nonce:   0,
				},
			},
			avaxBalance: uint256.NewInt(avaxAmount * atomic.X2CRateUint64),
			balances: map[ids.ID]*big.Int{
				customAssetID: big.NewInt(0),
			},
			expectedNonce: 1,
			shouldErr:     false,
		},
		{
			name: "spend too much custom",
			tx: []atomic.EVMInput{
				{
					Address: ethAddr,
					Amount:  customAmount + 1,
					AssetID: customAssetID,
					Nonce:   0,
				},
			},
			avaxBalance: uint256.NewInt(avaxAmount * atomic.X2CRateUint64),
			balances: map[ids.ID]*big.Int{
				customAssetID: big.NewInt(0),
			},
			expectedNonce: 1,
			shouldErr:     true,
		},
		{
			name: "spend everything",
			tx: []atomic.EVMInput{
				{
					Address: ethAddr,
					Amount:  customAmount,
					AssetID: customAssetID,
					Nonce:   0,
				},
				{
					Address: ethAddr,
					Amount:  avaxAmount,
					AssetID: snowtest.AVAXAssetID,
					Nonce:   0,
				},
			},
			avaxBalance: uint256.NewInt(0),
			balances: map[ids.ID]*big.Int{
				customAssetID: big.NewInt(0),
			},
			expectedNonce: 1,
			shouldErr:     false,
		},
		{
			name: "spend everything wrong nonce",
			tx: []atomic.EVMInput{
				{
					Address: ethAddr,
					Amount:  customAmount,
					AssetID: customAssetID,
					Nonce:   1,
				},
				{
					Address: ethAddr,
					Amount:  avaxAmount,
					AssetID: snowtest.AVAXAssetID,
					Nonce:   1,
				},
			},
			avaxBalance: uint256.NewInt(0),
			balances: map[ids.ID]*big.Int{
				customAssetID: big.NewInt(0),
			},
			expectedNonce: 1,
			shouldErr:     true,
		},
		{
			name: "spend everything changing nonces",
			tx: []atomic.EVMInput{
				{
					Address: ethAddr,
					Amount:  customAmount,
					AssetID: customAssetID,
					Nonce:   0,
				},
				{
					Address: ethAddr,
					Amount:  avaxAmount,
					AssetID: snowtest.AVAXAssetID,
					Nonce:   1,
				},
			},
			avaxBalance: uint256.NewInt(0),
			balances: map[ids.ID]*big.Int{
				customAssetID: big.NewInt(0),
			},
			expectedNonce: 1,
			shouldErr:     true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fork := upgradetest.NoUpgrades
			vm := newAtomicTestVM()
			tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork: &fork,
			})
			defer func() {
				if err := vm.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
			}()

			avaxUTXO := &avax.UTXO{
				UTXOID: avaxUTXOID,
				Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: avaxAmount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr},
					},
				},
			}

			avaxUTXOBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, avaxUTXO)
			if err != nil {
				t.Fatal(err)
			}

			customUTXOBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, customUTXO)
			if err != nil {
				t.Fatal(err)
			}

			xChainSharedMemory := tvm.AtomicMemory.NewSharedMemory(vm.Ctx.XChainID)
			if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.ChainID: {PutRequests: []*avalancheatomic.Element{
				{
					Key:   avaxInputID[:],
					Value: avaxUTXOBytes,
					Traits: [][]byte{
						addr.Bytes(),
					},
				},
				{
					Key:   customInputID[:],
					Value: customUTXOBytes,
					Traits: [][]byte{
						addr.Bytes(),
					},
				},
			}}}); err != nil {
				t.Fatal(err)
			}

			tx, err := vm.newImportTx(vm.Ctx.XChainID, ethAddr, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.AtomicMempool.AddLocalTx(tx); err != nil {
				t.Fatal(err)
			}

			msg, err := tvm.VM.WaitForEvent(context.Background())
			require.NoError(t, err)
			require.Equal(t, commonEng.PendingTxs, msg)

			blk, err := vm.BuildBlock(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			if err := blk.Verify(context.Background()); err != nil {
				t.Fatal(err)
			}

			if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
				t.Fatal(err)
			}

			if err := blk.Accept(context.Background()); err != nil {
				t.Fatal(err)
			}

			newTx := atomic.UnsignedExportTx{
				Ins: test.tx,
			}

			statedb, err := vm.Ethereum().BlockChain().State()
			if err != nil {
				t.Fatal(err)
			}

			wrappedStateDB := extstate.New(statedb)
			err = newTx.EVMStateTransfer(vm.Ctx, wrappedStateDB)
			if test.shouldErr {
				if err == nil {
					t.Fatal("expected EVMStateTransfer to fail")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			avaxBalance := wrappedStateDB.GetBalance(ethAddr)
			if avaxBalance.Cmp(test.avaxBalance) != 0 {
				t.Fatalf("address balance %s equal %s not %s", addr.String(), avaxBalance, test.avaxBalance)
			}

			for assetID, expectedBalance := range test.balances {
				balance := wrappedStateDB.GetBalanceMultiCoin(ethAddr, common.Hash(assetID))
				if avaxBalance.Cmp(test.avaxBalance) != 0 {
					t.Fatalf("%s address balance %s equal %s not %s", assetID, addr.String(), balance, expectedBalance)
				}
			}

			if wrappedStateDB.GetNonce(ethAddr) != test.expectedNonce {
				t.Fatalf("failed to set nonce to %d", test.expectedNonce)
			}
		})
	}
}

func TestExportTxSemanticVerify(t *testing.T) {
	fork := upgradetest.NoUpgrades
	vm := newAtomicTestVM()
	vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork: &fork,
	})
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	parent := vm.LastAcceptedExtendedBlock()

	key := vmtest.TestKeys[0]
	addr := key.Address()
	ethAddr := vmtest.TestEthAddrs[0]

	var (
		avaxBalance           = 10 * units.Avax
		custom0Balance uint64 = 100
		custom0AssetID        = ids.ID{1, 2, 3, 4, 5}
		custom1Balance uint64 = 1000
		custom1AssetID        = ids.ID{1, 2, 3, 4, 5, 6}
	)

	validExportTx := &atomic.UnsignedExportTx{
		NetworkID:        vm.Ctx.NetworkID,
		BlockchainID:     vm.Ctx.ChainID,
		DestinationChain: vm.Ctx.XChainID,
		Ins: []atomic.EVMInput{
			{
				Address: ethAddr,
				Amount:  avaxBalance,
				AssetID: vm.Ctx.AVAXAssetID,
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
	avalancheutils.Sort(validExportTx.Ins)

	validAVAXExportTx := &atomic.UnsignedExportTx{
		NetworkID:        vm.Ctx.NetworkID,
		BlockchainID:     vm.Ctx.ChainID,
		DestinationChain: vm.Ctx.XChainID,
		Ins: []atomic.EVMInput{
			{
				Address: ethAddr,
				Amount:  avaxBalance,
				AssetID: vm.Ctx.AVAXAssetID,
				Nonce:   0,
			},
		},
		ExportedOutputs: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: avaxBalance / 2,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr},
					},
				},
			},
		},
	}

	tests := []struct {
		name    string
		tx      *atomic.UnsignedExportTx
		signers [][]*secp256k1.PrivateKey
		fork    upgradetest.Fork
		wantErr error
	}{
		{
			name: "valid",
			tx:   validExportTx,
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork: upgradetest.ApricotPhase3,
		},
		{
			name: "P-chain before AP5",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validAVAXExportTx
				tx.DestinationChain = constants.PlatformChainID
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: atomic.ErrWrongChainID,
		},
		{
			name: "P-chain after AP5",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validAVAXExportTx
				tx.DestinationChain = constants.PlatformChainID
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
			},
			fork: upgradetest.ApricotPhase5,
		},
		{
			name: "random chain after AP5",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validAVAXExportTx
				tx.DestinationChain = ids.GenerateTestID()
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
			},
			fork:    upgradetest.ApricotPhase5,
			wantErr: atomic.ErrWrongChainID,
		},
		{
			name: "P-chain multi-coin before AP5",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validExportTx
				tx.DestinationChain = constants.PlatformChainID
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: atomic.ErrWrongChainID,
		},
		{
			name: "P-chain multi-coin after AP5",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validExportTx
				tx.DestinationChain = constants.PlatformChainID
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase5,
			wantErr: atomic.ErrWrongChainID,
		},
		{
			name: "random chain multi-coin after AP5",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validExportTx
				tx.DestinationChain = ids.GenerateTestID()
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase5,
			wantErr: atomic.ErrWrongChainID,
		},
		{
			name: "no outputs",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validExportTx
				tx.ExportedOutputs = nil
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: atomic.ErrNoExportOutputs,
		},
		{
			name: "wrong networkID",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validExportTx
				tx.NetworkID++
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: atomic.ErrWrongNetworkID,
		},
		{
			name: "wrong chainID",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validExportTx
				tx.BlockchainID = ids.GenerateTestID()
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: atomic.ErrWrongChainID,
		},
		{
			name: "invalid input",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validExportTx
				tx.Ins = append([]atomic.EVMInput{}, tx.Ins...)
				tx.Ins[2].Amount = 0
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: atomic.ErrNoValueInput,
		},
		{
			name: "invalid output",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validExportTx
				tx.ExportedOutputs = []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: custom0AssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: custom0Balance,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 0,
							Addrs:     []ids.ShortID{addr},
						},
					},
				}}
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: secp256k1fx.ErrOutputUnoptimized,
		},
		{
			name: "unsorted outputs",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validExportTx
				exportOutputs := []*avax.TransferableOutput{
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
				// Sort the outputs and then swap the ordering to ensure that they are ordered incorrectly
				avax.SortTransferableOutputs(exportOutputs, atomic.Codec)
				exportOutputs[0], exportOutputs[1] = exportOutputs[1], exportOutputs[0]
				tx.ExportedOutputs = exportOutputs
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: atomic.ErrOutputsNotSorted,
		},
		{
			name: "not unique inputs",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validExportTx
				tx.Ins = append([]atomic.EVMInput{}, tx.Ins...)
				tx.Ins[2] = tx.Ins[1]
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: atomic.ErrInputsNotSortedUnique,
		},
		{
			name: "custom asset insufficient funds",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validExportTx
				tx.ExportedOutputs = []*avax.TransferableOutput{
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
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: avax.ErrInsufficientFunds,
		},
		{
			name: "avax insufficient funds",
			tx: func() *atomic.UnsignedExportTx {
				tx := *validExportTx
				tx.ExportedOutputs = []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: avaxBalance, // after fees this should be too much
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{addr},
							},
						},
					},
				}
				return &tx
			}(),
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: avax.ErrInsufficientFunds,
		},
		{
			name: "too many signatures",
			tx:   validExportTx,
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: errIncorrectNumCredentials,
		},
		{
			name: "too few signatures",
			tx:   validExportTx,
			signers: [][]*secp256k1.PrivateKey{
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: errIncorrectNumCredentials,
		},
		{
			name: "too many signatures on credential",
			tx:   validExportTx,
			signers: [][]*secp256k1.PrivateKey{
				{key, vmtest.TestKeys[1]},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: errIncorrectNumSignatures,
		},
		{
			name: "too few signatures on credential",
			tx:   validExportTx,
			signers: [][]*secp256k1.PrivateKey{
				{},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: errIncorrectNumSignatures,
		},
		{
			name: "wrong signature on credential",
			tx:   validExportTx,
			signers: [][]*secp256k1.PrivateKey{
				{vmtest.TestKeys[1]},
				{key},
				{key},
			},
			fork:    upgradetest.ApricotPhase3,
			wantErr: errPublicKeySignatureMismatch,
		},
		{
			name:    "no signatures",
			tx:      validExportTx,
			signers: [][]*secp256k1.PrivateKey{},
			fork:    upgradetest.ApricotPhase3,
			wantErr: errIncorrectNumCredentials,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := &atomic.Tx{UnsignedAtomicTx: test.tx}
			require.NoError(t, tx.Sign(atomic.Codec, test.signers))

			rules := vmtest.ForkToRules(test.fork)
			backend := NewVerifierBackend(vm, *rules)

			err := backend.SemanticVerify(tx, parent, vmtest.InitialBaseFee)
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestExportTxAccept(t *testing.T) {
	fork := upgradetest.NoUpgrades
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork: &fork,
	})
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	xChainSharedMemory := tvm.AtomicMemory.NewSharedMemory(vm.Ctx.XChainID)

	key := vmtest.TestKeys[0]
	addr := key.Address()
	ethAddr := vmtest.TestEthAddrs[0]

	var (
		avaxBalance           = 10 * units.Avax
		custom0Balance uint64 = 100
		custom0AssetID        = ids.ID{1, 2, 3, 4, 5}
	)

	exportTx := &atomic.UnsignedExportTx{
		NetworkID:        vm.Ctx.NetworkID,
		BlockchainID:     vm.Ctx.ChainID,
		DestinationChain: vm.Ctx.XChainID,
		Ins: []atomic.EVMInput{
			{
				Address: ethAddr,
				Amount:  avaxBalance,
				AssetID: vm.Ctx.AVAXAssetID,
				Nonce:   0,
			},
			{
				Address: ethAddr,
				Amount:  custom0Balance,
				AssetID: custom0AssetID,
				Nonce:   0,
			},
		},
		ExportedOutputs: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: avaxBalance,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr},
					},
				},
			},
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

	tx := &atomic.Tx{UnsignedAtomicTx: exportTx}

	signers := [][]*secp256k1.PrivateKey{
		{key},
		{key},
		{key},
	}

	if err := tx.Sign(atomic.Codec, signers); err != nil {
		t.Fatal(err)
	}

	commitBatch, err := vm.VersionDB().CommitBatch()
	if err != nil {
		t.Fatalf("Failed to create commit batch for VM due to %s", err)
	}
	chainID, atomicRequests, err := tx.AtomicOps()
	if err != nil {
		t.Fatalf("Failed to accept export transaction due to: %s", err)
	}

	if err := vm.Ctx.SharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{chainID: atomicRequests}, commitBatch); err != nil {
		t.Fatal(err)
	}
	indexedValues, _, _, err := xChainSharedMemory.Indexed(vm.Ctx.ChainID, [][]byte{addr.Bytes()}, nil, nil, 3)
	if err != nil {
		t.Fatal(err)
	}

	if len(indexedValues) != 2 {
		t.Fatalf("expected 2 values but got %d", len(indexedValues))
	}

	avaxUTXOID := avax.UTXOID{
		TxID:        tx.ID(),
		OutputIndex: 0,
	}
	avaxInputID := avaxUTXOID.InputID()

	customUTXOID := avax.UTXOID{
		TxID:        tx.ID(),
		OutputIndex: 1,
	}
	customInputID := customUTXOID.InputID()

	fetchedValues, err := xChainSharedMemory.Get(vm.Ctx.ChainID, [][]byte{
		customInputID[:],
		avaxInputID[:],
	})
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(fetchedValues[0], indexedValues[0]) {
		t.Fatalf("inconsistent values returned fetched %x indexed %x", fetchedValues[0], indexedValues[0])
	}
	if !bytes.Equal(fetchedValues[1], indexedValues[1]) {
		t.Fatalf("inconsistent values returned fetched %x indexed %x", fetchedValues[1], indexedValues[1])
	}

	customUTXOBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, &avax.UTXO{
		UTXOID: customUTXOID,
		Asset:  avax.Asset{ID: custom0AssetID},
		Out:    exportTx.ExportedOutputs[1].Out,
	})
	if err != nil {
		t.Fatal(err)
	}

	avaxUTXOBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, &avax.UTXO{
		UTXOID: avaxUTXOID,
		Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
		Out:    exportTx.ExportedOutputs[0].Out,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(fetchedValues[0], customUTXOBytes) {
		t.Fatalf("incorrect values returned expected %x got %x", customUTXOBytes, fetchedValues[0])
	}
	if !bytes.Equal(fetchedValues[1], avaxUTXOBytes) {
		t.Fatalf("incorrect values returned expected %x got %x", avaxUTXOBytes, fetchedValues[1])
	}
}

func TestExportTxVerify(t *testing.T) {
	var exportAmount uint64 = 10000000
	exportTx := &atomic.UnsignedExportTx{
		NetworkID:        constants.UnitTestID,
		BlockchainID:     snowtest.CChainID,
		DestinationChain: snowtest.XChainID,
		Ins: []atomic.EVMInput{
			{
				Address: vmtest.TestEthAddrs[0],
				Amount:  exportAmount,
				AssetID: snowtest.AVAXAssetID,
				Nonce:   0,
			},
			{
				Address: vmtest.TestEthAddrs[2],
				Amount:  exportAmount,
				AssetID: snowtest.AVAXAssetID,
				Nonce:   0,
			},
		},
		ExportedOutputs: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: snowtest.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: exportAmount,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{vmtest.TestShortIDAddrs[0]},
					},
				},
			},
			{
				Asset: avax.Asset{ID: snowtest.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: exportAmount,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{vmtest.TestShortIDAddrs[1]},
					},
				},
			},
		},
	}

	// Sort the inputs and outputs to ensure the transaction is canonical
	avax.SortTransferableOutputs(exportTx.ExportedOutputs, atomic.Codec)
	// Pass in a list of signers here with the appropriate length
	// to avoid causing a nil-pointer error in the helper method
	emptySigners := make([][]*secp256k1.PrivateKey, 2)
	atomic.SortEVMInputsAndSigners(exportTx.Ins, emptySigners)

	ctx := snowtest.Context(t, snowtest.CChainID)

	tests := map[string]atomicTxVerifyTest{
		"nil tx": {
			generate: func() atomic.UnsignedAtomicTx {
				return (*atomic.UnsignedExportTx)(nil)
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrNilTx.Error(),
		},
		"valid export tx": {
			generate: func() atomic.UnsignedAtomicTx {
				return exportTx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: "",
		},
		"valid export tx banff": {
			generate: func() atomic.UnsignedAtomicTx {
				return exportTx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.Banff),
			expectedErr: "",
		},
		"incorrect networkID": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.NetworkID++
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrWrongNetworkID.Error(),
		},
		"incorrect blockchainID": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.BlockchainID = ids.GenerateTestID()
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrWrongChainID.Error(),
		},
		"incorrect destination chain": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.DestinationChain = ids.GenerateTestID()
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrWrongChainID.Error(), // TODO make this error more specific to destination not just chainID
		},
		"no exported outputs": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.ExportedOutputs = nil
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrNoExportOutputs.Error(),
		},
		"unsorted outputs": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.ExportedOutputs = []*avax.TransferableOutput{
					tx.ExportedOutputs[1],
					tx.ExportedOutputs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrOutputsNotSorted.Error(),
		},
		"invalid exported output": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.ExportedOutputs = []*avax.TransferableOutput{tx.ExportedOutputs[0], nil}
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: "nil transferable output is not valid",
		},
		"unsorted EVM inputs before AP1": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.Ins = []atomic.EVMInput{
					tx.Ins[1],
					tx.Ins[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: "",
		},
		"unsorted EVM inputs after AP1": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.Ins = []atomic.EVMInput{
					tx.Ins[1],
					tx.Ins[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.ApricotPhase1),
			expectedErr: atomic.ErrInputsNotSortedUnique.Error(),
		},
		"EVM input with amount 0": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.Ins = []atomic.EVMInput{
					{
						Address: vmtest.TestEthAddrs[0],
						Amount:  0,
						AssetID: snowtest.AVAXAssetID,
						Nonce:   0,
					},
				}
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrNoValueInput.Error(),
		},
		"non-unique EVM input before AP1": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.Ins = []atomic.EVMInput{tx.Ins[0], tx.Ins[0]}
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: "",
		},
		"non-unique EVM input after AP1": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.Ins = []atomic.EVMInput{tx.Ins[0], tx.Ins[0]}
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.ApricotPhase1),
			expectedErr: atomic.ErrInputsNotSortedUnique.Error(),
		},
		"non-AVAX input Apricot Phase 6": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.Ins = []atomic.EVMInput{
					{
						Address: vmtest.TestEthAddrs[0],
						Amount:  1,
						AssetID: ids.GenerateTestID(),
						Nonce:   0,
					},
				}
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.ApricotPhase6),
			expectedErr: "",
		},
		"non-AVAX output Apricot Phase 6": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.ExportedOutputs = []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: ids.GenerateTestID()},
						Out: &secp256k1fx.TransferOutput{
							Amt: exportAmount,
							OutputOwners: secp256k1fx.OutputOwners{
								Locktime:  0,
								Threshold: 1,
								Addrs:     []ids.ShortID{vmtest.TestShortIDAddrs[0]},
							},
						},
					},
				}
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.ApricotPhase6),
			expectedErr: "",
		},
		"non-AVAX input Banff": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.Ins = []atomic.EVMInput{
					{
						Address: vmtest.TestEthAddrs[0],
						Amount:  1,
						AssetID: ids.GenerateTestID(),
						Nonce:   0,
					},
				}
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.Banff),
			expectedErr: atomic.ErrExportNonAVAXInputBanff.Error(),
		},
		"non-AVAX output Banff": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.ExportedOutputs = []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: ids.GenerateTestID()},
						Out: &secp256k1fx.TransferOutput{
							Amt: exportAmount,
							OutputOwners: secp256k1fx.OutputOwners{
								Locktime:  0,
								Threshold: 1,
								Addrs:     []ids.ShortID{vmtest.TestShortIDAddrs[0]},
							},
						},
					},
				}
				return &tx
			},
			ctx:         ctx,
			rules:       vmtest.ForkToRules(upgradetest.Banff),
			expectedErr: atomic.ErrExportNonAVAXOutputBanff.Error(),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			executeTxVerifyTest(t, test)
		})
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
		UnsignedExportTx *atomic.UnsignedExportTx
		Keys             [][]*secp256k1.PrivateKey

		BaseFee         *big.Int
		ExpectedGasUsed uint64
		ExpectedFee     uint64
		FixedFee        bool
	}{
		"simple export 1wei BaseFee": {
			UnsignedExportTx: &atomic.UnsignedExportTx{
				NetworkID:        networkID,
				BlockchainID:     chainID,
				DestinationChain: xChainID,
				Ins: []atomic.EVMInput{
					{
						Address: vmtest.TestEthAddrs[0],
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
								Addrs:     []ids.ShortID{vmtest.TestShortIDAddrs[0]},
							},
						},
					},
				},
			},
			Keys:            [][]*secp256k1.PrivateKey{{vmtest.TestKeys[0]}},
			ExpectedGasUsed: 1230,
			ExpectedFee:     1,
			BaseFee:         big.NewInt(1),
		},
		"simple export 1wei BaseFee + fixed fee": {
			UnsignedExportTx: &atomic.UnsignedExportTx{
				NetworkID:        networkID,
				BlockchainID:     chainID,
				DestinationChain: xChainID,
				Ins: []atomic.EVMInput{
					{
						Address: vmtest.TestEthAddrs[0],
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
								Addrs:     []ids.ShortID{vmtest.TestShortIDAddrs[0]},
							},
						},
					},
				},
			},
			Keys:            [][]*secp256k1.PrivateKey{{vmtest.TestKeys[0]}},
			ExpectedGasUsed: 11230,
			ExpectedFee:     1,
			BaseFee:         big.NewInt(1),
			FixedFee:        true,
		},
		"simple export 25Gwei BaseFee": {
			UnsignedExportTx: &atomic.UnsignedExportTx{
				NetworkID:        networkID,
				BlockchainID:     chainID,
				DestinationChain: xChainID,
				Ins: []atomic.EVMInput{
					{
						Address: vmtest.TestEthAddrs[0],
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
								Addrs:     []ids.ShortID{vmtest.TestShortIDAddrs[0]},
							},
						},
					},
				},
			},
			Keys:            [][]*secp256k1.PrivateKey{{vmtest.TestKeys[0]}},
			ExpectedGasUsed: 1230,
			ExpectedFee:     30750,
			BaseFee:         big.NewInt(25 * utils.GWei),
		},
		"simple export 225Gwei BaseFee": {
			UnsignedExportTx: &atomic.UnsignedExportTx{
				NetworkID:        networkID,
				BlockchainID:     chainID,
				DestinationChain: xChainID,
				Ins: []atomic.EVMInput{
					{
						Address: vmtest.TestEthAddrs[0],
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
								Addrs:     []ids.ShortID{vmtest.TestShortIDAddrs[0]},
							},
						},
					},
				},
			},
			Keys:            [][]*secp256k1.PrivateKey{{vmtest.TestKeys[0]}},
			ExpectedGasUsed: 1230,
			ExpectedFee:     276750,
			BaseFee:         big.NewInt(225 * utils.GWei),
		},
		"complex export 25Gwei BaseFee": {
			UnsignedExportTx: &atomic.UnsignedExportTx{
				NetworkID:        networkID,
				BlockchainID:     chainID,
				DestinationChain: xChainID,
				Ins: []atomic.EVMInput{
					{
						Address: vmtest.TestEthAddrs[0],
						Amount:  exportAmount,
						AssetID: avaxAssetID,
						Nonce:   0,
					},
					{
						Address: vmtest.TestEthAddrs[1],
						Amount:  exportAmount,
						AssetID: avaxAssetID,
						Nonce:   0,
					},
					{
						Address: vmtest.TestEthAddrs[2],
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
								Addrs:     []ids.ShortID{vmtest.TestShortIDAddrs[0]},
							},
						},
					},
				},
			},
			Keys:            [][]*secp256k1.PrivateKey{{vmtest.TestKeys[0], vmtest.TestKeys[0], vmtest.TestKeys[0]}},
			ExpectedGasUsed: 3366,
			ExpectedFee:     84150,
			BaseFee:         big.NewInt(25 * utils.GWei),
		},
		"complex export 225Gwei BaseFee": {
			UnsignedExportTx: &atomic.UnsignedExportTx{
				NetworkID:        networkID,
				BlockchainID:     chainID,
				DestinationChain: xChainID,
				Ins: []atomic.EVMInput{
					{
						Address: vmtest.TestEthAddrs[0],
						Amount:  exportAmount,
						AssetID: avaxAssetID,
						Nonce:   0,
					},
					{
						Address: vmtest.TestEthAddrs[1],
						Amount:  exportAmount,
						AssetID: avaxAssetID,
						Nonce:   0,
					},
					{
						Address: vmtest.TestEthAddrs[2],
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
								Addrs:     []ids.ShortID{vmtest.TestShortIDAddrs[0]},
							},
						},
					},
				},
			},
			Keys:            [][]*secp256k1.PrivateKey{{vmtest.TestKeys[0], vmtest.TestKeys[0], vmtest.TestKeys[0]}},
			ExpectedGasUsed: 3366,
			ExpectedFee:     757350,
			BaseFee:         big.NewInt(225 * utils.GWei),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			tx := &atomic.Tx{UnsignedAtomicTx: test.UnsignedExportTx}

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

func TestNewExportTx(t *testing.T) {
	key, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)
	ethAddress := key.PublicKey().EthAddress()
	tests := []struct {
		fork               upgradetest.Fork
		bal                uint64
		expectedBurnedAVAX uint64
	}{
		{
			fork:               upgradetest.NoUpgrades,
			bal:                44000000,
			expectedBurnedAVAX: 1000000,
		},
		{
			fork:               upgradetest.ApricotPhase1,
			bal:                44000000,
			expectedBurnedAVAX: 1000000,
		},
		{
			fork:               upgradetest.ApricotPhase2,
			bal:                43000000,
			expectedBurnedAVAX: 1000000,
		},
		{
			fork:               upgradetest.ApricotPhase3,
			bal:                44446500,
			expectedBurnedAVAX: 276750,
		},
		{
			fork:               upgradetest.ApricotPhase4,
			bal:                44446500,
			expectedBurnedAVAX: 276750,
		},
		{
			fork:               upgradetest.ApricotPhase5,
			bal:                39946500,
			expectedBurnedAVAX: 2526750,
		},
	}
	for _, test := range tests {
		t.Run(test.fork.String(), func(t *testing.T) {
			vm := newAtomicTestVM()
			tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork: &test.fork,
			})
			defer func() {
				if err := vm.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
			}()

			importAmount := uint64(50000000)
			utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

			utxo := &avax.UTXO{
				UTXOID: utxoID,
				Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: importAmount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{key.Address()},
					},
				},
			}
			utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
			if err != nil {
				t.Fatal(err)
			}

			xChainSharedMemory := tvm.AtomicMemory.NewSharedMemory(vm.Ctx.XChainID)
			inputID := utxo.InputID()
			if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
				Key:   inputID[:],
				Value: utxoBytes,
				Traits: [][]byte{
					key.Address().Bytes(),
				},
			}}}}); err != nil {
				t.Fatal(err)
			}

			tx, err := vm.newImportTx(vm.Ctx.XChainID, ethAddress, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.AtomicMempool.AddLocalTx(tx); err != nil {
				t.Fatal(err)
			}

			msg, err := tvm.VM.WaitForEvent(context.Background())
			require.NoError(t, err)
			require.Equal(t, commonEng.PendingTxs, msg)

			blk, err := vm.BuildBlock(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			if err := blk.Verify(context.Background()); err != nil {
				t.Fatal(err)
			}

			if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
				t.Fatal(err)
			}

			if err := blk.Accept(context.Background()); err != nil {
				t.Fatal(err)
			}

			parent := vm.LastAcceptedExtendedBlock()
			exportAmount := uint64(5000000)
			statedb, err := vm.Ethereum().BlockChain().State()
			if err != nil {
				t.Fatal(err)
			}

			wrappedStateDB := extstate.New(statedb)
			tx, err = atomic.NewExportTx(vm.Ctx, vm.CurrentRules(), wrappedStateDB, vm.Ctx.AVAXAssetID, exportAmount, vm.Ctx.XChainID, key.Address(), vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
			if err != nil {
				t.Fatal(err)
			}

			exportTx := tx.UnsignedAtomicTx

			backend := NewVerifierBackend(vm, vm.CurrentRules())
			if err := backend.SemanticVerify(tx, parent, parent.GetEthBlock().BaseFee()); err != nil {
				t.Fatal("newExportTx created an invalid transaction", err)
			}

			burnedAVAX, err := exportTx.Burned(vm.Ctx.AVAXAssetID)
			if err != nil {
				t.Fatal(err)
			}
			if burnedAVAX != test.expectedBurnedAVAX {
				t.Fatalf("burned wrong amount of AVAX - expected %d burned %d", test.expectedBurnedAVAX, burnedAVAX)
			}

			commitBatch, err := vm.VersionDB().CommitBatch()
			if err != nil {
				t.Fatalf("Failed to create commit batch for VM due to %s", err)
			}
			chainID, atomicRequests, err := exportTx.AtomicOps()
			if err != nil {
				t.Fatalf("Failed to accept export transaction due to: %s", err)
			}

			if err := vm.Ctx.SharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{chainID: atomicRequests}, commitBatch); err != nil {
				t.Fatal(err)
			}

			statedb, err = vm.Ethereum().BlockChain().State()
			if err != nil {
				t.Fatal(err)
			}
			wrappedStateDB = extstate.New(statedb)
			err = exportTx.EVMStateTransfer(vm.Ctx, wrappedStateDB)
			if err != nil {
				t.Fatal(err)
			}

			if wrappedStateDB.GetBalance(ethAddress).Cmp(uint256.NewInt(test.bal*units.Avax)) != 0 {
				t.Fatalf("address balance %s equal %s not %s", ethAddress.String(), wrappedStateDB.GetBalance(ethAddress), new(big.Int).SetUint64(test.bal*units.Avax))
			}
		})
	}
}

func TestNewExportTxMulticoin(t *testing.T) {
	key, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)
	ethAddress := key.PublicKey().EthAddress()
	tests := []struct {
		fork  upgradetest.Fork
		bal   uint64
		balmc uint64
	}{
		{
			fork:  upgradetest.NoUpgrades,
			bal:   49000000,
			balmc: 25000000,
		},
		{
			fork:  upgradetest.ApricotPhase1,
			bal:   49000000,
			balmc: 25000000,
		},
		{
			fork:  upgradetest.ApricotPhase2,
			bal:   48000000,
			balmc: 25000000,
		},
		{
			fork:  upgradetest.ApricotPhase3,
			bal:   48947900,
			balmc: 25000000,
		},
	}
	for _, test := range tests {
		t.Run(test.fork.String(), func(t *testing.T) {
			vm := newAtomicTestVM()
			tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork: &test.fork,
			})
			defer func() {
				if err := vm.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
			}()

			importAmount := uint64(50000000)
			utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

			utxo := &avax.UTXO{
				UTXOID: utxoID,
				Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: importAmount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{key.Address()},
					},
				},
			}
			utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
			if err != nil {
				t.Fatal(err)
			}

			inputID := utxo.InputID()

			tid := ids.GenerateTestID()
			importAmount2 := uint64(30000000)
			utxoID2 := avax.UTXOID{TxID: ids.GenerateTestID()}
			utxo2 := &avax.UTXO{
				UTXOID: utxoID2,
				Asset:  avax.Asset{ID: tid},
				Out: &secp256k1fx.TransferOutput{
					Amt: importAmount2,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{key.Address()},
					},
				},
			}
			utxoBytes2, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo2)
			if err != nil {
				t.Fatal(err)
			}

			xChainSharedMemory := tvm.AtomicMemory.NewSharedMemory(vm.Ctx.XChainID)
			inputID2 := utxo2.InputID()
			if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.ChainID: {PutRequests: []*avalancheatomic.Element{
				{
					Key:   inputID[:],
					Value: utxoBytes,
					Traits: [][]byte{
						key.Address().Bytes(),
					},
				},
				{
					Key:   inputID2[:],
					Value: utxoBytes2,
					Traits: [][]byte{
						key.Address().Bytes(),
					},
				},
			}}}); err != nil {
				t.Fatal(err)
			}

			tx, err := vm.newImportTx(vm.Ctx.XChainID, ethAddress, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.AtomicMempool.AddRemoteTx(tx); err != nil {
				t.Fatal(err)
			}

			msg, err := tvm.VM.WaitForEvent(context.Background())
			require.NoError(t, err)
			require.Equal(t, commonEng.PendingTxs, msg)

			blk, err := vm.BuildBlock(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			if err := blk.Verify(context.Background()); err != nil {
				t.Fatal(err)
			}

			if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
				t.Fatal(err)
			}

			if err := blk.Accept(context.Background()); err != nil {
				t.Fatal(err)
			}

			parent := vm.LastAcceptedExtendedBlock()
			exportAmount := uint64(5000000)
			testKeys0Addr := vmtest.TestKeys[0].EthAddress()
			exportID, err := ids.ToShortID(testKeys0Addr[:])
			if err != nil {
				t.Fatal(err)
			}

			statedb, err := vm.Ethereum().BlockChain().State()
			if err != nil {
				t.Fatal(err)
			}

			wrappedStateDB := extstate.New(statedb)
			tx, err = atomic.NewExportTx(vm.Ctx, vm.CurrentRules(), wrappedStateDB, tid, exportAmount, vm.Ctx.XChainID, exportID, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
			if err != nil {
				t.Fatal(err)
			}

			exportTx := tx.UnsignedAtomicTx
			backend := NewVerifierBackend(vm, vm.CurrentRules())

			if err := backend.SemanticVerify(tx, parent, parent.GetEthBlock().BaseFee()); err != nil {
				t.Fatal("newExportTx created an invalid transaction", err)
			}

			commitBatch, err := vm.VersionDB().CommitBatch()
			if err != nil {
				t.Fatalf("Failed to create commit batch for VM due to %s", err)
			}
			chainID, atomicRequests, err := exportTx.AtomicOps()
			if err != nil {
				t.Fatalf("Failed to accept export transaction due to: %s", err)
			}

			if err := vm.Ctx.SharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{chainID: atomicRequests}, commitBatch); err != nil {
				t.Fatal(err)
			}

			statedb, err = vm.Ethereum().BlockChain().State()
			if err != nil {
				t.Fatal(err)
			}
			wrappedStateDB = extstate.New(statedb)
			err = exportTx.EVMStateTransfer(vm.Ctx, wrappedStateDB)
			if err != nil {
				t.Fatal(err)
			}

			if wrappedStateDB.GetBalance(ethAddress).Cmp(uint256.NewInt(test.bal*units.Avax)) != 0 {
				t.Fatalf("address balance %s equal %s not %s", ethAddress.String(), wrappedStateDB.GetBalance(ethAddress), new(big.Int).SetUint64(test.bal*units.Avax))
			}
			if wrappedStateDB.GetBalanceMultiCoin(ethAddress, common.BytesToHash(tid[:])).Cmp(new(big.Int).SetUint64(test.balmc)) != 0 {
				t.Fatalf("address balance multicoin %s equal %s not %s", ethAddress.String(), wrappedStateDB.GetBalanceMultiCoin(ethAddress, common.BytesToHash(tid[:])), new(big.Int).SetUint64(test.balmc))
			}
		})
	}
}
