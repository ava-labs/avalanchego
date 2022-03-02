// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/subnet-evm/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/chain"

	engCommon "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ava-labs/subnet-evm/consensus/dummy"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/eth"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/rpc"

	accountKeystore "github.com/ava-labs/subnet-evm/accounts/keystore"
)

var (
	testNetworkID   uint32 = 10
	testCChainID           = ids.ID{'c', 'c', 'h', 'a', 'i', 'n', 't', 'e', 's', 't'}
	testXChainID           = ids.ID{'t', 'e', 's', 't', 'x'}
	testMinGasPrice int64  = 225_000_000_000
	testKeys        []*ecdsa.PrivateKey
	testEthAddrs    []common.Address // testEthAddrs[i] corresponds to testKeys[i]
	testAvaxAssetID = ids.ID{1, 2, 3}
	username        = "Johns"
	password        = "CjasdjhiPeirbSenfeI13" // #nosec G101
	// Use chainId: 43111, so that it does not overlap with any Avalanche ChainIDs, which may have their
	// config overridden in vm.Initialize.
	genesisJSONMuirGlacier = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0x71562b71999873DB5b286dF957af199Ec94617F7\": {\"balance\":\"0x4192927743b88000\"}, \"0x703c4b2bD70c169f5717101CaeE543299Fc946C7\": {\"balance\":\"0x4192927743b88000\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	genesisJSONSubnetEVM   = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"subnetEVMTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0x71562b71999873DB5b286dF957af199Ec94617F7\": {\"balance\":\"0x4192927743b88000\"}, \"0x703c4b2bD70c169f5717101CaeE543299Fc946C7\": {\"balance\":\"0x4192927743b88000\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	firstTxAmount          *big.Int
	genesisBalance         *big.Int
)

func init() {
	key1, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	key2, _ := crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	testKeys = append(testKeys, key1, key2)
	addr1 := crypto.PubkeyToAddress(key1.PublicKey)
	addr2 := crypto.PubkeyToAddress(key2.PublicKey)
	testEthAddrs = append(testEthAddrs, addr1, addr2)

	minBlockTime = time.Millisecond
	maxBlockTime = time.Millisecond
	firstTxAmount = new(big.Int).Mul(big.NewInt(testMinGasPrice), big.NewInt(21000*100))
	genesisBalance = new(big.Int).Mul(big.NewInt(testMinGasPrice), big.NewInt(21000*1000))
}

// BuildGenesisTest returns the genesis bytes for Subnet EVM VM to be used in testing
func buildGenesisTest(t *testing.T, genesisJSON string) []byte {
	ss := CreateStaticService()

	genesis := &core.Genesis{}
	if err := json.Unmarshal([]byte(genesisJSON), genesis); err != nil {
		t.Fatalf("Problem unmarshaling genesis JSON: %s", err)
	}
	args := &BuildGenesisArgs{GenesisData: genesis}
	reply := &BuildGenesisReply{}
	err := ss.BuildGenesis(nil, args, reply)
	if err != nil {
		t.Fatalf("Failed to create test genesis")
	}
	genesisBytes, err := formatting.Decode(reply.Encoding, reply.GenesisBytes)
	if err != nil {
		t.Fatalf("Failed to decode genesis bytes: %s", err)
	}
	return genesisBytes
}

func NewContext() *snow.Context {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = testNetworkID
	ctx.ChainID = testCChainID
	ctx.AVAXAssetID = testAvaxAssetID
	ctx.XChainID = testXChainID
	aliaser := ctx.BCLookup.(ids.Aliaser)
	_ = aliaser.Alias(testCChainID, "C")
	_ = aliaser.Alias(testCChainID, testCChainID.String())
	_ = aliaser.Alias(testXChainID, "X")
	_ = aliaser.Alias(testXChainID, testXChainID.String())
	ctx.SNLookup = &snLookup{
		chainsToSubnet: map[ids.ID]ids.ID{
			constants.PlatformChainID: constants.PrimaryNetworkID,
			testXChainID:              constants.PrimaryNetworkID,
			testCChainID:              constants.PrimaryNetworkID,
		},
	}
	return ctx
}

