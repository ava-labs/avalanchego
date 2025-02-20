// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
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
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/holiman/uint256"

	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/eth/filters"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/metrics"
	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	constantsEng "github.com/ava-labs/avalanchego/utils/constants"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/rpc"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	accountKeystore "github.com/ava-labs/coreth/accounts/keystore"
)

var (
	testNetworkID    uint32 = 10
	testCChainID            = ids.ID{'c', 'c', 'h', 'a', 'i', 'n', 't', 'e', 's', 't'}
	testXChainID            = ids.ID{'t', 'e', 's', 't', 'x'}
	nonExistentID           = ids.ID{'F'}
	testKeys         []*secp256k1.PrivateKey
	testEthAddrs     []common.Address // testEthAddrs[i] corresponds to testKeys[i]
	testShortIDAddrs []ids.ShortID
	testAvaxAssetID  = ids.ID{1, 2, 3}

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
		if cfg.IsDurango(0) {
			addr := common.HexToAddress("0x99b9DEA54C48Dfea6aA9A4Ca4623633EE04ddbB5")
			balance := new(big.Int).Mul(big.NewInt(params.Ether), big.NewInt(10))
			g.Alloc[addr] = types.GenesisAccount{Balance: balance}
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

	genesisJSONApricotPhase0     = genesisJSON(params.TestLaunchConfig)
	genesisJSONApricotPhase1     = genesisJSON(params.TestApricotPhase1Config)
	genesisJSONApricotPhase2     = genesisJSON(params.TestApricotPhase2Config)
	genesisJSONApricotPhase3     = genesisJSON(params.TestApricotPhase3Config)
	genesisJSONApricotPhase4     = genesisJSON(params.TestApricotPhase4Config)
	genesisJSONApricotPhase5     = genesisJSON(params.TestApricotPhase5Config)
	genesisJSONApricotPhasePre6  = genesisJSON(params.TestApricotPhasePre6Config)
	genesisJSONApricotPhase6     = genesisJSON(params.TestApricotPhase6Config)
	genesisJSONApricotPhasePost6 = genesisJSON(params.TestApricotPhasePost6Config)
	genesisJSONBanff             = genesisJSON(params.TestBanffChainConfig)
	genesisJSONCortina           = genesisJSON(params.TestCortinaChainConfig)
	genesisJSONDurango           = genesisJSON(params.TestDurangoChainConfig)
	genesisJSONEtna              = genesisJSON(params.TestEtnaChainConfig)
	genesisJSONFUpgrade          = genesisJSON(params.TestFUpgradeChainConfig)
	genesisJSONLatest            = genesisJSONFUpgrade

	genesisJSONCancun = genesisJSON(activateCancun(params.TestChainConfig))

	apricotRulesPhase0 = params.Rules{}
	apricotRulesPhase1 = params.Rules{AvalancheRules: params.AvalancheRules{IsApricotPhase1: true}}
	apricotRulesPhase2 = params.Rules{AvalancheRules: params.AvalancheRules{IsApricotPhase1: true, IsApricotPhase2: true}}
	apricotRulesPhase3 = params.Rules{AvalancheRules: params.AvalancheRules{IsApricotPhase1: true, IsApricotPhase2: true, IsApricotPhase3: true}}
	apricotRulesPhase4 = params.Rules{AvalancheRules: params.AvalancheRules{IsApricotPhase1: true, IsApricotPhase2: true, IsApricotPhase3: true, IsApricotPhase4: true}}
	apricotRulesPhase5 = params.Rules{AvalancheRules: params.AvalancheRules{IsApricotPhase1: true, IsApricotPhase2: true, IsApricotPhase3: true, IsApricotPhase4: true, IsApricotPhase5: true}}
	apricotRulesPhase6 = params.Rules{AvalancheRules: params.AvalancheRules{IsApricotPhase1: true, IsApricotPhase2: true, IsApricotPhase3: true, IsApricotPhase4: true, IsApricotPhase5: true, IsApricotPhasePre6: true, IsApricotPhase6: true, IsApricotPhasePost6: true}}
	banffRules         = params.Rules{AvalancheRules: params.AvalancheRules{IsApricotPhase1: true, IsApricotPhase2: true, IsApricotPhase3: true, IsApricotPhase4: true, IsApricotPhase5: true, IsApricotPhasePre6: true, IsApricotPhase6: true, IsApricotPhasePost6: true, IsBanff: true}}
	// cortinaRules    = params.Rules{AvalancheRules: params.AvalancheRules{IsApricotPhase1: true, IsApricotPhase2: true, IsApricotPhase3: true, IsApricotPhase4: true, IsApricotPhase5: true, IsApricotPhasePre6: true, IsApricotPhase6: true, IsApricotPhasePost6: true, IsBanff: true, IsCortina: true}}
)

func init() {
	var b []byte

	for _, key := range []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
	} {
		b, _ = cb58.Decode(key)
		pk, _ := secp256k1.ToPrivateKey(b)
		testKeys = append(testKeys, pk)
		testEthAddrs = append(testEthAddrs, pk.EthAddress())
		testShortIDAddrs = append(testShortIDAddrs, pk.Address())
	}
}

func newPrefundedGenesis(
	balance int,
	addresses ...common.Address,
) *core.Genesis {
	alloc := types.GenesisAlloc{}
	for _, address := range addresses {
		alloc[address] = types.GenesisAccount{
			Balance: big.NewInt(int64(balance)),
		}
	}

	return &core.Genesis{
		Config:     params.TestChainConfig,
		Difficulty: big.NewInt(0),
		Alloc:      alloc,
	}
}

