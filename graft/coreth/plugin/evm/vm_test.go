// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/miner"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/paramstest"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ava-labs/coreth/utils"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	ethparams "github.com/ava-labs/libevm/params"
)

var (
	genesisJSONCancun = vmtest.GenesisJSON(activateCancun(params.TestChainConfig))

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
	require.NoError(t, vm.Shutdown(context.Background()))

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
				require.NoError(vm.Shutdown(context.Background()))
			}()

			require.Equal(test.expectedGasPrice, vm.txPool.GasTip())

			// Verify that the genesis is correctly managed.
			lastAcceptedID, err := vm.LastAccepted(context.Background())
			require.NoError(err)
			require.Equal(ids.ID(vm.genesisHash), lastAcceptedID)

			genesisBlk, err := vm.GetBlock(context.Background(), lastAcceptedID)
			require.NoError(err)
			require.Zero(genesisBlk.Height())

			_, err = vm.ParseBlock(context.Background(), genesisBlk.Bytes())
			require.NoError(err)
		})
	}
}

func TestBuildEthTxBlock(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testBuildEthTxBlock(t, scheme)
		})
	}
}

func testBuildEthTxBlock(t *testing.T, scheme string) {
	fork := upgradetest.ApricotPhase2
	vm := newDefaultTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	signedTx, err := newSignedLegacyTx(vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), vmtest.InitialBaseFee)
	if err != nil {
		t.Fatal(err)
	}
	blk1, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
	if err != nil {
		t.Fatalf("Failed to issue txs and build block: %s", err)
	}

	if err := blk1.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk1.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx, err := newSignedLegacyTx(vm.chainConfig, vmtest.TestKeys[1].ToECDSA(), uint64(i), vmtest.TestEthAddrs[0], big.NewInt(10), big.NewInt(ap0.MinGasPrice))
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	blk2, err := vmtest.IssueTxsAndSetPreference(txs, vm)
	if err != nil {
		t.Fatalf("Failed to issue txs and build block: %s", err)
	}

	if err := blk2.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk2.ID()) {
		t.Fatalf("Expected new block to match")
	}

	lastAcceptedID, err := vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk2.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)
	}

	ethBlk1 := blk1.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	if ethBlk1Root := ethBlk1.Root(); !vm.blockChain.HasState(ethBlk1Root) {
		t.Fatalf("Expected blk1 state root to not yet be pruned after blk2 was accepted because of tip buffer")
	}

	// Clear the cache and ensure that GetBlock returns internal blocks with the correct status
	vm.State.Flush()
	blk2Refreshed, err := vm.GetBlockInternal(context.Background(), blk2.ID())
	if err != nil {
		t.Fatal(err)
	}

	blk1RefreshedID := blk2Refreshed.Parent()
	blk1Refreshed, err := vm.GetBlockInternal(context.Background(), blk1RefreshedID)
	if err != nil {
		t.Fatal(err)
	}

	if blk1Refreshed.ID() != blk1.ID() {
		t.Fatalf("Found unexpected blkID for parent of blk2")
	}

	// Close the vm and all databases
	if err := vm.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	restartedVM := newDefaultTestVM()
	newCTX := snowtest.Context(t, snowtest.CChainID)
	newCTX.NetworkUpgrades = upgradetest.GetConfig(fork)
	newCTX.ChainDataDir = tvm.Ctx.ChainDataDir
	conf, err := vmtest.OverrideSchemeConfig(scheme, "")
	require.NoError(t, err)
	if err := restartedVM.Initialize(
		context.Background(),
		newCTX,
		tvm.DB,
		[]byte(vmtest.GenesisJSON(paramstest.ForkToChainConfig[fork])),
		[]byte(""),
		[]byte(conf),
		[]*commonEng.Fx{},
		nil,
	); err != nil {
		t.Fatal(err)
	}

	// State root should not have been committed and discarded on restart
	if ethBlk1Root := ethBlk1.Root(); restartedVM.Ethereum().BlockChain().HasState(ethBlk1Root) {
		t.Fatalf("Expected blk1 state root to be pruned after blk2 was accepted on top of it in pruning mode")
	}

	// State root should be committed when accepted tip on shutdown
	ethBlk2 := blk2.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	if ethBlk2Root := ethBlk2.Root(); !restartedVM.Ethereum().BlockChain().HasState(ethBlk2Root) {
		t.Fatalf("Expected blk2 state root to not be pruned after shutdown (last accepted tip should be committed)")
	}

	// Shutdown the newest VM
	if err := restartedVM.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
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
		if err := vm1.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	signedTx, err := newSignedLegacyTx(vm1.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), big.NewInt(ap0.MinGasPrice))
	if err != nil {
		t.Fatal(err)
	}
	vm1BlkA, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm1)
	if err != nil {
		t.Fatalf("Failed to build block with transaction: %s", err)
	}

	vm2BlkA, err := vm2.ParseBlock(context.Background(), vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if err := vm2.SetPreference(context.Background(), vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if err := vm2BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM2 failed to accept block: %s", err)
	}

	newHead := <-newTxPoolHeadChan1
	if newHead.Head.Hash() != common.Hash(vm1BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}
	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx, err := newSignedLegacyTx(vm1.chainConfig, vmtest.TestKeys[1].ToECDSA(), uint64(i), vmtest.TestEthAddrs[1], big.NewInt(10), big.NewInt(ap0.MinGasPrice))
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	_, err = vmtest.IssueTxsAndSetPreference(txs, vm1)
	if err != nil {
		t.Fatal(err)
	}

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	vm2BlkC, err := vmtest.IssueTxsAndSetPreference(txs[0:5], vm2)
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Block D
	vm2BlkD, err := vmtest.IssueTxsAndSetPreference(txs[5:10], vm2)
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	// VM1 receives blkC and blkD from VM1
	// and happens to call SetPreference on blkD without ever calling SetPreference
	// on blkC
	// Here we parse them in reverse order to simulate receiving a chain from the tip
	// back to the last accepted block as would typically be the case in the consensus
	// engine
	vm1BlkD, err := vm1.ParseBlock(context.Background(), vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("VM1 errored parsing blkD: %s", err)
	}
	vm1BlkC, err := vm1.ParseBlock(context.Background(), vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("VM1 errored parsing blkC: %s", err)
	}

	// The blocks must be verified in order. This invariant is maintained
	// in the consensus engine.
	if err := vm1BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("VM1 BlkC failed verification: %s", err)
	}
	if err := vm1BlkD.Verify(context.Background()); err != nil {
		t.Fatalf("VM1 BlkD failed verification: %s", err)
	}

	// Set VM1's preference to blockD, skipping blockC
	if err := vm1.SetPreference(context.Background(), vm1BlkD.ID()); err != nil {
		t.Fatal(err)
	}

	// Accept the longer chain on both VMs and ensure there are no errors
	// VM1 Accepts the blocks in order
	if err := vm1BlkC.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 BlkC failed on accept: %s", err)
	}
	if err := vm1BlkD.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 BlkC failed on accept: %s", err)
	}

	// VM2 Accepts the blocks in order
	if err := vm2BlkC.Accept(context.Background()); err != nil {
		t.Fatalf("VM2 BlkC failed on accept: %s", err)
	}
	if err := vm2BlkD.Accept(context.Background()); err != nil {
		t.Fatalf("VM2 BlkC failed on accept: %s", err)
	}

	log.Info("Validating canonical chain")
	// Verify the Canonical Chain for Both VMs
	if err := vm2.blockChain.ValidateCanonicalChain(); err != nil {
		t.Fatalf("VM2 failed canonical chain verification due to: %s", err)
	}

	if err := vm1.blockChain.ValidateCanonicalChain(); err != nil {
		t.Fatalf("VM1 failed canonical chain verification due to: %s", err)
	}
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
		if err := vm1.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := vmtest.TestKeys[1].ToECDSA()
	address := vmtest.TestEthAddrs[1]

	signedTx, err := newSignedLegacyTx(vm1.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), big.NewInt(ap0.MinGasPrice))
	if err != nil {
		t.Fatal(err)
	}
	vm1BlkA, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm1)
	if err != nil {
		t.Fatalf("Failed to build block with transaction: %s", err)
	}

	if err := vm1BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if err := vm1.SetPreference(context.Background(), vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := vm2.ParseBlock(context.Background(), vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if err := vm2.SetPreference(context.Background(), vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if err := vm2BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM2 failed to accept block: %s", err)
	}

	newHead := <-newTxPoolHeadChan1
	if newHead.Head.Hash() != common.Hash(vm1BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}
	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx, err := newSignedLegacyTx(vm1.chainConfig, key, uint64(i), address, big.NewInt(10), big.NewInt(ap0.MinGasPrice))
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	vm1BlkB, err := vmtest.IssueTxsAndSetPreference(txs, vm1)
	if err != nil {
		t.Fatal(err)
	}

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	vm2BlkC, err := vmtest.IssueTxsAndBuild(txs[0:5], vm2)
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	vm1BlkC, err := vm1.ParseBlock(context.Background(), vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	if err := vm1BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	// Accept B, such that block C should get Rejected.
	if err := vm1BlkB.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}

	// The below (setting preference blocks that have a common ancestor
	// with the preferred chain lower than the last finalized block)
	// should NEVER happen. However, the VM defends against this
	// just in case.
	if err := vm1.SetPreference(context.Background(), vm1BlkC.ID()); !strings.Contains(err.Error(), "cannot orphan finalized block") {
		t.Fatalf("Unexpected error when setting preference that would trigger reorg: %s", err)
	}

	if err := vm1BlkC.Accept(context.Background()); !strings.Contains(err.Error(), "expected accepted block to have parent") {
		t.Fatalf("Unexpected error when setting block at finalized height: %s", err)
	}
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
		if err := vm1.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := vmtest.TestKeys[1].ToECDSA()
	address := vmtest.TestEthAddrs[1]

	signedTx, err := newSignedLegacyTx(vm1.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), big.NewInt(ap0.MinGasPrice))
	if err != nil {
		t.Fatal(err)
	}

	vm1BlkA, err := vmtest.IssueTxsAndBuild([]*types.Transaction{signedTx}, vm1)
	if err != nil {
		t.Fatalf("Failed to build block with transaction: %s", err)
	}

	if _, err := vm1.GetBlockIDAtHeight(context.Background(), vm1BlkA.Height()); err != database.ErrNotFound {
		t.Fatalf("Expected unaccepted block not to be indexed by height, but found %s", err)
	}

	if err := vm1.SetPreference(context.Background(), vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := vm2.ParseBlock(context.Background(), vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if _, err := vm2.GetBlockIDAtHeight(context.Background(), vm2BlkA.Height()); err != database.ErrNotFound {
		t.Fatalf("Expected unaccepted block not to be indexed by height, but found %s", err)
	}
	if err := vm2.SetPreference(context.Background(), vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if blkID, err := vm1.GetBlockIDAtHeight(context.Background(), vm1BlkA.Height()); err != nil {
		t.Fatalf("Height lookuped failed on accepted block: %s", err)
	} else if blkID != vm1BlkA.ID() {
		t.Fatalf("Expected accepted block to be indexed by height, but found %s", blkID)
	}
	if err := vm2BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM2 failed to accept block: %s", err)
	}
	if blkID, err := vm2.GetBlockIDAtHeight(context.Background(), vm2BlkA.Height()); err != nil {
		t.Fatalf("Height lookuped failed on accepted block: %s", err)
	} else if blkID != vm2BlkA.ID() {
		t.Fatalf("Expected accepted block to be indexed by height, but found %s", blkID)
	}

	newHead := <-newTxPoolHeadChan1
	if newHead.Head.Hash() != common.Hash(vm1BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}
	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx, err := newSignedLegacyTx(vm1.chainConfig, key, uint64(i), address, big.NewInt(10), big.NewInt(ap0.MinGasPrice))
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	vm1BlkB, err := vmtest.IssueTxsAndBuild(txs, vm1)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := vm1.GetBlockIDAtHeight(context.Background(), vm1BlkB.Height()); err != database.ErrNotFound {
		t.Fatalf("Expected unaccepted block not to be indexed by height, but found %s", err)
	}

	if err := vm1.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	vm1.eth.APIBackend.SetAllowUnfinalizedQueries(true)

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	if b := vm1.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}

	vm2BlkC, err := vmtest.IssueTxsAndBuild(txs[0:5], vm2)
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	vm1BlkC, err := vm1.ParseBlock(context.Background(), vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	if err := vm1BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if _, err := vm1.GetBlockIDAtHeight(context.Background(), vm1BlkC.Height()); err != database.ErrNotFound {
		t.Fatalf("Expected unaccepted block not to be indexed by height, but found %s", err)
	}

	if err := vm1BlkC.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}

	if blkID, err := vm1.GetBlockIDAtHeight(context.Background(), vm1BlkC.Height()); err != nil {
		t.Fatalf("Height lookuped failed on accepted block: %s", err)
	} else if blkID != vm1BlkC.ID() {
		t.Fatalf("Expected accepted block to be indexed by height, but found %s", blkID)
	}

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	if b := vm1.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkCHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	}
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
		if err := vm1.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := vmtest.TestKeys[1].ToECDSA()
	address := vmtest.TestEthAddrs[1]

	signedTx, err := newSignedLegacyTx(vm1.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), big.NewInt(ap0.MinGasPrice))
	if err != nil {
		t.Fatal(err)
	}

	vm1BlkA, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm1)
	if err != nil {
		t.Fatalf("Failed to build block with transaction: %s", err)
	}

	vm2BlkA, err := vm2.ParseBlock(context.Background(), vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if err := vm2.SetPreference(context.Background(), vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if err := vm2BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM2 failed to accept block: %s", err)
	}

	newHead := <-newTxPoolHeadChan1
	if newHead.Head.Hash() != common.Hash(vm1BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}
	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx, err := newSignedLegacyTx(vm1.chainConfig, key, uint64(i), address, big.NewInt(10), big.NewInt(ap0.MinGasPrice))
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	vm1BlkB, err := vmtest.IssueTxsAndSetPreference(txs, vm1)
	if err != nil {
		t.Fatal(err)
	}

	vm1.eth.APIBackend.SetAllowUnfinalizedQueries(true)

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	if b := vm1.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}

	vm2BlkC, err := vmtest.IssueTxsAndSetPreference(txs[0:5], vm2)
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	vm2BlkD, err := vmtest.IssueTxsAndBuild(txs[5:], vm2)
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	// Parse blocks produced in vm2
	vm1BlkC, err := vm1.ParseBlock(context.Background(), vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()

	vm1BlkD, err := vm1.ParseBlock(context.Background(), vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	blkDHeight := vm1BlkD.Height()
	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()

	// Should be no-ops
	if err := vm1BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if b := vm1.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.blockChain.GetBlockByNumber(blkDHeight); b != nil {
		t.Fatalf("expected block at %d to be nil but got %s", blkDHeight, b.Hash().Hex())
	}
	if b := vm1.blockChain.CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	// Should still be no-ops on re-verify
	if err := vm1BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if b := vm1.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.blockChain.GetBlockByNumber(blkDHeight); b != nil {
		t.Fatalf("expected block at %d to be nil but got %s", blkDHeight, b.Hash().Hex())
	}
	if b := vm1.blockChain.CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	// Should be queryable after setting preference to side chain
	if err := vm1.SetPreference(context.Background(), vm1BlkD.ID()); err != nil {
		t.Fatal(err)
	}

	if b := vm1.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkCHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.blockChain.GetBlockByNumber(blkDHeight); b.Hash() != blkDHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkDHeight, blkDHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.blockChain.CurrentBlock(); b.Hash() != blkDHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
	}

	// Attempt to accept out of order
	err = vm1BlkD.Accept(context.Background())
	require.ErrorContains(t, err, "expected accepted block to have parent")

	// Accept in order
	if err := vm1BlkC.Accept(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Accept(context.Background()); err != nil {
		t.Fatalf("Block failed acceptance on VM1: %s", err)
	}

	// Ensure queryable after accepting
	if b := vm1.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkCHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.blockChain.GetBlockByNumber(blkDHeight); b.Hash() != blkDHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkDHeight, blkDHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.blockChain.CurrentBlock(); b.Hash() != blkDHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
	}
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
		if err := vm1.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := vmtest.TestKeys[1].ToECDSA()
	address := vmtest.TestEthAddrs[1]

	signedTx, err := newSignedLegacyTx(vm1.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), big.NewInt(ap0.MinGasPrice))
	if err != nil {
		t.Fatal(err)
	}
	vm1BlkA, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm1)
	if err != nil {
		t.Fatalf("Failed to build block with transaction: %s", err)
	}

	vm2BlkA, err := vm2.ParseBlock(context.Background(), vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if err := vm2.SetPreference(context.Background(), vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if err := vm2BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM2 failed to accept block: %s", err)
	}

	newHead := <-newTxPoolHeadChan1
	if newHead.Head.Hash() != common.Hash(vm1BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}
	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx, err := newSignedLegacyTx(vm1.chainConfig, key, uint64(i), address, big.NewInt(10), big.NewInt(ap0.MinGasPrice))
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	vm1BlkB, err := vmtest.IssueTxsAndSetPreference(txs, vm1)
	if err != nil {
		t.Fatal(err)
	}

	vm2BlkC, err := vmtest.IssueTxsAndSetPreference(txs[0:5], vm2)
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	vm2BlkD, err := vmtest.IssueTxsAndBuild(txs[5:10], vm2)
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

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
	require.NoError(t, err)
	err = uncleBlock.Verify(context.Background())
	require.ErrorIs(t, err, errUnclesUnsupported)
	if _, err := vm1.ParseBlock(context.Background(), vm2BlkC.Bytes()); err != nil {
		t.Fatalf("VM1 errored parsing blkC: %s", err)
	}
	_, err = vm1.ParseBlock(context.Background(), uncleBlock.Bytes())
	require.ErrorIs(t, err, errUnclesUnsupported)
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
		if err := vm1.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := vmtest.TestKeys[1].ToECDSA()
	address := vmtest.TestEthAddrs[1]

	signedTx, err := newSignedLegacyTx(vm1.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), big.NewInt(ap0.MinGasPrice))
	if err != nil {
		t.Fatal(err)
	}
	vm1BlkA, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm1)
	if err != nil {
		t.Fatalf("Failed to build block with transaction: %s", err)
	}

	vm2BlkA, err := vm2.ParseBlock(context.Background(), vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if err := vm2.SetPreference(context.Background(), vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if err := vm2BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM2 failed to accept block: %s", err)
	}

	newHead := <-newTxPoolHeadChan1
	if newHead.Head.Hash() != common.Hash(vm1BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}
	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		signedTx, err := newSignedLegacyTx(vm1.chainConfig, key, uint64(i), address, big.NewInt(10), big.NewInt(ap0.MinGasPrice))
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference
	// for block B
	vm1BlkB, err := vmtest.IssueTxsAndSetPreference(txs, vm1)
	if err != nil {
		t.Fatal(err)
	}

	vm2BlkC, err := vmtest.IssueTxsAndSetPreference(txs[0:5], vm2)
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	vm2BlkD, err := vmtest.IssueTxsAndBuild(txs[5:], vm2)
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	// Parse blocks produced in vm2
	vm1BlkC, err := vm1.ParseBlock(context.Background(), vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	vm1BlkD, err := vm1.ParseBlock(context.Background(), vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	if err := vm1BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	if b := vm1.blockChain.CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	if err := vm1BlkC.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	if b := vm1.blockChain.CurrentBlock(); b.Hash() != blkCHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkCHash.Hex(), b.Hash().Hex())
	}
	if err := vm1BlkB.Reject(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkD.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()
	if b := vm1.blockChain.CurrentBlock(); b.Hash() != blkDHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
	}
}

