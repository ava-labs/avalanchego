// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
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
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/components/gas"
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

	// Force-load precompiles to trigger registration
	_ "github.com/ava-labs/coreth/precompile/registry"
	// Force-load tracer engine to trigger registration
	//
	// We must import this package (not referenced elsewhere) so that the native "callTracer"
	// is added to a map of client-accessible tracers. In geth, this is done
	// inside of cmd/geth.
	_ "github.com/ava-labs/libevm/eth/tracers/js"
	_ "github.com/ava-labs/libevm/eth/tracers/native"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/eth/ethconfig"
	"github.com/ava-labs/coreth/miner"
	"github.com/ava-labs/coreth/network"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/gossip"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/ava-labs/coreth/plugin/evm/vmerrors"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ava-labs/coreth/sync/client/stats"
	"github.com/ava-labs/coreth/sync/handlers"
	"github.com/ava-labs/coreth/triedb/hashdb"
	"github.com/ava-labs/coreth/warp"

	avalanchegossip "github.com/ava-labs/avalanchego/network/p2p/gossip"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheUtils "github.com/ava-labs/avalanchego/utils"
	avalanchegoprometheus "github.com/ava-labs/avalanchego/vms/evm/metrics/prometheus"
	corethlog "github.com/ava-labs/coreth/plugin/evm/log"
	warpcontract "github.com/ava-labs/coreth/precompile/contracts/warp"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	vmsync "github.com/ava-labs/coreth/sync/vm"
	utilsrpc "github.com/ava-labs/coreth/utils/rpc"
	ethparams "github.com/ava-labs/libevm/params"
)

var (
	_ block.ChainVM                      = (*VM)(nil)
	_ block.BuildBlockWithContextChainVM = (*VM)(nil)
	_ block.StateSyncableVM              = (*VM)(nil)
	_ statesyncclient.EthBlockParser     = (*VM)(nil)
	_ vmsync.BlockAcceptor               = (*VM)(nil)
)

const (
	// Max time from current time allowed for blocks, before they're considered future blocks
	// and fail verification
	maxFutureBlockTime = 10 * time.Second

	secpCacheSize          = 1024
	decidedCacheSize       = 10 * units.MiB
	missingCacheSize       = 50
	unverifiedCacheSize    = 5 * units.MiB
	bytesToIDCacheSize     = 5 * units.MiB
	warpSignatureCacheSize = 500

	// Prefixes for metrics gatherers
	ethMetricsPrefix        = "eth"
	sdkMetricsPrefix        = "sdk"
	chainStateMetricsPrefix = "chain_state"
)

// Define the API endpoints for the VM
const (
	adminEndpoint        = "/admin"
	ethRPCEndpoint       = "/rpc"
	ethWSEndpoint        = "/ws"
	ethTxGossipNamespace = "eth_tx_gossip"
)

var (
	// Set last accepted key to be longer than the keys used to store accepted block IDs.
	lastAcceptedKey = []byte("last_accepted_key")
	acceptedPrefix  = []byte("snowman_accepted")
	metadataPrefix  = []byte("metadata")
	warpPrefix      = []byte("warp")
	ethDBPrefix     = []byte("ethdb")
)

var (
	errInvalidBlock                  = errors.New("invalid block")
	errInvalidNonce                  = errors.New("invalid nonce")
	errUnclesUnsupported             = errors.New("uncles unsupported")
	errNilBaseFeeApricotPhase3       = errors.New("nil base fee is invalid after apricotPhase3")
	errNilBlockGasCostApricotPhase4  = errors.New("nil blockGasCost is invalid after apricotPhase4")
	errInvalidHeaderPredicateResults = errors.New("invalid header predicate results")
	errInitializingLogger            = errors.New("failed to initialize logger")
	errShuttingDownVM                = errors.New("shutting down VM")
)

var originalStderr *os.File

