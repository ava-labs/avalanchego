// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
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
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/coreth/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/eth"
	"github.com/ava-labs/avalanchego/graft/coreth/miner"
	"github.com/ava-labs/avalanchego/graft/coreth/node"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/extension"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/avalanchego/graft/coreth/rpc"
	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	warpcontract "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	ethparams "github.com/ava-labs/libevm/params"
)

func TestMain(m *testing.M) {
	RegisterAllLibEVMExtras()

	// This MUST be called after [params.RegisterExtras].
	genesisJSONCancun = vmtest.GenesisJSON(activateCancun(params.TestChainConfig))

	os.Exit(m.Run())
}

const delegateCallPrecompileCode = "6080604052348015600e575f5ffd5b506106608061001c5f395ff3fe608060405234801561000f575f5ffd5b506004361061003f575f3560e01c80638b336b5e14610043578063b771b3bc14610061578063e4246eec1461007f575b5f5ffd5b61004b61009d565b604051610058919061029e565b60405180910390f35b610069610256565b6040516100769190610331565b60405180910390f35b61008761026e565b604051610094919061036a565b60405180910390f35b5f5f6040516020016100ae906103dd565b60405160208183030381529060405290505f63ee5b48eb60e01b826040516024016100d9919061046b565b604051602081830303815290604052907bffffffffffffffffffffffffffffffffffffffffffffffffffffffff19166020820180517bffffffffffffffffffffffffffffffffffffffffffffffffffffffff838183161783525050505090505f5f73020000000000000000000000000000000000000573ffffffffffffffffffffffffffffffffffffffff168360405161017391906104c5565b5f60405180830381855af49150503d805f81146101ab576040519150601f19603f3d011682016040523d82523d5f602084013e6101b0565b606091505b5091509150816101f5576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101ec9061054b565b60405180910390fd5b808060200190518101906102099190610597565b94505f5f1b850361024f576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016102469061060c565b60405180910390fd5b5050505090565b73020000000000000000000000000000000000000581565b73020000000000000000000000000000000000000581565b5f819050919050565b61029881610286565b82525050565b5f6020820190506102b15f83018461028f565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f819050919050565b5f6102f96102f46102ef846102b7565b6102d6565b6102b7565b9050919050565b5f61030a826102df565b9050919050565b5f61031b82610300565b9050919050565b61032b81610311565b82525050565b5f6020820190506103445f830184610322565b92915050565b5f610354826102b7565b9050919050565b6103648161034a565b82525050565b5f60208201905061037d5f83018461035b565b92915050565b5f82825260208201905092915050565b7f68656c6c6f0000000000000000000000000000000000000000000000000000005f82015250565b5f6103c7600583610383565b91506103d282610393565b602082019050919050565b5f6020820190508181035f8301526103f4816103bb565b9050919050565b5f81519050919050565b5f82825260208201905092915050565b8281835e5f83830152505050565b5f601f19601f8301169050919050565b5f61043d826103fb565b6104478185610405565b9350610457818560208601610415565b61046081610423565b840191505092915050565b5f6020820190508181035f8301526104838184610433565b905092915050565b5f81905092915050565b5f61049f826103fb565b6104a9818561048b565b93506104b9818560208601610415565b80840191505092915050565b5f6104d08284610495565b915081905092915050565b7f44656c65676174652063616c6c20746f2073656e64576172704d6573736167655f8201527f206661696c656400000000000000000000000000000000000000000000000000602082015250565b5f610535602783610383565b9150610540826104db565b604082019050919050565b5f6020820190508181035f83015261056281610529565b9050919050565b5f5ffd5b61057681610286565b8114610580575f5ffd5b50565b5f815190506105918161056d565b92915050565b5f602082840312156105ac576105ab610569565b5b5f6105b984828501610583565b91505092915050565b7f4661696c656420746f2073656e642077617270206d65737361676500000000005f82015250565b5f6105f6601b83610383565b9150610601826105c2565b602082019050919050565b5f6020820190508181035f830152610623816105ea565b905091905056fea2646970667358221220192acba01cff6d70ce187c63c7ccac116d811f6c35e316fde721f14929ced12564736f6c634300081e0033"

var (
	genesisJSONCancun string // set in [TestMain] to be after [params.RegisterExtras]

	activateCancun = func(cfg *params.ChainConfig) *params.ChainConfig {
		cpy := *cfg
		cpy.ShanghaiTime = utils.NewUint64(0)
		cpy.CancunTime = utils.NewUint64(0)
		return &cpy
	}
)

func defaultExtensions() *extension.Config {
	return &extension.Config{
		SyncSummaryProvider: &message.BlockSyncSummaryProvider{},
		SyncableParser:      &message.BlockSyncSummaryParser{},
		Clock:               &mockable.Clock{},
	}
}

// newDefaultTestVM returns a new instance of the VM with default extensions
// This should not be called if the VM is being extended
func newDefaultTestVM() *VM {
	vm := &VM{}
	if err := vm.SetExtensionConfig(defaultExtensions()); err != nil {
		panic(err)
	}
	return vm
}

func TestVMContinuousProfiler(t *testing.T) {
	profilerDir := t.TempDir()
	profilerFrequency := 500 * time.Millisecond
	configJSON := fmt.Sprintf(`{"continuous-profiler-dir": %q,"continuous-profiler-frequency": "500ms"}`, profilerDir)
	fork := upgradetest.Latest
	vm := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:       &fork,
		ConfigJSON: configJSON,
	})
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

func TestVMUpgrades(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testVMUpgrades(t, scheme)
		})
	}
}