type snLookup struct {
	chainsToSubnet map[ids.ID]ids.ID
}

func (sn *snLookup) SubnetID(chainID ids.ID) (ids.ID, error) {
	subnetID, ok := sn.chainsToSubnet[chainID]
	if !ok {
		return ids.ID{}, errors.New("unknown chain")
	}
	return subnetID, nil
}

func setupGenesis(t *testing.T,
	genesisJSON string,
) (*snow.Context,
	manager.Manager,
	[]byte,
	chan engCommon.Message) {
	genesisBytes := buildGenesisTest(t, genesisJSON)
	ctx := NewContext()

	baseDBManager := manager.NewMemDB(version.NewDefaultVersion(1, 4, 5))

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	userKeystore := keystore.New(logging.NoLog{}, manager.NewMemDB(version.NewDefaultVersion(1, 4, 5)))
	if err := userKeystore.CreateUser(username, password); err != nil {
		t.Fatal(err)
	}
	ctx.Keystore = userKeystore.NewBlockchainKeyStore(ctx.ChainID)

	issuer := make(chan engCommon.Message, 1)
	prefixedDBManager := baseDBManager.NewPrefixDBManager([]byte{1})
	return ctx, prefixedDBManager, genesisBytes, issuer
}

// GenesisVM creates a VM instance with the genesis test bytes and returns
// the channel use to send messages to the engine, the vm, and atomic memory
func GenesisVM(t *testing.T,
	finishBootstrapping bool,
	genesisJSON string,
	configJSON string,
	upgradeJSON string,
) (chan engCommon.Message,
	*VM, manager.Manager,
	*engCommon.SenderTest) {
	vm := &VM{}
	ctx, dbManager, genesisBytes, issuer := setupGenesis(t, genesisJSON)
	appSender := &engCommon.SenderTest{}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func([]byte) error { return nil }
	if err := vm.Initialize(
		ctx,
		dbManager,
		genesisBytes,
		[]byte(upgradeJSON),
		[]byte(configJSON),
		issuer,
		[]*engCommon.Fx{},
		appSender,
	); err != nil {
		t.Fatal(err)
	}

	if finishBootstrapping {
		assert.NoError(t, vm.SetState(snow.Bootstrapping))
		assert.NoError(t, vm.SetState(snow.NormalOp))
	}

	return issuer, vm, dbManager, appSender
}

func TestVMConfig(t *testing.T) {
	txFeeCap := float64(11)
	enabledEthAPIs := []string{"internal-private-debug"}
	configJSON := fmt.Sprintf("{\"rpc-tx-fee-cap\": %g,\"eth-apis\": %s}", txFeeCap, fmt.Sprintf("[%q]", enabledEthAPIs[0]))
	_, vm, _, _ := GenesisVM(t, false, genesisJSONMuirGlacier, configJSON, "")
	assert.Equal(t, vm.config.RPCTxFeeCap, txFeeCap, "Tx Fee Cap should be set")
	assert.Equal(t, vm.config.EthAPIs(), enabledEthAPIs, "EnabledEthAPIs should be set")
	assert.NoError(t, vm.Shutdown())
}

func TestVMConfigDefaults(t *testing.T) {
	txFeeCap := float64(11)
	enabledEthAPIs := []string{"internal-private-debug"}
	configJSON := fmt.Sprintf("{\"rpc-tx-fee-cap\": %g,\"eth-apis\": %s}", txFeeCap, fmt.Sprintf("[%q]", enabledEthAPIs[0]))
	_, vm, _, _ := GenesisVM(t, false, genesisJSONMuirGlacier, configJSON, "")

	var vmConfig Config
	vmConfig.SetDefaults()
	vmConfig.RPCTxFeeCap = txFeeCap
	vmConfig.EnabledEthAPIs = enabledEthAPIs
	assert.Equal(t, vmConfig, vm.config, "VM Config should match default with overrides")
	assert.NoError(t, vm.Shutdown())
}