// legacyApiNames maps pre geth v1.10.20 api names to their updated counterparts.
// used in attachEthService for backward configuration compatibility.
var legacyApiNames = map[string]string{
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

func init() {
	// Preserve [os.Stderr] prior to the call in plugin/main.go to plugin.Serve(...).
	// Preserving the log level allows us to update the root handler while writing to the original
	// [os.Stderr] that is being piped through to the logger via the rpcchainvm.
	originalStderr = os.Stderr
}

// VM implements the snowman.ChainVM interface
type VM struct {
	ctx *snow.Context
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

	// metadataDB is used to store one off keys.
	metadataDB database.Database

	// [chaindb] is the database supplied to the Ethereum backend
	chaindb ethdb.Database

	// [acceptedBlockDB] is the database to store the last accepted
	// block.
	acceptedBlockDB database.Database

	// [warpDB] is used to store warp message signatures
	// set to a prefixDB with the prefix [warpPrefix]
	warpDB database.Database

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
	IsPlugin     bool

	stateSyncDone chan struct{}

	logger corethlog.Logger
	// State sync server and client
	vmsync.Server
	vmsync.Client

	// Avalanche Warp Messaging backend
	// Used to serve BLS signatures of warp messages over RPC
	warpBackend warp.Backend

	ethTxPushGossiper avalancheUtils.Atomic[*avalanchegossip.PushGossiper[*GossipEthTx]]

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
	_ []byte,
	configBytes []byte,
	_ []*commonEng.Fx,
	appSender commonEng.AppSender,
) error {
	vm.ctx = chainCtx
	vm.clock = vm.extensionConfig.Clock

	cfg, deprecateMsg, err := config.GetConfig(configBytes, vm.ctx.NetworkID)
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	vm.config = cfg

	// Create logger
	alias, err := vm.ctx.BCLookup.PrimaryAlias(vm.ctx.ChainID)
	if err != nil {
		// fallback to ChainID string instead of erroring
		alias = vm.ctx.ChainID.String()
	}
	vm.chainAlias = alias

	var writer io.Writer = vm.ctx.Log
	if vm.IsPlugin {
		writer = originalStderr
	}

	corethLogger, err := corethlog.InitLogger(vm.chainAlias, vm.config.LogLevel, vm.config.LogJSONFormat, writer)
	if err != nil {
		return fmt.Errorf("%w: %w ", errInitializingLogger, err)
	}
	vm.logger = corethLogger

	log.Info("Initializing Coreth VM", "Version", Version, "libevm version", ethparams.LibEVMVersion, "Config", vm.config)

	if deprecateMsg != "" {
		log.Warn("Deprecation Warning", "msg", deprecateMsg)
	}

	// Enable debug-level metrics that might impact runtime performance
	metrics.EnabledExpensive = vm.config.MetricsExpensiveEnabled

	vm.shutdownChan = make(chan struct{}, 1)

	if err := vm.initializeMetrics(); err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}

	// Initialize the database
	vm.initializeDBs(db)
	if vm.config.InspectDatabase {
		if err := vm.inspectDatabases(); err != nil {
			return err
		}
	}

	g, err := parseGenesis(chainCtx, genesisBytes)
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
	vm.ethConfig.PriceOptionConfig.SlowFeePercentage = vm.config.PriceOptionSlowFeePercentage
	vm.ethConfig.PriceOptionConfig.FastFeePercentage = vm.config.PriceOptionFastFeePercentage
	vm.ethConfig.PriceOptionConfig.MaxTip = vm.config.PriceOptionMaxTip

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
	vm.ethConfig.SnapshotDelayInit = vm.stateSyncEnabled(lastAcceptedHeight)
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
		// Firewood only supports pruning for now.
		if !vm.config.Pruning {
			return errors.New("Pruning must be enabled for Firewood")
		}
		// Firewood does not support iterators, so the snapshot cannot be constructed
		if vm.config.SnapshotCache > 0 {
			return errors.New("Snapshot cache must be disabled for Firewood")
		}
		if vm.config.OfflinePruning {
			return errors.New("Offline pruning is not supported for Firewood")
		}
		if vm.config.StateSyncEnabled == nil || *vm.config.StateSyncEnabled {
			return errors.New("State sync is not yet supported for Firewood")
		}
	}
	if vm.ethConfig.StateScheme == rawdb.PathScheme {
		log.Error("Path state scheme is not supported. Please use HashDB or Firewood state schemes instead")
		return errors.New("Path state scheme is not supported")
	}

	// Create directory for offline pruning
	if len(vm.ethConfig.OfflinePruningDataDirectory) != 0 {
		if err := os.MkdirAll(vm.ethConfig.OfflinePruningDataDirectory, perms.ReadWriteExecute); err != nil {
			log.Error("failed to create offline pruning data directory", "error", err)
			return err
		}
	}

	vm.chainConfig = g.Config

	vm.networkCodec = message.Codec
	vm.Network, err = network.NewNetwork(vm.ctx, appSender, vm.networkCodec, vm.config.MaxOutboundActiveRequests, vm.sdkMetrics)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
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
		vm.warpDB,
		meteredCache,
		offchainWarpMessages,
	)
	if err != nil {
		return err
	}
	if err := vm.initializeChain(lastAcceptedHash); err != nil {
		return err
	}

	go vm.ctx.Log.RecoverAndPanic(vm.startContinuousProfiler)

	// Add p2p warp message warpHandler
	warpHandler := acp118.NewCachedHandler(meteredCache, vm.warpBackend, vm.ctx.WarpSigner)
	vm.Network.AddHandler(p2p.SignatureRequestHandlerID, warpHandler)

	vm.stateSyncDone = make(chan struct{})

	return vm.initializeStateSync(lastAcceptedHeight)
}

