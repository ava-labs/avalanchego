// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"math"
	"testing"

	stdjson "encoding/json"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/metrics"
	"github.com/ava-labs/avalanchego/vms/avm/states"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestInvalidGenesis(t *testing.T) {
	require := require.New(t)

	vm := &VM{}
	ctx := NewContext(t)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	err := vm.Initialize(
		context.Background(),
		ctx,                                     // context
		manager.NewMemDB(version.Semantic1_0_0), // dbManager
		nil,                                     // genesisState
		nil,                                     // upgradeBytes
		nil,                                     // configBytes
		make(chan common.Message, 1),            // engineMessenger
		nil,                                     // fxs
		nil,                                     // AppSender
	)
	require.ErrorIs(err, codec.ErrCantUnpackVersion)
}

func TestInvalidFx(t *testing.T) {
	require := require.New(t)

	vm := &VM{}
	ctx := NewContext(t)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	genesisBytes := BuildGenesisTest(t)
	err := vm.Initialize(
		context.Background(),
		ctx,                                     // context
		manager.NewMemDB(version.Semantic1_0_0), // dbManager
		genesisBytes,                            // genesisState
		nil,                                     // upgradeBytes
		nil,                                     // configBytes
		make(chan common.Message, 1),            // engineMessenger
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
	ctx := NewContext(t)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	genesisBytes := BuildGenesisTest(t)
	err := vm.Initialize(
		context.Background(),
		ctx,                                     // context
		manager.NewMemDB(version.Semantic1_0_0), // dbManager
		genesisBytes,                            // genesisState
		nil,                                     // upgradeBytes
		nil,                                     // configBytes
		make(chan common.Message, 1),            // engineMessenger
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

	env := setup(t, &envConfig{})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	tx := NewTxWithAsset(t, env.genesisBytes, env.vm, "AVAX")
	issueAndAccept(require, env.vm, env.issuer, tx)
}

// Test issuing a transaction that creates an NFT family
func TestIssueNFT(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		vmStaticConfig: &config.Config{},
	})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	createAssetTx := &txs.Tx{Unsigned: &txs.CreateAssetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
		}},
		Name:         "Team Rocket",
		Symbol:       "TR",
		Denomination: 0,
		States: []*txs.InitialState{{
			FxIndex: 1,
			Outs: []verify.State{
				&nftfx.MintOutput{
					GroupID: 1,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
				&nftfx.MintOutput{
					GroupID: 2,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		}},
	}}
	require.NoError(env.vm.parser.InitializeTx(createAssetTx))
	issueAndAccept(require, env.vm, env.issuer, createAssetTx)

	mintNFTTx := &txs.Tx{Unsigned: &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
		}},
		Ops: []*txs.Operation{{
			Asset: avax.Asset{ID: createAssetTx.ID()},
			UTXOIDs: []*avax.UTXOID{{
				TxID:        createAssetTx.ID(),
				OutputIndex: 0,
			}},
			Op: &nftfx.MintOperation{
				MintInput: secp256k1fx.Input{
					SigIndices: []uint32{0},
				},
				GroupID: 1,
				Payload: []byte{'h', 'e', 'l', 'l', 'o'},
				Outputs: []*secp256k1fx.OutputOwners{{}},
			},
		}},
	}}
	require.NoError(mintNFTTx.SignNFTFx(env.vm.parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}}))
	issueAndAccept(require, env.vm, env.issuer, mintNFTTx)

	transferNFTTx := &txs.Tx{
		Unsigned: &txs.OperationTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    constants.UnitTestID,
				BlockchainID: chainID,
			}},
			Ops: []*txs.Operation{{
				Asset: avax.Asset{ID: createAssetTx.ID()},
				UTXOIDs: []*avax.UTXOID{{
					TxID:        mintNFTTx.ID(),
					OutputIndex: 0,
				}},
				Op: &nftfx.TransferOperation{
					Input: secp256k1fx.Input{},
					Output: nftfx.TransferOutput{
						GroupID:      1,
						Payload:      []byte{'h', 'e', 'l', 'l', 'o'},
						OutputOwners: secp256k1fx.OutputOwners{},
					},
				},
			}},
		},
		Creds: []*fxs.FxCredential{
			{Verifiable: &nftfx.Credential{}},
		},
	}
	require.NoError(env.vm.parser.InitializeTx(transferNFTTx))
	issueAndAccept(require, env.vm, env.issuer, transferNFTTx)
}

