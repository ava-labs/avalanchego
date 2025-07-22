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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/api/metrics"
	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/eth/filters"
	"github.com/ava-labs/coreth/miner"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	atomictxpool "github.com/ava-labs/coreth/plugin/evm/atomic/txpool"
	atomicvm "github.com/ava-labs/coreth/plugin/evm/atomic/vm"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/coreth/utils/utilstest"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	schemes          = []string{rawdb.HashScheme, customrawdb.FirewoodScheme}
	initialBaseFee   = big.NewInt(ap3.InitialBaseFee)
	testKeys         = secp256k1.TestKeys()[:3]
	testEthAddrs     []common.Address // testEthAddrs[i] corresponds to testKeys[i]
	testShortIDAddrs []ids.ShortID

	genesisJSON = func(cfg *params.ChainConfig) string {
		g := new(core.Genesis)
		g.Difficulty = big.NewInt(0)
		g.GasLimit = 0x5f5e100
		g.Timestamp = uint64(upgrade.InitiallyActiveTime.Unix())

		// Use chainId: 43111, so that it does not overlap with any Avalanche ChainIDs, which may have their
		// config overridden in vm.Initialize.
		cpy := *cfg
		cpy.ChainID = big.NewInt(43111)
		g.Config = &cpy

		allocStr := `{"0100000000000000000000000000000000000000":{"code":"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033","balance":"0x0"}}`
		json.Unmarshal([]byte(allocStr), &g.Alloc)
		// After Durango, an additional account is funded in tests to use
		// with warp messages.
		if params.GetExtra(cfg).IsDurango(0) {
			addr := common.HexToAddress("0x99b9DEA54C48Dfea6aA9A4Ca4623633EE04ddbB5")
			balance := new(big.Int).Mul(big.NewInt(params.Ether), big.NewInt(10))
			g.Alloc[addr] = types.Account{Balance: balance}
		}

		b, err := json.Marshal(g)
		if err != nil {
			panic(err)
		}
		return string(b)
	}

	activateCancun = func(cfg *params.ChainConfig) *params.ChainConfig {
		cpy := *cfg
		cpy.ShanghaiTime = utils.NewUint64(0)
		cpy.CancunTime = utils.NewUint64(0)
		return &cpy
	}

	forkToChainConfig = map[upgradetest.Fork]*params.ChainConfig{
		upgradetest.NoUpgrades:        params.TestLaunchConfig,
		upgradetest.ApricotPhase1:     params.TestApricotPhase1Config,
		upgradetest.ApricotPhase2:     params.TestApricotPhase2Config,
		upgradetest.ApricotPhase3:     params.TestApricotPhase3Config,
		upgradetest.ApricotPhase4:     params.TestApricotPhase4Config,
		upgradetest.ApricotPhase5:     params.TestApricotPhase5Config,
		upgradetest.ApricotPhasePre6:  params.TestApricotPhasePre6Config,
		upgradetest.ApricotPhase6:     params.TestApricotPhase6Config,
		upgradetest.ApricotPhasePost6: params.TestApricotPhasePost6Config,
		upgradetest.Banff:             params.TestBanffChainConfig,
		upgradetest.Cortina:           params.TestCortinaChainConfig,
		upgradetest.Durango:           params.TestDurangoChainConfig,
		upgradetest.Etna:              params.TestEtnaChainConfig,
		upgradetest.Fortuna:           params.TestFortunaChainConfig,
		upgradetest.Granite:           params.TestGraniteChainConfig,
	}

	genesisJSONCancun = genesisJSON(activateCancun(params.TestChainConfig))

	apricotRulesPhase0 = *params.GetRulesExtra(params.TestLaunchConfig.Rules(common.Big0, params.IsMergeTODO, 0))
	apricotRulesPhase1 = *params.GetRulesExtra(params.TestApricotPhase1Config.Rules(common.Big0, params.IsMergeTODO, 0))
	apricotRulesPhase2 = *params.GetRulesExtra(params.TestApricotPhase2Config.Rules(common.Big0, params.IsMergeTODO, 0))
	apricotRulesPhase3 = *params.GetRulesExtra(params.TestApricotPhase3Config.Rules(common.Big0, params.IsMergeTODO, 0))
	apricotRulesPhase5 = *params.GetRulesExtra(params.TestApricotPhase5Config.Rules(common.Big0, params.IsMergeTODO, 0))
	apricotRulesPhase6 = *params.GetRulesExtra(params.TestApricotPhase6Config.Rules(common.Big0, params.IsMergeTODO, 0))
	banffRules         = *params.GetRulesExtra(params.TestBanffChainConfig.Rules(common.Big0, params.IsMergeTODO, 0))
)

func init() {
	for _, key := range testKeys {
		testEthAddrs = append(testEthAddrs, key.EthAddress())
		testShortIDAddrs = append(testShortIDAddrs, key.Address())
	}
}

func newPrefundedGenesis(
	balance int,
	addresses ...common.Address,
) *core.Genesis {
	alloc := types.GenesisAlloc{}
	for _, address := range addresses {
		alloc[address] = types.Account{
			Balance: big.NewInt(int64(balance)),
		}
	}

	return &core.Genesis{
		Config:     params.TestChainConfig,
		Difficulty: big.NewInt(0),
		Alloc:      alloc,
	}
}

type testVMConfig struct {
	isSyncing bool
	fork      *upgradetest.Fork
	// If genesisJSON is empty, defaults to the genesis corresponding to the
	// fork.
	genesisJSON string
	configJSON  string
	// the VM will start with UTXOs in the X-Chain Shared Memory containing
	// AVAX based on the map
	// The UTXOIDs are generated by using a hash of the address in the map such
	// that the UTXOs will be generated deterministically.
	utxos map[ids.ShortID]uint64
}

type testVM struct {
	t            *testing.T
	atomicVM     *atomicvm.VM
	vm           *VM
	db           *prefixdb.Database
	atomicMemory *avalancheatomic.Memory
	appSender    *enginetest.Sender
}

func newVM(t *testing.T, config testVMConfig) *testVM {
	ctx := snowtest.Context(t, snowtest.CChainID)
	fork := upgradetest.Latest
	if config.fork != nil {
		fork = *config.fork
	}
	ctx.NetworkUpgrades = upgradetest.GetConfig(fork)

	if len(config.genesisJSON) == 0 {
		config.genesisJSON = genesisJSON(forkToChainConfig[fork])
	}

	baseDB := memdb.New()

	// initialize the atomic memory
	atomicMemory := avalancheatomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = atomicMemory.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	prefixedDB := prefixdb.New([]byte{1}, baseDB)

	innerVM := &VM{}
	atomicVM := atomicvm.WrapVM(innerVM)
	appSender := &enginetest.Sender{
		T:                 t,
		CantSendAppGossip: true,
		SendAppGossipF:    func(context.Context, commonEng.SendConfig, []byte) error { return nil },
	}
	require.NoError(t, atomicVM.Initialize(
		context.Background(),
		ctx,
		prefixedDB,
		[]byte(config.genesisJSON),
		nil,
		[]byte(config.configJSON),
		nil,
		appSender,
	), "error initializing vm")

	if !config.isSyncing {
		require.NoError(t, atomicVM.SetState(context.Background(), snow.Bootstrapping))
		require.NoError(t, atomicVM.SetState(context.Background(), snow.NormalOp))
	}

	for addr, avaxAmount := range config.utxos {
		txID, err := ids.ToID(hashing.ComputeHash256(addr.Bytes()))
		if err != nil {
			t.Fatalf("Failed to generate txID from addr: %s", err)
		}
		if _, err := addUTXO(atomicMemory, innerVM.ctx, txID, 0, innerVM.ctx.AVAXAssetID, avaxAmount, addr); err != nil {
			t.Fatalf("Failed to add UTXO to shared memory: %s", err)
		}
	}

	return &testVM{
		t:            t,
		atomicVM:     atomicVM,
		vm:           innerVM,
		db:           prefixedDB,
		atomicMemory: atomicMemory,
		appSender:    appSender,
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

func (vm *testVM) WaitForEvent(ctx context.Context) commonEng.Message {
	msg, err := vm.vm.WaitForEvent(ctx)
	require.NoError(vm.t, err)
	return msg
}

// setupGenesis sets up the genesis
func setupGenesis(
	t *testing.T,
	fork upgradetest.Fork,
) (*snow.Context,
	*prefixdb.Database,
	[]byte,
	*avalancheatomic.Memory,
) {
	ctx := snowtest.Context(t, snowtest.CChainID)
	ctx.NetworkUpgrades = upgradetest.GetConfig(fork)

	baseDB := memdb.New()

	// initialize the atomic memory
	atomicMemory := avalancheatomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = atomicMemory.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	prefixedDB := prefixdb.New([]byte{1}, baseDB)
	genesisJSON := genesisJSON(forkToChainConfig[fork])
	return ctx, prefixedDB, []byte(genesisJSON), atomicMemory
}

func addUTXO(sharedMemory *avalancheatomic.Memory, ctx *snow.Context, txID ids.ID, index uint32, assetID ids.ID, amount uint64, addr ids.ShortID) (*avax.UTXO, error) {
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: index,
		},
		Asset: avax.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	if err != nil {
		return nil, err
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			addr.Bytes(),
		},
	}}}}); err != nil {
		return nil, err
	}

	return utxo, nil
}

func TestVMConfig(t *testing.T) {
	txFeeCap := float64(11)
	enabledEthAPIs := []string{"debug"}
	vm := newVM(t, testVMConfig{
		configJSON: fmt.Sprintf(`{"rpc-tx-fee-cap": %g,"eth-apis": [%q]}`, txFeeCap, enabledEthAPIs[0]),
	}).vm
	require.Equal(t, vm.config.RPCTxFeeCap, txFeeCap, "Tx Fee Cap should be set")
	require.Equal(t, vm.config.EthAPIs(), enabledEthAPIs, "EnabledEthAPIs should be set")
	require.NoError(t, vm.Shutdown(context.Background()))
}

func TestVMConfigDefaults(t *testing.T) {
	txFeeCap := float64(11)
	enabledEthAPIs := []string{"debug"}
	vm := newVM(t, testVMConfig{
		configJSON: fmt.Sprintf(`{"rpc-tx-fee-cap": %g,"eth-apis": [%q]}`, txFeeCap, enabledEthAPIs[0]),
	}).vm

	var vmConfig config.Config
	vmConfig.SetDefaults(defaultTxPoolConfig)
	vmConfig.RPCTxFeeCap = txFeeCap
	vmConfig.EnabledEthAPIs = enabledEthAPIs
	require.Equal(t, vmConfig, vm.config, "VM Config should match default with overrides")
	require.NoError(t, vm.Shutdown(context.Background()))
}

