// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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

	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/upgrade"
	avalanchegoConstants "github.com/ava-labs/avalanchego/utils/constants"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/eth/ethconfig"
	corethprometheus "github.com/ava-labs/coreth/metrics/prometheus"
	"github.com/ava-labs/coreth/miner"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/etna"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ava-labs/coreth/triedb/hashdb"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/metrics"

	warpcontract "github.com/ava-labs/coreth/precompile/contracts/warp"
	"github.com/ava-labs/coreth/rpc"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/client/stats"
	"github.com/ava-labs/coreth/warp"

	// Force-load tracer engine to trigger registration
	//
	// We must import this package (not referenced elsewhere) so that the native "callTracer"
	// is added to a map of client-accessible tracers. In geth, this is done
	// inside of cmd/geth.
	_ "github.com/ava-labs/coreth/eth/tracers/js"
	_ "github.com/ava-labs/coreth/eth/tracers/native"

	"github.com/ava-labs/coreth/precompile/precompileconfig"
	// Force-load precompiles to trigger registration
	_ "github.com/ava-labs/coreth/precompile/registry"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	avalancheRPC "github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"

	avalancheUtils "github.com/ava-labs/avalanchego/utils"
	avalancheJSON "github.com/ava-labs/avalanchego/utils/json"
)

var (
	_ block.ChainVM                      = &VM{}
	_ block.BuildBlockWithContextChainVM = &VM{}
	_ block.StateSyncableVM              = &VM{}
	_ statesyncclient.EthBlockParser     = &VM{}
	_ secp256k1fx.VM                     = &VM{}
)

const (
	// Max time from current time allowed for blocks, before they're considered future blocks
	// and fail verification
	maxFutureBlockTime = 10 * time.Second
	maxUTXOsToFetch    = 1024
	defaultMempoolSize = 4096

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

	targetAtomicTxsSize = 40 * units.KiB

	// gossip constants
	pushGossipDiscardedElements = 16_384
	txGossipTargetMessageSize   = 20 * units.KiB
	maxValidatorSetStaleness    = time.Minute
	txGossipThrottlingPeriod    = 10 * time.Second
	txGossipThrottlingLimit     = 2
	txGossipPollSize            = 1
)

// Define the API endpoints for the VM
const (
	avaxEndpoint            = "/avax"
	adminEndpoint           = "/admin"
	ethRPCEndpoint          = "/rpc"
	ethWSEndpoint           = "/ws"
	ethTxGossipNamespace    = "eth_tx_gossip"
	atomicTxGossipNamespace = "atomic_tx_gossip"
)

var (
	// Set last accepted key to be longer than the keys used to store accepted block IDs.
	lastAcceptedKey = []byte("last_accepted_key")
	acceptedPrefix  = []byte("snowman_accepted")
	metadataPrefix  = []byte("metadata")
	warpPrefix      = []byte("warp")
	ethDBPrefix     = []byte("ethdb")

	// Prefixes for atomic trie
	atomicTrieDBPrefix     = []byte("atomicTrieDB")
	atomicTrieMetaDBPrefix = []byte("atomicTrieMetaDB")
)

var (
	errEmptyBlock                    = errors.New("empty block")
	errUnsupportedFXs                = errors.New("unsupported feature extensions")
	errInvalidBlock                  = errors.New("invalid block")
	errInvalidNonce                  = errors.New("invalid nonce")
	errUnclesUnsupported             = errors.New("uncles unsupported")
	errRejectedParent                = errors.New("rejected parent")
	errNilBaseFeeApricotPhase3       = errors.New("nil base fee is invalid after apricotPhase3")
	errNilBlockGasCostApricotPhase4  = errors.New("nil blockGasCost is invalid after apricotPhase4")
	errInvalidHeaderPredicateResults = errors.New("invalid header predicate results")
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
	networkID   uint64
	genesisHash common.Hash
	chainConfig *params.ChainConfig
	ethConfig   ethconfig.Config

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

	// [acceptedBlockDB] is the database to store the last accepted
	// block.
	acceptedBlockDB database.Database

	// [warpDB] is used to store warp message signatures
	// set to a prefixDB with the prefix [warpPrefix]
	warpDB database.Database

	toEngine chan<- commonEng.Message

	syntacticBlockValidator BlockValidator

	// [atomicTxRepository] maintains two indexes on accepted atomic txs.
	// - txID to accepted atomic tx
	// - block height to list of atomic txs accepted on block at that height
	atomicTxRepository AtomicTxRepository
	// [atomicTrie] maintains a merkle forest of [height]=>[atomic txs].
	atomicTrie AtomicTrie
	// [atomicBackend] abstracts verification and processing of atomic transactions
	atomicBackend AtomicBackend

	builder *blockBuilder

	baseCodec codec.Registry
	clock     mockable.Clock
	mempool   *atomic.Mempool

	shutdownChan chan struct{}
	shutdownWg   sync.WaitGroup

	fx        secp256k1fx.Fx
	secpCache secp256k1.RecoverCache

	// Continuous Profiler
	profiler profiler.ContinuousProfiler

	peer.Network
	client       peer.NetworkClient
	networkCodec codec.Manager

	p2pValidators *p2p.Validators

	// Metrics
	sdkMetrics *prometheus.Registry

	bootstrapped avalancheUtils.Atomic[bool]
	IsPlugin     bool

	logger CorethLogger
	// State sync server and client
	StateSyncServer
	StateSyncClient

	// Avalanche Warp Messaging backend
	// Used to serve BLS signatures of warp messages over RPC
	warpBackend warp.Backend

	// Initialize only sets these if nil so they can be overridden in tests
	p2pSender             commonEng.AppSender
	ethTxGossipHandler    p2p.Handler
	ethTxPushGossiper     avalancheUtils.Atomic[*gossip.PushGossiper[*GossipEthTx]]
	ethTxPullGossiper     gossip.Gossiper
	atomicTxGossipHandler p2p.Handler
	atomicTxPushGossiper  *gossip.PushGossiper[*atomic.GossipAtomicTx]
	atomicTxPullGossiper  gossip.Gossiper

	chainAlias string
	// RPC handlers (should be stopped before closing chaindb)
	rpcHandlers []interface{ Stop() }
}

// CodecRegistry implements the secp256k1fx interface
func (vm *VM) CodecRegistry() codec.Registry { return vm.baseCodec }

// Clock implements the secp256k1fx interface
func (vm *VM) Clock() *mockable.Clock { return &vm.clock }

// Logger implements the secp256k1fx interface
func (vm *VM) Logger() logging.Logger { return vm.ctx.Log }