func parseGenesis(ctx *snow.Context, bytes []byte) (*core.Genesis, error) {
	g := new(core.Genesis)
	if err := json.Unmarshal(bytes, g); err != nil {
		return nil, fmt.Errorf("parsing genesis: %w", err)
	}

	// Populate the Avalanche config extras.
	configExtra := params.GetExtra(g.Config)
	configExtra.AvalancheContext = extras.AvalancheContext{
		SnowCtx: ctx,
	}
	configExtra.NetworkUpgrades = extras.GetNetworkUpgrades(ctx.NetworkUpgrades)

	// If Durango is scheduled, schedule the Warp Precompile at the same time.
	if configExtra.DurangoBlockTimestamp != nil {
		configExtra.PrecompileUpgrades = append(configExtra.PrecompileUpgrades, extras.PrecompileUpgrade{
			Config: warpcontract.NewDefaultConfig(configExtra.DurangoBlockTimestamp),
		})
	}
	if err := configExtra.Verify(); err != nil {
		return nil, fmt.Errorf("invalid chain config: %w", err)
	}

	// Align all the Ethereum upgrades to the Avalanche upgrades
	if err := params.SetEthUpgrades(g.Config); err != nil {
		return nil, fmt.Errorf("setting eth upgrades: %w", err)
	}
	return g, nil
}

func (vm *VM) initializeMetrics() error {
	metrics.Enabled = true
	vm.sdkMetrics = prometheus.NewRegistry()
	gatherer := avalanchegoprometheus.NewGatherer(metrics.DefaultRegistry)
	if err := vm.ctx.Metrics.Register(ethMetricsPrefix, gatherer); err != nil {
		return err
	}

	if vm.config.MetricsExpensiveEnabled && vm.config.StateScheme == customrawdb.FirewoodScheme {
		if err := ffi.StartMetrics(); err != nil {
			return fmt.Errorf("failed to start firewood metrics collection: %w", err)
		}
		if err := vm.ctx.Metrics.Register("firewood", ffi.Gatherer{}); err != nil {
			return fmt.Errorf("failed to register firewood metrics: %w", err)
		}
	}
	return vm.ctx.Metrics.Register(sdkMetricsPrefix, vm.sdkMetrics)
}

func (vm *VM) initializeChain(lastAcceptedHash common.Hash) error {
	nodecfg := &node.Config{
		CorethVersion:         Version,
		KeyStoreDir:           vm.config.KeystoreDirectory,
		ExternalSigner:        vm.config.KeystoreExternalSigner,
		InsecureUnlockAllowed: vm.config.KeystoreInsecureUnlockAllowed,
	}
	node, err := node.New(nodecfg)
	if err != nil {
		return err
	}

	// If the gas target is specified, calculate the desired target excess and
	// use it during block creation.
	var desiredTargetExcess *gas.Gas
	if vm.config.GasTarget != nil {
		desiredTargetExcess = new(gas.Gas)
		*desiredTargetExcess = acp176.DesiredTargetExcess(*vm.config.GasTarget)
	}

	vm.eth, err = eth.New(
		node,
		&vm.ethConfig,
		&EthPushGossiper{vm: vm},
		vm.chaindb,
		eth.Settings{MaxBlocksPerRequest: vm.config.MaxBlocksPerRequest},
		lastAcceptedHash,
		dummy.NewDummyEngine(
			vm.extensionConfig.ConsensusCallbacks,
			dummy.Mode{},
			vm.clock,
			desiredTargetExcess,
		),
		vm.clock,
	)
	if err != nil {
		return err
	}
	vm.eth.SetEtherbase(constants.BlackholeAddr)
	vm.txPool = vm.eth.TxPool()
	vm.blockChain = vm.eth.BlockChain()
	vm.miner = vm.eth.Miner()

	// Set the gas parameters for the tx pool to the minimum gas price for the
	// latest upgrade.
	vm.txPool.SetGasTip(big.NewInt(0))
	vm.txPool.SetMinFee(big.NewInt(acp176.MinGasPrice))

	vm.eth.Start()
	return vm.initChainState(vm.blockChain.LastAcceptedBlock())
}