func TestVMNilConfig(t *testing.T) {
	vm := newVM(t, testVMConfig{}).vm

	// VM Config should match defaults if no config is passed in
	var vmConfig config.Config
	vmConfig.SetDefaults(defaultTxPoolConfig)
	require.Equal(t, vmConfig, vm.config, "VM Config should match default config")
	require.NoError(t, vm.Shutdown(context.Background()))
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
	require.NoError(t, vm.Shutdown(context.Background()))

	// Check that the first profile was generated
	expectedFileName := filepath.Join(profilerDir, "cpu.profile.1")
	_, err := os.Stat(expectedFileName)
	require.NoError(t, err, "Expected continuous profiler to generate the first CPU profile at %s", expectedFileName)
}

func TestVMUpgrades(t *testing.T) {
	for _, scheme := range schemes {
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

			vm := newVM(t, testVMConfig{
				fork:       &test.fork,
				configJSON: getConfig(scheme, ""),
			}).vm
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

func TestImportMissingUTXOs(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testImportMissingUTXOs(t, scheme)
		})
	}
}

func testImportMissingUTXOs(t *testing.T, scheme string) {
	// make a VM with a shared memory that has an importable UTXO to build a block
	importAmount := uint64(50000000)
	fork := upgradetest.ApricotPhase2
	tvm1 := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		require.NoError(t, tvm1.vm.Shutdown(context.Background()))
	}()

	importTx, err := tvm1.atomicVM.NewImportTx(tvm1.vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	require.NoError(t, err)
	require.NoError(t, tvm1.atomicVM.AtomicMempool.AddLocalTx(importTx))
	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))
	blk, err := tvm1.vm.BuildBlock(context.Background())
	require.NoError(t, err)

	// make another VM which is missing the UTXO in shared memory
	vm2 := newVM(t, testVMConfig{
		fork:       &fork,
		configJSON: getConfig(scheme, ""),
	}).vm
	defer func() {
		require.NoError(t, vm2.Shutdown(context.Background()))
	}()

	vm2Blk, err := vm2.ParseBlock(context.Background(), blk.Bytes())
	require.NoError(t, err)
	err = vm2Blk.Verify(context.Background())
	require.ErrorIs(t, err, atomicvm.ErrMissingUTXOs)

	// This should not result in a bad block since the missing UTXO should
	// prevent InsertBlockManual from being called.
	badBlocks, _ := vm2.blockChain.BadBlocks()
	require.Len(t, badBlocks, 0)
}

// Simple test to ensure we can issue an import transaction followed by an export transaction
// and they will be indexed correctly when accepted.
func TestIssueAtomicTxs(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testIssueAtomicTxs(t, scheme)
		})
	}
}

func testIssueAtomicTxs(t *testing.T, scheme string) {
	importAmount := uint64(50000000)
	fork := upgradetest.ApricotPhase2
	tvm := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if lastAcceptedID, err := tvm.vm.LastAccepted(context.Background()); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}
	tvm.vm.blockChain.DrainAcceptorQueue()
	filterAPI := filters.NewFilterAPI(filters.NewFilterSystem(tvm.vm.eth.APIBackend, filters.Config{
		Timeout: 5 * time.Minute,
	}))
	blockHash := common.Hash(blk.ID())
	logs, err := filterAPI.GetLogs(context.Background(), filters.FilterCriteria{
		BlockHash: &blockHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Fatalf("Expected log length to be 0, but found %d", len(logs))
	}
	if logs == nil {
		t.Fatal("Expected logs to be non-nil")
	}

	exportTx, err := tvm.atomicVM.NewExportTx(tvm.vm.ctx.AVAXAssetID, importAmount-(2*ap0.AtomicTxFee), tvm.vm.ctx.XChainID, testShortIDAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(exportTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk2, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk2.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk2.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if lastAcceptedID, err := tvm.vm.LastAccepted(context.Background()); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blk2.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)
	}

	// Check that both atomic transactions were indexed as expected.
	indexedImportTx, status, height, err := tvm.atomicVM.GetAtomicTx(importTx.ID())
	assert.NoError(t, err)
	assert.Equal(t, atomic.Accepted, status)
	assert.Equal(t, uint64(1), height, "expected height of indexed import tx to be 1")
	assert.Equal(t, indexedImportTx.ID(), importTx.ID(), "expected ID of indexed import tx to match original txID")

	indexedExportTx, status, height, err := tvm.atomicVM.GetAtomicTx(exportTx.ID())
	assert.NoError(t, err)
	assert.Equal(t, atomic.Accepted, status)
	assert.Equal(t, uint64(2), height, "expected height of indexed export tx to be 2")
	assert.Equal(t, indexedExportTx.ID(), exportTx.ID(), "expected ID of indexed import tx to match original txID")
}

func TestBuildEthTxBlock(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testBuildEthTxBlock(t, scheme)
		})
	}
}

func testBuildEthTxBlock(t *testing.T, scheme string) {
	importAmount := uint64(20000000)
	fork := upgradetest.ApricotPhase2
	tvm := newVM(t, testVMConfig{
		fork:       &fork,
		configJSON: getConfig(scheme, `"pruning-enabled":true`),
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk1, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk1.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blk1.ID()); err != nil {
		t.Fatal(err)
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
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainID), testKeys[0].ToECDSA())
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := tvm.vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk2, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk2.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk2.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk2.ID()) {
		t.Fatalf("Expected new block to match")
	}

	lastAcceptedID, err := tvm.vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk2.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)
	}

	ethBlk1 := blk1.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	if ethBlk1Root := ethBlk1.Root(); !tvm.vm.blockChain.HasState(ethBlk1Root) {
		t.Fatalf("Expected blk1 state root to not yet be pruned after blk2 was accepted because of tip buffer")
	}

	// Clear the cache and ensure that GetBlock returns internal blocks with the correct status
	tvm.vm.State.Flush()
	blk2Refreshed, err := tvm.vm.GetBlockInternal(context.Background(), blk2.ID())
	if err != nil {
		t.Fatal(err)
	}

	blk1RefreshedID := blk2Refreshed.Parent()
	blk1Refreshed, err := tvm.vm.GetBlockInternal(context.Background(), blk1RefreshedID)
	if err != nil {
		t.Fatal(err)
	}

	if blk1Refreshed.ID() != blk1.ID() {
		t.Fatalf("Found unexpected blkID for parent of blk2")
	}

	restartedVM := atomicvm.WrapVM(&VM{})
	newCTX := snowtest.Context(t, snowtest.CChainID)
	newCTX.NetworkUpgrades = upgradetest.GetConfig(fork)
	newCTX.ChainDataDir = tvm.vm.ctx.ChainDataDir
	if err := restartedVM.Initialize(
		context.Background(),
		newCTX,
		tvm.db,
		[]byte(genesisJSON(forkToChainConfig[fork])),
		[]byte(""),
		[]byte(getConfig(scheme, `"pruning-enabled":true`)),
		[]*commonEng.Fx{},
		nil,
	); err != nil {
		t.Fatal(err)
	}

	// State root should not have been committed and discarded on restart
	if ethBlk1Root := ethBlk1.Root(); restartedVM.Blockchain().HasState(ethBlk1Root) {
		t.Fatalf("Expected blk1 state root to be pruned after blk2 was accepted on top of it in pruning mode")
	}

	// State root should be committed when accepted tip on shutdown
	ethBlk2 := blk2.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	if ethBlk2Root := ethBlk2.Root(); !restartedVM.Blockchain().HasState(ethBlk2Root) {
		t.Fatalf("Expected blk2 state root to not be pruned after shutdown (last accepted tip should be committed)")
	}
}