/*
 ******************************************************************************
 ********************************* Snowman API ********************************
 ******************************************************************************
 */

// implements SnowmanPlusPlusVM interface
func (vm *VM) GetActivationTime() time.Time {
	return utils.Uint64ToTime(vm.chainConfig.ApricotPhase4BlockTimestamp)
}

// Initialize implements the snowman.ChainVM interface
func (vm *VM) Initialize(
	_ context.Context,
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- commonEng.Message,
	fxs []*commonEng.Fx,
	appSender commonEng.AppSender,
) error {
	vm.config.SetDefaults(defaultTxPoolConfig)
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &vm.config); err != nil {
			return fmt.Errorf("failed to unmarshal config %s: %w", string(configBytes), err)
		}
	}
	vm.ctx = chainCtx

	if err := vm.config.Validate(vm.ctx.NetworkID); err != nil {
		return err
	}
	// We should deprecate config flags as the first thing, before we do anything else
	// because this can set old flags to new flags. log the message after we have
	// initialized the logger.
	deprecateMsg := vm.config.Deprecate()

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

	corethLogger, err := InitLogger(vm.chainAlias, vm.config.LogLevel, vm.config.LogJSONFormat, writer)
	if err != nil {
		return fmt.Errorf("failed to initialize logger due to: %w ", err)
	}
	vm.logger = corethLogger

	log.Info("Initializing Coreth VM", "Version", Version, "Config", vm.config)

	if deprecateMsg != "" {
		log.Warn("Deprecation Warning", "msg", deprecateMsg)
	}

	if len(fxs) > 0 {
		return errUnsupportedFXs
	}

	// Enable debug-level metrics that might impact runtime performance
	metrics.EnabledExpensive = vm.config.MetricsExpensiveEnabled

	vm.toEngine = toEngine
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

	g := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, g); err != nil {
		return err
	}

	var extDataHashes map[common.Hash]common.Hash
	// Set the chain config for mainnet/fuji chain IDs
	switch chainCtx.NetworkID {
	case avalanchegoConstants.MainnetID:
		extDataHashes = mainnetExtDataHashes
	case avalanchegoConstants.FujiID:
		extDataHashes = fujiExtDataHashes
	}

	// if the chainCtx.NetworkUpgrades is not empty, set the chain config
	// normally it should not be empty, but some tests may not set it
	if chainCtx.NetworkUpgrades != (upgrade.Config{}) {
		g.Config.NetworkUpgrades = params.GetNetworkUpgrades(chainCtx.NetworkUpgrades)
	}

	// If the Durango is activated, activate the Warp Precompile at the same time
	if g.Config.DurangoBlockTimestamp != nil {
		g.Config.PrecompileUpgrades = append(g.Config.PrecompileUpgrades, params.PrecompileUpgrade{
			Config: warpcontract.NewDefaultConfig(g.Config.DurangoBlockTimestamp),
		})
	}

	// Set the Avalanche Context on the ChainConfig
	g.Config.AvalancheContext = params.AvalancheContext{
		SnowCtx: chainCtx,
	}
	vm.syntacticBlockValidator = NewBlockValidator(extDataHashes)

	// Free the memory of the extDataHash map that is not used (i.e. if mainnet
	// config, free fuji)
	fujiExtDataHashes = nil
	mainnetExtDataHashes = nil

	vm.chainID = g.Config.ChainID

	g.Config.SetEthUpgrades()

	vm.ethConfig = ethconfig.NewDefaultConfig()
	vm.ethConfig.Genesis = g
	vm.ethConfig.NetworkId = vm.chainID.Uint64()
	vm.genesisHash = vm.ethConfig.Genesis.ToBlock().Hash() // must create genesis hash before [vm.readLastAccepted]
	lastAcceptedHash, lastAcceptedHeight, err := vm.readLastAccepted()
	if err != nil {
		return err
	}
	log.Info(fmt.Sprintf("lastAccepted = %s", lastAcceptedHash))

	// Set minimum price for mining and default gas price oracle value to the min
	// gas price to prevent so transactions and blocks all use the correct fees
	vm.ethConfig.RPCGasCap = vm.config.RPCGasCap
	vm.ethConfig.RPCEVMTimeout = vm.config.APIMaxDuration.Duration
	vm.ethConfig.RPCTxFeeCap = vm.config.RPCTxFeeCap

	vm.ethConfig.TxPool.NoLocals = !vm.config.LocalTxsEnabled
	vm.ethConfig.TxPool.PriceLimit = vm.config.TxPoolPriceLimit
	vm.ethConfig.TxPool.PriceBump = vm.config.TxPoolPriceBump
	vm.ethConfig.TxPool.AccountSlots = vm.config.TxPoolAccountSlots
	vm.ethConfig.TxPool.GlobalSlots = vm.config.TxPoolGlobalSlots
	vm.ethConfig.TxPool.AccountQueue = vm.config.TxPoolAccountQueue
	vm.ethConfig.TxPool.GlobalQueue = vm.config.TxPoolGlobalQueue
	vm.ethConfig.TxPool.Lifetime = vm.config.TxPoolLifetime.Duration

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
	vm.ethConfig.TransactionHistory = vm.config.TransactionHistory
	vm.ethConfig.SkipTxIndexing = vm.config.SkipTxIndexing

	// Create directory for offline pruning
	if len(vm.ethConfig.OfflinePruningDataDirectory) != 0 {
		if err := os.MkdirAll(vm.ethConfig.OfflinePruningDataDirectory, perms.ReadWriteExecute); err != nil {
			log.Error("failed to create offline pruning data directory", "error", err)
			return err
		}
	}

	vm.chainConfig = g.Config
	vm.networkID = vm.ethConfig.NetworkId
	vm.secpCache = secp256k1.RecoverCache{
		LRU: cache.LRU[ids.ID, *secp256k1.PublicKey]{
			Size: secpCacheSize,
		},
	}

	if err := vm.chainConfig.Verify(); err != nil {
		return fmt.Errorf("failed to verify chain config: %w", err)
	}

	// TODO: read size from settings
	vm.mempool, err = atomic.NewMempool(chainCtx, vm.sdkMetrics, defaultMempoolSize, vm.verifyTxAtTip)
	if err != nil {
		return fmt.Errorf("failed to initialize mempool: %w", err)
	}

	// initialize peer network
	if vm.p2pSender == nil {
		vm.p2pSender = appSender
	}

	// TODO: move all network stuff to peer.NewNetwork
	p2pNetwork, err := p2p.NewNetwork(vm.ctx.Log, vm.p2pSender, vm.sdkMetrics, "p2p")
	if err != nil {
		return fmt.Errorf("failed to initialize p2p network: %w", err)
	}
	vm.p2pValidators = p2p.NewValidators(p2pNetwork.Peers, vm.ctx.Log, vm.ctx.SubnetID, vm.ctx.ValidatorState, maxValidatorSetStaleness)
	vm.networkCodec = message.Codec
	vm.Network = peer.NewNetwork(p2pNetwork, appSender, vm.networkCodec, chainCtx.NodeID, vm.config.MaxOutboundActiveRequests)
	vm.client = peer.NewNetworkClient(vm.Network)

	// Initialize warp backend
	offchainWarpMessages := make([][]byte, len(vm.config.WarpOffChainMessages))
	for i, hexMsg := range vm.config.WarpOffChainMessages {
		offchainWarpMessages[i] = []byte(hexMsg)
	}
	warpSignatureCache := &cache.LRU[ids.ID, []byte]{Size: warpSignatureCacheSize}
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
	// initialize bonus blocks on mainnet
	var (
		bonusBlockHeights map[uint64]ids.ID
	)
	if vm.ctx.NetworkID == avalanchegoConstants.MainnetID {
		bonusBlockHeights, err = readMainnetBonusBlocks()
		if err != nil {
			return fmt.Errorf("failed to read mainnet bonus blocks: %w", err)
		}
	}

	// initialize atomic repository
	vm.atomicTxRepository, err = NewAtomicTxRepository(vm.versiondb, atomic.Codec, lastAcceptedHeight)
	if err != nil {
		return fmt.Errorf("failed to create atomic repository: %w", err)
	}
	vm.atomicBackend, err = NewAtomicBackend(
		vm.versiondb, vm.ctx.SharedMemory, bonusBlockHeights,
		vm.atomicTxRepository, lastAcceptedHeight, lastAcceptedHash,
		vm.config.CommitInterval,
	)
	if err != nil {
		return fmt.Errorf("failed to create atomic backend: %w", err)
	}
	vm.atomicTrie = vm.atomicBackend.AtomicTrie()

	go vm.ctx.Log.RecoverAndPanic(vm.startContinuousProfiler)

	// so [vm.baseCodec] is a dummy codec use to fulfill the secp256k1fx VM
	// interface. The fx will register all of its types, which can be safely
	// ignored by the VM's codec.
	vm.baseCodec = linearcodec.NewDefault()

	if err := vm.fx.Initialize(vm); err != nil {
		return err
	}

	// Add p2p warp message warpHandler
	warpHandler := acp118.NewCachedHandler(meteredCache, vm.warpBackend, vm.ctx.WarpSigner)
	vm.Network.AddHandler(p2p.SignatureRequestHandlerID, warpHandler)

	vm.setAppRequestHandlers()

	vm.StateSyncServer = NewStateSyncServer(&stateSyncServerConfig{
		Chain:            vm.blockChain,
		AtomicTrie:       vm.atomicTrie,
		SyncableInterval: vm.config.StateSyncCommitInterval,
	})
	return vm.initializeStateSyncClient(lastAcceptedHeight)
}