// BuildGenesisTest returns the genesis bytes for Coreth VM to be used in testing
func BuildGenesisTest(t *testing.T, genesisJSON string) []byte {
	ss := StaticService{}

	genesis := &core.Genesis{}
	if err := json.Unmarshal([]byte(genesisJSON), genesis); err != nil {
		t.Fatalf("Problem unmarshaling genesis JSON: %s", err)
	}
	genesisReply, err := ss.BuildGenesis(nil, genesis)
	if err != nil {
		t.Fatalf("Failed to create test genesis")
	}
	genesisBytes, err := formatting.Decode(genesisReply.Encoding, genesisReply.Bytes)
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
	ctx.SharedMemory = testSharedMemory()
	aliaser := ctx.BCLookup.(ids.Aliaser)
	_ = aliaser.Alias(testCChainID, "C")
	_ = aliaser.Alias(testCChainID, testCChainID.String())
	_ = aliaser.Alias(testXChainID, "X")
	_ = aliaser.Alias(testXChainID, testXChainID.String())
	ctx.ValidatorState = &validatorstest.State{
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			subnetID, ok := map[ids.ID]ids.ID{
				constantsEng.PlatformChainID: constantsEng.PrimaryNetworkID,
				testXChainID:                 constantsEng.PrimaryNetworkID,
				testCChainID:                 constantsEng.PrimaryNetworkID,
			}[chainID]
			if !ok {
				return ids.Empty, errors.New("unknown chain")
			}
			return subnetID, nil
		},
	}
	blsSecretKey, err := localsigner.New()
	if err != nil {
		panic(err)
	}
	ctx.WarpSigner = avalancheWarp.NewSigner(blsSecretKey, ctx.NetworkID, ctx.ChainID)
	ctx.PublicKey = blsSecretKey.PublicKey()
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
	*avalancheatomic.Memory,
) {
	if len(genesisJSON) == 0 {
		genesisJSON = genesisJSONLatest
	}
	genesisBytes := BuildGenesisTest(t, genesisJSON)
	ctx := NewContext()

	baseDB := memdb.New()

	// initialize the atomic memory
	atomicMemory := avalancheatomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = atomicMemory.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	issuer := make(chan commonEng.Message, 1)
	prefixedDB := prefixdb.New([]byte{1}, baseDB)
	return ctx, prefixedDB, genesisBytes, issuer, atomicMemory
}

// GenesisVM creates a VM instance with the genesis test bytes and returns
// the channel use to send messages to the engine, the VM, database manager,
// sender, and atomic memory.
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
	*avalancheatomic.Memory,
	*enginetest.Sender,
) {
	return GenesisVMWithClock(t, finishBootstrapping, genesisJSON, configJSON, upgradeJSON, mockable.Clock{})
}

// GenesisVMWithClock creates a VM instance as GenesisVM does, but also allows
// setting the vm's time before [Initialize] is called.
func GenesisVMWithClock(
	t *testing.T,
	finishBootstrapping bool,
	genesisJSON string,
	configJSON string,
	upgradeJSON string,
	clock mockable.Clock,
) (
	chan commonEng.Message,
	*VM,
	database.Database,
	*avalancheatomic.Memory,
	*enginetest.Sender,
) {
	vm := &VM{clock: clock}
	ctx, dbManager, genesisBytes, issuer, m := setupGenesis(t, genesisJSON)
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

	return issuer, vm, dbManager, m, appSender
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

// GenesisVMWithUTXOs creates a GenesisVM and generates UTXOs in the X-Chain Shared Memory containing AVAX based on the [utxos] map
// Generates UTXOIDs by using a hash of the address in the [utxos] map such that the UTXOs will be generated deterministically.
// If [genesisJSON] is empty, defaults to using [genesisJSONLatest]
func GenesisVMWithUTXOs(t *testing.T, finishBootstrapping bool, genesisJSON string, configJSON string, upgradeJSON string, utxos map[ids.ShortID]uint64) (chan commonEng.Message, *VM, database.Database, *avalancheatomic.Memory, *enginetest.Sender) {
	issuer, vm, db, sharedMemory, sender := GenesisVM(t, finishBootstrapping, genesisJSON, configJSON, upgradeJSON)
	for addr, avaxAmount := range utxos {
		txID, err := ids.ToID(hashing.ComputeHash256(addr.Bytes()))
		if err != nil {
			t.Fatalf("Failed to generate txID from addr: %s", err)
		}
		if _, err := addUTXO(sharedMemory, vm.ctx, txID, 0, vm.ctx.AVAXAssetID, avaxAmount, addr); err != nil {
			t.Fatalf("Failed to add UTXO to shared memory: %s", err)
		}
	}

	return issuer, vm, db, sharedMemory, sender
}

// resetMetrics resets the vm avalanchego metrics, and allows
// for the VM to be re-initialized in tests.
func resetMetrics(vm *VM) {
	vm.ctx.Metrics = metrics.NewPrefixGatherer()
}

func TestVMConfig(t *testing.T) {
	txFeeCap := float64(11)
	enabledEthAPIs := []string{"debug"}
	configJSON := fmt.Sprintf(`{"rpc-tx-fee-cap": %g,"eth-apis": %s}`, txFeeCap, fmt.Sprintf("[%q]", enabledEthAPIs[0]))
	_, vm, _, _, _ := GenesisVM(t, false, "", configJSON, "")
	require.Equal(t, vm.config.RPCTxFeeCap, txFeeCap, "Tx Fee Cap should be set")
	require.Equal(t, vm.config.EthAPIs(), enabledEthAPIs, "EnabledEthAPIs should be set")
	require.NoError(t, vm.Shutdown(context.Background()))
}

func TestVMConfigDefaults(t *testing.T) {
	txFeeCap := float64(11)
	enabledEthAPIs := []string{"debug"}
	configJSON := fmt.Sprintf(`{"rpc-tx-fee-cap": %g,"eth-apis": %s}`, txFeeCap, fmt.Sprintf("[%q]", enabledEthAPIs[0]))
	_, vm, _, _, _ := GenesisVM(t, false, "", configJSON, "")

	var vmConfig config.Config
	vmConfig.SetDefaults(defaultTxPoolConfig)
	vmConfig.RPCTxFeeCap = txFeeCap
	vmConfig.EnabledEthAPIs = enabledEthAPIs
	require.Equal(t, vmConfig, vm.config, "VM Config should match default with overrides")
	require.NoError(t, vm.Shutdown(context.Background()))
}

func TestVMNilConfig(t *testing.T) {
	_, vm, _, _, _ := GenesisVM(t, false, "", "", "")

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
	_, vm, _, _, _ := GenesisVM(t, false, "", configJSON, "")
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
			name:             "Apricot Phase 3",
			genesis:          genesisJSONApricotPhase3,
			expectedGasPrice: big.NewInt(0),
		},
		{
			name:             "Apricot Phase 4",
			genesis:          genesisJSONApricotPhase4,
			expectedGasPrice: big.NewInt(0),
		},
		{
			name:             "Apricot Phase 5",
			genesis:          genesisJSONApricotPhase5,
			expectedGasPrice: big.NewInt(0),
		},
		{
			name:             "Apricot Phase Pre 6",
			genesis:          genesisJSONApricotPhasePre6,
			expectedGasPrice: big.NewInt(0),
		},
		{
			name:             "Apricot Phase 6",
			genesis:          genesisJSONApricotPhase6,
			expectedGasPrice: big.NewInt(0),
		},
		{
			name:             "Apricot Phase Post 6",
			genesis:          genesisJSONApricotPhasePost6,
			expectedGasPrice: big.NewInt(0),
		},
		{
			name:             "Banff",
			genesis:          genesisJSONBanff,
			expectedGasPrice: big.NewInt(0),
		},
		{
			name:             "Cortina",
			genesis:          genesisJSONCortina,
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
			_, vm, _, _, _ := GenesisVM(t, true, test.genesis, "", "")

			if gasPrice := vm.txPool.GasTip(); gasPrice.Cmp(test.expectedGasPrice) != 0 {
				t.Fatalf("Expected pool gas price to be %d but found %d", test.expectedGasPrice, gasPrice)
			}
			defer func() {
				shutdownChan := make(chan error, 1)
				shutdownFunc := func() {
					err := vm.Shutdown(context.Background())
					shutdownChan <- err
				}

				go shutdownFunc()
				shutdownTimeout := 250 * time.Millisecond
				ticker := time.NewTicker(shutdownTimeout)
				defer ticker.Stop()

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
		})
	}
}

