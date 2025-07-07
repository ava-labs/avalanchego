// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/log"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/vms/components/chain"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/constants"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/txpool"
	"github.com/ava-labs/subnet-evm/eth"
	"github.com/ava-labs/subnet-evm/internal/testutils"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/plugin/evm/config"
	"github.com/ava-labs/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/subnet-evm/plugin/evm/header"
	"github.com/ava-labs/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/contracts/deployerallowlist"
	"github.com/ava-labs/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/subnet-evm/rpc"
	"github.com/ava-labs/subnet-evm/utils"

	avagoconstants "github.com/ava-labs/avalanchego/utils/constants"
)

var (
	testNetworkID uint32 = avagoconstants.UnitTestID

	testMinGasPrice int64 = 225_000_000_000
	testKeys        []*ecdsa.PrivateKey
	testEthAddrs    []common.Address // testEthAddrs[i] corresponds to testKeys[i]

	firstTxAmount = new(big.Int).Mul(big.NewInt(testMinGasPrice), big.NewInt(21000*100))

	genesisJSON = func(cfg *params.ChainConfig) string {
		g := new(core.Genesis)
		g.Difficulty = big.NewInt(0)
		g.GasLimit = 8000000
		g.Timestamp = uint64(upgrade.InitiallyActiveTime.Unix())

		// Use chainId: 43111, so that it does not overlap with any Avalanche ChainIDs, which may have their
		// config overridden in vm.Initialize.
		cpy := *cfg
		cpy.ChainID = big.NewInt(43111)
		g.Config = &cpy

		allocStr := `{"0x71562b71999873DB5b286dF957af199Ec94617F7": {"balance":"0x4192927743b88000"}, "0x703c4b2bD70c169f5717101CaeE543299Fc946C7": {"balance":"0x4192927743b88000"}}`
		json.Unmarshal([]byte(allocStr), &g.Alloc)

		b, err := json.Marshal(g)
		if err != nil {
			panic(err)
		}
		return string(b)
	}

	genesisJSONPreSubnetEVM = genesisJSON(params.TestPreSubnetEVMChainConfig)
	genesisJSONSubnetEVM    = genesisJSON(params.TestSubnetEVMChainConfig)
	genesisJSONDurango      = genesisJSON(params.TestDurangoChainConfig)
	genesisJSONEtna         = genesisJSON(params.TestEtnaChainConfig)
	// genesisJSONFortuna      = genesisJSON(params.TestFortunaChainConfig)
	genesisJSONGranite = genesisJSON(params.TestGraniteChainConfig)
	genesisJSONLatest  = genesisJSONGranite
)

func init() {
	key1, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	key2, _ := crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	testKeys = append(testKeys, key1, key2)
	addr1 := crypto.PubkeyToAddress(key1.PublicKey)
	addr2 := crypto.PubkeyToAddress(key2.PublicKey)
	testEthAddrs = append(testEthAddrs, addr1, addr2)
}

// setupGenesis sets up the genesis
// If [genesisJSON] is empty, defaults to using [genesisJSONLatest]
func setupGenesis(
	t *testing.T,
	genesisJSON string,
) (*snow.Context,
	database.Database,
	[]byte,
	chan commonEng.Message,
	*atomic.Memory,
) {
	if len(genesisJSON) == 0 {
		genesisJSON = genesisJSONLatest
	}
	ctx := utils.TestSnowContext()

	baseDB := memdb.New()

	// initialize the atomic memory
	atomicMemory := atomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = atomicMemory.NewSharedMemory(ctx.ChainID)

	issuer := make(chan commonEng.Message, 1)
	prefixedDB := prefixdb.New([]byte{1}, baseDB)
	return ctx, prefixedDB, []byte(genesisJSON), issuer, atomicMemory
}

// GenesisVM creates a VM instance with the genesis test bytes and returns
// the channel use to send messages to the engine, the VM, database manager,
// and sender.
// If [genesisJSON] is empty, defaults to using [genesisJSONLatest]
func GenesisVM(t *testing.T,
	finishBootstrapping bool,
	genesisJSON string,
	configJSON string,
	upgradeJSON string,
) (
	chan commonEng.Message,
	*VM,
	database.Database,
	*enginetest.Sender,
) {
	vm := &VM{}
	ctx, dbManager, genesisBytes, issuer, _ := setupGenesis(t, genesisJSON)
	appSender := &enginetest.Sender{T: t}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error { return nil }
	err := vm.Initialize(
		context.Background(),
		ctx,
		dbManager,
		genesisBytes,
		[]byte(upgradeJSON),
		[]byte(configJSON),
		issuer,
		[]*commonEng.Fx{},
		appSender,
	)
	require.NoError(t, err, "error initializing GenesisVM")

	if finishBootstrapping {
		require.NoError(t, vm.SetState(context.Background(), snow.Bootstrapping))
		require.NoError(t, vm.SetState(context.Background(), snow.NormalOp))
	}

	return issuer, vm, dbManager, appSender
}

func TestVMConfig(t *testing.T) {
	txFeeCap := float64(11)
	enabledEthAPIs := []string{"debug"}
	configJSON := fmt.Sprintf(`{"rpc-tx-fee-cap": %g,"eth-apis": %s}`, txFeeCap, fmt.Sprintf("[%q]", enabledEthAPIs[0]))
	_, vm, _, _ := GenesisVM(t, false, "", configJSON, "")
	require.Equal(t, vm.config.RPCTxFeeCap, txFeeCap, "Tx Fee Cap should be set")
	require.Equal(t, vm.config.EthAPIs(), enabledEthAPIs, "EnabledEthAPIs should be set")
	require.NoError(t, vm.Shutdown(context.Background()))
}

func TestVMConfigDefaults(t *testing.T) {
	txFeeCap := float64(11)
	enabledEthAPIs := []string{"debug"}
	configJSON := fmt.Sprintf(`{"rpc-tx-fee-cap": %g,"eth-apis": %s}`, txFeeCap, fmt.Sprintf("[%q]", enabledEthAPIs[0]))
	_, vm, _, _ := GenesisVM(t, false, "", configJSON, "")

	var vmConfig config.Config
	vmConfig.SetDefaults(defaultTxPoolConfig)
	vmConfig.RPCTxFeeCap = txFeeCap
	vmConfig.EnabledEthAPIs = enabledEthAPIs
	require.Equal(t, vmConfig, vm.config, "VM Config should match default with overrides")
	require.NoError(t, vm.Shutdown(context.Background()))
}

