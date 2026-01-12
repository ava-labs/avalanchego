// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/txpool"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/eth"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/ethclient"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/node"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/config"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/extension"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/deployerallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/rpc"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	warpcontract "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	avagoconstants "github.com/ava-labs/avalanchego/utils/constants"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

const delegateCallPrecompileCode = "6080604052348015600e575f5ffd5b506106608061001c5f395ff3fe608060405234801561000f575f5ffd5b506004361061003f575f3560e01c80638b336b5e14610043578063b771b3bc14610061578063e4246eec1461007f575b5f5ffd5b61004b61009d565b604051610058919061029e565b60405180910390f35b610069610256565b6040516100769190610331565b60405180910390f35b61008761026e565b604051610094919061036a565b60405180910390f35b5f5f6040516020016100ae906103dd565b60405160208183030381529060405290505f63ee5b48eb60e01b826040516024016100d9919061046b565b604051602081830303815290604052907bffffffffffffffffffffffffffffffffffffffffffffffffffffffff19166020820180517bffffffffffffffffffffffffffffffffffffffffffffffffffffffff838183161783525050505090505f5f73020000000000000000000000000000000000000573ffffffffffffffffffffffffffffffffffffffff168360405161017391906104c5565b5f60405180830381855af49150503d805f81146101ab576040519150601f19603f3d011682016040523d82523d5f602084013e6101b0565b606091505b5091509150816101f5576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101ec9061054b565b60405180910390fd5b808060200190518101906102099190610597565b94505f5f1b850361024f576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016102469061060c565b60405180910390fd5b5050505090565b73020000000000000000000000000000000000000581565b73020000000000000000000000000000000000000581565b5f819050919050565b61029881610286565b82525050565b5f6020820190506102b15f83018461028f565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f819050919050565b5f6102f96102f46102ef846102b7565b6102d6565b6102b7565b9050919050565b5f61030a826102df565b9050919050565b5f61031b82610300565b9050919050565b61032b81610311565b82525050565b5f6020820190506103445f830184610322565b92915050565b5f610354826102b7565b9050919050565b6103648161034a565b82525050565b5f60208201905061037d5f83018461035b565b92915050565b5f82825260208201905092915050565b7f68656c6c6f0000000000000000000000000000000000000000000000000000005f82015250565b5f6103c7600583610383565b91506103d282610393565b602082019050919050565b5f6020820190508181035f8301526103f4816103bb565b9050919050565b5f81519050919050565b5f82825260208201905092915050565b8281835e5f83830152505050565b5f601f19601f8301169050919050565b5f61043d826103fb565b6104478185610405565b9350610457818560208601610415565b61046081610423565b840191505092915050565b5f6020820190508181035f8301526104838184610433565b905092915050565b5f81905092915050565b5f61049f826103fb565b6104a9818561048b565b93506104b9818560208601610415565b80840191505092915050565b5f6104d08284610495565b915081905092915050565b7f44656c65676174652063616c6c20746f2073656e64576172704d6573736167655f8201527f206661696c656400000000000000000000000000000000000000000000000000602082015250565b5f610535602783610383565b9150610540826104db565b604082019050919050565b5f6020820190508181035f83015261056281610529565b9050919050565b5f5ffd5b61057681610286565b8114610580575f5ffd5b50565b5f815190506105918161056d565b92915050565b5f602082840312156105ac576105ab610569565b5b5f6105b984828501610583565b91505092915050565b7f4661696c656420746f2073656e642077617270206d65737361676500000000005f82015250565b5f6105f6601b83610383565b9150610601826105c2565b602082019050919050565b5f6020820190508181035f830152610623816105ea565b905091905056fea2646970667358221220192acba01cff6d70ce187c63c7ccac116d811f6c35e316fde721f14929ced12564736f6c634300081e0033"

func TestMain(m *testing.M) {
	RegisterAllLibEVMExtras()
	os.Exit(m.Run())
}

var (
	schemes = []string{rawdb.HashScheme, customrawdb.FirewoodScheme}

	testNetworkID uint32 = avagoconstants.UnitTestID

	testMinGasPrice int64            = 225_000_000_000
	testKeys                         = secp256k1.TestKeys()[:3]
	testEthAddrs    []common.Address // testEthAddrs[i] corresponds to testKeys[i]

	firstTxAmount = new(big.Int).Mul(big.NewInt(testMinGasPrice), big.NewInt(21000*100))

	toGenesisJSON = func(cfg *params.ChainConfig) string {
		g := new(core.Genesis)
		g.Difficulty = big.NewInt(0)
		g.GasLimit = 8000000
		g.Timestamp = uint64(upgrade.InitiallyActiveTime.Unix())

		// Use chainId: 43111, so that it does not overlap with any Avalanche ChainIDs, which may have their
		// config overridden in vm.Initialize.
		cpy := *cfg
		cpy.ChainID = big.NewInt(43111)
		g.Config = &cpy

		// Create allocation for the test addresses
		g.Alloc = make(types.GenesisAlloc)
		for _, addr := range testEthAddrs {
			balance := new(big.Int)
			balance.SetString("0x4192927743b88000", 0)
			g.Alloc[addr] = types.Account{
				Balance: balance,
			}
		}

		b, err := json.Marshal(g)
		if err != nil {
			panic(err)
		}
		return string(b)
	}

	// These will be initialized after init() runs
	genesisJSONPreSubnetEVM string
	genesisJSONSubnetEVM    string
)

func init() {
	for _, key := range testKeys {
		testEthAddrs = append(testEthAddrs, key.EthAddress())
	}

	genesisJSONPreSubnetEVM = toGenesisJSON(params.TestPreSubnetEVMChainConfig)
	genesisJSONSubnetEVM = toGenesisJSON(params.TestSubnetEVMChainConfig)
}

type testVMConfig struct {
	isSyncing bool
	fork      *upgradetest.Fork
	// If genesisJSON is empty, defaults to the genesis corresponding to the
	// fork.
	genesisJSON string
	upgradeJSON string
	configJSON  string
}

type testVM struct {
	vm           *VM
	db           *prefixdb.Database
	atomicMemory *atomic.Memory
	appSender    *enginetest.Sender
	config       testVMConfig
}

func newVM(t *testing.T, config testVMConfig) *testVM {
	ctx := utilstest.NewTestSnowContext(t, utilstest.SubnetEVMTestChainID)
	fork := upgradetest.Latest
	if config.fork != nil {
		fork = *config.fork
	}
	ctx.NetworkUpgrades = upgradetest.GetConfig(fork)

	if len(config.genesisJSON) == 0 {
		config.genesisJSON = toGenesisJSON(paramstest.ForkToChainConfig[fork])
	}

	baseDB := memdb.New()

	// initialize the atomic memory
	atomicMemory := atomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = atomicMemory.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	prefixedDB := prefixdb.New([]byte{1}, baseDB)

	vm := &VM{}
	appSender := &enginetest.Sender{T: t}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error { return nil }

	err := vm.Initialize(
		t.Context(),
		ctx,
		prefixedDB,
		[]byte(config.genesisJSON),
		[]byte(config.upgradeJSON),
		[]byte(config.configJSON),
		[]*commonEng.Fx{},
		appSender,
	)
	require.NoError(t, err, "error initializing vm")

	if !config.isSyncing {
		require.NoError(t, vm.SetState(t.Context(), snow.Bootstrapping))
		require.NoError(t, vm.SetState(t.Context(), snow.NormalOp))
	}

	return &testVM{
		vm:           vm,
		db:           prefixedDB,
		atomicMemory: atomicMemory,
		appSender:    appSender,
		config:       config,
	}
}

// Firewood cannot yet be run with an empty config.
func getConfig(scheme, otherConfig string) string {
	innerConfig := otherConfig
	if scheme == customrawdb.FirewoodScheme {
		if len(innerConfig) > 0 {
			innerConfig += ", "
		}
		innerConfig += fmt.Sprintf(`"state-scheme": "%s", "snapshot-cache": 0, "pruning-enabled": true, "state-sync-enabled": false, "metrics-expensive-enabled": false`, customrawdb.FirewoodScheme)
	}

	return fmt.Sprintf(`{%s}`, innerConfig)
}

// setupGenesis sets up the genesis
func setupGenesis(
	t *testing.T,
	fork upgradetest.Fork,
) (*snow.Context,
	*prefixdb.Database,
	[]byte,
) {
	ctx := utilstest.NewTestSnowContext(t, utilstest.SubnetEVMTestChainID)

	genesisJSON := toGenesisJSON(paramstest.ForkToChainConfig[fork])
	ctx.NetworkUpgrades = upgradetest.GetConfig(fork)

	baseDB := memdb.New()

	// initialize the atomic memory
	atomicMemory := atomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = atomicMemory.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	prefixedDB := prefixdb.New([]byte{1}, baseDB)

	return ctx, prefixedDB, []byte(genesisJSON)
}

func TestVMConfig(t *testing.T) {
	txFeeCap := float64(11)
	enabledEthAPIs := []string{"debug"}
	vm := newVM(t, testVMConfig{
		configJSON: fmt.Sprintf(`{"rpc-tx-fee-cap": %g,"eth-apis": %s}`, txFeeCap, fmt.Sprintf("[%q]", enabledEthAPIs[0])),
	}).vm

	require.Equal(t, vm.config.RPCTxFeeCap, txFeeCap, "Tx Fee Cap should be set")
	require.Equal(t, vm.config.EthAPIs(), enabledEthAPIs, "EnabledEthAPIs should be set")
	require.NoError(t, vm.Shutdown(t.Context()))
}

func TestVMContinuousProfiler(t *testing.T) {
	profilerDir := t.TempDir()
	profilerFrequency := 500 * time.Millisecond
	vm := newVM(t, testVMConfig{
		configJSON: fmt.Sprintf(`{"continuous-profiler-dir": %q,"continuous-profiler-frequency": "500ms"}`, profilerDir),
	}).vm

	require.Equal(t, vm.config.ContinuousProfilerDir, profilerDir, "profiler dir should be set")
	require.Equal(t, vm.config.ContinuousProfilerFrequency.Duration, profilerFrequency, "profiler frequency should be set")

	// Sleep for twice the frequency of the profiler to give it time
	// to generate the first profile.
	time.Sleep(2 * time.Second)
	require.NoError(t, vm.Shutdown(t.Context()))

	// Check that the first profile was generated
	expectedFileName := filepath.Join(profilerDir, "cpu.profile.1")
	_, err := os.Stat(expectedFileName)
	require.NoError(t, err, "Expected continuous profiler to generate the first CPU profile at %s", expectedFileName)
}

func TestExpectedGasPrice(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testExpectedGasPrice(t, scheme)
		})
	}
}

func testExpectedGasPrice(t *testing.T, scheme string) {
	genesisTests := []struct {
		name             string
		genesisJSON      string
		expectedGasPrice *big.Int
	}{
		{
			name:             "Subnet EVM",
			genesisJSON:      genesisJSONSubnetEVM,
			expectedGasPrice: big.NewInt(0),
		},
		{
			name:             "Durango",
			genesisJSON:      toGenesisJSON(params.TestDurangoChainConfig),
			expectedGasPrice: big.NewInt(0),
		},
	}

	for _, test := range genesisTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			vm := newVM(t, testVMConfig{
				genesisJSON: test.genesisJSON,
				configJSON:  getConfig(scheme, ""),
			}).vm

			defer func() {
				require.NoError(vm.Shutdown(t.Context()))
			}()

			require.Equal(test.expectedGasPrice, vm.txPool.GasTip())

			// Verify that the genesis is correctly managed.
			lastAcceptedID, err := vm.LastAccepted(t.Context())
			require.NoError(err)
			require.Equal(ids.ID(vm.genesisHash), lastAcceptedID)

			genesisBlk, err := vm.GetBlock(t.Context(), lastAcceptedID)
			require.NoError(err)
			require.Zero(genesisBlk.Height())

			_, err = vm.ParseBlock(t.Context(), genesisBlk.Bytes())
			require.NoError(err)
		})
	}
}

func issueAndAccept(t *testing.T, vm *VM) snowman.Block {
	t.Helper()

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	blk, err := vm.BuildBlock(t.Context())
	require.NoError(t, err)

	require.NoError(t, blk.Verify(t.Context()))

	require.NoError(t, vm.SetPreference(t.Context(), blk.ID()))

	require.NoError(t, blk.Accept(t.Context()))

	return blk
}

func TestBuildEthTxBlock(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testBuildEthTxBlock(t, scheme)
		})
	}
}

func testBuildEthTxBlock(t *testing.T, scheme string) {
	fork := upgradetest.ApricotPhase6
	tvm := newVM(t, testVMConfig{
		fork:        &fork,
		genesisJSON: genesisJSONSubnetEVM,
		configJSON:  getConfig(scheme, `"pruning-enabled":true`),
	})

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := utilstest.NewKey(t)

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)
	errs := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range errs {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	blk1 := issueAndAccept(t, tvm.vm)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, common.Hash(blk1.ID()), newHead.Head.Hash(), "Expected new block to match")

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), key.PrivateKey)
		require.NoError(t, err)
		txs[i] = signedTx
	}
	errs = tvm.vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	tvm.vm.clock.Set(tvm.vm.clock.Time().Add(2 * time.Second))
	blk2 := issueAndAccept(t, tvm.vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, common.Hash(blk2.ID()), newHead.Head.Hash(), "Expected new block to match")

	lastAcceptedID, err := tvm.vm.LastAccepted(t.Context())
	require.NoError(t, err)
	require.Equal(t, blk2.ID(), lastAcceptedID, "Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)

	ethBlk1 := blk1.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	ethBlk1Root := ethBlk1.Root()
	require.True(t, tvm.vm.blockChain.HasState(ethBlk1Root), "Expected blk1 state root to not yet be pruned after blk2 was accepted because of tip buffer")

	// Clear the cache and ensure that GetBlock returns internal blocks with the correct status
	tvm.vm.State.Flush()
	blk2Refreshed, err := tvm.vm.GetBlockInternal(t.Context(), blk2.ID())
	require.NoError(t, err)

	blk1RefreshedID := blk2Refreshed.Parent()
	blk1Refreshed, err := tvm.vm.GetBlockInternal(t.Context(), blk1RefreshedID)
	require.NoError(t, err)

	require.Equal(t, blk1.ID(), blk1Refreshed.ID(), "Found unexpected blkID for parent of blk2")

	// Close the vm and all databases
	require.NoError(t, tvm.vm.Shutdown(t.Context()))

	restartedVM := &VM{}
	newCTX := snowtest.Context(t, snowtest.CChainID)
	newCTX.NetworkUpgrades = upgradetest.GetConfig(fork)
	newCTX.ChainDataDir = tvm.vm.ctx.ChainDataDir
	conf := getConfig(scheme, "")
	require.NoError(t, restartedVM.Initialize(
		t.Context(),
		newCTX,
		tvm.db,
		[]byte(toGenesisJSON(paramstest.ForkToChainConfig[fork])),
		[]byte(""),
		[]byte(conf),
		[]*commonEng.Fx{},
		nil,
	))

	// State root should not have been committed and discarded on restart
	require.False(t, restartedVM.blockChain.HasState(ethBlk1Root), "Expected blk1 state root to be pruned after blk2 was accepted on top of it in pruning mode")

	// State root should be committed when accepted tip on shutdown
	require.True(t, restartedVM.blockChain.HasState(blk2.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Root()), "Expected blk2 state root to not be pruned after shutdown (last accepted tip should be committed)")

	// Shutdown the newest VM
	require.NoError(t, restartedVM.Shutdown(t.Context()))
}