// Test issuing a transaction that creates an Property family
func TestIssueProperty(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		vmStaticConfig: &config.Config{},
		additionalFxs: []*common.Fx{{
			ID: propertyfx.ID,
			Fx: &propertyfx.Fx{},
		}},
	})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	createAssetTx := &txs.Tx{Unsigned: &txs.CreateAssetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
		}},
		Name:         "Team Rocket",
		Symbol:       "TR",
		Denomination: 0,
		States: []*txs.InitialState{{
			FxIndex: 2,
			Outs: []verify.State{
				&propertyfx.MintOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		}},
	}}
	require.NoError(env.vm.parser.InitializeTx(createAssetTx))
	issueAndAccept(require, env.vm, env.issuer, createAssetTx)

	mintPropertyTx := &txs.Tx{Unsigned: &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
		}},
		Ops: []*txs.Operation{{
			Asset: avax.Asset{ID: createAssetTx.ID()},
			UTXOIDs: []*avax.UTXOID{{
				TxID:        createAssetTx.ID(),
				OutputIndex: 0,
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
		}},
	}}

	codec := env.vm.parser.Codec()
	require.NoError(mintPropertyTx.SignPropertyFx(codec, [][]*secp256k1.PrivateKey{
		{keys[0]},
	}))
	issueAndAccept(require, env.vm, env.issuer, mintPropertyTx)

	burnPropertyTx := &txs.Tx{Unsigned: &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
		}},
		Ops: []*txs.Operation{{
			Asset: avax.Asset{ID: createAssetTx.ID()},
			UTXOIDs: []*avax.UTXOID{{
				TxID:        mintPropertyTx.ID(),
				OutputIndex: 1,
			}},
			Op: &propertyfx.BurnOperation{Input: secp256k1fx.Input{}},
		}},
	}}

	require.NoError(burnPropertyTx.SignPropertyFx(codec, [][]*secp256k1.PrivateKey{
		{},
	}))
	issueAndAccept(require, env.vm, env.issuer, burnPropertyTx)
}

func TestIssueTxWithFeeAsset(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		isCustomFeeAsset: true,
	})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	// send first asset
	tx := NewTxWithAsset(t, env.genesisBytes, env.vm, feeAssetName)
	issueAndAccept(require, env.vm, env.issuer, tx)
}

func TestIssueTxWithAnotherAsset(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		isCustomFeeAsset: true,
	})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	// send second asset
	feeAssetCreateTx := GetCreateTxFromGenesisTest(t, env.genesisBytes, feeAssetName)
	createTx := GetCreateTxFromGenesisTest(t, env.genesisBytes, otherAssetName)

	tx := &txs.Tx{Unsigned: &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Ins: []*avax.TransferableInput{
				// fee asset
				{
					UTXOID: avax.UTXOID{
						TxID:        feeAssetCreateTx.ID(),
						OutputIndex: 2,
					},
					Asset: avax.Asset{ID: feeAssetCreateTx.ID()},
					In: &secp256k1fx.TransferInput{
						Amt: startBalance,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{
								0,
							},
						},
					},
				},
				// issued asset
				{
					UTXOID: avax.UTXOID{
						TxID:        createTx.ID(),
						OutputIndex: 2,
					},
					Asset: avax.Asset{ID: createTx.ID()},
					In: &secp256k1fx.TransferInput{
						Amt: startBalance,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{
								0,
							},
						},
					},
				},
			},
		},
	}}
	require.NoError(tx.SignSECP256K1Fx(env.vm.parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}, {keys[0]}}))

	issueAndAccept(require, env.vm, env.issuer, tx)
}

