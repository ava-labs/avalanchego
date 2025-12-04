// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/state"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/txstest"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestInvalidGenesis(t *testing.T) {
	require := require.New(t)

	vm := &VM{StateMigration: state.NoMigration{}}
	ctx := snowtest.Context(t, snowtest.XChainID)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	err := vm.Initialize(
		t.Context(),
		ctx,         // context
		memdb.New(), // database
		nil,         // genesisState
		nil,         // upgradeBytes
		nil,         // configBytes
		nil,         // fxs
		nil,         // AppSender
	)
	require.ErrorIs(err, codec.ErrCantUnpackVersion)
}

func TestInvalidFx(t *testing.T) {
	require := require.New(t)

	vm := &VM{}
	ctx := snowtest.Context(t, snowtest.XChainID)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
		ctx.Lock.Unlock()
	}()

	genesisBytes := newGenesisBytesTest(t)
	err := vm.Initialize(
		t.Context(),
		ctx,          // context
		memdb.New(),  // database
		genesisBytes, // genesisState
		nil,          // upgradeBytes
		nil,          // configBytes
		[]*common.Fx{ // fxs
			nil,
		},
		nil,
	)
	require.ErrorIs(err, errIncompatibleFx)
}

func TestFxInitializationFailure(t *testing.T) {
	require := require.New(t)

	vm := &VM{}
	ctx := snowtest.Context(t, snowtest.XChainID)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
		ctx.Lock.Unlock()
	}()

	genesisBytes := newGenesisBytesTest(t)
	err := vm.Initialize(
		t.Context(),
		ctx,          // context
		memdb.New(),  // database
		genesisBytes, // genesisState
		nil,          // upgradeBytes
		nil,          // configBytes
		[]*common.Fx{{ // fxs
			ID: ids.Empty,
			Fx: &FxTest{
				InitializeF: func(interface{}) error {
					return errUnknownFx
				},
			},
		}},
		nil,
	)
	require.ErrorIs(err, errUnknownFx)
}

func TestIssueTx(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork: upgradetest.Latest,
	})
	env.vm.ctx.Lock.Unlock()

	tx := newTx(t, env.genesisBytes, env.vm.ctx.ChainID, env.vm.parser, "AVAX")
	issueAndAccept(require, env.vm, tx)
}

// Test issuing a transaction that creates an NFT family
func TestIssueNFT(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork: upgradetest.Latest,
	})
	env.vm.ctx.Lock.Unlock()

	var (
		key = keys[0]
		kc  = secp256k1fx.NewKeychain(key)
	)

	// Create the asset
	initialStates := map[uint32][]verify.State{
		1: {
			&nftfx.MintOutput{
				GroupID: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		},
	}

	createAssetTx, err := env.txBuilder.CreateAssetTx(
		"Team Rocket", // name
		"TR",          // symbol
		0,             // denomination
		initialStates,
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, createAssetTx)

	// Mint the NFT
	mintNFTTx, err := env.txBuilder.MintNFT(
		createAssetTx.ID(),
		[]byte{'h', 'e', 'l', 'l', 'o'}, // payload
		[]*secp256k1fx.OutputOwners{{
			Threshold: 1,
			Addrs:     []ids.ShortID{key.Address()},
		}},
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, mintNFTTx)

	// Move the NFT
	utxos, err := avax.GetAllUTXOs(env.vm.state, kc.Addresses())
	require.NoError(err)
	transferOp, _, err := env.vm.SpendNFT(
		utxos,
		kc,
		createAssetTx.ID(),
		1,
		keys[2].Address(),
	)
	require.NoError(err)

	transferNFTTx, err := env.txBuilder.Operation(
		transferOp,
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, transferNFTTx)
}