// Regression test to ensure that after accepting block A
// then calling SetPreference on block B (when it becomes preferred)
// and the head of a longer chain (block D) does not corrupt the
// canonical chain.
//
//	  A
//	 / \
//	B   C
//	    |
//	    D
func TestSetPreferenceRace(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testSetPreferenceRace(t, scheme)
		})
	}
}

func testSetPreferenceRace(t *testing.T, scheme string) {
	// Create two VMs which will agree on block A and then
	// build the two distinct preferred chains above
	tvmConfig := testVMConfig{
		genesisJSON: genesisJSONSubnetEVM,
		configJSON:  getConfig(scheme, `"pruning-enabled":true`),
	}
	tvm1 := newVM(t, tvmConfig)
	tvm2 := newVM(t, tvmConfig)

	vm1 := tvm1.vm
	vm2 := tvm2.vm

	defer func() {
		require.NoError(t, vm1.Shutdown(t.Context()))

		require.NoError(t, vm2.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := vm1.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	msg, err := vm1.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm1BlkA, err := vm1.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build block with import transaction")

	require.NoError(t, vm1BlkA.Verify(t.Context()), "Block failed verification on VM1")

	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkA.ID()))

	vm2BlkA, err := vm2.ParseBlock(t.Context(), vm1BlkA.Bytes())
	require.NoError(t, err, "Unexpected error parsing block from vm2")
	require.NoError(t, vm2BlkA.Verify(t.Context()), "Block failed verification on VM2")
	require.NoError(t, vm2.SetPreference(t.Context(), vm2BlkA.ID()))

	require.NoError(t, vm1BlkA.Accept(t.Context()), "VM1 failed to accept block")
	require.NoError(t, vm2BlkA.Accept(t.Context()), "VM2 failed to accept block")

	newHead := <-newTxPoolHeadChan1
	require.Equal(t, common.Hash(vm1BlkA.ID()), newHead.Head.Hash(), "Expected new block to match")
	newHead = <-newTxPoolHeadChan2
	require.Equal(t, common.Hash(vm2BlkA.ID()), newHead.Head.Hash(), "Expected new block to match")

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[1].ToECDSA())
		require.NoError(t, err)
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM1 at index %d: %s", i, err)
	}

	msg, err = vm1.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm1BlkB, err := vm1.BuildBlock(t.Context())
	require.NoError(t, err)

	require.NoError(t, vm1BlkB.Verify(t.Context()))

	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkB.ID()))

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	errs = vm2.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM2 at index %d: %s", i, err)
	}

	msg, err = vm2.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm2BlkC, err := vm2.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build BlkC on VM2")

	require.NoError(t, vm2BlkC.Verify(t.Context()), "BlkC failed verification on VM2")

	require.NoError(t, vm2.SetPreference(t.Context(), vm2BlkC.ID()))

	newHead = <-newTxPoolHeadChan2
	require.Equal(t, common.Hash(vm2BlkC.ID()), newHead.Head.Hash(), "Expected new block to match")

	// Block D
	errs = vm2.txPool.AddRemotesSync(txs[5:10])
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM2 at index %d: %s", i, err)
	}

	msg, err = vm2.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)
	vm2BlkD, err := vm2.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build BlkD on VM2")

	require.NoError(t, vm2BlkD.Verify(t.Context()), "BlkD failed verification on VM2")

	require.NoError(t, vm2.SetPreference(t.Context(), vm2BlkD.ID()))

	// VM1 receives blkC and blkD from VM1
	// and happens to call SetPreference on blkD without ever calling SetPreference
	// on blkC
	// Here we parse them in reverse order to simulate receiving a chain from the tip
	// back to the last accepted block as would typically be the case in the consensus
	// engine
	vm1BlkD, err := vm1.ParseBlock(t.Context(), vm2BlkD.Bytes())
	require.NoError(t, err, "VM1 errored parsing blkD")
	vm1BlkC, err := vm1.ParseBlock(t.Context(), vm2BlkC.Bytes())
	require.NoError(t, err, "VM1 errored parsing blkC")

	// The blocks must be verified in order. This invariant is maintained
	// in the consensus engine.
	require.NoError(t, vm1BlkC.Verify(t.Context()), "VM1 BlkC failed verification")
	require.NoError(t, vm1BlkD.Verify(t.Context()), "VM1 BlkD failed verification")

	// Set VM1's preference to blockD, skipping blockC
	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkD.ID()))

	// Accept the longer chain on both VMs and ensure there are no errors
	// VM1 Accepts the blocks in order
	require.NoError(t, vm1BlkC.Accept(t.Context()), "VM1 BlkC failed on accept")
	require.NoError(t, vm1BlkD.Accept(t.Context()), "VM1 BlkC failed on accept")

	// VM2 Accepts the blocks in order
	require.NoError(t, vm2BlkC.Accept(t.Context()), "VM2 BlkC failed on accept")
	require.NoError(t, vm2BlkD.Accept(t.Context()), "VM2 BlkC failed on accept")

	log.Info("Validating canonical chain")
	// Verify the Canonical Chain for Both VMs
	require.NoError(t, vm2.blockChain.ValidateCanonicalChain(), "VM2 failed canonical chain verification due to")

	require.NoError(t, vm1.blockChain.ValidateCanonicalChain(), "VM1 failed canonical chain verification due to")
}

// Regression test to ensure that a VM that accepts block A and B
// will not attempt to orphan either when verifying blocks C and D
// from another VM (which have a common ancestor under the finalized
// frontier).
//
//	  A
//	 / \
//	B   C
//
// verifies block B and C, then Accepts block B. Then we test to ensure
// that the VM defends against any attempt to set the preference or to
// accept block C, which should be an orphaned block at this point and
// get rejected.
func TestReorgProtection(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testReorgProtection(t, scheme)
		})
	}
}

func testReorgProtection(t *testing.T, scheme string) {
	tvmConfig := testVMConfig{
		genesisJSON: genesisJSONSubnetEVM,
		configJSON:  getConfig(scheme, `"pruning-enabled":false`),
	}
	tvm1 := newVM(t, tvmConfig)
	tvm2 := newVM(t, tvmConfig)

	vm1 := tvm1.vm
	vm2 := tvm2.vm

	defer func() {
		require.NoError(t, vm1.Shutdown(t.Context()))

		require.NoError(t, vm2.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := vm1.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	msg, err := vm1.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm1BlkA, err := vm1.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build block with import transaction")

	require.NoError(t, vm1BlkA.Verify(t.Context()), "Block failed verification on VM1")

	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkA.ID()))

	vm2BlkA, err := vm2.ParseBlock(t.Context(), vm1BlkA.Bytes())
	require.NoError(t, err, "Unexpected error parsing block from vm2")
	require.NoError(t, vm2BlkA.Verify(t.Context()), "Block failed verification on VM2")
	require.NoError(t, vm2.SetPreference(t.Context(), vm2BlkA.ID()))

	require.NoError(t, vm1BlkA.Accept(t.Context()), "VM1 failed to accept block")
	require.NoError(t, vm2BlkA.Accept(t.Context()), "VM2 failed to accept block")

	newHead := <-newTxPoolHeadChan1
	require.Equal(t, common.Hash(vm1BlkA.ID()), newHead.Head.Hash(), "Expected new block to match")
	newHead = <-newTxPoolHeadChan2
	require.Equal(t, common.Hash(vm2BlkA.ID()), newHead.Head.Hash(), "Expected new block to match")

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[1].ToECDSA())
		require.NoError(t, err)
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM1 at index %d: %s", i, err)
	}

	msg, err = vm1.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm1BlkB, err := vm1.BuildBlock(t.Context())
	require.NoError(t, err)

	require.NoError(t, vm1BlkB.Verify(t.Context()))

	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkB.ID()))

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	errs = vm2.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM2 at index %d: %s", i, err)
	}

	msg, err = vm2.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm2BlkC, err := vm2.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build BlkC on VM2")

	require.NoError(t, vm2BlkC.Verify(t.Context()), "Block failed verification on VM2")

	vm1BlkC, err := vm1.ParseBlock(t.Context(), vm2BlkC.Bytes())
	require.NoError(t, err, "Unexpected error parsing block from vm2")

	require.NoError(t, vm1BlkC.Verify(t.Context()), "Block failed verification on VM1")

	// Accept B, such that block C should get Rejected.
	require.NoError(t, vm1BlkB.Accept(t.Context()), "VM1 failed to accept block")

	// The below (setting preference blocks that have a common ancestor
	// with the preferred chain lower than the last finalized block)
	// should NEVER happen. However, the VM defends against this
	// just in case.
	err = vm1.SetPreference(t.Context(), vm1BlkC.ID())
	require.ErrorContains(t, err, "cannot orphan finalized block", "Expected error when setting preference that would orphan finalized block") //nolint:forbidigo // uses upstream code
	err = vm1BlkC.Accept(t.Context())
	require.ErrorContains(t, err, "expected accepted block to have parent", "Expected error when accepting orphaned block") //nolint:forbidigo // uses upstream code
}

// Regression test to ensure that a VM that accepts block C while preferring
// block B will trigger a reorg.
//
//	  A
//	 / \
//	B   C
func TestNonCanonicalAccept(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testNonCanonicalAccept(t, scheme)
		})
	}
}

func testNonCanonicalAccept(t *testing.T, scheme string) {
	tvmConfig := testVMConfig{
		genesisJSON: genesisJSONSubnetEVM,
		configJSON:  getConfig(scheme, ""),
	}
	tvm1 := newVM(t, tvmConfig)
	tvm2 := newVM(t, tvmConfig)

	vm1 := tvm1.vm
	vm2 := tvm2.vm

	defer func() {
		require.NoError(t, vm1.Shutdown(t.Context()))

		require.NoError(t, vm2.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := vm1.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	msg, err := vm1.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm1BlkA, err := vm1.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build block with import transaction")

	require.NoError(t, vm1BlkA.Verify(t.Context()), "Block failed verification on VM1")

	_, err = vm1.GetBlockIDAtHeight(t.Context(), vm1BlkA.Height())
	require.ErrorIs(t, err, database.ErrNotFound, "Expected unaccepted block not to be indexed by height, but found %s", err)

	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkA.ID()))

	vm2BlkA, err := vm2.ParseBlock(t.Context(), vm1BlkA.Bytes())
	require.NoError(t, err, "Unexpected error parsing block from vm2")
	require.NoError(t, vm2BlkA.Verify(t.Context()), "Block failed verification on VM2")
	_, err = vm2.GetBlockIDAtHeight(t.Context(), vm2BlkA.Height())
	require.ErrorIs(t, err, database.ErrNotFound)
	require.NoError(t, vm2.SetPreference(t.Context(), vm2BlkA.ID()))

	require.NoError(t, vm1BlkA.Accept(t.Context()), "VM1 failed to accept block")
	blkID, err := vm1.GetBlockIDAtHeight(t.Context(), vm1BlkA.Height())
	require.NoError(t, err, "Height lookuped failed on accepted block")
	require.Equal(t, vm1BlkA.ID(), blkID, "Expected accepted block to be indexed by height, but found %s", blkID)

	require.NoError(t, vm2BlkA.Accept(t.Context()), "VM2 failed to accept block")
	blkID, err = vm2.GetBlockIDAtHeight(t.Context(), vm2BlkA.Height())
	require.NoError(t, err, "Height lookuped failed on accepted block")
	require.Equal(t, vm2BlkA.ID(), blkID, "Expected accepted block to be indexed by height, but found %s", blkID)

	newHead := <-newTxPoolHeadChan1
	require.Equal(t, common.Hash(vm1BlkA.ID()), newHead.Head.Hash(), "Expected new block to match")
	newHead = <-newTxPoolHeadChan2
	require.Equal(t, common.Hash(vm2BlkA.ID()), newHead.Head.Hash(), "Expected new block to match")

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[1].ToECDSA())
		require.NoError(t, err)
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM1 at index %d: %s", i, err)
	}

	msg, err = vm1.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm1BlkB, err := vm1.BuildBlock(t.Context())
	require.NoError(t, err)

	require.NoError(t, vm1BlkB.Verify(t.Context()))

	_, err = vm1.GetBlockIDAtHeight(t.Context(), vm1BlkB.Height())
	require.ErrorIs(t, err, database.ErrNotFound, "Expected unaccepted block not to be indexed by height, but found %s", err)

	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkB.ID()))

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	require.Equal(t, blkBHash, vm1.blockChain.GetBlockByNumber(blkBHeight).Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), vm1.blockChain.GetBlockByNumber(blkBHeight).Hash().Hex())

	errs = vm2.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM2 at index %d: %s", i, err)
	}

	msg, err = vm2.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm2BlkC, err := vm2.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build BlkC on VM2")

	vm1BlkC, err := vm1.ParseBlock(t.Context(), vm2BlkC.Bytes())
	require.NoError(t, err, "Unexpected error parsing block from vm2")

	require.NoError(t, vm1BlkC.Verify(t.Context()), "Block failed verification on VM1")

	_, err = vm1.GetBlockIDAtHeight(t.Context(), vm1BlkC.Height())
	require.ErrorIs(t, err, database.ErrNotFound, "Expected unaccepted block not to be indexed by height, but found %s", err)

	require.NoError(t, vm1BlkC.Accept(t.Context()), "VM1 failed to accept block")

	blkID, err = vm1.GetBlockIDAtHeight(t.Context(), vm1BlkC.Height())
	require.NoError(t, err, "Height lookuped failed on accepted block")
	require.Equal(t, vm1BlkC.ID(), blkID, "Expected accepted block to be indexed by height, but found %s", blkID)

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	require.Equal(t, blkCHash, vm1.blockChain.GetBlockByNumber(blkBHeight).Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), vm1.blockChain.GetBlockByNumber(blkBHeight).Hash().Hex())
}

// Regression test to ensure that a VM that verifies block B, C, then
// D (preferring block B) does not trigger a reorg through the re-verification
// of block C or D.
//
//	  A
//	 / \
//	B   C
//	    |
//	    D
func TestStickyPreference(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testStickyPreference(t, scheme)
		})
	}
}