// initializeStateSync initializes the vm for performing state sync and responding to peer requests.
// If state sync is disabled, this function will wipe any ongoing summary from
// disk to ensure that we do not continue syncing from an invalid snapshot.
func (vm *VM) initializeStateSync(lastAcceptedHeight uint64) error {
	// Create standalone EVM TrieDB (read only) for serving leafs requests.
	// We create a standalone TrieDB here, so that it has a standalone cache from the one
	// used by the node when processing blocks.
	// However, Firewood does not support multiple TrieDBs, so we use the same one.
	evmTrieDB := vm.eth.BlockChain().TrieDB()
	if vm.ethConfig.StateScheme != customrawdb.FirewoodScheme {
		evmTrieDB = triedb.NewDatabase(
			vm.chaindb,
			&triedb.Config{
				DBOverride: hashdb.Config{
					CleanCacheSize: vm.config.StateSyncServerTrieCache * units.MiB,
				}.BackendConstructor,
			},
		)
	}
	leafHandlers := make(LeafHandlers)
	leafMetricsNames := make(map[message.NodeType]string)
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
	leafHandlers[stateLeafRequestConfig.LeafType] = stateLeafRequestConfig.Handler
	leafMetricsNames[stateLeafRequestConfig.LeafType] = stateLeafRequestConfig.MetricName

	extraLeafConfig := vm.extensionConfig.ExtraSyncLeafHandlerConfig
	if extraLeafConfig != nil {
		leafHandlers[extraLeafConfig.LeafType] = extraLeafConfig.Handler
		leafMetricsNames[extraLeafConfig.LeafType] = extraLeafConfig.MetricName
	}

	networkHandler := newNetworkHandler(
		vm.blockChain,
		vm.chaindb,
		vm.networkCodec,
		leafHandlers,
		syncStats,
	)
	vm.Network.SetRequestHandler(networkHandler)

	vm.Server = vmsync.NewServer(vm.blockChain, vm.extensionConfig.SyncSummaryProvider, vm.config.StateSyncCommitInterval)
	stateSyncEnabled := vm.stateSyncEnabled(lastAcceptedHeight)
	// parse nodeIDs from state sync IDs in vm config
	var stateSyncIDs []ids.NodeID
	if stateSyncEnabled && len(vm.config.StateSyncIDs) > 0 {
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
		Enabled:            stateSyncEnabled,
		SkipResume:         vm.config.StateSyncSkipResume,
		MinBlocks:          vm.config.StateSyncMinBlocks,
		RequestSize:        vm.config.StateSyncRequestSize,
		LastAcceptedHeight: lastAcceptedHeight, // TODO clean up how this is passed around
		ChainDB:            vm.chaindb,
		VerDB:              vm.versiondb,
		MetadataDB:         vm.metadataDB,
		Acceptor:           vm,
		Parser:             vm.extensionConfig.SyncableParser,
		Extender:           vm.extensionConfig.SyncExtender,
	})

	// If StateSync is disabled, clear any ongoing summary so that we will not attempt to resume
	// sync using a snapshot that has been modified by the node running normal operations.
	if !stateSyncEnabled {
		return vm.Client.ClearOngoingSummary()
	}

	return nil
}