// Test issuing a transaction that creates an Property family
func TestIssueProperty(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork: upgradetest.Latest,
		additionalFxs: []*common.Fx{{
			ID: propertyfx.ID,
			Fx: &propertyfx.Fx{},
		}},
	})
	env.vm.ctx.Lock.Unlock()

	var (
		key = keys[0]
		kc  = secp256k1fx.NewKeychain(key)
	)

	// create the asset
	initialStates := map[uint32][]verify.State{
		2: {
			&propertyfx.MintOutput{
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		},
	}

	createAssetTx, err := env.txBuilder.CreateAssetTx(
		"Team Rocket", // name
		"TR",          // symbol
		0,             // denomination
		initialStates,
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, createAssetTx)

	// mint the property
	mintPropertyOp := &txs.Operation{
		Asset: avax.Asset{ID: createAssetTx.ID()},
		UTXOIDs: []*avax.UTXOID{{
			TxID:        createAssetTx.ID(),
			OutputIndex: 1,
		}},
		Op: &propertyfx.MintOperation{
			MintInput: secp256k1fx.Input{
				SigIndices: []uint32{0},
			},
			MintOutput: propertyfx.MintOutput{
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
			OwnedOutput: propertyfx.OwnedOutput{},
		},
	}

	mintPropertyTx, err := env.txBuilder.Operation(
		[]*txs.Operation{mintPropertyOp},
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, mintPropertyTx)

	// burn the property
	burnPropertyOp := &txs.Operation{
		Asset: avax.Asset{ID: createAssetTx.ID()},
		UTXOIDs: []*avax.UTXOID{{
			TxID:        mintPropertyTx.ID(),
			OutputIndex: 2,
		}},
		Op: &propertyfx.BurnOperation{Input: secp256k1fx.Input{}},
	}

	burnPropertyTx, err := env.txBuilder.Operation(
		[]*txs.Operation{burnPropertyOp},
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, burnPropertyTx)
}

func TestIssueTxWithFeeAsset(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork:             upgradetest.Latest,
		isCustomFeeAsset: true,
	})
	env.vm.ctx.Lock.Unlock()

	// send first asset
	tx := newTx(t, env.genesisBytes, env.vm.ctx.ChainID, env.vm.parser, feeAssetName)
	issueAndAccept(require, env.vm, tx)
}

func TestIssueTxWithAnotherAsset(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork:             upgradetest.Latest,
		isCustomFeeAsset: true,
	})
	env.vm.ctx.Lock.Unlock()

	// send second asset
	var (
		key = keys[0]
		kc  = secp256k1fx.NewKeychain(key)

		feeAssetCreateTx = getCreateTxFromGenesisTest(t, env.genesisBytes, feeAssetName)
		createTx         = getCreateTxFromGenesisTest(t, env.genesisBytes, otherAssetName)
	)

	tx, err := env.txBuilder.BaseTx(
		[]*avax.TransferableOutput{
			{ // fee asset
				Asset: avax.Asset{ID: feeAssetCreateTx.ID()},
				Out: &secp256k1fx.TransferOutput{
					Amt: startBalance - env.vm.TxFee,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{key.PublicKey().Address()},
					},
				},
			},
			{ // issued asset
				Asset: avax.Asset{ID: createTx.ID()},
				Out: &secp256k1fx.TransferOutput{
					Amt: startBalance - env.vm.TxFee,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{key.PublicKey().Address()},
					},
				},
			},
		},
		nil, // memo
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, tx)
}

func TestVMFormat(t *testing.T) {
	env := setup(t, &envConfig{
		fork: upgradetest.Latest,
	})
	defer env.vm.ctx.Lock.Unlock()

	tests := []struct {
		in       ids.ShortID
		expected string
	}{
		{
			in:       ids.ShortEmpty,
			expected: "X-testing1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqtu2yas",
		},
	}
	for _, test := range tests {
		t.Run(test.in.String(), func(t *testing.T) {
			require := require.New(t)
			addrStr, err := env.vm.FormatLocalAddress(test.in)
			require.NoError(err)
			require.Equal(test.expected, addrStr)
		})
	}
}

