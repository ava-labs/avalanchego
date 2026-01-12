// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/metrics"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/triedb"
	"github.com/prometheus/client_golang/prometheus"

	// Force-load tracer engine to trigger registration
	//
	// We must import this package (not referenced elsewhere) so that the native "callTracer"
	// is added to a map of client-accessible tracers. In geth, this is done
	// inside of cmd/geth.
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/registry" // Force-load precompiles to trigger registration
	_ "github.com/ava-labs/libevm/eth/tracers/js"
	_ "github.com/ava-labs/libevm/eth/tracers/native"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/evm/triedb/hashdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/txpool"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/eth"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/eth/ethconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/miner"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/network"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/node"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/config"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/extension"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/rpc"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/sync/client/stats"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/sync/handlers"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"

	subnetevmlog "github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/log"
	vmsync "github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/sync"
	statesyncclient "github.com/ava-labs/avalanchego/graft/subnet-evm/sync/client"
	handlerstats "github.com/ava-labs/avalanchego/graft/subnet-evm/sync/handlers/stats"
	avalanchegossip "github.com/ava-labs/avalanchego/network/p2p/gossip"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheUtils "github.com/ava-labs/avalanchego/utils"
	avajson "github.com/ava-labs/avalanchego/utils/json"
	avalanchegoprometheus "github.com/ava-labs/avalanchego/vms/evm/metrics/prometheus"
	ethparams "github.com/ava-labs/libevm/params"
	avalancheRPC "github.com/gorilla/rpc/v2"
)

var (
	_ block.ChainVM                      = (*VM)(nil)
	_ block.BuildBlockWithContextChainVM = (*VM)(nil)
	_ block.StateSyncableVM              = (*VM)(nil)
	_ statesyncclient.EthBlockParser     = (*VM)(nil)
)

const (
	// Max time from current time allowed for blocks, before they're considered future blocks
	// and fail verification
	maxFutureBlockTime     = 10 * time.Second
	decidedCacheSize       = 10 * units.MiB
	missingCacheSize       = 50
	unverifiedCacheSize    = 5 * units.MiB
	bytesToIDCacheSize     = 5 * units.MiB
	warpSignatureCacheSize = 500

	syncFrequency = 1 * time.Minute

	// Prefixes for metrics gatherers
	ethMetricsPrefix        = "eth"
	sdkMetricsPrefix        = "sdk"
	chainStateMetricsPrefix = "chain_state"
)

// Define the API endpoints for the VM
const (
	adminEndpoint      = "/admin"
	ethRPCEndpoint     = "/rpc"
	ethWSEndpoint      = "/ws"
	validatorsEndpoint = "/validators"
)

var (
	// Set last accepted key to be longer than the keys used to store accepted block IDs.
	lastAcceptedKey    = []byte("last_accepted_key")
	acceptedPrefix     = []byte("snowman_accepted")
	metadataPrefix     = []byte("metadata")
	warpPrefix         = []byte("warp")
	ethDBPrefix        = []byte("ethdb")
	validatorsDBPrefix = []byte("validators")
)

var (
	errEmptyBlock                                 = errors.New("empty block")
	errUnsupportedFXs                             = errors.New("unsupported feature extensions")
	errNilBaseFeeSubnetEVM                        = errors.New("nil base fee is invalid after subnetEVM")
	errNilBlockGasCostSubnetEVM                   = errors.New("nil blockGasCost is invalid after subnetEVM")
	errInvalidBlock                               = errors.New("invalid block")
	errInvalidNonce                               = errors.New("invalid nonce")
	errUnclesUnsupported                          = errors.New("uncles unsupported")
	errInvalidHeaderPredicateResults              = errors.New("invalid header predicate results")
	errInitializingLogger                         = errors.New("failed to initialize logger")
	errShuttingDownVM                             = errors.New("shutting down VM")
	errPathStateUnsupported                       = errors.New("path state scheme is not supported")
	errVerifyGenesis                              = errors.New("failed to verify genesis")
	errFirewoodSnapshotCacheDisabled              = errors.New("snapshot cache must be disabled for Firewood")
	errFirewoodOfflinePruningUnsupported          = errors.New("offline pruning is not supported for Firewood")
	errFirewoodStateSyncUnsupported               = errors.New("state sync is not yet supported for Firewood")
	errFirewoodMissingTrieRepopulationUnsupported = errors.New("missing trie repopulation is not supported for Firewood")
)

// legacyApiNames maps pre geth v1.10.20 api names to their updated counterparts.
// used in attachEthService for backward configuration compatibility.
var legacyAPINames = map[string]string{
	"internal-public-eth":              "internal-eth",
	"internal-public-blockchain":       "internal-blockchain",
	"internal-public-transaction-pool": "internal-transaction",
	"internal-public-tx-pool":          "internal-tx-pool",
	"internal-public-debug":            "internal-debug",
	"internal-private-debug":           "internal-debug",
	"internal-public-account":          "internal-account",
	"internal-private-personal":        "internal-personal",

	"public-eth":        "eth",
	"public-eth-filter": "eth-filter",
	"private-admin":     "admin",
	"public-debug":      "debug",
	"private-debug":     "debug",
}