func (vm *VM) initChainState(lastAcceptedBlock *types.Block) error {
	block, err := wrapBlock(lastAcceptedBlock, vm)
	if err != nil {
		return fmt.Errorf("failed to create block wrapper for the last accepted block: %w", err)
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
	// Initialize goroutines related to block building
	// once we enter normal operation as there is no need to handle mempool gossip before this point.
	return vm.initBlockBuilding()
}

// initBlockBuilding starts goroutines to manage block building
func (vm *VM) initBlockBuilding() error {
	ctx, cancel := context.WithCancel(context.TODO())
	vm.cancel = cancel

	ethTxGossipMarshaller := GossipEthTxMarshaller{}
	ethTxGossipClient := vm.Network.NewClient(p2p.TxGossipHandlerID)
	ethTxGossipMetrics, err := avalanchegossip.NewMetrics(vm.sdkMetrics, ethTxGossipNamespace)
	if err != nil {
		return fmt.Errorf("failed to initialize eth tx gossip metrics: %w", err)
	}
	ethTxPool, err := NewGossipEthTxPool(vm.txPool, vm.sdkMetrics)
	if err != nil {
		return fmt.Errorf("failed to initialize gossip eth tx pool: %w", err)
	}
	vm.shutdownWg.Add(1)
	go func() {
		ethTxPool.Subscribe(ctx)
		vm.shutdownWg.Done()
	}()
	pushGossipParams := avalanchegossip.BranchingFactor{
		StakePercentage: vm.config.PushGossipPercentStake,
		Validators:      vm.config.PushGossipNumValidators,
		Peers:           vm.config.PushGossipNumPeers,
	}
	pushRegossipParams := avalanchegossip.BranchingFactor{
		Validators: vm.config.PushRegossipNumValidators,
		Peers:      vm.config.PushRegossipNumPeers,
	}

	ethTxPushGossiper, err := avalanchegossip.NewPushGossiper[*GossipEthTx](
		ethTxGossipMarshaller,
		ethTxPool,
		vm.P2PValidators(),
		ethTxGossipClient,
		ethTxGossipMetrics,
		pushGossipParams,
		pushRegossipParams,
		config.PushGossipDiscardedElements,
		config.TxGossipTargetMessageSize,
		vm.config.RegossipFrequency.Duration,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize eth tx push gossiper: %w", err)
	}
	vm.ethTxPushGossiper.Set(ethTxPushGossiper)

	// NOTE: gossip network must be initialized first otherwise ETH tx gossip will not work.
	vm.builderLock.Lock()
	vm.builder = vm.NewBlockBuilder(vm.extensionConfig.ExtraMempool)
	vm.builder.awaitSubmittedTxs()
	vm.builderLock.Unlock()

	ethTxGossipHandler, err := gossip.NewTxGossipHandler[*GossipEthTx](
		vm.ctx.Log,
		ethTxGossipMarshaller,
		ethTxPool,
		ethTxGossipMetrics,
		config.TxGossipTargetMessageSize,
		config.TxGossipThrottlingPeriod,
		config.TxGossipRequestsPerPeer,
		vm.P2PValidators(),
		vm.sdkMetrics,
		"eth_tx_gossip",
	)
	if err != nil {
		return fmt.Errorf("failed to initialize eth tx gossip handler: %w", err)
	}

	if err := vm.Network.AddHandler(p2p.TxGossipHandlerID, ethTxGossipHandler); err != nil {
		return fmt.Errorf("failed to add eth tx gossip handler: %w", err)
	}

	ethTxPullGossiper := avalanchegossip.NewPullGossiper[*GossipEthTx](
		vm.ctx.Log,
		ethTxGossipMarshaller,
		ethTxPool,
		ethTxGossipClient,
		ethTxGossipMetrics,
		config.TxGossipPollSize,
	)

	ethTxPullGossiperWhenValidator := avalanchegossip.ValidatorGossiper{
		Gossiper:   ethTxPullGossiper,
		NodeID:     vm.ctx.NodeID,
		Validators: vm.P2PValidators(),
	}

	vm.shutdownWg.Add(1)
	go func() {
		avalanchegossip.Every(ctx, vm.ctx.Log, ethTxPushGossiper, vm.config.PushGossipFrequency.Duration)
		vm.shutdownWg.Done()
	}()
	vm.shutdownWg.Add(1)
	go func() {
		avalanchegossip.Every(ctx, vm.ctx.Log, ethTxPullGossiperWhenValidator, vm.config.PullGossipFrequency.Duration)
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

	return builder.waitForEvent(ctx)
}

// Shutdown implements the snowman.ChainVM interface
func (vm *VM) Shutdown(context.Context) error {
	if vm.ctx == nil {
		return nil
	}
	if vm.cancel != nil {
		vm.cancel()
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
	vm.eth.Stop()
	vm.shutdownWg.Wait()
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
	vm.builder.handleGenerateBlock()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", vmerrors.ErrGenerateBlockFailed, err)
	}

	// Note: the status of block is set by ChainState
	blk, err := wrapBlock(block, vm)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", vmerrors.ErrWrapBlockFailed, err)
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
		return nil, fmt.Errorf("%w: %w", vmerrors.ErrBlockVerificationFailed, err)
	}

	log.Debug("built block",
		"id", blk.ID(),
	)
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
	// with non-nil ethBlock value, GetExtendedBlock should never return a
	// (*Block) with a nil ethBlock value.
	block, err := vm.GetExtendedBlock(ctx, blkID)
	if err != nil {
		return fmt.Errorf("failed to set preference to %s: %w", blkID, err)
	}

	return vm.blockChain.SetPreference(block.GetEthBlock())
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

// CreateHandlers makes new http handlers that can handle API calls
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	handler := rpc.NewServer(vm.config.APIMaxDuration.Duration)
	if vm.config.BatchRequestLimit > 0 && vm.config.BatchResponseMaxSize > 0 {
		handler.SetBatchLimits(int(vm.config.BatchRequestLimit), int(vm.config.BatchResponseMaxSize))
	}
	if vm.config.HttpBodyLimit > 0 {
		handler.SetHTTPBodyLimit(int(vm.config.HttpBodyLimit))
	}

	enabledAPIs := vm.config.EthAPIs()
	if err := attachEthService(handler, vm.eth.APIs(), enabledAPIs); err != nil {
		return nil, err
	}

	apis := make(map[string]http.Handler)

	if vm.config.AdminAPIEnabled {
		adminAPI, err := utilsrpc.NewHandler("admin", NewAdminService(vm, os.ExpandEnv(fmt.Sprintf("%s_coreth_performance_%s", vm.config.AdminAPIDir, vm.chainAlias))))
		if err != nil {
			return nil, fmt.Errorf("failed to register service for admin API due to %w", err)
		}
		apis[adminEndpoint] = adminAPI
		enabledAPIs = append(enabledAPIs, "coreth-admin")
	}

	if vm.config.WarpAPIEnabled {
		warpSDKClient := vm.Network.NewClient(p2p.SignatureRequestHandlerID)
		signatureAggregator := acp118.NewSignatureAggregator(vm.ctx.Log, warpSDKClient)

		if err := handler.RegisterName("warp", warp.NewAPI(vm.ctx, vm.warpBackend, signatureAggregator, vm.requirePrimaryNetworkSigners)); err != nil {
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

func (*VM) NewHTTPHandler(context.Context) (http.Handler, error) {
	return nil, nil
}

func (vm *VM) chainConfigExtra() *extras.ChainConfig {
	return params.GetExtra(vm.chainConfig)
}

func (vm *VM) rules(number *big.Int, time uint64) extras.Rules {
	ethrules := vm.chainConfig.Rules(number, params.IsMergeTODO, time)
	return *params.GetRulesExtra(ethrules)
}

// currentRules returns the chain rules for the current block.
func (vm *VM) currentRules() extras.Rules {
	header := vm.eth.APIBackend.CurrentHeader()
	return vm.rules(header.Number, header.Time)
}

// requirePrimaryNetworkSigners returns true if warp messages from the primary
// network must be signed by the primary network validators.
// This is necessary when the subnet is not validating the primary network.
func (vm *VM) requirePrimaryNetworkSigners() bool {
	switch c := vm.currentRules().Precompiles[warpcontract.ContractAddress].(type) {
	case *warpcontract.Config:
		return c.RequirePrimaryNetworkSigners
	default: // includes nil due to non-presence
		return false
	}
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

// attachEthService registers the backend RPC services provided by Ethereum
// to the provided handler under their assigned namespaces.
func attachEthService(handler *rpc.Server, apis []rpc.API, names []string) error {
	enabledServicesSet := make(map[string]struct{})
	for _, ns := range names {
		// handle pre geth v1.10.20 api names as aliases for their updated values
		// to allow configurations to be backwards compatible.
		if newName, isLegacy := legacyApiNames[ns]; isLegacy {
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

func (vm *VM) stateSyncEnabled(lastAcceptedHeight uint64) bool {
	if vm.config.StateSyncEnabled != nil {
		// if the config is set, use that
		return *vm.config.StateSyncEnabled
	}

	// enable state sync by default if the chain is empty.
	return lastAcceptedHeight == 0
}

func (vm *VM) PutLastAcceptedID(id ids.ID) error {
	return vm.acceptedBlockDB.Put(lastAcceptedKey, id[:])
}