func TestTxAcceptAfterParseTx(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork:          upgradetest.Latest,
		notLinearized: true,
	})
	defer env.vm.ctx.Lock.Unlock()

	var (
		key = keys[0]
		kc  = secp256k1fx.NewKeychain(key)
	)

	firstTx, err := env.txBuilder.BaseTx(
		[]*avax.TransferableOutput{{
			Asset: avax.Asset{ID: env.genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: startBalance - env.vm.TxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
		nil, // memo
		kc,
		key.Address(),
	)
	require.NoError(err)

	// let secondTx spend firstTx outputs
	secondTx := &txs.Tx{Unsigned: &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: env.vm.ctx.XChainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        firstTx.ID(),
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: env.genesisTx.ID()},
				In: &secp256k1fx.TransferInput{
					Amt: startBalance - env.vm.TxFee,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{
							0,
						},
					},
				},
			}},
		},
	}}
	require.NoError(secondTx.SignSECP256K1Fx(env.vm.parser.Codec(), [][]*secp256k1.PrivateKey{{key}}))

	parsedFirstTx, err := env.vm.ParseTx(t.Context(), firstTx.Bytes())
	require.NoError(err)

	require.NoError(parsedFirstTx.Verify(t.Context()))
	require.NoError(parsedFirstTx.Accept(t.Context()))

	parsedSecondTx, err := env.vm.ParseTx(t.Context(), secondTx.Bytes())
	require.NoError(err)

	require.NoError(parsedSecondTx.Verify(t.Context()))
	require.NoError(parsedSecondTx.Accept(t.Context()))

	_, err = env.vm.state.GetTx(firstTx.ID())
	require.NoError(err)

	_, err = env.vm.state.GetTx(secondTx.ID())
	require.NoError(err)
}

// Test issuing an import transaction.
func TestIssueImportTx(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork: upgradetest.Durango,
	})
	defer env.vm.ctx.Lock.Unlock()

	peerSharedMemory := env.sharedMemory.NewSharedMemory(constants.PlatformChainID)

	genesisTx := getCreateTxFromGenesisTest(t, env.genesisBytes, "AVAX")
	avaxID := genesisTx.ID()

	var (
		key = keys[0]
		kc  = secp256k1fx.NewKeychain(key)

		utxoID = avax.UTXOID{
			TxID: ids.ID{
				0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
				0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
				0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
				0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
			},
		}
		txAssetID    = avax.Asset{ID: avaxID}
		importedUtxo = &avax.UTXO{
			UTXOID: utxoID,
			Asset:  txAssetID,
			Out: &secp256k1fx.TransferOutput{
				Amt: 1010,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}
	)

	// Provide the platform UTXO:
	utxoBytes, err := env.vm.parser.Codec().Marshal(txs.CodecVersion, importedUtxo)
	require.NoError(err)

	inputID := importedUtxo.InputID()
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		env.vm.ctx.ChainID: {
			PutRequests: []*atomic.Element{{
				Key:   inputID[:],
				Value: utxoBytes,
				Traits: [][]byte{
					key.PublicKey().Address().Bytes(),
				},
			}},
		},
	}))

	tx, err := env.txBuilder.ImportTx(
		constants.PlatformChainID, // source chain
		key.Address(),
		kc,
	)
	require.NoError(err)

	env.vm.ctx.Lock.Unlock()

	issueAndAccept(require, env.vm, tx)

	env.vm.ctx.Lock.Lock()

	id := utxoID.InputID()
	_, err = env.vm.ctx.SharedMemory.Get(constants.PlatformChainID, [][]byte{id[:]})
	require.ErrorIs(err, database.ErrNotFound)
}

// Test force accepting an import transaction.
func TestForceAcceptImportTx(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork:          upgradetest.Durango,
		notLinearized: true,
	})
	defer env.vm.ctx.Lock.Unlock()

	genesisTx := getCreateTxFromGenesisTest(t, env.genesisBytes, "AVAX")
	avaxID := genesisTx.ID()

	key := keys[0]
	utxoID := avax.UTXOID{
		TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		},
	}

	txAssetID := avax.Asset{ID: avaxID}
	tx := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: env.vm.ctx.XChainID,
			Outs: []*avax.TransferableOutput{{
				Asset: txAssetID,
				Out: &secp256k1fx.TransferOutput{
					Amt: 10,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			}},
		}},
		SourceChain: constants.PlatformChainID,
		ImportedIns: []*avax.TransferableInput{{
			UTXOID: utxoID,
			Asset:  txAssetID,
			In: &secp256k1fx.TransferInput{
				Amt: 1010,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{0},
				},
			},
		}},
	}}
	require.NoError(tx.SignSECP256K1Fx(env.vm.parser.Codec(), [][]*secp256k1.PrivateKey{{key}}))

	parsedTx, err := env.vm.ParseTx(t.Context(), tx.Bytes())
	require.NoError(err)

	require.NoError(parsedTx.Verify(t.Context()))
	require.NoError(parsedTx.Accept(t.Context()))

	id := utxoID.InputID()
	_, err = env.vm.ctx.SharedMemory.Get(constants.PlatformChainID, [][]byte{id[:]})
	require.ErrorIs(err, database.ErrNotFound)
}