func testStickyPreference(t *testing.T, scheme string) {
	tvmConfig := testVMConfig{
		genesisJSON: genesisJSONSubnetEVM,
		configJSON:  getConfig(scheme, ""),
	}
	tvm1 := newVM(t, tvmConfig)
	tvm2 := newVM(t, tvmConfig)

	vm1 := tvm1.vm
	vm2 := tvm2.vm

	defer func() {
		require.NoError(t, vm1.Shutdown(t.Context()))

		require.NoError(t, vm2.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := vm1.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	msg, err := vm1.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm1BlkA, err := vm1.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build block with import transaction")

	require.NoError(t, vm1BlkA.Verify(t.Context()), "Block failed verification on VM1")

	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkA.ID()))

	vm2BlkA, err := vm2.ParseBlock(t.Context(), vm1BlkA.Bytes())
	require.NoError(t, err, "Unexpected error parsing block from vm2")
	require.NoError(t, vm2BlkA.Verify(t.Context()), "Block failed verification on VM2")
	require.NoError(t, vm2.SetPreference(t.Context(), vm2BlkA.ID()))

	require.NoError(t, vm1BlkA.Accept(t.Context()), "VM1 failed to accept block")
	require.NoError(t, vm2BlkA.Accept(t.Context()), "VM2 failed to accept block")

	newHead := <-newTxPoolHeadChan1
	require.Equal(t, common.Hash(vm1BlkA.ID()), newHead.Head.Hash(), "Expected new block to match")
	newHead = <-newTxPoolHeadChan2
	require.Equal(t, common.Hash(vm2BlkA.ID()), newHead.Head.Hash(), "Expected new block to match")

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[1].ToECDSA())
		require.NoError(t, err)
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM1 at index %d: %s", i, err)
	}

	msg, err = vm1.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm1BlkB, err := vm1.BuildBlock(t.Context())
	require.NoError(t, err)

	require.NoError(t, vm1BlkB.Verify(t.Context()))

	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkB.ID()))

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	foundBlkBHash := vm1.blockChain.GetBlockByNumber(blkBHeight).Hash()
	require.Equal(t, blkBHash, foundBlkBHash, "expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), vm1.blockChain.GetBlockByNumber(blkBHeight).Hash().Hex())

	errs = vm2.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM2 at index %d: %s", i, err)
	}

	msg, err = vm2.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm2BlkC, err := vm2.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build BlkC on VM2")

	require.NoError(t, vm2BlkC.Verify(t.Context()), "BlkC failed verification on VM2")

	require.NoError(t, vm2.SetPreference(t.Context(), vm2BlkC.ID()))

	newHead = <-newTxPoolHeadChan2
	require.Equal(t, common.Hash(vm2BlkC.ID()), newHead.Head.Hash(), "Expected new block to match")

	errs = vm2.txPool.AddRemotesSync(txs[5:])
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM2 at index %d: %s", i, err)
	}

	msg, err = vm2.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm2BlkD, err := vm2.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build BlkD on VM2")

	// Parse blocks produced in vm2
	vm1BlkC, err := vm1.ParseBlock(t.Context(), vm2BlkC.Bytes())
	require.NoError(t, err, "Unexpected error parsing block from vm2")
	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()

	vm1BlkD, err := vm1.ParseBlock(t.Context(), vm2BlkD.Bytes())
	require.NoError(t, err, "Unexpected error parsing block from vm2")
	blkDHeight := vm1BlkD.Height()
	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()

	// Should be no-ops
	require.NoError(t, vm1BlkC.Verify(t.Context()), "Block failed verification on VM1")
	require.NoError(t, vm1BlkD.Verify(t.Context()), "Block failed verification on VM1")
	require.Equal(t, blkBHash, vm1.blockChain.GetBlockByNumber(blkBHeight).Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), vm1.blockChain.GetBlockByNumber(blkBHeight).Hash().Hex())
	require.Nil(t, vm1.blockChain.GetBlockByNumber(blkDHeight), "expected block at %d to be nil", blkDHeight)
	require.Equal(t, blkBHash, vm1.blockChain.CurrentBlock().Hash(), "expected current block to have hash %s but got %s", blkBHash.Hex(), vm1.blockChain.CurrentBlock().Hash().Hex())

	// Should still be no-ops on re-verify
	require.NoError(t, vm1BlkC.Verify(t.Context()), "Block failed verification on VM1")
	require.NoError(t, vm1BlkD.Verify(t.Context()), "Block failed verification on VM1")
	require.Equal(t, blkBHash, vm1.blockChain.GetBlockByNumber(blkBHeight).Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), vm1.blockChain.GetBlockByNumber(blkBHeight).Hash().Hex())
	require.Nil(t, vm1.blockChain.GetBlockByNumber(blkDHeight), "expected block at %d to be nil", blkDHeight)
	require.Equal(t, blkBHash, vm1.blockChain.CurrentBlock().Hash(), "expected current block to have hash %s but got %s", blkBHash.Hex(), vm1.blockChain.CurrentBlock().Hash().Hex())

	// Should be queryable after setting preference to side chain
	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkD.ID()))

	require.Equal(t, blkCHash, vm1.blockChain.GetBlockByNumber(blkBHeight).Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), vm1.blockChain.GetBlockByNumber(blkBHeight).Hash().Hex())
	require.Equal(t, blkDHash, vm1.blockChain.GetBlockByNumber(blkDHeight).Hash(), "expected block at %d to have hash %s but got %s", blkDHeight, blkDHash.Hex(), vm1.blockChain.GetBlockByNumber(blkDHeight).Hash().Hex())
	require.Equal(t, blkDHash, vm1.blockChain.CurrentBlock().Hash(), "expected current block to have hash %s but got %s", blkDHash.Hex(), vm1.blockChain.CurrentBlock().Hash().Hex())

	// Attempt to accept out of order
	require.ErrorContains(t, vm1BlkD.Accept(t.Context()), "expected accepted block to have parent", "unexpected error when accepting out of order block") //nolint:forbidigo // uses upstream code

	// Accept in order
	require.NoError(t, vm1BlkC.Accept(t.Context()), "Block failed verification on VM1")
	require.NoError(t, vm1BlkD.Accept(t.Context()), "Block failed acceptance on VM1")

	// Ensure queryable after accepting
	require.Equal(t, blkCHash, vm1.blockChain.GetBlockByNumber(blkBHeight).Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), vm1.blockChain.GetBlockByNumber(blkBHeight).Hash().Hex())
	require.Equal(t, blkDHash, vm1.blockChain.GetBlockByNumber(blkDHeight).Hash(), "expected block at %d to have hash %s but got %s", blkDHeight, blkDHash.Hex(), vm1.blockChain.GetBlockByNumber(blkDHeight).Hash().Hex())
	require.Equal(t, blkDHash, vm1.blockChain.CurrentBlock().Hash(), "expected current block to have hash %s but got %s", blkDHash.Hex(), vm1.blockChain.CurrentBlock().Hash().Hex())
}

// Regression test to ensure that a VM that prefers block B is able to parse
// block C but unable to parse block D because it names B as an uncle, which
// are not supported.
//
//	  A
//	 / \
//	B   C
//	    |
//	    D
func TestUncleBlock(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testUncleBlock(t, scheme)
		})
	}
}

func testUncleBlock(t *testing.T, scheme string) {
	tvmConfig := testVMConfig{
		genesisJSON: genesisJSONSubnetEVM,
		configJSON:  getConfig(scheme, ""),
	}
	tvm1 := newVM(t, tvmConfig)
	tvm2 := newVM(t, tvmConfig)

	vm1 := tvm1.vm
	vm2 := tvm2.vm

	defer func() {
		require.NoError(t, vm1.Shutdown(t.Context()))
		require.NoError(t, vm2.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := vm1.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	msg, err := vm1.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm1BlkA, err := vm1.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build block with import transaction")

	require.NoError(t, vm1BlkA.Verify(t.Context()), "Block failed verification on VM1")

	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkA.ID()))

	vm2BlkA, err := vm2.ParseBlock(t.Context(), vm1BlkA.Bytes())
	require.NoError(t, err, "Unexpected error parsing block from vm2")
	require.NoError(t, vm2BlkA.Verify(t.Context()), "Block failed verification on VM2")
	require.NoError(t, vm2.SetPreference(t.Context(), vm2BlkA.ID()))

	require.NoError(t, vm1BlkA.Accept(t.Context()), "VM1 failed to accept block")
	require.NoError(t, vm2BlkA.Accept(t.Context()), "VM2 failed to accept block")

	newHead := <-newTxPoolHeadChan1
	require.Equal(t, common.Hash(vm1BlkA.ID()), newHead.Head.Hash(), "Expected new block to match")
	newHead = <-newTxPoolHeadChan2
	require.Equal(t, common.Hash(vm2BlkA.ID()), newHead.Head.Hash(), "Expected new block to match")

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[1].ToECDSA())
		require.NoError(t, err)
		txs[i] = signedTx
	}

	var errs []error

	errs = vm1.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM1 at index %d: %s", i, err)
	}

	msg, err = vm1.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm1BlkB, err := vm1.BuildBlock(t.Context())
	require.NoError(t, err)

	require.NoError(t, vm1BlkB.Verify(t.Context()))

	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkB.ID()))

	errs = vm2.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM2 at index %d: %s", i, err)
	}

	msg, err = vm2.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm2BlkC, err := vm2.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build BlkC on VM2")

	require.NoError(t, vm2BlkC.Verify(t.Context()), "BlkC failed verification on VM2")

	require.NoError(t, vm2.SetPreference(t.Context(), vm2BlkC.ID()))

	newHead = <-newTxPoolHeadChan2
	require.Equal(t, common.Hash(vm2BlkC.ID()), newHead.Head.Hash(), "Expected new block to match")

	errs = vm2.txPool.AddRemotesSync(txs[5:10])
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM2 at index %d: %s", i, err)
	}

	msg, err = vm2.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm2BlkD, err := vm2.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build BlkD on VM2")

	// Create uncle block from blkD
	blkDEthBlock := vm2BlkD.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	uncles := []*types.Header{vm1BlkB.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Header()}
	uncleBlockHeader := types.CopyHeader(blkDEthBlock.Header())
	uncleBlockHeader.UncleHash = types.CalcUncleHash(uncles)

	uncleEthBlock := types.NewBlock(
		uncleBlockHeader,
		blkDEthBlock.Transactions(),
		uncles,
		nil,
		trie.NewStackTrie(nil),
	)
	uncleBlock, _ := wrapBlock(uncleEthBlock, tvm2.vm)

	verifyErr := uncleBlock.Verify(t.Context())
	require.ErrorIs(t, verifyErr, errUnclesUnsupported)
	_, err = vm1.ParseBlock(t.Context(), vm2BlkC.Bytes())
	require.NoError(t, err, "VM1 errored parsing blkC")
	_, err = vm1.ParseBlock(t.Context(), uncleBlock.Bytes())
	require.ErrorIs(t, err, errUnclesUnsupported)
}

// Regression test to ensure that a VM that is not able to parse a block that
// contains no transactions.
func TestEmptyBlock(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testEmptyBlock(t, scheme)
		})
	}
}

func testEmptyBlock(t *testing.T, scheme string) {
	tvm := newVM(t, testVMConfig{
		genesisJSON: genesisJSONSubnetEVM,
		configJSON:  getConfig(scheme, ""),
	})

	defer func() {
		require.NoError(t, tvm.vm.Shutdown(t.Context()))
	}()

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	msg, err := tvm.vm.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	blk, err := tvm.vm.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build block with import transaction")

	// Create empty block from blkA
	ethBlock := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()

	emptyEthBlock := types.NewBlock(
		types.CopyHeader(ethBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
	)

	emptyBlock, err := wrapBlock(emptyEthBlock, tvm.vm)
	require.NoError(t, err)

	_, err = tvm.vm.ParseBlock(t.Context(), emptyBlock.Bytes())
	require.ErrorIs(t, err, errEmptyBlock)
	verifyErr := emptyBlock.Verify(t.Context())
	require.ErrorIs(t, verifyErr, errEmptyBlock)
}

// Regression test to ensure that a VM that verifies block B, C, then
// D (preferring block B) reorgs when C and then D are accepted.
//
//	  A
//	 / \
//	B   C
//	    |
//	    D
func TestAcceptReorg(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testAcceptReorg(t, scheme)
		})
	}
}

func testAcceptReorg(t *testing.T, scheme string) {
	tvmConfig := testVMConfig{
		genesisJSON: genesisJSONSubnetEVM,
		configJSON:  getConfig(scheme, ""),
	}
	tvm1 := newVM(t, tvmConfig)
	tvm2 := newVM(t, tvmConfig)

	vm1 := tvm1.vm
	vm2 := tvm2.vm

	defer func() {
		require.NoError(t, vm1.Shutdown(t.Context()))

		require.NoError(t, vm2.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := vm1.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	msg, err := vm1.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm1BlkA, err := vm1.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build block with import transaction")

	require.NoError(t, vm1BlkA.Verify(t.Context()), "Block failed verification on VM1")

	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkA.ID()))

	vm2BlkA, err := vm2.ParseBlock(t.Context(), vm1BlkA.Bytes())
	require.NoError(t, err, "Unexpected error parsing block from vm2")
	require.NoError(t, vm2BlkA.Verify(t.Context()), "Block failed verification on VM2")
	require.NoError(t, vm2.SetPreference(t.Context(), vm2BlkA.ID()))

	require.NoError(t, vm1BlkA.Accept(t.Context()), "VM1 failed to accept block")
	require.NoError(t, vm2BlkA.Accept(t.Context()), "VM2 failed to accept block")

	newHead := <-newTxPoolHeadChan1
	require.Equal(t, common.Hash(vm1BlkA.ID()), newHead.Head.Hash(), "Expected new block to match")
	newHead = <-newTxPoolHeadChan2
	require.Equal(t, common.Hash(vm2BlkA.ID()), newHead.Head.Hash(), "Expected new block to match")

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[1].ToECDSA())
		require.NoError(t, err)
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference
	// for block B
	errs := vm1.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM1 at index %d: %s", i, err)
	}

	msg, err = vm1.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm1BlkB, err := vm1.BuildBlock(t.Context())
	require.NoError(t, err)

	require.NoError(t, vm1BlkB.Verify(t.Context()))

	require.NoError(t, vm1.SetPreference(t.Context(), vm1BlkB.ID()))

	errs = vm2.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM2 at index %d: %s", i, err)
	}

	msg, err = vm2.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm2BlkC, err := vm2.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build BlkC on VM2")

	require.NoError(t, vm2BlkC.Verify(t.Context()), "BlkC failed verification on VM2")

	require.NoError(t, vm2.SetPreference(t.Context(), vm2BlkC.ID()))

	newHead = <-newTxPoolHeadChan2
	require.Equal(t, common.Hash(vm2BlkC.ID()), newHead.Head.Hash(), "Expected new block to match")

	errs = vm2.txPool.AddRemotesSync(txs[5:])
	for i, err := range errs {
		require.NoError(t, err, "Failed to add transaction to VM2 at index %d: %s", i, err)
	}

	msg, err = vm2.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	vm2BlkD, err := vm2.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build BlkD on VM2")

	// Parse blocks produced in vm2
	vm1BlkC, err := vm1.ParseBlock(t.Context(), vm2BlkC.Bytes())
	require.NoError(t, err, "Unexpected error parsing block from vm2")

	vm1BlkD, err := vm1.ParseBlock(t.Context(), vm2BlkD.Bytes())
	require.NoError(t, err, "Unexpected error parsing block from vm2")

	require.NoError(t, vm1BlkC.Verify(t.Context()), "Block failed verification on VM1")
	require.NoError(t, vm1BlkD.Verify(t.Context()), "Block failed verification on VM1")

	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	require.Equal(t, blkBHash, vm1.blockChain.CurrentBlock().Hash(), "expected current block to have hash %s but got %s", blkBHash.Hex(), vm1.blockChain.CurrentBlock().Hash().Hex())

	require.NoError(t, vm1BlkC.Accept(t.Context()))

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	require.Equal(t, blkCHash, vm1.blockChain.CurrentBlock().Hash(), "expected current block to have hash %s but got %s", blkCHash.Hex(), vm1.blockChain.CurrentBlock().Hash().Hex())
	require.NoError(t, vm1BlkB.Reject(t.Context()))

	require.NoError(t, vm1BlkD.Accept(t.Context()))
	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	require.Equal(t, blkDHash, vm1.blockChain.CurrentBlock().Hash(), "expected current block to have hash %s but got %s", blkDHash.Hex(), vm1.blockChain.CurrentBlock().Hash().Hex())
}