func testVMUpgrades(t *testing.T, scheme string) {
	genesisTests := []struct {
		fork             upgradetest.Fork
		expectedGasPrice *big.Int
	}{
		{
			fork:             upgradetest.ApricotPhase3,
			expectedGasPrice: big.NewInt(0),
		},
		{
			fork:             upgradetest.ApricotPhase4,
			expectedGasPrice: big.NewInt(0),
		},
		{
			fork:             upgradetest.ApricotPhase5,
			expectedGasPrice: big.NewInt(0),
		},
		{
			fork:             upgradetest.ApricotPhasePre6,
			expectedGasPrice: big.NewInt(0),
		},
		{
			fork:             upgradetest.ApricotPhase6,
			expectedGasPrice: big.NewInt(0),
		},
		{
			fork:             upgradetest.ApricotPhasePost6,
			expectedGasPrice: big.NewInt(0),
		},
		{
			fork:             upgradetest.Banff,
			expectedGasPrice: big.NewInt(0),
		},
		{
			fork:             upgradetest.Cortina,
			expectedGasPrice: big.NewInt(0),
		},
		{
			fork:             upgradetest.Durango,
			expectedGasPrice: big.NewInt(0),
		},
	}

	for _, test := range genesisTests {
		t.Run(test.fork.String(), func(t *testing.T) {
			require := require.New(t)
			vm := newDefaultTestVM()
			vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork:   &test.fork,
				Scheme: scheme,
			})

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

func TestBuildEthTxBlock(t *testing.T) {
	// This test is done for all schemes to ensure the VM can be started with any scheme.
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme, customrawdb.FirewoodScheme} {
		t.Run(scheme, func(t *testing.T) {
			testBuildEthTxBlock(t, scheme)
		})
	}
}

func testBuildEthTxBlock(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.ApricotPhase2
	vm := newDefaultTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)
	blk1, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
	require.NoError(err, "Failed to issue txs and build block")

	require.NoError(blk1.Accept(t.Context()))

	newHead := <-newTxPoolHeadChan
	require.Equal(common.Hash(blk1.ID()), newHead.Head.Hash())

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[1].ToECDSA(), uint64(i), &vmtest.TestEthAddrs[1], big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		txs[i] = signedTx
	}
	blk2, err := vmtest.IssueTxsAndSetPreference(txs, vm)
	require.NoError(err)

	require.NoError(blk2.Accept(t.Context()))

	newHead = <-newTxPoolHeadChan
	require.Equal(common.Hash(blk2.ID()), newHead.Head.Hash())

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(blk2.ID(), lastAcceptedID, "Expected last accepted blockID to be the accepted block")

	ethBlk1 := blk1.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	require.True(vm.blockChain.HasState(ethBlk1.Root()))

	// Clear the cache and ensure that GetBlock returns internal blocks with the correct status
	vm.State.Flush()
	blk2Refreshed, err := vm.GetBlockInternal(t.Context(), blk2.ID())
	require.NoError(err)

	blk1RefreshedID := blk2Refreshed.Parent()
	blk1Refreshed, err := vm.GetBlockInternal(t.Context(), blk1RefreshedID)
	require.NoError(err)

	require.Equal(blk1.ID(), blk1Refreshed.ID())

	// Close the vm and all databases
	require.NoError(vm.Shutdown(t.Context()))

	restartedVM := newDefaultTestVM()
	newCTX := snowtest.Context(t, snowtest.CChainID)
	newCTX.NetworkUpgrades = upgradetest.GetConfig(fork)
	newCTX.ChainDataDir = tvm.Ctx.ChainDataDir
	conf, err := vmtest.OverrideSchemeConfig(scheme, "")
	require.NoError(err)
	require.NoError(restartedVM.Initialize(
		t.Context(),
		newCTX,
		tvm.DB,
		[]byte(vmtest.GenesisJSON(paramstest.ForkToChainConfig[fork])),
		[]byte(""),
		[]byte(conf),
		[]*commonEng.Fx{},
		nil,
	))

	// State root should not have been committed and discarded on restart
	ethBlk1Root := ethBlk1.Root()
	require.False(restartedVM.Ethereum().BlockChain().HasState(ethBlk1Root), "Expected blk1 state root to be pruned after blk2 was accepted on top of it in pruning mode")

	// State root should be committed when accepted tip on shutdown
	ethBlk2 := blk2.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	ethBlk2Root := ethBlk2.Root()
	require.True(restartedVM.Ethereum().BlockChain().HasState(ethBlk2Root), "Expected blk2 state root to not be pruned after shutdown (last accepted tip should be committed)")

	// Shutdown the newest VM
	require.NoError(restartedVM.Shutdown(t.Context()))
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
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testSetPreferenceRace(t, scheme)
		})
	}
}

func testSetPreferenceRace(t *testing.T, scheme string) {
	require := require.New(t)
	// Create two VMs which will agree on block A and then
	// build the two distinct preferred chains above
	fork := upgradetest.NoUpgrades
	conf := vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	}
	vm1 := newDefaultTestVM()
	vm2 := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm1, conf)
	vmtest.SetupTestVM(t, vm2, conf)

	defer func() {
		require.NoError(vm1.Shutdown(t.Context()))
		require.NoError(vm2.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	signedTx := newSignedLegacyTx(t, vm1.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, big.NewInt(ap0.MinGasPrice), nil)
	vm1BlkA, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm1)
	require.NoError(err)
	vm2BlkA, err := vm2.ParseBlock(t.Context(), vm1BlkA.Bytes())
	require.NoError(err)
	require.NoError(vm2BlkA.Verify(t.Context()))
	require.NoError(vm2.SetPreference(t.Context(), vm2BlkA.ID()))
	require.NoError(vm1BlkA.Accept(t.Context()))
	require.NoError(vm2BlkA.Accept(t.Context()))

	newHead := <-newTxPoolHeadChan1
	require.Equal(common.Hash(vm1BlkA.ID()), newHead.Head.Hash())
	newHead = <-newTxPoolHeadChan2
	require.Equal(common.Hash(vm2BlkA.ID()), newHead.Head.Hash())

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx := newSignedLegacyTx(t, vm1.chainConfig, vmtest.TestKeys[1].ToECDSA(), uint64(i), &vmtest.TestEthAddrs[1], big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	_, err = vmtest.IssueTxsAndSetPreference(txs, vm1)
	require.NoError(err)

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	vm2BlkC, err := vmtest.IssueTxsAndSetPreference(txs[0:5], vm2)
	require.NoError(err)

	newHead = <-newTxPoolHeadChan2
	require.Equal(common.Hash(vm2BlkC.ID()), newHead.Head.Hash())

	// Block D
	vm2BlkD, err := vmtest.IssueTxsAndSetPreference(txs[5:10], vm2)
	require.NoError(err)

	// VM1 receives blkC and blkD from VM1
	// and happens to call SetPreference on blkD without ever calling SetPreference
	// on blkC
	// Here we parse them in reverse order to simulate receiving a chain from the tip
	// back to the last accepted block as would typically be the case in the consensus
	// engine
	vm1BlkD, err := vm1.ParseBlock(t.Context(), vm2BlkD.Bytes())
	require.NoError(err)
	vm1BlkC, err := vm1.ParseBlock(t.Context(), vm2BlkC.Bytes())
	require.NoError(err)

	// The blocks must be verified in order. This invariant is maintained
	// in the consensus engine.
	require.NoError(vm1BlkC.Verify(t.Context()))
	require.NoError(vm1BlkD.Verify(t.Context()))

	// Set VM1's preference to blockD, skipping blockC
	require.NoError(vm1.SetPreference(t.Context(), vm1BlkD.ID()))

	// Accept the longer chain on both VMs and ensure there are no errors
	// VM1 Accepts the blocks in order
	require.NoError(vm1BlkC.Accept(t.Context()))
	require.NoError(vm1BlkD.Accept(t.Context()))

	// VM2 Accepts the blocks in order
	require.NoError(vm2BlkC.Accept(t.Context()))
	require.NoError(vm2BlkD.Accept(t.Context()))

	log.Info("Validating canonical chain")
	// Verify the Canonical Chain for Both VMs
	require.NoError(vm1.blockChain.ValidateCanonicalChain())
	require.NoError(vm2.blockChain.ValidateCanonicalChain())
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
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testReorgProtection(t, scheme)
		})
	}
}