func TestVMNilConfig(t *testing.T) {
	_, vm, _, _ := GenesisVM(t, false, "", "", "")

	// VM Config should match defaults if no config is passed in
	var vmConfig config.Config
	vmConfig.SetDefaults(defaultTxPoolConfig)
	require.Equal(t, vmConfig, vm.config, "VM Config should match default config")
	require.NoError(t, vm.Shutdown(context.Background()))
}

func TestVMContinuousProfiler(t *testing.T) {
	profilerDir := t.TempDir()
	profilerFrequency := 500 * time.Millisecond
	configJSON := fmt.Sprintf(`{"continuous-profiler-dir": %q,"continuous-profiler-frequency": "500ms"}`, profilerDir)
	_, vm, _, _ := GenesisVM(t, false, "", configJSON, "")
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
	genesisTests := []struct {
		name             string
		genesis          string
		expectedGasPrice *big.Int
	}{
		{
			name:             "Subnet EVM",
			genesis:          genesisJSONSubnetEVM,
			expectedGasPrice: big.NewInt(0),
		},
		{
			name:             "Durango",
			genesis:          genesisJSONDurango,
			expectedGasPrice: big.NewInt(0),
		},
	}
	for _, test := range genesisTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			_, vm, _, _ := GenesisVM(t, true, test.genesis, "", "")
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

func issueAndAccept(t *testing.T, issuer <-chan commonEng.Message, vm *VM) snowman.Block {
	t.Helper()
	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	return blk
}

func TestBuildEthTxBlock(t *testing.T) {
	// reduce block gas cost
	issuer, vm, dbManager, _ := GenesisVM(t, true, genesisJSONSubnetEVM, `{"pruning-enabled":true}`, "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testutils.NewKey(t)

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}
	errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	blk1 := issueAndAccept(t, issuer, vm)
	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk1.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs = vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	vm.clock.Set(vm.clock.Time().Add(2 * time.Second))
	blk2 := issueAndAccept(t, issuer, vm)
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

	ethBlk1 := blk1.(*chain.BlockWrapper).Block.(*Block).ethBlock
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

	restartedVM := &VM{}

	if err := restartedVM.Initialize(
		context.Background(),
		utils.TestSnowContext(),
		dbManager,
		[]byte(genesisJSONSubnetEVM),
		[]byte(""),
		[]byte(`{"pruning-enabled":true}`),
		issuer,
		[]*commonEng.Fx{},
		nil,
	); err != nil {
		t.Fatal(err)
	}

	// State root should not have been committed and discarded on restart
	if ethBlk1Root := ethBlk1.Root(); restartedVM.blockChain.HasState(ethBlk1Root) {
		t.Fatalf("Expected blk1 state root to be pruned after blk2 was accepted on top of it in pruning mode")
	}

	// State root should be committed when accepted tip on shutdown
	ethBlk2 := blk2.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if ethBlk2Root := ethBlk2.Root(); !restartedVM.blockChain.HasState(ethBlk2Root) {
		t.Fatalf("Expected blk2 state root to not be pruned after shutdown (last accepted tip should be committed)")
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
	// Create two VMs which will agree on block A and then
	// build the two distinct preferred chains above
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, `{"pruning-enabled":true}`, "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, `{"pruning-enabled":true}`, "")

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

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm1.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
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
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[1])
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm1.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	errs = vm2.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkC, err := vm2.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if err := vm2.SetPreference(context.Background(), vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Block D
	errs = vm2.txPool.AddRemotesSync(txs[5:10])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkD, err := vm2.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	if err := vm2BlkD.Verify(context.Background()); err != nil {
		t.Fatalf("BlkD failed verification on VM2: %s", err)
	}

	if err := vm2.SetPreference(context.Background(), vm2BlkD.ID()); err != nil {
		t.Fatal(err)
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
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, `{"pruning-enabled":false}`, "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, `{"pruning-enabled":false}`, "")

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

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm1.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
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
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[1])
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm1.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	errs = vm2.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkC, err := vm2.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
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
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")

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

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm1.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
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
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[1])
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := vm1.GetBlockIDAtHeight(context.Background(), vm1BlkB.Height()); err != database.ErrNotFound {
		t.Fatalf("Expected unaccepted block not to be indexed by height, but found %s", err)
	}

	if err := vm1.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}

	errs = vm2.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkC, err := vm2.BuildBlock(context.Background())
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

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
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
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")

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

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm1.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
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
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[1])
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm1.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}

	errs = vm2.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkC, err := vm2.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if err := vm2.SetPreference(context.Background(), vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	errs = vm2.txPool.AddRemotesSync(txs[5:])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkD, err := vm2.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	// Parse blocks produced in vm2
	vm1BlkC, err := vm1.ParseBlock(context.Background(), vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()

	vm1BlkD, err := vm1.ParseBlock(context.Background(), vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	blkDHeight := vm1BlkD.Height()
	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()

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
	if err := vm1BlkD.Accept(context.Background()); !strings.Contains(err.Error(), "expected accepted block to have parent") {
		t.Fatalf("unexpected error when accepting out of order block: %s", err)
	}

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
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")

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

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm1.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
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

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[1])
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	errs = vm1.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm1.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	errs = vm2.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkC, err := vm2.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if err := vm2.SetPreference(context.Background(), vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	errs = vm2.txPool.AddRemotesSync(txs[5:10])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkD, err := vm2.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	// Create uncle block from blkD
	blkDEthBlock := vm2BlkD.(*chain.BlockWrapper).Block.(*Block).ethBlock
	uncles := []*types.Header{vm1BlkB.(*chain.BlockWrapper).Block.(*Block).ethBlock.Header()}
	uncleBlockHeader := types.CopyHeader(blkDEthBlock.Header())
	uncleBlockHeader.UncleHash = types.CalcUncleHash(uncles)

	uncleEthBlock := types.NewBlock(
		uncleBlockHeader,
		blkDEthBlock.Transactions(),
		uncles,
		nil,
		trie.NewStackTrie(nil),
	)
	uncleBlock := vm2.newBlock(uncleEthBlock)

	if err := uncleBlock.Verify(context.Background()); !errors.Is(err, errUnclesUnsupported) {
		t.Fatalf("VM2 should have failed with %q but got %q", errUnclesUnsupported, err.Error())
	}
	if _, err := vm1.ParseBlock(context.Background(), vm2BlkC.Bytes()); err != nil {
		t.Fatalf("VM1 errored parsing blkC: %s", err)
	}
	if _, err := vm1.ParseBlock(context.Background(), uncleBlock.Bytes()); !errors.Is(err, errUnclesUnsupported) {
		t.Fatalf("VM1 should have failed with %q but got %q", errUnclesUnsupported, err.Error())
	}
}

// Regression test to ensure that a VM that is not able to parse a block that
// contains no transactions.
func TestEmptyBlock(t *testing.T) {
	issuer, vm, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	// Create empty block from blkA
	ethBlock := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock

	emptyEthBlock := types.NewBlock(
		types.CopyHeader(ethBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
	)

	emptyBlock := vm.newBlock(emptyEthBlock)

	if _, err := vm.ParseBlock(context.Background(), emptyBlock.Bytes()); !errors.Is(err, errEmptyBlock) {
		t.Fatalf("VM should have failed with errEmptyBlock but got %s", err.Error())
	}
	if err := emptyBlock.Verify(context.Background()); !errors.Is(err, errEmptyBlock) {
		t.Fatalf("block should have failed verification with errEmptyBlock but got %s", err.Error())
	}
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
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")

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

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm1.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
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
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[1])
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference
	// for block B
	errs := vm1.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm1.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	errs = vm2.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2

	vm2BlkC, err := vm2.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if err := vm2.SetPreference(context.Background(), vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	errs = vm2.txPool.AddRemotesSync(txs[5:])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2

	vm2BlkD, err := vm2.BuildBlock(context.Background())
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

	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.blockChain.CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	if err := vm1BlkC.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.blockChain.CurrentBlock(); b.Hash() != blkCHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkCHash.Hex(), b.Hash().Hex())
	}
	if err := vm1BlkB.Reject(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkD.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.blockChain.CurrentBlock(); b.Hash() != blkDHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
	}
}

func TestFutureBlock(t *testing.T) {
	issuer, vm, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blkA, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	// Create empty block from blkA
	internalBlkA := blkA.(*chain.BlockWrapper).Block.(*Block)
	modifiedHeader := types.CopyHeader(internalBlkA.ethBlock.Header())
	// Set the VM's clock to the time of the produced block
	vm.clock.Set(time.Unix(int64(modifiedHeader.Time), 0))
	// Set the modified time to exceed the allowed future time
	modifiedTime := modifiedHeader.Time + uint64(maxFutureBlockTime.Seconds()+1)
	modifiedHeader.Time = modifiedTime
	modifiedBlock := types.NewBlock(
		modifiedHeader,
		internalBlkA.ethBlock.Transactions(),
		nil,
		nil,
		trie.NewStackTrie(nil),
	)

	futureBlock := vm.newBlock(modifiedBlock)

	if err := futureBlock.Verify(context.Background()); err == nil {
		t.Fatal("Future block should have failed verification due to block timestamp too far in the future")
	} else if !strings.Contains(err.Error(), "block timestamp is too far in the future") {
		t.Fatalf("Expected error to be block timestamp too far in the future but found %s", err)
	}
}

func TestLastAcceptedBlockNumberAllow(t *testing.T) {
	issuer, vm, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM: %s", err)
	}

	if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	blkHeight := blk.Height()
	blkHash := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()

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
	if !errors.Is(err, eth.ErrUnfinalizedData) {
		t.Fatalf("expected ErrUnfinalizedData but got %s", err.Error())
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatalf("VM failed to accept block: %s", err)
	}

	if b := vm.blockChain.GetBlockByNumber(blkHeight); b.Hash() != blkHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())
	}
}