func TestTimeSemanticVerify(t *testing.T) {
	timestamp := time.Unix(1714339200, 123_456_789)
	cases := []struct {
		name             string
		fork             upgradetest.Fork
		timeSeconds      uint64
		timeMilliseconds *uint64
		expectedError    error
	}{
		{
			name:             "Fortuna without TimeMilliseconds",
			fork:             upgradetest.Fortuna,
			timeSeconds:      uint64(timestamp.Unix()),
			timeMilliseconds: nil,
		},
		{
			name:             "Granite with TimeMilliseconds",
			fork:             upgradetest.Granite,
			timeSeconds:      uint64(timestamp.Unix()),
			timeMilliseconds: utils.NewUint64(uint64(timestamp.UnixMilli())),
		},
		{
			name:             "Fortuna with TimeMilliseconds",
			fork:             upgradetest.Fortuna,
			timeSeconds:      uint64(timestamp.Unix()),
			timeMilliseconds: utils.NewUint64(uint64(timestamp.UnixMilli())),
			expectedError:    customheader.ErrTimeMillisecondsBeforeGranite,
		},
		{
			name:             "Granite without TimeMilliseconds",
			fork:             upgradetest.Granite,
			timeSeconds:      uint64(timestamp.Unix()),
			timeMilliseconds: nil,
			expectedError:    customheader.ErrTimeMillisecondsRequired,
		},
		{
			name:             "Granite with mismatched TimeMilliseconds",
			fork:             upgradetest.Granite,
			timeSeconds:      uint64(timestamp.Unix()),
			timeMilliseconds: utils.NewUint64(uint64(timestamp.UnixMilli()) + 1000),
			expectedError:    customheader.ErrTimeMillisecondsMismatched,
		},
		{
			name:             "Block too far in the future",
			fork:             upgradetest.Granite,
			timeSeconds:      uint64(timestamp.Add(2 * time.Hour).Unix()),
			timeMilliseconds: utils.NewUint64(uint64(timestamp.Add(2 * time.Hour).UnixMilli())),
			expectedError:    customheader.ErrBlockTooFarInFuture,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tvm := newVM(t, testVMConfig{
				genesisJSON: genesisJSONSubnetEVM,
				fork:        &test.fork,
			})

			defer func() {
				require.NoError(t, tvm.vm.Shutdown(t.Context()))
			}()

			tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
			signedTx, err := types.SignTx(tx, types.LatestSigner(tvm.vm.chainConfig), testKeys[0].ToECDSA())
			require.NoError(t, err)

			txErrors := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
			for i, err := range txErrors {
				require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
			}

			msg, err := tvm.vm.WaitForEvent(t.Context())
			require.NoError(t, err)
			require.Equal(t, commonEng.PendingTxs, msg)

			blk, err := tvm.vm.BuildBlock(t.Context())
			require.NoError(t, err, "Failed to build block with import transaction")
			require.NoError(t, blk.Verify(t.Context()), "Block failed verification on VM")

			// Create empty block from blkA
			ethBlk := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock

			// Modify the header to have the desired time values
			modifiedHeader := types.CopyHeader(ethBlk.Header())
			modifiedHeader.Time = test.timeSeconds
			modifiedExtra := customtypes.GetHeaderExtra(modifiedHeader)
			modifiedExtra.TimeMilliseconds = test.timeMilliseconds

			// Build new block with modified header
			receipts := tvm.vm.blockChain.GetReceiptsByHash(ethBlk.Hash())
			modifiedBlock := types.NewBlock(
				modifiedHeader,
				ethBlk.Transactions(),
				nil,
				receipts,
				trie.NewStackTrie(nil),
			)
			modifiedBlk, err := wrapBlock(modifiedBlock, tvm.vm)
			require.NoError(t, err)

			tvm.vm.clock.Set(timestamp) // set current time to base for time checks
			err = modifiedBlk.Verify(t.Context())
			require.ErrorIs(t, err, test.expectedError)
		})
	}
}

func TestBuildTimeMilliseconds(t *testing.T) {
	buildTime := time.Unix(1714339200, 123_456_789)
	cases := []struct {
		name                     string
		fork                     upgradetest.Fork
		expectedTimeMilliseconds *uint64
	}{
		{
			name:                     "fortuna_should_not_have_timestamp_milliseconds",
			fork:                     upgradetest.Fortuna,
			expectedTimeMilliseconds: nil,
		},
		{
			name:                     "granite_should_have_timestamp_milliseconds",
			fork:                     upgradetest.Granite,
			expectedTimeMilliseconds: utils.NewUint64(uint64(buildTime.UnixMilli())),
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tvm := newVM(t, testVMConfig{
				fork:        &test.fork,
				genesisJSON: genesisJSONSubnetEVM,
			})

			defer func() {
				require.NoError(t, tvm.vm.Shutdown(t.Context()))
			}()

			tvm.vm.clock.Set(buildTime)
			tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
			require.NoError(t, err)

			txErrors := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
			for i, err := range txErrors {
				require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
			}

			msg, err := tvm.vm.WaitForEvent(t.Context())
			require.NoError(t, err)
			require.Equal(t, commonEng.PendingTxs, msg)

			blk, err := tvm.vm.BuildBlock(t.Context())
			require.NoError(t, err, "Failed to build block with import transaction")
			require.NoError(t, err)
			ethBlk := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
			require.Equal(t, test.expectedTimeMilliseconds, customtypes.BlockTimeMilliseconds(ethBlk))
		})
	}
}

func TestLastAcceptedBlockNumberAllow(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testLastAcceptedBlockNumberAllow(t, scheme)
		})
	}
}

func testLastAcceptedBlockNumberAllow(t *testing.T, scheme string) {
	tvm := newVM(t, testVMConfig{
		genesisJSON: genesisJSONSubnetEVM,
		configJSON:  getConfig(scheme, ""),
	})

	defer func() {
		require.NoError(t, tvm.vm.Shutdown(t.Context()))
	}()

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	msg, err := tvm.vm.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	blk, err := tvm.vm.BuildBlock(t.Context())
	require.NoError(t, err, "Failed to build block with import transaction")

	require.NoError(t, blk.Verify(t.Context()), "Block failed verification on VM")

	require.NoError(t, tvm.vm.SetPreference(t.Context(), blk.ID()))

	blkHeight := blk.Height()
	blkHash := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock().Hash()

	tvm.vm.eth.APIBackend.SetAllowUnfinalizedQueries(true)

	ctx := t.Context()
	b, err := tvm.vm.eth.APIBackend.BlockByNumber(ctx, rpc.BlockNumber(blkHeight))
	require.NoError(t, err)
	require.Equal(t, blkHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())

	tvm.vm.eth.APIBackend.SetAllowUnfinalizedQueries(false)

	_, err = tvm.vm.eth.APIBackend.BlockByNumber(ctx, rpc.BlockNumber(blkHeight))
	require.ErrorIs(t, err, eth.ErrUnfinalizedData, "expected ErrUnfinalizedData but got %s", err)
	require.NoError(t, blk.Accept(t.Context()), "VM failed to accept block")

	b = tvm.vm.blockChain.GetBlockByNumber(blkHeight)
	require.Equal(t, blkHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())
}

func TestBuildAllowListActivationBlock(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testBuildAllowListActivationBlock(t, scheme)
		})
	}
}

func testBuildAllowListActivationBlock(t *testing.T, scheme string) {
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)))
	params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
		deployerallowlist.ConfigKey: deployerallowlist.NewConfig(utils.TimeToNewUint64(time.Now()), testEthAddrs, nil, nil),
	}

	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)
	tvm := newVM(t, testVMConfig{
		genesisJSON: string(genesisJSON),
		configJSON:  getConfig(scheme, ""),
	})

	defer func() {
		require.NoError(t, tvm.vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	genesisState, err := tvm.vm.blockChain.StateAt(tvm.vm.blockChain.Genesis().Root())
	require.NoError(t, err)
	role := deployerallowlist.GetContractDeployerAllowListStatus(genesisState, testEthAddrs[0])
	require.Equal(t, allowlist.NoRole, role, "Expected allow list status to be set to no role: %s, but found: %s", allowlist.NoRole, role)

	// Send basic transaction to construct a simple block and confirm that the precompile state configuration in the worker behaves correctly.
	tx := types.NewTransaction(uint64(0), testEthAddrs[1], new(big.Int).Mul(firstTxAmount, big.NewInt(4)), 21000, big.NewInt(testMinGasPrice*3), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	blk := issueAndAccept(t, tvm.vm)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, common.Hash(blk.ID()), newHead.Head.Hash(), "Expected new block to match")

	// Verify that the allow list config activation was handled correctly in the first block.
	blkState, err := tvm.vm.blockChain.StateAt(blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Root())
	require.NoError(t, err)
	role = deployerallowlist.GetContractDeployerAllowListStatus(blkState, testEthAddrs[0])
	require.Equal(t, allowlist.AdminRole, role, "Expected allow list status to be set role %s, but found: %s", allowlist.AdminRole, role)
}

// Test that the tx allow list allows whitelisted transactions and blocks non-whitelisted addresses
func TestTxAllowListSuccessfulTx(t *testing.T) {
	// Setup chain params
	managerKey := testKeys[1]
	managerAddress := testEthAddrs[1]
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Durango]))))
	// this manager role should not be activated because DurangoTimestamp is in the future
	params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
		txallowlist.ConfigKey: txallowlist.NewConfig(utils.NewUint64(0), testEthAddrs[0:1], nil, nil),
	}
	durangoTime := time.Now().Add(10 * time.Hour)
	params.GetExtra(genesis.Config).DurangoTimestamp = utils.TimeToNewUint64(durangoTime)
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)

	// prepare the new upgrade bytes to disable the TxAllowList
	disableAllowListTime := durangoTime.Add(10 * time.Hour)
	reenableAllowlistTime := disableAllowListTime.Add(10 * time.Hour)
	upgradeConfig := &extras.UpgradeConfig{
		PrecompileUpgrades: []extras.PrecompileUpgrade{
			{
				Config: txallowlist.NewDisableConfig(utils.TimeToNewUint64(disableAllowListTime)),
			},
			// re-enable the tx allowlist after Durango to set the manager role
			{
				Config: txallowlist.NewConfig(utils.TimeToNewUint64(reenableAllowlistTime), testEthAddrs[0:1], nil, []common.Address{managerAddress}),
			},
		},
	}
	upgradeBytesJSON, err := json.Marshal(upgradeConfig)
	require.NoError(t, err)

	fork := upgradetest.Durango
	tvm := newVM(t, testVMConfig{
		fork:        &fork,
		genesisJSON: string(genesisJSON),
		upgradeJSON: string(upgradeBytesJSON),
	})

	defer func() {
		require.NoError(t, tvm.vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	genesisState, err := tvm.vm.blockChain.StateAt(tvm.vm.blockChain.Genesis().Root())
	require.NoError(t, err)

	// Check that address 0 is whitelisted and address 1 is not
	role := txallowlist.GetTxAllowListStatus(genesisState, testEthAddrs[0])
	require.Equal(t, allowlist.AdminRole, role, "Expected allow list status to be set to admin: %s, but found: %s", allowlist.AdminRole, role)
	role = txallowlist.GetTxAllowListStatus(genesisState, testEthAddrs[1])
	require.Equal(t, allowlist.NoRole, role, "Expected allow list status to be set to no role: %s, but found: %s", allowlist.NoRole, role)
	// Should not be a manager role because Durango has not activated yet
	role = txallowlist.GetTxAllowListStatus(genesisState, managerAddress)
	require.Equal(t, allowlist.NoRole, role)

	// Submit a successful transaction
	tx0 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx0, err := types.SignTx(tx0, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	errs := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	require.NoError(t, errs[0], "Failed to add tx at index")

	// Submit a rejected transaction, should throw an error
	tx1 := types.NewTransaction(uint64(0), testEthAddrs[1], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx1, err := types.SignTx(tx1, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[1].ToECDSA())
	require.NoError(t, err)

	errs = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	require.ErrorIs(t, errs[0], vmerrors.ErrSenderAddressNotAllowListed)

	// Submit a rejected transaction, should throw an error because manager is not activated
	tx2 := types.NewTransaction(uint64(0), managerAddress, big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx2, err := types.SignTx(tx2, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), managerKey.ToECDSA())
	require.NoError(t, err)

	errs = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})
	require.ErrorIs(t, errs[0], vmerrors.ErrSenderAddressNotAllowListed)

	blk := issueAndAccept(t, tvm.vm)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))

	// Verify that the constructed block only has the whitelisted tx
	block := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()

	txs := block.Transactions()

	require.Len(t, txs, 1, "Expected number of txs to be %d, but found %d", 1, txs.Len())

	require.Equal(t, signedTx0.Hash(), txs[0].Hash())

	tvm.vm.clock.Set(reenableAllowlistTime.Add(time.Hour))

	// Re-Submit a successful transaction
	tx0 = types.NewTransaction(uint64(1), testEthAddrs[0], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx0, err = types.SignTx(tx0, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	errs = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	require.NoError(t, errs[0])

	// accept block to trigger upgrade
	blk = issueAndAccept(t, tvm.vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	block = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()

	blkState, err := tvm.vm.blockChain.StateAt(block.Root())
	require.NoError(t, err)

	// Check that address 0 is admin and address 1 is manager
	role = txallowlist.GetTxAllowListStatus(blkState, testEthAddrs[0])
	require.Equal(t, allowlist.AdminRole, role)
	role = txallowlist.GetTxAllowListStatus(blkState, managerAddress)
	require.Equal(t, allowlist.ManagerRole, role)

	tvm.vm.clock.Set(tvm.vm.clock.Time().Add(2 * time.Second)) // add 2 seconds for gas fee to adjust
	// Submit a successful transaction, should not throw an error because manager is activated
	tx3 := types.NewTransaction(uint64(0), managerAddress, big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx3, err := types.SignTx(tx3, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), managerKey.ToECDSA())
	require.NoError(t, err)

	tvm.vm.clock.Set(tvm.vm.clock.Time().Add(2 * time.Second)) // add 2 seconds for gas fee to adjust
	errs = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx3})
	require.NoError(t, errs[0])

	blk = issueAndAccept(t, tvm.vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))

	// Verify that the constructed block only has the whitelisted tx
	block = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	txs = block.Transactions()

	require.Len(t, txs, 1)
	require.Equal(t, signedTx3.Hash(), txs[0].Hash())
}

func TestVerifyManagerConfig(t *testing.T) {
	genesis := &core.Genesis{}
	ctx, dbManager, genesisBytes := setupGenesis(t, upgradetest.Durango)
	require.NoError(t, genesis.UnmarshalJSON(genesisBytes))

	durangoTimestamp := time.Now().Add(10 * time.Hour)
	params.GetExtra(genesis.Config).DurangoTimestamp = utils.TimeToNewUint64(durangoTimestamp)
	// this manager role should not be activated because DurangoTimestamp is in the future
	params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
		txallowlist.ConfigKey: txallowlist.NewConfig(utils.NewUint64(0), testEthAddrs[0:1], nil, []common.Address{testEthAddrs[1]}),
	}

	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)

	vm := &VM{}
	err = vm.Initialize(
		t.Context(),
		ctx,
		dbManager,
		genesisJSON, // Manually set genesis bytes due to custom genesis
		[]byte(""),
		[]byte(""),
		[]*commonEng.Fx{},
		nil,
	)
	require.ErrorIs(t, err, allowlist.ErrCannotAddManagersBeforeDurango)

	genesis = &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Durango]))))
	params.GetExtra(genesis.Config).DurangoTimestamp = utils.TimeToNewUint64(durangoTimestamp)
	genesisJSON, err = genesis.MarshalJSON()
	require.NoError(t, err)
	// use an invalid upgrade now with managers set before Durango
	upgradeConfig := &extras.UpgradeConfig{
		PrecompileUpgrades: []extras.PrecompileUpgrade{
			{
				Config: txallowlist.NewConfig(utils.TimeToNewUint64(durangoTimestamp.Add(-time.Second)), nil, nil, []common.Address{testEthAddrs[1]}),
			},
		},
	}
	upgradeBytesJSON, err := json.Marshal(upgradeConfig)
	require.NoError(t, err)

	vm = &VM{}
	ctx, dbManager, _ = setupGenesis(t, upgradetest.Latest)
	err = vm.Initialize(
		t.Context(),
		ctx,
		dbManager,
		genesisJSON, // Manually set genesis bytes due to custom genesis
		upgradeBytesJSON,
		[]byte(""),
		[]*commonEng.Fx{},
		nil,
	)
	require.ErrorIs(t, err, allowlist.ErrCannotAddManagersBeforeDurango)
}