func TestFutureBlock(t *testing.T) {
	for _, scheme := range vmtest.Schemes {
		t.Run(scheme, func(t *testing.T) {
			testFutureBlock(t, scheme)
		})
	}
}

func testFutureBlock(t *testing.T, scheme string) {
	fork := upgradetest.NoUpgrades
	vm := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	signedTx, err := newSignedLegacyTx(vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), big.NewInt(ap0.MinGasPrice))
	if err != nil {
		t.Fatal(err)
	}
	blkA, err := vmtest.IssueTxsAndBuild([]*types.Transaction{signedTx}, vm)
	if err != nil {
		t.Fatalf("Failed to build block with transaction: %s", err)
	}

	// Create empty block from blkA
	internalBlkA := blkA.(*chain.BlockWrapper).Block.(*wrappedBlock)
	modifiedHeader := types.CopyHeader(internalBlkA.ethBlock.Header())
	// Set the VM's clock to the time of the produced block
	vm.clock.Set(time.Unix(int64(modifiedHeader.Time), 0))
	// Set the modified time to exceed the allowed future time
	modifiedTime := modifiedHeader.Time + uint64(maxFutureBlockTime.Seconds()+1)
	modifiedHeader.Time = modifiedTime
	modifiedBlock := customtypes.NewBlockWithExtData(
		modifiedHeader,
		nil,
		nil,
		nil,
		new(trie.Trie),
		customtypes.BlockExtData(internalBlkA.ethBlock),
		false,
	)

	futureBlock, err := wrapBlock(modifiedBlock, vm)
	require.NoError(t, err)
	if err := futureBlock.Verify(context.Background()); err == nil {
		t.Fatal("Future block should have failed verification due to block timestamp too far in the future")
	} else if !strings.Contains(err.Error(), "block timestamp is too far in the future") {
		t.Fatalf("Expected error to be block timestamp too far in the future but found %s", err)
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
	fork := upgradetest.ApricotPhase1
	vm := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := vmtest.TestKeys[1].ToECDSA()
	address := vmtest.TestEthAddrs[1]

	signedTx, err := newSignedLegacyTx(vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), vmtest.InitialBaseFee)
	if err != nil {
		t.Fatal(err)
	}
	blk, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 5; i++ {
		signedTx, err := newSignedLegacyTx(vm.chainConfig, key, uint64(i), address, big.NewInt(10), big.NewInt(ap0.MinGasPrice))
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	for i := 5; i < 10; i++ {
		signedTx, err := newSignedLegacyTx(vm.chainConfig, key, uint64(i), address, big.NewInt(10), big.NewInt(ap1.MinGasPrice))
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	blk, err = vmtest.IssueTxsAndBuild(txs, vm)
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	lastAcceptedID, err := vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}

	// Confirm all txs are present
	ethBlkTxs := vm.blockChain.GetBlockByNumber(2).Transactions()
	for i, tx := range txs {
		if len(ethBlkTxs) <= i {
			t.Fatalf("missing transactions expected: %d but found: %d", len(txs), len(ethBlkTxs))
		}
		if ethBlkTxs[i].Hash() != tx.Hash() {
			t.Fatalf("expected tx at index %d to have hash: %x but has: %x", i, txs[i].Hash(), tx.Hash())
		}
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
	fork := upgradetest.NoUpgrades
	vm := newDefaultTestVM()
	vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:   &fork,
		Scheme: scheme,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	signedTx, err := newSignedLegacyTx(vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), big.NewInt(ap0.MinGasPrice))
	if err != nil {
		t.Fatal(err)
	}
	blk, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
	if err != nil {
		t.Fatalf("Failed to build block with transaction: %s", err)
	}

	blkHeight := blk.Height()
	blkHash := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock.Hash()

	vm.eth.APIBackend.SetAllowUnfinalizedQueries(true)

	ctx := context.Background()
	b, err := vm.eth.APIBackend.BlockByNumber(ctx, rpc.BlockNumber(blkHeight))
	if err != nil {
		t.Fatal(err)
	}
	if b.Hash() != blkHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())
	}

	vm.eth.APIBackend.SetAllowUnfinalizedQueries(false)

	_, err = vm.eth.APIBackend.BlockByNumber(ctx, rpc.BlockNumber(blkHeight))
	require.ErrorIs(t, err, eth.ErrUnfinalizedData)

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatalf("VM failed to accept block: %s", err)
	}

	if b := vm.blockChain.GetBlockByNumber(blkHeight); b.Hash() != blkHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())
	}
}