// Regression test to ensure we can build blocks if we are starting with the
// Subnet EVM ruleset in genesis.
func TestBuildSubnetEVMBlock(t *testing.T) {
	issuer, vm, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], new(big.Int).Mul(firstTxAmount, big.NewInt(4)), 21000, big.NewInt(testMinGasPrice*3), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	blk := issueAndAccept(t, issuer, vm)
	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(testMinGasPrice*3), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[1])
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	blk = issueAndAccept(t, issuer, vm)
	ethBlk := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if customtypes.BlockGasCost(ethBlk) == nil || customtypes.BlockGasCost(ethBlk).Cmp(big.NewInt(100)) < 0 {
		t.Fatalf("expected blockGasCost to be at least 100 but got %d", customtypes.BlockGasCost(ethBlk))
	}
	chainConfig := params.GetExtra(vm.chainConfig)
	minRequiredTip, err := header.EstimateRequiredTip(chainConfig, ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(big.NewInt(0.05*utils.GWei)) < 0 {
		t.Fatalf("expected minRequiredTip to be at least 0.05 gwei but got %d", minRequiredTip)
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

func TestBuildAllowListActivationBlock(t *testing.T) {
	genesis := &core.Genesis{}
	if err := genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)); err != nil {
		t.Fatal(err)
	}
	params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
		deployerallowlist.ConfigKey: deployerallowlist.NewConfig(utils.TimeToNewUint64(time.Now()), testEthAddrs, nil, nil),
	}

	genesisJSON, err := genesis.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	issuer, vm, _, _ := GenesisVM(t, true, string(genesisJSON), "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	genesisState, err := vm.blockChain.StateAt(vm.blockChain.Genesis().Root())
	if err != nil {
		t.Fatal(err)
	}
	role := deployerallowlist.GetContractDeployerAllowListStatus(genesisState, testEthAddrs[0])
	if role != allowlist.NoRole {
		t.Fatalf("Expected allow list status to be set to no role: %s, but found: %s", allowlist.NoRole, role)
	}

	// Send basic transaction to construct a simple block and confirm that the precompile state configuration in the worker behaves correctly.
	tx := types.NewTransaction(uint64(0), testEthAddrs[1], new(big.Int).Mul(firstTxAmount, big.NewInt(4)), 21000, big.NewInt(testMinGasPrice*3), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	blk := issueAndAccept(t, issuer, vm)
	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Verify that the allow list config activation was handled correctly in the first block.
	blkState, err := vm.blockChain.StateAt(blk.(*chain.BlockWrapper).Block.(*Block).ethBlock.Root())
	if err != nil {
		t.Fatal(err)
	}
	role = deployerallowlist.GetContractDeployerAllowListStatus(blkState, testEthAddrs[0])
	if role != allowlist.AdminRole {
		t.Fatalf("Expected allow list status to be set role %s, but found: %s", allowlist.AdminRole, role)
	}
}

// Test that the tx allow list allows whitelisted transactions and blocks non-whitelisted addresses
func TestTxAllowListSuccessfulTx(t *testing.T) {
	// Setup chain params
	managerKey := testKeys[1]
	managerAddress := testEthAddrs[1]
	genesis := &core.Genesis{}
	if err := genesis.UnmarshalJSON([]byte(genesisJSONDurango)); err != nil {
		t.Fatal(err)
	}
	// this manager role should not be activated because DurangoTimestamp is in the future
	params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
		txallowlist.ConfigKey: txallowlist.NewConfig(utils.NewUint64(0), testEthAddrs[0:1], nil, nil),
	}
	durangoTime := time.Now().Add(10 * time.Hour)
	params.GetExtra(genesis.Config).DurangoTimestamp = utils.TimeToNewUint64(durangoTime)
	genesisJSON, err := genesis.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

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
	issuer, vm, _, _ := GenesisVM(t, true, string(genesisJSON), "", string(upgradeBytesJSON))

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	genesisState, err := vm.blockChain.StateAt(vm.blockChain.Genesis().Root())
	if err != nil {
		t.Fatal(err)
	}

	// Check that address 0 is whitelisted and address 1 is not
	role := txallowlist.GetTxAllowListStatus(genesisState, testEthAddrs[0])
	if role != allowlist.AdminRole {
		t.Fatalf("Expected allow list status to be set to admin: %s, but found: %s", allowlist.AdminRole, role)
	}
	role = txallowlist.GetTxAllowListStatus(genesisState, testEthAddrs[1])
	if role != allowlist.NoRole {
		t.Fatalf("Expected allow list status to be set to no role: %s, but found: %s", allowlist.NoRole, role)
	}
	// Should not be a manager role because Durango has not activated yet
	role = txallowlist.GetTxAllowListStatus(genesisState, managerAddress)
	require.Equal(t, allowlist.NoRole, role)

	// Submit a successful transaction
	tx0 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx0, err := types.SignTx(tx0, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	require.NoError(t, err)

	errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	if err := errs[0]; err != nil {
		t.Fatalf("Failed to add tx at index: %s", err)
	}

	// Submit a rejected transaction, should throw an error
	tx1 := types.NewTransaction(uint64(0), testEthAddrs[1], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx1, err := types.SignTx(tx1, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[1])
	if err != nil {
		t.Fatal(err)
	}

	errs = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	if err := errs[0]; !errors.Is(err, vmerrors.ErrSenderAddressNotAllowListed) {
		t.Fatalf("expected ErrSenderAddressNotAllowListed, got: %s", err)
	}

	// Submit a rejected transaction, should throw an error because manager is not activated
	tx2 := types.NewTransaction(uint64(0), managerAddress, big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx2, err := types.SignTx(tx2, types.NewEIP155Signer(vm.chainConfig.ChainID), managerKey)
	require.NoError(t, err)

	errs = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})
	require.ErrorIs(t, errs[0], vmerrors.ErrSenderAddressNotAllowListed)

	blk := issueAndAccept(t, issuer, vm)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))

	// Verify that the constructed block only has the whitelisted tx
	block := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock

	txs := block.Transactions()

	if txs.Len() != 1 {
		t.Fatalf("Expected number of txs to be %d, but found %d", 1, txs.Len())
	}

	require.Equal(t, signedTx0.Hash(), txs[0].Hash())

	vm.clock.Set(reenableAllowlistTime.Add(time.Hour))

	// Re-Submit a successful transaction
	tx0 = types.NewTransaction(uint64(1), testEthAddrs[0], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx0, err = types.SignTx(tx0, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	require.NoError(t, err)

	errs = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	require.NoError(t, errs[0])

	// accept block to trigger upgrade
	blk = issueAndAccept(t, issuer, vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	block = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock

	blkState, err := vm.blockChain.StateAt(block.Root())
	require.NoError(t, err)

	// Check that address 0 is admin and address 1 is manager
	role = txallowlist.GetTxAllowListStatus(blkState, testEthAddrs[0])
	require.Equal(t, allowlist.AdminRole, role)
	role = txallowlist.GetTxAllowListStatus(blkState, managerAddress)
	require.Equal(t, allowlist.ManagerRole, role)

	vm.clock.Set(vm.clock.Time().Add(2 * time.Second)) // add 2 seconds for gas fee to adjust
	// Submit a successful transaction, should not throw an error because manager is activated
	tx3 := types.NewTransaction(uint64(0), managerAddress, big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx3, err := types.SignTx(tx3, types.NewEIP155Signer(vm.chainConfig.ChainID), managerKey)
	require.NoError(t, err)

	vm.clock.Set(vm.clock.Time().Add(2 * time.Second)) // add 2 seconds for gas fee to adjust
	errs = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx3})
	require.NoError(t, errs[0])

	blk = issueAndAccept(t, issuer, vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))

	// Verify that the constructed block only has the whitelisted tx
	block = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	txs = block.Transactions()

	require.Len(t, txs, 1)
	require.Equal(t, signedTx3.Hash(), txs[0].Hash())
}