// Test that the tx allow list allows whitelisted transactions and blocks non-whitelisted addresses
// and the allowlist is removed after the precompile is disabled.
func TestTxAllowListDisablePrecompile(t *testing.T) {
	// Setup chain params
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Latest]))))
	enableAllowListTimestamp := upgrade.InitiallyActiveTime // enable at initially active time
	params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
		txallowlist.ConfigKey: txallowlist.NewConfig(utils.TimeToNewUint64(enableAllowListTimestamp), testEthAddrs[0:1], nil, nil),
	}
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)

	// arbitrary choice ahead of enableAllowListTimestamp
	disableAllowListTimestamp := enableAllowListTimestamp.Add(10 * time.Hour)
	// configure a network upgrade to remove the allowlist
	upgradeConfig := fmt.Sprintf(`
	{
		"precompileUpgrades": [
			{
				"txAllowListConfig": {
					"blockTimestamp": %d,
					"disable": true
				}
			}
		]
	}
	`, disableAllowListTimestamp.Unix())

	tvm := newVM(t, testVMConfig{
		genesisJSON: string(genesisJSON),
		upgradeJSON: upgradeConfig,
	})

	tvm.vm.clock.Set(disableAllowListTimestamp) // upgrade takes effect after a block is issued, so we can set vm's clock here.

	defer func() {
		require.NoError(t, tvm.vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	genesisState, err := tvm.vm.blockChain.StateAt(tvm.vm.blockChain.Genesis().Root())
	require.NoError(t, err)

	// Check that address 0 is whitelisted and address 1 is not
	role := txallowlist.GetTxAllowListStatus(genesisState, testEthAddrs[0])
	require.Equal(t, allowlist.AdminRole, role, "expected allow list status to be set to admin: %s, but found: %s", allowlist.AdminRole, role)
	role = txallowlist.GetTxAllowListStatus(genesisState, testEthAddrs[1])
	require.Equal(t, allowlist.NoRole, role, "expected allow list status to be set to no role: %s, but found: %s", allowlist.NoRole, role)

	// Submit a successful transaction
	tx0 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx0, err := types.SignTx(tx0, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	errs := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	require.NoError(t, errs[0], "Failed to add tx at index")

	// Submit a rejected transaction, should throw an error
	tx1 := types.NewTransaction(uint64(0), testEthAddrs[1], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx1, err := types.SignTx(tx1, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[1].ToECDSA())
	require.NoError(t, err)

	errs = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	require.ErrorIs(t, errs[0], vmerrors.ErrSenderAddressNotAllowListed, "want %s, got %s", vmerrors.ErrSenderAddressNotAllowListed, errs[0])

	blk := issueAndAccept(t, tvm.vm)

	// Verify that the constructed block only has the whitelisted tx
	block := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	txs := block.Transactions()
	require.Len(t, txs, 1, "Expected number of txs to be %d, but found %d", 1, txs.Len())
	require.Equal(t, signedTx0.Hash(), txs[0].Hash())

	// verify the issued block is after the network upgrade
	require.GreaterOrEqual(t, int64(block.Time()), disableAllowListTimestamp.Unix())

	<-newTxPoolHeadChan // wait for new head in tx pool

	// retry the rejected Tx, which should now succeed
	errs = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	require.NoError(t, errs[0], "Failed to add tx at index")

	tvm.vm.clock.Set(tvm.vm.clock.Time().Add(2 * time.Second)) // add 2 seconds for gas fee to adjust
	blk = issueAndAccept(t, tvm.vm)

	// Verify that the constructed block only has the previously rejected tx
	block = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	txs = block.Transactions()
	require.Equal(t, 1, txs.Len(), "Expected number of txs to be %d, but found %d", 1, txs.Len())
	require.Equal(t, signedTx1.Hash(), txs[0].Hash())
}

// Test that the fee manager changes fee configuration
func TestFeeManagerChangeFee(t *testing.T) {
	// Setup chain params
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)))
	configExtra := params.GetExtra(genesis.Config)
	configExtra.GenesisPrecompiles = extras.Precompiles{
		feemanager.ConfigKey: feemanager.NewConfig(utils.NewUint64(0), testEthAddrs[0:1], nil, nil, nil),
	}

	// set a lower fee config now
	testLowFeeConfig := commontype.FeeConfig{
		GasLimit:        big.NewInt(8_000_000),
		TargetBlockRate: 5, // in seconds

		MinBaseFee:               big.NewInt(5_000_000_000),
		TargetGas:                big.NewInt(18_000_000),
		BaseFeeChangeDenominator: big.NewInt(3396),

		MinBlockGasCost:  big.NewInt(0),
		MaxBlockGasCost:  big.NewInt(4_000_000),
		BlockGasCostStep: big.NewInt(500_000),
	}

	configExtra.FeeConfig = testLowFeeConfig
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)
	tvm := newVM(t, testVMConfig{
		genesisJSON: string(genesisJSON),
	})

	defer func() {
		require.NoError(t, tvm.vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	genesisState, err := tvm.vm.blockChain.StateAt(tvm.vm.blockChain.Genesis().Root())
	require.NoError(t, err)

	// Check that address 0 is whitelisted and address 1 is not
	role := feemanager.GetFeeManagerStatus(genesisState, testEthAddrs[0])
	require.Equal(t, allowlist.AdminRole, role, "expected fee manager list status to be set to admin: %s, but found: %s", allowlist.AdminRole, role)
	role = feemanager.GetFeeManagerStatus(genesisState, testEthAddrs[1])
	require.Equal(t, allowlist.NoRole, role, "expected fee manager list status to be set to no role: %s, but found: %s", allowlist.NoRole, role)
	// Contract is initialized but no preconfig is given, reader should return genesis fee config
	feeConfig, lastChangedAt, err := tvm.vm.blockChain.GetFeeConfigAt(tvm.vm.blockChain.Genesis().Header())
	require.NoError(t, err)
	require.Equal(t, testLowFeeConfig, feeConfig)
	require.Zero(t, tvm.vm.blockChain.CurrentBlock().Number.Cmp(lastChangedAt))

	// set a different fee config now
	testHighFeeConfig := testLowFeeConfig
	testHighFeeConfig.MinBaseFee = big.NewInt(28_000_000_000)

	data, err := feemanager.PackSetFeeConfig(testHighFeeConfig)
	require.NoError(t, err)

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   genesis.Config.ChainID,
		Nonce:     uint64(0),
		To:        &feemanager.ContractAddress,
		Gas:       testLowFeeConfig.GasLimit.Uint64(),
		Value:     common.Big0,
		GasFeeCap: testLowFeeConfig.MinBaseFee, // give low fee, it should work since we still haven't applied high fees
		GasTipCap: common.Big0,
		Data:      data,
	})

	signedTx, err := types.SignTx(tx, types.LatestSigner(genesis.Config), testKeys[0].ToECDSA())
	require.NoError(t, err)

	errs := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.NoError(t, errs[0], "Failed to add tx at index")

	blk := issueAndAccept(t, tvm.vm)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, common.Hash(blk.ID()), newHead.Head.Hash(), "Expected new block to match")

	block := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()

	feeConfig, lastChangedAt, err = tvm.vm.blockChain.GetFeeConfigAt(block.Header())
	require.NoError(t, err)
	require.Equal(t, testHighFeeConfig, feeConfig)
	require.Equal(t, tvm.vm.blockChain.CurrentBlock().Number, lastChangedAt)

	// should fail, with same params since fee is higher now
	tx2 := types.NewTx(&types.DynamicFeeTx{
		ChainID:   genesis.Config.ChainID,
		Nonce:     uint64(1),
		To:        &feemanager.ContractAddress,
		Gas:       configExtra.FeeConfig.GasLimit.Uint64(),
		Value:     common.Big0,
		GasFeeCap: testLowFeeConfig.MinBaseFee, // this is too low for applied config, should fail
		GasTipCap: common.Big0,
		Data:      data,
	})

	signedTx2, err := types.SignTx(tx2, types.LatestSigner(genesis.Config), testKeys[0].ToECDSA())
	require.NoError(t, err)

	err = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})[0]
	require.ErrorIs(t, err, txpool.ErrUnderpriced)
}

// Test Allow Fee Recipients is disabled and, etherbase must be blackhole address
func TestAllowFeeRecipientDisabled(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testAllowFeeRecipientDisabled(t, scheme)
		})
	}
}

func testAllowFeeRecipientDisabled(t *testing.T, scheme string) {
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)))
	params.GetExtra(genesis.Config).AllowFeeRecipients = false // set to false initially
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)
	tvm := newVM(t, testVMConfig{
		genesisJSON: string(genesisJSON),
		configJSON:  getConfig(scheme, ""),
	})

	tvm.vm.miner.SetEtherbase(common.HexToAddress("0x0123456789")) // set non-blackhole address by force
	defer func() {
		require.NoError(t, tvm.vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], new(big.Int).Mul(firstTxAmount, big.NewInt(4)), 21000, big.NewInt(testMinGasPrice*3), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	msg, err := tvm.vm.WaitForEvent(t.Context())
	require.NoError(t, err)
	require.Equal(t, commonEng.PendingTxs, msg)

	blk, err := tvm.vm.BuildBlock(t.Context())
	require.NoError(t, err) // this won't return an error since miner will set the etherbase to blackhole address

	ethBlock := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	require.Equal(t, constants.BlackholeAddr, ethBlock.Coinbase())

	// Create empty block from blk
	internalBlk := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock)
	modifiedHeader := types.CopyHeader(internalBlk.GetEthBlock().Header())
	modifiedHeader.Coinbase = common.HexToAddress("0x0123456789") // set non-blackhole address by force
	modifiedBlock := types.NewBlock(
		modifiedHeader,
		internalBlk.GetEthBlock().Transactions(),
		nil,
		nil,
		trie.NewStackTrie(nil),
	)

	modifiedBlk, err := wrapBlock(modifiedBlock, tvm.vm)
	require.NoError(t, err)

	err = modifiedBlk.Verify(t.Context())
	require.ErrorIs(t, err, vmerrors.ErrInvalidCoinbase)
}

func TestAllowFeeRecipientEnabled(t *testing.T) {
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)))
	params.GetExtra(genesis.Config).AllowFeeRecipients = true
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)

	etherBase := common.HexToAddress("0x0123456789")
	c := config.NewDefaultConfig()
	c.FeeRecipient = etherBase.String()
	configJSON, err := json.Marshal(c)
	require.NoError(t, err)
	tvm := newVM(t, testVMConfig{
		genesisJSON: string(genesisJSON),
		configJSON:  string(configJSON),
	})

	defer func() {
		require.NoError(t, tvm.vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], new(big.Int).Mul(firstTxAmount, big.NewInt(4)), 21000, big.NewInt(testMinGasPrice*3), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	blk := issueAndAccept(t, tvm.vm)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, common.Hash(blk.ID()), newHead.Head.Hash(), "Expected new block to match")
	ethBlock := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	require.Equal(t, etherBase, ethBlock.Coinbase())
	// Verify that etherBase has received fees
	blkState, err := tvm.vm.blockChain.StateAt(ethBlock.Root())
	require.NoError(t, err)

	balance := blkState.GetBalance(etherBase)
	require.Equal(t, 1, balance.Cmp(common.U2560))
}