// VM implements the snowman.ChainVM interface
type VM struct {
	ctx *snow.Context
	// contextLock is used to coordinate global VM operations.
	// This can be used safely instead of snow.Context.Lock which is deprecated and should not be used in rpcchainvm.
	vmLock sync.RWMutex
	// [cancel] may be nil until [snow.NormalOp] starts
	cancel context.CancelFunc
	// *chain.State helps to implement the VM interface by wrapping blocks
	// with an efficient caching layer.
	*chain.State

	config config.Config

	chainID     *big.Int
	genesisHash common.Hash
	chainConfig *params.ChainConfig
	ethConfig   ethconfig.Config

	// Extension Points
	extensionConfig *extension.Config

	// pointers to eth constructs
	eth        *eth.Ethereum
	txPool     *txpool.TxPool
	blockChain *core.BlockChain
	miner      *miner.Miner

	// [versiondb] is the VM's current versioned database
	versiondb *versiondb.Database

	// [db] is the VM's current database
	db database.Database

	// metadataDB is used to store one off keys.
	metadataDB database.Database

	// [chaindb] is the database supplied to the Ethereum backend
	chaindb ethdb.Database

	usingStandaloneDB bool

	// [acceptedBlockDB] is the database to store the last accepted
	// block.
	acceptedBlockDB database.Database
	// [warpDB] is used to store warp message signatures
	// set to a prefixDB with the prefix [warpPrefix]
	warpDB database.Database

	validatorsDB database.Database

	// builderLock is used to synchronize access to the block builder,
	// as it is uninitialized at first and is only initialized when onNormalOperationsStarted is called.
	builderLock sync.Mutex
	builder     *blockBuilder

	clock *mockable.Clock

	shutdownChan chan struct{}
	shutdownWg   sync.WaitGroup

	// Continuous Profiler
	profiler profiler.ContinuousProfiler

	network.Network
	networkCodec codec.Manager

	// Metrics
	sdkMetrics *prometheus.Registry

	bootstrapped avalancheUtils.Atomic[bool]

	stateSyncDone chan struct{}

	logger subnetevmlog.Logger
	// State sync server and client
	vmsync.Server
	vmsync.Client

	// Avalanche Warp Messaging backend
	// Used to serve BLS signatures of warp messages over RPC
	warpBackend warp.Backend

	// Initialize only sets these if nil so they can be overridden in tests
	ethTxPushGossiper avalancheUtils.Atomic[*avalanchegossip.PushGossiper[*GossipEthTx]]
	ethTxPullGossiper avalanchegossip.Gossiper

	uptimeTracker *uptimetracker.UptimeTracker

	chainAlias string
	// RPC handlers (should be stopped before closing chaindb)
	rpcHandlers []interface{ Stop() }
}