func TestImportTxNotState(t *testing.T) {
	require := require.New(t)

	intf := interface{}(&txs.ImportTx{})
	_, ok := intf.(verify.State)
	require.False(ok)
}

// Test issuing an export transaction.
func TestIssueExportTx(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{fork: upgradetest.Durango})
	defer env.vm.ctx.Lock.Unlock()

	genesisTx := getCreateTxFromGenesisTest(t, env.genesisBytes, "AVAX")

	var (
		avaxID     = genesisTx.ID()
		key        = keys[0]
		kc         = secp256k1fx.NewKeychain(key)
		to         = key.PublicKey().Address()
		changeAddr = to
	)

	tx, err := env.txBuilder.ExportTx(
		constants.PlatformChainID,
		to, // to
		avaxID,
		startBalance-env.vm.TxFee,
		kc,
		changeAddr,
	)
	require.NoError(err)

	peerSharedMemory := env.sharedMemory.NewSharedMemory(constants.PlatformChainID)
	utxoBytes, _, _, err := peerSharedMemory.Indexed(
		env.vm.ctx.ChainID,
		[][]byte{
			key.PublicKey().Address().Bytes(),
		},
		nil,
		nil,
		math.MaxInt32,
	)
	require.NoError(err)
	require.Empty(utxoBytes)

	env.vm.ctx.Lock.Unlock()

	issueAndAccept(require, env.vm, tx)

	env.vm.ctx.Lock.Lock()

	utxoBytes, _, _, err = peerSharedMemory.Indexed(
		env.vm.ctx.ChainID,
		[][]byte{
			key.PublicKey().Address().Bytes(),
		},
		nil,
		nil,
		math.MaxInt32,
	)
	require.NoError(err)
	require.Len(utxoBytes, 1)
}

func TestClearForceAcceptedExportTx(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork: upgradetest.Latest,
	})
	defer env.vm.ctx.Lock.Unlock()

	genesisTx := getCreateTxFromGenesisTest(t, env.genesisBytes, "AVAX")

	var (
		avaxID     = genesisTx.ID()
		key        = keys[0]
		kc         = secp256k1fx.NewKeychain(key)
		to         = key.PublicKey().Address()
		changeAddr = to
	)

	tx, err := env.txBuilder.ExportTx(
		constants.PlatformChainID,
		to, // to
		avaxID,
		startBalance-env.vm.TxFee,
		kc,
		changeAddr,
	)
	require.NoError(err)

	utxo := avax.UTXOID{
		TxID:        tx.ID(),
		OutputIndex: 0,
	}
	utxoID := utxo.InputID()

	peerSharedMemory := env.sharedMemory.NewSharedMemory(constants.PlatformChainID)
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		env.vm.ctx.ChainID: {
			RemoveRequests: [][]byte{utxoID[:]},
		},
	}))

	_, err = peerSharedMemory.Get(env.vm.ctx.ChainID, [][]byte{utxoID[:]})
	require.ErrorIs(err, database.ErrNotFound)

	env.vm.ctx.Lock.Unlock()

	issueAndAccept(require, env.vm, tx)

	env.vm.ctx.Lock.Lock()

	_, err = peerSharedMemory.Get(env.vm.ctx.ChainID, [][]byte{utxoID[:]})
	require.ErrorIs(err, database.ErrNotFound)
}