func testReorgProtection(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.NoUpgrades
	vm1 := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm1, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})
	vm2 := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm2, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})

	defer func() {
		require.NoError(vm1.Shutdown(t.Context()))
		require.NoError(vm2.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := vmtest.TestKeys[1].ToECDSA()
	address := vmtest.TestEthAddrs[1]

	signedTx := newSignedLegacyTx(t, vm1.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, big.NewInt(ap0.MinGasPrice), nil)
	vm1BlkA, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm1)
	require.NoError(err)

	require.NoError(vm1BlkA.Verify(t.Context()))
	require.NoError(vm1.SetPreference(t.Context(), vm1BlkA.ID()))

	vm2BlkA, err := vm2.ParseBlock(t.Context(), vm1BlkA.Bytes())
	require.NoError(err)
	require.NoError(vm2BlkA.Verify(t.Context()))
	require.NoError(vm2.SetPreference(t.Context(), vm2BlkA.ID()))

	require.NoError(vm1BlkA.Accept(t.Context()))
	require.NoError(vm2BlkA.Accept(t.Context()))

	newHead := <-newTxPoolHeadChan1
	require.Equal(common.Hash(vm1BlkA.ID()), newHead.Head.Hash())
	newHead = <-newTxPoolHeadChan2
	require.Equal(common.Hash(vm2BlkA.ID()), newHead.Head.Hash())

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx := newSignedLegacyTx(t, vm1.chainConfig, key, uint64(i), &address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	vm1BlkB, err := vmtest.IssueTxsAndSetPreference(txs, vm1)
	require.NoError(err)

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	vm2BlkC, err := vmtest.IssueTxsAndBuild(txs[0:5], vm2)
	require.NoError(err)

	vm1BlkC, err := vm1.ParseBlock(t.Context(), vm2BlkC.Bytes())
	require.NoError(err)

	require.NoError(vm1BlkC.Verify(t.Context()))

	// Accept B, such that block C should get Rejected.
	require.NoError(vm1BlkB.Accept(t.Context()))

	// The below (setting preference blocks that have a common ancestor
	// with the preferred chain lower than the last finalized block)
	// should NEVER happen. However, the VM defends against this
	// just in case.
	err = vm1.SetPreference(t.Context(), vm1BlkC.ID())
	require.ErrorContains(err, "cannot orphan finalized block") //nolint:forbidigo // uses upstream code
	err = vm1BlkC.Accept(t.Context())
	require.ErrorContains(err, "expected accepted block to have parent") //nolint:forbidigo // uses upstream code
}

// Regression test to ensure that a VM that accepts block C while preferring
// block B will trigger a reorg.
//
//	  A
//	 / \
//	B   C
func TestNonCanonicalAccept(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testNonCanonicalAccept(t, scheme)
		})
	}
}

func testNonCanonicalAccept(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.NoUpgrades
	tvmConfig := vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	}
	vm1 := newDefaultTestVM()
	vm2 := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm1, tvmConfig)
	vmtest.SetupTestVM(t, vm2, tvmConfig)

	defer func() {
		require.NoError(vm1.Shutdown(t.Context()))
		require.NoError(vm2.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := vmtest.TestKeys[1].ToECDSA()
	address := vmtest.TestEthAddrs[1]

	signedTx := newSignedLegacyTx(t, vm1.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, big.NewInt(ap0.MinGasPrice), nil)
	vm1BlkA, err := vmtest.IssueTxsAndBuild([]*types.Transaction{signedTx}, vm1)
	require.NoError(err)

	_, err = vm1.GetBlockIDAtHeight(t.Context(), vm1BlkA.Height())
	require.ErrorIs(err, database.ErrNotFound, "Expected unaccepted block not to be indexed by height")

	require.NoError(vm1.SetPreference(t.Context(), vm1BlkA.ID()))

	vm2BlkA, err := vm2.ParseBlock(t.Context(), vm1BlkA.Bytes())
	require.NoError(err)
	require.NoError(vm2BlkA.Verify(t.Context()))
	_, err = vm2.GetBlockIDAtHeight(t.Context(), vm2BlkA.Height())
	require.ErrorIs(err, database.ErrNotFound, "Expected unaccepted block not to be indexed by height")
	require.NoError(vm2.SetPreference(t.Context(), vm2BlkA.ID()))

	require.NoError(vm1BlkA.Accept(t.Context()))
	blkID, err := vm1.GetBlockIDAtHeight(t.Context(), vm1BlkA.Height())
	require.NoError(err)
	require.Equal(blkID, vm1BlkA.ID())
	require.NoError(vm2BlkA.Accept(t.Context()))

	blkID, err = vm2.GetBlockIDAtHeight(t.Context(), vm2BlkA.Height())
	require.NoError(err)
	require.Equal(blkID, vm2BlkA.ID())

	newHead := <-newTxPoolHeadChan1
	require.Equal(common.Hash(vm1BlkA.ID()), newHead.Head.Hash())
	newHead = <-newTxPoolHeadChan2
	require.Equal(common.Hash(vm2BlkA.ID()), newHead.Head.Hash())

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx := newSignedLegacyTx(t, vm1.chainConfig, key, uint64(i), &address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	vm1BlkB, err := vmtest.IssueTxsAndBuild(txs, vm1)
	require.NoError(err)

	_, err = vm1.GetBlockIDAtHeight(t.Context(), vm1BlkB.Height())
	require.ErrorIs(err, database.ErrNotFound, "Expected unaccepted block not to be indexed by height")

	require.NoError(vm1.SetPreference(t.Context(), vm1BlkB.ID()))

	vm1.eth.APIBackend.SetAllowUnfinalizedQueries(true)

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	b := vm1.blockChain.GetBlockByNumber(blkBHeight)
	require.Equal(blkBHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())

	vm2BlkC, err := vmtest.IssueTxsAndBuild(txs[0:5], vm2)
	require.NoError(err)

	vm1BlkC, err := vm1.ParseBlock(t.Context(), vm2BlkC.Bytes())
	require.NoError(err)

	require.NoError(vm1BlkC.Verify(t.Context()))

	_, err = vm1.GetBlockIDAtHeight(t.Context(), vm1BlkC.Height())
	require.ErrorIs(err, database.ErrNotFound, "Expected unaccepted block not to be indexed by height, but found %s", err)

	require.NoError(vm1BlkC.Accept(t.Context()))

	blkID, err = vm1.GetBlockIDAtHeight(t.Context(), vm1BlkC.Height())
	require.NoError(err)
	require.Equal(blkID, vm1BlkC.ID())

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	b = vm1.blockChain.GetBlockByNumber(blkBHeight)
	require.Equal(blkCHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
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
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testStickyPreference(t, scheme)
		})
	}
}