func TestVerifyManagerConfig(t *testing.T) {
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(genesisJSONDurango)))

	durangoTimestamp := time.Now().Add(10 * time.Hour)
	params.GetExtra(genesis.Config).DurangoTimestamp = utils.TimeToNewUint64(durangoTimestamp)
	// this manager role should not be activated because DurangoTimestamp is in the future
	params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
		txallowlist.ConfigKey: txallowlist.NewConfig(utils.NewUint64(0), testEthAddrs[0:1], nil, []common.Address{testEthAddrs[1]}),
	}

	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)

	vm := &VM{}
	ctx, dbManager, genesisBytes, issuer, _ := setupGenesis(t, string(genesisJSON))
	err = vm.Initialize(
		context.Background(),
		ctx,
		dbManager,
		genesisBytes,
		[]byte(""),
		[]byte(""),
		issuer,
		[]*commonEng.Fx{},
		nil,
	)
	require.ErrorIs(t, err, allowlist.ErrCannotAddManagersBeforeDurango)

	genesis = &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(genesisJSONDurango)))
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
	ctx, dbManager, genesisBytes, issuer, _ = setupGenesis(t, string(genesisJSON))
	err = vm.Initialize(
		context.Background(),
		ctx,
		dbManager,
		genesisBytes,
		upgradeBytesJSON,
		[]byte(""),
		issuer,
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
	if err := genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)); err != nil {
		t.Fatal(err)
	}
	enableAllowListTimestamp := upgrade.InitiallyActiveTime // enable at initially active time
	params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
		txallowlist.ConfigKey: txallowlist.NewConfig(utils.TimeToNewUint64(enableAllowListTimestamp), testEthAddrs[0:1], nil, nil),
	}
	genesisJSON, err := genesis.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

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

	issuer, vm, _, _ := GenesisVM(t, true, string(genesisJSON), "", upgradeConfig)

	vm.clock.Set(disableAllowListTimestamp) // upgrade takes effect after a block is issued, so we can set vm's clock here.

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	genesisState, err := vm.blockChain.StateAt(vm.blockChain.Genesis().Root())
	if err != nil {
		t.Fatal(err)
	}

	// Check that address 0 is whitelisted and address 1 is not
	role := txallowlist.GetTxAllowListStatus(genesisState, testEthAddrs[0])
	if role != allowlist.AdminRole {
		t.Fatalf("Expected allow list status to be set to admin: %s, but found: %s", allowlist.AdminRole, role)
	}
	role = txallowlist.GetTxAllowListStatus(genesisState, testEthAddrs[1])
	if role != allowlist.NoRole {
		t.Fatalf("Expected allow list status to be set to no role: %s, but found: %s", allowlist.NoRole, role)
	}

	// Submit a successful transaction
	tx0 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx0, err := types.SignTx(tx0, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	require.NoError(t, err)

	errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	if err := errs[0]; err != nil {
		t.Fatalf("Failed to add tx at index: %s", err)
	}

	// Submit a rejected transaction, should throw an error
	tx1 := types.NewTransaction(uint64(0), testEthAddrs[1], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx1, err := types.SignTx(tx1, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[1])
	if err != nil {
		t.Fatal(err)
	}

	errs = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	if err := errs[0]; !errors.Is(err, vmerrors.ErrSenderAddressNotAllowListed) {
		t.Fatalf("expected ErrSenderAddressNotAllowListed, got: %s", err)
	}

	blk := issueAndAccept(t, issuer, vm)

	// Verify that the constructed block only has the whitelisted tx
	block := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	txs := block.Transactions()
	if txs.Len() != 1 {
		t.Fatalf("Expected number of txs to be %d, but found %d", 1, txs.Len())
	}
	require.Equal(t, signedTx0.Hash(), txs[0].Hash())

	// verify the issued block is after the network upgrade
	require.GreaterOrEqual(t, int64(block.Time()), disableAllowListTimestamp.Unix())

	<-newTxPoolHeadChan // wait for new head in tx pool

	// retry the rejected Tx, which should now succeed
	errs = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	if err := errs[0]; err != nil {
		t.Fatalf("Failed to add tx at index: %s", err)
	}

	vm.clock.Set(vm.clock.Time().Add(2 * time.Second)) // add 2 seconds for gas fee to adjust
	blk = issueAndAccept(t, issuer, vm)

	// Verify that the constructed block only has the previously rejected tx
	block = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	txs = block.Transactions()
	if txs.Len() != 1 {
		t.Fatalf("Expected number of txs to be %d, but found %d", 1, txs.Len())
	}
	require.Equal(t, signedTx1.Hash(), txs[0].Hash())
}

// Test that the fee manager changes fee configuration
func TestFeeManagerChangeFee(t *testing.T) {
	// Setup chain params
	genesis := &core.Genesis{}
	if err := genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)); err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	issuer, vm, _, _ := GenesisVM(t, true, string(genesisJSON), "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	genesisState, err := vm.blockChain.StateAt(vm.blockChain.Genesis().Root())
	if err != nil {
		t.Fatal(err)
	}

	// Check that address 0 is whitelisted and address 1 is not
	role := feemanager.GetFeeManagerStatus(genesisState, testEthAddrs[0])
	if role != allowlist.AdminRole {
		t.Fatalf("Expected fee manager list status to be set to admin: %s, but found: %s", allowlist.AdminRole, role)
	}
	role = feemanager.GetFeeManagerStatus(genesisState, testEthAddrs[1])
	if role != allowlist.NoRole {
		t.Fatalf("Expected fee manager list status to be set to no role: %s, but found: %s", allowlist.NoRole, role)
	}
	// Contract is initialized but no preconfig is given, reader should return genesis fee config
	feeConfig, lastChangedAt, err := vm.blockChain.GetFeeConfigAt(vm.blockChain.Genesis().Header())
	require.NoError(t, err)
	require.EqualValues(t, feeConfig, testLowFeeConfig)
	require.Zero(t, vm.blockChain.CurrentBlock().Number.Cmp(lastChangedAt))

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

	signedTx, err := types.SignTx(tx, types.LatestSigner(genesis.Config), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	if err := errs[0]; err != nil {
		t.Fatalf("Failed to add tx at index: %s", err)
	}

	blk := issueAndAccept(t, issuer, vm)
	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	block := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock

	feeConfig, lastChangedAt, err = vm.blockChain.GetFeeConfigAt(block.Header())
	require.NoError(t, err)
	require.EqualValues(t, testHighFeeConfig, feeConfig)
	require.EqualValues(t, vm.blockChain.CurrentBlock().Number, lastChangedAt)

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

	signedTx2, err := types.SignTx(tx2, types.LatestSigner(genesis.Config), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	err = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})[0]
	require.ErrorIs(t, err, txpool.ErrUnderpriced)
}

