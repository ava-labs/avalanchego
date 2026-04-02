// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

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
	require.NoError(t, err)

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.Ctx.XChainID)
	inputID := utxo.InputID()
	require.NoError(t, xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			key.Address().Bytes(),
		},
	}}}}))

	// Import the funds
	importTx, err := vm.newImportTx(vm.Ctx.XChainID, ethAddr, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
	require.NoError(t, err)

	require.NoError(t, vm.AtomicMempool.AddLocalTx(importTx))

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	blk, err := vm.BuildBlock(t.Context())
	require.NoError(t, err)

	require.NoError(t, blk.Verify(t.Context()))

	require.NoError(t, vm.SetPreference(t.Context(), blk.ID()))

	require.NoError(t, blk.Accept(t.Context()))

	statedb, err := vm.Ethereum().BlockChain().State()
	require.NoError(t, err)

	// Use the funds to create 3 conflicting export transactions sending the funds to each of the test addresses
	exportTxs := make([]*atomic.Tx, 0, 3)
	wrappedStateDB := extstate.New(statedb)
	for _, addr := range vmtest.TestShortIDAddrs {
		exportTx, err := atomic.NewExportTx(vm.Ctx, vm.CurrentRules(), wrappedStateDB, vm.Ctx.AVAXAssetID, uint64(5000000), vm.Ctx.XChainID, addr, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{vmtest.TestKeys[0]})
		require.NoError(t, err)
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
		expectedError error
	}{
		{
			name:        "no transfers",
			tx:          nil,
			avaxBalance: uint256.NewInt(avaxAmount * atomic.X2CRateUint64),
			balances: map[ids.ID]*big.Int{
				customAssetID: big.NewInt(int64(customAmount)),
			},
			expectedNonce: 0,
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
			expectedError: atomic.ErrInsufficientFunds,
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
			expectedError: atomic.ErrInsufficientFunds,
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
			expectedError: atomic.ErrInvalidNonce,
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
			expectedError: atomic.ErrInvalidNonce,
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
				require.NoError(t, vm.Shutdown(t.Context()))
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
			require.NoError(t, err)

			customUTXOBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, customUTXO)
			require.NoError(t, err)

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
				require.NoError(t, err)
			}

			tx, err := vm.newImportTx(vm.Ctx.XChainID, ethAddr, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
			require.NoError(t, err)

			require.NoError(t, vm.AtomicMempool.AddLocalTx(tx))

			msg, err := tvm.VM.WaitForEvent(t.Context())
			require.NoError(t, err)
			require.Equal(t, commonEng.PendingTxs, msg)

			blk, err := vm.BuildBlock(t.Context())
			require.NoError(t, err)

			require.NoError(t, blk.Verify(t.Context()))

			require.NoError(t, vm.SetPreference(t.Context(), blk.ID()))

			require.NoError(t, blk.Accept(t.Context()))

			newTx := atomic.UnsignedExportTx{
				Ins: test.tx,
			}

			statedb, err := vm.Ethereum().BlockChain().State()
			require.NoError(t, err)

			wrappedStateDB := extstate.New(statedb)
			err = newTx.EVMStateTransfer(vm.Ctx, wrappedStateDB)
			require.ErrorIs(t, err, test.expectedError)
			if test.expectedError != nil {
				return
			}

			avaxBalance := wrappedStateDB.GetBalance(ethAddr)
			require.Zero(t, avaxBalance.Cmp(test.avaxBalance), "address balance %s equal %s not %s", addr.String(), avaxBalance, test.avaxBalance)

			for assetID, expectedBalance := range test.balances {
				balance := wrappedStateDB.GetBalanceMultiCoin(ethAddr, common.Hash(assetID))
				require.Equalf(t, 0, avaxBalance.Cmp(test.avaxBalance), "%s address balance %s equal %s not %s", assetID, addr.String(), balance, expectedBalance)
			}

			require.Equalf(t, test.expectedNonce, wrappedStateDB.GetNonce(ethAddr), "failed to set nonce to %d", test.expectedNonce)
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
		require.NoError(t, vm.Shutdown(t.Context()))
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

			rules := extrastest.ForkToRules(test.fork)
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
		require.NoError(t, vm.Shutdown(t.Context()))
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

	require.NoError(t, tx.Sign(atomic.Codec, signers))

	commitBatch, err := vm.VersionDB().CommitBatch()
	require.NoErrorf(t, err, "Failed to create commit batch for VM due to %s", err)
	chainID, atomicRequests, err := tx.AtomicOps()
	require.NoErrorf(t, err, "Failed to accept export transaction due to: %s", err)

	require.NoError(t, vm.Ctx.SharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{chainID: atomicRequests}, commitBatch))
	indexedValues, _, _, err := xChainSharedMemory.Indexed(vm.Ctx.ChainID, [][]byte{addr.Bytes()}, nil, nil, 3)
	require.NoError(t, err)
	require.Len(t, indexedValues, 2)

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
	require.NoError(t, err)
	require.Equalf(t, fetchedValues, indexedValues, "inconsistent values returned fetched vs indexed")

	customUTXOBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, &avax.UTXO{
		UTXOID: customUTXOID,
		Asset:  avax.Asset{ID: custom0AssetID},
		Out:    exportTx.ExportedOutputs[1].Out,
	})
	require.NoError(t, err)

	avaxUTXOBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, &avax.UTXO{
		UTXOID: avaxUTXOID,
		Asset:  avax.Asset{ID: vm.Ctx.AVAXAssetID},
		Out:    exportTx.ExportedOutputs[0].Out,
	})
	require.NoError(t, err)

	require.Equal(t, fetchedValues[0], customUTXOBytes)
	require.Equal(t, fetchedValues[1], avaxUTXOBytes)
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
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrNilTx,
		},
		"valid export tx": {
			generate: func() atomic.UnsignedAtomicTx {
				return exportTx
			},
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.NoUpgrades),
		},
		"valid export tx banff": {
			generate: func() atomic.UnsignedAtomicTx {
				return exportTx
			},
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.Banff),
		},
		"incorrect networkID": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.NetworkID++
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrWrongNetworkID,
		},
		"incorrect blockchainID": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.BlockchainID = ids.GenerateTestID()
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrWrongChainID,
		},
		"incorrect destination chain": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.DestinationChain = ids.GenerateTestID()
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrWrongChainID, // TODO make this error more specific to destination not just chainID
		},
		"no exported outputs": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.ExportedOutputs = nil
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrNoExportOutputs,
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
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrOutputsNotSorted,
		},
		"invalid exported output": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.ExportedOutputs = []*avax.TransferableOutput{tx.ExportedOutputs[0], nil}
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: avax.ErrNilTransferableOutput,
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
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.NoUpgrades),
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
			rules:       extrastest.ForkToRules(upgradetest.ApricotPhase1),
			expectedErr: atomic.ErrInputsNotSortedUnique,
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
			rules:       extrastest.ForkToRules(upgradetest.NoUpgrades),
			expectedErr: atomic.ErrNoValueInput,
		},
		"non-unique EVM input before AP1": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.Ins = []atomic.EVMInput{tx.Ins[0], tx.Ins[0]}
				return &tx
			},
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.NoUpgrades),
		},
		"non-unique EVM input after AP1": {
			generate: func() atomic.UnsignedAtomicTx {
				tx := *exportTx
				tx.Ins = []atomic.EVMInput{tx.Ins[0], tx.Ins[0]}
				return &tx
			},
			ctx:         ctx,
			rules:       extrastest.ForkToRules(upgradetest.ApricotPhase1),
			expectedErr: atomic.ErrInputsNotSortedUnique,
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
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.ApricotPhase6),
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
			ctx:   ctx,
			rules: extrastest.ForkToRules(upgradetest.ApricotPhase6),
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
			rules:       extrastest.ForkToRules(upgradetest.Banff),
			expectedErr: atomic.ErrExportNonAVAXInputBanff,
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
			rules:       extrastest.ForkToRules(upgradetest.Banff),
			expectedErr: atomic.ErrExportNonAVAXOutputBanff,
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
				require.NoError(t, vm.Shutdown(t.Context()))
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
			require.NoError(t, err)

			xChainSharedMemory := tvm.AtomicMemory.NewSharedMemory(vm.Ctx.XChainID)
			inputID := utxo.InputID()
			if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.Ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
				Key:   inputID[:],
				Value: utxoBytes,
				Traits: [][]byte{
					key.Address().Bytes(),
				},
			}}}}); err != nil {
				require.NoError(t, err)
			}

			tx, err := vm.newImportTx(vm.Ctx.XChainID, ethAddress, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
			require.NoError(t, err)

			require.NoError(t, vm.AtomicMempool.AddLocalTx(tx))

			msg, err := tvm.VM.WaitForEvent(t.Context())
			require.NoError(t, err)
			require.Equal(t, commonEng.PendingTxs, msg)

			blk, err := vm.BuildBlock(t.Context())
			require.NoError(t, err)

			require.NoError(t, blk.Verify(t.Context()))

			require.NoError(t, vm.SetPreference(t.Context(), blk.ID()))

			require.NoError(t, blk.Accept(t.Context()))

			parent := vm.LastAcceptedExtendedBlock()
			exportAmount := uint64(5000000)
			statedb, err := vm.Ethereum().BlockChain().State()
			require.NoError(t, err)

			wrappedStateDB := extstate.New(statedb)
			tx, err = atomic.NewExportTx(vm.Ctx, vm.CurrentRules(), wrappedStateDB, vm.Ctx.AVAXAssetID, exportAmount, vm.Ctx.XChainID, key.Address(), vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
			require.NoError(t, err)

			exportTx := tx.UnsignedAtomicTx

			backend := NewVerifierBackend(vm, vm.CurrentRules())
			require.NoError(t, backend.SemanticVerify(tx, parent, parent.GetEthBlock().BaseFee()), "newExportTx created an invalid transaction")

			burnedAVAX, err := exportTx.Burned(vm.Ctx.AVAXAssetID)
			require.NoError(t, err)
			require.Equal(t, test.expectedBurnedAVAX, burnedAVAX, "unexpected amount of AVAX burned")

			commitBatch, err := vm.VersionDB().CommitBatch()
			require.NoErrorf(t, err, "Failed to create commit batch for VM due to %s", err)
			chainID, atomicRequests, err := exportTx.AtomicOps()
			require.NoErrorf(t, err, "Failed to accept export transaction due to: %s", err)

			require.NoError(t, vm.Ctx.SharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{chainID: atomicRequests}, commitBatch))
			statedb, err = vm.Ethereum().BlockChain().State()
			require.NoError(t, err)
			wrappedStateDB = extstate.New(statedb)
			require.NoError(t, exportTx.EVMStateTransfer(vm.Ctx, wrappedStateDB))

			balance := wrappedStateDB.GetBalance(ethAddress)
			expectedBalance := uint256.NewInt(test.bal * units.Avax)
			require.Zerof(t, balance.Cmp(expectedBalance), "address balance %s equal %s not %s", ethAddress.String(), balance, expectedBalance)
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
				require.NoError(t, vm.Shutdown(t.Context()))
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
			require.NoError(t, err)

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
			require.NoError(t, err)

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
				require.NoError(t, err)
			}

			tx, err := vm.newImportTx(vm.Ctx.XChainID, ethAddress, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
			require.NoError(t, err)

			require.NoError(t, vm.AtomicMempool.AddRemoteTx(tx))

			msg, err := tvm.VM.WaitForEvent(t.Context())
			require.NoError(t, err)
			require.Equal(t, commonEng.PendingTxs, msg)

			blk, err := vm.BuildBlock(t.Context())
			require.NoError(t, err)

			require.NoError(t, blk.Verify(t.Context()))

			require.NoError(t, vm.SetPreference(t.Context(), blk.ID()))

			require.NoError(t, blk.Accept(t.Context()))

			parent := vm.LastAcceptedExtendedBlock()
			exportAmount := uint64(5000000)
			testKeys0Addr := vmtest.TestKeys[0].EthAddress()
			exportID, err := ids.ToShortID(testKeys0Addr[:])
			require.NoError(t, err)

			statedb, err := vm.Ethereum().BlockChain().State()
			require.NoError(t, err)

			wrappedStateDB := extstate.New(statedb)
			tx, err = atomic.NewExportTx(vm.Ctx, vm.CurrentRules(), wrappedStateDB, tid, exportAmount, vm.Ctx.XChainID, exportID, vmtest.InitialBaseFee, []*secp256k1.PrivateKey{key})
			require.NoError(t, err)

			exportTx := tx.UnsignedAtomicTx
			backend := NewVerifierBackend(vm, vm.CurrentRules())

			require.NoError(t, backend.SemanticVerify(tx, parent, parent.GetEthBlock().BaseFee()), "newExportTx created an invalid transaction")

			commitBatch, err := vm.VersionDB().CommitBatch()
			require.NoErrorf(t, err, "Failed to create commit batch for VM due to %s", err)
			chainID, atomicRequests, err := exportTx.AtomicOps()
			require.NoErrorf(t, err, "Failed to accept export transaction due to: %s", err)

			require.NoError(t, vm.Ctx.SharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{chainID: atomicRequests}, commitBatch))
			statedb, err = vm.Ethereum().BlockChain().State()
			require.NoError(t, err)
			wrappedStateDB = extstate.New(statedb)
			require.NoError(t, exportTx.EVMStateTransfer(vm.Ctx, wrappedStateDB))

			balance := wrappedStateDB.GetBalance(ethAddress)
			expectedBalance := uint256.NewInt(test.bal * units.Avax)
			require.Zerof(t, balance.Cmp(expectedBalance), "address balance %s equal %s not %s", ethAddress.String(), balance, expectedBalance)

			balanceMC := wrappedStateDB.GetBalanceMultiCoin(ethAddress, common.BytesToHash(tid[:]))
			expectedBalanceMC := new(big.Int).SetUint64(test.balmc)
			require.Equalf(t, expectedBalanceMC, balanceMC, "address multicoin balance %s equal %s not %s", ethAddress.String(), balanceMC, expectedBalanceMC)
		})
	}
}