func TestVMFormat(t *testing.T) {
	env := setup(t, &envConfig{})
	defer func() {
		require.NoError(t, env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

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

func TestTxCached(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	newTx := NewTxWithAsset(t, env.genesisBytes, env.vm, "AVAX")
	txBytes := newTx.Bytes()

	_, err := env.vm.ParseTx(context.Background(), txBytes)
	require.NoError(err)

	registerer := prometheus.NewRegistry()

	env.vm.metrics, err = metrics.New("", registerer)
	require.NoError(err)

	db := memdb.New()
	vdb := versiondb.New(db)
	env.vm.state, err = states.New(vdb, env.vm.parser, registerer)
	require.NoError(err)

	_, err = env.vm.ParseTx(context.Background(), txBytes)
	require.NoError(err)

	count, err := database.Count(vdb)
	require.NoError(err)
	require.Zero(count)
}

func TestTxNotCached(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	newTx := NewTxWithAsset(t, env.genesisBytes, env.vm, "AVAX")
	txBytes := newTx.Bytes()

	_, err := env.vm.ParseTx(context.Background(), txBytes)
	require.NoError(err)

	registerer := prometheus.NewRegistry()
	env.vm.metrics, err = metrics.New("", registerer)
	require.NoError(err)

	db := memdb.New()
	vdb := versiondb.New(db)
	env.vm.state, err = states.New(vdb, env.vm.parser, registerer)
	require.NoError(err)

	env.vm.uniqueTxs.Flush()

	_, err = env.vm.ParseTx(context.Background(), txBytes)
	require.NoError(err)

	count, err := database.Count(vdb)
	require.NoError(err)
	require.NotZero(count)
}

func TestTxVerifyAfterIssueTx(t *testing.T) {
	require := require.New(t)

	issuer, vm, ctx, issueTxs := setupIssueTx(t)
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()
	firstTx := issueTxs[1]
	secondTx := issueTxs[2]
	parsedSecondTx, err := vm.ParseTx(context.Background(), secondTx.Bytes())
	require.NoError(err)
	require.NoError(parsedSecondTx.Verify(context.Background()))
	_, err = vm.IssueTx(firstTx.Bytes())
	require.NoError(err)
	require.NoError(parsedSecondTx.Accept(context.Background()))
	ctx.Lock.Unlock()

	require.Equal(common.PendingTxs, <-issuer)
	ctx.Lock.Lock()

	txs := vm.PendingTxs(context.Background())
	require.Len(txs, 1)

	parsedFirstTx := txs[0]
	err = parsedFirstTx.Verify(context.Background())
	require.ErrorIs(err, database.ErrNotFound)
}

func TestTxVerifyAfterGet(t *testing.T) {
	require := require.New(t)

	_, vm, ctx, issueTxs := setupIssueTx(t)
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()
	firstTx := issueTxs[1]
	secondTx := issueTxs[2]

	parsedSecondTx, err := vm.ParseTx(context.Background(), secondTx.Bytes())
	require.NoError(err)
	require.NoError(parsedSecondTx.Verify(context.Background()))
	_, err = vm.IssueTx(firstTx.Bytes())
	require.NoError(err)
	parsedFirstTx, err := vm.GetTx(context.Background(), firstTx.ID())
	require.NoError(err)
	require.NoError(parsedSecondTx.Accept(context.Background()))
	err = parsedFirstTx.Verify(context.Background())
	require.ErrorIs(err, database.ErrNotFound)
}

// Test issuing an import transaction.
func TestIssueImportTx(t *testing.T) {
	require := require.New(t)

	genesisBytes := BuildGenesisTest(t)

	issuer := make(chan common.Message, 1)
	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)

	m := atomic.NewMemory(prefixdb.New([]byte{0}, baseDBManager.Current().Database))

	ctx := NewContext(t)
	ctx.SharedMemory = m.NewSharedMemory(chainID)
	peerSharedMemory := m.NewSharedMemory(constants.PlatformChainID)

	genesisTx := GetCreateTxFromGenesisTest(t, genesisBytes, "AVAX")

	avaxID := genesisTx.ID()
	platformID := ids.Empty.Prefix(0)

	ctx.Lock.Lock()

	avmConfig := Config{
		IndexTransactions: true,
	}

	avmConfigBytes, err := stdjson.Marshal(avmConfig)
	require.NoError(err)
	vm := &VM{}
	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		baseDBManager.NewPrefixDBManager([]byte{1}),
		genesisBytes,
		nil,
		avmConfigBytes,
		issuer,
		[]*common.Fx{{
			ID: ids.Empty,
			Fx: &secp256k1fx.Fx{},
		}},
		nil,
	))

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

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
			BlockchainID: chainID,
			Outs: []*avax.TransferableOutput{{
				Asset: txAssetID,
				Out: &secp256k1fx.TransferOutput{
					Amt: 1000,
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
	require.NoError(tx.SignSECP256K1Fx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{key}}))

	_, err = vm.IssueTx(tx.Bytes())
	require.ErrorIs(err, database.ErrNotFound)

	// Provide the platform UTXO:

	utxo := &avax.UTXO{
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

	utxoBytes, err := vm.parser.Codec().Marshal(txs.CodecVersion, utxo)
	require.NoError(err)

	inputID := utxo.InputID()

	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			key.PublicKey().Address().Bytes(),
		},
	}}}}))

	_, err = vm.IssueTx(tx.Bytes())
	require.NoError(err)
	ctx.Lock.Unlock()

	require.Equal(common.PendingTxs, <-issuer)

	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	txs := vm.PendingTxs(context.Background())
	require.Len(txs, 1)

	parsedTx := txs[0]
	require.NoError(parsedTx.Verify(context.Background()))
	require.NoError(parsedTx.Accept(context.Background()))

	checkIndexedTX(t, vm.db, 0, key.PublicKey().Address(), txAssetID.AssetID(), parsedTx.ID())
	assertLatestIdx(t, vm.db, key.PublicKey().Address(), avaxID, 1)

	id := utxoID.InputID()
	_, err = vm.ctx.SharedMemory.Get(platformID, [][]byte{id[:]})
	require.ErrorIs(err, database.ErrNotFound)
}