func TestVMNilConfig(t *testing.T) {
	_, vm, _, _ := GenesisVM(t, false, genesisJSONMuirGlacier, "", "")

	// VM Config should match defaults if no config is passed in
	var vmConfig Config
	vmConfig.SetDefaults()
	assert.Equal(t, vmConfig, vm.config, "VM Config should match default config")
	assert.NoError(t, vm.Shutdown())
}

func TestVMContinuosProfiler(t *testing.T) {
	profilerDir := t.TempDir()
	profilerFrequency := 500 * time.Millisecond
	configJSON := fmt.Sprintf("{\"continuous-profiler-dir\": %q,\"continuous-profiler-frequency\": \"500ms\"}", profilerDir)
	_, vm, _, _ := GenesisVM(t, false, genesisJSONMuirGlacier, configJSON, "")
	assert.Equal(t, vm.config.ContinuousProfilerDir, profilerDir, "profiler dir should be set")
	assert.Equal(t, vm.config.ContinuousProfilerFrequency.Duration, profilerFrequency, "profiler frequency should be set")

	// Sleep for twice the frequency of the profiler to give it time
	// to generate the first profile.
	time.Sleep(2 * time.Second)
	assert.NoError(t, vm.Shutdown())

	// Check that the first profile was generated
	expectedFileName := filepath.Join(profilerDir, "cpu.profile.1")
	_, err := os.Stat(expectedFileName)
	assert.NoError(t, err, "Expected continuous profiler to generate the first CPU profile at %s", expectedFileName)
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
	}
	for _, test := range genesisTests {
		t.Run(test.name, func(t *testing.T) {
			_, vm, _, _ := GenesisVM(t, true, test.genesis, "", "")

			if gasPrice := vm.chain.GetTxPool().GasPrice(); gasPrice.Cmp(test.expectedGasPrice) != 0 {
				t.Fatalf("Expected pool gas price to be %d but found %d", test.expectedGasPrice, gasPrice)
			}
			defer func() {
				shutdownChan := make(chan error, 1)
				shutdownFunc := func() {
					err := vm.Shutdown()
					shutdownChan <- err
				}

				go shutdownFunc()
				shutdownTimeout := 50 * time.Millisecond
				ticker := time.NewTicker(shutdownTimeout)
				select {
				case <-ticker.C:
					t.Fatalf("VM shutdown took longer than timeout: %v", shutdownTimeout)
				case err := <-shutdownChan:
					if err != nil {
						t.Fatalf("Shutdown errored: %s", err)
					}
				}
			}()

			lastAcceptedID, err := vm.LastAccepted()
			if err != nil {
				t.Fatal(err)
			}

			if lastAcceptedID != ids.ID(vm.genesisHash) {
				t.Fatal("Expected last accepted block to match the genesis block hash")
			}

			genesisBlk, err := vm.GetBlock(lastAcceptedID)
			if err != nil {
				t.Fatalf("Failed to get genesis block due to %s", err)
			}

			if height := genesisBlk.Height(); height != 0 {
				t.Fatalf("Expected height of geneiss block to be 0, found: %d", height)
			}

			if _, err := vm.ParseBlock(genesisBlk.Bytes()); err != nil {
				t.Fatalf("Failed to parse genesis block due to %s", err)
			}

			genesisStatus := genesisBlk.Status()
			if genesisStatus != choices.Accepted {
				t.Fatalf("expected genesis status to be %s but was %s", choices.Accepted, genesisStatus)
			}
		})
	}
}

