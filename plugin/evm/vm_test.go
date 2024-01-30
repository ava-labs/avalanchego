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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	avalancheConstants "github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/chain"

	"github.com/ava-labs/subnet-evm/accounts/abi"
	accountKeystore "github.com/ava-labs/subnet-evm/accounts/keystore"
	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/consensus/dummy"
	"github.com/ava-labs/subnet-evm/constants"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/txpool"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/eth"
	"github.com/ava-labs/subnet-evm/internal/ethapi"
	"github.com/ava-labs/subnet-evm/metrics"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/contracts/deployerallowlist"
	"github.com/ava-labs/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/subnet-evm/rpc"
	"github.com/ava-labs/subnet-evm/trie"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/ava-labs/subnet-evm/vmerrs"

	avagoconstants "github.com/ava-labs/avalanchego/utils/constants"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	testNetworkID   uint32 = avagoconstants.UnitTestID
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
	genesisJSONSubnetEVM    = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"eip150Block\":0,\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"subnetEVMTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x7A1200\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0x71562b71999873DB5b286dF957af199Ec94617F7\": {\"balance\":\"0x4192927743b88000\"}, \"0x703c4b2bD70c169f5717101CaeE543299Fc946C7\": {\"balance\":\"0x4192927743b88000\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	genesisJSONDurango      = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"eip150Block\":0,\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"subnetEVMTimestamp\":0,\"durangoTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x7A1200\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0x71562b71999873DB5b286dF957af199Ec94617F7\": {\"balance\":\"0x4192927743b88000\"}, \"0x703c4b2bD70c169f5717101CaeE543299Fc946C7\": {\"balance\":\"0x4192927743b88000\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	genesisJSONPreSubnetEVM = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"eip150Block\":0,\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x7A1200\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0x71562b71999873DB5b286dF957af199Ec94617F7\": {\"balance\":\"0x4192927743b88000\"}, \"0x703c4b2bD70c169f5717101CaeE543299Fc946C7\": {\"balance\":\"0x4192927743b88000\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	genesisJSONLatest       = genesisJSONDurango

	firstTxAmount  = new(big.Int).Mul(big.NewInt(testMinGasPrice), big.NewInt(21000*100))
	genesisBalance = new(big.Int).Mul(big.NewInt(testMinGasPrice), big.NewInt(21000*1000))
)

func init() {
	key1, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	key2, _ := crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	testKeys = append(testKeys, key1, key2)
	addr1 := crypto.PubkeyToAddress(key1.PublicKey)
	addr2 := crypto.PubkeyToAddress(key2.PublicKey)
	testEthAddrs = append(testEthAddrs, addr1, addr2)
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
	ctx := utils.TestSnowContext()
	ctx.NodeID = ids.GenerateTestNodeID()
	ctx.NetworkID = testNetworkID
	ctx.ChainID = testCChainID
	ctx.AVAXAssetID = testAvaxAssetID
	ctx.XChainID = testXChainID
	aliaser := ctx.BCLookup.(ids.Aliaser)
	_ = aliaser.Alias(testCChainID, "C")
	_ = aliaser.Alias(testCChainID, testCChainID.String())
	_ = aliaser.Alias(testXChainID, "X")
	_ = aliaser.Alias(testXChainID, testXChainID.String())
	ctx.ValidatorState = &validators.TestState{
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			subnetID, ok := map[ids.ID]ids.ID{
				avalancheConstants.PlatformChainID: avalancheConstants.PrimaryNetworkID,
				testXChainID:                       avalancheConstants.PrimaryNetworkID,
				testCChainID:                       avalancheConstants.PrimaryNetworkID,
			}[chainID]
			if !ok {
				return ids.Empty, errors.New("unknown chain")
			}
			return subnetID, nil
		},
	}
	blsSecretKey, err := bls.NewSecretKey()
	if err != nil {
		panic(err)
	}
	ctx.WarpSigner = avalancheWarp.NewSigner(blsSecretKey, ctx.NetworkID, ctx.ChainID)
	ctx.PublicKey = bls.PublicFromSecretKey(blsSecretKey)
	return ctx
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
	genesisBytes := buildGenesisTest(t, genesisJSON)
	ctx := NewContext()

	baseDB := memdb.New()

	// initialize the atomic memory
	atomicMemory := atomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = atomicMemory.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	userKeystore := keystore.New(logging.NoLog{}, memdb.New())
	if err := userKeystore.CreateUser(username, password); err != nil {
		t.Fatal(err)
	}
	ctx.Keystore = userKeystore.NewBlockchainKeyStore(ctx.ChainID)

	issuer := make(chan commonEng.Message, 1)
	prefixedDB := prefixdb.New([]byte{1}, baseDB)
	return ctx, prefixedDB, genesisBytes, issuer, atomicMemory
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
) (chan commonEng.Message,
	*VM, database.Database,
	*commonEng.SenderTest,
) {
	vm := &VM{}
	ctx, dbManager, genesisBytes, issuer, _ := setupGenesis(t, genesisJSON)
	appSender := &commonEng.SenderTest{T: t}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func(context.Context, []byte) error { return nil }
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
	configJSON := fmt.Sprintf("{\"rpc-tx-fee-cap\": %g,\"eth-apis\": %s}", txFeeCap, fmt.Sprintf("[%q]", enabledEthAPIs[0]))
	_, vm, _, _ := GenesisVM(t, false, "", configJSON, "")
	require.Equal(t, vm.config.RPCTxFeeCap, txFeeCap, "Tx Fee Cap should be set")
	require.Equal(t, vm.config.EthAPIs(), enabledEthAPIs, "EnabledEthAPIs should be set")
	require.NoError(t, vm.Shutdown(context.Background()))
}