func TestImportMissingUTXOs(t *testing.T) {
	// make a VM with a shared memory that has an importable UTXO to build a block
	importAmount := uint64(50000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase2, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	require.NoError(t, err)
	err = vm.mempool.AddLocalTx(importTx)
	require.NoError(t, err)
	<-issuer
	blk, err := vm.BuildBlock(context.Background())
	require.NoError(t, err)

	// make another VM which is missing the UTXO in shared memory
	_, vm2, _, _, _ := GenesisVM(t, true, genesisJSONApricotPhase2, "", "")
	defer func() {
		err := vm2.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	vm2Blk, err := vm2.ParseBlock(context.Background(), blk.Bytes())
	require.NoError(t, err)
	err = vm2Blk.Verify(context.Background())
	require.ErrorIs(t, err, errMissingUTXOs)

	// This should not result in a bad block since the missing UTXO should
	// prevent InsertBlockManual from being called.
	badBlocks, _ := vm2.blockChain.BadBlocks()
	require.Len(t, badBlocks, 0)
}

// Simple test to ensure we can issue an import transaction followed by an export transaction
// and they will be indexed correctly when accepted.
func TestIssueAtomicTxs(t *testing.T) {
	importAmount := uint64(50000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase2, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

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

	if lastAcceptedID, err := vm.LastAccepted(context.Background()); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}
	vm.blockChain.DrainAcceptorQueue()
	filterAPI := filters.NewFilterAPI(filters.NewFilterSystem(vm.eth.APIBackend, filters.Config{
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

	exportTx, err := vm.newExportTx(vm.ctx.AVAXAssetID, importAmount-(2*ap0.AtomicTxFee), vm.ctx.XChainID, testShortIDAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(exportTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk2, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk2.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk2.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if lastAcceptedID, err := vm.LastAccepted(context.Background()); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blk2.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)
	}

	// Check that both atomic transactions were indexed as expected.
	indexedImportTx, status, height, err := vm.getAtomicTx(importTx.ID())
	assert.NoError(t, err)
	assert.Equal(t, atomic.Accepted, status)
	assert.Equal(t, uint64(1), height, "expected height of indexed import tx to be 1")
	assert.Equal(t, indexedImportTx.ID(), importTx.ID(), "expected ID of indexed import tx to match original txID")

	indexedExportTx, status, height, err := vm.getAtomicTx(exportTx.ID())
	assert.NoError(t, err)
	assert.Equal(t, atomic.Accepted, status)
	assert.Equal(t, uint64(2), height, "expected height of indexed export tx to be 2")
	assert.Equal(t, indexedExportTx.ID(), exportTx.ID(), "expected ID of indexed import tx to match original txID")
}

func TestBuildEthTxBlock(t *testing.T) {
	importAmount := uint64(20000000)
	issuer, vm, dbManager, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase2, `{"pruning-enabled":true}`, "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk1, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk1.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blk1.ID()); err != nil {
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
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), testKeys[0].ToECDSA())
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

	<-issuer

	blk2, err := vm.BuildBlock(context.Background())
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
		NewContext(),
		dbManager,
		[]byte(genesisJSONApricotPhase2),
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

func testConflictingImportTxs(t *testing.T, genesis string) {
	importAmount := uint64(10000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesis, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
		testShortIDAddrs[1]: importAmount,
		testShortIDAddrs[2]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTxs := make([]*atomic.Tx, 0, 3)
	conflictTxs := make([]*atomic.Tx, 0, 3)
	for i, key := range testKeys {
		importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[i], initialBaseFee, []*secp256k1.PrivateKey{key})
		if err != nil {
			t.Fatal(err)
		}
		importTxs = append(importTxs, importTx)

		conflictAddr := testEthAddrs[(i+1)%len(testEthAddrs)]
		conflictTx, err := vm.newImportTx(vm.ctx.XChainID, conflictAddr, initialBaseFee, []*secp256k1.PrivateKey{key})
		if err != nil {
			t.Fatal(err)
		}
		conflictTxs = append(conflictTxs, conflictTx)
	}

	expectedParentBlkID, err := vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range importTxs[:2] {
		if err := vm.mempool.AddLocalTx(tx); err != nil {
			t.Fatal(err)
		}

		<-issuer

		vm.clock.Set(vm.clock.Time().Add(2 * time.Second))
		blk, err := vm.BuildBlock(context.Background())
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
		if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
			t.Fatal(err)
		}
	}

	// Check that for each conflict tx (whose conflict is in the chain ancestry)
	// the VM returns an error when it attempts to issue the conflict into the mempool
	// and when it attempts to build a block with the conflict force added to the mempool.
	for i, tx := range conflictTxs[:2] {
		if err := vm.mempool.AddLocalTx(tx); err == nil {
			t.Fatal("Expected issueTx to fail due to conflicting transaction")
		}
		// Force issue transaction directly to the mempool
		if err := vm.mempool.ForceAddTx(tx); err != nil {
			t.Fatal(err)
		}
		<-issuer

		vm.clock.Set(vm.clock.Time().Add(2 * time.Second))
		_, err = vm.BuildBlock(context.Background())
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
	if err := vm.mempool.AddLocalTx(importTxs[2]); err != nil {
		t.Fatal(err)
	}
	<-issuer
	vm.clock.Set(vm.clock.Time().Add(2 * time.Second))

	validBlock, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := validBlock.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	validEthBlock := validBlock.(*chain.BlockWrapper).Block.(*Block).ethBlock

	rules := vm.currentRules()
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

	conflictingAtomicTxBlock := types.NewBlockWithExtData(
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

	parsedBlock, err := vm.ParseBlock(context.Background(), blockBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Verify(context.Background()); !errors.Is(err, atomic.ErrConflictingAtomicInputs) {
		t.Fatalf("Expected to fail with err: %s, but found err: %s", atomic.ErrConflictingAtomicInputs, err)
	}

	if !rules.IsApricotPhase5 {
		return
	}

	extraData, err = atomic.Codec.Marshal(atomic.CodecVersion, []*atomic.Tx{importTxs[2], conflictTxs[2]})
	if err != nil {
		t.Fatal(err)
	}

	header := types.CopyHeader(validEthBlock.Header())
	header.ExtDataGasUsed.Mul(common.Big2, header.ExtDataGasUsed)

	internalConflictBlock := types.NewBlockWithExtData(
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

	parsedBlock, err = vm.ParseBlock(context.Background(), blockBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Verify(context.Background()); !errors.Is(err, atomic.ErrConflictingAtomicInputs) {
		t.Fatalf("Expected to fail with err: %s, but found err: %s", atomic.ErrConflictingAtomicInputs, err)
	}
}

func TestReissueAtomicTxHigherGasPrice(t *testing.T) {
	kc := secp256k1fx.NewKeychain(testKeys...)

	for name, issueTxs := range map[string]func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, discarded []*atomic.Tx){
		"single UTXO override": func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, evicted []*atomic.Tx) {
			utxo, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			tx1, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, kc, []*avax.UTXO{utxo})
			if err != nil {
				t.Fatal(err)
			}
			tx2, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(common.Big2, initialBaseFee), kc, []*avax.UTXO{utxo})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.mempool.AddLocalTx(tx1); err != nil {
				t.Fatal(err)
			}
			if err := vm.mempool.AddLocalTx(tx2); err != nil {
				t.Fatal(err)
			}

			return []*atomic.Tx{tx2}, []*atomic.Tx{tx1}
		},
		"one of two UTXOs overrides": func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, evicted []*atomic.Tx) {
			utxo1, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			utxo2, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			tx1, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, kc, []*avax.UTXO{utxo1, utxo2})
			if err != nil {
				t.Fatal(err)
			}
			tx2, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(common.Big2, initialBaseFee), kc, []*avax.UTXO{utxo1})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.mempool.AddLocalTx(tx1); err != nil {
				t.Fatal(err)
			}
			if err := vm.mempool.AddLocalTx(tx2); err != nil {
				t.Fatal(err)
			}

			return []*atomic.Tx{tx2}, []*atomic.Tx{tx1}
		},
		"hola": func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) (issued []*atomic.Tx, evicted []*atomic.Tx) {
			utxo1, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			utxo2, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}

			importTx1, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, kc, []*avax.UTXO{utxo1})
			if err != nil {
				t.Fatal(err)
			}

			importTx2, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(big.NewInt(3), initialBaseFee), kc, []*avax.UTXO{utxo2})
			if err != nil {
				t.Fatal(err)
			}

			reissuanceTx1, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(big.NewInt(2), initialBaseFee), kc, []*avax.UTXO{utxo1, utxo2})
			if err != nil {
				t.Fatal(err)
			}
			if err := vm.mempool.AddLocalTx(importTx1); err != nil {
				t.Fatal(err)
			}

			if err := vm.mempool.AddLocalTx(importTx2); err != nil {
				t.Fatal(err)
			}

			if err := vm.mempool.AddLocalTx(reissuanceTx1); !errors.Is(err, atomic.ErrConflictingAtomicTx) {
				t.Fatalf("Expected to fail with err: %s, but found err: %s", atomic.ErrConflictingAtomicTx, err)
			}

			assert.True(t, vm.mempool.Has(importTx1.ID()))
			assert.True(t, vm.mempool.Has(importTx2.ID()))
			assert.False(t, vm.mempool.Has(reissuanceTx1.ID()))

			reissuanceTx2, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(big.NewInt(4), initialBaseFee), kc, []*avax.UTXO{utxo1, utxo2})
			if err != nil {
				t.Fatal(err)
			}
			if err := vm.mempool.AddLocalTx(reissuanceTx2); err != nil {
				t.Fatal(err)
			}

			return []*atomic.Tx{reissuanceTx2}, []*atomic.Tx{importTx1, importTx2}
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase5, "", "")
			issuedTxs, evictedTxs := issueTxs(t, vm, sharedMemory)

			for i, tx := range issuedTxs {
				_, issued := vm.mempool.GetPendingTx(tx.ID())
				assert.True(t, issued, "expected issued tx at index %d to be issued", i)
			}

			for i, tx := range evictedTxs {
				_, discarded, _ := vm.mempool.GetTx(tx.ID())
				assert.True(t, discarded, "expected discarded tx at index %d to be discarded", i)
			}
		})
	}
}

