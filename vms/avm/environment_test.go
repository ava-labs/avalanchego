// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"errors"
	"math/rand"
	"testing"

	stdjson "encoding/json"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/avm/blocks/executor"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	keystoreutils "github.com/ava-labs/avalanchego/vms/components/keystore"
)

var (
	testChangeAddr = ids.GenerateTestShortID()
	testCases      = []struct {
		name      string
		avaxAsset bool
	}{
		{
			name:      "genesis asset is AVAX",
			avaxAsset: true,
		},
		{
			name:      "genesis asset is TEST",
			avaxAsset: false,
		},
	}
)

var (
	chainID      = ids.ID{5, 4, 3, 2, 1}
	testTxFee    = uint64(1000)
	startBalance = uint64(50000)

	keys  []*secp256k1.PrivateKey
	addrs []ids.ShortID // addrs[i] corresponds to keys[i]

	assetID        = ids.ID{1, 2, 3}
	username       = "bobby"
	password       = "StrnasfqewiurPasswdn56d" // #nosec G101
	feeAssetName   = "TEST"
	otherAssetName = "OTHER"

	errMissing = errors.New("missing")
)

func init() {
	factory := secp256k1.Factory{}

	for _, key := range []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
	} {
		keyBytes, _ := cb58.Decode(key)
		pk, _ := factory.ToPrivateKey(keyBytes)
		keys = append(keys, pk)
		addrs = append(addrs, pk.PublicKey().Address())
	}
}

type user struct {
	username    string
	password    string
	initialKeys []*secp256k1.PrivateKey
}

type envConfig struct {
	isCustomFeeAsset bool
	keystoreUsers    []*user
	vmStaticConfig   *config.Config
	vmDynamicConfig  *Config
	additionalFxs    []*common.Fx
}

type environment struct {
	genesisBytes  []byte
	genesisTx     *txs.Tx
	sharedMemory  *atomic.Memory
	issuer        chan common.Message
	vm            *VM
	service       *Service
	walletService *WalletService
}

// setup the testing environment
func setup(tb testing.TB, c *envConfig) *environment {
	require := require.New(tb)

	var (
		genesisArgs *BuildGenesisArgs
		assetName   = "AVAX"
	)
	if c.isCustomFeeAsset {
		genesisArgs = makeCustomAssetGenesis(tb)
		assetName = feeAssetName
	} else {
		genesisArgs = makeDefaultGenesis(tb)
	}

	genesisBytes := BuildGenesisTestWithArgs(tb, genesisArgs)
	ctx := NewContext(tb)

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)

	m := atomic.NewMemory(prefixdb.New([]byte{0}, baseDBManager.Current().Database))
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	userKeystore, err := keystore.CreateTestKeystore()
	require.NoError(err)
	ctx.Keystore = userKeystore.NewBlockchainKeyStore(ctx.ChainID)

	for _, user := range c.keystoreUsers {
		require.NoError(userKeystore.CreateUser(user.username, user.password))

		// Import the initially funded private keys
		keystoreUser, err := keystoreutils.NewUserFromKeystore(ctx.Keystore, user.username, user.password)
		require.NoError(err)

		require.NoError(keystoreUser.PutKeys(user.initialKeys...))
		require.NoError(keystoreUser.Close())
	}

	vmStaticConfig := config.Config{
		TxFee:            testTxFee,
		CreateAssetTxFee: testTxFee,
	}
	if c.vmStaticConfig != nil {
		vmStaticConfig = *c.vmStaticConfig
	}

	vm := &VM{
		Config: vmStaticConfig,
	}

	vmDynamicConfig := Config{
		IndexTransactions: true,
	}
	if c.vmDynamicConfig != nil {
		vmDynamicConfig = *c.vmDynamicConfig
	}
	configBytes, err := stdjson.Marshal(vmDynamicConfig)
	require.NoError(err)

	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		baseDBManager.NewPrefixDBManager([]byte{1}),
		genesisBytes,
		nil,
		configBytes,
		nil,
		append(
			[]*common.Fx{
				{
					ID: secp256k1fx.ID,
					Fx: &secp256k1fx.Fx{},
				},
				{
					ID: nftfx.ID,
					Fx: &nftfx.Fx{},
				},
			},
			c.additionalFxs...,
		),
		&common.SenderTest{},
	))

	stopVertexID := ids.GenerateTestID()
	issuer := make(chan common.Message, 1)

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.Linearize(context.Background(), stopVertexID, issuer))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	return &environment{
		genesisBytes: genesisBytes,
		genesisTx:    GetCreateTxFromGenesisTest(tb, genesisBytes, assetName),
		sharedMemory: m,
		issuer:       issuer,
		vm:           vm,
		service: &Service{
			vm: vm,
		},
		walletService: &WalletService{
			vm:         vm,
			pendingTxs: linkedhashmap.New[ids.ID, *txs.Tx](),
		},
	}
}

