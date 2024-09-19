// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/vms/avm/block/executor"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/txstest"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	avajson "github.com/ava-labs/avalanchego/utils/json"
	keystoreutils "github.com/ava-labs/avalanchego/vms/components/keystore"
)

const (
	testTxFee    uint64 = 1000
	startBalance uint64 = 50000

	username       = "bobby"
	password       = "StrnasfqewiurPasswdn56d" //#nosec G101
	feeAssetName   = "TEST"
	otherAssetName = "OTHER"
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

	assetID = ids.ID{1, 2, 3}

	keys  = secp256k1.TestKeys()[:3] // TODO: Remove [:3]
	addrs []ids.ShortID              // addrs[i] corresponds to keys[i]
)

func init() {
	addrs = make([]ids.ShortID, len(keys))
	for i, key := range keys {
		addrs[i] = key.Address()
	}
}

type user struct {
	username    string
	password    string
	initialKeys []*secp256k1.PrivateKey
}

type envConfig struct {
	fork             upgradetest.Fork
	isCustomFeeAsset bool
	keystoreUsers    []*user
	vmStaticConfig   *config.Config
	vmDynamicConfig  *Config
	additionalFxs    []*common.Fx
	notLinearized    bool
	notBootstrapped  bool
}

type environment struct {
	genesisBytes []byte
	genesisTx    *txs.Tx
	sharedMemory *atomic.Memory
	issuer       chan common.Message
	vm           *VM
	txBuilder    *txstest.Builder
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

	genesisBytes := buildGenesisTestWithArgs(tb, genesisArgs)

	ctx := snowtest.Context(tb, snowtest.XChainID)

	baseDB := memdb.New()
	m := atomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	userKeystore := keystore.New(logging.NoLog{}, memdb.New())
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
		Upgrades:         upgradetest.GetConfig(c.fork),
		TxFee:            testTxFee,
		CreateAssetTxFee: testTxFee,
	}
	if c.vmStaticConfig != nil {
		vmStaticConfig = *c.vmStaticConfig
	}

	vm := &VM{
		Config: vmStaticConfig,
	}

	vmDynamicConfig := DefaultConfig
	vmDynamicConfig.IndexTransactions = true
	if c.vmDynamicConfig != nil {
		vmDynamicConfig = *c.vmDynamicConfig
	}
	configBytes, err := json.Marshal(vmDynamicConfig)
	require.NoError(err)

	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		prefixdb.New([]byte{1}, baseDB),
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
		&enginetest.Sender{},
	))

	stopVertexID := ids.GenerateTestID()
	issuer := make(chan common.Message, 1)

	env := &environment{
		genesisBytes: genesisBytes,
		genesisTx:    getCreateTxFromGenesisTest(tb, genesisBytes, assetName),
		sharedMemory: m,
		issuer:       issuer,
		vm:           vm,
		txBuilder:    txstest.New(vm.parser.Codec(), vm.ctx, &vm.Config, vm.feeAssetID, vm.state),
	}

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	if c.notLinearized {
		return env
	}

	require.NoError(vm.Linearize(context.Background(), stopVertexID, issuer))
	if c.notBootstrapped {
		return env
	}

	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	tb.Cleanup(func() {
		env.vm.ctx.Lock.Lock()
		defer env.vm.ctx.Lock.Unlock()

		require.NoError(env.vm.Shutdown(context.Background()))
	})

	return env
}

// Returns:
//
//  1. tx in genesis that creates asset
//  2. the index of the output
func getCreateTxFromGenesisTest(tb testing.TB, genesisBytes []byte, assetName string) *txs.Tx {
	require := require.New(tb)

	parser, err := txs.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
		},
	)
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
	require.NoError(tx.Initialize(parser.GenesisCodec()))
	return tx
}

// buildGenesisTest is the common Genesis builder for most tests
func buildGenesisTest(tb testing.TB) []byte {
	defaultArgs := makeDefaultGenesis(tb)
	return buildGenesisTestWithArgs(tb, defaultArgs)
}

// buildGenesisTestWithArgs allows building the genesis while injecting different starting points (args)
func buildGenesisTestWithArgs(tb testing.TB, args *BuildGenesisArgs) []byte {
	require := require.New(tb)

	ss := CreateStaticService()

	reply := BuildGenesisReply{}
	require.NoError(ss.BuildGenesis(nil, args, &reply))

	b, err := formatting.Decode(reply.Encoding, reply.Bytes)
	require.NoError(err)
	return b
}

func newTx(tb testing.TB, genesisBytes []byte, chainID ids.ID, parser txs.Parser, assetName string) *txs.Tx {
	require := require.New(tb)

	createTx := getCreateTxFromGenesisTest(tb, genesisBytes, assetName)
	tx := &txs.Tx{Unsigned: &txs.BaseTx{
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
	require.NoError(tx.SignSECP256K1Fx(parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}}))
	return tx
}

// Sample from a set of addresses and return them raw and formatted as strings.
// The size of the sample is between 1 and len(addrs)
// If len(addrs) == 0, returns nil
func sampleAddrs(tb testing.TB, addressFormatter avax.AddressManager, addrs []ids.ShortID) ([]ids.ShortID, []string) {
	require := require.New(tb)

	sampledAddrs := []ids.ShortID{}
	sampledAddrsStr := []string{}

	sampler := sampler.NewUniform()
	sampler.Initialize(uint64(len(addrs)))

	numAddrs := 1 + rand.Intn(len(addrs)) // #nosec G404
	indices, ok := sampler.Sample(numAddrs)
	require.True(ok)
	for _, index := range indices {
		addr := addrs[index]
		addrStr, err := addressFormatter.FormatLocalAddress(addr)
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
							Amount:  avajson.Uint64(startBalance),
							Address: addr0Str,
						},
						Holder{
							Amount:  avajson.Uint64(startBalance),
							Address: addr1Str,
						},
						Holder{
							Amount:  avajson.Uint64(startBalance),
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
							Amount:  avajson.Uint64(startBalance),
							Address: addr0Str,
						},
						Holder{
							Amount:  avajson.Uint64(startBalance),
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
							Amount:  avajson.Uint64(startBalance),
							Address: addr0Str,
						},
						Holder{
							Amount:  avajson.Uint64(startBalance),
							Address: addr1Str,
						},
						Holder{
							Amount:  avajson.Uint64(startBalance),
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
							Amount:  avajson.Uint64(startBalance),
							Address: addr0Str,
						},
						Holder{
							Amount:  avajson.Uint64(startBalance),
							Address: addr1Str,
						},
						Holder{
							Amount:  avajson.Uint64(startBalance),
							Address: addr2Str,
						},
					},
				},
			},
		},
	}
}

// issueAndAccept expects the context lock not to be held
func issueAndAccept(
	require *require.Assertions,
	vm *VM,
	issuer <-chan common.Message,
	tx *txs.Tx,
) {
	txID, err := vm.issueTxFromRPC(tx)
	require.NoError(err)
	require.Equal(tx.ID(), txID)

	buildAndAccept(require, vm, issuer, txID)
}

// buildAndAccept expects the context lock not to be held
func buildAndAccept(
	require *require.Assertions,
	vm *VM,
	issuer <-chan common.Message,
	txID ids.ID,
) {
	require.Equal(common.PendingTxs, <-issuer)

	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

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