// Initialize implements the snowman.ChainVM interface
func (vm *VM) Initialize(
	_ context.Context,
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*commonEng.Fx,
	appSender commonEng.AppSender,
) error {
	vm.ctx = chainCtx

	cfg, deprecateMsg, err := config.GetConfig(configBytes, vm.ctx.NetworkID)
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	vm.config = cfg

	vm.extensionConfig = defaultExtensions()
	// Get clock from extension config
	vm.clock = vm.extensionConfig.Clock

	vm.ctx = chainCtx

	// Create logger
	alias, err := vm.ctx.BCLookup.PrimaryAlias(vm.ctx.ChainID)
	if err != nil {
		// fallback to ChainID string instead of erroring
		alias = vm.ctx.ChainID.String()
	}
	vm.chainAlias = alias

	subnetEVMLogger, err := subnetevmlog.InitLogger(vm.chainAlias, vm.config.LogLevel, vm.config.LogJSONFormat, vm.ctx.Log)
	if err != nil {
		return fmt.Errorf("%w: %w ", errInitializingLogger, err)
	}
	vm.logger = subnetEVMLogger

	log.Info("Initializing Subnet EVM VM", "Version", Version, "libevm version", ethparams.LibEVMVersion, "Config", vm.config)

	if deprecateMsg != "" {
		log.Warn("Deprecation Warning", "msg", deprecateMsg)
	}

	if len(fxs) > 0 {
		return errUnsupportedFXs
	}

	// Enable debug-level metrics that might impact runtime performance
	metrics.EnabledExpensive = vm.config.MetricsExpensiveEnabled

	vm.shutdownChan = make(chan struct{}, 1)

	if err := vm.initializeMetrics(); err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}

	// Initialize the database
	if err := vm.initializeDBs(db); err != nil {
		return fmt.Errorf("failed to initialize databases: %w", err)
	}

	if vm.config.InspectDatabase {
		if err := vm.inspectDatabases(); err != nil {
			return err
		}
	}

	g, err := parseGenesis(chainCtx, genesisBytes, upgradeBytes, vm.config.AirdropFile)
	if err != nil {
		return err
	}

	// vm.ChainConfig() should be available for wrapping VMs before vm.initializeChain()
	vm.chainConfig = g.Config
	vm.chainID = g.Config.ChainID

	vm.ethConfig = ethconfig.NewDefaultConfig()
	vm.ethConfig.Genesis = g
	vm.ethConfig.NetworkId = vm.chainID.Uint64()
	vm.genesisHash = vm.ethConfig.Genesis.ToBlock().Hash() // must create genesis hash before [vm.ReadLastAccepted]
	lastAcceptedHash, lastAcceptedHeight, err := vm.ReadLastAccepted()
	if err != nil {
		return err
	}
	log.Info("read last accepted",
		"hash", lastAcceptedHash,
		"height", lastAcceptedHeight,
	)

	// Set minimum price for mining and default gas price oracle value to the min
	// gas price to prevent so transactions and blocks all use the correct fees
	vm.ethConfig.RPCGasCap = vm.config.RPCGasCap
	vm.ethConfig.RPCEVMTimeout = vm.config.APIMaxDuration.Duration
	vm.ethConfig.RPCTxFeeCap = vm.config.RPCTxFeeCap

	vm.ethConfig.TxPool.Locals = vm.config.PriorityRegossipAddresses
	vm.ethConfig.TxPool.NoLocals = !vm.config.LocalTxsEnabled
	vm.ethConfig.TxPool.PriceLimit = vm.config.TxPoolPriceLimit
	vm.ethConfig.TxPool.PriceBump = vm.config.TxPoolPriceBump
	vm.ethConfig.TxPool.AccountSlots = vm.config.TxPoolAccountSlots
	vm.ethConfig.TxPool.GlobalSlots = vm.config.TxPoolGlobalSlots
	vm.ethConfig.TxPool.AccountQueue = vm.config.TxPoolAccountQueue
	vm.ethConfig.TxPool.GlobalQueue = vm.config.TxPoolGlobalQueue
	vm.ethConfig.TxPool.Lifetime = vm.config.TxPoolLifetime.Duration
	// If we re-enable txpool journaling, we should also add the saved local
	// transactions to the p2p gossip on startup.
	vm.ethConfig.TxPool.Journal = "" // disable journal

	vm.ethConfig.AllowUnfinalizedQueries = vm.config.AllowUnfinalizedQueries
	vm.ethConfig.AllowUnprotectedTxs = vm.config.AllowUnprotectedTxs
	vm.ethConfig.AllowUnprotectedTxHashes = vm.config.AllowUnprotectedTxHashes
	vm.ethConfig.Preimages = vm.config.Preimages
	vm.ethConfig.Pruning = vm.config.Pruning
	vm.ethConfig.TrieCleanCache = vm.config.TrieCleanCache
	vm.ethConfig.TrieDirtyCache = vm.config.TrieDirtyCache
	vm.ethConfig.TrieDirtyCommitTarget = vm.config.TrieDirtyCommitTarget
	vm.ethConfig.TriePrefetcherParallelism = vm.config.TriePrefetcherParallelism
	vm.ethConfig.SnapshotCache = vm.config.SnapshotCache
	vm.ethConfig.AcceptorQueueLimit = vm.config.AcceptorQueueLimit
	vm.ethConfig.PopulateMissingTries = vm.config.PopulateMissingTries
	vm.ethConfig.PopulateMissingTriesParallelism = vm.config.PopulateMissingTriesParallelism
	vm.ethConfig.AllowMissingTries = vm.config.AllowMissingTries
	vm.ethConfig.SnapshotDelayInit = vm.config.StateSyncEnabled
	vm.ethConfig.SnapshotWait = vm.config.SnapshotWait
	vm.ethConfig.SnapshotVerify = vm.config.SnapshotVerify
	vm.ethConfig.HistoricalProofQueryWindow = vm.config.HistoricalProofQueryWindow
	vm.ethConfig.OfflinePruning = vm.config.OfflinePruning
	vm.ethConfig.OfflinePruningBloomFilterSize = vm.config.OfflinePruningBloomFilterSize
	vm.ethConfig.OfflinePruningDataDirectory = vm.config.OfflinePruningDataDirectory
	vm.ethConfig.CommitInterval = vm.config.CommitInterval
	vm.ethConfig.SkipUpgradeCheck = vm.config.SkipUpgradeCheck
	vm.ethConfig.AcceptedCacheSize = vm.config.AcceptedCacheSize
	vm.ethConfig.StateHistory = vm.config.StateHistory
	vm.ethConfig.TransactionHistory = vm.config.TransactionHistory
	vm.ethConfig.SkipTxIndexing = vm.config.SkipTxIndexing
	vm.ethConfig.StateScheme = vm.config.StateScheme

	if vm.ethConfig.StateScheme == customrawdb.FirewoodScheme {
		log.Warn("Firewood state scheme is enabled")
		log.Warn("This is untested in production, use at your own risk")
		// Firewood does not support iterators, so the snapshot cannot be constructed
		if vm.config.SnapshotCache > 0 {
			return errFirewoodSnapshotCacheDisabled
		}
		if vm.config.OfflinePruning {
			return errFirewoodOfflinePruningUnsupported
		}
		if vm.config.StateSyncEnabled {
			return errFirewoodStateSyncUnsupported
		}
		if vm.config.PopulateMissingTries != nil {
			return errFirewoodMissingTrieRepopulationUnsupported
		}
	}
	if vm.ethConfig.StateScheme == rawdb.PathScheme {
		log.Error("Path state scheme is not supported. Please use HashDB or Firewood state schemes instead")
		return errPathStateUnsupported
	}

	// Create directory for offline pruning
	if len(vm.ethConfig.OfflinePruningDataDirectory) != 0 {
		if err := os.MkdirAll(vm.ethConfig.OfflinePruningDataDirectory, perms.ReadWriteExecute); err != nil {
			log.Error("failed to create offline pruning data directory", "error", err)
			return err
		}
	}

	// Handle custom fee recipient
	if common.IsHexAddress(vm.config.FeeRecipient) {
		address := common.HexToAddress(vm.config.FeeRecipient)
		log.Info("Setting fee recipient", "address", address)
		vm.ethConfig.Miner.Etherbase = address
	} else {
		log.Info("Config has not specified any coinbase address. Defaulting to the blackhole address.")
		vm.ethConfig.Miner.Etherbase = constants.BlackholeAddr
	}

	vm.networkCodec = message.Codec
	vm.Network, err = network.NewNetwork(vm.ctx, appSender, vm.networkCodec, vm.config.MaxOutboundActiveRequests, vm.sdkMetrics)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	vm.uptimeTracker, err = uptimetracker.New(vm.ctx.ValidatorState, vm.ctx.SubnetID, vm.validatorsDB, vm.clock)
	if err != nil {
		return fmt.Errorf("failed to initialize uptime tracker: %w", err)
	}

	// Initialize warp backend
	offchainWarpMessages := make([][]byte, len(vm.config.WarpOffChainMessages))
	for i, hexMsg := range vm.config.WarpOffChainMessages {
		offchainWarpMessages[i] = []byte(hexMsg)
	}
	warpSignatureCache := lru.NewCache[ids.ID, []byte](warpSignatureCacheSize)
	meteredCache, err := metercacher.New("warp_signature_cache", vm.sdkMetrics, warpSignatureCache)
	if err != nil {
		return fmt.Errorf("failed to create warp signature cache: %w", err)
	}

	// clear warpdb on initialization if config enabled
	if vm.config.PruneWarpDB {
		if err := database.Clear(vm.warpDB, ethdb.IdealBatchSize); err != nil {
			return fmt.Errorf("failed to prune warpDB: %w", err)
		}
	}

	vm.warpBackend, err = warp.NewBackend(
		vm.ctx.NetworkID,
		vm.ctx.ChainID,
		vm.ctx.WarpSigner,
		vm,
		vm.uptimeTracker,
		vm.warpDB,
		meteredCache,
		offchainWarpMessages,
	)
	if err != nil {
		return err
	}
	if err := vm.initializeChain(lastAcceptedHash, vm.ethConfig); err != nil {
		return err
	}

	go vm.ctx.Log.RecoverAndPanic(vm.startContinuousProfiler)

	// Add p2p warp message warpHandler
	warpHandler := acp118.NewCachedHandler(meteredCache, vm.warpBackend, vm.ctx.WarpSigner)
	if err = vm.P2PNetwork().AddHandler(p2p.SignatureRequestHandlerID, warpHandler); err != nil {
		return err
	}

	vm.stateSyncDone = make(chan struct{})

	return vm.initializeStateSync(lastAcceptedHeight)
}