func (vm *VM) initializeMetrics() error {
	metrics.Enabled = true
	vm.sdkMetrics = prometheus.NewRegistry()
	gatherer := corethprometheus.NewGatherer(metrics.DefaultRegistry)
	if err := vm.ctx.Metrics.Register(ethMetricsPrefix, gatherer); err != nil {
		return err
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
	callbacks := vm.createConsensusCallbacks()
	vm.eth, err = eth.New(
		node,
		&vm.ethConfig,
		&EthPushGossiper{vm: vm},
		vm.chaindb,
		eth.Settings{MaxBlocksPerRequest: vm.config.MaxBlocksPerRequest},
		lastAcceptedHash,
		dummy.NewFakerWithClock(callbacks, &vm.clock),
		&vm.clock,
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
	vm.txPool.SetMinFee(big.NewInt(etna.MinBaseFee))

	vm.eth.Start()
	return vm.initChainState(vm.blockChain.LastAcceptedBlock())
}

// initializeStateSyncClient initializes the client for performing state sync.
// If state sync is disabled, this function will wipe any ongoing summary from
// disk to ensure that we do not continue syncing from an invalid snapshot.
func (vm *VM) initializeStateSyncClient(lastAcceptedHeight uint64) error {
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

	vm.StateSyncClient = NewStateSyncClient(&stateSyncClientConfig{
		chain: vm.eth,
		state: vm.State,
		client: statesyncclient.NewClient(
			&statesyncclient.ClientConfig{
				NetworkClient:    vm.client,
				Codec:            vm.networkCodec,
				Stats:            stats.NewClientSyncerStats(),
				StateSyncNodeIDs: stateSyncIDs,
				BlockParser:      vm,
			},
		),
		enabled:              stateSyncEnabled,
		skipResume:           vm.config.StateSyncSkipResume,
		stateSyncMinBlocks:   vm.config.StateSyncMinBlocks,
		stateSyncRequestSize: vm.config.StateSyncRequestSize,
		lastAcceptedHeight:   lastAcceptedHeight, // TODO clean up how this is passed around
		chaindb:              vm.chaindb,
		metadataDB:           vm.metadataDB,
		acceptedBlockDB:      vm.acceptedBlockDB,
		db:                   vm.versiondb,
		atomicBackend:        vm.atomicBackend,
		toEngine:             vm.toEngine,
	})

	// If StateSync is disabled, clear any ongoing summary so that we will not attempt to resume
	// sync using a snapshot that has been modified by the node running normal operations.
	if !stateSyncEnabled {
		return vm.StateSyncClient.ClearOngoingSummary()
	}

	return nil
}

func (vm *VM) initChainState(lastAcceptedBlock *types.Block) error {
	block, err := vm.newBlock(lastAcceptedBlock)
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

func (vm *VM) createConsensusCallbacks() dummy.ConsensusCallbacks {
	return dummy.ConsensusCallbacks{
		OnFinalizeAndAssemble: vm.onFinalizeAndAssemble,
		OnExtraStateChange:    vm.onExtraStateChange,
	}
}

func (vm *VM) preBatchOnFinalizeAndAssemble(header *types.Header, state *state.StateDB, txs []*types.Transaction) ([]byte, *big.Int, *big.Int, error) {
	for {
		tx, exists := vm.mempool.NextTx()
		if !exists {
			break
		}
		// Take a snapshot of [state] before calling verifyTx so that if the transaction fails verification
		// we can revert to [snapshot].
		// Note: snapshot is taken inside the loop because you cannot revert to the same snapshot more than
		// once.
		snapshot := state.Snapshot()
		rules := vm.chainConfig.Rules(header.Number, header.Time)
		if err := vm.verifyTx(tx, header.ParentHash, header.BaseFee, state, rules); err != nil {
			// Discard the transaction from the mempool on failed verification.
			log.Debug("discarding tx from mempool on failed verification", "txID", tx.ID(), "err", err)
			vm.mempool.DiscardCurrentTx(tx.ID())
			state.RevertToSnapshot(snapshot)
			continue
		}

		atomicTxBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, tx)
		if err != nil {
			// Discard the transaction from the mempool and error if the transaction
			// cannot be marshalled. This should never happen.
			log.Debug("discarding tx due to unmarshal err", "txID", tx.ID(), "err", err)
			vm.mempool.DiscardCurrentTx(tx.ID())
			return nil, nil, nil, fmt.Errorf("failed to marshal atomic transaction %s due to %w", tx.ID(), err)
		}
		var contribution, gasUsed *big.Int
		if rules.IsApricotPhase4 {
			contribution, gasUsed, err = tx.BlockFeeContribution(rules.IsApricotPhase5, vm.ctx.AVAXAssetID, header.BaseFee)
			if err != nil {
				return nil, nil, nil, err
			}
		}
		return atomicTxBytes, contribution, gasUsed, nil
	}

	if len(txs) == 0 {
		// this could happen due to the async logic of geth tx pool
		return nil, nil, nil, errEmptyBlock
	}

	return nil, nil, nil, nil
}

// assumes that we are in at least Apricot Phase 5.
func (vm *VM) postBatchOnFinalizeAndAssemble(header *types.Header, state *state.StateDB, txs []*types.Transaction) ([]byte, *big.Int, *big.Int, error) {
	var (
		batchAtomicTxs    []*atomic.Tx
		batchAtomicUTXOs  set.Set[ids.ID]
		batchContribution *big.Int = new(big.Int).Set(common.Big0)
		batchGasUsed      *big.Int = new(big.Int).Set(common.Big0)
		rules                      = vm.chainConfig.Rules(header.Number, header.Time)
		size              int
	)

	for {
		tx, exists := vm.mempool.NextTx()
		if !exists {
			break
		}

		// Ensure that adding [tx] to the block will not exceed the block size soft limit.
		txSize := len(tx.SignedBytes())
		if size+txSize > targetAtomicTxsSize {
			vm.mempool.CancelCurrentTx(tx.ID())
			break
		}

		var (
			txGasUsed, txContribution *big.Int
			err                       error
		)

		// Note: we do not need to check if we are in at least ApricotPhase4 here because
		// we assume that this function will only be called when the block is in at least
		// ApricotPhase5.
		txContribution, txGasUsed, err = tx.BlockFeeContribution(true, vm.ctx.AVAXAssetID, header.BaseFee)
		if err != nil {
			return nil, nil, nil, err
		}
		// ensure [gasUsed] + [batchGasUsed] doesnt exceed the [atomicGasLimit]
		if totalGasUsed := new(big.Int).Add(batchGasUsed, txGasUsed); !utils.BigLessOrEqualUint64(totalGasUsed, ap5.AtomicGasLimit) {
			// Send [tx] back to the mempool's tx heap.
			vm.mempool.CancelCurrentTx(tx.ID())
			break
		}

		if batchAtomicUTXOs.Overlaps(tx.InputUTXOs()) {
			// Discard the transaction from the mempool since it will fail verification
			// after this block has been accepted.
			// Note: if the proposed block is not accepted, the transaction may still be
			// valid, but we discard it early here based on the assumption that the proposed
			// block will most likely be accepted.
			// Discard the transaction from the mempool on failed verification.
			log.Debug("discarding tx due to overlapping input utxos", "txID", tx.ID())
			vm.mempool.DiscardCurrentTx(tx.ID())
			continue
		}

		snapshot := state.Snapshot()
		if err := vm.verifyTx(tx, header.ParentHash, header.BaseFee, state, rules); err != nil {
			// Discard the transaction from the mempool and reset the state to [snapshot]
			// if it fails verification here.
			// Note: prior to this point, we have not modified [state] so there is no need to
			// revert to a snapshot if we discard the transaction prior to this point.
			log.Debug("discarding tx from mempool due to failed verification", "txID", tx.ID(), "err", err)
			vm.mempool.DiscardCurrentTx(tx.ID())
			state.RevertToSnapshot(snapshot)
			continue
		}

		batchAtomicTxs = append(batchAtomicTxs, tx)
		batchAtomicUTXOs.Union(tx.InputUTXOs())
		// Add the [txGasUsed] to the [batchGasUsed] when the [tx] has passed verification
		batchGasUsed.Add(batchGasUsed, txGasUsed)
		batchContribution.Add(batchContribution, txContribution)
		size += txSize
	}

	// If there is a non-zero number of transactions, marshal them and return the byte slice
	// for the block's extra data along with the contribution and gas used.
	if len(batchAtomicTxs) > 0 {
		atomicTxBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, batchAtomicTxs)
		if err != nil {
			// If we fail to marshal the batch of atomic transactions for any reason,
			// discard the entire set of current transactions.
			log.Debug("discarding txs due to error marshaling atomic transactions", "err", err)
			vm.mempool.DiscardCurrentTxs()
			return nil, nil, nil, fmt.Errorf("failed to marshal batch of atomic transactions due to %w", err)
		}
		return atomicTxBytes, batchContribution, batchGasUsed, nil
	}

	// If there are no regular transactions and there were also no atomic transactions to be included,
	// then the block is empty and should be considered invalid.
	if len(txs) == 0 {
		// this could happen due to the async logic of geth tx pool
		return nil, nil, nil, errEmptyBlock
	}

	// If there are no atomic transactions, but there is a non-zero number of regular transactions, then
	// we return a nil slice with no contribution from the atomic transactions and a nil error.
	return nil, nil, nil, nil
}

func (vm *VM) onFinalizeAndAssemble(header *types.Header, state *state.StateDB, txs []*types.Transaction) ([]byte, *big.Int, *big.Int, error) {
	if !vm.chainConfig.IsApricotPhase5(header.Time) {
		return vm.preBatchOnFinalizeAndAssemble(header, state, txs)
	}
	return vm.postBatchOnFinalizeAndAssemble(header, state, txs)
}

func (vm *VM) onExtraStateChange(block *types.Block, state *state.StateDB) (*big.Int, *big.Int, error) {
	var (
		batchContribution *big.Int = big.NewInt(0)
		batchGasUsed      *big.Int = big.NewInt(0)
		header                     = block.Header()
		rules                      = vm.chainConfig.Rules(header.Number, header.Time)
	)

	txs, err := atomic.ExtractAtomicTxs(block.ExtData(), rules.IsApricotPhase5, atomic.Codec)
	if err != nil {
		return nil, nil, err
	}

	// If [atomicBackend] is nil, the VM is still initializing and is reprocessing accepted blocks.
	if vm.atomicBackend != nil {
		if vm.atomicBackend.IsBonus(block.NumberU64(), block.Hash()) {
			log.Info("skipping atomic tx verification on bonus block", "block", block.Hash())
		} else {
			// Verify [txs] do not conflict with themselves or ancestor blocks.
			if err := vm.verifyTxs(txs, block.ParentHash(), block.BaseFee(), block.NumberU64(), rules); err != nil {
				return nil, nil, err
			}
		}
		// Update the atomic backend with [txs] from this block.
		//
		// Note: The atomic trie canonically contains the duplicate operations
		// from any bonus blocks.
		_, err := vm.atomicBackend.InsertTxs(block.Hash(), block.NumberU64(), block.ParentHash(), txs)
		if err != nil {
			return nil, nil, err
		}
	}

	// If there are no transactions, we can return early.
	if len(txs) == 0 {
		return nil, nil, nil
	}

	for _, tx := range txs {
		if err := tx.UnsignedAtomicTx.EVMStateTransfer(vm.ctx, state); err != nil {
			return nil, nil, err
		}
		// If ApricotPhase4 is enabled, calculate the block fee contribution
		if rules.IsApricotPhase4 {
			contribution, gasUsed, err := tx.BlockFeeContribution(rules.IsApricotPhase5, vm.ctx.AVAXAssetID, block.BaseFee())
			if err != nil {
				return nil, nil, err
			}

			batchContribution.Add(batchContribution, contribution)
			batchGasUsed.Add(batchGasUsed, gasUsed)
		}

		// If ApricotPhase5 is enabled, enforce that the atomic gas used does not exceed the
		// atomic gas limit.
		if rules.IsApricotPhase5 {
			// Ensure that [tx] does not push [block] above the atomic gas limit.
			if !utils.BigLessOrEqualUint64(batchGasUsed, ap5.AtomicGasLimit) {
				return nil, nil, fmt.Errorf("atomic gas used (%d) by block (%s), exceeds atomic gas limit (%d)", batchGasUsed, block.Hash().Hex(), ap5.AtomicGasLimit)
			}
		}
	}
	return batchContribution, batchGasUsed, nil
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
	if err := vm.StateSyncClient.Error(); err != nil {
		return err
	}
	// After starting bootstrapping, do not attempt to resume a previous state sync.
	if err := vm.StateSyncClient.ClearOngoingSummary(); err != nil {
		return err
	}
	// Ensure snapshots are initialized before bootstrapping (i.e., if state sync is skipped).
	// Note calling this function has no effect if snapshots are already initialized.
	vm.blockChain.InitializeSnapshots()

	return vm.fx.Bootstrapping()
}

// onNormalOperationsStarted marks this VM as bootstrapped
func (vm *VM) onNormalOperationsStarted() error {
	if vm.bootstrapped.Get() {
		return nil
	}
	vm.bootstrapped.Set(true)
	if err := vm.fx.Bootstrapped(); err != nil {
		return err
	}
	// Initialize goroutines related to block building
	// once we enter normal operation as there is no need to handle mempool gossip before this point.
	return vm.initBlockBuilding()
}

// initBlockBuilding starts goroutines to manage block building
func (vm *VM) initBlockBuilding() error {
	ctx, cancel := context.WithCancel(context.TODO())
	vm.cancel = cancel

	ethTxGossipMarshaller := GossipEthTxMarshaller{}
	ethTxGossipClient := vm.Network.NewClient(p2p.TxGossipHandlerID, p2p.WithValidatorSampling(vm.p2pValidators))
	ethTxGossipMetrics, err := gossip.NewMetrics(vm.sdkMetrics, ethTxGossipNamespace)
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

	atomicTxGossipMarshaller := atomic.GossipAtomicTxMarshaller{}
	atomicTxGossipClient := vm.Network.NewClient(p2p.AtomicTxGossipHandlerID, p2p.WithValidatorSampling(vm.p2pValidators))
	atomicTxGossipMetrics, err := gossip.NewMetrics(vm.sdkMetrics, atomicTxGossipNamespace)
	if err != nil {
		return fmt.Errorf("failed to initialize atomic tx gossip metrics: %w", err)
	}

	pushGossipParams := gossip.BranchingFactor{
		StakePercentage: vm.config.PushGossipPercentStake,
		Validators:      vm.config.PushGossipNumValidators,
		Peers:           vm.config.PushGossipNumPeers,
	}
	pushRegossipParams := gossip.BranchingFactor{
		Validators: vm.config.PushRegossipNumValidators,
		Peers:      vm.config.PushRegossipNumPeers,
	}

	ethTxPushGossiper := vm.ethTxPushGossiper.Get()
	if ethTxPushGossiper == nil {
		ethTxPushGossiper, err = gossip.NewPushGossiper[*GossipEthTx](
			ethTxGossipMarshaller,
			ethTxPool,
			vm.p2pValidators,
			ethTxGossipClient,
			ethTxGossipMetrics,
			pushGossipParams,
			pushRegossipParams,
			pushGossipDiscardedElements,
			txGossipTargetMessageSize,
			vm.config.RegossipFrequency.Duration,
		)
		if err != nil {
			return fmt.Errorf("failed to initialize eth tx push gossiper: %w", err)
		}
		vm.ethTxPushGossiper.Set(ethTxPushGossiper)
	}

	if vm.atomicTxPushGossiper == nil {
		vm.atomicTxPushGossiper, err = gossip.NewPushGossiper[*atomic.GossipAtomicTx](
			atomicTxGossipMarshaller,
			vm.mempool,
			vm.p2pValidators,
			atomicTxGossipClient,
			atomicTxGossipMetrics,
			pushGossipParams,
			pushRegossipParams,
			pushGossipDiscardedElements,
			txGossipTargetMessageSize,
			vm.config.RegossipFrequency.Duration,
		)
		if err != nil {
			return fmt.Errorf("failed to initialize atomic tx push gossiper: %w", err)
		}
	}

	// NOTE: gossip network must be initialized first otherwise ETH tx gossip will not work.
	vm.builder = vm.NewBlockBuilder(vm.toEngine)
	vm.builder.awaitSubmittedTxs()

	var p2pValidators p2p.ValidatorSet = &validatorSet{}
	if vm.config.PullGossipFrequency.Duration > 0 {
		p2pValidators = vm.p2pValidators
	}

	if vm.ethTxGossipHandler == nil {
		vm.ethTxGossipHandler = newTxGossipHandler[*GossipEthTx](
			vm.ctx.Log,
			ethTxGossipMarshaller,
			ethTxPool,
			ethTxGossipMetrics,
			txGossipTargetMessageSize,
			txGossipThrottlingPeriod,
			txGossipThrottlingLimit,
			p2pValidators,
		)
	}

	if err := vm.Network.AddHandler(p2p.TxGossipHandlerID, vm.ethTxGossipHandler); err != nil {
		return fmt.Errorf("failed to add eth tx gossip handler: %w", err)
	}

	if vm.atomicTxGossipHandler == nil {
		vm.atomicTxGossipHandler = newTxGossipHandler[*atomic.GossipAtomicTx](
			vm.ctx.Log,
			atomicTxGossipMarshaller,
			vm.mempool,
			atomicTxGossipMetrics,
			txGossipTargetMessageSize,
			txGossipThrottlingPeriod,
			txGossipThrottlingLimit,
			p2pValidators,
		)
	}

	if err := vm.Network.AddHandler(p2p.AtomicTxGossipHandlerID, vm.atomicTxGossipHandler); err != nil {
		return fmt.Errorf("failed to add atomic tx gossip handler: %w", err)
	}

	if vm.ethTxPullGossiper == nil {
		ethTxPullGossiper := gossip.NewPullGossiper[*GossipEthTx](
			vm.ctx.Log,
			ethTxGossipMarshaller,
			ethTxPool,
			ethTxGossipClient,
			ethTxGossipMetrics,
			txGossipPollSize,
		)

		vm.ethTxPullGossiper = gossip.ValidatorGossiper{
			Gossiper:   ethTxPullGossiper,
			NodeID:     vm.ctx.NodeID,
			Validators: vm.p2pValidators,
		}
	}

	if vm.config.PushGossipFrequency.Duration > 0 {
		vm.shutdownWg.Add(1)
		go func() {
			gossip.Every(ctx, vm.ctx.Log, ethTxPushGossiper, vm.config.PushGossipFrequency.Duration)
			vm.shutdownWg.Done()
		}()
	}
	if vm.config.PullGossipFrequency.Duration > 0 {
		vm.shutdownWg.Add(1)
		go func() {
			gossip.Every(ctx, vm.ctx.Log, vm.ethTxPullGossiper, vm.config.PullGossipFrequency.Duration)
			vm.shutdownWg.Done()
		}()
	}

	if vm.atomicTxPullGossiper == nil {
		atomicTxPullGossiper := gossip.NewPullGossiper[*atomic.GossipAtomicTx](
			vm.ctx.Log,
			atomicTxGossipMarshaller,
			vm.mempool,
			atomicTxGossipClient,
			atomicTxGossipMetrics,
			txGossipPollSize,
		)

		vm.atomicTxPullGossiper = &gossip.ValidatorGossiper{
			Gossiper:   atomicTxPullGossiper,
			NodeID:     vm.ctx.NodeID,
			Validators: vm.p2pValidators,
		}
	}

	if vm.config.PushGossipFrequency.Duration > 0 {
		vm.shutdownWg.Add(1)
		go func() {
			gossip.Every(ctx, vm.ctx.Log, vm.atomicTxPushGossiper, vm.config.PushGossipFrequency.Duration)
			vm.shutdownWg.Done()
		}()
	}
	if vm.config.PullGossipFrequency.Duration > 0 {
		vm.shutdownWg.Add(1)
		go func() {
			gossip.Every(ctx, vm.ctx.Log, vm.atomicTxPullGossiper, vm.config.PullGossipFrequency.Duration)
			vm.shutdownWg.Done()
		}()
	}

	return nil
}

// setAppRequestHandlers sets the request handlers for the VM to serve state sync
// requests.
func (vm *VM) setAppRequestHandlers() {
	// Create standalone EVM TrieDB (read only) for serving leafs requests.
	// We create a standalone TrieDB here, so that it has a standalone cache from the one
	// used by the node when processing blocks.
	evmTrieDB := triedb.NewDatabase(
		vm.chaindb,
		&triedb.Config{
			HashDB: &hashdb.Config{
				CleanCacheSize: vm.config.StateSyncServerTrieCache * units.MiB,
			},
		},
	)
	networkHandler := newNetworkHandler(
		vm.blockChain,
		vm.chaindb,
		evmTrieDB,
		vm.atomicTrie.TrieDB(),
		vm.warpBackend,
		vm.networkCodec,
	)
	vm.Network.SetRequestHandler(networkHandler)
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
	if err := vm.StateSyncClient.Shutdown(); err != nil {
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

func (vm *VM) buildBlockWithContext(ctx context.Context, proposerVMBlockCtx *block.Context) (snowman.Block, error) {
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
		vm.mempool.CancelCurrentTxs()
		return nil, err
	}

	// Note: the status of block is set by ChainState
	blk, err := vm.newBlock(block)
	if err != nil {
		log.Debug("discarding txs due to error making new block", "err", err)
		vm.mempool.DiscardCurrentTxs()
		return nil, err
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
		vm.mempool.CancelCurrentTxs()
		return nil, fmt.Errorf("block failed verification due to: %w", err)
	}

	log.Debug(fmt.Sprintf("Built block %s", blk.ID()))
	// Marks the current transactions from the mempool as being successfully issued
	// into a block.
	vm.mempool.IssueCurrentTxs()
	return blk, nil
}

// parseBlock parses [b] into a block to be wrapped by ChainState.
func (vm *VM) parseBlock(_ context.Context, b []byte) (snowman.Block, error) {
	ethBlock := new(types.Block)
	if err := rlp.DecodeBytes(b, ethBlock); err != nil {
		return nil, err
	}

	// Note: the status of block is set by ChainState
	block, err := vm.newBlock(ethBlock)
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

	return block.(*Block).ethBlock, nil
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
	return vm.newBlock(ethBlock)
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

	return vm.blockChain.SetPreference(block.(*Block).ethBlock)
}

// VerifyHeightIndex always returns a nil error since the index is maintained by
// vm.blockChain.
func (vm *VM) VerifyHeightIndex(context.Context) error {
	return nil
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

func (vm *VM) Version(context.Context) (string, error) {
	return Version, nil
}

// NewHandler returns a new Handler for a service where:
//   - The handler's functionality is defined by [service]
//     [service] should be a gorilla RPC service (see https://www.gorillatoolkit.org/pkg/rpc/v2)
//   - The name of the service is [name]
func newHandler(name string, service interface{}) (http.Handler, error) {
	server := avalancheRPC.NewServer()
	server.RegisterCodec(avalancheJSON.NewCodec(), "application/json")
	server.RegisterCodec(avalancheJSON.NewCodec(), "application/json;charset=UTF-8")
	return server, server.RegisterService(service, name)
}

// CreateHandlers makes new http handlers that can handle API calls
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	handler := rpc.NewServer(vm.config.APIMaxDuration.Duration)
	if vm.config.HttpBodyLimit > 0 {
		handler.SetHTTPBodyLimit(int(vm.config.HttpBodyLimit))
	}

	enabledAPIs := vm.config.EthAPIs()
	if err := attachEthService(handler, vm.eth.APIs(), enabledAPIs); err != nil {
		return nil, err
	}

	apis := make(map[string]http.Handler)
	avaxAPI, err := newHandler("avax", &AvaxAPI{vm})
	if err != nil {
		return nil, fmt.Errorf("failed to register service for AVAX API due to %w", err)
	}
	enabledAPIs = append(enabledAPIs, "avax")
	apis[avaxEndpoint] = avaxAPI

	if vm.config.AdminAPIEnabled {
		adminAPI, err := newHandler("admin", NewAdminService(vm, os.ExpandEnv(fmt.Sprintf("%s_coreth_performance_%s", vm.config.AdminAPIDir, vm.chainAlias))))
		if err != nil {
			return nil, fmt.Errorf("failed to register service for admin API due to %w", err)
		}
		apis[adminEndpoint] = adminAPI
		enabledAPIs = append(enabledAPIs, "coreth-admin")
	}

	// RPC APIs
	if vm.config.SnowmanAPIEnabled {
		if err := handler.RegisterName("snowman", &SnowmanAPI{vm}); err != nil {
			return nil, err
		}
		enabledAPIs = append(enabledAPIs, "snowman")
	}

	if vm.config.WarpAPIEnabled {
		if err := handler.RegisterName("warp", warp.NewAPI(vm.ctx.NetworkID, vm.ctx.SubnetID, vm.ctx.ChainID, vm.ctx.ValidatorState, vm.warpBackend, vm.client, vm.requirePrimaryNetworkSigners)); err != nil {
			return nil, err
		}
		enabledAPIs = append(enabledAPIs, "warp")
	}

	log.Info(fmt.Sprintf("Enabled APIs: %s", strings.Join(enabledAPIs, ", ")))
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

// CreateStaticHandlers makes new http handlers that can handle API calls
func (vm *VM) CreateStaticHandlers(context.Context) (map[string]http.Handler, error) {
	handler := rpc.NewServer(0)
	if vm.config.HttpBodyLimit > 0 {
		handler.SetHTTPBodyLimit(int(vm.config.HttpBodyLimit))
	}
	if err := handler.RegisterName("static", &StaticService{}); err != nil {
		return nil, err
	}

	vm.rpcHandlers = append(vm.rpcHandlers, handler)
	return map[string]http.Handler{
		"/rpc": handler,
	}, nil
}

/*
 ******************************************************************************
 *********************************** Helpers **********************************
 ******************************************************************************
 */

// getAtomicTx returns the requested transaction, status, and height.
// If the status is Unknown, then the returned transaction will be nil.
func (vm *VM) getAtomicTx(txID ids.ID) (*atomic.Tx, atomic.Status, uint64, error) {
	if tx, height, err := vm.atomicTxRepository.GetByTxID(txID); err == nil {
		return tx, atomic.Accepted, height, nil
	} else if err != database.ErrNotFound {
		return nil, atomic.Unknown, 0, err
	}
	tx, dropped, found := vm.mempool.GetTx(txID)
	switch {
	case found && dropped:
		return tx, atomic.Dropped, 0, nil
	case found:
		return tx, atomic.Processing, 0, nil
	default:
		return nil, atomic.Unknown, 0, nil
	}
}

// ParseAddress takes in an address and produces the ID of the chain it's for
// the ID of the address
func (vm *VM) ParseAddress(addrStr string) (ids.ID, ids.ShortID, error) {
	chainIDAlias, hrp, addrBytes, err := address.Parse(addrStr)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}

	chainID, err := vm.ctx.BCLookup.Lookup(chainIDAlias)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}

	expectedHRP := avalanchegoConstants.GetHRP(vm.ctx.NetworkID)
	if hrp != expectedHRP {
		return ids.ID{}, ids.ShortID{}, fmt.Errorf("expected hrp %q but got %q",
			expectedHRP, hrp)
	}

	addr, err := ids.ToShortID(addrBytes)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}
	return chainID, addr, nil
}