func testConflictingImportTxs(t *testing.T, fork upgradetest.Fork, scheme string) {
	importAmount := uint64(10000000)
	tvm := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
			testShortIDAddrs[1]: importAmount,
			testShortIDAddrs[2]: importAmount,
		},
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTxs := make([]*atomic.Tx, 0, 3)
	conflictTxs := make([]*atomic.Tx, 0, 3)
	for i, key := range testKeys {
		importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[i], initialBaseFee, []*secp256k1.PrivateKey{key})
		if err != nil {
			t.Fatal(err)
		}
		importTxs = append(importTxs, importTx)

		conflictAddr := testEthAddrs[(i+1)%len(testEthAddrs)]
		conflictTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, conflictAddr, initialBaseFee, []*secp256k1.PrivateKey{key})
		if err != nil {
			t.Fatal(err)
		}
		conflictTxs = append(conflictTxs, conflictTx)
	}

	expectedParentBlkID, err := tvm.vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range importTxs[:2] {
		if err := tvm.atomicVM.AtomicMempool.AddLocalTx(tx); err != nil {
			t.Fatal(err)
		}

		require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

		tvm.vm.clock.Set(tvm.vm.clock.Time().Add(2 * time.Second))
		blk, err := tvm.vm.BuildBlock(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if err := blk.Verify(context.Background()); err != nil {
			t.Fatal(err)
		}

		if parentID := blk.Parent(); parentID != expectedParentBlkID {
			t.Fatalf("Expected parent to have blockID %s, but found %s", expectedParentBlkID, parentID)
		}

		expectedParentBlkID = blk.ID()
		if err := tvm.vm.SetPreference(context.Background(), blk.ID()); err != nil {
			t.Fatal(err)
		}
	}

	// Check that for each conflict tx (whose conflict is in the chain ancestry)
	// the VM returns an error when it attempts to issue the conflict into the mempool
	// and when it attempts to build a block with the conflict force added to the mempool.
	for i, tx := range conflictTxs[:2] {
		if err := tvm.atomicVM.AtomicMempool.AddLocalTx(tx); err == nil {
			t.Fatal("Expected issueTx to fail due to conflicting transaction")
		}
		// Force issue transaction directly to the mempool
		if err := tvm.atomicVM.AtomicMempool.ForceAddTx(tx); err != nil {
			t.Fatal(err)
		}
		require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

		tvm.vm.clock.Set(tvm.vm.clock.Time().Add(2 * time.Second))
		_, err = tvm.vm.BuildBlock(context.Background())
		// The new block is verified in BuildBlock, so
		// BuildBlock should fail due to an attempt to
		// double spend an atomic UTXO.
		if err == nil {
			t.Fatalf("Block verification should have failed in BuildBlock %d due to double spending atomic UTXO", i)
		}
	}

	// Generate one more valid block so that we can copy the header to create an invalid block
	// with modified extra data. This new block will be invalid for more than one reason (invalid merkle root)
	// so we check to make sure that the expected error is returned from block verification.
	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTxs[2]); err != nil {
		t.Fatal(err)
	}
	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))
	tvm.vm.clock.Set(tvm.vm.clock.Time().Add(2 * time.Second))

	validBlock, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := validBlock.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	validEthBlock := validBlock.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()

	rules := tvm.vm.currentRules()
	var extraData []byte
	switch {
	case rules.IsApricotPhase5:
		extraData, err = atomic.Codec.Marshal(atomic.CodecVersion, []*atomic.Tx{conflictTxs[1]})
	default:
		extraData, err = atomic.Codec.Marshal(atomic.CodecVersion, conflictTxs[1])
	}
	if err != nil {
		t.Fatal(err)
	}

	conflictingAtomicTxBlock := customtypes.NewBlockWithExtData(
		types.CopyHeader(validEthBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	blockBytes, err := rlp.EncodeToBytes(conflictingAtomicTxBlock)
	if err != nil {
		t.Fatal(err)
	}

	parsedBlock, err := tvm.vm.ParseBlock(context.Background(), blockBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Verify(context.Background()); !errors.Is(err, atomicvm.ErrConflictingAtomicInputs) {
		t.Fatalf("Expected to fail with err: %s, but found err: %s", atomicvm.ErrConflictingAtomicInputs, err)
	}

	if !rules.IsApricotPhase5 {
		return
	}

	extraData, err = atomic.Codec.Marshal(atomic.CodecVersion, []*atomic.Tx{importTxs[2], conflictTxs[2]})
	if err != nil {
		t.Fatal(err)
	}

	header := types.CopyHeader(validEthBlock.Header())
	headerExtra := customtypes.GetHeaderExtra(header)
	headerExtra.ExtDataGasUsed.Mul(common.Big2, headerExtra.ExtDataGasUsed)

	internalConflictBlock := customtypes.NewBlockWithExtData(
		header,
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	blockBytes, err = rlp.EncodeToBytes(internalConflictBlock)
	if err != nil {
		t.Fatal(err)
	}

	parsedBlock, err = tvm.vm.ParseBlock(context.Background(), blockBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Verify(context.Background()); !errors.Is(err, atomicvm.ErrConflictingAtomicInputs) {
		t.Fatalf("Expected to fail with err: %s, but found err: %s", atomicvm.ErrConflictingAtomicInputs, err)
	}
}

func TestReissueAtomicTxHigherGasPrice(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testReissueAtomicTxHigherGasPrice(t, scheme)
		})
	}
}

func testReissueAtomicTxHigherGasPrice(t *testing.T, scheme string) {
	kc := secp256k1fx.NewKeychain(testKeys...)
	tests := map[string]func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, discarded []*atomic.Tx){
		"single UTXO override": func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, evicted []*atomic.Tx) {
			utxo, err := addUTXO(sharedMemory, vm.Ctx, ids.GenerateTestID(), 0, vm.Ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			tx1, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.Clock().Unix(), vm.Ctx.XChainID, testEthAddrs[0], initialBaseFee, kc, []*avax.UTXO{utxo})
			if err != nil {
				t.Fatal(err)
			}
			tx2, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.Clock().Unix(), vm.Ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(common.Big2, initialBaseFee), kc, []*avax.UTXO{utxo})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.AtomicMempool.AddLocalTx(tx1); err != nil {
				t.Fatal(err)
			}
			if err := vm.AtomicMempool.AddLocalTx(tx2); err != nil {
				t.Fatal(err)
			}

			return []*atomic.Tx{tx2}, []*atomic.Tx{tx1}
		},
		"one of two UTXOs overrides": func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, evicted []*atomic.Tx) {
			utxo1, err := addUTXO(sharedMemory, vm.Ctx, ids.GenerateTestID(), 0, vm.Ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			utxo2, err := addUTXO(sharedMemory, vm.Ctx, ids.GenerateTestID(), 0, vm.Ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			tx1, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.Clock().Unix(), vm.Ctx.XChainID, testEthAddrs[0], initialBaseFee, kc, []*avax.UTXO{utxo1, utxo2})
			if err != nil {
				t.Fatal(err)
			}
			tx2, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.Clock().Unix(), vm.Ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(common.Big2, initialBaseFee), kc, []*avax.UTXO{utxo1})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.AtomicMempool.AddLocalTx(tx1); err != nil {
				t.Fatal(err)
			}
			if err := vm.AtomicMempool.AddLocalTx(tx2); err != nil {
				t.Fatal(err)
			}

			return []*atomic.Tx{tx2}, []*atomic.Tx{tx1}
		},
		"hola": func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, evicted []*atomic.Tx) {
			utxo1, err := addUTXO(sharedMemory, vm.Ctx, ids.GenerateTestID(), 0, vm.Ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			utxo2, err := addUTXO(sharedMemory, vm.Ctx, ids.GenerateTestID(), 0, vm.Ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}

			importTx1, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.Clock().Unix(), vm.Ctx.XChainID, testEthAddrs[0], initialBaseFee, kc, []*avax.UTXO{utxo1})
			if err != nil {
				t.Fatal(err)
			}

			importTx2, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.Clock().Unix(), vm.Ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(big.NewInt(3), initialBaseFee), kc, []*avax.UTXO{utxo2})
			if err != nil {
				t.Fatal(err)
			}

			reissuanceTx1, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.Clock().Unix(), vm.Ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(big.NewInt(2), initialBaseFee), kc, []*avax.UTXO{utxo1, utxo2})
			if err != nil {
				t.Fatal(err)
			}
			if err := vm.AtomicMempool.AddLocalTx(importTx1); err != nil {
				t.Fatal(err)
			}

			if err := vm.AtomicMempool.AddLocalTx(importTx2); err != nil {
				t.Fatal(err)
			}

			if err := vm.AtomicMempool.AddLocalTx(reissuanceTx1); !errors.Is(err, atomictxpool.ErrConflictingAtomicTx) {
				t.Fatalf("Expected to fail with err: %s, but found err: %s", atomictxpool.ErrConflictingAtomicTx, err)
			}

			assert.True(t, vm.AtomicMempool.Has(importTx1.ID()))
			assert.True(t, vm.AtomicMempool.Has(importTx2.ID()))
			assert.False(t, vm.AtomicMempool.Has(reissuanceTx1.ID()))

			reissuanceTx2, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.Clock().Unix(), vm.Ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(big.NewInt(4), initialBaseFee), kc, []*avax.UTXO{utxo1, utxo2})
			if err != nil {
				t.Fatal(err)
			}
			if err := vm.AtomicMempool.AddLocalTx(reissuanceTx2); err != nil {
				t.Fatal(err)
			}

			return []*atomic.Tx{reissuanceTx2}, []*atomic.Tx{importTx1, importTx2}
		},
	}
	for name, issueTxs := range tests {
		t.Run(name, func(t *testing.T) {
			fork := upgradetest.ApricotPhase5
			tvm := newVM(t, testVMConfig{
				fork:       &fork,
				configJSON: getConfig(scheme, `"pruning-enabled":true`),
			})
			issuedTxs, evictedTxs := issueTxs(t, tvm.atomicVM, tvm.atomicMemory)

			for i, tx := range issuedTxs {
				_, issued := tvm.atomicVM.AtomicMempool.GetPendingTx(tx.ID())
				assert.True(t, issued, "expected issued tx at index %d to be issued", i)
			}

			for i, tx := range evictedTxs {
				_, discarded, _ := tvm.atomicVM.AtomicMempool.GetTx(tx.ID())
				assert.True(t, discarded, "expected discarded tx at index %d to be discarded", i)
			}
		})
	}
}

func TestConflictingImportTxsAcrossBlocks(t *testing.T) {
	for _, fork := range []upgradetest.Fork{
		upgradetest.ApricotPhase1,
		upgradetest.ApricotPhase2,
		upgradetest.ApricotPhase3,
		upgradetest.ApricotPhase4,
		upgradetest.ApricotPhase5,
	} {
		t.Run(fork.String(), func(t *testing.T) {
			for _, scheme := range schemes {
				t.Run(scheme, func(t *testing.T) {
					testConflictingImportTxs(t, fork, scheme)
				})
			}
		})
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
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testSetPreferenceRace(t, scheme)
		})
	}
}

func testSetPreferenceRace(t *testing.T, scheme string) {
	// Create two VMs which will agree on block A and then
	// build the two distinct preferred chains above
	importAmount := uint64(1000000000)
	fork := upgradetest.NoUpgrades
	tvmConfig := testVMConfig{
		fork:       &fork,
		configJSON: getConfig(scheme, `"pruning-enabled":true`),
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
	}
	tvm1 := newVM(t, tvmConfig)
	tvm2 := newVM(t, tvmConfig)

	defer func() {
		if err := tvm1.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := tvm2.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	tvm1.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	tvm2.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	importTx, err := tvm1.atomicVM.NewImportTx(tvm1.vm.ctx.XChainID, testEthAddrs[1], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm1.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))

	vm1BlkA, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := tvm2.vm.ParseBlock(context.Background(), vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if err := tvm2.vm.SetPreference(context.Background(), vm2BlkA.ID()); err != nil {
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
		tx := types.NewTransaction(uint64(i), testEthAddrs[1], big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm1.vm.chainID), testKeys[1].ToECDSA())
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = tvm1.vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))

	vm1BlkB, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	errs = tvm2.vm.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm2.WaitForEvent(context.Background()))
	vm2BlkC, err := tvm2.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if err := tvm2.vm.SetPreference(context.Background(), vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Block D
	errs = tvm2.vm.txPool.AddRemotesSync(txs[5:10])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm2.WaitForEvent(context.Background()))
	vm2BlkD, err := tvm2.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	if err := vm2BlkD.Verify(context.Background()); err != nil {
		t.Fatalf("BlkD failed verification on VM2: %s", err)
	}

	if err := tvm2.vm.SetPreference(context.Background(), vm2BlkD.ID()); err != nil {
		t.Fatal(err)
	}

	// VM1 receives blkC and blkD from VM1
	// and happens to call SetPreference on blkD without ever calling SetPreference
	// on blkC
	// Here we parse them in reverse order to simulate receiving a chain from the tip
	// back to the last accepted block as would typically be the case in the consensus
	// engine
	vm1BlkD, err := tvm1.vm.ParseBlock(context.Background(), vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("VM1 errored parsing blkD: %s", err)
	}
	vm1BlkC, err := tvm1.vm.ParseBlock(context.Background(), vm2BlkC.Bytes())
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
	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkD.ID()); err != nil {
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
	if err := tvm2.vm.blockChain.ValidateCanonicalChain(); err != nil {
		t.Fatalf("VM2 failed canonical chain verification due to: %s", err)
	}

	if err := tvm1.vm.blockChain.ValidateCanonicalChain(); err != nil {
		t.Fatalf("VM1 failed canonical chain verification due to: %s", err)
	}
}