func TestVMConfigDefaults(t *testing.T) {
	txFeeCap := float64(11)
	enabledEthAPIs := []string{"debug"}
	configJSON := fmt.Sprintf("{\"rpc-tx-fee-cap\": %g,\"eth-apis\": %s}", txFeeCap, fmt.Sprintf("[%q]", enabledEthAPIs[0]))
	_, vm, _, _ := GenesisVM(t, false, "", configJSON, "")

	var vmConfig Config
	vmConfig.SetDefaults()
	vmConfig.RPCTxFeeCap = txFeeCap
	vmConfig.EnabledEthAPIs = enabledEthAPIs
	require.Equal(t, vmConfig, vm.config, "VM Config should match default with overrides")
	require.NoError(t, vm.Shutdown(context.Background()))
}

func TestVMNilConfig(t *testing.T) {
	_, vm, _, _ := GenesisVM(t, false, "", "", "")

	// VM Config should match defaults if no config is passed in
	var vmConfig Config
	vmConfig.SetDefaults()
	require.Equal(t, vmConfig, vm.config, "VM Config should match default config")
	require.NoError(t, vm.Shutdown(context.Background()))
}

func TestVMContinuousProfiler(t *testing.T) {
	profilerDir := t.TempDir()
	profilerFrequency := 500 * time.Millisecond
	configJSON := fmt.Sprintf("{\"continuous-profiler-dir\": %q,\"continuous-profiler-frequency\": \"500ms\"}", profilerDir)
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
	}
	for _, test := range genesisTests {
		t.Run(test.name, func(t *testing.T) {
			_, vm, _, _ := GenesisVM(t, true, test.genesis, "", "")

			if gasPrice := vm.txPool.GasPrice(); gasPrice.Cmp(test.expectedGasPrice) != 0 {
				t.Fatalf("Expected pool gas price to be %d but found %d", test.expectedGasPrice, gasPrice)
			}
			defer func() {
				shutdownChan := make(chan error, 1)
				shutdownFunc := func() {
					err := vm.Shutdown(context.Background())
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

			lastAcceptedID, err := vm.LastAccepted(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			if lastAcceptedID != ids.ID(vm.genesisHash) {
				t.Fatal("Expected last accepted block to match the genesis block hash")
			}

			genesisBlk, err := vm.GetBlock(context.Background(), lastAcceptedID)
			if err != nil {
				t.Fatalf("Failed to get genesis block due to %s", err)
			}

			if height := genesisBlk.Height(); height != 0 {
				t.Fatalf("Expected height of geneiss block to be 0, found: %d", height)
			}

			if _, err := vm.ParseBlock(context.Background(), genesisBlk.Bytes()); err != nil {
				t.Fatalf("Failed to parse genesis block due to %s", err)
			}

			genesisStatus := genesisBlk.Status()
			if genesisStatus != choices.Accepted {
				t.Fatalf("expected genesis status to be %s but was %s", choices.Accepted, genesisStatus)
			}
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

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
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
	issuer, vm, dbManager, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "{\"pruning-enabled\":true}", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

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

	if status := blk2.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
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
	if status := blk2Refreshed.Status(); status != choices.Accepted {
		t.Fatalf("Expected refreshed blk2 to be Accepted, but found status: %s", status)
	}

	blk1RefreshedID := blk2Refreshed.Parent()
	blk1Refreshed, err := vm.GetBlockInternal(context.Background(), blk1RefreshedID)
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
		context.Background(),
		NewContext(),
		dbManager,
		genesisBytes,
		[]byte(""),
		[]byte("{\"pruning-enabled\":true}"),
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
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "{\"pruning-enabled\":true}", "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "{\"pruning-enabled\":true}", "")

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

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
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
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
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

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
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

	if status := vm2BlkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block C to be %s, but found %s", choices.Processing, status)
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

	if status := vm2BlkD.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block D to be %s, but found %s", choices.Processing, status)
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
	issuer1, vm1, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "{\"pruning-enabled\":false}", "")
	issuer2, vm2, _, _ := GenesisVM(t, true, genesisJSONSubnetEVM, "{\"pruning-enabled\":false}", "")

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

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
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
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
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

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
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
	if status := vm2BlkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
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

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
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
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
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

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	vm1.eth.APIBackend.SetAllowUnfinalizedQueries(true)

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

	if err := vm1BlkC.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
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

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
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
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
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

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	vm1.eth.APIBackend.SetAllowUnfinalizedQueries(true)

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

	if status := vm2BlkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block C to be %s, but found %s", choices.Processing, status)
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

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
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
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
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

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
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

	if status := vm2BlkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block C to be %s, but found %s", choices.Processing, status)
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

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
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
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
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

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
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

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
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
			ctx, dbManager, genesisBytes, issuer, _ := setupGenesis(t, test.genesisJSON)
			appSender := &commonEng.SenderTest{T: t}
			appSender.CantSendAppGossip = true
			appSender.SendAppGossipF = func(context.Context, []byte) error { return nil }
			err := vm.Initialize(
				context.Background(),
				ctx,
				dbManager,
				genesisBytes,
				[]byte(""),
				[]byte(test.logConfig),
				issuer,
				[]*commonEng.Fx{},
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
					err := vm.Shutdown(context.Background())
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
	errs := vm.txPool.AddRemotes(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	blk = issueAndAccept(t, issuer, vm)
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
	genesis.Config.GenesisPrecompiles = params.Precompiles{
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
	genesis.Config.GenesisPrecompiles = params.Precompiles{
		txallowlist.ConfigKey: txallowlist.NewConfig(utils.NewUint64(0), testEthAddrs[0:1], nil, nil),
	}
	durangoTime := time.Now().Add(10 * time.Hour)
	genesis.Config.DurangoTimestamp = utils.TimeToNewUint64(durangoTime)
	genesisJSON, err := genesis.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	// prepare the new upgrade bytes to disable the TxAllowList
	disableAllowListTime := durangoTime.Add(10 * time.Hour)
	reenableAllowlistTime := disableAllowListTime.Add(10 * time.Hour)
	upgradeConfig := &params.UpgradeConfig{
		PrecompileUpgrades: []params.PrecompileUpgrade{
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
	if err := errs[0]; !errors.Is(err, vmerrs.ErrSenderAddressNotAllowListed) {
		t.Fatalf("expected ErrSenderAddressNotAllowListed, got: %s", err)
	}

	// Submit a rejected transaction, should throw an error because manager is not activated
	tx2 := types.NewTransaction(uint64(0), managerAddress, big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx2, err := types.SignTx(tx2, types.NewEIP155Signer(vm.chainConfig.ChainID), managerKey)
	require.NoError(t, err)

	errs = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx2})
	require.ErrorIs(t, errs[0], vmerrs.ErrSenderAddressNotAllowListed)

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
	genesis.Config.DurangoTimestamp = utils.TimeToNewUint64(durangoTimestamp)
	// this manager role should not be activated because DurangoTimestamp is in the future
	genesis.Config.GenesisPrecompiles = params.Precompiles{
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
	genesis.Config.DurangoTimestamp = utils.TimeToNewUint64(durangoTimestamp)
	genesisJSON, err = genesis.MarshalJSON()
	require.NoError(t, err)
	// use an invalid upgrade now with managers set before Durango
	upgradeConfig := &params.UpgradeConfig{
		PrecompileUpgrades: []params.PrecompileUpgrade{
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
	enableAllowListTimestamp := time.Unix(0, 0) // enable at genesis
	genesis.Config.GenesisPrecompiles = params.Precompiles{
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
	if err := errs[0]; !errors.Is(err, vmerrs.ErrSenderAddressNotAllowListed) {
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
	require.GreaterOrEqual(t, int64(block.Timestamp()), disableAllowListTimestamp.Unix())

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
	genesis.Config.GenesisPrecompiles = params.Precompiles{
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

	genesis.Config.FeeConfig = testLowFeeConfig
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

	// Contract is initialized but no state is given, reader should return genesis fee config
	feeConfig, lastChangedAt, err = vm.blockChain.GetFeeConfigAt(block.Header())
	require.NoError(t, err)
	require.EqualValues(t, testHighFeeConfig, feeConfig)
	require.EqualValues(t, vm.blockChain.CurrentBlock().Number, lastChangedAt)

	// should fail, with same params since fee is higher now
	tx2 := types.NewTx(&types.DynamicFeeTx{
		ChainID:   genesis.Config.ChainID,
		Nonce:     uint64(1),
		To:        &feemanager.ContractAddress,
		Gas:       genesis.Config.FeeConfig.GasLimit.Uint64(),
		Value:     common.Big0,
		GasFeeCap: testLowFeeConfig.MinBaseFee, // this is too low for applied config, should fail
		GasTipCap: common.Big0,
		Data:      data,
	})

	signedTx2, err := types.SignTx(tx2, types.LatestSigner(genesis.Config), testKeys[0])
	if err != nil {
		t.Fatal(err)
	}

	err = vm.txPool.AddRemote(signedTx2)
	require.ErrorIs(t, err, txpool.ErrUnderpriced)
}

// Test Allow Fee Recipients is disabled and, etherbase must be blackhole address
func TestAllowFeeRecipientDisabled(t *testing.T) {
	genesis := &core.Genesis{}
	if err := genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)); err != nil {
		t.Fatal(err)
	}
	genesis.Config.AllowFeeRecipients = false // set to false initially
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

	require.ErrorIs(t, modifiedBlk.Verify(context.Background()), vmerrs.ErrInvalidCoinbase)
}

func TestAllowFeeRecipientEnabled(t *testing.T) {
	genesis := &core.Genesis{}
	if err := genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)); err != nil {
		t.Fatal(err)
	}
	genesis.Config.AllowFeeRecipients = true
	genesisJSON, err := genesis.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	etherBase := common.HexToAddress("0x0123456789")
	c := Config{}
	c.SetDefaults()
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
	require.Equal(t, 1, balance.Cmp(common.Big0))
}

func TestRewardManagerPrecompileSetRewardAddress(t *testing.T) {
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)))

	genesis.Config.GenesisPrecompiles = params.Precompiles{
		rewardmanager.ConfigKey: rewardmanager.NewConfig(utils.NewUint64(0), testEthAddrs[0:1], nil, nil, nil),
	}
	genesis.Config.AllowFeeRecipients = true // enable this in genesis to test if this is recognized by the reward manager
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)

	etherBase := common.HexToAddress("0x0123456789") // give custom ether base
	c := Config{}
	c.SetDefaults()
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

	gas := 21000 + 240 + rewardmanager.SetRewardAddressGasCost // 21000 for tx, 240 for tx data

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
	require.Equal(t, 1, balance.Cmp(common.Big0))

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
	require.GreaterOrEqual(t, int64(ethBlock.Timestamp()), disableTime.Unix())

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
	require.GreaterOrEqual(t, int64(ethBlock.Timestamp()), disableTime.Unix())

	// Verify that Blackhole has received fees
	blkState, err = vm.blockChain.StateAt(ethBlock.Root())
	require.NoError(t, err)

	balance = blkState.GetBalance(etherBase)
	require.Equal(t, 1, balance.Cmp(previousBalance))
}

func TestRewardManagerPrecompileAllowFeeRecipients(t *testing.T) {
	genesis := &core.Genesis{}
	require.NoError(t, genesis.UnmarshalJSON([]byte(genesisJSONSubnetEVM)))

	genesis.Config.GenesisPrecompiles = params.Precompiles{
		rewardmanager.ConfigKey: rewardmanager.NewConfig(utils.NewUint64(0), testEthAddrs[0:1], nil, nil, nil),
	}
	genesis.Config.AllowFeeRecipients = false // disable this in genesis
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(t, err)
	etherBase := common.HexToAddress("0x0123456789") // give custom ether base
	c := Config{}
	c.SetDefaults()
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

	gas := 21000 + 240 + rewardmanager.AllowFeeRecipientsGasCost // 21000 for tx, 240 for tx data

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
	require.Equal(t, 1, balance.Cmp(common.Big0))

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
	require.GreaterOrEqual(t, int64(ethBlock.Timestamp()), disableTime.Unix())

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
	require.Greater(t, int64(ethBlock.Timestamp()), disableTime.Unix())

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
	// Hack: registering metrics uses global variables, so we need to disable metrics here so that we can initialize the VM twice.
	metrics.Enabled = false
	defer func() { metrics.Enabled = true }()

	issuer, vm, dbManager, appSender := GenesisVM(t, true, genesisJSONPreSubnetEVM, "{\"pruning-enabled\":true}", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

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
	require.NoError(t, json.Unmarshal([]byte(genesisJSONPreSubnetEVM), genesisWithUpgrade))
	genesisWithUpgrade.Config.SubnetEVMTimestamp = utils.TimeToNewUint64(blk.Timestamp())
	genesisWithUpgradeBytes, err := json.Marshal(genesisWithUpgrade)
	require.NoError(t, err)

	// this will not be allowed
	err = reinitVM.Initialize(context.Background(), vm.ctx, dbManager, genesisWithUpgradeBytes, []byte{}, []byte{}, issuer, []*commonEng.Fx{}, appSender)
	require.ErrorContains(t, err, "mismatching SubnetEVM fork block timestamp in database")

	// try again with skip-upgrade-check
	config := []byte("{\"skip-upgrade-check\": true}")
	err = reinitVM.Initialize(context.Background(), vm.ctx, dbManager, genesisWithUpgradeBytes, []byte{}, config, issuer, []*commonEng.Fx{}, appSender)
	require.NoError(t, err)
	require.NoError(t, reinitVM.Shutdown(context.Background()))
}

func TestCrossChainMessagestoVM(t *testing.T) {
	crossChainCodec := message.CrossChainCodec
	require := require.New(t)

	//  the following is based on this contract:
	//  contract T {
	//  	event received(address sender, uint amount, bytes memo);
	//  	event receivedAddr(address sender);
	//
	//  	function receive(bytes calldata memo) external payable returns (string memory res) {
	//  		emit received(msg.sender, msg.value, memo);
	//  		emit receivedAddr(msg.sender);
	//		return "hello world";
	//  	}
	//  }

	const abiBin = `0x608060405234801561001057600080fd5b506102a0806100206000396000f3fe60806040526004361061003b576000357c010000000000000000000000000000000000000000000000000000000090048063a69b6ed014610040575b600080fd5b6100b76004803603602081101561005657600080fd5b810190808035906020019064010000000081111561007357600080fd5b82018360208201111561008557600080fd5b803590602001918460018302840111640100000000831117156100a757600080fd5b9091929391929390505050610132565b6040518080602001828103825283818151815260200191508051906020019080838360005b838110156100f75780820151818401526020810190506100dc565b50505050905090810190601f1680156101245780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b60607f75fd880d39c1daf53b6547ab6cb59451fc6452d27caa90e5b6649dd8293b9eed33348585604051808573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001848152602001806020018281038252848482818152602001925080828437600081840152601f19601f8201169050808301925050509550505050505060405180910390a17f46923992397eac56cf13058aced2a1871933622717e27b24eabc13bf9dd329c833604051808273ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200191505060405180910390a16040805190810160405280600b81526020017f68656c6c6f20776f726c6400000000000000000000000000000000000000000081525090509291505056fea165627a7a72305820ff0c57dad254cfeda48c9cfb47f1353a558bccb4d1bc31da1dae69315772d29e0029`
	const abiJSON = `[ { "constant": false, "inputs": [ { "name": "memo", "type": "bytes" } ], "name": "receive", "outputs": [ { "name": "res", "type": "string" } ], "payable": true, "stateMutability": "payable", "type": "function" }, { "anonymous": false, "inputs": [ { "indexed": false, "name": "sender", "type": "address" }, { "indexed": false, "name": "amount", "type": "uint256" }, { "indexed": false, "name": "memo", "type": "bytes" } ], "name": "received", "type": "event" }, { "anonymous": false, "inputs": [ { "indexed": false, "name": "sender", "type": "address" } ], "name": "receivedAddr", "type": "event" } ]`
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	require.NoErrorf(err, "could not parse abi: %v")

	calledSendCrossChainAppResponseFn := false
	issuer, vm, _, appSender := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")

	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(err)
	}()

	appSender.SendCrossChainAppResponseF = func(ctx context.Context, respondingChainID ids.ID, requestID uint32, responseBytes []byte) {
		calledSendCrossChainAppResponseFn = true

		var response message.EthCallResponse
		if _, err = crossChainCodec.Unmarshal(responseBytes, &response); err != nil {
			require.NoErrorf(err, "unexpected error during unmarshal: %w")
		}

		result := core.ExecutionResult{}
		err = json.Unmarshal(response.ExecutionResult, &result)
		require.NoError(err)
		require.NotNil(result.ReturnData)

		finalResult, err := parsed.Unpack("receive", result.ReturnData)
		require.NoError(err)
		require.NotNil(finalResult)
		require.Equal("hello world", finalResult[0])
	}

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	tx := types.NewTransaction(uint64(0), testEthAddrs[1], firstTxAmount, 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	require.NoError(err)

	txErrors := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for _, err := range txErrors {
		require.NoError(err)
	}

	<-issuer

	blk1, err := vm.BuildBlock(context.Background())
	require.NoError(err)

	err = blk1.Verify(context.Background())
	require.NoError(err)

	if status := blk1.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	err = vm.SetPreference(context.Background(), blk1.ID())
	require.NoError(err)

	err = blk1.Accept(context.Background())
	require.NoError(err)

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk1.ID()) {
		t.Fatalf("Expected new block to match")
	}

	if status := blk1.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID, err := vm.LastAccepted(context.Background())
	require.NoError(err)

	if lastAcceptedID != blk1.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk1.ID(), lastAcceptedID)
	}

	contractTx := types.NewContractCreation(1, common.Big0, 200000, big.NewInt(testMinGasPrice), common.FromHex(abiBin))
	contractSignedTx, err := types.SignTx(contractTx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	require.NoError(err)

	errs := vm.txPool.AddRemotesSync([]*types.Transaction{contractSignedTx})
	for _, err := range errs {
		require.NoError(err)
	}
	testAddr := testEthAddrs[0]
	contractAddress := crypto.CreateAddress(testAddr, 1)

	<-issuer

	blk2, err := vm.BuildBlock(context.Background())
	require.NoError(err)

	err = blk2.Verify(context.Background())
	require.NoError(err)

	if status := blk2.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	err = vm.SetPreference(context.Background(), blk2.ID())
	require.NoError(err)

	err = blk2.Accept(context.Background())
	require.NoError(err)

	newHead = <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk2.ID()) {
		t.Fatalf("Expected new block to match")
	}

	if status := blk2.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID, err = vm.LastAccepted(context.Background())
	require.NoError(err)

	if lastAcceptedID != blk2.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)
	}

	input, err := parsed.Pack("receive", []byte("X"))
	require.NoError(err)

	data := hexutil.Bytes(input)

	requestArgs, err := json.Marshal(&ethapi.TransactionArgs{
		To:   &contractAddress,
		Data: &data,
	})
	require.NoError(err)

	var ethCallRequest message.CrossChainRequest = message.EthCallRequest{
		RequestArgs: requestArgs,
	}

	crossChainRequest, err := crossChainCodec.Marshal(message.Version, &ethCallRequest)
	require.NoError(err)

	requestingChainID := ids.ID(common.BytesToHash([]byte{1, 2, 3, 4, 5}))

	// we need all items in the acceptor queue to be processed before we process a cross chain request
	vm.blockChain.DrainAcceptorQueue()
	err = vm.Network.CrossChainAppRequest(context.Background(), requestingChainID, 1, time.Now().Add(60*time.Second), crossChainRequest)
	require.NoError(err)
	require.True(calledSendCrossChainAppResponseFn, "sendCrossChainAppResponseFn was not called")
}