// verifyTxAtTip verifies that [tx] is valid to be issued on top of the currently preferred block
func (vm *VM) verifyTxAtTip(tx *atomic.Tx) error {
	if txByteLen := len(tx.SignedBytes()); txByteLen > targetAtomicTxsSize {
		return fmt.Errorf("tx size (%d) exceeds total atomic txs size target (%d)", txByteLen, targetAtomicTxsSize)
	}
	gasUsed, err := tx.GasUsed(true)
	if err != nil {
		return err
	}
	if gasUsed > ap5.AtomicGasLimit {
		return fmt.Errorf("tx gas usage (%d) exceeds atomic gas limit (%d)", gasUsed, ap5.AtomicGasLimit)
	}

	// Note: we fetch the current block and then the state at that block instead of the current state directly
	// since we need the header of the current block below.
	preferredBlock := vm.blockChain.CurrentBlock()
	preferredState, err := vm.blockChain.StateAt(preferredBlock.Root)
	if err != nil {
		return fmt.Errorf("failed to retrieve block state at tip while verifying atomic tx: %w", err)
	}
	rules := vm.currentRules()
	parentHeader := preferredBlock
	var nextBaseFee *big.Int
	timestamp := uint64(vm.clock.Time().Unix())
	if vm.chainConfig.IsApricotPhase3(timestamp) {
		nextBaseFee, err = header.EstimateNextBaseFee(vm.chainConfig, parentHeader, timestamp)
		if err != nil {
			// Return extremely detailed error since CalcBaseFee should never encounter an issue here
			return fmt.Errorf("failed to calculate base fee with parent timestamp (%d), parent ExtraData: (0x%x), and current timestamp (%d): %w", parentHeader.Time, parentHeader.Extra, timestamp, err)
		}
	}

	// We don’t need to revert the state here in case verifyTx errors, because
	// [preferredState] is thrown away either way.
	return vm.verifyTx(tx, parentHeader.Hash(), nextBaseFee, preferredState, rules)
}

