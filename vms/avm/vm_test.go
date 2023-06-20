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
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/version"
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

	env := setup(t, &envConfig{
		isAVAXAsset: true,
	})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	newTx := NewTxWithAsset(t, env.genesisBytes, env.vm, "AVAX")

	txID, err := env.vm.IssueTx(newTx.Bytes())
	require.NoError(err)
	require.Equal(newTx.ID(), txID)
	env.vm.ctx.Lock.Unlock()

	require.Equal(common.PendingTxs, <-env.issuer)
	env.vm.ctx.Lock.Lock()

	require.Len(env.vm.PendingTxs(context.Background()), 1)
}

// Test issuing a transaction that consumes a currently pending UTXO. The
// transaction should be issued successfully.
func TestIssueDependentTx(t *testing.T) {
	require := require.New(t)

	issuer, vm, ctx, txs := setupIssueTx(t)
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	firstTx := txs[1]
	secondTx := txs[2]

	_, err := vm.IssueTx(firstTx.Bytes())
	require.NoError(err)

	_, err = vm.IssueTx(secondTx.Bytes())
	require.NoError(err)
	ctx.Lock.Unlock()

	require.Equal(common.PendingTxs, <-issuer)
	ctx.Lock.Lock()

	require.Len(vm.PendingTxs(context.Background()), 2)
}

// Test issuing a transaction that creates an NFT family
func TestIssueNFT(t *testing.T) {
	require := require.New(t)

	vm := &VM{}
	ctx := NewContext(t)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	m := atomic.NewMemory(prefixdb.New([]byte{0}, baseDBManager.Current().Database))
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	genesisBytes := BuildGenesisTest(t)
	issuer := make(chan common.Message, 1)
	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		baseDBManager,
		genesisBytes,
		nil,
		nil,
		issuer,
		[]*common.Fx{
			{
				ID: ids.Empty.Prefix(0),
				Fx: &secp256k1fx.Fx{},
			},
			{
				ID: ids.Empty.Prefix(1),
				Fx: &nftfx.Fx{},
			},
		},
		nil,
	))

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

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
	require.NoError(vm.parser.InitializeTx(createAssetTx))
	issueAndAccept(require, vm, issuer, createAssetTx)

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
	require.NoError(mintNFTTx.SignNFTFx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}}))
	issueAndAccept(require, vm, issuer, mintNFTTx)

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
	require.NoError(vm.parser.InitializeTx(transferNFTTx))
	issueAndAccept(require, vm, issuer, transferNFTTx)
}

// Test issuing a transaction that creates an Property family
func TestIssueProperty(t *testing.T) {
	require := require.New(t)
	vm := &VM{}
	ctx := NewContext(t)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	m := atomic.NewMemory(prefixdb.New([]byte{0}, baseDBManager.Current().Database))
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	genesisBytes := BuildGenesisTest(t)
	issuer := make(chan common.Message, 1)
	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		baseDBManager,
		genesisBytes,
		nil,
		nil,
		issuer,
		[]*common.Fx{
			{
				ID: ids.Empty.Prefix(0),
				Fx: &secp256k1fx.Fx{},
			},
			{
				ID: ids.Empty.Prefix(1),
				Fx: &nftfx.Fx{},
			},
			{
				ID: ids.Empty.Prefix(2),
				Fx: &propertyfx.Fx{},
			},
		},
		nil,
	))

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

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
	require.NoError(vm.parser.InitializeTx(createAssetTx))
	issueAndAccept(require, vm, issuer, createAssetTx)

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

	codec := vm.parser.Codec()
	require.NoError(mintPropertyTx.SignPropertyFx(codec, [][]*secp256k1.PrivateKey{
		{keys[0]},
	}))
	issueAndAccept(require, vm, issuer, mintPropertyTx)

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
	issueAndAccept(require, vm, issuer, burnPropertyTx)
}