// Tests that VM.Linearize migrates existing state to merkle-ized state
func TestVMLinearizeStateMigration(t *testing.T) {
	require := require.New(t)

	db := memdb.New()

	snowCtx := snowtest.Context(t, snowtest.XChainID)
	snowCtx.SharedMemory = atomic.NewMemory(db).NewSharedMemory(ids.Empty)

	configBytes, err := json.Marshal(DefaultConfig)
	require.NoError(err)

	fxs := []*common.Fx{{ID: secp256k1fx.ID, Fx: &secp256k1fx.Fx{}}}

	config := config.Config{
		Upgrades: upgradetest.GetConfig(upgradetest.Latest),
	}

	vm := &VM{
		Config:         config,
		StateMigration: state.NoMigration{},
	}

	genesisData := makeDefaultGenesisData(t)
	genesis, err := NewGenesis(
		constants.UnitTestID,
		genesisData,
	)
	require.NoError(err)
	genesisBytes, err := genesis.Bytes()
	require.NoError(err)

	// Start the VM and create some initial state that needs to be migrated
	require.NoError(vm.Initialize(
		t.Context(),
		snowCtx,
		db,
		genesisBytes,
		nil,
		configBytes,
		fxs,
		&enginetest.Sender{},
	))
	require.NoError(vm.SetState(t.Context(), snow.Bootstrapping))

	genesisTx := getCreateTxFromGenesisTest(t, genesisBytes, genesis.Txs[0].Name)

	txBuilder := txstest.New(
		vm.parser.Codec(),
		vm.ctx,
		&vm.Config,
		vm.ctx.AVAXAssetID,
		vm.state,
	)

	key := keys[0]
	kc := secp256k1fx.NewKeychain(key)

	firstTx, err := txBuilder.BaseTx(
		[]*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: genesisTx.ID()},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{key.PublicKey().Address()},
					},
				},
			},
		},
		nil,
		kc,
		key.Address(),
	)
	require.NoError(err)

	stopVtx, err := vm.ParseTx(t.Context(), firstTx.Bytes())
	require.NoError(err)
	require.NoError(stopVtx.Verify(t.Context()))
	require.NoError(stopVtx.Accept(t.Context()))

	require.NoError(vm.Linearize(t.Context(), stopVtx.ID()))
	require.NoError(vm.SetState(t.Context(), snow.NormalOp))

	genesisBlkID, err := vm.LastAccepted(t.Context())
	require.NoError(err)

	var (
		wantBlkIDs        = []ids.ID{genesisBlkID}
		wantTxIDs         []ids.ID
		wantConsumedUTXOs []*avax.UTXO
		wantProducedUTXOs []*avax.UTXO
	)

	// Create an asset and export it into shared memory
	tx, err := txBuilder.CreateAssetTx(
		"FOO",
		"BAR",
		0,
		map[uint32][]verify.State{
			0: {
				&secp256k1fx.TransferOutput{
					Amt: 100,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].Address()},
					},
				},
			},
		},
		kc,
		keys[0].Address(),
	)
	require.NoError(err)
	blkID := issueAndAccept(require, vm, tx)

	wantTxIDs = append(wantTxIDs, tx.ID())
	wantConsumedUTXOs = append(wantConsumedUTXOs, tx.UTXOs()...)

	wantBlkIDs = append(wantBlkIDs, blkID)

	atomicTx, err := txBuilder.ExportTx(
		snowtest.PChainID,
		ids.GenerateTestShortID(),
		tx.ID(),
		1,
		kc,
		keys[0].Address(),
	)
	require.NoError(err)
	blkID = issueAndAccept(require, vm, atomicTx)

	wantTxIDs = append(wantTxIDs, atomicTx.ID())
	wantProducedUTXOs = append(wantProducedUTXOs, atomicTx.UTXOs()...)

	wantBlkIDs = append(wantBlkIDs, blkID)

	// Genesis block + 2 blocks that were built
	require.Len(wantBlkIDs, 3)

	db, err = memdb.Copy(db)
	require.NoError(err)
	require.NoError(vm.Shutdown(t.Context()))

	// Restart the VM with the migration enabled and verify that all state is
	// migrated into the new database format
	snowCtx = snowtest.Context(t, snowtest.XChainID)
	snowCtx.SharedMemory = atomic.NewMemory(db).NewSharedMemory(ids.Empty)

	vm = &VM{
		Config:         config,
		StateMigration: &state.FirewoodMigration{CommitFrequency: 1},
	}

	require.NoError(vm.Initialize(
		t.Context(),
		snowCtx,
		db,
		genesisBytes,
		nil,
		configBytes,
		fxs,
		&enginetest.Sender{},
	))
	require.NoError(vm.SetState(t.Context(), snow.Bootstrapping))
	require.NoError(vm.Linearize(t.Context(), stopVtx.ID()))
	require.NoError(vm.SetState(t.Context(), snow.NormalOp))

	// Check that all previous state still exists
	for height, wantBlkID := range wantBlkIDs {
		gotBlkID, err := vm.GetBlockIDAtHeight(t.Context(), uint64(height))
		require.NoError(err)
		require.Equal(wantBlkID, gotBlkID)

		gotBlk, err := vm.GetBlock(t.Context(), wantBlkID)
		require.NoError(err)
		require.Equal(wantBlkID, gotBlk.ID())
	}

	for _, txID := range wantTxIDs {
		gotTx, err := vm.GetTx(txID)
		require.NoError(err)
		require.Equal(txID, gotTx.ID())
	}

	for _, utxo := range wantConsumedUTXOs {
		_, err := vm.GetUTXO(utxo.InputID())
		// Consumed UTXOs should be removed from state
		require.ErrorIs(err, database.ErrNotFound)
	}

	for _, utxo := range wantProducedUTXOs {
		gotUTXO, err := vm.GetUTXO(utxo.InputID())
		require.NoError(err)
		require.Equal(utxo.InputID(), gotUTXO.InputID())
	}

	wantLastAcceptedBlkID := wantBlkIDs[len(wantBlkIDs)-1]
	gotLastAcceptedBlkID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(wantLastAcceptedBlkID, gotLastAcceptedBlkID)

	db, err = memdb.Copy(db)
	require.NoError(err)
	require.NoError(vm.Shutdown(t.Context()))

	// Restart the VM using the old state format and verify that all the previous
	// data was deleted
	snowCtx = snowtest.Context(t, snowtest.XChainID)
	snowCtx.SharedMemory = atomic.NewMemory(db).NewSharedMemory(ids.Empty)

	vm = &VM{
		Config:         config,
		StateMigration: state.NoMigration{},
	}
	require.NoError(vm.Initialize(
		t.Context(),
		snowCtx,
		db,
		genesisBytes,
		nil,
		configBytes,
		fxs,
		&enginetest.Sender{},
	))
	require.NoError(vm.SetState(t.Context(), snow.Bootstrapping))
	require.NoError(vm.Linearize(t.Context(), stopVtx.ID()))
	require.NoError(vm.SetState(t.Context(), snow.NormalOp))

	// The genesis block should always be present because it is initialized as
	// part of the VM initial state.
	_, err = vm.GetBlock(t.Context(), genesisBlkID)
	require.NoError(err)
	_, err = vm.GetBlockIDAtHeight(t.Context(), 0)
	require.NoError(err)

	// Any blocks after genesis should be deleted because they exist in the new
	// state format.
	for height, wantBlkID := range wantBlkIDs {
		if height == 0 {
			continue
		}

		_, err := vm.GetBlock(t.Context(), wantBlkID)
		require.ErrorIs(err, database.ErrNotFound)

		_, err = vm.GetBlockIDAtHeight(t.Context(), uint64(height))
		require.ErrorIs(err, database.ErrNotFound)
	}

	for _, txID := range wantTxIDs {
		_, err := vm.GetTx(txID)
		require.ErrorIs(err, database.ErrNotFound)
	}

	for _, utxo := range wantConsumedUTXOs {
		_, err := vm.GetUTXO(utxo.InputID())
		require.ErrorIs(err, database.ErrNotFound)
	}

	for _, utxo := range wantProducedUTXOs {
		_, err := vm.GetUTXO(utxo.InputID())
		require.ErrorIs(err, database.ErrNotFound)
	}
}