// verifyTx verifies that [tx] is valid to be issued into a block with parent block [parentHash]
// and validated at [state] using [rules] as the current rule set.
// Note: verifyTx may modify [state]. If [state] needs to be properly maintained, the caller is responsible
// for reverting to the correct snapshot after calling this function. If this function is called with a
// throwaway state, then this is not necessary.
func (vm *VM) verifyTx(tx *atomic.Tx, parentHash common.Hash, baseFee *big.Int, state *state.StateDB, rules params.Rules) error {
	parentIntf, err := vm.GetBlockInternal(context.TODO(), ids.ID(parentHash))
	if err != nil {
		return fmt.Errorf("failed to get parent block: %w", err)
	}
	parent, ok := parentIntf.(*Block)
	if !ok {
		return fmt.Errorf("parent block %s had unexpected type %T", parentIntf.ID(), parentIntf)
	}
	atomicBackend := &atomic.Backend{
		Ctx:          vm.ctx,
		Fx:           &vm.fx,
		Rules:        rules,
		Bootstrapped: vm.bootstrapped.Get(),
		BlockFetcher: vm,
		SecpCache:    &vm.secpCache,
	}
	if err := tx.UnsignedAtomicTx.SemanticVerify(atomicBackend, tx, parent, baseFee); err != nil {
		return err
	}
	return tx.UnsignedAtomicTx.EVMStateTransfer(vm.ctx, state)
}