func parseGenesis(ctx *snow.Context, genesisBytes []byte, upgradeBytes []byte, airdropFile string) (*core.Genesis, error) {
	g := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, g); err != nil {
		return nil, fmt.Errorf("parsing genesis: %w", err)
	}

	// Set the default chain config if not provided
	if g.Config == nil {
		g.Config = params.SubnetEVMDefaultChainConfig
	}

	// Populate the Avalanche config extras.
	configExtra := params.GetExtra(g.Config)
	configExtra.AvalancheContext = extras.AvalancheContext{
		SnowCtx: ctx,
	}

	if configExtra.FeeConfig == commontype.EmptyFeeConfig {
		log.Info("No fee config given in genesis, setting default fee config", "DefaultFeeConfig", params.DefaultFeeConfig)
		configExtra.FeeConfig = params.DefaultFeeConfig
	}

	// Load airdrop file if provided
	if airdropFile != "" {
		var err error
		g.AirdropData, err = os.ReadFile(airdropFile)
		if err != nil {
			return nil, fmt.Errorf("could not read airdrop file '%s': %w", airdropFile, err)
		}
	}

	// Set network upgrade defaults
	configExtra.SetDefaults(ctx.NetworkUpgrades)

	// Apply upgradeBytes (if any) by unmarshalling them into [chainConfig.UpgradeConfig].
	// Initializing the chain will verify upgradeBytes are compatible with existing values.
	// This should be called before g.Verify().
	if len(upgradeBytes) > 0 {
		var upgradeConfig extras.UpgradeConfig
		if err := json.Unmarshal(upgradeBytes, &upgradeConfig); err != nil {
			return nil, fmt.Errorf("failed to parse upgrade bytes: %w", err)
		}
		configExtra.UpgradeConfig = upgradeConfig
	}

	if configExtra.UpgradeConfig.NetworkUpgradeOverrides != nil {
		overrides := configExtra.UpgradeConfig.NetworkUpgradeOverrides
		marshaled, err := json.Marshal(overrides)
		if err != nil {
			log.Warn("Failed to marshal network upgrade overrides", "error", err, "overrides", overrides)
		} else {
			log.Info("Applying network upgrade overrides", "overrides", string(marshaled))
		}
		configExtra.Override(overrides)
	}

	if err := g.Verify(); err != nil {
		return nil, fmt.Errorf("%w: %w", errVerifyGenesis, err)
	}

	// Align all the Ethereum upgrades to the Avalanche upgrades
	if err := params.SetEthUpgrades(g.Config); err != nil {
		return nil, fmt.Errorf("setting eth upgrades: %w", err)
	}
	return g, nil
}

func (vm *VM) initializeMetrics() error {
	// [metrics.Enabled] is a global variable imported from go-ethereum/metrics
	// and must be set to true to enable metrics collection.
	metrics.Enabled = true
	vm.sdkMetrics = prometheus.NewRegistry()
	gatherer := avalanchegoprometheus.NewGatherer(metrics.DefaultRegistry)
	if err := vm.ctx.Metrics.Register(ethMetricsPrefix, gatherer); err != nil {
		return err
	}

	if vm.config.StateScheme == customrawdb.FirewoodScheme {
		if err := vm.ctx.Metrics.Register(customrawdb.FirewoodScheme, ffi.Gatherer{}); err != nil {
			return fmt.Errorf("registering firewood metrics: %w", err)
		}
	}
	return vm.ctx.Metrics.Register(sdkMetricsPrefix, vm.sdkMetrics)
}

func (vm *VM) initializeChain(lastAcceptedHash common.Hash, ethConfig ethconfig.Config) error {
	nodecfg := &node.Config{
		SubnetEVMVersion:      Version,
		KeyStoreDir:           vm.config.KeystoreDirectory,
		ExternalSigner:        vm.config.KeystoreExternalSigner,
		InsecureUnlockAllowed: vm.config.KeystoreInsecureUnlockAllowed,
	}
	node, err := node.New(nodecfg)
	if err != nil {
		return err
	}

	var desiredDelayExcess *acp226.DelayExcess
	if vm.config.MinDelayTarget != nil {
		desiredDelayExcess = new(acp226.DelayExcess)
		*desiredDelayExcess = acp226.DesiredDelayExcess(*vm.config.MinDelayTarget)
	}

	vm.eth, err = eth.New(
		node,
		&vm.ethConfig,
		&EthPushGossiper{vm: vm},
		vm.chaindb,
		eth.Settings{MaxBlocksPerRequest: vm.config.MaxBlocksPerRequest},
		lastAcceptedHash,
		dummy.NewDummyEngine(
			dummy.Mode{},
			desiredDelayExcess,
		),
		vm.clock,
	)
	if err != nil {
		return err
	}
	vm.eth.SetEtherbase(ethConfig.Miner.Etherbase)
	vm.txPool = vm.eth.TxPool()
	vm.blockChain = vm.eth.BlockChain()
	vm.miner = vm.eth.Miner()
	lastAccepted := vm.blockChain.LastAcceptedBlock()
	feeConfig, _, err := vm.blockChain.GetFeeConfigAt(lastAccepted.Header())
	if err != nil {
		return err
	}
	vm.txPool.SetMinFee(feeConfig.MinBaseFee)
	vm.txPool.SetGasTip(big.NewInt(0))

	vm.eth.Start()
	return vm.initChainState(lastAccepted)
}

