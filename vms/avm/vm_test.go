// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"math"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
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
	ctx := newContext(t)
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
	ctx := newContext(t)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	genesisBytes := buildGenesisTest(t)
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
	ctx := newContext(t)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	genesisBytes := buildGenesisTest(t)
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

	tx := newTx(t, env.genesisBytes, env.vm, "AVAX")
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
	tx := newTx(t, env.genesisBytes, env.vm, feeAssetName)
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
	feeAssetCreateTx := getCreateTxFromGenesisTest(t, env.genesisBytes, feeAssetName)
	createTx := getCreateTxFromGenesisTest(t, env.genesisBytes, otherAssetName)

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

	newTx := newTx(t, env.genesisBytes, env.vm, "AVAX")
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

	newTx := newTx(t, env.genesisBytes, env.vm, "AVAX")
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

func TestTxAcceptAfterParseTx(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		notLinearized: true,
	})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	key := keys[0]
	firstTx := &txs.Tx{Unsigned: &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        env.genesisTx.ID(),
					OutputIndex: 2,
				},
				Asset: avax.Asset{ID: env.genesisTx.ID()},
				In: &secp256k1fx.TransferInput{
					Amt: startBalance,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{
							0,
						},
					},
				},
			}},
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: env.genesisTx.ID()},
				Out: &secp256k1fx.TransferOutput{
					Amt: startBalance - env.vm.TxFee,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{key.PublicKey().Address()},
					},
				},
			}},
		},
	}}
	require.NoError(firstTx.SignSECP256K1Fx(env.vm.parser.Codec(), [][]*secp256k1.PrivateKey{{key}}))

	secondTx := &txs.Tx{Unsigned: &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
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

	firstTxStatus, err := env.vm.state.GetStatus(firstTx.ID())
	require.NoError(err)
	require.Equal(choices.Accepted, firstTxStatus)

	secondTxStatus, err := env.vm.state.GetStatus(secondTx.ID())
	require.NoError(err)
	require.Equal(choices.Accepted, secondTxStatus)
}

// Test issuing an import transaction.
func TestIssueImportTx(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		vmStaticConfig: &config.Config{},
	})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	peerSharedMemory := env.sharedMemory.NewSharedMemory(constants.PlatformChainID)

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
	require.NoError(tx.SignSECP256K1Fx(env.vm.parser.Codec(), [][]*secp256k1.PrivateKey{{key}}))

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

	utxoBytes, err := env.vm.parser.Codec().Marshal(txs.CodecVersion, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
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

	issueAndAccept(require, env.vm, env.issuer, tx)

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
		vmStaticConfig: &config.Config{},
		notLinearized:  true,
	})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

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

	env := setup(t, &envConfig{})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	genesisTx := getCreateTxFromGenesisTest(t, env.genesisBytes, "AVAX")
	avaxID := genesisTx.ID()

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
				Amt: startBalance - env.vm.TxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}
	require.NoError(tx.SignSECP256K1Fx(env.vm.parser.Codec(), [][]*secp256k1.PrivateKey{{key}}))

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

	issueAndAccept(require, env.vm, env.issuer, tx)

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

	env := setup(t, &envConfig{})
	defer func() {
		require.NoError(env.vm.Shutdown(context.Background()))
		env.vm.ctx.Lock.Unlock()
	}()

	genesisTx := getCreateTxFromGenesisTest(t, env.genesisBytes, "AVAX")
	avaxID := genesisTx.ID()

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
				Amt: startBalance - env.vm.TxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}
	require.NoError(tx.SignSECP256K1Fx(env.vm.parser.Codec(), [][]*secp256k1.PrivateKey{{key}}))

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

	_, err := peerSharedMemory.Get(env.vm.ctx.ChainID, [][]byte{utxoID[:]})
	require.ErrorIs(err, database.ErrNotFound)

	issueAndAccept(require, env.vm, env.issuer, tx)

	_, err = peerSharedMemory.Get(env.vm.ctx.ChainID, [][]byte{utxoID[:]})
	require.ErrorIs(err, database.ErrNotFound)

	assertIndexedTX(t, env.vm.db, 0, key.PublicKey().Address(), assetID.AssetID(), tx.ID())
	assertLatestIdx(t, env.vm.db, key.PublicKey().Address(), assetID.AssetID(), 1)
}
