// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
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
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/vms/avm/config"
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

	vm := &VM{StateMigrationFactory: NoStateMigrationFactory{}}
	ctx := snowtest.Context(t, snowtest.XChainID)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	err := vm.Initialize(
		context.Background(),
		ctx,                          // context
		memdb.New(),                  // database
		nil,                          // genesisState
		nil,                          // upgradeBytes
		nil,                          // configBytes
		make(chan common.Message, 1), // engineMessenger
		nil,                          // fxs
		nil,                          // AppSender
	)
	require.ErrorIs(err, codec.ErrCantUnpackVersion)
}

func TestInvalidFx(t *testing.T) {
	require := require.New(t)

	vm := &VM{}
	ctx := snowtest.Context(t, snowtest.XChainID)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	genesisBytes := buildGenesisTest(t)
	err := vm.Initialize(
		context.Background(),
		ctx,                          // context
		memdb.New(),                  // database
		genesisBytes,                 // genesisState
		nil,                          // upgradeBytes
		nil,                          // configBytes
		make(chan common.Message, 1), // engineMessenger
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
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	genesisBytes := buildGenesisTest(t)
	err := vm.Initialize(
		context.Background(),
		ctx,                          // context
		memdb.New(),                  // database
		genesisBytes,                 // genesisState
		nil,                          // upgradeBytes
		nil,                          // configBytes
		make(chan common.Message, 1), // engineMessenger
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
	issueAndAccept(require, env.vm, env.issuer, tx)
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
	issueAndAccept(require, env.vm, env.issuer, createAssetTx)

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
	issueAndAccept(require, env.vm, env.issuer, mintNFTTx)

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
	issueAndAccept(require, env.vm, env.issuer, transferNFTTx)
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
	issueAndAccept(require, env.vm, env.issuer, createAssetTx)

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
	issueAndAccept(require, env.vm, env.issuer, mintPropertyTx)

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
	issueAndAccept(require, env.vm, env.issuer, burnPropertyTx)
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
	issueAndAccept(require, env.vm, env.issuer, tx)
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
	issueAndAccept(require, env.vm, env.issuer, tx)
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

	parsedFirstTx, err := env.vm.ParseTx(context.Background(), firstTx.Bytes())
	require.NoError(err)

	require.NoError(parsedFirstTx.Verify(context.Background()))
	require.NoError(parsedFirstTx.Accept(context.Background()))

	parsedSecondTx, err := env.vm.ParseTx(context.Background(), secondTx.Bytes())
	require.NoError(err)

	require.NoError(parsedSecondTx.Verify(context.Background()))
	require.NoError(parsedSecondTx.Accept(context.Background()))

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

	issueAndAccept(require, env.vm, env.issuer, tx)

	env.vm.ctx.Lock.Lock()

	assertIndexedTX(t, env.vm.db, 0, key.PublicKey().Address(), txAssetID.AssetID(), tx.ID())
	assertLatestIdx(t, env.vm.db, key.PublicKey().Address(), avaxID, 1)

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

	parsedTx, err := env.vm.ParseTx(context.Background(), tx.Bytes())
	require.NoError(err)

	require.NoError(parsedTx.Verify(context.Background()))
	require.NoError(parsedTx.Accept(context.Background()))

	assertIndexedTX(t, env.vm.db, 0, key.PublicKey().Address(), txAssetID.AssetID(), tx.ID())
	assertLatestIdx(t, env.vm.db, key.PublicKey().Address(), avaxID, 1)

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

	issueAndAccept(require, env.vm, env.issuer, tx)

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
		assetID    = avax.Asset{ID: avaxID}
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

	issueAndAccept(require, env.vm, env.issuer, tx)

	env.vm.ctx.Lock.Lock()

	_, err = peerSharedMemory.Get(env.vm.ctx.ChainID, [][]byte{utxoID[:]})
	require.ErrorIs(err, database.ErrNotFound)

	assertIndexedTX(t, env.vm.db, 0, key.PublicKey().Address(), assetID.AssetID(), tx.ID())
	assertLatestIdx(t, env.vm.db, key.PublicKey().Address(), assetID.AssetID(), 1)
}

// Tests that VM.Linearize migrates existing state to merkle-ized state
func TestVMLinearizeStateMigration(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	snowCtx := snowtest.Context(t, snowtest.XChainID)
	snowCtx.SharedMemory = atomic.NewMemory(db).NewSharedMemory(ids.ID{})
	configBytes, err := json.Marshal(DefaultConfig)
	require.NoError(err)

	fxs := []*common.Fx{
		{
			ID: secp256k1fx.ID,
			Fx: &secp256k1fx.Fx{},
		},
	}

	config := config.Config{
		Upgrades: upgradetest.GetConfig(upgradetest.Latest),
	}

	vm := &VM{
		Config:                config,
		StateMigrationFactory: NoStateMigrationFactory{},
	}

	toEngine := make(chan common.Message, 1)
	genesisBytes := buildGenesisTest(t)
	require.NoError(vm.Initialize(
		context.Background(),
		snowCtx,
		db,
		genesisBytes,
		nil,
		configBytes,
		toEngine,
		fxs,
		&enginetest.Sender{},
	))
	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.Linearize(context.Background(), ids.ID{}, toEngine))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	txBuilder := txstest.New(
		vm.parser.Codec(),
		vm.ctx,
		&vm.Config,
		vm.ctx.AVAXAssetID,
		vm.state,
	)

	genesisBlkID, err := vm.LastAccepted(context.Background())
	require.NoError(err)

	wantBlkIDs := linked.NewHashmap[ids.ID, uint64]()
	wantBlkIDs.Put(genesisBlkID, 0)

	letters := []rune("ABCEFGHIJKLMNOPQRSTUVWXYZ")
	numBlocks := 10
	wantTxs := make([]ids.ID, 0)
	wantUTXOs := make([]*avax.UTXO, 0)

	for i := 1; i < numBlocks; i++ {
		symbol := string(letters[i%(len(letters)-1)])

		tx, err := txBuilder.CreateAssetTx(
			symbol,
			symbol,
			0,
			map[uint32][]verify.State{
				0: {
					&secp256k1fx.MintOutput{
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].Address()},
						},
					},
				},
			},
			secp256k1fx.NewKeychain(keys[0]),
			keys[0].Address(),
		)
		require.NoError(err)
		issueAndAccept(require, vm, toEngine, tx)

		wantTxs = append(wantTxs, tx.ID())
		utxos := tx.UTXOs()
		wantUTXOs = append(wantUTXOs, utxos...)

		wantBlkID, err := vm.LastAccepted(context.Background())
		require.NoError(err)
		wantBlkIDs.Put(wantBlkID, uint64(i))
	}

	require.Equal(numBlocks, wantBlkIDs.Len())

	db, err = memdb.Copy(db)
	require.NoError(err)
	require.NoError(vm.Shutdown(context.Background()))

	// Migrate state
	snowCtx = snowtest.Context(t, snowtest.XChainID)
	snowCtx.SharedMemory = atomic.NewMemory(db).NewSharedMemory(ids.ID{})

	vm = &VM{
		Config:                config,
		StateMigrationFactory: GForkStateMigrationFactory{CommitFrequency: 1},
	}

	toEngine = make(chan common.Message, 1)
	require.NoError(vm.Initialize(
		context.Background(),
		snowCtx,
		db,
		genesisBytes,
		nil,
		configBytes,
		toEngine,
		fxs,
		&enginetest.Sender{},
	))
	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.Linearize(context.Background(), ids.ID{}, toEngine))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	// Check that all previous state still exists
	for itr := wantBlkIDs.NewIterator(); itr.Next(); {
		wantBlkID := itr.Key()
		height := itr.Value()

		gotBlkID, err := vm.GetBlockIDAtHeight(context.Background(), height)
		require.NoError(err)
		require.Equal(wantBlkID, gotBlkID)

		gotBlk, err := vm.GetBlock(context.Background(), wantBlkID)
		require.NoError(err)
		require.Equal(wantBlkID, gotBlk.ID())
	}

	for _, wantTxID := range wantTxs {
		gotTx, err := vm.GetTx(wantTxID)
		require.NoError(err)
		require.Equal(wantTxID, gotTx.ID())
	}

	for _, wantUTXO := range wantUTXOs {
		gotUTXO, err := vm.GetUTXO(wantUTXO.InputID())
		require.NoError(err)
		require.Equal(wantUTXO.InputID(), gotUTXO.InputID())
	}

	wantLastAcceptedBlkID, _, _ := wantBlkIDs.Newest()
	gotLastAcceptedBlkID, err := vm.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(wantLastAcceptedBlkID, gotLastAcceptedBlkID)

	db, err = memdb.Copy(db)
	require.NoError(err)
	require.NoError(vm.Shutdown(context.Background()))

	// Check that all previous state was deleted
	snowCtx = snowtest.Context(t, snowtest.XChainID)
	snowCtx.SharedMemory = atomic.NewMemory(db).NewSharedMemory(ids.ID{})

	vm = &VM{
		Config:                config,
		StateMigrationFactory: NoStateMigrationFactory{},
	}
	toEngine = make(chan common.Message, 1)
	require.NoError(vm.Initialize(
		context.Background(),
		snowCtx,
		db,
		genesisBytes,
		nil,
		configBytes,
		toEngine,
		fxs,
		&enginetest.Sender{},
	))
	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.Linearize(context.Background(), ids.ID{}, toEngine))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	for itr := wantBlkIDs.NewIterator(); itr.Next(); {
		wantBlkID := itr.Key()
		height := itr.Value()

		_, err := vm.GetBlock(context.Background(), wantBlkID)
		require.ErrorIs(err, database.ErrNotFound)

		_, err = vm.GetBlockIDAtHeight(context.Background(), height)
		require.ErrorIs(err, database.ErrNotFound)
	}

	for _, txID := range wantTxs {
		_, err := vm.GetTx(txID)
		require.ErrorIs(err, database.ErrNotFound)
	}

	for _, utxo := range wantUTXOs {
		_, err := vm.GetUTXO(utxo.InputID())
		require.ErrorIs(err, database.ErrNotFound)
	}
}