// initializeStateSyncClient initializes the client for performing state sync.
// If state sync is disabled, this function will wipe any ongoing summary from
// disk to ensure that we do not continue syncing from an invalid snapshot.
func (vm *VM) initializeStateSync(lastAcceptedHeight uint64) error {
	// Create standalone EVM TrieDB (read only) for serving leafs requests.
	// We create a standalone TrieDB here, so that it has a standalone cache from the one
	// used by the node when processing blocks.
	evmTrieDB := triedb.NewDatabase(
		vm.chaindb,
		&triedb.Config{
			DBOverride: hashdb.Config{
				CleanCacheSize: vm.config.StateSyncServerTrieCache * units.MiB,
			}.BackendConstructor,
		},
	)

	// register default leaf request handler for state trie
	syncStats := handlerstats.GetOrRegisterHandlerStats(metrics.Enabled)
	stateLeafRequestConfig := &extension.LeafRequestConfig{
		LeafType:   message.StateTrieNode,
		MetricName: "sync_state_trie_leaves",
		Handler: handlers.NewLeafsRequestHandler(evmTrieDB,
			message.StateTrieKeyLength,
			vm.blockChain, vm.networkCodec,
			syncStats,
		),
	}

	leafHandlers := make(LeafHandlers)
	leafHandlers[stateLeafRequestConfig.LeafType] = stateLeafRequestConfig.Handler

	networkHandler := newNetworkHandler(
		vm.blockChain,
		vm.chaindb,
		vm.networkCodec,
		leafHandlers,
		syncStats,
	)
	vm.Network.SetRequestHandler(networkHandler)

	vm.Server = vmsync.NewServer(vm.blockChain, vm.extensionConfig.SyncSummaryProvider, vm.config.StateSyncCommitInterval) // parse nodeIDs from state sync IDs in vm config
	// parse nodeIDs from state sync IDs in vm config
	var stateSyncIDs []ids.NodeID
	if vm.config.StateSyncEnabled && len(vm.config.StateSyncIDs) > 0 {
		nodeIDs := strings.Split(vm.config.StateSyncIDs, ",")
		stateSyncIDs = make([]ids.NodeID, len(nodeIDs))
		for i, nodeIDString := range nodeIDs {
			nodeID, err := ids.NodeIDFromString(nodeIDString)
			if err != nil {
				return fmt.Errorf("failed to parse %s as NodeID: %w", nodeIDString, err)
			}
			stateSyncIDs[i] = nodeID
		}
	}

	// Initialize the state sync client
	leafMetricsNames := make(map[message.NodeType]string)
	leafMetricsNames[stateLeafRequestConfig.LeafType] = stateLeafRequestConfig.MetricName

	vm.Client = vmsync.NewClient(&vmsync.ClientConfig{
		StateSyncDone: vm.stateSyncDone,
		Chain:         vm.eth,
		State:         vm.State,
		Client: statesyncclient.NewClient(
			&statesyncclient.ClientConfig{
				NetworkClient:    vm.Network,
				Codec:            vm.networkCodec,
				Stats:            stats.NewClientSyncerStats(leafMetricsNames),
				StateSyncNodeIDs: stateSyncIDs,
				BlockParser:      vm,
			},
		),
		Enabled:            vm.config.StateSyncEnabled,
		SkipResume:         vm.config.StateSyncSkipResume,
		MinBlocks:          vm.config.StateSyncMinBlocks,
		RequestSize:        vm.config.StateSyncRequestSize,
		LastAcceptedHeight: lastAcceptedHeight, // TODO clean up how this is passed around
		ChaindDB:           vm.chaindb,
		VerDB:              vm.versiondb,
		MetadataDB:         vm.metadataDB,
		Acceptor:           vm,
		Parser:             vm.extensionConfig.SyncableParser,
		Extender:           nil,
	})

	// If StateSync is disabled, clear any ongoing summary so that we will not attempt to resume
	// sync using a snapshot that has been modified by the node running normal operations.
	if !vm.config.StateSyncEnabled {
		return vm.Client.ClearOngoingSummary()
	}

	return nil
}

func (vm *VM) initChainState(lastAcceptedBlock *types.Block) error {
	block, err := wrapBlock(lastAcceptedBlock, vm)
	if err != nil {
		return fmt.Errorf("failed to wrap last accepted block: %w", err)
	}

	config := &chain.Config{
		DecidedCacheSize:      decidedCacheSize,
		MissingCacheSize:      missingCacheSize,
		UnverifiedCacheSize:   unverifiedCacheSize,
		BytesToIDCacheSize:    bytesToIDCacheSize,
		GetBlock:              vm.getBlock,
		UnmarshalBlock:        vm.parseBlock,
		BuildBlock:            vm.buildBlock,
		BuildBlockWithContext: vm.buildBlockWithContext,
		LastAcceptedBlock:     block,
	}

	// Register chain state metrics
	chainStateRegisterer := prometheus.NewRegistry()
	state, err := chain.NewMeteredState(chainStateRegisterer, config)
	if err != nil {
		return fmt.Errorf("could not create metered state: %w", err)
	}
	vm.State = state

	if !metrics.Enabled {
		return nil
	}

	return vm.ctx.Metrics.Register(chainStateMetricsPrefix, chainStateRegisterer)
}

func (vm *VM) SetState(_ context.Context, state snow.State) error {
	vm.vmLock.Lock()
	defer vm.vmLock.Unlock()
	switch state {
	case snow.StateSyncing:
		vm.bootstrapped.Set(false)
		return nil
	case snow.Bootstrapping:
		return vm.onBootstrapStarted()
	case snow.NormalOp:
		return vm.onNormalOperationsStarted()
	default:
		return snow.ErrUnknownState
	}
}

// onBootstrapStarted marks this VM as bootstrapping
func (vm *VM) onBootstrapStarted() error {
	vm.bootstrapped.Set(false)
	if err := vm.Client.Error(); err != nil {
		return err
	}
	// After starting bootstrapping, do not attempt to resume a previous state sync.
	if err := vm.Client.ClearOngoingSummary(); err != nil {
		return err
	}
	// Ensure snapshots are initialized before bootstrapping (i.e., if state sync is skipped).
	// Note calling this function has no effect if snapshots are already initialized.
	vm.blockChain.InitializeSnapshots()
	return nil
}