func testStickyPreference(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.NoUpgrades
	tvmConfig := vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	}
	vm1 := newDefaultTestVM()
	vm2 := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm1, tvmConfig)
	vmtest.SetupTestVM(t, vm2, tvmConfig)

	defer func() {
		require.NoError(vm1.Shutdown(t.Context()))
		require.NoError(vm2.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := vmtest.TestKeys[1].ToECDSA()
	address := vmtest.TestEthAddrs[1]

	signedTx := newSignedLegacyTx(t, vm1.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, big.NewInt(ap0.MinGasPrice), nil)
	vm1BlkA, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm1)
	require.NoError(err)

	vm2BlkA, err := vm2.ParseBlock(t.Context(), vm1BlkA.Bytes())
	require.NoError(err)
	require.NoError(vm2BlkA.Verify(t.Context()))
	require.NoError(vm2.SetPreference(t.Context(), vm2BlkA.ID()))

	require.NoError(vm1BlkA.Accept(t.Context()))
	require.NoError(vm2BlkA.Accept(t.Context()))

	newHead := <-newTxPoolHeadChan1
	require.Equal(common.Hash(vm1BlkA.ID()), newHead.Head.Hash())
	newHead = <-newTxPoolHeadChan2
	require.Equal(common.Hash(vm2BlkA.ID()), newHead.Head.Hash())

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx := newSignedLegacyTx(t, vm1.chainConfig, key, uint64(i), &address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	vm1BlkB, err := vmtest.IssueTxsAndSetPreference(txs, vm1)
	require.NoError(err)

	vm1.eth.APIBackend.SetAllowUnfinalizedQueries(true)

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	b := vm1.blockChain.GetBlockByNumber(blkBHeight)
	require.Equal(blkBHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())

	vm2BlkC, err := vmtest.IssueTxsAndSetPreference(txs[0:5], vm2)
	require.NoError(err)

	newHead = <-newTxPoolHeadChan2
	require.Equal(common.Hash(vm2BlkC.ID()), newHead.Head.Hash())

	vm2BlkD, err := vmtest.IssueTxsAndBuild(txs[5:], vm2)
	require.NoError(err)

	// Parse blocks produced in vm2
	vm1BlkC, err := vm1.ParseBlock(t.Context(), vm2BlkC.Bytes())
	require.NoError(err)
	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()

	vm1BlkD, err := vm1.ParseBlock(t.Context(), vm2BlkD.Bytes())
	require.NoError(err)
	blkDHeight := vm1BlkD.Height()
	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()

	// Should be no-ops
	require.NoError(vm1BlkC.Verify(t.Context()))
	require.NoError(vm1BlkD.Verify(t.Context()))
	b = vm1.blockChain.GetBlockByNumber(blkBHeight)
	require.Equal(blkBHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	b = vm1.blockChain.GetBlockByNumber(blkDHeight)
	require.Nil(b, "expected block at %d to be nil but got %s")
	h := vm1.blockChain.CurrentBlock()
	require.Equal(blkBHash, h.Hash(), "expected current block to have hash %s but got %s", blkBHash.Hex(), h.Hash().Hex())

	// Should still be no-ops on re-verify
	require.NoError(vm1BlkC.Verify(t.Context()))
	require.NoError(vm1BlkD.Verify(t.Context()))
	b = vm1.blockChain.GetBlockByNumber(blkBHeight)
	require.Equal(blkBHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	b = vm1.blockChain.GetBlockByNumber(blkDHeight)
	require.Nil(b, "expected block at %d to be nil but got %s")
	h = vm1.blockChain.CurrentBlock()
	require.Equal(blkBHash, h.Hash(), "expected current block to have hash %s but got %s", blkBHash.Hex(), h.Hash().Hex())

	// Should be queryable after setting preference to side chain
	require.NoError(vm1.SetPreference(t.Context(), vm1BlkD.ID()))

	b = vm1.blockChain.GetBlockByNumber(blkBHeight)
	require.Equal(blkCHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	b = vm1.blockChain.GetBlockByNumber(blkDHeight)
	require.Equal(blkDHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkDHeight, blkDHash.Hex(), b.Hash().Hex())
	h = vm1.blockChain.CurrentBlock()
	require.Equal(blkDHash, h.Hash(), "expected current block to have hash %s but got %s", blkDHash.Hex(), h.Hash().Hex())

	// Attempt to accept out of order
	err = vm1BlkD.Accept(t.Context())
	require.ErrorContains(err, "expected accepted block to have parent") //nolint:forbidigo // uses upstream code

	// Accept in order
	require.NoError(vm1BlkC.Accept(t.Context()))
	require.NoError(vm1BlkD.Accept(t.Context()))

	// Ensure queryable after accepting
	b = vm1.blockChain.GetBlockByNumber(blkBHeight)
	require.Equal(blkCHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	b = vm1.blockChain.GetBlockByNumber(blkDHeight)
	require.Equal(blkDHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkDHeight, blkDHash.Hex(), b.Hash().Hex())
	h = vm1.blockChain.CurrentBlock()
	require.Equal(blkDHash, h.Hash(), "expected current block to have hash %s but got %s", blkDHash.Hex(), h.Hash().Hex())
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
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testUncleBlock(t, scheme)
		})
	}
}

func testUncleBlock(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.NoUpgrades
	tvmConfig := vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	}
	vm1 := newDefaultTestVM()
	vm2 := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm1, tvmConfig)
	vmtest.SetupTestVM(t, vm2, tvmConfig)

	defer func() {
		require.NoError(vm1.Shutdown(t.Context()))
		require.NoError(vm2.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := vmtest.TestKeys[1].ToECDSA()
	address := vmtest.TestEthAddrs[1]

	signedTx := newSignedLegacyTx(t, vm1.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, big.NewInt(ap0.MinGasPrice), nil)
	vm1BlkA, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm1)
	require.NoError(err)

	vm2BlkA, err := vm2.ParseBlock(t.Context(), vm1BlkA.Bytes())
	require.NoError(err)
	require.NoError(vm2BlkA.Verify(t.Context()))
	require.NoError(vm2.SetPreference(t.Context(), vm2BlkA.ID()))

	require.NoError(vm1BlkA.Accept(t.Context()))
	require.NoError(vm2BlkA.Accept(t.Context()))

	newHead := <-newTxPoolHeadChan1
	require.Equal(common.Hash(vm1BlkA.ID()), newHead.Head.Hash())
	newHead = <-newTxPoolHeadChan2
	require.Equal(common.Hash(vm2BlkA.ID()), newHead.Head.Hash())

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx := newSignedLegacyTx(t, vm1.chainConfig, key, uint64(i), &address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		txs[i] = signedTx
	}

	vm1BlkB, err := vmtest.IssueTxsAndSetPreference(txs, vm1)
	require.NoError(err)

	vm2BlkC, err := vmtest.IssueTxsAndSetPreference(txs[0:5], vm2)
	require.NoError(err)

	newHead = <-newTxPoolHeadChan2
	require.Equal(common.Hash(vm2BlkC.ID()), newHead.Head.Hash())

	vm2BlkD, err := vmtest.IssueTxsAndBuild(txs[5:10], vm2)
	require.NoError(err)

	// Create uncle block from blkD
	blkDEthBlock := vm2BlkD.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	uncles := []*types.Header{vm1BlkB.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Header()}
	uncleBlockHeader := types.CopyHeader(blkDEthBlock.Header())
	uncleBlockHeader.UncleHash = types.CalcUncleHash(uncles)

	uncleEthBlock := customtypes.NewBlockWithExtData(
		uncleBlockHeader,
		blkDEthBlock.Transactions(),
		uncles,
		nil,
		trie.NewStackTrie(nil),
		customtypes.BlockExtData(blkDEthBlock),
		false,
	)
	uncleBlock, err := wrapBlock(uncleEthBlock, vm2)
	require.NoError(err)
	err = uncleBlock.Verify(t.Context())
	require.ErrorIs(err, errUnclesUnsupported)
	_, err = vm1.ParseBlock(t.Context(), vm2BlkC.Bytes())
	require.NoError(err)
	_, err = vm1.ParseBlock(t.Context(), uncleBlock.Bytes())
	require.ErrorIs(err, errUnclesUnsupported)
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
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testAcceptReorg(t, scheme)
		})
	}
}