// verifyTxs verifies that [txs] are valid to be issued into a block with parent block [parentHash]
// using [rules] as the current rule set.
func (vm *VM) verifyTxs(txs []*atomic.Tx, parentHash common.Hash, baseFee *big.Int, height uint64, rules params.Rules) error {
	// Ensure that the parent was verified and inserted correctly.
	if !vm.blockChain.HasBlock(parentHash, height-1) {
		return errRejectedParent
	}

	ancestorID := ids.ID(parentHash)
	// If the ancestor is unknown, then the parent failed verification when
	// it was called.
	// If the ancestor is rejected, then this block shouldn't be inserted
	// into the canonical chain because the parent will be missing.
	ancestorInf, err := vm.GetBlockInternal(context.TODO(), ancestorID)
	if err != nil {
		return errRejectedParent
	}
	ancestor, ok := ancestorInf.(*Block)
	if !ok {
		return fmt.Errorf("expected parent block %s, to be *Block but is %T", ancestor.ID(), ancestorInf)
	}

	// Ensure each tx in [txs] doesn't conflict with any other atomic tx in
	// a processing ancestor block.
	inputs := set.Set[ids.ID]{}
	atomicBackend := &atomic.Backend{
		Ctx:          vm.ctx,
		Fx:           &vm.fx,
		Rules:        rules,
		Bootstrapped: vm.bootstrapped.Get(),
		BlockFetcher: vm,
		SecpCache:    &vm.secpCache,
	}
	for _, atomicTx := range txs {
		utx := atomicTx.UnsignedAtomicTx
		if err := utx.SemanticVerify(atomicBackend, atomicTx, ancestor, baseFee); err != nil {
			return fmt.Errorf("invalid block due to failed semanatic verify: %w at height %d", err, height)
		}
		txInputs := utx.InputUTXOs()
		if inputs.Overlaps(txInputs) {
			return atomic.ErrConflictingAtomicInputs
		}
		inputs.Union(txInputs)
	}
	return nil
}