// onNormalOperationsStarted marks this VM as bootstrapped
func (vm *VM) onNormalOperationsStarted() error {
	if vm.bootstrapped.Get() {
		return nil
	}
	vm.bootstrapped.Set(true)

	ctx, cancel := context.WithCancel(context.TODO())
	vm.cancel = cancel

	// Initially sync the uptime tracker so that APIs expose recent data even if
	// called immediately after bootstrapping.
	if err := vm.uptimeTracker.Sync(ctx); err != nil {
		return err
	}

	vm.shutdownWg.Add(1)
	go func() {
		defer vm.shutdownWg.Done()
		ticker := time.NewTicker(syncFrequency)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := vm.uptimeTracker.Sync(ctx); err != nil {
					log.Error("failed to sync uptime tracker", "err", err)
				}
			}
		}
	}()

	ethTxPool, err := NewGossipEthTxPool(vm.txPool, vm.sdkMetrics)
	if err != nil {
		return fmt.Errorf("failed to initialize gossip eth tx pool: %w", err)
	}
	vm.shutdownWg.Add(1)
	go func() {
		ethTxPool.Subscribe(ctx)
		vm.shutdownWg.Done()
	}()

	handler, pullGossiper, pushGossiper, err := avalanchegossip.NewSystem(
		vm.ctx.NodeID,
		vm.P2PNetwork(),
		vm.P2PValidators(),
		ethTxPool,
		GossipEthTxMarshaller{},
		avalanchegossip.SystemConfig{
			Log:           vm.ctx.Log,
			Registry:      vm.sdkMetrics,
			Namespace:     "eth_tx_gossip",
			RequestPeriod: vm.config.PullGossipFrequency.Duration,
			PushGossipParams: avalanchegossip.BranchingFactor{
				StakePercentage: vm.config.PushGossipPercentStake,
				Validators:      vm.config.PushGossipNumValidators,
				Peers:           vm.config.PushGossipNumPeers,
			},
			PushRegossipParams: avalanchegossip.BranchingFactor{
				Validators: vm.config.PushRegossipNumValidators,
				Peers:      vm.config.PushRegossipNumPeers,
			},
			RegossipPeriod: vm.config.RegossipFrequency.Duration,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to initialize gossip system: %w", err)
	}

	ethTxPushGossiper := vm.ethTxPushGossiper.Get()
	if ethTxPushGossiper == nil {
		ethTxPushGossiper = pushGossiper
		vm.ethTxPushGossiper.Set(ethTxPushGossiper)
	}

	// NOTE: gossip network must be initialized first otherwise ETH tx gossip will not work.
	vm.builderLock.Lock()
	vm.builder = vm.NewBlockBuilder()
	vm.builder.setChainHeadHash(vm.blockChain.CurrentBlock().Hash())
	vm.builder.setMempoolHeadHash(vm.blockChain.CurrentBlock().Hash())
	vm.builder.awaitSubmittedTxs()
	vm.builderLock.Unlock()

	if err := vm.P2PNetwork().AddHandler(p2p.TxGossipHandlerID, handler); err != nil {
		return fmt.Errorf("failed to add eth tx gossip handler: %w", err)
	}

	if vm.ethTxPullGossiper == nil {
		vm.ethTxPullGossiper = pullGossiper
	}

	vm.shutdownWg.Add(1)
	go func() {
		avalanchegossip.Every(ctx, vm.ctx.Log, ethTxPushGossiper, vm.config.PushGossipFrequency.Duration)
		vm.shutdownWg.Done()
	}()
	vm.shutdownWg.Add(1)
	go func() {
		avalanchegossip.Every(ctx, vm.ctx.Log, vm.ethTxPullGossiper, vm.config.PullGossipFrequency.Duration)
		vm.shutdownWg.Done()
	}()

	return nil
}

func (vm *VM) WaitForEvent(ctx context.Context) (commonEng.Message, error) {
	vm.builderLock.Lock()
	builder := vm.builder
	vm.builderLock.Unlock()

	// Block building is not initialized yet, so we haven't finished syncing or bootstrapping.
	if builder == nil {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-vm.stateSyncDone:
			return commonEng.StateSyncDone, nil
		case <-vm.shutdownChan:
			return commonEng.Message(0), errShuttingDownVM
		}
	}

	return builder.waitForEvent(ctx, vm.blockChain.CurrentHeader())
}

// Shutdown implements the snowman.ChainVM interface
func (vm *VM) Shutdown(context.Context) error {
	vm.vmLock.Lock()
	defer vm.vmLock.Unlock()
	if vm.ctx == nil {
		return nil
	}
	if vm.cancel != nil {
		vm.cancel()
	}
	if vm.bootstrapped.Get() {
		if err := vm.uptimeTracker.Shutdown(); err != nil {
			return fmt.Errorf("failed to shutdown uptime tracker: %w", err)
		}
	}
	vm.Network.Shutdown()
	if err := vm.Client.Shutdown(); err != nil {
		log.Error("error stopping state syncer", "err", err)
	}
	close(vm.shutdownChan)
	// Stop RPC handlers before eth.Stop which will close the database
	for _, handler := range vm.rpcHandlers {
		handler.Stop()
	}
	if err := vm.eth.Stop(); err != nil {
		log.Error("failed stopping eth", "err", err)
	}
	if vm.usingStandaloneDB {
		if err := vm.db.Close(); err != nil {
			log.Error("failed to close database: %w", err)
		} else {
			log.Info("Database closed")
		}
	}
	vm.shutdownWg.Wait()
	log.Info("Subnet-EVM Shutdown completed")
	return nil
}

// buildBlock builds a block to be wrapped by ChainState
func (vm *VM) buildBlock(ctx context.Context) (snowman.Block, error) {
	return vm.buildBlockWithContext(ctx, nil)
}

func (vm *VM) buildBlockWithContext(_ context.Context, proposerVMBlockCtx *block.Context) (snowman.Block, error) {
	if proposerVMBlockCtx != nil {
		log.Debug("Building block with context", "pChainBlockHeight", proposerVMBlockCtx.PChainHeight)
	} else {
		log.Debug("Building block without context")
	}
	predicateCtx := &precompileconfig.PredicateContext{
		SnowCtx:            vm.ctx,
		ProposerVMBlockCtx: proposerVMBlockCtx,
	}

	block, err := vm.miner.GenerateBlock(predicateCtx)
	vm.builder.handleGenerateBlock(vm.blockChain.CurrentHeader().ParentHash)
	if err != nil {
		return nil, err
	}

	// Note: the status of block is set by ChainState
	blk, err := wrapBlock(block, vm)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap built block: %w", err)
	}

	// Verify is called on a non-wrapped block here, such that this
	// does not add [blk] to the processing blocks map in ChainState.
	//
	// TODO cache verification since Verify() will be called by the
	// consensus engine as well.
	//
	// Note: this is only called when building a new block, so caching
	// verification will only be a significant optimization for nodes
	// that produce a large number of blocks.
	// We call verify without writes here to avoid generating a reference
	// to the blk state root in the triedb when we are going to call verify
	// again from the consensus engine with writes enabled.
	if err := blk.verify(predicateCtx, false /*=writes*/); err != nil {
		return nil, fmt.Errorf("block failed verification due to: %w", err)
	}

	log.Debug("built block",
		"id", blk.ID(),
	)
	// Marks the current transactions from the mempool as being successfully issued
	// into a block.
	return blk, nil
}

// parseBlock parses [b] into a block to be wrapped by ChainState.
func (vm *VM) parseBlock(_ context.Context, b []byte) (snowman.Block, error) {
	ethBlock := new(types.Block)
	if err := rlp.DecodeBytes(b, ethBlock); err != nil {
		return nil, err
	}

	// Note: the status of block is set by ChainState
	block, err := wrapBlock(ethBlock, vm)
	if err != nil {
		return nil, err
	}
	// Performing syntactic verification in ParseBlock allows for
	// short-circuiting bad blocks before they are processed by the VM.
	if err := block.syntacticVerify(); err != nil {
		return nil, fmt.Errorf("syntactic block verification failed: %w", err)
	}
	return block, nil
}