func GenesisVMWithArgs(tb testing.TB, additionalFxs []*common.Fx, args *BuildGenesisArgs) ([]byte, chan common.Message, *VM, *atomic.Memory) {
	require := require.New(tb)

	var genesisBytes []byte

	if args != nil {
		genesisBytes = BuildGenesisTestWithArgs(tb, args)
	} else {
		genesisBytes = BuildGenesisTest(tb)
	}

	ctx := NewContext(tb)

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)

	m := atomic.NewMemory(prefixdb.New([]byte{0}, baseDBManager.Current().Database))
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	userKeystore, err := keystore.CreateTestKeystore()
	require.NoError(err)
	require.NoError(userKeystore.CreateUser(username, password))
	ctx.Keystore = userKeystore.NewBlockchainKeyStore(ctx.ChainID)

	txIssuer := make(chan common.Message, 1)
	vm := &VM{Config: config.Config{
		TxFee:            testTxFee,
		CreateAssetTxFee: testTxFee,
	}}
	configBytes, err := stdjson.Marshal(Config{IndexTransactions: true})
	require.NoError(err)

	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		baseDBManager.NewPrefixDBManager([]byte{1}),
		genesisBytes,
		nil,
		configBytes,
		txIssuer,
		append(
			[]*common.Fx{
				{
					ID: ids.Empty,
					Fx: &secp256k1fx.Fx{},
				},
				{
					ID: nftfx.ID,
					Fx: &nftfx.Fx{},
				},
			},
			additionalFxs...,
		),
		&common.SenderTest{},
	))

	stopVertexID := ids.GenerateTestID()
	blkIssuer := make(chan common.Message, 1)

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))
	require.NoError(vm.Linearize(context.Background(), stopVertexID, blkIssuer))

	return genesisBytes, blkIssuer, vm, m
}

func NewContext(tb testing.TB) *snow.Context {
	require := require.New(tb)

	genesisBytes := BuildGenesisTest(tb)

	tx := GetCreateTxFromGenesisTest(tb, genesisBytes, "AVAX")

	ctx := snow.DefaultContextTest()
	ctx.NetworkID = constants.UnitTestID
	ctx.ChainID = chainID
	ctx.AVAXAssetID = tx.ID()
	ctx.XChainID = ids.Empty.Prefix(0)
	ctx.CChainID = ids.Empty.Prefix(1)
	aliaser := ctx.BCLookup.(ids.Aliaser)

	require.NoError(aliaser.Alias(chainID, "X"))
	require.NoError(aliaser.Alias(chainID, chainID.String()))
	require.NoError(aliaser.Alias(constants.PlatformChainID, "P"))
	require.NoError(aliaser.Alias(constants.PlatformChainID, constants.PlatformChainID.String()))

	ctx.ValidatorState = &validators.TestState{
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			subnetID, ok := map[ids.ID]ids.ID{
				constants.PlatformChainID: ctx.SubnetID,
				chainID:                   ctx.SubnetID,
			}[chainID]
			if !ok {
				return ids.Empty, errMissing
			}
			return subnetID, nil
		},
	}
	return ctx
}