func testAcceptReorg(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.NoUpgrades
	tvmConfig := vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	}
	vm1 := newDefaultTestVM()
	vm2 := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm1, tvmConfig)
	vmtest.SetupTestVM(t, vm2, tvmConfig)

	defer func() {
		require.NoError(vm1.Shutdown(t.Context()))
		require.NoError(vm2.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := vmtest.TestKeys[1].ToECDSA()
	address := vmtest.TestEthAddrs[1]

	signedTx := newSignedLegacyTx(t, vm1.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, big.NewInt(ap0.MinGasPrice), nil)
	vm1BlkA, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm1)
	require.NoError(err)

	vm2BlkA, err := vm2.ParseBlock(t.Context(), vm1BlkA.Bytes())
	require.NoError(err)
	require.NoError(vm2BlkA.Verify(t.Context()))
	require.NoError(vm2.SetPreference(t.Context(), vm2BlkA.ID()))

	require.NoError(vm1BlkA.Accept(t.Context()))
	require.NoError(vm2BlkA.Accept(t.Context()))

	newHead := <-newTxPoolHeadChan1
	require.Equal(common.Hash(vm1BlkA.ID()), newHead.Head.Hash())
	newHead = <-newTxPoolHeadChan2
	require.Equal(common.Hash(vm2BlkA.ID()), newHead.Head.Hash())

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx = newSignedLegacyTx(t, vm1.chainConfig, key, uint64(i), &address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference
	// for block B
	vm1BlkB, err := vmtest.IssueTxsAndSetPreference(txs, vm1)
	require.NoError(err)

	vm2BlkC, err := vmtest.IssueTxsAndSetPreference(txs[0:5], vm2)
	require.NoError(err)

	newHead = <-newTxPoolHeadChan2
	require.Equal(common.Hash(vm2BlkC.ID()), newHead.Head.Hash())

	vm2BlkD, err := vmtest.IssueTxsAndBuild(txs[5:], vm2)
	require.NoError(err)

	// Parse blocks produced in vm2
	vm1BlkC, err := vm1.ParseBlock(t.Context(), vm2BlkC.Bytes())
	require.NoError(err)

	vm1BlkD, err := vm1.ParseBlock(t.Context(), vm2BlkD.Bytes())
	require.NoError(err)

	require.NoError(vm1BlkC.Verify(t.Context()))
	require.NoError(vm1BlkD.Verify(t.Context()))

	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	b := vm1.blockChain.CurrentBlock()
	require.Equal(blkBHash, b.Hash(), "expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())

	require.NoError(vm1BlkC.Accept(t.Context()))

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	b = vm1.blockChain.CurrentBlock()
	require.Equal(blkCHash, b.Hash(), "expected current block to have hash %s but got %s", blkCHash.Hex(), b.Hash().Hex())
	require.NoError(vm1BlkB.Reject(t.Context()))

	require.NoError(vm1BlkD.Accept(t.Context()))

	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	b = vm1.blockChain.CurrentBlock()
	require.Equal(blkDHash, b.Hash(), "expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
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
			require := require.New(t)
			vm := newDefaultTestVM()
			_ = vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork: &test.fork,
			})

			defer func() {
				require.NoError(vm.Shutdown(t.Context()))
			}()

			// Create a block
			signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
			blk, err := vmtest.IssueTxsAndBuild([]*types.Transaction{signedTx}, vm)
			require.NoError(err)

			// Modify the header to have the desired time values
			ethBlk := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
			modifiedHeader := types.CopyHeader(ethBlk.Header())
			modifiedHeader.Time = test.timeSeconds
			modifiedExtra := customtypes.GetHeaderExtra(modifiedHeader)
			modifiedExtra.TimeMilliseconds = test.timeMilliseconds

			// Build new block with modified header
			receipts := vm.blockChain.GetReceiptsByHash(ethBlk.Hash())
			modifiedBlock := customtypes.NewBlockWithExtData(
				modifiedHeader,
				ethBlk.Transactions(),
				nil,
				receipts,
				trie.NewStackTrie(nil),
				customtypes.BlockExtData(ethBlk),
				false,
			)
			modifiedBlk, err := wrapBlock(modifiedBlock, vm)
			require.NoError(err)

			vm.clock.Set(timestamp) // set current time to base for time checks
			err = modifiedBlk.Verify(t.Context())
			require.ErrorIs(err, test.expectedError)
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
			require := require.New(t)
			vm := newDefaultTestVM()
			_ = vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork: &test.fork,
			})

			defer func() {
				require.NoError(vm.Shutdown(t.Context()))
			}()

			vm.clock.Set(buildTime)
			signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
			blk, err := vmtest.IssueTxsAndBuild([]*types.Transaction{signedTx}, vm)
			require.NoError(err)
			ethBlk := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
			require.Equal(test.expectedTimeMilliseconds, customtypes.BlockTimeMilliseconds(ethBlk))
		})
	}
}

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 1 ruleset in genesis.
func TestBuildApricotPhase1Block(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testBuildApricotPhase1Block(t, scheme)
		})
	}
}