// GetAtomicUTXOs returns the utxos that at least one of the provided addresses is
// referenced in.
func (vm *VM) GetAtomicUTXOs(
	chainID ids.ID,
	addrs set.Set[ids.ShortID],
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
	if limit <= 0 || limit > maxUTXOsToFetch {
		limit = maxUTXOsToFetch
	}

	return avax.GetAtomicUTXOs(
		vm.ctx.SharedMemory,
		atomic.Codec,
		chainID,
		addrs,
		startAddr,
		startUTXOID,
		limit,
	)
}

// currentRules returns the chain rules for the current block.
func (vm *VM) currentRules() params.Rules {
	header := vm.eth.APIBackend.CurrentHeader()
	return vm.chainConfig.Rules(header.Number, header.Time)
}

// requirePrimaryNetworkSigners returns true if warp messages from the primary
// network must be signed by the primary network validators.
// This is necessary when the subnet is not validating the primary network.
func (vm *VM) requirePrimaryNetworkSigners() bool {
	switch c := vm.currentRules().ActivePrecompiles[warpcontract.ContractAddress].(type) {
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
		filepath.Join(vm.config.ContinuousProfilerDir),
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
func (vm *VM) readLastAccepted() (common.Hash, uint64, error) {
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

func (vm *VM) newImportTx(
	chainID ids.ID, // chain to import from
	to common.Address, // Address of recipient
	baseFee *big.Int, // fee to use post-AP3
	keys []*secp256k1.PrivateKey, // Keys to import the funds
) (*atomic.Tx, error) {
	kc := secp256k1fx.NewKeychain()
	for _, key := range keys {
		kc.Add(key)
	}

	atomicUTXOs, _, _, err := vm.GetAtomicUTXOs(chainID, kc.Addresses(), ids.ShortEmpty, ids.Empty, -1)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}

	return atomic.NewImportTx(vm.ctx, vm.currentRules(), vm.clock.Unix(), chainID, to, baseFee, kc, atomicUTXOs)
}

// newExportTx returns a new ExportTx
func (vm *VM) newExportTx(
	assetID ids.ID, // AssetID of the tokens to export
	amount uint64, // Amount of tokens to export
	chainID ids.ID, // Chain to send the UTXOs to
	to ids.ShortID, // Address of chain recipient
	baseFee *big.Int, // fee to use post-AP3
	keys []*secp256k1.PrivateKey, // Pay the fee and provide the tokens
) (*atomic.Tx, error) {
	state, err := vm.blockChain.State()
	if err != nil {
		return nil, err
	}

	// Create the transaction
	tx, err := atomic.NewExportTx(
		vm.ctx,            // Context
		vm.currentRules(), // VM rules
		state,
		assetID, // AssetID
		amount,  // Amount
		chainID, // ID of the chain to send the funds to
		to,      // Address
		baseFee,
		keys, // Private keys
	)
	if err != nil {
		return nil, err
	}

	return tx, nil
}