// Test force accepting an import transaction.
func TestForceAcceptImportTx(t *testing.T) {
	require := require.New(t)

	genesisBytes := BuildGenesisTest(t)

	issuer := make(chan common.Message, 1)
	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)

	m := atomic.NewMemory(prefixdb.New([]byte{0}, baseDBManager.Current().Database))

	ctx := NewContext(t)
	ctx.SharedMemory = m.NewSharedMemory(chainID)

	platformID := ids.Empty.Prefix(0)

	vm := &VM{}
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()
	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		baseDBManager.NewPrefixDBManager([]byte{1}),
		genesisBytes,
		nil,
		nil,
		issuer,
		[]*common.Fx{{
			ID: ids.Empty,
			Fx: &secp256k1fx.Fx{},
		}},
		nil,
	))

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	key := keys[0]

	genesisTx := GetCreateTxFromGenesisTest(t, genesisBytes, "AVAX")

	utxoID := avax.UTXOID{
		TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		},
	}

	tx := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
		}},
		SourceChain: constants.PlatformChainID,
		ImportedIns: []*avax.TransferableInput{{
			UTXOID: utxoID,
			Asset:  avax.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt:   1000,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}},
	}}

	require.NoError(tx.SignSECP256K1Fx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{key}}))

	parsedTx, err := vm.ParseTx(context.Background(), tx.Bytes())
	require.NoError(err)

	err = parsedTx.Verify(context.Background())
	require.ErrorIs(err, database.ErrNotFound)

	require.NoError(parsedTx.Accept(context.Background()))

	id := utxoID.InputID()
	_, err = vm.ctx.SharedMemory.Get(platformID, [][]byte{id[:]})
	require.ErrorIs(err, database.ErrNotFound)
}

func TestImportTxNotState(t *testing.T) {
	require := require.New(t)

	intf := interface{}(&txs.ImportTx{})
	_, ok := intf.(verify.State)
	require.False(ok)
}