func testBuildApricotPhase1Block(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.ApricotPhase1
	vm := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := vmtest.TestKeys[1].ToECDSA()
	address := vmtest.TestEthAddrs[1]

	signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)
	blk, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
	require.NoError(err)

	require.NoError(blk.Accept(t.Context()))
	newHead := <-newTxPoolHeadChan
	require.Equal(common.Hash(blk.ID()), newHead.Head.Hash())

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 5; i++ {
		signedTx := newSignedLegacyTx(t, vm.chainConfig, key, uint64(i), &address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		txs[i] = signedTx
	}
	for i := 5; i < 10; i++ {
		signedTx := newSignedLegacyTx(t, vm.chainConfig, key, uint64(i), &address, big.NewInt(10), 21000, big.NewInt(ap1.MinGasPrice), nil)
		txs[i] = signedTx
	}
	blk, err = vmtest.IssueTxsAndBuild(txs, vm)
	require.NoError(err)

	require.NoError(blk.Accept(t.Context()))

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(blk.ID(), lastAcceptedID)

	// Confirm all txs are present
	ethBlkTxs := vm.blockChain.GetBlockByNumber(2).Transactions()
	for i, tx := range txs {
		require.Greater(len(ethBlkTxs), i, "missing transactions expected: %d but found: %d", len(txs), len(ethBlkTxs))
		require.Equal(ethBlkTxs[i].Hash(), tx.Hash(), "expected tx at index %d to have hash: %x but has: %x", i, txs[i].Hash(), tx.Hash())
	}
}

func TestLastAcceptedBlockNumberAllow(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testLastAcceptedBlockNumberAllow(t, scheme)
		})
	}
}

func testLastAcceptedBlockNumberAllow(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.NoUpgrades
	vm := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, big.NewInt(ap0.MinGasPrice), nil)
	blk, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
	require.NoError(err)

	blkHeight := blk.Height()
	blkHash := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()

	vm.eth.APIBackend.SetAllowUnfinalizedQueries(true)

	ctx := t.Context()
	b, err := vm.eth.APIBackend.BlockByNumber(ctx, rpc.BlockNumber(blkHeight))
	require.NoError(err)
	require.Equal(blkHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())

	vm.eth.APIBackend.SetAllowUnfinalizedQueries(false)

	_, err = vm.eth.APIBackend.BlockByNumber(ctx, rpc.BlockNumber(blkHeight))
	require.ErrorIs(err, eth.ErrUnfinalizedData)

	require.NoError(blk.Accept(t.Context()))

	b = vm.blockChain.GetBlockByNumber(blkHeight)
	require.Equal(blkHash, b.Hash(), "expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())
}

func TestSkipChainConfigCheckCompatible(t *testing.T) {
	require := require.New(t)
	fork := upgradetest.Durango
	vm := newDefaultTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork: &fork,
	})

	// Since rewinding is permitted for last accepted height of 0, we must
	// accept one block to test the SkipUpgradeCheck functionality.
	signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)
	blk, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
	require.NoError(err)
	require.NoError(blk.Accept(t.Context()))

	require.NoError(vm.Shutdown(t.Context()))

	reinitVM := newDefaultTestVM()
	// use the block's timestamp instead of 0 since rewind to genesis
	// is hardcoded to be allowed in core/genesis.go.
	newCTX := snowtest.Context(t, vm.ctx.ChainID)
	upgradetest.SetTimesTo(&newCTX.NetworkUpgrades, upgradetest.Latest, upgrade.UnscheduledActivationTime)
	upgradetest.SetTimesTo(&newCTX.NetworkUpgrades, fork+1, blk.Timestamp())
	upgradetest.SetTimesTo(&newCTX.NetworkUpgrades, fork, upgrade.InitiallyActiveTime)
	genesis := []byte(vmtest.GenesisJSON(paramstest.ForkToChainConfig[fork]))
	err = reinitVM.Initialize(t.Context(), newCTX, tvm.DB, genesis, []byte{}, []byte{}, []*commonEng.Fx{}, tvm.AppSender)
	require.ErrorContains(err, "mismatching Cancun fork timestamp in database") //nolint:forbidigo // uses upstream code

	// try again with skip-upgrade-check
	reinitVM = newDefaultTestVM()
	vmtest.ResetMetrics(newCTX)
	config := []byte(`{"skip-upgrade-check": true}`)
	require.NoError(reinitVM.Initialize(t.Context(), newCTX, tvm.DB, genesis, []byte{}, config, []*commonEng.Fx{}, tvm.AppSender))
	require.NoError(reinitVM.Shutdown(t.Context()))
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
			require := require.New(t)
			fork := test.fork
			vm := newDefaultTestVM()
			vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork: &fork,
			})

			defer func() {
				require.NoError(vm.Shutdown(t.Context()))
			}()

			signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)
			blk, err := vmtest.IssueTxsAndBuild([]*types.Transaction{signedTx}, vm)
			require.NoError(err)

			// Modify the block to have a parent beacon root
			ethBlock := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
			header := types.CopyHeader(ethBlock.Header())
			header.ParentBeaconRoot = test.beaconRoot
			parentBeaconEthBlock := ethBlock.WithSeal(header)

			parentBeaconBlock, err := wrapBlock(parentBeaconEthBlock, vm)
			require.NoError(err)

			_, err = vm.ParseBlock(t.Context(), parentBeaconBlock.Bytes())
			require.ErrorIs(err, test.expectedError)
			err = parentBeaconBlock.Verify(t.Context())
			require.ErrorIs(err, test.expectedError)
		})
	}
}