func TestSkipChainConfigCheckCompatible(t *testing.T) {
	fork := upgradetest.Durango
	vm := newDefaultTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork: &fork,
	})

	// Since rewinding is permitted for last accepted height of 0, we must
	// accept one block to test the SkipUpgradeCheck functionality.
	signedTx, err := newSignedLegacyTx(vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), vmtest.InitialBaseFee)
	if err != nil {
		t.Fatal(err)
	}
	blk, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
	require.NoError(t, err)
	require.NoError(t, blk.Accept(context.Background()))

	require.NoError(t, vm.Shutdown(context.Background()))

	reinitVM := newDefaultTestVM()
	// use the block's timestamp instead of 0 since rewind to genesis
	// is hardcoded to be allowed in core/genesis.go.
	newCTX := snowtest.Context(t, vm.ctx.ChainID)
	upgradetest.SetTimesTo(&newCTX.NetworkUpgrades, upgradetest.Latest, upgrade.UnscheduledActivationTime)
	upgradetest.SetTimesTo(&newCTX.NetworkUpgrades, fork+1, blk.Timestamp())
	upgradetest.SetTimesTo(&newCTX.NetworkUpgrades, fork, upgrade.InitiallyActiveTime)
	genesis := []byte(vmtest.GenesisJSON(paramstest.ForkToChainConfig[fork]))
	err = reinitVM.Initialize(context.Background(), newCTX, tvm.DB, genesis, []byte{}, []byte{}, []*commonEng.Fx{}, tvm.AppSender)
	require.ErrorContains(t, err, "mismatching Cancun fork timestamp in database")

	// try again with skip-upgrade-check
	reinitVM = newDefaultTestVM()
	vmtest.ResetMetrics(newCTX)
	config := []byte(`{"skip-upgrade-check": true}`)
	require.NoError(t, reinitVM.Initialize(context.Background(), newCTX, tvm.DB, genesis, []byte{}, config, []*commonEng.Fx{}, tvm.AppSender))
	require.NoError(t, reinitVM.Shutdown(context.Background()))
}