func TestConflictingImportTxsAcrossBlocks(t *testing.T) {
	for name, genesis := range map[string]string{
		"apricotPhase1": genesisJSONApricotPhase1,
		"apricotPhase2": genesisJSONApricotPhase2,
		"apricotPhase3": genesisJSONApricotPhase3,
		"apricotPhase4": genesisJSONApricotPhase4,
		"apricotPhase5": genesisJSONApricotPhase5,
	} {
		t.Run(name, func(t *testing.T) {
			testConflictingImportTxs(t, genesis)
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
	// Create two VMs which will agree on block A and then
	// build the two distinct preferred chains above
	importAmount := uint64(1000000000)
	issuer1, vm1, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, `{"pruning-enabled":true}`, "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	issuer2, vm2, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, `{"pruning-enabled":true}`, "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
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

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, testEthAddrs[1], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
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
		tx := types.NewTransaction(uint64(i), testEthAddrs[1], big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), testKeys[1].ToECDSA())
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

func TestConflictingTransitiveAncestryWithGap(t *testing.T) {
	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	key0 := testKeys[0]
	addr0 := key0.Address()

	key1 := testKeys[1]
	addr1 := key1.Address()

	importAmount := uint64(1000000000)

	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "",
		map[ids.ShortID]uint64{
			addr0: importAmount,
			addr1: importAmount,
		})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	importTx0A, err := vm.newImportTx(vm.ctx.XChainID, key.Address, initialBaseFee, []*secp256k1.PrivateKey{key0})
	if err != nil {
		t.Fatal(err)
	}
	// Create a conflicting transaction
	importTx0B, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[2], initialBaseFee, []*secp256k1.PrivateKey{key0})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx0A); err != nil {
		t.Fatalf("Failed to issue importTx0A: %s", err)
	}

	<-issuer

	blk0, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blk0.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification: %s", err)
	}

	if err := vm.SetPreference(context.Background(), blk0.ID()); err != nil {
		t.Fatal(err)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk0.ID()) {
		t.Fatalf("Expected new block to match")
	}

	tx := types.NewTransaction(0, key.Address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer

	blk1, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build blk1: %s", err)
	}

	if err := blk1.Verify(context.Background()); err != nil {
		t.Fatalf("blk1 failed verification due to %s", err)
	}

	if err := vm.SetPreference(context.Background(), blk1.ID()); err != nil {
		t.Fatal(err)
	}

	importTx1, err := vm.newImportTx(vm.ctx.XChainID, key.Address, initialBaseFee, []*secp256k1.PrivateKey{key1})
	if err != nil {
		t.Fatalf("Failed to issue importTx1 due to: %s", err)
	}

	if err := vm.mempool.AddLocalTx(importTx1); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk2, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blk2.Verify(context.Background()); err != nil {
		t.Fatalf("Block failed verification: %s", err)
	}

	if err := vm.SetPreference(context.Background(), blk2.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx0B); err == nil {
		t.Fatalf("Should not have been able to issue import tx with conflict")
	}
	// Force issue transaction directly into the mempool
	if err := vm.mempool.ForceAddTx(importTx0B); err != nil {
		t.Fatal(err)
	}
	<-issuer

	_, err = vm.BuildBlock(context.Background())
	if err == nil {
		t.Fatal("Shouldn't have been able to build an invalid block")
	}
}