func TestNoBlobsAllowed(t *testing.T) {
	ctx := t.Context()
	require := require.New(t)

	gspec := new(core.Genesis)
	require.NoError(json.Unmarshal([]byte(genesisJSONCancun), gspec))

	// Make one block with a single blob tx
	signer := types.NewCancunSigner(gspec.Config.ChainID)
	blockGen := func(_ int, b *core.BlockGen) {
		b.SetCoinbase(constants.BlackholeAddr)
		fee := big.NewInt(500)
		fee.Add(fee, b.BaseFee())
		tx, err := types.SignTx(types.NewTx(&types.BlobTx{
			Nonce:      0,
			GasTipCap:  uint256.NewInt(1),
			GasFeeCap:  uint256.MustFromBig(fee),
			Gas:        ethparams.TxGas,
			To:         vmtest.TestEthAddrs[0],
			BlobFeeCap: uint256.NewInt(1),
			BlobHashes: []common.Hash{{1}}, // This blob is expected to cause verification to fail
			Value:      new(uint256.Int),
		}), signer, vmtest.TestKeys[0].ToECDSA())
		require.NoError(err)
		b.AddTx(tx)
	}
	// FullFaker used to skip header verification so we can generate a block with blobs
	_, blocks, _, err := core.GenerateChainWithGenesis(gspec, dummy.NewFullFaker(), 1, 10, blockGen)
	require.NoError(err)

	// Create a VM with the genesis (will use header verification)
	vm := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		GenesisJSON: genesisJSONCancun,
	})
	defer func() { require.NoError(vm.Shutdown(ctx)) }()

	// Verification should fail
	extendedBlock, err := wrapBlock(blocks[0], vm)
	require.NoError(err)
	_, err = vm.ParseBlock(ctx, extendedBlock.Bytes())
	require.ErrorIs(err, errBlobsNotEnabled)
	err = extendedBlock.Verify(ctx)
	require.ErrorIs(err, errBlobsNotEnabled)
}

func TestBuildBlockWithInsufficientCapacity(t *testing.T) {
	ctx := t.Context()
	require := require.New(t)

	fork := upgradetest.Fortuna
	vm := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork: &fork,
	})
	defer func() {
		require.NoError(vm.Shutdown(ctx))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	// Build a block consuming all of the available gas
	var (
		txs = make([]*types.Transaction, 2)
		err error
	)
	for i := uint64(0); i < 2; i++ {
		tx := types.NewContractCreation(
			i,
			big.NewInt(0),
			acp176.MinMaxCapacity,
			big.NewInt(ap0.MinGasPrice),
			[]byte{0xfe}, // invalid opcode consumes all gas
		)
		txs[i], err = types.SignTx(tx, types.LatestSigner(vm.chainConfig), vmtest.TestKeys[0].ToECDSA())
		require.NoError(err)
	}

	blk2, err := vmtest.IssueTxsAndBuild([]*types.Transaction{txs[0]}, vm)
	require.NoError(err)

	require.NoError(blk2.Accept(ctx))
	require.NoError(vm.SetPreference(t.Context(), blk2.ID()))

	// Attempt to build a block consuming more than the current gas capacity
	_, err = vmtest.IssueTxsAndBuild([]*types.Transaction{txs[1]}, vm)
	// Expect block building to fail due to insufficient gas capacity
	require.ErrorIs(err, miner.ErrInsufficientGasCapacityToBuild)

	// Wait to fill block capacity and retry block builiding
	vm.clock.Set(vm.clock.Time().Add(acp176.TimeToFillCapacity * time.Second))

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk3, err := vm.BuildBlock(ctx)
	require.NoError(err)

	require.NoError(blk3.Verify(ctx))
	require.NoError(blk3.Accept(ctx))
	require.NoError(vm.SetPreference(t.Context(), blk3.ID()))
}