func TestParentBeaconRootBlock(t *testing.T) {
	tests := []struct {
		name          string
		fork          upgradetest.Fork
		beaconRoot    *common.Hash
		expectedError bool
		errString     string
	}{
		{
			name:          "non-empty parent beacon root in Durango",
			fork:          upgradetest.Durango,
			beaconRoot:    &common.Hash{0x01},
			expectedError: true,
			// err string wont work because it will also fail with blob gas is non-empty (zeroed)
		},
		{
			name:          "empty parent beacon root in Durango",
			fork:          upgradetest.Durango,
			beaconRoot:    &common.Hash{},
			expectedError: true,
		},
		{
			name:          "nil parent beacon root in Durango",
			fork:          upgradetest.Durango,
			beaconRoot:    nil,
			expectedError: false,
		},
		{
			name:          "non-empty parent beacon root in E-Upgrade (Cancun)",
			fork:          upgradetest.Etna,
			beaconRoot:    &common.Hash{0x01},
			expectedError: true,
			errString:     "expected empty hash",
		},
		{
			name:          "empty parent beacon root in E-Upgrade (Cancun)",
			fork:          upgradetest.Etna,
			beaconRoot:    &common.Hash{},
			expectedError: false,
		},
		{
			name:          "nil parent beacon root in E-Upgrade (Cancun)",
			fork:          upgradetest.Etna,
			beaconRoot:    nil,
			expectedError: true,
			errString:     "header is missing parentBeaconRoot",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fork := test.fork
			vm := newDefaultTestVM()
			vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork: &fork,
			})

			defer func() {
				if err := vm.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
			}()

			signedTx, err := newSignedLegacyTx(vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), vmtest.InitialBaseFee)
			if err != nil {
				t.Fatal(err)
			}
			blk, err := vmtest.IssueTxsAndBuild([]*types.Transaction{signedTx}, vm)
			if err != nil {
				t.Fatalf("Failed to build block with transaction: %s", err)
			}

			// Modify the block to have a parent beacon root
			ethBlock := blk.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
			header := types.CopyHeader(ethBlock.Header())
			header.ParentBeaconRoot = test.beaconRoot
			parentBeaconEthBlock := ethBlock.WithSeal(header)

			parentBeaconBlock, err := wrapBlock(parentBeaconEthBlock, vm)
			require.NoError(t, err)

			errCheck := func(err error) {
				if test.expectedError {
					if test.errString != "" {
						require.ErrorContains(t, err, test.errString)
					} else {
						require.Error(t, err)
					}
				} else {
					require.NoError(t, err)
				}
			}

			_, err = vm.ParseBlock(context.Background(), parentBeaconBlock.Bytes())
			errCheck(err)
			err = parentBeaconBlock.Verify(context.Background())
			errCheck(err)
		})
	}
}