func TestBuildEthTxBlock(t *testing.T) {
	// reduce block gas cost
	issuer, vm, dbManager, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "{\"pruning-enabled\":true}", "")

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}
	errs := vm.chain.AddRemoteTxsSync([]*types.Transaction{signedTx})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blk1, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk1.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk1.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm.SetPreference(blk1.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk1.Accept(); err != nil {
		t.Fatal(err)
	}

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
	errs = vm.chain.AddRemoteTxsSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	time.Sleep(2 * time.Second)

	<-issuer

	blk2, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk2.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk2.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := blk2.Accept(); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk2.ID()) {
		t.Fatalf("Expected new block to match")
	}

	if status := blk2.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID, err := vm.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk2.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)
	}

	ethBlk1 := blk1.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if ethBlk1Root := ethBlk1.Root(); !vm.chain.BlockChain().HasState(ethBlk1Root) {
		t.Fatalf("Expected blk1 state root to not yet be pruned after blk2 was accepted because of tip buffer")
	}

	// Clear the cache and ensure that GetBlock returns internal blocks with the correct status
	vm.State.Flush()
	blk2Refreshed, err := vm.GetBlockInternal(blk2.ID())
	if err != nil {
		t.Fatal(err)
	}
	if status := blk2Refreshed.Status(); status != choices.Accepted {
		t.Fatalf("Expected refreshed blk2 to be Accepted, but found status: %s", status)
	}

	blk1RefreshedID := blk2Refreshed.Parent()
	blk1Refreshed, err := vm.GetBlockInternal(blk1RefreshedID)
	if err != nil {
		t.Fatal(err)
	}
	if status := blk1Refreshed.Status(); status != choices.Accepted {
		t.Fatalf("Expected refreshed blk1 to be Accepted, but found status: %s", status)
	}

	if blk1Refreshed.ID() != blk1.ID() {
		t.Fatalf("Found unexpected blkID for parent of blk2")
	}

	restartedVM := &VM{}
	genesisBytes := buildGenesisTest(t, genesisJSONSubnetEVM)

	if err := restartedVM.Initialize(
		NewContext(),
		dbManager,
		genesisBytes,
		[]byte(""),
		[]byte("{\"pruning-enabled\":true}"),
		issuer,
		[]*engCommon.Fx{},
		nil,
	); err != nil {
		t.Fatal(err)
	}

	// State root should not have been committed and discarded on restart
	if ethBlk1Root := ethBlk1.Root(); restartedVM.chain.BlockChain().HasState(ethBlk1Root) {
		t.Fatalf("Expected blk1 state root to be pruned after blk2 was accepted on top of it in pruning mode")
	}

	// State root should be committed when accepted tip on shutdown
	ethBlk2 := blk2.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if ethBlk2Root := ethBlk2.Root(); !restartedVM.chain.BlockChain().HasState(ethBlk2Root) {
		t.Fatalf("Expected blk2 state root to not be pruned after shutdown (last accepted tip should be committed)")
	}
}