func TestConflictingTransitiveAncestryWithGap(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testConflictingTransitiveAncestryWithGap(t, scheme)
		})
	}
}

func testConflictingTransitiveAncestryWithGap(t *testing.T, scheme string) {
	key := utilstest.NewKey(t)

	key0 := testKeys[0]
	addr0 := key0.Address()

	key1 := testKeys[1]
	addr1 := key1.Address()

	importAmount := uint64(1000000000)

	fork := upgradetest.NoUpgrades
	tvm := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			addr0: importAmount,
			addr1: importAmount,
		},
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	importTx0A, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, key.Address, initialBaseFee, []*secp256k1.PrivateKey{key0})
	if err != nil {
		t.Fatal(err)
	}
	// Create a conflicting transaction
	importTx0B, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[2], initialBaseFee, []*secp256k1.PrivateKey{key0})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx0A); err != nil {
		t.Fatalf("Failed to issue importTx0A: %s", err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk0, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blk0.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification: %s", err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blk0.ID()); err != nil {
		t.Fatal(err)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk0.ID()) {
		t.Fatalf("Expected new block to match")
	}

	tx := types.NewTransaction(0, key.Address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainID), key.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk1, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build blk1: %s", err)
	}

	if err := blk1.Verify(context.Background()); err != nil {
		t.Fatalf("blk1 failed verification due to %s", err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blk1.ID()); err != nil {
		t.Fatal(err)
	}

	importTx1, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, key.Address, initialBaseFee, []*secp256k1.PrivateKey{key1})
	if err != nil {
		t.Fatalf("Failed to issue importTx1 due to: %s", err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx1); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk2, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blk2.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification: %s", err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blk2.ID()); err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx0B); err == nil {
		t.Fatalf("Should not have been able to issue import tx with conflict")
	}
	// Force issue transaction directly into the mempool
	if err := tvm.atomicVM.AtomicMempool.ForceAddTx(importTx0B); err != nil {
		t.Fatal(err)
	}
	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	_, err = tvm.vm.BuildBlock(context.Background())
	if err == nil {
		t.Fatal("Shouldn't have been able to build an invalid block")
	}
}

func TestBonusBlocksTxs(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testBonusBlocksTxs(t, scheme)
		})
	}
}

func testBonusBlocksTxs(t *testing.T, scheme string) {
	fork := upgradetest.NoUpgrades
	tvm := newVM(t, testVMConfig{
		fork:       &fork,
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importAmount := uint64(10000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: tvm.vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].Address()},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := tvm.atomicMemory.NewSharedMemory(tvm.vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{tvm.vm.ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Make [blk] a bonus block.
	tvm.atomicVM.AtomicBackend.AddBonusBlock(blk.Height(), blk.ID())

	// Remove the UTXOs from shared memory, so that non-bonus blocks will fail verification
	if err := tvm.vm.ctx.SharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{tvm.vm.ctx.XChainID: {RemoveRequests: [][]byte{inputID[:]}}}); err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	lastAcceptedID, err := tvm.vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
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
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testReorgProtection(t, scheme)
		})
	}
}

func testReorgProtection(t *testing.T, scheme string) {
	importAmount := uint64(1000000000)
	fork := upgradetest.NoUpgrades
	tvmConfig := testVMConfig{
		fork:       &fork,
		configJSON: getConfig(scheme, `"pruning-enabled":false`),
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
	}
	tvm1 := newVM(t, tvmConfig)
	tvm2 := newVM(t, tvmConfig)
	defer func() {
		if err := tvm1.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := tvm2.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	tvm1.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	tvm2.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := tvm1.atomicVM.NewImportTx(tvm1.vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm1.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))

	vm1BlkA, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := tvm2.vm.ParseBlock(context.Background(), vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if err := tvm2.vm.SetPreference(context.Background(), vm2BlkA.ID()); err != nil {
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
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm1.vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = tvm1.vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))

	vm1BlkB, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	errs = tvm2.vm.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm2.WaitForEvent(context.Background()))
	vm2BlkC, err := tvm2.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}

	vm1BlkC, err := tvm1.vm.ParseBlock(context.Background(), vm2BlkC.Bytes())
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
	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkC.ID()); !strings.Contains(err.Error(), "cannot orphan finalized block") {
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
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testNonCanonicalAccept(t, scheme)
		})
	}
}
func testNonCanonicalAccept(t *testing.T, scheme string) {
	importAmount := uint64(1000000000)
	fork := upgradetest.NoUpgrades
	tvmConfig := testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
		configJSON: getConfig(scheme, ""),
	}
	tvm1 := newVM(t, tvmConfig)
	tvm2 := newVM(t, tvmConfig)
	defer func() {
		if err := tvm1.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := tvm2.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	tvm1.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	tvm2.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := tvm1.atomicVM.NewImportTx(tvm1.vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm1.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))

	vm1BlkA, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if _, err := tvm1.vm.GetBlockIDAtHeight(context.Background(), vm1BlkA.Height()); err != database.ErrNotFound {
		t.Fatalf("Expected unaccepted block not to be indexed by height, but found %s", err)
	}

	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := tvm2.vm.ParseBlock(context.Background(), vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if _, err := tvm2.vm.GetBlockIDAtHeight(context.Background(), vm2BlkA.Height()); err != database.ErrNotFound {
		t.Fatalf("Expected unaccepted block not to be indexed by height, but found %s", err)
	}
	if err := tvm2.vm.SetPreference(context.Background(), vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if blkID, err := tvm1.vm.GetBlockIDAtHeight(context.Background(), vm1BlkA.Height()); err != nil {
		t.Fatalf("Height lookuped failed on accepted block: %s", err)
	} else if blkID != vm1BlkA.ID() {
		t.Fatalf("Expected accepted block to be indexed by height, but found %s", blkID)
	}
	if err := vm2BlkA.Accept(context.Background()); err != nil {
		t.Fatalf("VM2 failed to accept block: %s", err)
	}
	if blkID, err := tvm2.vm.GetBlockIDAtHeight(context.Background(), vm2BlkA.Height()); err != nil {
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
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm1.vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = tvm1.vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))

	vm1BlkB, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := tvm1.vm.GetBlockIDAtHeight(context.Background(), vm1BlkB.Height()); err != database.ErrNotFound {
		t.Fatalf("Expected unaccepted block not to be indexed by height, but found %s", err)
	}

	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	tvm1.vm.eth.APIBackend.SetAllowUnfinalizedQueries(true)

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock().Hash()
	if b := tvm1.vm.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}

	errs = tvm2.vm.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm2.WaitForEvent(context.Background()))
	vm2BlkC, err := tvm2.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	vm1BlkC, err := tvm1.vm.ParseBlock(context.Background(), vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	if err := vm1BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if _, err := tvm1.vm.GetBlockIDAtHeight(context.Background(), vm1BlkC.Height()); err != database.ErrNotFound {
		t.Fatalf("Expected unaccepted block not to be indexed by height, but found %s", err)
	}

	if err := vm1BlkC.Accept(context.Background()); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}

	if blkID, err := tvm1.vm.GetBlockIDAtHeight(context.Background(), vm1BlkC.Height()); err != nil {
		t.Fatalf("Height lookuped failed on accepted block: %s", err)
	} else if blkID != vm1BlkC.ID() {
		t.Fatalf("Expected accepted block to be indexed by height, but found %s", blkID)
	}

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock().Hash()
	if b := tvm1.vm.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkCHash {
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
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testStickyPreference(t, scheme)
		})
	}
}

func testStickyPreference(t *testing.T, scheme string) {
	importAmount := uint64(1000000000)
	fork := upgradetest.NoUpgrades
	tvmConfig := testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
		configJSON: getConfig(scheme, ""),
	}
	tvm1 := newVM(t, tvmConfig)
	tvm2 := newVM(t, tvmConfig)
	defer func() {
		if err := tvm1.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := tvm2.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	tvm1.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	tvm2.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := tvm1.atomicVM.NewImportTx(tvm1.vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm1.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))

	vm1BlkA, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := tvm2.vm.ParseBlock(context.Background(), vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if err := tvm2.vm.SetPreference(context.Background(), vm2BlkA.ID()); err != nil {
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
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm1.vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = tvm1.vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))

	vm1BlkB, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	tvm1.vm.eth.APIBackend.SetAllowUnfinalizedQueries(true)

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock().Hash()
	if b := tvm1.vm.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}

	errs = tvm2.vm.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm2.WaitForEvent(context.Background()))
	vm2BlkC, err := tvm2.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if err := tvm2.vm.SetPreference(context.Background(), vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	errs = tvm2.vm.txPool.AddRemotesSync(txs[5:])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm2.WaitForEvent(context.Background()))
	vm2BlkD, err := tvm2.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	// Parse blocks produced in vm2
	vm1BlkC, err := tvm1.vm.ParseBlock(context.Background(), vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock().Hash()

	vm1BlkD, err := tvm1.vm.ParseBlock(context.Background(), vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	blkDHeight := vm1BlkD.Height()
	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock().Hash()

	// Should be no-ops
	if err := vm1BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if b := tvm1.vm.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}
	if b := tvm1.vm.blockChain.GetBlockByNumber(blkDHeight); b != nil {
		t.Fatalf("expected block at %d to be nil but got %s", blkDHeight, b.Hash().Hex())
	}
	if b := tvm1.vm.blockChain.CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	// Should still be no-ops on re-verify
	if err := vm1BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if b := tvm1.vm.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}
	if b := tvm1.vm.blockChain.GetBlockByNumber(blkDHeight); b != nil {
		t.Fatalf("expected block at %d to be nil but got %s", blkDHeight, b.Hash().Hex())
	}
	if b := tvm1.vm.blockChain.CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	// Should be queryable after setting preference to side chain
	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkD.ID()); err != nil {
		t.Fatal(err)
	}

	if b := tvm1.vm.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkCHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	}
	if b := tvm1.vm.blockChain.GetBlockByNumber(blkDHeight); b.Hash() != blkDHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkDHeight, blkDHash.Hex(), b.Hash().Hex())
	}
	if b := tvm1.vm.blockChain.CurrentBlock(); b.Hash() != blkDHash {
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
	if b := tvm1.vm.blockChain.GetBlockByNumber(blkBHeight); b.Hash() != blkCHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	}
	if b := tvm1.vm.blockChain.GetBlockByNumber(blkDHeight); b.Hash() != blkDHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkDHeight, blkDHash.Hex(), b.Hash().Hex())
	}
	if b := tvm1.vm.blockChain.CurrentBlock(); b.Hash() != blkDHash {
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
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testUncleBlock(t, scheme)
		})
	}
}