func (vm *VM) ParseEthBlock(b []byte) (*types.Block, error) {
	block, err := vm.parseBlock(context.TODO(), b)
	if err != nil {
		return nil, err
	}

	return block.(*wrappedBlock).ethBlock, nil
}

// getBlock attempts to retrieve block [id] from the VM to be wrapped
// by ChainState.
func (vm *VM) getBlock(_ context.Context, id ids.ID) (snowman.Block, error) {
	ethBlock := vm.blockChain.GetBlockByHash(common.Hash(id))
	// If [ethBlock] is nil, return [database.ErrNotFound] here
	// so that the miss is considered cacheable.
	if ethBlock == nil {
		return nil, database.ErrNotFound
	}
	// Note: the status of block is set by ChainState
	return wrapBlock(ethBlock, vm)
}

// GetAcceptedBlock attempts to retrieve block [blkID] from the VM. This method
// only returns accepted blocks.
func (vm *VM) GetAcceptedBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	blk, err := vm.GetBlock(ctx, blkID)
	if err != nil {
		return nil, err
	}

	height := blk.Height()
	acceptedBlkID, err := vm.GetBlockIDAtHeight(ctx, height)
	if err != nil {
		return nil, err
	}

	if acceptedBlkID != blkID {
		// The provided block is not accepted.
		return nil, database.ErrNotFound
	}
	return blk, nil
}

// SetPreference sets what the current tail of the chain is
func (vm *VM) SetPreference(ctx context.Context, blkID ids.ID) error {
	// Since each internal handler used by [vm.State] always returns a block
	// with non-nil ethBlock value, GetBlockInternal should never return a
	// (*Block) with a nil ethBlock value.
	block, err := vm.GetBlockInternal(ctx, blkID)
	if err != nil {
		return fmt.Errorf("failed to set preference to %s: %w", blkID, err)
	}

	wb, isWrappedBlock := block.(*wrappedBlock)
	if !isWrappedBlock {
		return fmt.Errorf("expected block %s to be of type *wrappedBlock but got %T", blkID, block)
	}

	vm.setPendingBlock(wb.GetEthBlock().Hash())

	return vm.blockChain.SetPreference(wb.ethBlock)
}

// GetBlockIDAtHeight returns the canonical block at [height].
// Note: the engine assumes that if a block is not found at [height], then
// [database.ErrNotFound] will be returned. This indicates that the VM has state
// synced and does not have all historical blocks available.
func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	lastAcceptedBlock := vm.LastAcceptedBlock()
	if lastAcceptedBlock.Height() < height {
		return ids.ID{}, database.ErrNotFound
	}

	hash := vm.blockChain.GetCanonicalHash(height)
	if hash == (common.Hash{}) {
		return ids.ID{}, database.ErrNotFound
	}
	return ids.ID(hash), nil
}

func (*VM) Version(context.Context) (string, error) {
	return Version, nil
}

// NewHandler returns a new Handler for a service where:
//   - The handler's functionality is defined by [service]
//     [service] should be a gorilla RPC service (see https://www.gorillatoolkit.org/pkg/rpc/v2)
//   - The name of the service is [name]
func newHandler(name string, service interface{}) (http.Handler, error) {
	server := avalancheRPC.NewServer()
	server.RegisterCodec(avajson.NewCodec(), "application/json")
	server.RegisterCodec(avajson.NewCodec(), "application/json;charset=UTF-8")
	return server, server.RegisterService(service, name)
}

// CreateHandlers makes new http handlers that can handle API calls
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	handler := rpc.NewServer(vm.config.APIMaxDuration.Duration)
	if vm.config.BatchRequestLimit > 0 && vm.config.BatchResponseMaxSize > 0 {
		handler.SetBatchLimits(int(vm.config.BatchRequestLimit), int(vm.config.BatchResponseMaxSize))
	}
	if vm.config.HTTPBodyLimit > 0 {
		handler.SetHTTPBodyLimit(int(vm.config.HTTPBodyLimit))
	}

	enabledAPIs := vm.config.EthAPIs()
	if err := attachEthService(handler, vm.eth.APIs(), enabledAPIs); err != nil {
		return nil, err
	}

	apis := make(map[string]http.Handler)
	if vm.config.AdminAPIEnabled {
		adminAPI, err := newHandler("admin", NewAdminService(vm, os.ExpandEnv(fmt.Sprintf("%s_subnet_evm_performance_%s", vm.config.AdminAPIDir, vm.chainAlias))))
		if err != nil {
			return nil, fmt.Errorf("failed to register service for admin API due to %w", err)
		}
		apis[adminEndpoint] = adminAPI
		enabledAPIs = append(enabledAPIs, "subnet-evm-admin")
	}

	if vm.config.ValidatorsAPIEnabled {
		validatorsAPI, err := newHandler("validators", &ValidatorsAPI{vm})
		if err != nil {
			return nil, fmt.Errorf("failed to register service for validators API due to %w", err)
		}
		apis[validatorsEndpoint] = validatorsAPI
		enabledAPIs = append(enabledAPIs, "validators")
	}

	if vm.config.WarpAPIEnabled {
		warpSDKClient := vm.P2PNetwork().NewClient(p2p.SignatureRequestHandlerID, vm.P2PValidators())
		signatureAggregator := acp118.NewSignatureAggregator(vm.ctx.Log, warpSDKClient)

		if err := handler.RegisterName("warp", warp.NewAPI(vm.ctx, vm.warpBackend, signatureAggregator)); err != nil {
			return nil, err
		}
		enabledAPIs = append(enabledAPIs, "warp")
	}

	log.Info("enabling apis",
		"apis", enabledAPIs,
	)
	apis[ethRPCEndpoint] = handler
	apis[ethWSEndpoint] = handler.WebsocketHandlerWithDuration(
		[]string{"*"},
		vm.config.APIMaxDuration.Duration,
		vm.config.WSCPURefillRate.Duration,
		vm.config.WSCPUMaxStored.Duration,
	)

	vm.rpcHandlers = append(vm.rpcHandlers, handler)
	return apis, nil
}