func setupTxFeeAssets(tb testing.TB) ([]byte, chan common.Message, *VM, *atomic.Memory) {
	require := require.New(tb)

	addr0Str, _ := address.FormatBech32(constants.UnitTestHRP, addrs[0].Bytes())
	addr1Str, _ := address.FormatBech32(constants.UnitTestHRP, addrs[1].Bytes())
	addr2Str, _ := address.FormatBech32(constants.UnitTestHRP, addrs[2].Bytes())
	assetAlias := "asset1"
	customArgs := &BuildGenesisArgs{
		Encoding: formatting.Hex,
		GenesisData: map[string]AssetDefinition{
			assetAlias: {
				Name:   feeAssetName,
				Symbol: "TST",
				InitialState: map[string][]interface{}{
					"fixedCap": {
						Holder{
							Amount:  json.Uint64(startBalance),
							Address: addr0Str,
						},
						Holder{
							Amount:  json.Uint64(startBalance),
							Address: addr1Str,
						},
						Holder{
							Amount:  json.Uint64(startBalance),
							Address: addr2Str,
						},
					},
				},
			},
			"asset2": {
				Name:   otherAssetName,
				Symbol: "OTH",
				InitialState: map[string][]interface{}{
					"fixedCap": {
						Holder{
							Amount:  json.Uint64(startBalance),
							Address: addr0Str,
						},
						Holder{
							Amount:  json.Uint64(startBalance),
							Address: addr1Str,
						},
						Holder{
							Amount:  json.Uint64(startBalance),
							Address: addr2Str,
						},
					},
				},
			},
		},
	}
	genesisBytes, issuer, vm, m := GenesisVMWithArgs(tb, nil, customArgs)
	expectedID, err := vm.Aliaser.Lookup(assetAlias)
	require.NoError(err)
	require.Equal(expectedID, vm.feeAssetID)
	return genesisBytes, issuer, vm, m
}

func TestIssueTxWithFeeAsset(t *testing.T) {
	require := require.New(t)

	genesisBytes, issuer, vm, _ := setupTxFeeAssets(t)
	ctx := vm.ctx
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()
	// send first asset
	newTx := NewTxWithAsset(t, genesisBytes, vm, feeAssetName)

	txID, err := vm.IssueTx(newTx.Bytes())
	require.NoError(err)
	require.Equal(txID, newTx.ID())

	ctx.Lock.Unlock()

	msg := <-issuer
	require.Equal(msg, common.PendingTxs)

	ctx.Lock.Lock()
	txs := vm.PendingTxs(context.Background())
	require.Len(txs, 1)
	t.Log(txs)
}

func TestIssueTxWithAnotherAsset(t *testing.T) {
	require := require.New(t)

	genesisBytes, issuer, vm, _ := setupTxFeeAssets(t)
	ctx := vm.ctx
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	// send second asset
	feeAssetCreateTx := GetCreateTxFromGenesisTest(t, genesisBytes, feeAssetName)
	createTx := GetCreateTxFromGenesisTest(t, genesisBytes, otherAssetName)

	newTx := &txs.Tx{Unsigned: &txs.BaseTx{
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
	require.NoError(newTx.SignSECP256K1Fx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}, {keys[0]}}))

	txID, err := vm.IssueTx(newTx.Bytes())
	require.NoError(err)
	require.Equal(txID, newTx.ID())

	ctx.Lock.Unlock()

	msg := <-issuer
	require.Equal(msg, common.PendingTxs)

	ctx.Lock.Lock()
	txs := vm.PendingTxs(context.Background())
	require.Len(txs, 1)
}