func testUncleBlock(t *testing.T, scheme string) {
	importAmount := uint64(1000000000)
	fork := upgradetest.NoUpgrades
	tvmConfig := testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
		configJSON: getConfig(scheme, ""),
	}
	tvm1 := newVM(t, tvmConfig)
	tvm2 := newVM(t, tvmConfig)
	defer func() {
		if err := tvm1.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := tvm2.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	tvm1.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	tvm2.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := tvm1.atomicVM.NewImportTx(tvm1.vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm1.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))

	vm1BlkA, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := tvm2.vm.ParseBlock(context.Background(), vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if err := tvm2.vm.SetPreference(context.Background(), vm2BlkA.ID()); err != nil {
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
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm1.vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	errs = tvm1.vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))

	vm1BlkB, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	errs = tvm2.vm.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm2.WaitForEvent(context.Background()))
	vm2BlkC, err := tvm2.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if err := tvm2.vm.SetPreference(context.Background(), vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	errs = tvm2.vm.txPool.AddRemotesSync(txs[5:10])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm2.WaitForEvent(context.Background()))
	vm2BlkD, err := tvm2.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	// Create uncle block from blkD
	blkDEthBlock := vm2BlkD.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	uncles := []*types.Header{vm1BlkB.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock().Header()}
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
	uncleBlock, err := wrapBlock(uncleEthBlock, tvm2.vm)
	if err != nil {
		t.Fatal(err)
	}
	if err := uncleBlock.Verify(context.Background()); !errors.Is(err, errUnclesUnsupported) {
		t.Fatalf("VM2 should have failed with %q but got %q", errUnclesUnsupported, err.Error())
	}
	if _, err := tvm1.vm.ParseBlock(context.Background(), vm2BlkC.Bytes()); err != nil {
		t.Fatalf("VM1 errored parsing blkC: %s", err)
	}
	if _, err := tvm1.vm.ParseBlock(context.Background(), uncleBlock.Bytes()); !errors.Is(err, errUnclesUnsupported) {
		t.Fatalf("VM1 should have failed with %q but got %q", errUnclesUnsupported, err.Error())
	}
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
	importAmount := uint64(1000000000)
	fork := upgradetest.NoUpgrades
	tvm := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	// Create empty block from blkA
	ethBlock := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()

	emptyEthBlock := customtypes.NewBlockWithExtData(
		types.CopyHeader(ethBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		nil,
		false,
	)

	if len(customtypes.BlockExtData(emptyEthBlock)) != 0 || customtypes.GetHeaderExtra(emptyEthBlock.Header()).ExtDataHash != (common.Hash{}) {
		t.Fatalf("emptyEthBlock should not have any extra data")
	}

	emptyBlock, err := wrapBlock(emptyEthBlock, tvm.vm)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tvm.vm.ParseBlock(context.Background(), emptyBlock.Bytes()); !errors.Is(err, atomicvm.ErrEmptyBlock) {
		t.Fatalf("VM should have failed with errEmptyBlock but got %s", err.Error())
	}
	if err := emptyBlock.Verify(context.Background()); !errors.Is(err, atomicvm.ErrEmptyBlock) {
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
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testAcceptReorg(t, scheme)
		})
	}
}

func testAcceptReorg(t *testing.T, scheme string) {
	importAmount := uint64(1000000000)
	fork := upgradetest.NoUpgrades
	tvmConfig := testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
		configJSON: getConfig(scheme, ""),
	}
	tvm1 := newVM(t, tvmConfig)
	tvm2 := newVM(t, tvmConfig)
	defer func() {
		if err := tvm1.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}

		if err := tvm2.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	tvm1.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	tvm2.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := tvm1.atomicVM.NewImportTx(tvm1.vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm1.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))

	vm1BlkA, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := tvm2.vm.ParseBlock(context.Background(), vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if err := tvm2.vm.SetPreference(context.Background(), vm2BlkA.ID()); err != nil {
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
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm1.vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference
	// for block B
	errs := tvm1.vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))

	vm1BlkB, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm1.vm.SetPreference(context.Background(), vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	errs = tvm2.vm.txPool.AddRemotesSync(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm2.WaitForEvent(context.Background()))

	vm2BlkC, err := tvm2.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if err := tvm2.vm.SetPreference(context.Background(), vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	errs = tvm2.vm.txPool.AddRemotesSync(txs[5:])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm2.WaitForEvent(context.Background()))

	vm2BlkD, err := tvm2.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	// Parse blocks produced in vm2
	vm1BlkC, err := tvm1.vm.ParseBlock(context.Background(), vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	vm1BlkD, err := tvm1.vm.ParseBlock(context.Background(), vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	if err := vm1BlkC.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock().Hash()
	if b := tvm1.vm.blockChain.CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	if err := vm1BlkC.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock().Hash()
	if b := tvm1.vm.blockChain.CurrentBlock(); b.Hash() != blkCHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkCHash.Hex(), b.Hash().Hex())
	}
	if err := vm1BlkB.Reject(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkD.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock().Hash()
	if b := tvm1.vm.blockChain.CurrentBlock(); b.Hash() != blkDHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
	}
}

func TestFutureBlock(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testFutureBlock(t, scheme)
		})
	}
}

func testFutureBlock(t *testing.T, scheme string) {
	importAmount := uint64(1000000000)
	fork := upgradetest.NoUpgrades
	tvm := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blkA, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	// Create empty block from blkA
	internalBlkA := blkA.(*chain.BlockWrapper).Block.(extension.ExtendedBlock)
	modifiedHeader := types.CopyHeader(internalBlkA.GetEthBlock().Header())
	// Set the VM's clock to the time of the produced block
	tvm.vm.clock.Set(time.Unix(int64(modifiedHeader.Time), 0))
	// Set the modified time to exceed the allowed future time
	modifiedTime := modifiedHeader.Time + uint64(maxFutureBlockTime.Seconds()+1)
	modifiedHeader.Time = modifiedTime
	modifiedBlock := customtypes.NewBlockWithExtData(
		modifiedHeader,
		nil,
		nil,
		nil,
		new(trie.Trie),
		customtypes.BlockExtData(internalBlkA.GetEthBlock()),
		false,
	)

	futureBlock, err := wrapBlock(modifiedBlock, tvm.vm)
	if err != nil {
		t.Fatal(err)
	}

	if err := futureBlock.Verify(context.Background()); err == nil {
		t.Fatal("Future block should have failed verification due to block timestamp too far in the future")
	} else if !strings.Contains(err.Error(), "block timestamp is too far in the future") {
		t.Fatalf("Expected error to be block timestamp too far in the future but found %s", err)
	}
}

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 1 ruleset in genesis.
func TestBuildApricotPhase1Block(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testBuildApricotPhase1Block(t, scheme)
		})
	}
}

func testBuildApricotPhase1Block(t *testing.T, scheme string) {
	importAmount := uint64(1000000000)
	fork := upgradetest.ApricotPhase1
	tvm := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blk.ID()); err != nil {
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
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	for i := 5; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap1.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := tvm.vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk, err = tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	lastAcceptedID, err := tvm.vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}

	// Confirm all txs are present
	ethBlkTxs := tvm.vm.blockChain.GetBlockByNumber(2).Transactions()
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
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testLastAcceptedBlockNumberAllow(t, scheme)
		})
	}
}

func testLastAcceptedBlockNumberAllow(t *testing.T, scheme string) {
	importAmount := uint64(1000000000)
	fork := upgradetest.NoUpgrades
	tvm := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification on VM: %s", err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	blkHeight := blk.Height()
	blkHash := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock().Hash()

	tvm.vm.eth.APIBackend.SetAllowUnfinalizedQueries(true)

	ctx := context.Background()
	b, err := tvm.vm.eth.APIBackend.BlockByNumber(ctx, rpc.BlockNumber(blkHeight))
	if err != nil {
		t.Fatal(err)
	}
	if b.Hash() != blkHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())
	}

	tvm.vm.eth.APIBackend.SetAllowUnfinalizedQueries(false)

	_, err = tvm.vm.eth.APIBackend.BlockByNumber(ctx, rpc.BlockNumber(blkHeight))
	if !errors.Is(err, eth.ErrUnfinalizedData) {
		t.Fatalf("expected ErrUnfinalizedData but got %s", err.Error())
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatalf("VM failed to accept block: %s", err)
	}

	if b := tvm.vm.blockChain.GetBlockByNumber(blkHeight); b.Hash() != blkHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())
	}
}

// Builds [blkA] with a virtuous import transaction and [blkB] with a separate import transaction
// that does not conflict. Accepts [blkB] and rejects [blkA], then asserts that the virtuous atomic
// transaction in [blkA] is correctly re-issued into the atomic transaction mempool.
func TestReissueAtomicTx(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testReissueAtomicTx(t, scheme)
		})
	}
}

func testReissueAtomicTx(t *testing.T, scheme string) {
	fork := upgradetest.ApricotPhase1
	tvm := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: 10000000,
			testShortIDAddrs[1]: 10000000,
		},
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	genesisBlkID, err := tvm.vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blkA, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blkA.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blkA.ID()); err != nil {
		t.Fatal(err)
	}

	// SetPreference to parent before rejecting (will rollback state to genesis
	// so that atomic transaction can be reissued, otherwise current block will
	// conflict with UTXO to be reissued)
	if err := tvm.vm.SetPreference(context.Background(), genesisBlkID); err != nil {
		t.Fatal(err)
	}

	// Rejecting [blkA] should cause [importTx] to be re-issued into the mempool.
	if err := blkA.Reject(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Sleep for a minimum of two seconds to ensure that [blkB] will have a different timestamp
	// than [blkA] so that the block will be unique. This is necessary since we have marked [blkA]
	// as Rejected.
	time.Sleep(2 * time.Second)
	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))
	blkB, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if blkB.Height() != blkA.Height() {
		t.Fatalf("Expected blkB (%d) to have the same height as blkA (%d)", blkB.Height(), blkA.Height())
	}

	if err := blkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blkB.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blkB.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if lastAcceptedID, err := tvm.vm.LastAccepted(context.Background()); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blkB.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blkB.ID(), lastAcceptedID)
	}

	// Check that [importTx] has been indexed correctly after [blkB] is accepted.
	_, height, err := tvm.atomicVM.AtomicTxRepository.GetByTxID(importTx.ID())
	if err != nil {
		t.Fatal(err)
	} else if height != blkB.Height() {
		t.Fatalf("Expected indexed height of import tx to be %d, but found %d", blkB.Height(), height)
	}
}