func TestRewardManagerPrecompileSetRewardAddress(t *testing.T) {
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)))

	params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
		rewardmanager.ConfigKey: rewardmanager.NewConfig(utils.NewUint64(0), testEthAddrs[0:1], nil, nil, nil),
	}
	params.GetExtra(genesis.Config).AllowFeeRecipients = true // enable this in genesis to test if this is recognized by the reward manager
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)

	etherBase := common.HexToAddress("0x0123456789") // give custom ether base
	c := config.NewDefaultConfig()
	c.FeeRecipient = etherBase.String()
	configJSON, err := json.Marshal(c)
	require.NoError(t, err)

	// arbitrary choice ahead of enableAllowListTimestamp
	// configure a network upgrade to remove the reward manager
	disableTime := time.Now().Add(10 * time.Hour)

	// configure a network upgrade to remove the allowlist
	upgradeConfig := fmt.Sprintf(`
		{
			"precompileUpgrades": [
				{
					"rewardManagerConfig": {
						"blockTimestamp": %d,
						"disable": true
					}
				}
			]
		}
		`, disableTime.Unix())

	tvm := newVM(t, testVMConfig{
		genesisJSON: string(genesisJSON),
		configJSON:  string(configJSON),
		upgradeJSON: upgradeConfig,
	})

	defer func() {
		require.NoError(t, tvm.vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	testAddr := common.HexToAddress("0x9999991111")
	data, err := rewardmanager.PackSetRewardAddress(testAddr)
	require.NoError(t, err)

	gas := 21000 + 240 + rewardmanager.SetRewardAddressGasCost + rewardmanager.RewardAddressChangedEventGasCost // 21000 for tx, 240 for tx data

	tx := types.NewTransaction(uint64(0), rewardmanager.ContractAddress, big.NewInt(1), gas, big.NewInt(testMinGasPrice), data)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk := issueAndAccept(t, tvm.vm)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	require.Equal(t, etherBase, ethBlock.Coinbase()) // reward address is activated at this block so this is fine

	tx1 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice*3), nil)
	signedTx1, err := types.SignTx(tx1, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[1].ToECDSA())
	require.NoError(t, err)

	txErrors = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk = issueAndAccept(t, tvm.vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	require.Equal(t, testAddr, ethBlock.Coinbase()) // reward address was activated at previous block
	// Verify that etherBase has received fees
	blkState, err := tvm.vm.blockChain.StateAt(ethBlock.Root())
	require.NoError(t, err)

	balance := blkState.GetBalance(testAddr)
	require.Equal(t, 1, balance.Cmp(common.U2560))

	// Test Case: Disable reward manager
	// This should revert back to enabling fee recipients
	previousBalance := blkState.GetBalance(etherBase)

	// issue a new block to trigger the upgrade
	tvm.vm.clock.Set(disableTime) // upgrade takes effect after a block is issued, so we can set vm's clock here.
	tx2 := types.NewTransaction(uint64(1), testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx2, err := types.SignTx(tx2, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[1].ToECDSA())
	require.NoError(t, err)

	txErrors = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk = issueAndAccept(t, tvm.vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	// Reward manager deactivated at this block, so we expect the parent state
	// to determine the coinbase for this block before full deactivation in the
	// next block.
	require.Equal(t, testAddr, ethBlock.Coinbase())
	require.GreaterOrEqual(t, int64(ethBlock.Time()), disableTime.Unix())

	tvm.vm.clock.Set(tvm.vm.clock.Time().Add(3 * time.Hour)) // let time pass to decrease gas price
	// issue another block to verify that the reward manager is disabled
	tx2 = types.NewTransaction(uint64(2), testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx2, err = types.SignTx(tx2, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[1].ToECDSA())
	require.NoError(t, err)

	txErrors = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk = issueAndAccept(t, tvm.vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	// reward manager was disabled at previous block
	// so this block should revert back to enabling fee recipients
	require.Equal(t, etherBase, ethBlock.Coinbase())
	require.GreaterOrEqual(t, int64(ethBlock.Time()), disableTime.Unix())

	// Verify that Blackhole has received fees
	blkState, err = tvm.vm.blockChain.StateAt(ethBlock.Root())
	require.NoError(t, err)

	balance = blkState.GetBalance(etherBase)
	require.Equal(t, 1, balance.Cmp(previousBalance))
}

func TestRewardManagerPrecompileAllowFeeRecipients(t *testing.T) {
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)))

	params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
		rewardmanager.ConfigKey: rewardmanager.NewConfig(utils.NewUint64(0), testEthAddrs[0:1], nil, nil, nil),
	}
	params.GetExtra(genesis.Config).AllowFeeRecipients = false // disable this in genesis
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)
	etherBase := common.HexToAddress("0x0123456789") // give custom ether base
	c := config.NewDefaultConfig()
	c.FeeRecipient = etherBase.String()
	configJSON, err := json.Marshal(c)
	require.NoError(t, err)
	// configure a network upgrade to remove the reward manager
	// arbitrary choice ahead of enableAllowListTimestamp
	// configure a network upgrade to remove the reward manager
	disableTime := time.Now().Add(10 * time.Hour)

	// configure a network upgrade to remove the allowlist
	upgradeConfig := fmt.Sprintf(`
		{
			"precompileUpgrades": [
				{
					"rewardManagerConfig": {
						"blockTimestamp": %d,
						"disable": true
					}
				}
			]
		}
		`, disableTime.Unix())
	tvm := newVM(t, testVMConfig{
		genesisJSON: string(genesisJSON),
		configJSON:  string(configJSON),
		upgradeJSON: upgradeConfig,
	})

	defer func() {
		require.NoError(t, tvm.vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	data, err := rewardmanager.PackAllowFeeRecipients()
	require.NoError(t, err)

	gas := 21000 + 240 + rewardmanager.SetRewardAddressGasCost + rewardmanager.RewardAddressChangedEventGasCost // 21000 for tx, 240 for tx data

	tx := types.NewTransaction(uint64(0), rewardmanager.ContractAddress, big.NewInt(1), gas, big.NewInt(testMinGasPrice), data)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)

	txErrors := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk := issueAndAccept(t, tvm.vm)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	require.Equal(t, constants.BlackholeAddr, ethBlock.Coinbase()) // reward address is activated at this block so this is fine

	tx1 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice*3), nil)
	signedTx1, err := types.SignTx(tx1, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[1].ToECDSA())
	require.NoError(t, err)

	txErrors = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk = issueAndAccept(t, tvm.vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	require.Equal(t, etherBase, ethBlock.Coinbase()) // reward address was activated at previous block
	// Verify that etherBase has received fees
	blkState, err := tvm.vm.blockChain.StateAt(ethBlock.Root())
	require.NoError(t, err)

	balance := blkState.GetBalance(etherBase)
	require.Equal(t, 1, balance.Cmp(common.U2560))

	// Test Case: Disable reward manager
	// This should revert back to burning fees
	previousBalance := blkState.GetBalance(constants.BlackholeAddr)

	tvm.vm.clock.Set(disableTime) // upgrade takes effect after a block is issued, so we can set vm's clock here.
	tx2 := types.NewTransaction(uint64(1), testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx2, err := types.SignTx(tx2, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[1].ToECDSA())
	require.NoError(t, err)

	txErrors = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk = issueAndAccept(t, tvm.vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	require.Equal(t, etherBase, ethBlock.Coinbase()) // reward address was activated at previous block
	require.GreaterOrEqual(t, int64(ethBlock.Time()), disableTime.Unix())

	tvm.vm.clock.Set(tvm.vm.clock.Time().Add(3 * time.Hour)) // let time pass so that gas price is reduced
	tx2 = types.NewTransaction(uint64(2), testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx2, err = types.SignTx(tx2, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[1].ToECDSA())
	require.NoError(t, err)

	txErrors = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk = issueAndAccept(t, tvm.vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	require.Equal(t, constants.BlackholeAddr, ethBlock.Coinbase()) // reward address was activated at previous block
	require.Greater(t, int64(ethBlock.Time()), disableTime.Unix())

	// Verify that Blackhole has received fees
	blkState, err = tvm.vm.blockChain.StateAt(ethBlock.Root())
	require.NoError(t, err)

	balance = blkState.GetBalance(constants.BlackholeAddr)
	require.Equal(t, 1, balance.Cmp(previousBalance))
}

func TestSkipChainConfigCheckCompatible(t *testing.T) {
	// The most recent network upgrade in Subnet-EVM is SubnetEVM itself, which cannot be disabled for this test since it results in
	// disabling dynamic fees and causes a panic since some code assumes that this is enabled.
	// TODO update this test when there is a future network upgrade that can be skipped in the config.
	t.Skip("no skippable upgrades")

	tvm := newVM(t, testVMConfig{
		genesisJSON: genesisJSONPreSubnetEVM,
		configJSON:  `{"pruning-enabled":true}`,
	})

	defer func() {
		require.NoError(t, tvm.vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := utilstest.NewKey(t)

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)
	errs := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range errs {
		require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
	}

	blk := issueAndAccept(t, tvm.vm)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, common.Hash(blk.ID()), newHead.Head.Hash(), "Expected new block to match")

	reinitVM := &VM{}
	// use the block's timestamp instead of 0 since rewind to genesis
	// is hardcoded to be allowed in core/genesis.go.
	genesisWithUpgrade := &core.Genesis{}
	require.NoError(t, json.Unmarshal([]byte(toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Durango])), genesisWithUpgrade))
	params.GetExtra(genesisWithUpgrade.Config).EtnaTimestamp = utils.TimeToNewUint64(blk.Timestamp())
	genesisWithUpgradeBytes, err := json.Marshal(genesisWithUpgrade)
	require.NoError(t, err)

	// Reset metrics to allow re-initialization
	tvm.vm.ctx.Metrics = metrics.NewPrefixGatherer()

	// this will not be allowed
	err = reinitVM.Initialize(t.Context(), tvm.vm.ctx, tvm.db, genesisWithUpgradeBytes, []byte{}, []byte{}, []*commonEng.Fx{}, tvm.appSender)
	require.ErrorContains(t, err, "mismatching Cancun fork timestamp in database") //nolint:forbidigo // uses upstream code

	// Reset metrics to allow re-initialization
	tvm.vm.ctx.Metrics = metrics.NewPrefixGatherer()

	// try again with skip-upgrade-check
	config := []byte(`{"skip-upgrade-check": true}`)
	require.NoError(t, reinitVM.Initialize(t.Context(), tvm.vm.ctx, tvm.db, genesisWithUpgradeBytes, []byte{}, config, []*commonEng.Fx{}, tvm.appSender))
	require.NoError(t, reinitVM.Shutdown(t.Context()))
}

func TestParentBeaconRootBlock(t *testing.T) {
	tests := []struct {
		name          string
		fork          upgradetest.Fork
		beaconRoot    *common.Hash
		expectedError error
	}{
		{
			name:          "non-empty parent beacon root in Durango",
			fork:          upgradetest.Durango,
			beaconRoot:    &common.Hash{0x01},
			expectedError: errInvalidParentBeaconRootBeforeCancun,
		},
		{
			name:          "empty parent beacon root in Durango",
			fork:          upgradetest.Durango,
			beaconRoot:    &common.Hash{},
			expectedError: errInvalidParentBeaconRootBeforeCancun,
		},
		{
			name:       "nil parent beacon root in Durango",
			fork:       upgradetest.Durango,
			beaconRoot: nil,
		},
		{
			name:          "non-empty parent beacon root in E-Upgrade (Cancun)",
			fork:          upgradetest.Etna,
			beaconRoot:    &common.Hash{0x01},
			expectedError: errParentBeaconRootNonEmpty,
		},
		{
			name:       "empty parent beacon root in E-Upgrade (Cancun)",
			fork:       upgradetest.Etna,
			beaconRoot: &common.Hash{},
		},
		{
			name:          "nil parent beacon root in E-Upgrade (Cancun)",
			fork:          upgradetest.Etna,
			beaconRoot:    nil,
			expectedError: errMissingParentBeaconRoot,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tvm := newVM(t, testVMConfig{
				fork: &test.fork,
			})

			defer func() {
				require.NoError(t, tvm.vm.Shutdown(t.Context()))
			}()

			tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
			require.NoError(t, err)

			txErrors := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
			for i, err := range txErrors {
				require.NoError(t, err, "Failed to add tx at index %d: %s", i, err)
			}

			msg, err := tvm.vm.WaitForEvent(t.Context())
			require.NoError(t, err)
			require.Equal(t, commonEng.PendingTxs, msg)

			blk, err := tvm.vm.BuildBlock(t.Context())
			require.NoError(t, err, "Failed to build block with import transaction")

			// Modify the block to have a parent beacon root
			ethBlock := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
			header := types.CopyHeader(ethBlock.Header())
			header.ParentBeaconRoot = test.beaconRoot
			parentBeaconEthBlock := ethBlock.WithSeal(header)

			parentBeaconBlock, err := wrapBlock(parentBeaconEthBlock, tvm.vm)
			require.NoError(t, err)

			_, err = tvm.vm.ParseBlock(t.Context(), parentBeaconBlock.Bytes())
			require.ErrorIs(t, err, test.expectedError)
			err = parentBeaconBlock.Verify(t.Context())
			require.ErrorIs(t, err, test.expectedError)
		})
	}
}

func TestStandaloneDB(t *testing.T) {
	vm := &VM{}
	ctx := utilstest.NewTestSnowContext(t, utilstest.SubnetEVMTestChainID)
	baseDB := memdb.New()
	atomicMemory := atomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = atomicMemory.NewSharedMemory(ctx.ChainID)
	sharedDB := prefixdb.New([]byte{1}, baseDB)
	// alter network ID to use standalone database
	ctx.NetworkID = 123456
	appSender := &enginetest.Sender{T: t}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error { return nil }
	configJSON := `{"database-type": "memdb"}`

	isDBEmpty := func(db database.Database) bool {
		it := db.NewIterator()
		defer it.Release()
		return !it.Next()
	}
	// Ensure that the database is empty
	require.True(t, isDBEmpty(baseDB))

	err := vm.Initialize(
		t.Context(),
		ctx,
		sharedDB,
		[]byte(toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Latest])),
		nil,
		[]byte(configJSON),
		[]*commonEng.Fx{},
		appSender,
	)
	defer func() {
		require.NoError(t, vm.Shutdown(t.Context()))
	}()
	require.NoError(t, err, "error initializing VM")
	require.NoError(t, vm.SetState(t.Context(), snow.Bootstrapping))
	require.NoError(t, vm.SetState(t.Context(), snow.NormalOp))

	// Issue a block
	acceptedBlockEvent := make(chan core.ChainEvent, 1)
	vm.blockChain.SubscribeChainAcceptedEvent(acceptedBlockEvent)
	tx0 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx0, err := types.SignTx(tx0, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(t, err)
	errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	require.NoError(t, errs[0])

	// accept block
	blk := issueAndAccept(t, vm)
	newBlock := <-acceptedBlockEvent
	require.Equal(t, newBlock.Block.Hash(), common.Hash(blk.ID()))

	// Ensure that the shared database is empty
	require.True(t, isDBEmpty(baseDB))
	// Ensure that the standalone database is not empty
	require.False(t, isDBEmpty(vm.db))
	require.False(t, isDBEmpty(vm.acceptedBlockDB))
}

func TestFeeManagerRegressionMempoolMinFeeAfterRestart(t *testing.T) {
	// Setup chain params
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)))
	precompileActivationTime := utils.NewUint64(genesis.Timestamp + 5) // 5 seconds after genesis
	configExtra := params.GetExtra(genesis.Config)
	configExtra.GenesisPrecompiles = extras.Precompiles{
		feemanager.ConfigKey: feemanager.NewConfig(precompileActivationTime, testEthAddrs[0:1], nil, nil, nil),
	}

	// set a higher fee config now
	testHighFeeConfig := commontype.FeeConfig{
		GasLimit:        big.NewInt(8_000_000),
		TargetBlockRate: 5, // in seconds

		MinBaseFee:               big.NewInt(50_000_000),
		TargetGas:                big.NewInt(18_000_000),
		BaseFeeChangeDenominator: big.NewInt(3396),

		MinBlockGasCost:  big.NewInt(0),
		MaxBlockGasCost:  big.NewInt(4_000_000),
		BlockGasCostStep: big.NewInt(500_000),
	}

	configExtra.FeeConfig = testHighFeeConfig
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)
	tvm := newVM(t, testVMConfig{
		genesisJSON: string(genesisJSON),
	})

	// tx pool min base fee should be the high fee config
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   genesis.Config.ChainID,
		Nonce:     uint64(0),
		To:        &feemanager.ContractAddress,
		Gas:       21_000,
		Value:     common.Big0,
		GasFeeCap: big.NewInt(5_000_000), // give a lower base fee
		GasTipCap: common.Big0,
		Data:      nil,
	})
	signedTx, err := types.SignTx(tx, types.LatestSigner(genesis.Config), testKeys[0].ToECDSA())
	require.NoError(t, err)

	errs := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0], txpool.ErrUnderpriced) // should fail because mempool expects higher fee

	// restart vm and try again
	restartedTVM, err := restartVM(tvm, testVMConfig{
		genesisJSON: string(genesisJSON),
	})
	require.NoError(t, err)
	restartedVM := restartedTVM.vm

	// it still should fail
	errs = restartedVM.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0], txpool.ErrUnderpriced)

	// send a tx to activate the precompile
	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	restartedVM.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)
	restartedVM.clock.Set(utils.Uint64ToTime(precompileActivationTime).Add(time.Second * 10))
	tx = types.NewTransaction(uint64(0), testEthAddrs[0], common.Big0, 21000, big.NewInt(testHighFeeConfig.MinBaseFee.Int64()), nil)
	signedTx, err = types.SignTx(tx, types.LatestSigner(genesis.Config), testKeys[0].ToECDSA())
	require.NoError(t, err)
	errs = restartedVM.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.NoError(t, errs[0])
	blk := issueAndAccept(t, restartedVM)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	// Contract is initialized but no preconfig is given, reader should return genesis fee config
	// We must query the current block header here (not genesis) because the FeeManager precompile
	// is only activated at precompileActivationTime, not at genesis. Querying the genesis header would
	// return the chain config fee config and lastChangedAt as zero, which is not correct after activation.
	feeConfig, lastChangedAt, err := restartedVM.blockChain.GetFeeConfigAt(restartedVM.blockChain.CurrentBlock())
	require.NoError(t, err)
	require.Equal(t, testHighFeeConfig, feeConfig)
	require.Equal(t, restartedVM.blockChain.CurrentBlock().Number, lastChangedAt)

	// set a lower fee config now through feemanager
	testLowFeeConfig := testHighFeeConfig
	testLowFeeConfig.MinBaseFee = big.NewInt(25_000_000)
	data, err := feemanager.PackSetFeeConfig(testLowFeeConfig)
	require.NoError(t, err)
	tx = types.NewTx(&types.DynamicFeeTx{
		ChainID:   genesis.Config.ChainID,
		Nonce:     uint64(1),
		To:        &feemanager.ContractAddress,
		Gas:       1_000_000,
		Value:     common.Big0,
		GasFeeCap: testHighFeeConfig.MinBaseFee, // the blockchain state still expects high fee
		Data:      data,
	})
	// let some time pass for block gas cost
	restartedVM.clock.Set(restartedVM.clock.Time().Add(time.Second * 10))
	signedTx, err = types.SignTx(tx, types.LatestSigner(genesis.Config), testKeys[0].ToECDSA())
	require.NoError(t, err)
	errs = restartedVM.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.NoError(t, errs[0])
	blk = issueAndAccept(t, restartedVM)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))

	// check that the fee config is updated
	block := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	feeConfig, lastChangedAt, err = restartedVM.blockChain.GetFeeConfigAt(block.Header())
	require.NoError(t, err)
	require.Equal(t, restartedVM.blockChain.CurrentBlock().Number, lastChangedAt)
	require.Equal(t, testLowFeeConfig, feeConfig)

	// send another tx with low fee
	tx = types.NewTransaction(uint64(2), testEthAddrs[0], common.Big0, 21000, big.NewInt(testLowFeeConfig.MinBaseFee.Int64()), nil)
	signedTx, err = types.SignTx(tx, types.LatestSigner(genesis.Config), testKeys[0].ToECDSA())
	require.NoError(t, err)
	errs = restartedVM.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.NoError(t, errs[0])
	// let some time pass for block gas cost and fees to be updated
	restartedVM.clock.Set(restartedVM.clock.Time().Add(time.Hour * 10))
	blk = issueAndAccept(t, restartedVM)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))

	// Regression: Mempool should see the new config after restart
	restartedTVM, err = restartVM(restartedTVM, testVMConfig{
		genesisJSON: string(genesisJSON),
	})
	restartedVM = restartedTVM.vm
	require.NoError(t, err)
	newTxPoolHeadChan = make(chan core.NewTxPoolReorgEvent, 1)
	restartedVM.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)
	// send a tx with low fee
	tx = types.NewTransaction(uint64(3), testEthAddrs[0], common.Big0, 21000, big.NewInt(testLowFeeConfig.MinBaseFee.Int64()), nil)
	signedTx, err = types.SignTx(tx, types.LatestSigner(genesis.Config), testKeys[0].ToECDSA())
	require.NoError(t, err)
	errs = restartedVM.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.NoError(t, errs[0])
	blk = issueAndAccept(t, restartedVM)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
}