// NewHTTPHandler implements the block.ChainVM interface
func (vm *VM) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	handlers, err := vm.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}

	// Return the main RPC handler as the primary HTTP handler
	if handler, exists := handlers[ethRPCEndpoint]; exists {
		return handler, nil
	}

	// Fallback to a default handler if no RPC handler exists
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "No HTTP handler available", http.StatusNotFound)
	}), nil
}

func (*VM) CreateHTTP2Handler(context.Context) (http.Handler, error) {
	return nil, nil
}

/*
 ******************************************************************************
 *********************************** Helpers **********************************
 ******************************************************************************
 */

// GetCurrentNonce returns the nonce associated with the address at the
// preferred block
func (vm *VM) GetCurrentNonce(address common.Address) (uint64, error) {
	// Note: current state uses the state of the preferred block.
	state, err := vm.blockChain.State()
	if err != nil {
		return 0, err
	}
	return state.GetNonce(address), nil
}

func (vm *VM) chainConfigExtra() *extras.ChainConfig {
	return params.GetExtra(vm.chainConfig)
}

func (vm *VM) rules(number *big.Int, time uint64) extras.Rules {
	ethrules := vm.chainConfig.Rules(number, params.IsMergeTODO, time)
	return *params.GetRulesExtra(ethrules)
}

func (vm *VM) startContinuousProfiler() {
	// If the profiler directory is empty, return immediately
	// without creating or starting a continuous profiler.
	if vm.config.ContinuousProfilerDir == "" {
		return
	}
	vm.profiler = profiler.NewContinuous(
		filepath.Clean(vm.config.ContinuousProfilerDir),
		vm.config.ContinuousProfilerFrequency.Duration,
		vm.config.ContinuousProfilerMaxFiles,
	)
	defer vm.profiler.Shutdown()

	vm.shutdownWg.Add(1)
	go func() {
		defer vm.shutdownWg.Done()
		log.Info("Dispatching continuous profiler", "dir", vm.config.ContinuousProfilerDir, "freq", vm.config.ContinuousProfilerFrequency, "maxFiles", vm.config.ContinuousProfilerMaxFiles)
		err := vm.profiler.Dispatch()
		if err != nil {
			log.Error("continuous profiler failed", "err", err)
		}
	}()
	// Wait for shutdownChan to be closed
	<-vm.shutdownChan
}

// readLastAccepted reads the last accepted hash from [acceptedBlockDB] and returns the
// last accepted block hash and height by reading directly from [vm.chaindb] instead of relying
// on [chain].
// Note: assumes [vm.chaindb] and [vm.genesisHash] have been initialized.
func (vm *VM) ReadLastAccepted() (common.Hash, uint64, error) {
	// Attempt to load last accepted block to determine if it is necessary to
	// initialize state with the genesis block.
	lastAcceptedBytes, lastAcceptedErr := vm.acceptedBlockDB.Get(lastAcceptedKey)
	switch {
	case lastAcceptedErr == database.ErrNotFound:
		// If there is nothing in the database, return the genesis block hash and height
		return vm.genesisHash, 0, nil
	case lastAcceptedErr != nil:
		return common.Hash{}, 0, fmt.Errorf("failed to get last accepted block ID due to: %w", lastAcceptedErr)
	case len(lastAcceptedBytes) != common.HashLength:
		return common.Hash{}, 0, fmt.Errorf("last accepted bytes should have been length %d, but found %d", common.HashLength, len(lastAcceptedBytes))
	default:
		lastAcceptedHash := common.BytesToHash(lastAcceptedBytes)
		height := rawdb.ReadHeaderNumber(vm.chaindb, lastAcceptedHash)
		if height == nil {
			return common.Hash{}, 0, fmt.Errorf("failed to retrieve header number of last accepted block: %s", lastAcceptedHash)
		}
		return lastAcceptedHash, *height, nil
	}
}

// defaultExtensions returns the default extension configuration.
func defaultExtensions() *extension.Config {
	return &extension.Config{
		Clock:               &mockable.Clock{},
		SyncSummaryProvider: &message.BlockSyncSummaryProvider{},
		SyncableParser:      message.NewBlockSyncSummaryParser(),
	}
}

// attachEthService registers the backend RPC services provided by Ethereum
// to the provided handler under their assigned namespaces.
func attachEthService(handler *rpc.Server, apis []rpc.API, names []string) error {
	enabledServicesSet := make(map[string]struct{})
	for _, ns := range names {
		// handle pre geth v1.10.20 api names as aliases for their updated values
		// to allow configurations to be backwards compatible.
		if newName, isLegacy := legacyAPINames[ns]; isLegacy {
			log.Info("deprecated api name referenced in configuration.", "deprecated", ns, "new", newName)
			enabledServicesSet[newName] = struct{}{}
			continue
		}

		enabledServicesSet[ns] = struct{}{}
	}

	apiSet := make(map[string]rpc.API)
	for _, api := range apis {
		if existingAPI, exists := apiSet[api.Name]; exists {
			return fmt.Errorf("duplicated API name: %s, namespaces %s and %s", api.Name, api.Namespace, existingAPI.Namespace)
		}
		apiSet[api.Name] = api
	}

	for name := range enabledServicesSet {
		api, exists := apiSet[name]
		if !exists {
			return fmt.Errorf("API service %s not found", name)
		}
		if err := handler.RegisterName(api.Namespace, api.Service); err != nil {
			return err
		}
	}

	return nil
}

func (vm *VM) setPendingBlock(hash common.Hash) {
	vm.builderLock.Lock()
	defer vm.builderLock.Unlock()

	if vm.builder == nil {
		return
	}

	vm.builder.setChainHeadHash(hash)
}

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	vm.vmLock.Lock()
	defer vm.vmLock.Unlock()

	if err := vm.uptimeTracker.Connect(nodeID); err != nil {
		return fmt.Errorf("uptime tracker failed to connect node %s: %w", nodeID, err)
	}

	return vm.Network.Connected(ctx, nodeID, version)
}

func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	vm.vmLock.Lock()
	defer vm.vmLock.Unlock()

	if err := vm.uptimeTracker.Disconnect(nodeID); err != nil {
		return fmt.Errorf("uptime tracker failed to disconnect node %s: %w", nodeID, err)
	}

	return vm.Network.Disconnected(ctx, nodeID)
}

func (vm *VM) PutLastAcceptedID(id ids.ID) error {
	return vm.acceptedBlockDB.Put(lastAcceptedKey, id[:])
}