func TestAtomicTxFailsEVMStateTransferBuildBlock(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testAtomicTxFailsEVMStateTransferBuildBlock(t, scheme)
		})
	}
}

func testAtomicTxFailsEVMStateTransferBuildBlock(t *testing.T, scheme string) {
	fork := upgradetest.ApricotPhase1
	tvm := newVM(t, testVMConfig{
		fork:       &fork,
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	exportTxs := createExportTxOptions(t, tvm.atomicVM, tvm.atomicMemory)
	exportTx1, exportTx2 := exportTxs[0], exportTxs[1]

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(exportTx1); err != nil {
		t.Fatal(err)
	}
	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))
	exportBlk1, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := exportBlk1.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm.vm.SetPreference(context.Background(), exportBlk1.ID()); err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(exportTx2); err == nil {
		t.Fatal("Should have failed to issue due to an invalid export tx")
	}

	if err := tvm.atomicVM.AtomicMempool.AddRemoteTx(exportTx2); err == nil {
		t.Fatal("Should have failed to add because conflicting")
	}

	// Manually add transaction to mempool to bypass validation
	if err := tvm.atomicVM.AtomicMempool.ForceAddTx(exportTx2); err != nil {
		t.Fatal(err)
	}
	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	_, err = tvm.vm.BuildBlock(context.Background())
	if err == nil {
		t.Fatal("BuildBlock should have returned an error due to invalid export transaction")
	}
}

func TestBuildInvalidBlockHead(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testBuildInvalidBlockHead(t, scheme)
		})
	}
}

func testBuildInvalidBlockHead(t *testing.T, scheme string) {
	fork := upgradetest.ApricotPhase1
	tvm := newVM(t, testVMConfig{
		fork:       &fork,
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	key0 := testKeys[0]
	addr0 := key0.Address()

	// Create the transaction
	utx := &atomic.UnsignedImportTx{
		NetworkID:    tvm.vm.ctx.NetworkID,
		BlockchainID: tvm.vm.ctx.ChainID,
		Outs: []atomic.EVMOutput{{
			Address: common.Address(addr0),
			Amount:  1 * units.Avax,
			AssetID: tvm.vm.ctx.AVAXAssetID,
		}},
		ImportedInputs: []*avax.TransferableInput{
			{
				Asset: avax.Asset{ID: tvm.vm.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1 * units.Avax,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			},
		},
		SourceChain: tvm.vm.ctx.XChainID,
	}
	tx := &atomic.Tx{UnsignedAtomicTx: utx}
	if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key0}}); err != nil {
		t.Fatal(err)
	}

	currentBlock := tvm.vm.blockChain.CurrentBlock()

	// Verify that the transaction fails verification when attempting to issue
	// it into the atomic mempool.
	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(tx); err == nil {
		t.Fatal("Should have failed to issue invalid transaction")
	}
	// Force issue the transaction directly to the mempool
	if err := tvm.atomicVM.AtomicMempool.ForceAddTx(tx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	if _, err := tvm.vm.BuildBlock(context.Background()); err == nil {
		t.Fatalf("Unexpectedly created a block")
	}

	newCurrentBlock := tvm.vm.blockChain.CurrentBlock()

	if currentBlock.Hash() != newCurrentBlock.Hash() {
		t.Fatal("current block changed")
	}
}

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 4 ruleset in genesis.
func TestBuildApricotPhase4Block(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testBuildApricotPhase4Block(t, scheme)
		})
	}
}

func testBuildApricotPhase4Block(t *testing.T, scheme string) {
	fork := upgradetest.ApricotPhase4
	tvm := newVM(t, testVMConfig{
		fork:       &fork,
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importAmount := uint64(1000000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: tvm.vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].Address()},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := tvm.atomicMemory.NewSharedMemory(tvm.vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{tvm.vm.ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	ethBlk := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	if eBlockGasCost := customtypes.BlockGasCost(ethBlk); eBlockGasCost == nil || eBlockGasCost.Cmp(common.Big0) != 0 {
		t.Fatalf("expected blockGasCost to be 0 but got %d", eBlockGasCost)
	}
	if eExtDataGasUsed := customtypes.BlockExtDataGasUsed(ethBlk); eExtDataGasUsed == nil || eExtDataGasUsed.Cmp(big.NewInt(1230)) != 0 {
		t.Fatalf("expected extDataGasUsed to be 1000 but got %d", eExtDataGasUsed)
	}
	minRequiredTip, err := header.EstimateRequiredTip(tvm.vm.chainConfigExtra(), ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(common.Big0) != 0 {
		t.Fatalf("expected minRequiredTip to be 0 but got %d", minRequiredTip)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 5; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	for i := 5; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap1.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := tvm.vm.txPool.AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk, err = tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	ethBlk = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	if customtypes.BlockGasCost(ethBlk) == nil || customtypes.BlockGasCost(ethBlk).Cmp(big.NewInt(100)) < 0 {
		t.Fatalf("expected blockGasCost to be at least 100 but got %d", customtypes.BlockGasCost(ethBlk))
	}
	if customtypes.BlockExtDataGasUsed(ethBlk) == nil || customtypes.BlockExtDataGasUsed(ethBlk).Cmp(common.Big0) != 0 {
		t.Fatalf("expected extDataGasUsed to be 0 but got %d", customtypes.BlockExtDataGasUsed(ethBlk))
	}
	minRequiredTip, err = header.EstimateRequiredTip(tvm.vm.chainConfigExtra(), ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(big.NewInt(0.05*utils.GWei)) < 0 {
		t.Fatalf("expected minRequiredTip to be at least 0.05 gwei but got %d", minRequiredTip)
	}

	lastAcceptedID, err := tvm.vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}

	// Confirm all txs are present
	ethBlkTxs := tvm.vm.blockChain.GetBlockByNumber(2).Transactions()
	for i, tx := range txs {
		if len(ethBlkTxs) <= i {
			t.Fatalf("missing transactions expected: %d but found: %d", len(txs), len(ethBlkTxs))
		}
		if ethBlkTxs[i].Hash() != tx.Hash() {
			t.Fatalf("expected tx at index %d to have hash: %x but has: %x", i, txs[i].Hash(), tx.Hash())
		}
	}
}

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 5 ruleset in genesis.
func TestBuildApricotPhase5Block(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testBuildApricotPhase5Block(t, scheme)
		})
	}
}

func testBuildApricotPhase5Block(t *testing.T, scheme string) {
	fork := upgradetest.ApricotPhase5
	tvm := newVM(t, testVMConfig{
		fork:       &fork,
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importAmount := uint64(1000000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: tvm.vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].Address()},
			},
		},
	}
	utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := tvm.atomicMemory.NewSharedMemory(tvm.vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{tvm.vm.ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	ethBlk := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	if eBlockGasCost := customtypes.BlockGasCost(ethBlk); eBlockGasCost == nil || eBlockGasCost.Cmp(common.Big0) != 0 {
		t.Fatalf("expected blockGasCost to be 0 but got %d", eBlockGasCost)
	}
	if eExtDataGasUsed := customtypes.BlockExtDataGasUsed(ethBlk); eExtDataGasUsed == nil || eExtDataGasUsed.Cmp(big.NewInt(11230)) != 0 {
		t.Fatalf("expected extDataGasUsed to be 11230 but got %d", eExtDataGasUsed)
	}
	minRequiredTip, err := header.EstimateRequiredTip(tvm.vm.chainConfigExtra(), ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(common.Big0) != 0 {
		t.Fatalf("expected minRequiredTip to be 0 but got %d", minRequiredTip)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(3*ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := tvm.vm.txPool.Add(txs, false, false)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk, err = tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	ethBlk = blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	if customtypes.BlockGasCost(ethBlk) == nil || customtypes.BlockGasCost(ethBlk).Cmp(big.NewInt(100)) < 0 {
		t.Fatalf("expected blockGasCost to be at least 100 but got %d", customtypes.BlockGasCost(ethBlk))
	}
	if customtypes.BlockExtDataGasUsed(ethBlk) == nil || customtypes.BlockExtDataGasUsed(ethBlk).Cmp(common.Big0) != 0 {
		t.Fatalf("expected extDataGasUsed to be 0 but got %d", customtypes.BlockExtDataGasUsed(ethBlk))
	}
	minRequiredTip, err = header.EstimateRequiredTip(tvm.vm.chainConfigExtra(), ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(big.NewInt(0.05*utils.GWei)) < 0 {
		t.Fatalf("expected minRequiredTip to be at least 0.05 gwei but got %d", minRequiredTip)
	}

	lastAcceptedID, err := tvm.vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}

	// Confirm all txs are present
	ethBlkTxs := tvm.vm.blockChain.GetBlockByNumber(2).Transactions()
	for i, tx := range txs {
		if len(ethBlkTxs) <= i {
			t.Fatalf("missing transactions expected: %d but found: %d", len(txs), len(ethBlkTxs))
		}
		if ethBlkTxs[i].Hash() != tx.Hash() {
			t.Fatalf("expected tx at index %d to have hash: %x but has: %x", i, txs[i].Hash(), tx.Hash())
		}
	}
}

// This is a regression test to ensure that if two consecutive atomic transactions fail verification
// in onFinalizeAndAssemble it will not cause a panic due to calling RevertToSnapshot(revID) on the
// same revision ID twice.
func TestConsecutiveAtomicTransactionsRevertSnapshot(t *testing.T) {
	fork := upgradetest.ApricotPhase1
	tvm := newVM(t, testVMConfig{
		fork: &fork,
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	// Create three conflicting import transactions
	importTxs := createImportTxOptions(t, tvm.atomicVM, tvm.atomicMemory)

	// Issue the first import transaction, build, and accept the block.
	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTxs[0]); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := tvm.vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Add the two conflicting transactions directly to the mempool, so that two consecutive transactions
	// will fail verification when build block is called.
	tvm.atomicVM.AtomicMempool.AddRemoteTx(importTxs[1])
	tvm.atomicVM.AtomicMempool.AddRemoteTx(importTxs[2])

	if _, err := tvm.vm.BuildBlock(context.Background()); err == nil {
		t.Fatal("Expected build block to fail due to empty block")
	}
}

func TestAtomicTxBuildBlockDropsConflicts(t *testing.T) {
	importAmount := uint64(10000000)
	fork := upgradetest.ApricotPhase5
	tvm := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
			testShortIDAddrs[1]: importAmount,
			testShortIDAddrs[2]: importAmount,
		},
	})
	conflictKey := utilstest.NewKey(t)
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	// Create a conflict set for each pair of transactions
	conflictSets := make([]set.Set[ids.ID], len(testKeys))
	for index, key := range testKeys {
		importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[index], initialBaseFee, []*secp256k1.PrivateKey{key})
		if err != nil {
			t.Fatal(err)
		}
		if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
			t.Fatal(err)
		}
		conflictSets[index].Add(importTx.ID())
		conflictTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, conflictKey.Address, initialBaseFee, []*secp256k1.PrivateKey{key})
		if err != nil {
			t.Fatal(err)
		}
		if err := tvm.atomicVM.AtomicMempool.AddLocalTx(conflictTx); err == nil {
			t.Fatal("should conflict with the utxoSet in the mempool")
		}
		// force add the tx
		tvm.atomicVM.AtomicMempool.ForceAddTx(conflictTx)
		conflictSets[index].Add(conflictTx.ID())
	}
	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))
	// Note: this only checks the path through OnFinalizeAndAssemble, we should make sure to add a test
	// that verifies blocks received from the network will also fail verification
	blk, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	atomicTxs := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetBlockExtension().(atomic.AtomicBlockContext).AtomicTxs()
	assert.True(t, len(atomicTxs) == len(testKeys), "Conflict transactions should be out of the batch")
	atomicTxIDs := set.Set[ids.ID]{}
	for _, tx := range atomicTxs {
		atomicTxIDs.Add(tx.ID())
	}

	// Check that removing the txIDs actually included in the block from each conflict set
	// leaves one item remaining for each conflict set ie. only one tx from each conflict set
	// has been included in the block.
	for _, conflictSet := range conflictSets {
		conflictSet.Difference(atomicTxIDs)
		assert.Equal(t, 1, conflictSet.Len())
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBuildBlockDoesNotExceedAtomicGasLimit(t *testing.T) {
	importAmount := uint64(10000000)
	fork := upgradetest.ApricotPhase5
	tvm := newVM(t, testVMConfig{
		fork: &fork,
	})
	defer func() {
		if err := tvm.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	kc := secp256k1fx.NewKeychain()
	kc.Add(testKeys[0])
	txID, err := ids.ToID(hashing.ComputeHash256(testShortIDAddrs[0][:]))
	assert.NoError(t, err)

	mempoolTxs := 200
	for i := 0; i < mempoolTxs; i++ {
		utxo, err := addUTXO(tvm.atomicMemory, tvm.vm.ctx, txID, uint32(i), tvm.vm.ctx.AVAXAssetID, importAmount, testShortIDAddrs[0])
		assert.NoError(t, err)

		importTx, err := atomic.NewImportTx(tvm.vm.ctx, tvm.vm.currentRules(), tvm.vm.clock.Unix(), tvm.vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, kc, []*avax.UTXO{utxo})
		if err != nil {
			t.Fatal(err)
		}
		if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
			t.Fatal(err)
		}
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))
	blk, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	atomicTxs := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetBlockExtension().(atomic.AtomicBlockContext).AtomicTxs()

	// Need to ensure that not all of the transactions in the mempool are included in the block.
	// This ensures that we hit the atomic gas limit while building the block before we hit the
	// upper limit on the size of the codec for marshalling the atomic transactions.
	if len(atomicTxs) >= mempoolTxs {
		t.Fatalf("Expected number of atomic transactions included in the block (%d) to be less than the number of transactions added to the mempool (%d)", len(atomicTxs), mempoolTxs)
	}
}