func TestVMFormat(t *testing.T) {
	env := setup(t, &envConfig{
		isAVAXAsset: true,
	})
	defer func() {
		require.NoError(t, env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	tests := []struct {
		in       ids.ShortID
		expected string
	}{
		{ids.ShortEmpty, "X-testing1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqtu2yas"},
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

	env := setup(t, &envConfig{
		isAVAXAsset: true,
	})
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

	env := setup(t, &envConfig{
		isAVAXAsset: true,
	})
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

func TestImportTxSerialization(t *testing.T) {
	require := require.New(t)

	_, vm, _, _ := setupIssueTx(t)
	expected := []byte{
		// Codec version
		0x00, 0x00,
		// txID:
		0x00, 0x00, 0x00, 0x03,
		// networkID:
		0x00, 0x00, 0x00, 0x02,
		// blockchainID:
		0xff, 0xff, 0xff, 0xff, 0xee, 0xee, 0xee, 0xee,
		0xdd, 0xdd, 0xdd, 0xdd, 0xcc, 0xcc, 0xcc, 0xcc,
		0xbb, 0xbb, 0xbb, 0xbb, 0xaa, 0xaa, 0xaa, 0xaa,
		0x99, 0x99, 0x99, 0x99, 0x88, 0x88, 0x88, 0x88,
		// number of base outs:
		0x00, 0x00, 0x00, 0x00,
		// number of base inputs:
		0x00, 0x00, 0x00, 0x00,
		// Memo length:
		0x00, 0x00, 0x00, 0x04,
		// Memo:
		0x00, 0x01, 0x02, 0x03,
		// Source Chain ID:
		0x1f, 0x8f, 0x9f, 0x0f, 0x1e, 0x8e, 0x9e, 0x0e,
		0x2d, 0x7d, 0xad, 0xfd, 0x2c, 0x7c, 0xac, 0xfc,
		0x3b, 0x6b, 0xbb, 0xeb, 0x3a, 0x6a, 0xba, 0xea,
		0x49, 0x59, 0xc9, 0xd9, 0x48, 0x58, 0xc8, 0xd8,
		// number of inputs:
		0x00, 0x00, 0x00, 0x01,
		// utxoID:
		0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
		0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
		0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
		0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		// output index
		0x00, 0x00, 0x00, 0x00,
		// assetID:
		0x1f, 0x3f, 0x5f, 0x7f, 0x9e, 0xbe, 0xde, 0xfe,
		0x1d, 0x3d, 0x5d, 0x7d, 0x9c, 0xbc, 0xdc, 0xfc,
		0x1b, 0x3b, 0x5b, 0x7b, 0x9a, 0xba, 0xda, 0xfa,
		0x19, 0x39, 0x59, 0x79, 0x98, 0xb8, 0xd8, 0xf8,
		// input:
		// input ID:
		0x00, 0x00, 0x00, 0x05,
		// amount:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0xe8,
		// num sig indices:
		0x00, 0x00, 0x00, 0x01,
		// sig index[0]:
		0x00, 0x00, 0x00, 0x00,
		// number of credentials:
		0x00, 0x00, 0x00, 0x00,
	}

	tx := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID: 2,
			BlockchainID: ids.ID{
				0xff, 0xff, 0xff, 0xff, 0xee, 0xee, 0xee, 0xee,
				0xdd, 0xdd, 0xdd, 0xdd, 0xcc, 0xcc, 0xcc, 0xcc,
				0xbb, 0xbb, 0xbb, 0xbb, 0xaa, 0xaa, 0xaa, 0xaa,
				0x99, 0x99, 0x99, 0x99, 0x88, 0x88, 0x88, 0x88,
			},
			Memo: []byte{0x00, 0x01, 0x02, 0x03},
		}},
		SourceChain: ids.ID{
			0x1f, 0x8f, 0x9f, 0x0f, 0x1e, 0x8e, 0x9e, 0x0e,
			0x2d, 0x7d, 0xad, 0xfd, 0x2c, 0x7c, 0xac, 0xfc,
			0x3b, 0x6b, 0xbb, 0xeb, 0x3a, 0x6a, 0xba, 0xea,
			0x49, 0x59, 0xc9, 0xd9, 0x48, 0x58, 0xc8, 0xd8,
		},
		ImportedIns: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{TxID: ids.ID{
				0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
				0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
				0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
				0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
			}},
			Asset: avax.Asset{ID: ids.ID{
				0x1f, 0x3f, 0x5f, 0x7f, 0x9e, 0xbe, 0xde, 0xfe,
				0x1d, 0x3d, 0x5d, 0x7d, 0x9c, 0xbc, 0xdc, 0xfc,
				0x1b, 0x3b, 0x5b, 0x7b, 0x9a, 0xba, 0xda, 0xfa,
				0x19, 0x39, 0x59, 0x79, 0x98, 0xb8, 0xd8, 0xf8,
			}},
			In: &secp256k1fx.TransferInput{
				Amt:   1000,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}},
	}}

	require.NoError(vm.parser.InitializeTx(tx))
	require.Equal(tx.ID().String(), "9wdPb5rsThXYLX4WxkNeyYrNMfDE5cuWLgifSjxKiA2dCmgCZ")
	require.Equal(expected, tx.Bytes())

	credBytes := []byte{
		// type id
		0x00, 0x00, 0x00, 0x09,

		// there are two signers (thus two signatures)
		0x00, 0x00, 0x00, 0x02,

		// 65 bytes
		0x8c, 0xc7, 0xdc, 0x8c, 0x11, 0xd3, 0x75, 0x9e, 0x16, 0xa5,
		0x9f, 0xd2, 0x9c, 0x64, 0xd7, 0x1f, 0x9b, 0xad, 0x1a, 0x62,
		0x33, 0x98, 0xc7, 0xaf, 0x67, 0x02, 0xc5, 0xe0, 0x75, 0x8e,
		0x62, 0xcf, 0x15, 0x6d, 0x99, 0xf5, 0x4e, 0x71, 0xb8, 0xf4,
		0x8b, 0x5b, 0xbf, 0x0c, 0x59, 0x62, 0x79, 0x34, 0x97, 0x1a,
		0x1f, 0x49, 0x9b, 0x0a, 0x4f, 0xbf, 0x95, 0xfc, 0x31, 0x39,
		0x46, 0x4e, 0xa1, 0xaf, 0x00,

		// 65 bytes
		0x8c, 0xc7, 0xdc, 0x8c, 0x11, 0xd3, 0x75, 0x9e, 0x16, 0xa5,
		0x9f, 0xd2, 0x9c, 0x64, 0xd7, 0x1f, 0x9b, 0xad, 0x1a, 0x62,
		0x33, 0x98, 0xc7, 0xaf, 0x67, 0x02, 0xc5, 0xe0, 0x75, 0x8e,
		0x62, 0xcf, 0x15, 0x6d, 0x99, 0xf5, 0x4e, 0x71, 0xb8, 0xf4,
		0x8b, 0x5b, 0xbf, 0x0c, 0x59, 0x62, 0x79, 0x34, 0x97, 0x1a,
		0x1f, 0x49, 0x9b, 0x0a, 0x4f, 0xbf, 0x95, 0xfc, 0x31, 0x39,
		0x46, 0x4e, 0xa1, 0xaf, 0x00,

		// type id
		0x00, 0x00, 0x00, 0x09,

		// there are two signers (thus two signatures)
		0x00, 0x00, 0x00, 0x02,

		// 65 bytes
		0x8c, 0xc7, 0xdc, 0x8c, 0x11, 0xd3, 0x75, 0x9e, 0x16, 0xa5,
		0x9f, 0xd2, 0x9c, 0x64, 0xd7, 0x1f, 0x9b, 0xad, 0x1a, 0x62,
		0x33, 0x98, 0xc7, 0xaf, 0x67, 0x02, 0xc5, 0xe0, 0x75, 0x8e,
		0x62, 0xcf, 0x15, 0x6d, 0x99, 0xf5, 0x4e, 0x71, 0xb8, 0xf4,
		0x8b, 0x5b, 0xbf, 0x0c, 0x59, 0x62, 0x79, 0x34, 0x97, 0x1a,
		0x1f, 0x49, 0x9b, 0x0a, 0x4f, 0xbf, 0x95, 0xfc, 0x31, 0x39,
		0x46, 0x4e, 0xa1, 0xaf, 0x00,

		// 65 bytes
		0x8c, 0xc7, 0xdc, 0x8c, 0x11, 0xd3, 0x75, 0x9e, 0x16, 0xa5,
		0x9f, 0xd2, 0x9c, 0x64, 0xd7, 0x1f, 0x9b, 0xad, 0x1a, 0x62,
		0x33, 0x98, 0xc7, 0xaf, 0x67, 0x02, 0xc5, 0xe0, 0x75, 0x8e,
		0x62, 0xcf, 0x15, 0x6d, 0x99, 0xf5, 0x4e, 0x71, 0xb8, 0xf4,
		0x8b, 0x5b, 0xbf, 0x0c, 0x59, 0x62, 0x79, 0x34, 0x97, 0x1a,
		0x1f, 0x49, 0x9b, 0x0a, 0x4f, 0xbf, 0x95, 0xfc, 0x31, 0x39,
		0x46, 0x4e, 0xa1, 0xaf, 0x00,
	}
	require.NoError(tx.SignSECP256K1Fx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0], keys[0]}, {keys[0], keys[0]}}))
	require.Equal(tx.ID().String(), "pCW7sVBytzdZ1WrqzGY1DvA2S9UaMr72xpUMxVyx1QHBARNYx")

	// there are two credentials
	expected[len(expected)-1] = 0x02
	expected = append(expected, credBytes...)
	require.Equal(expected, tx.Bytes())
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

	assertIndexedTX(t, vm.db, 0, key.PublicKey().Address(), txAssetID.AssetID(), parsedTx.ID())
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

	assertIndexedTX(t, vm.db, 0, key.PublicKey().Address(), assetID.AssetID(), parsedTx.ID())
	assertLatestIdx(t, vm.db, key.PublicKey().Address(), assetID.AssetID(), 1)

	_, err = peerSharedMemory.Get(vm.ctx.ChainID, [][]byte{utxoID[:]})
	require.ErrorIs(err, database.ErrNotFound)
}