func TestBuildBlockLargeTxStarvation(t *testing.T) {
	ctx := t.Context()
	require := require.New(t)

	fork := upgradetest.Fortuna
	amount := new(big.Int).Mul(big.NewInt(ethparams.Ether), big.NewInt(4000))
	genesis := vmtest.NewTestGenesis(paramstest.ForkToChainConfig[fork])
	for _, addr := range vmtest.TestEthAddrs {
		genesis.Alloc[addr] = types.Account{Balance: amount}
	}
	genesisBytes, err := json.Marshal(genesis)
	require.NoError(err)

	vm := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:        &fork,
		GenesisJSON: string(genesisBytes),
	})
	defer func() {
		require.NoError(vm.Shutdown(ctx))
	}()

	// Build a block consuming all of the available gas
	var (
		highGasPrice = big.NewInt(2 * ap0.MinGasPrice)
		lowGasPrice  = big.NewInt(ap0.MinGasPrice)
	)

	// Refill capacity
	vm.clock.Set(vm.clock.Time().Add(acp176.TimeToFillCapacity * time.Second))
	maxSizeTxs := make([]*types.Transaction, 2)
	for i := uint64(0); i < 2; i++ {
		tx := types.NewContractCreation(
			i,
			big.NewInt(0),
			acp176.MinMaxCapacity,
			highGasPrice,
			[]byte{0xfe}, // invalid opcode consumes all gas
		)
		var err error
		maxSizeTxs[i], err = types.SignTx(tx, types.LatestSigner(vm.chainConfig), vmtest.TestKeys[0].ToECDSA())
		require.NoError(err)
	}

	blk2, err := vmtest.IssueTxsAndBuild([]*types.Transaction{maxSizeTxs[0]}, vm)
	require.NoError(err)

	require.NoError(blk2.Accept(ctx))
	require.NoError(vm.SetPreference(t.Context(), blk2.ID()))

	// Add a second transaction trying to consume the max guaranteed gas capacity at a higher gas price
	errs := vm.txPool.AddRemotesSync([]*types.Transaction{maxSizeTxs[1]})
	require.Len(errs, 1)
	require.NoError(errs[0])

	// Build a smaller transaction that consumes less gas at a lower price. Block building should
	// fail and enforce waiting for more capacity to avoid starving the larger transaction.
	tx := types.NewContractCreation(0, big.NewInt(0), 2_000_000, lowGasPrice, []byte{0xfe})
	signedTx, err := types.SignTx(tx, types.LatestSigner(vm.chainConfig), vmtest.TestKeys[1].ToECDSA())
	require.NoError(err)
	_, err = vmtest.IssueTxsAndBuild([]*types.Transaction{signedTx}, vm)
	require.ErrorIs(err, miner.ErrInsufficientGasCapacityToBuild)

	// Wait to fill block capacity and retry block building
	vm.clock.Set(vm.clock.Time().Add(acp176.TimeToFillCapacity * time.Second))

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk4, err := vm.BuildBlock(ctx)
	require.NoError(err)
	ethBlk4 := blk4.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	actualTxs := ethBlk4.Transactions()
	require.Len(actualTxs, 1)
	require.Equal(maxSizeTxs[1].Hash(), actualTxs[0].Hash())

	require.NoError(blk4.Verify(ctx))
	require.NoError(blk4.Accept(ctx))
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

				signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)
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
				signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)

				err := errors.Join(vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})...)
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

				signedTx = newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 1, &vmtest.TestEthAddrs[2], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)

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

				signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)

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
				signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)
				err := errors.Join(vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})...)
				require.NoError(t, err)

				blk, err := vm.BuildBlock(t.Context())
				require.NoError(t, err)

				require.NoError(t, blk.Verify(t.Context()))
				require.NoError(t, vm.SetPreference(t.Context(), blk.ID()))

				signedTx = newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 1, &vmtest.TestEthAddrs[2], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)
				err = errors.Join(vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})...)
				require.NoError(t, err)

				time.Sleep(time.Second * 2)
				blk2, err := vm.BuildBlock(t.Context())
				require.NoError(t, err)

				require.NoError(t, blk2.Verify(t.Context()))

				signedTx = newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 2, &vmtest.TestEthAddrs[2], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)
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
		// TODO (ceyonur): remove this test after Granite is activated. (See https://github.com/ava-labs/avalanchego/graft/coreth/issues/1318)
		{
			name: "WaitForEvent does not wait for new block to be built in fortuna",
			Fork: &fortunaFork,
			testCase: func(t *testing.T, vm *VM) {
				t.Parallel()
				signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)
				blk, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
				require.NoError(t, err)
				require.NoError(t, blk.Accept(t.Context()))
				signedTx = newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 1, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)

				for _, err := range vm.txPool.AddRemotesSync([]*types.Transaction{signedTx}) {
					require.NoError(t, err)
				}

				msg, err := vm.WaitForEvent(t.Context())
				require.NoError(t, err)
				require.Equal(t, commonEng.PendingTxs, msg)
			},
		},
		// TODO (ceyonur): remove this test after Granite is activated. (See https://github.com/ava-labs/avalanchego/graft/coreth/issues/1318)
		{
			name: "WaitForEvent waits for a delay with a retry in fortuna",
			Fork: &fortunaFork,
			testCase: func(t *testing.T, vm *VM) {
				t.Parallel()
				lastBuildBlockTime := time.Now()
				_, err := vm.BuildBlock(t.Context())
				require.NoError(t, err)
				// we haven't accepted the previous built block, so this should be a retry
				signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)
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
			vm := newDefaultTestVM()
			vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork: &fork,
			})
			testCase.testCase(t, vm)
			require.NoError(t, vm.Shutdown(t.Context()))
		})
	}
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
		ctx  = t.Context()
		fork = upgradetest.Latest
		vm   = newDefaultTestVM()
	)
	vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork: &fork,
	})
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
	callerAddr := vmtest.TestEthAddrs[0]
	callerKey := vmtest.TestKeys[0]

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
			deployGasPrice: vmtest.InitialBaseFee,
			txGasPrice:     vmtest.InitialBaseFee,
			// Time is irrelevant as only the fork dictates the logic
			refillCapacityFortuna: false,
			wantIncluded:          true,
			wantReceiptStatus:     types.ReceiptStatusFailed,
		},
		{
			name:                  "fortuna_post_cutoff_should_invalidate",
			fork:                  upgradetest.Fortuna,
			deployGasPrice:        big.NewInt(ap0.MinGasPrice),
			txGasPrice:            big.NewInt(ap0.MinGasPrice),
			setTime:               params.InvalidateDelegateUnix + 1,
			refillCapacityFortuna: true,
			wantIncluded:          false,
		},
		{
			name:                  "fortuna_pre_cutoff_should_succeed",
			fork:                  upgradetest.Fortuna,
			deployGasPrice:        big.NewInt(ap0.MinGasPrice),
			txGasPrice:            big.NewInt(ap0.MinGasPrice),
			preDeployTime:         params.InvalidateDelegateUnix - acp176.TimeToFillCapacity - 1,
			refillCapacityFortuna: true,
			wantIncluded:          true,
			wantReceiptStatus:     types.ReceiptStatusSuccessful,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := newDefaultTestVM()
			vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork: &tt.fork,
			})
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
			nonce := vm.txPool.Nonce(vmtest.TestEthAddrs[0])
			signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), nonce, &contractAddr, big.NewInt(0), 100000, tt.txGasPrice, data)

			blk, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
			require.NoError(t, err)
			require.NoError(t, blk.Accept(ctx))

			ethBlock := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock

			if !tt.wantIncluded {
				require.Empty(t, ethBlock.Transactions())
				return
			}

			require.Len(t, ethBlock.Transactions(), 1)
			receipts := vm.blockChain.GetReceiptsByHash(ethBlock.Hash())
			require.Len(t, receipts, 1)
			require.Equal(t, tt.wantReceiptStatus, receipts[0].Status)
		})
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
				To:         &vmtest.TestEthAddrs[0],
				Gas:        acp176.MinMaxCapacity,
				GasFeeCap:  big.NewInt(10),
				GasTipCap:  big.NewInt(10),
				Value:      common.Big0,
				AccessList: accessList,
			}),
			types.LatestSigner(vm.chainConfig),
			vmtest.TestKeys[0].ToECDSA(),
		)
		require.NoError(err)

		ethBlock := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
		modifiedHeader := types.CopyHeader(ethBlock.Header())

		// Set the gasUsed after calculating the extra prefix to support large
		// claimed gas used values.
		modifiedHeader.GasUsed = claimedGasUsed
		return customtypes.NewBlockWithExtData(
			modifiedHeader,
			[]*types.Transaction{tx},
			nil,
			nil,
			trie.NewStackTrie(nil),
			nil,
			true,
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

			vm := newDefaultTestVM()
			vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{})
			defer func() {
				require.NoError(vm.Shutdown(ctx))
			}()

			blk := newBlock(t, vm, test.gasUsed)

			modifiedBlk, err := wrapBlock(blk, vm)
			require.NoError(err)

			err = modifiedBlk.Verify(ctx)
			require.ErrorIs(err, test.want)
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
			vm := newDefaultTestVM()
			vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork:       &test.fork,
				ConfigJSON: configJSON,
			})

			defer func() {
				require.NoError(vm.Shutdown(ctx))
			}()

			// Build a block
			signedTx := newSignedLegacyTx(t, vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, &vmtest.TestEthAddrs[1], big.NewInt(1), 21000, vmtest.InitialBaseFee, nil)
			blk, err := vmtest.IssueTxsAndBuild([]*types.Transaction{signedTx}, vm)
			require.NoError(err)

			// Check the min delay excess in the header
			ethBlock := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
			headerExtra := customtypes.GetHeaderExtra(ethBlock.Header())

			require.Equal(test.expectedMinDelayExcess, headerExtra.MinDelayExcess, "expected %s, got %s", test.expectedMinDelayExcess, headerExtra.MinDelayExcess)
		})
	}
}

func TestInspectDatabases(t *testing.T) {
	var (
		vm = newDefaultTestVM()
		db = memdb.New()
	)

	vm.initializeDBs(db)
	require.NoError(t, vm.inspectDatabases())
}