func TestExtraStateChangeAtomicGasLimitExceeded(t *testing.T) {
	importAmount := uint64(10000000)
	// We create two VMs one in ApriotPhase4 and one in ApricotPhase5, so that we can construct a block
	// containing a large enough atomic transaction that it will exceed the atomic gas limit in
	// ApricotPhase5.
	ap4 := upgradetest.ApricotPhase4
	tvm1 := newVM(t, testVMConfig{
		fork: &ap4,
	})
	ap5 := upgradetest.ApricotPhase5
	tvm2 := newVM(t, testVMConfig{
		fork: &ap5,
	})
	defer func() {
		if err := tvm1.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := tvm2.vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	kc := secp256k1fx.NewKeychain()
	kc.Add(testKeys[0])
	txID, err := ids.ToID(hashing.ComputeHash256(testShortIDAddrs[0][:]))
	assert.NoError(t, err)

	// Add enough UTXOs, such that the created import transaction will attempt to consume more gas than allowed
	// in ApricotPhase5.
	for i := 0; i < 100; i++ {
		_, err := addUTXO(tvm1.atomicMemory, tvm1.vm.ctx, txID, uint32(i), tvm1.vm.ctx.AVAXAssetID, importAmount, testShortIDAddrs[0])
		assert.NoError(t, err)

		_, err = addUTXO(tvm2.atomicMemory, tvm2.vm.ctx, txID, uint32(i), tvm2.vm.ctx.AVAXAssetID, importAmount, testShortIDAddrs[0])
		assert.NoError(t, err)
	}

	// Double the initial base fee used when estimating the cost of this transaction to ensure that when it is
	// used in ApricotPhase5 it still pays a sufficient fee with the fixed fee per atomic transaction.
	importTx, err := tvm1.atomicVM.NewImportTx(tvm1.vm.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(common.Big2, initialBaseFee), []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}
	if err := tvm1.atomicVM.AtomicMempool.ForceAddTx(importTx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm1.WaitForEvent(context.Background()))
	blk1, err := tvm1.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := blk1.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	validEthBlock := blk1.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()

	extraData, err := atomic.Codec.Marshal(atomic.CodecVersion, []*atomic.Tx{importTx})
	if err != nil {
		t.Fatal(err)
	}

	// Construct the new block with the extra data in the new format (slice of atomic transactions).
	ethBlk2 := customtypes.NewBlockWithExtData(
		types.CopyHeader(validEthBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	state, err := tvm2.vm.blockChain.State()
	if err != nil {
		t.Fatal(err)
	}

	// Hack: test [onExtraStateChange] directly to ensure it catches the atomic gas limit error correctly.
	onExtraStateChangeFn := tvm2.vm.extensionConfig.ConsensusCallbacks.OnExtraStateChange
	if _, _, err := onExtraStateChangeFn(ethBlk2, nil, state); err == nil || !strings.Contains(err.Error(), "exceeds atomic gas limit") {
		t.Fatalf("Expected block to fail verification due to exceeded atomic gas limit, but found error: %v", err)
	}
}

func TestSkipChainConfigCheckCompatible(t *testing.T) {
	importAmount := uint64(50000000)
	fork := upgradetest.Durango
	tvm := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
	})
	defer func() { require.NoError(t, tvm.vm.Shutdown(context.Background())) }()

	// Since rewinding is permitted for last accepted height of 0, we must
	// accept one block to test the SkipUpgradeCheck functionality.
	importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	require.NoError(t, err)
	require.NoError(t, tvm.atomicVM.AtomicMempool.AddLocalTx(importTx))
	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	blk, err := tvm.vm.BuildBlock(context.Background())
	require.NoError(t, err)
	require.NoError(t, blk.Verify(context.Background()))
	require.NoError(t, tvm.vm.SetPreference(context.Background(), blk.ID()))
	require.NoError(t, blk.Accept(context.Background()))

	reinitVM := atomicvm.WrapVM(&VM{})
	// use the block's timestamp instead of 0 since rewind to genesis
	// is hardcoded to be allowed in core/genesis.go.
	newCTX := snowtest.Context(t, tvm.vm.ctx.ChainID)
	upgradetest.SetTimesTo(&newCTX.NetworkUpgrades, upgradetest.Latest, upgrade.UnscheduledActivationTime)
	upgradetest.SetTimesTo(&newCTX.NetworkUpgrades, fork+1, blk.Timestamp())
	upgradetest.SetTimesTo(&newCTX.NetworkUpgrades, fork, upgrade.InitiallyActiveTime)
	genesis := []byte(genesisJSON(forkToChainConfig[fork]))
	err = reinitVM.Initialize(context.Background(), newCTX, tvm.db, genesis, []byte{}, []byte{}, []*commonEng.Fx{}, tvm.appSender)
	require.ErrorContains(t, err, "mismatching Cancun fork timestamp in database")

	reinitVM = atomicvm.WrapVM(&VM{})
	newCTX.Metrics = metrics.NewPrefixGatherer()

	// try again with skip-upgrade-check
	config := []byte(`{"skip-upgrade-check": true}`)
	require.NoError(t, reinitVM.Initialize(
		context.Background(),
		newCTX,
		tvm.db,
		genesis,
		[]byte{},
		config,
		[]*commonEng.Fx{},
		tvm.appSender))
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
			importAmount := uint64(1000000000)
			tvm := newVM(t, testVMConfig{
				fork: &test.fork,
				utxos: map[ids.ShortID]uint64{
					testShortIDAddrs[0]: importAmount,
				},
			})
			defer func() {
				if err := tvm.vm.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
			}()

			importTx, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
			if err != nil {
				t.Fatal(err)
			}

			if err := tvm.atomicVM.AtomicMempool.AddLocalTx(importTx); err != nil {
				t.Fatal(err)
			}

			require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

			blk, err := tvm.vm.BuildBlock(context.Background())
			if err != nil {
				t.Fatalf("Failed to build block with import transaction: %s", err)
			}

			// Modify the block to have a parent beacon root
			ethBlock := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
			header := types.CopyHeader(ethBlock.Header())
			header.ParentBeaconRoot = test.beaconRoot
			parentBeaconEthBlock := customtypes.NewBlockWithExtData(
				header,
				nil,
				nil,
				nil,
				new(trie.Trie),
				customtypes.BlockExtData(ethBlock),
				false,
			)

			parentBeaconBlock, err := wrapBlock(parentBeaconEthBlock, tvm.vm)
			if err != nil {
				t.Fatal(err)
			}

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

			_, err = tvm.vm.ParseBlock(context.Background(), parentBeaconBlock.Bytes())
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
			Gas:        params.TxGas,
			To:         testEthAddrs[0],
			BlobFeeCap: uint256.NewInt(1),
			BlobHashes: []common.Hash{{1}}, // This blob is expected to cause verification to fail
			Value:      new(uint256.Int),
		}), signer, testKeys[0].ToECDSA())
		require.NoError(err)
		b.AddTx(tx)
	}
	// FullFaker used to skip header verification so we can generate a block with blobs
	_, blocks, _, err := core.GenerateChainWithGenesis(gspec, dummy.NewFullFaker(), 1, 10, blockGen)
	require.NoError(err)

	// Create a VM with the genesis (will use header verification)
	vm := newVM(t, testVMConfig{
		genesisJSON: genesisJSONCancun,
	}).vm
	defer func() { require.NoError(vm.Shutdown(ctx)) }()

	// Verification should fail
	vmBlock, err := wrapBlock(blocks[0], vm)
	require.NoError(err)
	_, err = vm.ParseBlock(ctx, vmBlock.Bytes())
	require.ErrorContains(err, "blobs not enabled on avalanche networks")
	err = vmBlock.Verify(ctx)
	require.ErrorContains(err, "blobs not enabled on avalanche networks")
}