// Returns:
//
//  1. tx in genesis that creates asset
//  2. the index of the output
func GetCreateTxFromGenesisTest(tb testing.TB, genesisBytes []byte, assetName string) *txs.Tx {
	require := require.New(tb)
	parser, err := txs.NewParser([]fxs.Fx{
		&secp256k1fx.Fx{},
	})
	require.NoError(err)

	cm := parser.GenesisCodec()
	genesis := Genesis{}
	_, err = cm.Unmarshal(genesisBytes, &genesis)
	require.NoError(err)

	require.NotEmpty(genesis.Txs)

	var assetTx *GenesisAsset
	for _, tx := range genesis.Txs {
		if tx.Name == assetName {
			assetTx = tx
			break
		}
	}
	require.NotNil(assetTx)

	tx := &txs.Tx{
		Unsigned: &assetTx.CreateAssetTx,
	}
	require.NoError(parser.InitializeGenesisTx(tx))
	return tx
}

// BuildGenesisTest is the common Genesis builder for most tests
func BuildGenesisTest(tb testing.TB) []byte {
	defaultArgs := makeDefaultGenesis(tb)
	return BuildGenesisTestWithArgs(tb, defaultArgs)
}

// BuildGenesisTestWithArgs allows building the genesis while injecting different starting points (args)
func BuildGenesisTestWithArgs(tb testing.TB, args *BuildGenesisArgs) []byte {
	require := require.New(tb)

	ss := CreateStaticService()

	reply := BuildGenesisReply{}
	require.NoError(ss.BuildGenesis(nil, args, &reply))

	b, err := formatting.Decode(reply.Encoding, reply.Bytes)
	require.NoError(err)
	return b
}

func NewTxWithAsset(tb testing.TB, genesisBytes []byte, vm *VM, assetName string) *txs.Tx {
	require := require.New(tb)

	createTx := GetCreateTxFromGenesisTest(tb, genesisBytes, assetName)

	newTx := &txs.Tx{Unsigned: &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Ins: []*avax.TransferableInput{{
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
			}},
		},
	}}
	require.NoError(newTx.SignSECP256K1Fx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}}))
	return newTx
}

func setupIssueTx(tb testing.TB) (chan common.Message, *VM, *snow.Context, []*txs.Tx) {
	require := require.New(tb)

	genesisBytes, issuer, vm, _ := GenesisVMWithArgs(tb, nil, nil)
	ctx := vm.ctx

	avaxTx := GetCreateTxFromGenesisTest(tb, genesisBytes, "AVAX")
	key := keys[0]
	firstTx := &txs.Tx{Unsigned: &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        avaxTx.ID(),
					OutputIndex: 2,
				},
				Asset: avax.Asset{ID: avaxTx.ID()},
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
				Asset: avax.Asset{ID: avaxTx.ID()},
				Out: &secp256k1fx.TransferOutput{
					Amt: startBalance - vm.TxFee,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{key.PublicKey().Address()},
					},
				},
			}},
		},
	}}
	require.NoError(firstTx.SignSECP256K1Fx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{key}}))

	secondTx := &txs.Tx{Unsigned: &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        avaxTx.ID(),
					OutputIndex: 2,
				},
				Asset: avax.Asset{ID: avaxTx.ID()},
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
				Asset: avax.Asset{ID: avaxTx.ID()},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{key.PublicKey().Address()},
					},
				},
			}},
		},
	}}
	require.NoError(secondTx.SignSECP256K1Fx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{key}}))
	return issuer, vm, ctx, []*txs.Tx{avaxTx, firstTx, secondTx}
}