// Regression test to ensure that after accepting block A
// then calling SetPreference on block B (when it becomes preferred)
// and the head of a longer chain (block D) does not corrupt the
// canonical chain.
//  A
// / \
// B  C
//    |
//    D
func TestSetPreferenceRace(t *testing.T) {
	// Create two VMs which will agree on block A and then
	// build the two distinct preferred chains above
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "{\"pruning-enabled\":true}", "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "{\"pruning-enabled\":true}", "")

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm1.chain.AddRemoteTxsSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := vm2.ParseBlock(vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
	}
	if err := vm2.SetPreference(vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if err := vm2BlkA.Accept(); err != nil {
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
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.chain.AddRemoteTxsSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	errs = vm2.chain.AddRemoteTxsSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkC, err := vm2.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if status := vm2BlkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block C to be %s, but found %s", choices.Processing, status)
	}

	if err := vm2.SetPreference(vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Block D
	errs = vm2.chain.AddRemoteTxsSync(txs[5:10])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkD, err := vm2.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	if err := vm2BlkD.Verify(); err != nil {
		t.Fatalf("BlkD failed verification on VM2: %s", err)
	}

	if status := vm2BlkD.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block D to be %s, but found %s", choices.Processing, status)
	}

	if err := vm2.SetPreference(vm2BlkD.ID()); err != nil {
		t.Fatal(err)
	}

	// VM1 receives blkC and blkD from VM1
	// and happens to call SetPreference on blkD without ever calling SetPreference
	// on blkC
	// Here we parse them in reverse order to simulate receiving a chain from the tip
	// back to the last accepted block as would typically be the case in the consensus
	// engine
	vm1BlkD, err := vm1.ParseBlock(vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("VM1 errored parsing blkD: %s", err)
	}
	vm1BlkC, err := vm1.ParseBlock(vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("VM1 errored parsing blkC: %s", err)
	}

	// The blocks must be verified in order. This invariant is maintained
	// in the consensus engine.
	if err := vm1BlkC.Verify(); err != nil {
		t.Fatalf("VM1 BlkC failed verification: %s", err)
	}
	if err := vm1BlkD.Verify(); err != nil {
		t.Fatalf("VM1 BlkD failed verification: %s", err)
	}

	// Set VM1's preference to blockD, skipping blockC
	if err := vm1.SetPreference(vm1BlkD.ID()); err != nil {
		t.Fatal(err)
	}

	// Accept the longer chain on both VMs and ensure there are no errors
	// VM1 Accepts the blocks in order
	if err := vm1BlkC.Accept(); err != nil {
		t.Fatalf("VM1 BlkC failed on accept: %s", err)
	}
	if err := vm1BlkD.Accept(); err != nil {
		t.Fatalf("VM1 BlkC failed on accept: %s", err)
	}

	// VM2 Accepts the blocks in order
	if err := vm2BlkC.Accept(); err != nil {
		t.Fatalf("VM2 BlkC failed on accept: %s", err)
	}
	if err := vm2BlkD.Accept(); err != nil {
		t.Fatalf("VM2 BlkC failed on accept: %s", err)
	}

	log.Info("Validating canonical chain")
	// Verify the Canonical Chain for Both VMs
	if err := vm2.chain.ValidateCanonicalChain(); err != nil {
		t.Fatalf("VM2 failed canonical chain verification due to: %s", err)
	}

	if err := vm1.chain.ValidateCanonicalChain(); err != nil {
		t.Fatalf("VM1 failed canonical chain verification due to: %s", err)
	}
}

// Regression test to ensure that a VM that accepts block A and B
// will not attempt to orphan either when verifying blocks C and D
// from another VM (which have a common ancestor under the finalized
// frontier).
//   A
//  / \
// B   C
//
// verifies block B and C, then Accepts block B. Then we test to ensure
// that the VM defends against any attempt to set the preference or to
// accept block C, which should be an orphaned block at this point and
// get rejected.
func TestReorgProtection(t *testing.T) {
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "{\"pruning-enabled\":false}", "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "{\"pruning-enabled\":false}", "")

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm1.chain.AddRemoteTxsSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := vm2.ParseBlock(vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
	}
	if err := vm2.SetPreference(vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if err := vm2BlkA.Accept(); err != nil {
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
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.chain.AddRemoteTxsSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	errs = vm2.chain.AddRemoteTxsSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkC, err := vm2.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if status := vm2BlkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
	}

	vm1BlkC, err := vm1.ParseBlock(vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	if err := vm1BlkC.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	// Accept B, such that block C should get Rejected.
	if err := vm1BlkB.Accept(); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}

	// The below (setting preference blocks that have a common ancestor
	// with the preferred chain lower than the last finalized block)
	// should NEVER happen. However, the VM defends against this
	// just in case.
	if err := vm1.SetPreference(vm1BlkC.ID()); !strings.Contains(err.Error(), "cannot orphan finalized block") {
		t.Fatalf("Unexpected error when setting preference that would trigger reorg: %s", err)
	}

	if err := vm1BlkC.Accept(); !strings.Contains(err.Error(), "expected accepted block to have parent") {
		t.Fatalf("Unexpected error when setting block at finalized height: %s", err)
	}
}

// Regression test to ensure that a VM that accepts block C while preferring
// block B will trigger a reorg.
//   A
//  / \
// B   C
func TestNonCanonicalAccept(t *testing.T) {
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "", "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "", "")

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm1.chain.AddRemoteTxsSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := vm2.ParseBlock(vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
	}
	if err := vm2.SetPreference(vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if err := vm2BlkA.Accept(); err != nil {
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
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.chain.AddRemoteTxsSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	vm1.chain.BlockChain().GetVMConfig().AllowUnfinalizedQueries = true

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}

	errs = vm2.chain.AddRemoteTxsSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkC, err := vm2.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	vm1BlkC, err := vm1.ParseBlock(vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	if err := vm1BlkC.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if err := vm1BlkC.Accept(); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkCHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	}
}

// Regression test to ensure that a VM that verifies block B, C, then
// D (preferring block B) does not trigger a reorg through the re-verification
// of block C or D.
//   A
//  / \
// B   C
//     |
//     D
func TestStickyPreference(t *testing.T) {
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "", "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "", "")

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm1.chain.AddRemoteTxsSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := vm2.ParseBlock(vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
	}
	if err := vm2.SetPreference(vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if err := vm2BlkA.Accept(); err != nil {
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
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.chain.AddRemoteTxsSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	vm1.chain.BlockChain().GetVMConfig().AllowUnfinalizedQueries = true

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}

	errs = vm2.chain.AddRemoteTxsSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkC, err := vm2.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if status := vm2BlkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block C to be %s, but found %s", choices.Processing, status)
	}

	if err := vm2.SetPreference(vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	errs = vm2.chain.AddRemoteTxsSync(txs[5:])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkD, err := vm2.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	// Parse blocks produced in vm2
	vm1BlkC, err := vm1.ParseBlock(vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()

	vm1BlkD, err := vm1.ParseBlock(vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	blkDHeight := vm1BlkD.Height()
	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()

	// Should be no-ops
	if err := vm1BlkC.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.chain.GetBlockByNumber(blkDHeight); b != nil {
		t.Fatalf("expected block at %d to be nil but got %s", blkDHeight, b.Hash().Hex())
	}
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	// Should still be no-ops on re-verify
	if err := vm1BlkC.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.chain.GetBlockByNumber(blkDHeight); b != nil {
		t.Fatalf("expected block at %d to be nil but got %s", blkDHeight, b.Hash().Hex())
	}
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	// Should be queryable after setting preference to side chain
	if err := vm1.SetPreference(vm1BlkD.ID()); err != nil {
		t.Fatal(err)
	}

	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkCHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.chain.GetBlockByNumber(blkDHeight); b.Hash() != blkDHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkDHeight, blkDHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkDHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
	}

	// Attempt to accept out of order
	if err := vm1BlkD.Accept(); !strings.Contains(err.Error(), "expected accepted block to have parent") {
		t.Fatalf("unexpected error when accepting out of order block: %s", err)
	}

	// Accept in order
	if err := vm1BlkC.Accept(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Accept(); err != nil {
		t.Fatalf("Block failed acceptance on VM1: %s", err)
	}

	// Ensure queryable after accepting
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkCHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.chain.GetBlockByNumber(blkDHeight); b.Hash() != blkDHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkDHeight, blkDHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkDHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
	}
}

// Regression test to ensure that a VM that prefers block B is able to parse
// block C but unable to parse block D because it names B as an uncle, which
// are not supported.
//   A
//  / \
// B   C
//     |
//     D
func TestUncleBlock(t *testing.T) {
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "", "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "", "")

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm1.chain.AddRemoteTxsSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := vm2.ParseBlock(vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
	}
	if err := vm2.SetPreference(vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if err := vm2BlkA.Accept(); err != nil {
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
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	errs = vm1.chain.AddRemoteTxsSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	errs = vm2.chain.AddRemoteTxsSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkC, err := vm2.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if status := vm2BlkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block C to be %s, but found %s", choices.Processing, status)
	}

	if err := vm2.SetPreference(vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	errs = vm2.chain.AddRemoteTxsSync(txs[5:10])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkD, err := vm2.BuildBlock()
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
		new(trie.Trie),
	)
	uncleBlock := &Block{
		vm:       vm2,
		ethBlock: uncleEthBlock,
		id:       ids.ID(uncleEthBlock.Hash()),
	}
	if err := uncleBlock.Verify(); !errors.Is(err, errUnclesUnsupported) {
		t.Fatalf("VM2 should have failed with %q but got %q", errUnclesUnsupported, err.Error())
	}
	if _, err := vm1.ParseBlock(vm2BlkC.Bytes()); err != nil {
		t.Fatalf("VM1 errored parsing blkC: %s", err)
	}
	if _, err := vm1.ParseBlock(uncleBlock.Bytes()); !errors.Is(err, errUnclesUnsupported) {
		t.Fatalf("VM1 should have failed with %q but got %q", errUnclesUnsupported, err.Error())
	}
}

// Regression test to ensure that a VM that is not able to parse a block that
// contains no transactions.
func TestEmptyBlock(t *testing.T) {
	issuer, vm, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "", "")

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm.chain.AddRemoteTxsSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blk, err := vm.BuildBlock()
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

	emptyBlock := &Block{
		vm:       vm,
		ethBlock: emptyEthBlock,
		id:       ids.ID(emptyEthBlock.Hash()),
	}

	if _, err := vm.ParseBlock(emptyBlock.Bytes()); !errors.Is(err, errEmptyBlock) {
		t.Fatalf("VM should have failed with errEmptyBlock but got %s", err.Error())
	}
	if err := emptyBlock.Verify(); !errors.Is(err, errEmptyBlock) {
		t.Fatalf("block should have failed verification with errEmptyBlock but got %s", err.Error())
	}
}

// Regression test to ensure that a VM that verifies block B, C, then
// D (preferring block B) reorgs when C and then D are accepted.
//   A
//  / \
// B   C
//     |
//     D
func TestAcceptReorg(t *testing.T) {
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "", "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "", "")

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan2)
	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm1.chain.AddRemoteTxsSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := vm2.ParseBlock(vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
	}
	if err := vm2.SetPreference(vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if err := vm2BlkA.Accept(); err != nil {
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
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainConfig.ChainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference
	// for block B
	errs := vm1.chain.AddRemoteTxsSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	errs = vm2.chain.AddRemoteTxsSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2

	vm2BlkC, err := vm2.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if err := vm2.SetPreference(vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	errs = vm2.chain.AddRemoteTxsSync(txs[5:])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2

	vm2BlkD, err := vm2.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	// Parse blocks produced in vm2
	vm1BlkC, err := vm1.ParseBlock(vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	vm1BlkD, err := vm1.ParseBlock(vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	if err := vm1BlkC.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	if err := vm1BlkC.Accept(); err != nil {
		t.Fatal(err)
	}

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkCHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkCHash.Hex(), b.Hash().Hex())
	}

	if err := vm1BlkB.Reject(); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkD.Accept(); err != nil {
		t.Fatal(err)
	}

	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkDHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
	}
}

func TestFutureBlock(t *testing.T) {
	issuer, vm, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "", "")

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm.chain.AddRemoteTxsSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blkA, err := vm.BuildBlock()
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
		new(trie.Trie),
	)

	futureBlock := &Block{
		vm:       vm,
		ethBlock: modifiedBlock,
		id:       ids.ID(modifiedBlock.Hash()),
	}

	if err := futureBlock.Verify(); err == nil {
		t.Fatal("Future block should have failed verification due to block timestamp too far in the future")
	} else if !strings.Contains(err.Error(), "block timestamp is too far in the future") {
		t.Fatalf("Expected error to be block timestamp too far in the future but found %s", err)
	}
}

func TestLastAcceptedBlockNumberAllow(t *testing.T) {
	issuer, vm, _, _ := GenesisVM(t, true, genesisJSONMuirGlacier, "", "")

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tx := types.NewTransaction(uint64(0), key.Address, firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm.chain.AddRemoteTxsSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM: %s", err)
	}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm.SetPreference(blk.ID()); err != nil {
		t.Fatal(err)
	}

	blkHeight := blk.Height()
	blkHash := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()

	vm.chain.BlockChain().GetVMConfig().AllowUnfinalizedQueries = true

	ctx := context.Background()
	b, err := vm.chain.APIBackend().BlockByNumber(ctx, rpc.BlockNumber(blkHeight))
	if err != nil {
		t.Fatal(err)
	}
	if b.Hash() != blkHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())
	}

	vm.chain.BlockChain().GetVMConfig().AllowUnfinalizedQueries = false

	_, err = vm.chain.APIBackend().BlockByNumber(ctx, rpc.BlockNumber(blkHeight))
	if !errors.Is(err, eth.ErrUnfinalizedData) {
		t.Fatalf("expected ErrUnfinalizedData but got %s", err.Error())
	}

	if err := blk.Accept(); err != nil {
		t.Fatalf("VM failed to accept block: %s", err)
	}

	if b := vm.chain.GetBlockByNumber(blkHeight); b.Hash() != blkHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())
	}
}