func restartVM(tvm *testVM, tvmConfig testVMConfig) (*testVM, error) {
	if err := tvm.vm.Shutdown(context.Background()); err != nil {
		return nil, err
	}
	restartedVM := &VM{}
	tvm.vm.ctx.Metrics = metrics.NewPrefixGatherer()
	err := restartedVM.Initialize(context.Background(), tvm.vm.ctx, tvm.db, []byte(tvmConfig.genesisJSON), []byte(tvmConfig.upgradeJSON), []byte(tvmConfig.configJSON), []*commonEng.Fx{}, tvm.appSender)
	if err != nil {
		return nil, err
	}

	if !tvmConfig.isSyncing {
		err = restartedVM.SetState(context.Background(), snow.Bootstrapping)
		if err != nil {
			return nil, err
		}
		err = restartedVM.SetState(context.Background(), snow.NormalOp)
		if err != nil {
			return nil, err
		}
	}
	return &testVM{
		vm:           restartedVM,
		db:           tvm.db,
		atomicMemory: tvm.atomicMemory,
		appSender:    tvm.appSender,
		config:       tvmConfig,
	}, nil
}

func TestWaitForEvent(t *testing.T) {
	type result struct {
		msg commonEng.Message
		err error
	}

	fortunaFork := upgradetest.Fortuna
	for _, testCase := range []struct {
		name     string
		Fork     *upgradetest.Fork
		testCase func(*testing.T, *VM)
	}{
		{
			name: "WaitForEvent with context cancelled returns 0",
			testCase: func(t *testing.T, vm *VM) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond*100)
				defer cancel()

				msg, err := vm.WaitForEvent(ctx)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Zero(t, msg)
			},
		},
		{
			name: "WaitForEvent returns when a transaction is added to the mempool",
			testCase: func(t *testing.T, vm *VM) {
				t.Parallel()

				results := make(chan result)
				go func() {
					msg, err := vm.WaitForEvent(t.Context())
					results <- result{
						msg: msg,
						err: err,
					}
				}()

				tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
				require.NoError(t, err)

				for _, err := range vm.txPool.AddRemotesSync([]*types.Transaction{signedTx}) {
					require.NoError(t, err)
				}

				r := <-results
				require.NoError(t, r.err)
				require.Equal(t, commonEng.PendingTxs, r.msg)
			},
		},
		{
			name: "WaitForEvent build block after re-org",
			testCase: func(t *testing.T, vm *VM) {
				t.Parallel()

				tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
				require.NoError(t, err)

				err = errors.Join(vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})...)
				require.NoError(t, err)

				ctx, cancel := context.WithTimeout(t.Context(), time.Second)

				msg, err := vm.WaitForEvent(ctx)
				require.NoError(t, err)
				require.Equal(t, commonEng.PendingTxs, msg)

				cancel()

				blk, err := vm.BuildBlock(t.Context())
				require.NoError(t, err)

				require.NoError(t, blk.Verify(t.Context()))

				require.NoError(t, vm.SetPreference(t.Context(), blk.ID()))

				tx = types.NewTransaction(uint64(1), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
				signedTx, err = types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
				require.NoError(t, err)

				err = errors.Join(vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})...)
				require.NoError(t, err)

				ctx, cancel = context.WithTimeout(t.Context(), time.Second*2)
				defer cancel()

				msg, err = vm.WaitForEvent(ctx)
				require.NoError(t, err)
				require.Equal(t, commonEng.PendingTxs, msg)

				blk2, err := vm.BuildBlock(t.Context())
				require.NoError(t, err)

				require.NoError(t, blk2.Verify(t.Context()))

				require.NoError(t, blk.Accept(t.Context()))
				require.NoError(t, blk2.Accept(t.Context()))
			},
		},
		{
			name: "WaitForEvent doesn't return once a block is built and accepted",
			testCase: func(t *testing.T, vm *VM) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond*100)
				defer cancel()

				msg, err := vm.WaitForEvent(ctx)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Zero(t, msg)

				tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
				require.NoError(t, err)

				err = errors.Join(vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})...)
				require.NoError(t, err)

				blk, err := vm.BuildBlock(t.Context())
				require.NoError(t, err)

				require.NoError(t, blk.Verify(t.Context()))

				require.NoError(t, vm.SetPreference(t.Context(), blk.ID()))

				require.NoError(t, blk.Accept(t.Context()))

				ctx, cancel = context.WithTimeout(t.Context(), time.Millisecond*100)
				defer cancel()

				msg, err = vm.WaitForEvent(ctx)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Zero(t, msg)
			},
		},
		{
			name: "WaitForEvent for two accepted blocks in a row",
			testCase: func(t *testing.T, vm *VM) {
				t.Parallel()

				tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
				require.NoError(t, err)

				err = errors.Join(vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})...)
				require.NoError(t, err)

				blk, err := vm.BuildBlock(t.Context())
				require.NoError(t, err)

				require.NoError(t, blk.Verify(t.Context()))
				require.NoError(t, vm.SetPreference(t.Context(), blk.ID()))

				tx = types.NewTransaction(uint64(1), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
				signedTx, err = types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
				require.NoError(t, err)
				err = errors.Join(vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})...)
				require.NoError(t, err)

				time.Sleep(time.Second * 2)
				blk2, err := vm.BuildBlock(t.Context())
				require.NoError(t, err)

				require.NoError(t, blk2.Verify(t.Context()))

				tx = types.NewTransaction(uint64(2), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
				signedTx, err = types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
				require.NoError(t, err)
				err = errors.Join(vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})...)
				require.NoError(t, err)

				results := make(chan result)
				// We run WaitForEvent in a goroutine to ensure it can be safely called concurrently.
				go func() {
					msg, err := vm.WaitForEvent(t.Context())
					results <- result{
						msg: msg,
						err: err,
					}
				}()
				err = blk.Accept(t.Context())
				require.NoError(t, err)
				err = blk2.Accept(t.Context())
				require.NoError(t, err)
				require.NoError(t, vm.SetPreference(t.Context(), blk2.ID()))
				res := <-results
				require.NoError(t, res.err)
				require.Equal(t, commonEng.PendingTxs, res.msg)
			},
		},
		// TODO (ceyonur): remove this test after Granite is activated. (See https://github.com/ava-labs/coreth/issues/1318)
		{
			name: "WaitForEvent does not wait for new block to be built in fortuna",
			Fork: &fortunaFork,
			testCase: func(t *testing.T, vm *VM) {
				t.Parallel()

				signedTx := newSignedLegacyTx(t, vm.chainConfig, testKeys[0].ToECDSA(), 0, &testEthAddrs[1], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
				blk, err := IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
				require.NoError(t, err)
				require.NoError(t, blk.Accept(t.Context()))
				signedTx = newSignedLegacyTx(t, vm.chainConfig, testKeys[0].ToECDSA(), 1, &testEthAddrs[1], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)

				for _, err := range vm.txPool.AddRemotesSync([]*types.Transaction{signedTx}) {
					require.NoError(t, err)
				}

				msg, err := vm.WaitForEvent(t.Context())
				require.NoError(t, err)
				require.Equal(t, commonEng.PendingTxs, msg)
			},
		},
		// TODO (ceyonur): remove this test after Granite is activated. (See https://github.com/ava-labs/coreth/issues/1318)
		{
			name: "WaitForEvent waits for a delay with a retry in fortuna",
			Fork: &fortunaFork,
			testCase: func(t *testing.T, vm *VM) {
				lastBuildBlockTime := time.Now()
				signedTx := newSignedLegacyTx(t, vm.chainConfig, testKeys[0].ToECDSA(), 0, &testEthAddrs[1], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
				for _, err := range vm.txPool.AddRemotesSync([]*types.Transaction{signedTx}) {
					require.NoError(t, err)
				}
				_, err := vm.BuildBlock(t.Context())
				require.NoError(t, err)
				// we haven't advanced the tip to include the previous built block, so this is a retry
				signedTx = newSignedLegacyTx(t, vm.chainConfig, testKeys[1].ToECDSA(), 0, &testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
				for _, err := range vm.txPool.AddRemotesSync([]*types.Transaction{signedTx}) {
					require.NoError(t, err)
				}

				msg, err := vm.WaitForEvent(t.Context())
				require.NoError(t, err)
				require.Equal(t, commonEng.PendingTxs, msg)
				require.GreaterOrEqual(t, time.Since(lastBuildBlockTime), RetryDelay)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fork := upgradetest.Latest
			if testCase.Fork != nil {
				fork = *testCase.Fork
			}
			tvm := newVM(t, testVMConfig{
				fork: &fork,
			}).vm
			testCase.testCase(t, tvm)
			require.NoError(t, tvm.Shutdown(t.Context()))
		})
	}
}

func TestGenesisGasLimit(t *testing.T) {
	ctx, db, genesisBytes := setupGenesis(t, upgradetest.Granite)
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON(genesisBytes))
	// change the gas limit in the genesis to be different from the fee config
	genesis.GasLimit = params.GetExtra(genesis.Config).FeeConfig.GasLimit.Uint64() - 1
	genesisBytes, err := genesis.MarshalJSON()
	require.NoError(t, err)

	vm := &VM{}
	err = vm.Initialize(t.Context(), ctx, db, genesisBytes, []byte{}, []byte{}, []*commonEng.Fx{}, &enginetest.Sender{})
	// This should fail because the gas limit is different from the fee config
	require.ErrorIs(t, err, errVerifyGenesis)

	// This should succeed because the gas limit is the same as the fee config
	genesis.GasLimit = params.GetExtra(genesis.Config).FeeConfig.GasLimit.Uint64()
	genesisBytes, err = genesis.MarshalJSON()
	require.NoError(t, err)
	ctx.Metrics = metrics.NewPrefixGatherer()

	require.NoError(t, vm.Initialize(t.Context(), ctx, db, genesisBytes, []byte{}, []byte{}, []*commonEng.Fx{}, &enginetest.Sender{}))
	require.NoError(t, vm.Shutdown(t.Context()))
}

// Copied from rpc/testservice_test.go
type testService struct{}

type echoArgs struct {
	S string
}
type echoResult struct {
	String string
	Int    int
	Args   *echoArgs
}

func (*testService) Echo(str string, i int, args *echoArgs) echoResult {
	return echoResult{str, i, args}
}

// emulates server test
func TestCreateHandlers(t *testing.T) {
	var (
		ctx = t.Context()
		vm  = newVM(t, testVMConfig{
			genesisJSON: genesisJSONSubnetEVM,
		}).vm
	)
	defer func() {
		require.NoError(t, vm.Shutdown(ctx))
	}()

	handlers, err := vm.CreateHandlers(ctx)
	require.NoError(t, err)
	require.NotNil(t, handlers)

	handler, ok := handlers[ethRPCEndpoint]
	require.True(t, ok)
	server, ok := handler.(*rpc.Server)
	require.True(t, ok)

	// registers at test_echo
	require.NoError(t, server.RegisterName("test", new(testService)))
	var (
		batch        []rpc.BatchElem
		client       = rpc.DialInProc(server)
		maxResponses = node.DefaultConfig.BatchRequestLimit // Should be default
	)
	defer client.Close()

	// Make a request at limit, ensure that all requests are handled
	for i := 0; i < maxResponses; i++ {
		batch = append(batch, rpc.BatchElem{
			Method: "test_echo",
			Args:   []any{"x", 1},
			Result: new(echoResult),
		})
	}
	require.NoError(t, client.BatchCall(batch))
	for _, r := range batch {
		require.NoError(t, r.Error, "error in batch response")
	}

	// Create a new batch that is too large
	batch = nil
	for i := 0; i < maxResponses+1; i++ {
		batch = append(batch, rpc.BatchElem{
			Method: "test_echo",
			Args:   []any{"x", 1},
			Result: new(echoResult),
		})
	}
	require.NoError(t, client.BatchCall(batch))
	require.ErrorContains(t, batch[0].Error, "batch too large") //nolint:forbidigo // uses upstream code

	// All other elements should have an error indicating there's no response
	for _, elem := range batch[1:] {
		require.ErrorIs(t, elem.Error, rpc.ErrMissingBatchResponse)
	}
}