func TestBuildBlockWithInsufficientCapacity(t *testing.T) {
	ctx := context.Background()
	require := require.New(t)

	importAmount := uint64(2_000_000_000_000_000) // 2M AVAX
	fork := upgradetest.Fortuna
	tvm := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
		},
	})
	defer func() {
		require.NoError(tvm.vm.Shutdown(ctx))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

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
		txs[i], err = types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainID), testKeys[0].ToECDSA())
		require.NoError(err)
	}

	errs := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{txs[0]})
	require.Len(errs, 1)
	require.NoError(errs[0])

	require.Equal(commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))
	blk2, err := tvm.vm.BuildBlock(ctx)
	require.NoError(err)

	require.NoError(blk2.Verify(ctx))
	require.NoError(blk2.Accept(ctx))

	// Attempt to build a block consuming more than the current gas capacity
	errs = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{txs[1]})
	require.Len(errs, 1)
	require.NoError(errs[0])

	require.Equal(commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))
	// Expect block building to fail due to insufficient gas capacity
	_, err = tvm.vm.BuildBlock(ctx)
	require.ErrorIs(err, miner.ErrInsufficientGasCapacityToBuild)

	// Wait to fill block capacity and retry block builiding
	tvm.vm.clock.Set(tvm.vm.clock.Time().Add(acp176.TimeToFillCapacity * time.Second))

	require.Equal(commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))
	blk3, err := tvm.vm.BuildBlock(ctx)
	require.NoError(err)

	require.NoError(blk3.Verify(ctx))
	require.NoError(blk3.Accept(ctx))
}

func TestBuildBlockLargeTxStarvation(t *testing.T) {
	ctx := context.Background()
	require := require.New(t)

	importAmount := uint64(2_000_000_000_000_000) // 2M AVAX
	fork := upgradetest.Fortuna
	tvm := newVM(t, testVMConfig{
		fork: &fork,
		utxos: map[ids.ShortID]uint64{
			testShortIDAddrs[0]: importAmount,
			testShortIDAddrs[1]: importAmount,
		},
	})
	defer func() {
		require.NoError(tvm.vm.Shutdown(ctx))
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	tvm.vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	importTx1, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	require.NoError(err)
	require.NoError(tvm.atomicVM.AtomicMempool.AddLocalTx(importTx1))

	importTx2, err := tvm.atomicVM.NewImportTx(tvm.vm.ctx.XChainID, testEthAddrs[1], initialBaseFee, []*secp256k1.PrivateKey{testKeys[1]})
	require.NoError(err)
	require.NoError(tvm.atomicVM.AtomicMempool.AddLocalTx(importTx2))

	require.Equal(commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))
	blk1, err := tvm.vm.BuildBlock(ctx)
	require.NoError(err)

	require.NoError(blk1.Verify(ctx))
	require.NoError(tvm.vm.SetPreference(ctx, blk1.ID()))
	require.NoError(blk1.Accept(ctx))

	newHead := <-newTxPoolHeadChan
	require.Equal(common.Hash(blk1.ID()), newHead.Head.Hash())

	// Build a block consuming all of the available gas
	var (
		highGasPrice = big.NewInt(2 * ap0.MinGasPrice)
		lowGasPrice  = big.NewInt(ap0.MinGasPrice)
	)

	// Refill capacity after distributing funds with import transactions
	tvm.vm.clock.Set(tvm.vm.clock.Time().Add(acp176.TimeToFillCapacity * time.Second))
	maxSizeTxs := make([]*types.Transaction, 2)
	for i := uint64(0); i < 2; i++ {
		tx := types.NewContractCreation(
			i,
			big.NewInt(0),
			acp176.MinMaxCapacity,
			highGasPrice,
			[]byte{0xfe}, // invalid opcode consumes all gas
		)
		maxSizeTxs[i], err = types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainID), testKeys[0].ToECDSA())
		require.NoError(err)
	}

	errs := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{maxSizeTxs[0]})
	require.Len(errs, 1)
	require.NoError(errs[0])

	require.Equal(commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))
	blk2, err := tvm.vm.BuildBlock(ctx)
	require.NoError(err)

	require.NoError(blk2.Verify(ctx))
	require.NoError(blk2.Accept(ctx))

	// Add a second transaction trying to consume the max guaranteed gas capacity at a higher gas price
	errs = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{maxSizeTxs[1]})
	require.Len(errs, 1)
	require.NoError(errs[0])

	// Build a smaller transaction that consumes less gas at a lower price. Block building should
	// fail and enforce waiting for more capacity to avoid starving the larger transaction.
	tx := types.NewContractCreation(0, big.NewInt(0), 2_000_000, lowGasPrice, []byte{0xfe})
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(tvm.vm.chainID), testKeys[1].ToECDSA())
	require.NoError(err)
	errs = tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	require.Len(errs, 1)
	require.NoError(errs[0])

	require.Equal(commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))
	_, err = tvm.vm.BuildBlock(ctx)
	require.ErrorIs(err, miner.ErrInsufficientGasCapacityToBuild)

	// Wait to fill block capacity and retry block building
	tvm.vm.clock.Set(tvm.vm.clock.Time().Add(acp176.TimeToFillCapacity * time.Second))
	require.Equal(commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))
	blk4, err := tvm.vm.BuildBlock(ctx)
	require.NoError(err)
	ethBlk4 := blk4.(*chain.BlockWrapper).Block.(*wrappedBlock).ethBlock
	actualTxs := ethBlk4.Transactions()
	require.Len(actualTxs, 1)
	require.Equal(maxSizeTxs[1].Hash(), actualTxs[0].Hash())

	require.NoError(blk4.Verify(ctx))
	require.NoError(blk4.Accept(ctx))
}

func TestWaitForEvent(t *testing.T) {
	populateAtomicMemory := func(t *testing.T, tvm *testVM) (common.Address, *ecdsa.PrivateKey) {
		key := testKeys[0].ToECDSA()
		address := testEthAddrs[0]

		importAmount := uint64(1000000000)
		utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

		utxo := &avax.UTXO{
			UTXOID: utxoID,
			Asset:  avax.Asset{ID: tvm.vm.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: importAmount,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{testKeys[0].Address()},
				},
			},
		}
		utxoBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, utxo)
		require.NoError(t, err)

		xChainSharedMemory := tvm.atomicMemory.NewSharedMemory(tvm.vm.ctx.XChainID)
		inputID := utxo.InputID()
		require.NoError(t, xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{tvm.vm.ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
			Key:   inputID[:],
			Value: utxoBytes,
			Traits: [][]byte{
				testKeys[0].Address().Bytes(),
			},
		}}}}))

		return address, key
	}

	for _, testCase := range []struct {
		name     string
		testCase func(*testing.T, *VM, *atomicvm.VM, common.Address, *ecdsa.PrivateKey)
	}{
		{
			name: "WaitForEvent with context cancelled returns 0",
			testCase: func(t *testing.T, vm *VM, _ *atomicvm.VM, address common.Address, key *ecdsa.PrivateKey) {
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
			testCase: func(t *testing.T, vm *VM, atomicVM *atomicvm.VM, address common.Address, key *ecdsa.PrivateKey) {
				importTx, err := atomicVM.NewImportTx(vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
				require.NoError(t, err)

				var wg sync.WaitGroup
				wg.Add(1)

				go func() {
					defer wg.Done()
					msg, err := vm.WaitForEvent(context.Background())
					require.NoError(t, err)
					require.Equal(t, commonEng.PendingTxs, msg)
				}()

				require.NoError(t, atomicVM.AtomicMempool.AddLocalTx(importTx))

				wg.Wait()
			},
		},
		{
			name: "WaitForEvent doesn't return once a block is built and accepted",
			testCase: func(t *testing.T, vm *VM, atomicVM *atomicvm.VM, address common.Address, key *ecdsa.PrivateKey) {
				importTx, err := atomicVM.NewImportTx(vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
				require.NoError(t, err)

				require.NoError(t, atomicVM.AtomicMempool.AddLocalTx(importTx))

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

				txs := make([]*types.Transaction, 10)
				for i := 0; i < 10; i++ {
					tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(3*ap0.MinGasPrice), nil)
					signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
					require.NoError(t, err)

					txs[i] = signedTx
				}
				errs := vm.txPool.Add(txs, false, false)
				for _, err := range errs {
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
			testCase: func(t *testing.T, vm *VM, atomicVM *atomicvm.VM, address common.Address, key *ecdsa.PrivateKey) {
				importTx, err := atomicVM.NewImportTx(vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
				require.NoError(t, err)

				require.NoError(t, atomicVM.AtomicMempool.AddLocalTx(importTx))

				lastBuildBlockTime := time.Now()

				blk, err := vm.BuildBlock(context.Background())
				require.NoError(t, err)

				require.NoError(t, blk.Verify(context.Background()))

				require.NoError(t, vm.SetPreference(context.Background(), blk.ID()))

				require.NoError(t, blk.Accept(context.Background()))

				txs := make([]*types.Transaction, 10)
				for i := 0; i < 10; i++ {
					tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(3*ap0.MinGasPrice), nil)
					signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
					require.NoError(t, err)

					txs[i] = signedTx
				}
				errs := vm.txPool.Add(txs, false, false)
				for _, err := range errs {
					require.NoError(t, err)
				}

				var wg sync.WaitGroup
				wg.Add(1)

				go func() {
					defer wg.Done()
					msg, err := vm.WaitForEvent(context.Background())
					require.NoError(t, err)
					require.Equal(t, commonEng.PendingTxs, msg)
					require.GreaterOrEqual(t, time.Since(lastBuildBlockTime), minBlockBuildingRetryDelay)
				}()

				wg.Wait()
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fork := upgradetest.Latest
			tvm := newVM(t, testVMConfig{
				fork: &fork,
			})
			address, key := populateAtomicMemory(t, tvm)
			testCase.testCase(t, tvm.vm, tvm.atomicVM, address, key)
			tvm.vm.Shutdown(context.Background())
		})
	}
}