// Test Allow Fee Recipients is disabled and, etherbase must be blackhole address
func TestAllowFeeRecipientDisabled(t *testing.T) {
	genesis := &core.Genesis{}
	if err := genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)); err != nil {
		t.Fatal(err)
	}
	params.GetExtra(genesis.Config).AllowFeeRecipients = false // set to false initially
	genesisJSON, err := genesis.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	issuer, vm, _, _ := GenesisVM(t, true, string(genesisJSON), "", "")

	vm.miner.SetEtherbase(common.HexToAddress("0x0123456789")) // set non-blackhole address by force
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], new(big.Int).Mul(firstTxAmount, big.NewInt(4)), 21000, big.NewInt(testMinGasPrice*3), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	require.NoError(t, err) // this won't return an error since miner will set the etherbase to blackhole address

	ethBlock := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	require.Equal(t, constants.BlackholeAddr, ethBlock.Coinbase())

	// Create empty block from blk
	internalBlk := blk.(*chain.BlockWrapper).Block.(*Block)
	modifiedHeader := types.CopyHeader(internalBlk.ethBlock.Header())
	modifiedHeader.Coinbase = common.HexToAddress("0x0123456789") // set non-blackhole address by force
	modifiedBlock := types.NewBlock(
		modifiedHeader,
		internalBlk.ethBlock.Transactions(),
		nil,
		nil,
		trie.NewStackTrie(nil),
	)

	modifiedBlk := vm.newBlock(modifiedBlock)

	require.ErrorIs(t, modifiedBlk.Verify(context.Background()), vmerrors.ErrInvalidCoinbase)
}