func TestBonusBlocksTxs(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importAmount := uint64(10000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
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

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Make [blk] a bonus block.
	vm.atomicBackend.(*atomicBackend).bonusBlocks = map[uint64]ids.ID{blk.Height(): blk.ID()}

	// Remove the UTXOs from shared memory, so that non-bonus blocks will fail verification
	if err := vm.ctx.SharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.ctx.XChainID: {RemoveRequests: [][]byte{inputID[:]}}}); err != nil {
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

	lastAcceptedID, err := vm.LastAccepted(context.Background())
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
	importAmount := uint64(1000000000)
	issuer1, vm1, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, `{"pruning-enabled":false}`, "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	issuer2, vm2, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, `{"pruning-enabled":false}`, "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
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

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
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
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key)
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
	importAmount := uint64(1000000000)
	issuer1, vm1, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	issuer2, vm2, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
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

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
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
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key)
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
	importAmount := uint64(1000000000)
	issuer1, vm1, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	issuer2, vm2, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
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

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
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
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key)
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
	importAmount := uint64(1000000000)
	issuer1, vm1, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	issuer2, vm2, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
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

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
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
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key)
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

	uncleEthBlock := types.NewBlockWithExtData(
		uncleBlockHeader,
		blkDEthBlock.Transactions(),
		uncles,
		nil,
		trie.NewStackTrie(nil),
		blkDEthBlock.ExtData(),
		false,
	)
	uncleBlock, err := vm2.newBlock(uncleEthBlock)
	if err != nil {
		t.Fatal(err)
	}
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
	importAmount := uint64(1000000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	// Create empty block from blkA
	ethBlock := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock

	emptyEthBlock := types.NewBlockWithExtData(
		types.CopyHeader(ethBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		nil,
		false,
	)

	if len(emptyEthBlock.ExtData()) != 0 || emptyEthBlock.Header().ExtDataHash != (common.Hash{}) {
		t.Fatalf("emptyEthBlock should not have any extra data")
	}

	emptyBlock, err := vm.newBlock(emptyEthBlock)
	if err != nil {
		t.Fatal(err)
	}

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
	importAmount := uint64(1000000000)
	issuer1, vm1, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	issuer2, vm2, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
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

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
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
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key)
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
	importAmount := uint64(1000000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
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
	modifiedBlock := types.NewBlockWithExtData(
		modifiedHeader,
		nil,
		nil,
		nil,
		new(trie.Trie),
		internalBlkA.ethBlock.ExtData(),
		false,
	)

	futureBlock, err := vm.newBlock(modifiedBlock)
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
	importAmount := uint64(1000000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase1, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := vm.newImportTx(vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

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

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 5; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap0.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	for i := 5; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap1.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
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

	<-issuer

	blk, err = vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
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
	importAmount := uint64(1000000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
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

// Builds [blkA] with a virtuous import transaction and [blkB] with a separate import transaction
// that does not conflict. Accepts [blkB] and rejects [blkA], then asserts that the virtuous atomic
// transaction in [blkA] is correctly re-issued into the atomic transaction mempool.
func TestReissueAtomicTx(t *testing.T) {
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase1, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: 10000000,
		testShortIDAddrs[1]: 10000000,
	})

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	genesisBlkID, err := vm.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blkA, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blkA.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blkA.ID()); err != nil {
		t.Fatal(err)
	}

	// SetPreference to parent before rejecting (will rollback state to genesis
	// so that atomic transaction can be reissued, otherwise current block will
	// conflict with UTXO to be reissued)
	if err := vm.SetPreference(context.Background(), genesisBlkID); err != nil {
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
	<-issuer
	blkB, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if blkB.Height() != blkA.Height() {
		t.Fatalf("Expected blkB (%d) to have the same height as blkA (%d)", blkB.Height(), blkA.Height())
	}

	if err := blkB.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blkB.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blkB.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if lastAcceptedID, err := vm.LastAccepted(context.Background()); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blkB.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blkB.ID(), lastAcceptedID)
	}

	// Check that [importTx] has been indexed correctly after [blkB] is accepted.
	_, height, err := vm.atomicTxRepository.GetByTxID(importTx.ID())
	if err != nil {
		t.Fatal(err)
	} else if height != blkB.Height() {
		t.Fatalf("Expected indexed height of import tx to be %d, but found %d", blkB.Height(), height)
	}
}

func TestAtomicTxFailsEVMStateTransferBuildBlock(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase1, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	exportTxs := createExportTxOptions(t, vm, issuer, sharedMemory)
	exportTx1, exportTx2 := exportTxs[0], exportTxs[1]

	if err := vm.mempool.AddLocalTx(exportTx1); err != nil {
		t.Fatal(err)
	}
	<-issuer
	exportBlk1, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := exportBlk1.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), exportBlk1.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(exportTx2); err == nil {
		t.Fatal("Should have failed to issue due to an invalid export tx")
	}

	if err := vm.mempool.AddRemoteTx(exportTx2); err == nil {
		t.Fatal("Should have failed to add because conflicting")
	}

	// Manually add transaction to mempool to bypass validation
	if err := vm.mempool.ForceAddTx(exportTx2); err != nil {
		t.Fatal(err)
	}
	<-issuer

	_, err = vm.BuildBlock(context.Background())
	if err == nil {
		t.Fatal("BuildBlock should have returned an error due to invalid export transaction")
	}
}