// TestBlockGasValidation tests the two validation checks:
// 1. invalid gas used relative to capacity
// 2. total intrinsic gas cost is greater than claimed gas used
func TestBlockGasValidation(t *testing.T) {
	newBlock := func(
		t *testing.T,
		vm *VM,
		claimedGasUsed uint64,
	) *types.Block {
		require := require.New(t)

		blk, err := vm.BuildBlock(t.Context())
		require.NoError(err)

		callPayload, err := payload.NewAddressedCall(nil, nil)
		require.NoError(err)
		unsignedMessage, err := avalancheWarp.NewUnsignedMessage(
			1,
			ids.Empty,
			callPayload.Bytes(),
		)
		require.NoError(err)
		signersBitSet := set.NewBits()
		warpSignature := &avalancheWarp.BitSetSignature{
			Signers: signersBitSet.Bytes(),
		}
		signedMessage, err := avalancheWarp.NewMessage(
			unsignedMessage,
			warpSignature,
		)
		require.NoError(err)

		// 9401 is the maximum number of predicates so that the block is less
		// than 2 MiB.
		const numPredicates = 9401
		accessList := make(types.AccessList, 0, numPredicates)
		predicate := predicate.New(signedMessage.Bytes())
		for range numPredicates {
			accessList = append(accessList, types.AccessTuple{
				Address:     warpcontract.ContractAddress,
				StorageKeys: predicate,
			})
		}

		tx, err := types.SignTx(
			types.NewTx(&types.DynamicFeeTx{
				ChainID:    vm.chainConfig.ChainID,
				Nonce:      1,
				To:         &testEthAddrs[0],
				Gas:        8_000_000, // block gas limit
				GasFeeCap:  big.NewInt(10),
				GasTipCap:  big.NewInt(10),
				Value:      common.Big0,
				AccessList: accessList,
			}),
			types.LatestSigner(vm.chainConfig),
			testKeys[0].ToECDSA(),
		)
		require.NoError(err)

		ethBlock := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
		modifiedHeader := types.CopyHeader(ethBlock.Header())

		// Set the gasUsed after calculating the extra prefix to support large
		// claimed gas used values.
		modifiedHeader.GasUsed = claimedGasUsed
		return types.NewBlock(
			modifiedHeader,
			[]*types.Transaction{tx},
			nil,
			nil,
			trie.NewStackTrie(nil),
		)
	}

	tests := []struct {
		name    string
		gasUsed uint64
		want    error
	}{
		{
			name:    "gas_used_over_capacity",
			gasUsed: math.MaxUint64,
			want:    errInvalidGasUsedRelativeToCapacity,
		},
		{
			name:    "intrinsic_gas_over_gas_used",
			gasUsed: 0,
			want:    errTotalIntrinsicGasCostExceedsClaimed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctx := t.Context()

			// Configure genesis with warp precompile enabled since test uses warp predicates
			genesis := &core.Genesis{}
			require.NoError(genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)))
			params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				warpcontract.ConfigKey: warpcontract.NewDefaultConfig(utils.TimeToNewUint64(upgrade.InitiallyActiveTime)),
			}
			genesisJSON, err := genesis.MarshalJSON()
			require.NoError(err)

			tvm := newVM(t, testVMConfig{
				genesisJSON: string(genesisJSON),
			})
			vm := tvm.vm
			defer func() {
				require.NoError(vm.Shutdown(ctx))
			}()

			// Add a transaction to the pool so BuildBlock doesn't create an empty block
			// (subnet-evm doesn't allow empty blocks)
			tx := types.NewTransaction(uint64(0), testEthAddrs[1], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
			require.NoError(err)
			errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
			for i, err := range errs {
				require.NoError(err, "Failed to add tx at index %d", i)
			}

			blk := newBlock(t, vm, test.gasUsed)

			modifiedBlk, err := wrapBlock(blk, vm)
			require.NoError(err)

			err = modifiedBlk.Verify(ctx)
			require.ErrorIs(err, test.want)
		})
	}
}

// newSignedLegacyTx builds a legacy transaction and signs it using the
// LatestSigner derived from the provided chain config.
func newSignedLegacyTx(
	t *testing.T,
	cfg *params.ChainConfig,
	key *ecdsa.PrivateKey,
	nonce uint64,
	to *common.Address,
	value *big.Int,
	gas uint64,
	gasPrice *big.Int,
	data []byte,
) *types.Transaction {
	t.Helper()

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       to,
		Value:    value,
		Gas:      gas,
		GasPrice: gasPrice,
		Data:     data,
	})
	signedTx, err := types.SignTx(tx, types.LatestSigner(cfg), key)
	require.NoError(t, err)
	return signedTx
}

// deployContract deploys the provided EVM bytecode using a prefunded test account
// and returns the created contract address. It is reusable for any contract code.
func deployContract(ctx context.Context, t *testing.T, vm *VM, gasPrice *big.Int, code []byte) common.Address {
	callerAddr := testEthAddrs[0]
	callerKey := testKeys[0]

	nonce := vm.txPool.Nonce(callerAddr)
	signedTx := newSignedLegacyTx(t, vm.chainConfig, callerKey.ToECDSA(), nonce, nil, big.NewInt(0), 1000000, gasPrice, code)

	for _, err := range vm.txPool.AddRemotesSync([]*types.Transaction{signedTx}) {
		require.NoError(t, err)
	}

	blk, err := vm.BuildBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, blk.Verify(ctx))
	require.NoError(t, vm.SetPreference(ctx, blk.ID()))
	require.NoError(t, blk.Accept(ctx))

	ethBlock := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	receipts := vm.blockChain.GetReceiptsByHash(ethBlock.Hash())
	require.Len(t, receipts, len(ethBlock.Transactions()))

	found := false
	for i, btx := range ethBlock.Transactions() {
		if btx.Hash() == signedTx.Hash() {
			found = true
			require.Equal(t, types.ReceiptStatusSuccessful, receipts[i].Status)
			break
		}
	}
	require.True(t, found, "deployContract: expected deploy tx %s to be included in block %s (caller=%s, nonce=%d)",
		signedTx.Hash().Hex(),
		ethBlock.Hash().Hex(),
		callerAddr.Hex(),
		nonce,
	)

	return crypto.CreateAddress(callerAddr, nonce)
}

func TestDelegatePrecompile_BehaviorAcrossUpgrades(t *testing.T) {
	ctx := t.Context()
	tests := []struct {
		name                  string
		fork                  upgradetest.Fork
		deployGasPrice        *big.Int
		txGasPrice            *big.Int
		preDeployTime         int64
		setTime               int64
		refillCapacityFortuna bool
		wantIncluded          bool
		wantReceiptStatus     uint64
	}{
		{
			name:           "granite_should_revert",
			fork:           upgradetest.Granite,
			deployGasPrice: big.NewInt(testMinGasPrice),
			txGasPrice:     big.NewInt(testMinGasPrice),
			// Time is irrelevant as only the fork dictates the logic
			refillCapacityFortuna: false,
			wantIncluded:          true,
			wantReceiptStatus:     types.ReceiptStatusFailed,
		},
		{
			name:                  "fortuna_post_cutoff_should_invalidate",
			fork:                  upgradetest.Fortuna,
			deployGasPrice:        big.NewInt(testMinGasPrice),
			txGasPrice:            big.NewInt(testMinGasPrice),
			setTime:               params.InvalidateDelegateUnix + 1,
			refillCapacityFortuna: true,
			wantIncluded:          false,
		},
		{
			name:                  "fortuna_pre_cutoff_should_succeed",
			fork:                  upgradetest.Fortuna,
			deployGasPrice:        big.NewInt(testMinGasPrice),
			txGasPrice:            big.NewInt(testMinGasPrice),
			preDeployTime:         params.InvalidateDelegateUnix - acp176.TimeToFillCapacity - 1,
			refillCapacityFortuna: true,
			wantIncluded:          true,
			wantReceiptStatus:     types.ReceiptStatusSuccessful,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			genesis := &core.Genesis{}
			require.NoError(t, genesis.UnmarshalJSON([]byte(toGenesisJSON(paramstest.ForkToChainConfig[tt.fork]))))
			params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				warpcontract.ConfigKey: warpcontract.NewDefaultConfig(utils.TimeToNewUint64(upgrade.InitiallyActiveTime)),
			}
			genesisJSON, err := genesis.MarshalJSON()
			require.NoError(t, err)

			vm := newVM(t, testVMConfig{
				genesisJSON: string(genesisJSON),
				fork:        &tt.fork,
			}).vm
			defer func() {
				require.NoError(t, vm.Shutdown(ctx))
			}()

			if tt.preDeployTime != 0 {
				vm.clock.Set(time.Unix(tt.preDeployTime, 0))
			}

			contractAddr := deployContract(ctx, t, vm, tt.deployGasPrice, common.FromHex(delegateCallPrecompileCode))

			if tt.setTime != 0 {
				vm.clock.Set(time.Unix(tt.setTime, 0))
			}

			if tt.refillCapacityFortuna {
				// Ensure gas capacity is refilled relative to the parent block's timestamp
				parent := vm.blockChain.CurrentBlock()
				parentTime := time.Unix(int64(parent.Time), 0)
				minRefillTime := parentTime.Add(acp176.TimeToFillCapacity * time.Second)
				if vm.clock.Time().Before(minRefillTime) {
					vm.clock.Set(minRefillTime)
				}
			}

			data := crypto.Keccak256([]byte("delegateSendHello()"))[:4]
			nonce := vm.txPool.Nonce(testEthAddrs[0])
			signedTx := newSignedLegacyTx(t, vm.chainConfig, testKeys[0].ToECDSA(), nonce, &contractAddr, big.NewInt(0), 100000, tt.txGasPrice, data)

			blk, err := IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
			if !tt.wantIncluded {
				// On subnet-evm, InvalidateExecution causes the transaction to be excluded from the block.
				// BuildBlock will create a block but it will fail verification because it's empty
				// and subnet-evm doesn't allow empty blocks.
				require.ErrorIs(t, err, errEmptyBlock)
				return
			}
			require.NoError(t, err)
			require.NoError(t, blk.Accept(ctx))

			ethBlock := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock

			require.Len(t, ethBlock.Transactions(), 1)
			receipts := vm.blockChain.GetReceiptsByHash(ethBlock.Hash())
			require.Len(t, receipts, 1)
			require.Equal(t, tt.wantReceiptStatus, receipts[0].Status)
		})
	}
}

func TestMinDelayExcessInHeader(t *testing.T) {
	tests := []struct {
		name                   string
		fork                   upgradetest.Fork
		desiredMinDelay        *uint64
		expectedMinDelayExcess *acp226.DelayExcess
	}{
		{
			name:                   "pre_granite_no_min_delay_excess",
			fork:                   upgradetest.Fortuna,
			desiredMinDelay:        nil,
			expectedMinDelayExcess: nil,
		},
		{
			name:                   "pre_granite_min_delay_excess",
			fork:                   upgradetest.Fortuna,
			desiredMinDelay:        utils.NewUint64(1000),
			expectedMinDelayExcess: nil,
		},
		{
			name:                   "granite_first_block_initial_delay_excess",
			fork:                   upgradetest.Granite,
			desiredMinDelay:        nil,
			expectedMinDelayExcess: utilstest.PointerTo(acp226.DelayExcess(acp226.InitialDelayExcess)),
		},
		{
			name:                   "granite_with_excessive_desired_min_delay_excess",
			fork:                   upgradetest.Granite,
			desiredMinDelay:        utils.NewUint64(4000),
			expectedMinDelayExcess: utilstest.PointerTo(acp226.DelayExcess(acp226.InitialDelayExcess + acp226.MaxDelayExcessDiff)),
		},
		{
			name:                   "granite_with_zero_desired_min_delay_excess",
			fork:                   upgradetest.Granite,
			desiredMinDelay:        utils.NewUint64(0),
			expectedMinDelayExcess: utilstest.PointerTo(acp226.DelayExcess(acp226.InitialDelayExcess - acp226.MaxDelayExcessDiff)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctx := t.Context()
			var configJSON string
			if test.desiredMinDelay != nil {
				// convert excess to delay
				configJSON = fmt.Sprintf(`{"min-delay-target": %d}`, *test.desiredMinDelay)
			}
			vm := newVM(t, testVMConfig{
				fork:       &test.fork,
				configJSON: configJSON,
			})

			defer func() {
				require.NoError(vm.vm.Shutdown(ctx))
			}()

			// Build a block
			signedTx := newSignedLegacyTx(t, vm.vm.chainConfig, testKeys[0].ToECDSA(), 0, &testEthAddrs[1], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
			txErrors := vm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
			for _, err := range txErrors {
				require.NoError(err)
			}

			blk, err := vm.vm.BuildBlock(ctx)
			require.NoError(err)

			// Check the min delay excess in the header
			ethBlock := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
			headerExtra := customtypes.GetHeaderExtra(ethBlock.Header())

			require.Equal(test.expectedMinDelayExcess, headerExtra.MinDelayExcess, "expected %s, got %s", test.expectedMinDelayExcess, headerExtra.MinDelayExcess)
		})
	}
}

// Tests that querying states no longer in memory is still possible when in
// archival mode.
//
// Querying for the nonce of the zero address at various heights is sufficient
// as this succeeds only if the EVM has the matching trie at each height.
func TestArchivalQueries(t *testing.T) {
	// Setting the state history to 5 means that we keep around only the 5 latest
	// tries in memory. By creating numBlocks (10), we'll have:
	//	- Tries 0-5: on-disk
	// 	- Tries 6-10: in-memory
	tests := []struct {
		name   string
		config string
	}{
		{
			name: "firewood",
			config: `{
				"state-scheme": "firewood",
				"snapshot-cache": 0,
				"pruning-enabled": false,
				"state-sync-enabled": false,
				"state-history": 5
			}`,
		},
		{
			name: "hashdb",
			config: `{
				"state-scheme": "hash",
				"pruning-enabled": false,
				"state-history": 5
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := t.Context()

			vm := newVM(t, testVMConfig{configJSON: tt.config})

			numBlocks := 10
			for range numBlocks {
				nonce := vm.vm.txPool.Nonce(testEthAddrs[0])
				signedTx := newSignedLegacyTx(
					t,
					vm.vm.chainConfig,
					testKeys[0].ToECDSA(),
					nonce,
					&common.Address{},
					big.NewInt(0),
					21_000,
					big.NewInt(testMinGasPrice),
					nil,
				)

				blk, err := IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm.vm)
				require.NoError(err)

				require.NoError(blk.Accept(ctx))
			}

			handlers, err := vm.vm.CreateHandlers(ctx)
			require.NoError(err)

			server := httptest.NewServer(handlers[ethRPCEndpoint])
			t.Cleanup(server.Close)

			client, err := ethclient.Dial(server.URL)
			require.NoError(err)

			for i := 0; i <= numBlocks; i++ {
				nonce, err := client.NonceAt(ctx, common.Address{}, big.NewInt(int64(i)))
				require.NoErrorf(err, "failed to get nonce at block %d", i)
				require.Zero(nonce)
			}
		})
	}
}

func IssueTxsAndBuild(txs []*types.Transaction, vm *VM) (snowman.Block, error) {
	errs := vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("failed to add tx at index %d: %w", i, err)
		}
	}

	msg, err := vm.WaitForEvent(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to wait for event: %w", err)
	}
	if msg != commonEng.PendingTxs {
		return nil, fmt.Errorf("expected pending txs, got %v", msg)
	}

	block, err := vm.BuildBlock(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to build block with transaction: %w", err)
	}

	if err := block.Verify(context.Background()); err != nil {
		return nil, fmt.Errorf("block verification failed: %w", err)
	}

	return block, nil
}

func IssueTxsAndSetPreference(txs []*types.Transaction, vm *VM) (snowman.Block, error) {
	block, err := IssueTxsAndBuild(txs, vm)
	if err != nil {
		return nil, err
	}

	if err := vm.SetPreference(context.Background(), block.ID()); err != nil {
		return nil, fmt.Errorf("failed to set preference: %w", err)
	}

	return block, nil
}

func TestInspectDatabases(t *testing.T) {
	var (
		vm = newVM(t, testVMConfig{}).vm
		db = memdb.New()
	)

	require.NoError(t, vm.initializeDBs(db))
	require.NoError(t, vm.inspectDatabases())
}