func TestAllowFeeRecipientEnabled(t *testing.T) {
	genesis := &core.Genesis{}
	if err := genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)); err != nil {
		t.Fatal(err)
	}
	params.GetExtra(genesis.Config).AllowFeeRecipients = true
	genesisJSON, err := genesis.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	etherBase := common.HexToAddress("0x0123456789")
	c := config.Config{}
	c.SetDefaults(defaultTxPoolConfig)
	c.FeeRecipient = etherBase.String()
	configJSON, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	issuer, vm, _, _ := GenesisVM(t, true, string(genesisJSON), string(configJSON), "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], new(big.Int).Mul(firstTxAmount, big.NewInt(4)), 21000, big.NewInt(testMinGasPrice*3), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	blk := issueAndAccept(t, issuer, vm)
	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}
	ethBlock := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	require.Equal(t, etherBase, ethBlock.Coinbase())
	// Verify that etherBase has received fees
	blkState, err := vm.blockChain.StateAt(ethBlock.Root())
	if err != nil {
		t.Fatal(err)
	}

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
	c := config.Config{}
	c.SetDefaults(defaultTxPoolConfig)
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

	issuer, vm, _, _ := GenesisVM(t, true, string(genesisJSON), string(configJSON), upgradeConfig)

	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	testAddr := common.HexToAddress("0x9999991111")
	data, err := rewardmanager.PackSetRewardAddress(testAddr)
	require.NoError(t, err)

	gas := 21000 + 240 + rewardmanager.SetRewardAddressGasCost + rewardmanager.RewardAddressChangedEventGasCost // 21000 for tx, 240 for tx data

	tx := types.NewTransaction(uint64(0), rewardmanager.ContractAddress, big.NewInt(1), gas, big.NewInt(testMinGasPrice), data)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	require.NoError(t, err)

	txErrors := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk := issueAndAccept(t, issuer, vm)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	require.Equal(t, etherBase, ethBlock.Coinbase()) // reward address is activated at this block so this is fine

	tx1 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice*3), nil)
	signedTx1, err := types.SignTx(tx1, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[1])
	require.NoError(t, err)

	txErrors = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk = issueAndAccept(t, issuer, vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	require.Equal(t, testAddr, ethBlock.Coinbase()) // reward address was activated at previous block
	// Verify that etherBase has received fees
	blkState, err := vm.blockChain.StateAt(ethBlock.Root())
	require.NoError(t, err)

	balance := blkState.GetBalance(testAddr)
	require.Equal(t, 1, balance.Cmp(common.U2560))

	// Test Case: Disable reward manager
	// This should revert back to enabling fee recipients
	previousBalance := blkState.GetBalance(etherBase)

	// issue a new block to trigger the upgrade
	vm.clock.Set(disableTime) // upgrade takes effect after a block is issued, so we can set vm's clock here.
	tx2 := types.NewTransaction(uint64(1), testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx2, err := types.SignTx(tx2, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[1])
	require.NoError(t, err)

	txErrors = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk = issueAndAccept(t, issuer, vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	// Reward manager deactivated at this block, so we expect the parent state
	// to determine the coinbase for this block before full deactivation in the
	// next block.
	require.Equal(t, testAddr, ethBlock.Coinbase())
	require.GreaterOrEqual(t, int64(ethBlock.Time()), disableTime.Unix())

	vm.clock.Set(vm.clock.Time().Add(3 * time.Hour)) // let time pass to decrease gas price
	// issue another block to verify that the reward manager is disabled
	tx2 = types.NewTransaction(uint64(2), testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx2, err = types.SignTx(tx2, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[1])
	require.NoError(t, err)

	txErrors = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk = issueAndAccept(t, issuer, vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	// reward manager was disabled at previous block
	// so this block should revert back to enabling fee recipients
	require.Equal(t, etherBase, ethBlock.Coinbase())
	require.GreaterOrEqual(t, int64(ethBlock.Time()), disableTime.Unix())

	// Verify that Blackhole has received fees
	blkState, err = vm.blockChain.StateAt(ethBlock.Root())
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
	c := config.Config{}
	c.SetDefaults(defaultTxPoolConfig)
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
	issuer, vm, _, _ := GenesisVM(t, true, string(genesisJSON), string(configJSON), upgradeConfig)

	defer func() {
		require.NoError(t, vm.Shutdown(context.Background()))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	data, err := rewardmanager.PackAllowFeeRecipients()
	require.NoError(t, err)

	gas := 21000 + 240 + rewardmanager.SetRewardAddressGasCost + rewardmanager.RewardAddressChangedEventGasCost // 21000 for tx, 240 for tx data

	tx := types.NewTransaction(uint64(0), rewardmanager.ContractAddress, big.NewInt(1), gas, big.NewInt(testMinGasPrice), data)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	require.NoError(t, err)

	txErrors := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk := issueAndAccept(t, issuer, vm)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	require.Equal(t, constants.BlackholeAddr, ethBlock.Coinbase()) // reward address is activated at this block so this is fine

	tx1 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice*3), nil)
	signedTx1, err := types.SignTx(tx1, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[1])
	require.NoError(t, err)

	txErrors = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk = issueAndAccept(t, issuer, vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	require.Equal(t, etherBase, ethBlock.Coinbase()) // reward address was activated at previous block
	// Verify that etherBase has received fees
	blkState, err := vm.blockChain.StateAt(ethBlock.Root())
	require.NoError(t, err)

	balance := blkState.GetBalance(etherBase)
	require.Equal(t, 1, balance.Cmp(common.U2560))

	// Test Case: Disable reward manager
	// This should revert back to burning fees
	previousBalance := blkState.GetBalance(constants.BlackholeAddr)

	vm.clock.Set(disableTime) // upgrade takes effect after a block is issued, so we can set vm's clock here.
	tx2 := types.NewTransaction(uint64(1), testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx2, err := types.SignTx(tx2, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[1])
	require.NoError(t, err)

	txErrors = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk = issueAndAccept(t, issuer, vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	require.Equal(t, etherBase, ethBlock.Coinbase()) // reward address was activated at previous block
	require.GreaterOrEqual(t, int64(ethBlock.Time()), disableTime.Unix())

	vm.clock.Set(vm.clock.Time().Add(3 * time.Hour)) // let time pass so that gas price is reduced
	tx2 = types.NewTransaction(uint64(2), testEthAddrs[0], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx2, err = types.SignTx(tx2, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[1])
	require.NoError(t, err)

	txErrors = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})
	for _, err := range txErrors {
		require.NoError(t, err)
	}

	blk = issueAndAccept(t, issuer, vm)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	ethBlock = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	require.Equal(t, constants.BlackholeAddr, ethBlock.Coinbase()) // reward address was activated at previous block
	require.Greater(t, int64(ethBlock.Time()), disableTime.Unix())

	// Verify that Blackhole has received fees
	blkState, err = vm.blockChain.StateAt(ethBlock.Root())
	require.NoError(t, err)

	balance = blkState.GetBalance(constants.BlackholeAddr)
	require.Equal(t, 1, balance.Cmp(previousBalance))
}

func TestSkipChainConfigCheckCompatible(t *testing.T) {
	// The most recent network upgrade in Subnet-EVM is SubnetEVM itself, which cannot be disabled for this test since it results in
	// disabling dynamic fees and causes a panic since some code assumes that this is enabled.
	// TODO update this test when there is a future network upgrade that can be skipped in the config.
	t.Skip("no skippable upgrades")

	issuer, vm, dbManager, appSender := GenesisVM(t, true, genesisJSONPreSubnetEVM, `{"pruning-enabled":true}`, "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testutils.NewKey(t)

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}
	errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	blk := issueAndAccept(t, issuer, vm)
	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	reinitVM := &VM{}
	// use the block's timestamp instead of 0 since rewind to genesis
	// is hardcoded to be allowed in core/genesis.go.
	genesisWithUpgrade := &core.Genesis{}
	require.NoError(t, json.Unmarshal([]byte(genesisJSONDurango), genesisWithUpgrade))
	params.GetExtra(genesisWithUpgrade.Config).EtnaTimestamp = utils.TimeToNewUint64(blk.Timestamp())
	genesisWithUpgradeBytes, err := json.Marshal(genesisWithUpgrade)
	require.NoError(t, err)

	// Reset metrics to allow re-initialization
	vm.ctx.Metrics = metrics.NewPrefixGatherer()

	// this will not be allowed
	err = reinitVM.Initialize(context.Background(), vm.ctx, dbManager, genesisWithUpgradeBytes, []byte{}, []byte{}, issuer, []*commonEng.Fx{}, appSender)
	require.ErrorContains(t, err, "mismatching Cancun fork timestamp in database")

	// Reset metrics to allow re-initialization
	vm.ctx.Metrics = metrics.NewPrefixGatherer()

	// try again with skip-upgrade-check
	config := []byte(`{"skip-upgrade-check": true}`)
	err = reinitVM.Initialize(context.Background(), vm.ctx, dbManager, genesisWithUpgradeBytes, []byte{}, config, issuer, []*commonEng.Fx{}, appSender)
	require.NoError(t, err)
	require.NoError(t, reinitVM.Shutdown(context.Background()))
}

func TestParentBeaconRootBlock(t *testing.T) {
	tests := []struct {
		name          string
		genesisJSON   string
		beaconRoot    *common.Hash
		expectedError bool
		errString     string
	}{
		{
			name:          "non-empty parent beacon root in Durango",
			genesisJSON:   genesisJSONDurango,
			beaconRoot:    &common.Hash{0x01},
			expectedError: true,
			// err string wont work because it will also fail with blob gas is non-empty (zeroed)
		},
		{
			name:          "empty parent beacon root in Durango",
			genesisJSON:   genesisJSONDurango,
			beaconRoot:    &common.Hash{},
			expectedError: true,
		},
		{
			name:          "nil parent beacon root in Durango",
			genesisJSON:   genesisJSONDurango,
			beaconRoot:    nil,
			expectedError: false,
		},
		{
			name:          "non-empty parent beacon root in E-Upgrade (Cancun)",
			genesisJSON:   genesisJSONEtna,
			beaconRoot:    &common.Hash{0x01},
			expectedError: true,
			errString:     "expected empty hash",
		},
		{
			name:          "empty parent beacon root in E-Upgrade (Cancun)",
			genesisJSON:   genesisJSONEtna,
			beaconRoot:    &common.Hash{},
			expectedError: false,
		},
		{
			name:          "nil parent beacon root in E-Upgrade (Cancun)",
			genesisJSON:   genesisJSONEtna,
			beaconRoot:    nil,
			expectedError: true,
			errString:     "header is missing parentBeaconRoot",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			issuer, vm, _, _ := GenesisVM(t, true, test.genesisJSON, "", "")

			defer func() {
				if err := vm.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
			}()

			tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
			if err != nil {
				t.Fatal(err)
			}

			txErrors := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
			for i, err := range txErrors {
				if err != nil {
					t.Fatalf("Failed to add tx at index %d: %s", i, err)
				}
			}

			<-issuer

			blk, err := vm.BuildBlock(context.Background())
			if err != nil {
				t.Fatalf("Failed to build block with import transaction: %s", err)
			}

			// Modify the block to have a parent beacon root
			ethBlock := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
			header := types.CopyHeader(ethBlock.Header())
			header.ParentBeaconRoot = test.beaconRoot
			parentBeaconEthBlock := ethBlock.WithSeal(header)

			parentBeaconBlock := vm.newBlock(parentBeaconEthBlock)

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

func TestStandaloneDB(t *testing.T) {
	vm := &VM{}
	ctx := utils.TestSnowContext()
	baseDB := memdb.New()
	atomicMemory := atomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = atomicMemory.NewSharedMemory(ctx.ChainID)
	issuer := make(chan commonEng.Message, 1)
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
		context.Background(),
		ctx,
		sharedDB,
		[]byte(genesisJSONLatest),
		nil,
		[]byte(configJSON),
		issuer,
		[]*commonEng.Fx{},
		appSender,
	)
	defer vm.Shutdown(context.Background())
	require.NoError(t, err, "error initializing VM")
	require.NoError(t, vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(t, vm.SetState(context.Background(), snow.NormalOp))

	// Issue a block
	acceptedBlockEvent := make(chan core.ChainEvent, 1)
	vm.blockChain.SubscribeChainAcceptedEvent(acceptedBlockEvent)
	tx0 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx0, err := types.SignTx(tx0, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	require.NoError(t, err)
	errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	require.NoError(t, errs[0])

	// accept block
	blk := issueAndAccept(t, issuer, vm)
	newBlock := <-acceptedBlockEvent
	require.Equal(t, newBlock.Block.Hash(), common.Hash(blk.ID()))

	// Ensure that the shared database is empty
	assert.True(t, isDBEmpty(baseDB))
	// Ensure that the standalone database is not empty
	assert.False(t, isDBEmpty(vm.db))
	assert.False(t, isDBEmpty(vm.acceptedBlockDB))
}

func TestFeeManagerRegressionMempoolMinFeeAfterRestart(t *testing.T) {
	// Setup chain params
	genesis := &core.Genesis{}
	if err := genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)); err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	issuer, vm, sharedDB, appSender := GenesisVM(t, true, string(genesisJSON), "", "")

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
	signedTx, err := types.SignTx(tx, types.LatestSigner(genesis.Config), testKeys[0])
	require.NoError(t, err)

	errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0], txpool.ErrUnderpriced) // should fail because mempool expects higher fee

	// restart vm and try again
	restartedVM, err := restartVM(vm, sharedDB, genesisJSON, issuer, appSender, true)
	require.NoError(t, err)

	// it still should fail
	errs = restartedVM.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0], txpool.ErrUnderpriced)

	// send a tx to activate the precompile
	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	restartedVM.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)
	restartedVM.clock.Set(utils.Uint64ToTime(precompileActivationTime).Add(time.Second * 10))
	tx = types.NewTransaction(uint64(0), testEthAddrs[0], common.Big0, 21000, big.NewInt(testHighFeeConfig.MinBaseFee.Int64()), nil)
	signedTx, err = types.SignTx(tx, types.LatestSigner(genesis.Config), testKeys[0])
	require.NoError(t, err)
	errs = restartedVM.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.NoError(t, errs[0])
	blk := issueAndAccept(t, issuer, restartedVM)
	newHead := <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
	// Contract is initialized but no preconfig is given, reader should return genesis fee config
	feeConfig, lastChangedAt, err := vm.blockChain.GetFeeConfigAt(vm.blockChain.Genesis().Header())
	require.NoError(t, err)
	require.EqualValues(t, feeConfig, testHighFeeConfig)
	require.Zero(t, vm.blockChain.CurrentBlock().Number.Cmp(lastChangedAt))

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
	signedTx, err = types.SignTx(tx, types.LatestSigner(genesis.Config), testKeys[0])
	require.NoError(t, err)
	errs = restartedVM.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.NoError(t, errs[0])
	blk = issueAndAccept(t, issuer, restartedVM)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))

	// check that the fee config is updated
	block := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	feeConfig, lastChangedAt, err = restartedVM.blockChain.GetFeeConfigAt(block.Header())
	require.NoError(t, err)
	require.EqualValues(t, restartedVM.blockChain.CurrentBlock().Number, lastChangedAt)
	require.EqualValues(t, testLowFeeConfig, feeConfig)

	// send another tx with low fee
	tx = types.NewTransaction(uint64(2), testEthAddrs[0], common.Big0, 21000, big.NewInt(testLowFeeConfig.MinBaseFee.Int64()), nil)
	signedTx, err = types.SignTx(tx, types.LatestSigner(genesis.Config), testKeys[0])
	require.NoError(t, err)
	errs = restartedVM.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.NoError(t, errs[0])
	// let some time pass for block gas cost and fees to be updated
	restartedVM.clock.Set(restartedVM.clock.Time().Add(time.Hour * 10))
	blk = issueAndAccept(t, issuer, restartedVM)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))

	// Regression: Mempool should see the new config after restart
	restartedVM, err = restartVM(restartedVM, sharedDB, genesisJSON, issuer, appSender, true)
	require.NoError(t, err)
	newTxPoolHeadChan = make(chan core.NewTxPoolReorgEvent, 1)
	restartedVM.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)
	// send a tx with low fee
	tx = types.NewTransaction(uint64(3), testEthAddrs[0], common.Big0, 21000, big.NewInt(testLowFeeConfig.MinBaseFee.Int64()), nil)
	signedTx, err = types.SignTx(tx, types.LatestSigner(genesis.Config), testKeys[0])
	require.NoError(t, err)
	errs = restartedVM.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.NoError(t, errs[0])
	blk = issueAndAccept(t, issuer, restartedVM)
	newHead = <-newTxPoolHeadChan
	require.Equal(t, newHead.Head.Hash(), common.Hash(blk.ID()))
}

func restartVM(vm *VM, sharedDB database.Database, genesisBytes []byte, issuer chan commonEng.Message, appSender commonEng.AppSender, finishBootstrapping bool) (*VM, error) {
	vm.Shutdown(context.Background())
	restartedVM := &VM{}
	vm.ctx.Metrics = metrics.NewPrefixGatherer()
	err := restartedVM.Initialize(context.Background(), vm.ctx, sharedDB, genesisBytes, nil, nil, issuer, []*commonEng.Fx{}, appSender)
	if err != nil {
		return nil, err
	}

	if finishBootstrapping {
		err = restartedVM.SetState(context.Background(), snow.Bootstrapping)
		if err != nil {
			return nil, err
		}
		err = restartedVM.SetState(context.Background(), snow.NormalOp)
		if err != nil {
			return nil, err
		}
	}
	return restartedVM, nil
}