func TestNoBlobsAllowed(t *testing.T) {
	ctx := context.Background()
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
	ctx := context.Background()
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

	// Attempt to build a block consuming more than the current gas capacity
	_, err = vmtest.IssueTxsAndBuild([]*types.Transaction{txs[1]}, vm)
	// Expect block building to fail due to insufficient gas capacity
	require.ErrorIs(err, miner.ErrInsufficientGasCapacityToBuild)

	// Wait to fill block capacity and retry block builiding
	vm.clock.Set(vm.clock.Time().Add(acp176.TimeToFillCapacity * time.Second))

	msg, err := vm.WaitForEvent(context.Background())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk3, err := vm.BuildBlock(ctx)
	require.NoError(err)

	require.NoError(blk3.Verify(ctx))
	require.NoError(blk3.Accept(ctx))
}

func TestBuildBlockLargeTxStarvation(t *testing.T) {
	ctx := context.Background()
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

	msg, err := vm.WaitForEvent(context.Background())
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
	for _, testCase := range []struct {
		name     string
		testCase func(*testing.T, *VM)
	}{
		{
			name: "WaitForEvent with context cancelled returns 0",
			testCase: func(t *testing.T, vm *VM) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
				defer cancel()

				var wg sync.WaitGroup
				wg.Add(1)

				// We run WaitForEvent in a goroutine to ensure it can be safely called concurrently.
				go func() {
					defer wg.Done()
					msg, err := vm.WaitForEvent(ctx)
					require.ErrorIs(t, err, context.DeadlineExceeded)
					require.Zero(t, msg)
				}()

				wg.Wait()
			},
		},
		{
			name: "WaitForEvent returns when a transaction is added to the mempool",
			testCase: func(t *testing.T, vm *VM) {
				var wg sync.WaitGroup
				wg.Add(1)

				go func() {
					defer wg.Done()
					msg, err := vm.WaitForEvent(context.Background())
					require.NoError(t, err)
					require.Equal(t, commonEng.PendingTxs, msg)
				}()

				signedTx, err := newSignedLegacyTx(vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), vmtest.InitialBaseFee)
				require.NoError(t, err)

				for _, err := range vm.txPool.AddRemotesSync([]*types.Transaction{signedTx}) {
					require.NoError(t, err)
				}

				wg.Wait()
			},
		},
		{
			name: "WaitForEvent doesn't return once a block is built and accepted",
			testCase: func(t *testing.T, vm *VM) {
				signedTx, err := newSignedLegacyTx(vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), vmtest.InitialBaseFee)
				require.NoError(t, err)

				for _, err := range vm.txPool.AddRemotesSync([]*types.Transaction{signedTx}) {
					require.NoError(t, err)
				}

				blk, err := vm.BuildBlock(context.Background())
				require.NoError(t, err)

				require.NoError(t, blk.Verify(context.Background()))

				require.NoError(t, vm.SetPreference(context.Background(), blk.ID()))

				require.NoError(t, blk.Accept(context.Background()))

				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
				defer cancel()

				var wg sync.WaitGroup
				wg.Add(1)

				// We run WaitForEvent in a goroutine to ensure it can be safely called concurrently.
				go func() {
					defer wg.Done()
					msg, err := vm.WaitForEvent(ctx)
					require.ErrorIs(t, err, context.DeadlineExceeded)
					require.Zero(t, msg)
				}()

				wg.Wait()

				t.Log("WaitForEvent returns when regular transactions are added to the mempool")

				time.Sleep(time.Second * 2) // sleep some time to let the gas capacity to refill

				signedTx, err = newSignedLegacyTx(vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 1, vmtest.TestEthAddrs[1], big.NewInt(1), vmtest.InitialBaseFee)
				require.NoError(t, err)

				for _, err := range vm.txPool.AddRemotesSync([]*types.Transaction{signedTx}) {
					require.NoError(t, err)
				}

				wg.Add(1)

				go func() {
					defer wg.Done()
					msg, err := vm.WaitForEvent(context.Background())
					require.NoError(t, err)
					require.Equal(t, commonEng.PendingTxs, msg)
				}()

				wg.Wait()

				// Build a block again to wipe out the subscription
				blk, err = vm.BuildBlock(context.Background())
				require.NoError(t, err)

				require.NoError(t, blk.Verify(context.Background()))

				require.NoError(t, vm.SetPreference(context.Background(), blk.ID()))

				require.NoError(t, blk.Accept(context.Background()))
			},
		},
		{
			name: "WaitForEvent waits some time after a block is built",
			testCase: func(t *testing.T, vm *VM) {
				signedTx, err := newSignedLegacyTx(vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 0, vmtest.TestEthAddrs[1], big.NewInt(1), vmtest.InitialBaseFee)
				require.NoError(t, err)

				lastBuildBlockTime := time.Now()

				blk, err := vmtest.IssueTxsAndBuild([]*types.Transaction{signedTx}, vm)
				require.NoError(t, err)

				require.NoError(t, blk.Accept(context.Background()))

				signedTx, err = newSignedLegacyTx(vm.chainConfig, vmtest.TestKeys[0].ToECDSA(), 1, vmtest.TestEthAddrs[1], big.NewInt(1), vmtest.InitialBaseFee)
				require.NoError(t, err)

				for _, err := range vm.txPool.AddRemotesSync([]*types.Transaction{signedTx}) {
					require.NoError(t, err)
				}

				var wg sync.WaitGroup
				wg.Add(1)

				go func() {
					defer wg.Done()
					msg, err := vm.WaitForEvent(context.Background())
					require.NoError(t, err)
					require.Equal(t, commonEng.PendingTxs, msg)
					require.GreaterOrEqual(t, time.Since(lastBuildBlockTime), MinBlockBuildingRetryDelay)
				}()

				wg.Wait()
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fork := upgradetest.Latest
			vm := newDefaultTestVM()
			vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
				Fork: &fork,
			})
			testCase.testCase(t, vm)
			vm.Shutdown(context.Background())
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
		ctx  = context.Background()
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
	require.ErrorContains(t, batch[0].Error, "batch too large")

	// All other elements should have an error indicating there's no response
	for _, elem := range batch[1:] {
		require.ErrorIs(t, elem.Error, rpc.ErrMissingBatchResponse)
	}
}

// newSignedLegacyTx builds a legacy transaction and signs it using the
// LatestSigner derived from the provided chain config.
func newSignedLegacyTx(
	cfg *params.ChainConfig,
	key *ecdsa.PrivateKey,
	nonce uint64,
	to common.Address,
	value *big.Int,
	gasPrice *big.Int,
) (*types.Transaction, error) {
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       &to,
		Value:    value,
		Gas:      21000,
		GasPrice: gasPrice,
	})

	return types.SignTx(tx, types.LatestSigner(cfg), key)
}