func TestConfigureLogLevel(t *testing.T) {
	configTests := []struct {
		name                     string
		logConfig                string
		genesisJSON, upgradeJSON string
		expectedErr              string
	}{
		{
			name:        "Log level info",
			logConfig:   "{\"log-level\": \"info\"}",
			genesisJSON: genesisJSONSubnetEVM,
			upgradeJSON: "",
			expectedErr: "",
		},
	}
	for _, test := range configTests {
		t.Run(test.name, func(t *testing.T) {
			vm := &VM{}
			ctx, dbManager, genesisBytes, issuer := setupGenesis(t, test.genesisJSON)
			appSender := &engCommon.SenderTest{}
			appSender.CantSendAppGossip = true
			appSender.SendAppGossipF = func([]byte) error { return nil }
			err := vm.Initialize(
				ctx,
				dbManager,
				genesisBytes,
				[]byte(""),
				[]byte(test.logConfig),
				issuer,
				[]*engCommon.Fx{},
				appSender,
			)
			if len(test.expectedErr) == 0 && err != nil {
				t.Fatal(err)
			} else if len(test.expectedErr) > 0 {
				if err == nil {
					t.Fatalf("initialize should have failed due to %s", test.expectedErr)
				} else if !strings.Contains(err.Error(), test.expectedErr) {
					t.Fatalf("Expected initialize to fail due to %s, but failed with %s", test.expectedErr, err.Error())
				}
			}

			// If the VM was not initialized, do not attept to shut it down
			if err == nil {
				shutdownChan := make(chan error, 1)
				shutdownFunc := func() {
					err := vm.Shutdown()
					shutdownChan <- err
				}
				go shutdownFunc()

				shutdownTimeout := 50 * time.Millisecond
				ticker := time.NewTicker(shutdownTimeout)
				select {
				case <-ticker.C:
					t.Fatalf("VM shutdown took longer than timeout: %v", shutdownTimeout)
				case err := <-shutdownChan:
					if err != nil {
						t.Fatalf("Shutdown errored: %s", err)
					}
				}
			}
		})
	}
}