// Sample from a set of addresses and return them raw and formatted as strings.
// The size of the sample is between 1 and len(addrs)
// If len(addrs) == 0, returns nil
func sampleAddrs(tb testing.TB, vm *VM, addrs []ids.ShortID) ([]ids.ShortID, []string) {
	require := require.New(tb)

	sampledAddrs := []ids.ShortID{}
	sampledAddrsStr := []string{}

	sampler := sampler.NewUniform()
	sampler.Initialize(uint64(len(addrs)))

	numAddrs := 1 + rand.Intn(len(addrs)) // #nosec G404
	indices, err := sampler.Sample(numAddrs)
	require.NoError(err)
	for _, index := range indices {
		addr := addrs[index]
		addrStr, err := vm.FormatLocalAddress(addr)
		require.NoError(err)

		sampledAddrs = append(sampledAddrs, addr)
		sampledAddrsStr = append(sampledAddrsStr, addrStr)
	}
	return sampledAddrs, sampledAddrsStr
}

func makeDefaultGenesis(tb testing.TB) *BuildGenesisArgs {
	require := require.New(tb)

	addr0Str, err := address.FormatBech32(constants.UnitTestHRP, addrs[0].Bytes())
	require.NoError(err)

	addr1Str, err := address.FormatBech32(constants.UnitTestHRP, addrs[1].Bytes())
	require.NoError(err)

	addr2Str, err := address.FormatBech32(constants.UnitTestHRP, addrs[2].Bytes())
	require.NoError(err)

	return &BuildGenesisArgs{
		Encoding: formatting.Hex,
		GenesisData: map[string]AssetDefinition{
			"asset1": {
				Name:   "AVAX",
				Symbol: "SYMB",
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
				Name:   "myVarCapAsset",
				Symbol: "MVCA",
				InitialState: map[string][]interface{}{
					"variableCap": {
						Owners{
							Threshold: 1,
							Minters: []string{
								addr0Str,
								addr1Str,
							},
						},
						Owners{
							Threshold: 2,
							Minters: []string{
								addr0Str,
								addr1Str,
								addr2Str,
							},
						},
					},
				},
			},
			"asset3": {
				Name: "myOtherVarCapAsset",
				InitialState: map[string][]interface{}{
					"variableCap": {
						Owners{
							Threshold: 1,
							Minters: []string{
								addr0Str,
							},
						},
					},
				},
			},
			"asset4": {
				Name: "myFixedCapAsset",
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
					},
				},
			},
		},
	}
}

func makeCustomAssetGenesis(tb testing.TB) *BuildGenesisArgs {
	require := require.New(tb)

	addr0Str, err := address.FormatBech32(constants.UnitTestHRP, addrs[0].Bytes())
	require.NoError(err)

	addr1Str, err := address.FormatBech32(constants.UnitTestHRP, addrs[1].Bytes())
	require.NoError(err)

	addr2Str, err := address.FormatBech32(constants.UnitTestHRP, addrs[2].Bytes())
	require.NoError(err)

	return &BuildGenesisArgs{
		Encoding: formatting.Hex,
		GenesisData: map[string]AssetDefinition{
			"asset1": {
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
}

// issueAndAccept expects the context lock to be held
func issueAndAccept(
	require *require.Assertions,
	vm *VM,
	issuer <-chan common.Message,
	tx *txs.Tx,
) {
	txID, err := vm.IssueTx(tx.Bytes())
	require.NoError(err)
	require.Equal(tx.ID(), txID)

	buildAndAccept(require, vm, issuer, txID)
}

// buildAndAccept expects the context lock to be held
func buildAndAccept(
	require *require.Assertions,
	vm *VM,
	issuer <-chan common.Message,
	txID ids.ID,
) {
	vm.ctx.Lock.Unlock()
	require.Equal(common.PendingTxs, <-issuer)
	vm.ctx.Lock.Lock()

	blkIntf, err := vm.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&executor.Block{}, blkIntf)

	blk := blkIntf.(*executor.Block)
	txs := blk.Txs()
	require.Len(txs, 1)

	issuedTx := txs[0]
	require.Equal(txID, issuedTx.ID())
	require.NoError(blk.Verify(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), blk.ID()))
	require.NoError(blk.Accept(context.Background()))
}