// Test issuing an import transaction.
func TestIssueExportTx(t *testing.T) {
	require := require.New(t)
	genesisBytes := BuildGenesisTest(t)

	issuer := make(chan common.Message, 1)
	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)

	m := atomic.NewMemory(prefixdb.New([]byte{0}, baseDBManager.Current().Database))

	ctx := NewContext(t)
	ctx.SharedMemory = m.NewSharedMemory(chainID)

	genesisTx := GetCreateTxFromGenesisTest(t, genesisBytes, "AVAX")

	avaxID := genesisTx.ID()

	ctx.Lock.Lock()
	vm := &VM{}
	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		baseDBManager.NewPrefixDBManager([]byte{1}),
		genesisBytes,
		nil,
		nil,
		issuer, []*common.Fx{{
			ID: ids.Empty,
			Fx: &secp256k1fx.Fx{},
		}},
		nil,
	))

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	key := keys[0]

	tx := &txs.Tx{Unsigned: &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        avaxID,
					OutputIndex: 2,
				},
				Asset: avax.Asset{ID: avaxID},
				In: &secp256k1fx.TransferInput{
					Amt:   startBalance,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
		}},
		DestinationChain: constants.PlatformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxID},
			Out: &secp256k1fx.TransferOutput{
				Amt: startBalance - vm.TxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}
	require.NoError(tx.SignSECP256K1Fx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{key}}))

	_, err := vm.IssueTx(tx.Bytes())
	require.NoError(err)

	ctx.Lock.Unlock()

	require.Equal(common.PendingTxs, <-issuer)

	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	txs := vm.PendingTxs(context.Background())
	require.Len(txs, 1)

	parsedTx := txs[0]
	require.NoError(parsedTx.Verify(context.Background()))
	require.NoError(parsedTx.Accept(context.Background()))

	peerSharedMemory := m.NewSharedMemory(constants.PlatformChainID)
	utxoBytes, _, _, err := peerSharedMemory.Indexed(
		vm.ctx.ChainID,
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

	genesisBytes := BuildGenesisTest(t)

	issuer := make(chan common.Message, 1)
	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)

	m := atomic.NewMemory(prefixdb.New([]byte{0}, baseDBManager.Current().Database))

	ctx := NewContext(t)
	ctx.SharedMemory = m.NewSharedMemory(chainID)

	genesisTx := GetCreateTxFromGenesisTest(t, genesisBytes, "AVAX")

	avaxID := genesisTx.ID()
	platformID := ids.Empty.Prefix(0)

	ctx.Lock.Lock()

	avmConfig := Config{
		IndexTransactions: true,
	}
	avmConfigBytes, err := stdjson.Marshal(avmConfig)
	require.NoError(err)
	vm := &VM{}
	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		baseDBManager.NewPrefixDBManager([]byte{1}),
		genesisBytes,
		nil,
		avmConfigBytes,
		issuer,
		[]*common.Fx{{
			ID: ids.Empty,
			Fx: &secp256k1fx.Fx{},
		}},
		nil,
	))

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	key := keys[0]

	assetID := avax.Asset{ID: avaxID}
	tx := &txs.Tx{Unsigned: &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        avaxID,
					OutputIndex: 2,
				},
				Asset: assetID,
				In: &secp256k1fx.TransferInput{
					Amt:   startBalance,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
		}},
		DestinationChain: constants.PlatformChainID,
		ExportedOuts: []*avax.TransferableOutput{{
			Asset: assetID,
			Out: &secp256k1fx.TransferOutput{
				Amt: startBalance - vm.TxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}
	require.NoError(tx.SignSECP256K1Fx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{key}}))

	_, err = vm.IssueTx(tx.Bytes())
	require.NoError(err)

	ctx.Lock.Unlock()

	require.Equal(common.PendingTxs, <-issuer)

	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	txs := vm.PendingTxs(context.Background())
	require.Len(txs, 1)

	parsedTx := txs[0]
	require.NoError(parsedTx.Verify(context.Background()))

	utxo := avax.UTXOID{
		TxID:        tx.ID(),
		OutputIndex: 0,
	}
	utxoID := utxo.InputID()

	peerSharedMemory := m.NewSharedMemory(platformID)
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {RemoveRequests: [][]byte{utxoID[:]}}}))

	require.NoError(parsedTx.Accept(context.Background()))

	checkIndexedTX(t, vm.db, 0, key.PublicKey().Address(), assetID.AssetID(), parsedTx.ID())
	assertLatestIdx(t, vm.db, key.PublicKey().Address(), assetID.AssetID(), 1)

	_, err = peerSharedMemory.Get(vm.ctx.ChainID, [][]byte{utxoID[:]})
	require.ErrorIs(err, database.ErrNotFound)
}