func TestBuildInvalidBlockHead(t *testing.T) {
	issuer, vm, _, _, _ := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	key0 := testKeys[0]
	addr0 := key0.Address()

	// Create the transaction
	utx := &atomic.UnsignedImportTx{
		NetworkID:    vm.ctx.NetworkID,
		BlockchainID: vm.ctx.ChainID,
		Outs: []atomic.EVMOutput{{
			Address: common.Address(addr0),
			Amount:  1 * units.Avax,
			AssetID: vm.ctx.AVAXAssetID,
		}},
		ImportedInputs: []*avax.TransferableInput{
			{
				Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1 * units.Avax,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			},
		},
		SourceChain: vm.ctx.XChainID,
	}
	tx := &atomic.Tx{UnsignedAtomicTx: utx}
	if err := tx.Sign(atomic.Codec, [][]*secp256k1.PrivateKey{{key0}}); err != nil {
		t.Fatal(err)
	}

	currentBlock := vm.blockChain.CurrentBlock()

	// Verify that the transaction fails verification when attempting to issue
	// it into the atomic mempool.
	if err := vm.mempool.AddLocalTx(tx); err == nil {
		t.Fatal("Should have failed to issue invalid transaction")
	}
	// Force issue the transaction directly to the mempool
	if err := vm.mempool.ForceAddTx(tx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	if _, err := vm.BuildBlock(context.Background()); err == nil {
		t.Fatalf("Unexpectedly created a block")
	}

	newCurrentBlock := vm.blockChain.CurrentBlock()

	if currentBlock.Hash() != newCurrentBlock.Hash() {
		t.Fatal("current block changed")
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
			logConfig:   `{"log-level": "info"}`,
			genesisJSON: genesisJSONApricotPhase2,
			upgradeJSON: "",
			expectedErr: "",
		},
		{
			name:        "Invalid log level",
			logConfig:   `{"log-level": "cchain"}`,
			genesisJSON: genesisJSONApricotPhase3,
			upgradeJSON: "",
			expectedErr: "failed to initialize logger due to",
		},
	}
	for _, test := range configTests {
		t.Run(test.name, func(t *testing.T) {
			vm := &VM{}
			ctx, dbManager, genesisBytes, issuer, _ := setupGenesis(t, test.genesisJSON)
			appSender := &enginetest.Sender{T: t}
			appSender.CantSendAppGossip = true
			appSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error { return nil }
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

			// If the VM was not initialized, do not attempt to shut it down
			if err == nil {
				shutdownChan := make(chan error, 1)
				shutdownFunc := func() {
					err := vm.Shutdown(context.Background())
					shutdownChan <- err
				}
				go shutdownFunc()

				shutdownTimeout := 250 * time.Millisecond
				ticker := time.NewTicker(shutdownTimeout)
				defer ticker.Stop()

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
// Apricot Phase 4 ruleset in genesis.
func TestBuildApricotPhase4Block(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase4, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importAmount := uint64(1000000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
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

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

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

	ethBlk := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if eBlockGasCost := ethBlk.BlockGasCost(); eBlockGasCost == nil || eBlockGasCost.Cmp(common.Big0) != 0 {
		t.Fatalf("expected blockGasCost to be 0 but got %d", eBlockGasCost)
	}
	if eExtDataGasUsed := ethBlk.ExtDataGasUsed(); eExtDataGasUsed == nil || eExtDataGasUsed.Cmp(big.NewInt(1230)) != 0 {
		t.Fatalf("expected extDataGasUsed to be 1000 but got %d", eExtDataGasUsed)
	}
	minRequiredTip, err := header.EstimateRequiredTip(vm.chainConfig, ethBlk.Header())
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
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	for i := 5; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(ap1.MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
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

	<-issuer

	blk, err = vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	ethBlk = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if ethBlk.BlockGasCost() == nil || ethBlk.BlockGasCost().Cmp(big.NewInt(100)) < 0 {
		t.Fatalf("expected blockGasCost to be at least 100 but got %d", ethBlk.BlockGasCost())
	}
	if ethBlk.ExtDataGasUsed() == nil || ethBlk.ExtDataGasUsed().Cmp(common.Big0) != 0 {
		t.Fatalf("expected extDataGasUsed to be 0 but got %d", ethBlk.ExtDataGasUsed())
	}
	minRequiredTip, err = header.EstimateRequiredTip(vm.chainConfig, ethBlk.Header())
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

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 5 ruleset in genesis.
func TestBuildApricotPhase5Block(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase5, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importAmount := uint64(1000000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
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

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*avalancheatomic.Requests{vm.ctx.ChainID: {PutRequests: []*avalancheatomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, address, initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.AddLocalTx(importTx); err != nil {
		t.Fatal(err)
	}

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

	ethBlk := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if eBlockGasCost := ethBlk.BlockGasCost(); eBlockGasCost == nil || eBlockGasCost.Cmp(common.Big0) != 0 {
		t.Fatalf("expected blockGasCost to be 0 but got %d", eBlockGasCost)
	}
	if eExtDataGasUsed := ethBlk.ExtDataGasUsed(); eExtDataGasUsed == nil || eExtDataGasUsed.Cmp(big.NewInt(11230)) != 0 {
		t.Fatalf("expected extDataGasUsed to be 11230 but got %d", eExtDataGasUsed)
	}
	minRequiredTip, err := header.EstimateRequiredTip(vm.chainConfig, ethBlk.Header())
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
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := vm.txPool.Add(txs, false, false)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blk, err = vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	ethBlk = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if ethBlk.BlockGasCost() == nil || ethBlk.BlockGasCost().Cmp(big.NewInt(100)) < 0 {
		t.Fatalf("expected blockGasCost to be at least 100 but got %d", ethBlk.BlockGasCost())
	}
	if ethBlk.ExtDataGasUsed() == nil || ethBlk.ExtDataGasUsed().Cmp(common.Big0) != 0 {
		t.Fatalf("expected extDataGasUsed to be 0 but got %d", ethBlk.ExtDataGasUsed())
	}
	minRequiredTip, err = header.EstimateRequiredTip(vm.chainConfig, ethBlk.Header())
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

// This is a regression test to ensure that if two consecutive atomic transactions fail verification
// in onFinalizeAndAssemble it will not cause a panic due to calling RevertToSnapshot(revID) on the
// same revision ID twice.
func TestConsecutiveAtomicTransactionsRevertSnapshot(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase1, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)

	// Create three conflicting import transactions
	importTxs := createImportTxOptions(t, vm, sharedMemory)

	// Issue the first import transaction, build, and accept the block.
	if err := vm.mempool.AddLocalTx(importTxs[0]); err != nil {
		t.Fatal(err)
	}

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

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Add the two conflicting transactions directly to the mempool, so that two consecutive transactions
	// will fail verification when build block is called.
	vm.mempool.AddRemoteTx(importTxs[1])
	vm.mempool.AddRemoteTx(importTxs[2])

	if _, err := vm.BuildBlock(context.Background()); err == nil {
		t.Fatal("Expected build block to fail due to empty block")
	}
}

func TestAtomicTxBuildBlockDropsConflicts(t *testing.T) {
	importAmount := uint64(10000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase5, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
		testShortIDAddrs[1]: importAmount,
		testShortIDAddrs[2]: importAmount,
	})
	conflictKey, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	// Create a conflict set for each pair of transactions
	conflictSets := make([]set.Set[ids.ID], len(testKeys))
	for index, key := range testKeys {
		importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[index], initialBaseFee, []*secp256k1.PrivateKey{key})
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.mempool.AddLocalTx(importTx); err != nil {
			t.Fatal(err)
		}
		conflictSets[index].Add(importTx.ID())
		conflictTx, err := vm.newImportTx(vm.ctx.XChainID, conflictKey.Address, initialBaseFee, []*secp256k1.PrivateKey{key})
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.mempool.AddLocalTx(conflictTx); err == nil {
			t.Fatal("should conflict with the utxoSet in the mempool")
		}
		// force add the tx
		vm.mempool.ForceAddTx(conflictTx)
		conflictSets[index].Add(conflictTx.ID())
	}
	<-issuer
	// Note: this only checks the path through OnFinalizeAndAssemble, we should make sure to add a test
	// that verifies blocks received from the network will also fail verification
	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	atomicTxs := blk.(*chain.BlockWrapper).Block.(*Block).atomicTxs
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
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase5, "", "")

	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	kc := secp256k1fx.NewKeychain()
	kc.Add(testKeys[0])
	txID, err := ids.ToID(hashing.ComputeHash256(testShortIDAddrs[0][:]))
	assert.NoError(t, err)

	mempoolTxs := 200
	for i := 0; i < mempoolTxs; i++ {
		utxo, err := addUTXO(sharedMemory, vm.ctx, txID, uint32(i), vm.ctx.AVAXAssetID, importAmount, testShortIDAddrs[0])
		assert.NoError(t, err)

		importTx, err := atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, kc, []*avax.UTXO{utxo})
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.mempool.AddLocalTx(importTx); err != nil {
			t.Fatal(err)
		}
	}

	<-issuer
	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	atomicTxs := blk.(*chain.BlockWrapper).Block.(*Block).atomicTxs

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
	issuer, vm1, _, sharedMemory1, _ := GenesisVM(t, true, genesisJSONApricotPhase4, "", "")
	_, vm2, _, sharedMemory2, _ := GenesisVM(t, true, genesisJSONApricotPhase5, "", "")

	defer func() {
		if err := vm1.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Shutdown(context.Background()); err != nil {
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
		_, err := addUTXO(sharedMemory1, vm1.ctx, txID, uint32(i), vm1.ctx.AVAXAssetID, importAmount, testShortIDAddrs[0])
		assert.NoError(t, err)

		_, err = addUTXO(sharedMemory2, vm2.ctx, txID, uint32(i), vm2.ctx.AVAXAssetID, importAmount, testShortIDAddrs[0])
		assert.NoError(t, err)
	}

	// Double the initial base fee used when estimating the cost of this transaction to ensure that when it is
	// used in ApricotPhase5 it still pays a sufficient fee with the fixed fee per atomic transaction.
	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(common.Big2, initialBaseFee), []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}
	if err := vm1.mempool.ForceAddTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer
	blk1, err := vm1.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := blk1.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	validEthBlock := blk1.(*chain.BlockWrapper).Block.(*Block).ethBlock

	extraData, err := atomic.Codec.Marshal(atomic.CodecVersion, []*atomic.Tx{importTx})
	if err != nil {
		t.Fatal(err)
	}

	// Construct the new block with the extra data in the new format (slice of atomic transactions).
	ethBlk2 := types.NewBlockWithExtData(
		types.CopyHeader(validEthBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	state, err := vm2.blockChain.State()
	if err != nil {
		t.Fatal(err)
	}

	// Hack: test [onExtraStateChange] directly to ensure it catches the atomic gas limit error correctly.
	if _, _, err := vm2.onExtraStateChange(ethBlk2, state); err == nil || !strings.Contains(err.Error(), "exceeds atomic gas limit") {
		t.Fatalf("Expected block to fail verification due to exceeded atomic gas limit, but found error: %v", err)
	}
}

func TestSkipChainConfigCheckCompatible(t *testing.T) {
	importAmount := uint64(50000000)
	issuer, vm, dbManager, _, appSender := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase1, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	defer func() { require.NoError(t, vm.Shutdown(context.Background())) }()

	// Since rewinding is permitted for last accepted height of 0, we must
	// accept one block to test the SkipUpgradeCheck functionality.
	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	require.NoError(t, err)
	require.NoError(t, vm.mempool.AddLocalTx(importTx))
	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	require.NoError(t, err)
	require.NoError(t, blk.Verify(context.Background()))
	require.NoError(t, vm.SetPreference(context.Background(), blk.ID()))
	require.NoError(t, blk.Accept(context.Background()))

	reinitVM := &VM{}
	// use the block's timestamp instead of 0 since rewind to genesis
	// is hardcoded to be allowed in core/genesis.go.
	genesisWithUpgrade := &core.Genesis{}
	require.NoError(t, json.Unmarshal([]byte(genesisJSONApricotPhase1), genesisWithUpgrade))
	genesisWithUpgrade.Config.ApricotPhase2BlockTimestamp = utils.TimeToNewUint64(blk.Timestamp())
	genesisWithUpgradeBytes, err := json.Marshal(genesisWithUpgrade)
	require.NoError(t, err)

	resetMetrics(vm)

	// this will not be allowed
	err = reinitVM.Initialize(context.Background(), vm.ctx, dbManager, genesisWithUpgradeBytes, []byte{}, []byte{}, issuer, []*commonEng.Fx{}, appSender)
	require.ErrorContains(t, err, "mismatching ApricotPhase2 fork block timestamp in database")

	resetMetrics(vm)

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
			importAmount := uint64(1000000000)
			issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, test.genesisJSON, "", "", map[ids.ShortID]uint64{
				testShortIDAddrs[0]: importAmount,
			})

			defer func() {
				if err := vm.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
			}()

			importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.mempool.AddLocalTx(importTx); err != nil {
				t.Fatal(err)
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
			parentBeaconEthBlock := types.NewBlockWithExtData(
				header,
				nil,
				nil,
				nil,
				new(trie.Trie),
				ethBlock.ExtData(),
				false,
			)

			parentBeaconBlock, err := vm.newBlock(parentBeaconEthBlock)
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
	err := json.Unmarshal([]byte(genesisJSONCancun), gspec)
	require.NoError(err)

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
	_, vm, _, _, _ := GenesisVM(t, true, genesisJSONCancun, "", "")
	defer func() { require.NoError(vm.Shutdown(ctx)) }()

	// Verification should fail
	vmBlock, err := vm.newBlock(blocks[0])
	require.NoError(err)
	_, err = vm.ParseBlock(ctx, vmBlock.Bytes())
	require.ErrorContains(err, "blobs not enabled on avalanche networks")
	err = vmBlock.Verify(ctx)
	require.ErrorContains(err, "blobs not enabled on avalanche networks")
}