// Regression test to ensure we can build blocks if we are starting with the
// Subnet EVM ruleset in genesis.
func TestBuildSubnetEVMBlock(t *testing.T) {
	issuer, vm, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tx := types.NewTransaction(uint64(0), key.Address, new(big.Int).Mul(firstTxAmount, big.NewInt(4)), 21000, big.NewInt(testMinGasPrice*3), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	txErrors := vm.chain.AddRemoteTxsSync([]*types.Transaction{signedTx})
	for i, err := range txErrors {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm.SetPreference(blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, big.NewInt(testMinGasPrice*3), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := vm.chain.AddRemoteTxs(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blk, err = vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	ethBlk := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if ethBlk.BlockGasCost() == nil || ethBlk.BlockGasCost().Cmp(big.NewInt(100)) < 0 {
		t.Fatalf("expected blockGasCost to be at least 100 but got %d", ethBlk.BlockGasCost())
	}
	minRequiredTip, err := dummy.MinRequiredTip(vm.chainConfig, ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(big.NewInt(0.05*params.GWei)) < 0 {
		t.Fatalf("expected minRequiredTip to be at least 0.05 gwei but got %d", minRequiredTip)
	}

	if status := blk.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID, err := vm.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}

	// Confirm all txs are present
	ethBlkTxs := vm.chain.GetBlockByNumber(2).Transactions()
	for i, tx := range txs {
		if len(ethBlkTxs) <= i {
			t.Fatalf("missing transactions expected: %d but found: %d", len(txs), len(ethBlkTxs))
		}
		if ethBlkTxs[i].Hash() != tx.Hash() {
			t.Fatalf("expected tx at index %d to have hash: %x but has: %x", i, txs[i].Hash(), tx.Hash())
		}
	}
}
