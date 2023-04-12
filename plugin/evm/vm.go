// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	avalanchegoMetrics "github.com/ava-labs/avalanchego/api/metrics"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/constants"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/eth"
	"github.com/ava-labs/subnet-evm/eth/ethconfig"
	"github.com/ava-labs/subnet-evm/metrics"
	subnetEVMPrometheus "github.com/ava-labs/subnet-evm/metrics/prometheus"
	"github.com/ava-labs/subnet-evm/miner"
	"github.com/ava-labs/subnet-evm/node"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/peer"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/rpc"
	statesyncclient "github.com/ava-labs/subnet-evm/sync/client"
	"github.com/ava-labs/subnet-evm/sync/client/stats"
	"github.com/ava-labs/subnet-evm/trie"
	"github.com/ava-labs/subnet-evm/warp"

	// Force-load tracer engine to trigger registration
	//
	// We must import this package (not referenced elsewhere) so that the native "callTracer"
	// is added to a map of client-accessible tracers. In geth, this is done
	// inside of cmd/geth.
	_ "github.com/ava-labs/subnet-evm/eth/tracers/native"

	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	// Force-load precompiles to trigger registration
	_ "github.com/ava-labs/subnet-evm/precompile/registry"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	avalancheRPC "github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	cjson "github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/chain"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"

	avalancheJSON "github.com/ava-labs/avalanchego/utils/json"
)

var (
	_ block.ChainVM                      = &VM{}
	_ block.HeightIndexedChainVM         = &VM{}
	_ block.BuildBlockWithContextChainVM = &VM{}
)

const (
	// Max time from current time allowed for blocks, before they're considered future blocks
	// and fail verification
	maxFutureBlockTime = 10 * time.Second

	decidedCacheSize       = 100
	missingCacheSize       = 50
	unverifiedCacheSize    = 50
	warpSignatureCacheSize = 500

	// Prefixes for metrics gatherers
	ethMetricsPrefix        = "eth"
	chainStateMetricsPrefix = "chain_state"
)

// Define the API endpoints for the VM
const (
	adminEndpoint  = "/admin"
	ethRPCEndpoint = "/rpc"
	ethWSEndpoint  = "/ws"
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
	errEmptyBlock                 = errors.New("empty block")
	errUnsupportedFXs             = errors.New("unsupported feature extensions")
	errInvalidBlock               = errors.New("invalid block")
	errInvalidNonce               = errors.New("invalid nonce")
	errUnclesUnsupported          = errors.New("uncles unsupported")
	errNilBaseFeeSubnetEVM        = errors.New("nil base fee is invalid after subnetEVM")
	errNilBlockGasCostSubnetEVM   = errors.New("nil blockGasCost is invalid after subnetEVM")
	errSubnetEVMUpgradeNotEnabled = errors.New("SubnetEVM upgrade is not enabled in genesis")
)

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

// VM implements the snowman.ChainVM interface
type VM struct {
	ctx *snow.Context
	// *chain.State helps to implement the VM interface by wrapping blocks
	// with an efficient caching layer.
	*chain.State

	config Config

	networkID   uint64
	genesisHash common.Hash
	chainConfig *params.ChainConfig
	ethConfig   ethconfig.Config

	// pointers to eth constructs
	eth        *eth.Ethereum
	txPool     *core.TxPool
	blockChain *core.BlockChain
	miner      *miner.Miner

	// [db] is the VM's current database managed by ChainState
	db *versiondb.Database

	// metadataDB is used to store one off keys.
	metadataDB database.Database

	// [chaindb] is the database supplied to the Ethereum backend
	chaindb Database

	// [acceptedBlockDB] is the database to store the last accepted
	// block.
	acceptedBlockDB database.Database

	// [warpDB] is used to store warp message signatures
	// set to a prefixDB with the prefix [warpPrefix]
	warpDB database.Database

	toEngine chan<- commonEng.Message

	syntacticBlockValidator BlockValidator

	builder *blockBuilder

	gossiper Gossiper

	clock mockable.Clock

	shutdownChan chan struct{}
	shutdownWg   sync.WaitGroup

	// Continuous Profiler
	profiler profiler.ContinuousProfiler

	peer.Network
	client       peer.NetworkClient
	networkCodec codec.Manager

	// Metrics
	multiGatherer avalanchegoMetrics.MultiGatherer

	bootstrapped bool

	logger SubnetEVMLogger
	// State sync server and client
	StateSyncServer
	StateSyncClient

	// Avalanche Warp Messaging backend
	// Used to serve BLS signatures of warp messages over RPC
	warpBackend warp.WarpBackend
}

// Initialize implements the snowman.ChainVM interface
func (vm *VM) Initialize(
	_ context.Context,
	chainCtx *snow.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- commonEng.Message,
	fxs []*commonEng.Fx,
	appSender commonEng.AppSender,
) error {
	vm.config.SetDefaults()
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &vm.config); err != nil {
			return fmt.Errorf("failed to unmarshal config %s: %w", string(configBytes), err)
		}
	}
	if err := vm.config.Validate(); err != nil {
		return err
	}

	vm.ctx = chainCtx

	// Create logger
	alias, err := vm.ctx.BCLookup.PrimaryAlias(vm.ctx.ChainID)
	if err != nil {
		// fallback to ChainID string instead of erroring
		alias = vm.ctx.ChainID.String()
	}

	subnetEVMLogger, err := InitLogger(alias, vm.config.LogLevel, vm.config.LogJSONFormat, vm.ctx.Log)
	if err != nil {
		return fmt.Errorf("failed to initialize logger due to: %w ", err)
	}
	vm.logger = subnetEVMLogger

	log.Info("Initializing Subnet EVM VM", "Version", Version, "Config", vm.config)

	if len(fxs) > 0 {
		return errUnsupportedFXs
	}

	// Enable debug-level metrics that might impact runtime performance
	metrics.EnabledExpensive = vm.config.MetricsExpensiveEnabled

	vm.toEngine = toEngine
	vm.shutdownChan = make(chan struct{}, 1)
	baseDB := dbManager.Current().Database
	// Use NewNested rather than New so that the structure of the database
	// remains the same regardless of the provided baseDB type.
	vm.chaindb = Database{prefixdb.NewNested(ethDBPrefix, baseDB)}
	vm.db = versiondb.New(baseDB)
	vm.acceptedBlockDB = prefixdb.New(acceptedPrefix, vm.db)
	vm.metadataDB = prefixdb.New(metadataPrefix, vm.db)
	vm.warpDB = prefixdb.New(warpPrefix, vm.db)

	if vm.config.InspectDatabase {
		start := time.Now()
		log.Info("Starting database inspection")
		if err := rawdb.InspectDatabase(vm.chaindb, nil, nil); err != nil {
			return err
		}
		log.Info("Completed database inspection", "elapsed", time.Since(start))
	}

	g := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, g); err != nil {
		return err
	}

	if g.Config == nil {
		g.Config = params.SubnetEVMDefaultChainConfig
	}

	// Load airdrop file if provided
	if vm.config.AirdropFile != "" {
		g.AirdropData, err = os.ReadFile(vm.config.AirdropFile)
		if err != nil {
			return fmt.Errorf("could not read airdrop file '%s': %w", vm.config.AirdropFile, err)
		}
	}
	// Set the Avalanche Context on the ChainConfig
	g.Config.AvalancheContext = params.AvalancheContext{
		SnowCtx: chainCtx,
	}
	vm.syntacticBlockValidator = NewBlockValidator()

	if g.Config.FeeConfig == commontype.EmptyFeeConfig {
		log.Info("No fee config given in genesis, setting default fee config", "DefaultFeeConfig", params.DefaultFeeConfig)
		g.Config.FeeConfig = params.DefaultFeeConfig
	}

	vm.ethConfig = ethconfig.NewDefaultConfig()
	vm.ethConfig.Genesis = g
	vm.ethConfig.NetworkId = g.Config.ChainID.Uint64()

	// Set minimum price for mining and default gas price oracle value to the min
	// gas price to prevent so transactions and blocks all use the correct fees
	vm.ethConfig.RPCGasCap = vm.config.RPCGasCap
	vm.ethConfig.RPCEVMTimeout = vm.config.APIMaxDuration.Duration
	vm.ethConfig.RPCTxFeeCap = vm.config.RPCTxFeeCap

	vm.ethConfig.TxPool.Locals = vm.config.PriorityRegossipAddresses
	vm.ethConfig.TxPool.NoLocals = !vm.config.LocalTxsEnabled
	vm.ethConfig.TxPool.Journal = vm.config.TxPoolJournal
	vm.ethConfig.TxPool.Rejournal = vm.config.TxPoolRejournal.Duration
	vm.ethConfig.TxPool.PriceLimit = vm.config.TxPoolPriceLimit
	vm.ethConfig.TxPool.PriceBump = vm.config.TxPoolPriceBump
	vm.ethConfig.TxPool.AccountSlots = vm.config.TxPoolAccountSlots
	vm.ethConfig.TxPool.GlobalSlots = vm.config.TxPoolGlobalSlots
	vm.ethConfig.TxPool.AccountQueue = vm.config.TxPoolAccountQueue
	vm.ethConfig.TxPool.GlobalQueue = vm.config.TxPoolGlobalQueue

	vm.ethConfig.AllowUnfinalizedQueries = vm.config.AllowUnfinalizedQueries
	vm.ethConfig.AllowUnprotectedTxs = vm.config.AllowUnprotectedTxs
	vm.ethConfig.AllowUnprotectedTxHashes = vm.config.AllowUnprotectedTxHashes
	vm.ethConfig.Preimages = vm.config.Preimages
	vm.ethConfig.Pruning = vm.config.Pruning
	vm.ethConfig.TrieCleanCache = vm.config.TrieCleanCache
	vm.ethConfig.TrieCleanJournal = vm.config.TrieCleanJournal
	vm.ethConfig.TrieCleanRejournal = vm.config.TrieCleanRejournal.Duration
	vm.ethConfig.TrieDirtyCache = vm.config.TrieDirtyCache
	vm.ethConfig.TrieDirtyCommitTarget = vm.config.TrieDirtyCommitTarget
	vm.ethConfig.SnapshotCache = vm.config.SnapshotCache
	vm.ethConfig.AcceptorQueueLimit = vm.config.AcceptorQueueLimit
	vm.ethConfig.PopulateMissingTries = vm.config.PopulateMissingTries
	vm.ethConfig.PopulateMissingTriesParallelism = vm.config.PopulateMissingTriesParallelism
	vm.ethConfig.AllowMissingTries = vm.config.AllowMissingTries
	vm.ethConfig.SnapshotDelayInit = vm.config.StateSyncEnabled
	vm.ethConfig.SnapshotAsync = vm.config.SnapshotAsync
	vm.ethConfig.SnapshotVerify = vm.config.SnapshotVerify
	vm.ethConfig.OfflinePruning = vm.config.OfflinePruning
	vm.ethConfig.OfflinePruningBloomFilterSize = vm.config.OfflinePruningBloomFilterSize
	vm.ethConfig.OfflinePruningDataDirectory = vm.config.OfflinePruningDataDirectory
	vm.ethConfig.CommitInterval = vm.config.CommitInterval
	vm.ethConfig.SkipUpgradeCheck = vm.config.SkipUpgradeCheck
	vm.ethConfig.AcceptedCacheSize = vm.config.AcceptedCacheSize
	vm.ethConfig.TxLookupLimit = vm.config.TxLookupLimit

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

	vm.chainConfig = g.Config
	vm.networkID = vm.ethConfig.NetworkId

	// TODO: remove SkipSubnetEVMUpgradeCheck after next network upgrade
	if !vm.config.SkipSubnetEVMUpgradeCheck {
		// check that subnetEVM upgrade is enabled from genesis before upgradeBytes
		if !vm.chainConfig.IsSubnetEVM(common.Big0) {
			return errSubnetEVMUpgradeNotEnabled
		}
	}

	// Apply upgradeBytes (if any) by unmarshalling them into [chainConfig.UpgradeConfig].
	// Initializing the chain will verify upgradeBytes are compatible with existing values.
	if len(upgradeBytes) > 0 {
		var upgradeConfig params.UpgradeConfig
		if err := json.Unmarshal(upgradeBytes, &upgradeConfig); err != nil {
			return fmt.Errorf("failed to parse upgrade bytes: %w", err)
		}
		vm.chainConfig.UpgradeConfig = upgradeConfig
	}

	// create genesisHash after applying upgradeBytes in case
	// upgradeBytes modifies genesis.
	vm.genesisHash = vm.ethConfig.Genesis.ToBlock(nil).Hash()

	lastAcceptedHash, lastAcceptedHeight, err := vm.readLastAccepted()
	if err != nil {
		return err
	}
	log.Info("reading accepted block db", "lastAcceptedHash", lastAcceptedHash)

	if err := vm.initializeMetrics(); err != nil {
		return err
	}

	// initialize peer network
	vm.networkCodec = message.Codec
	vm.Network = peer.NewNetwork(appSender, vm.networkCodec, message.CrossChainCodec, chainCtx.NodeID, vm.config.MaxOutboundActiveRequests, vm.config.MaxOutboundActiveCrossChainRequests)
	vm.client = peer.NewNetworkClient(vm.Network)

	// initialize warp backend
	vm.warpBackend = warp.NewWarpBackend(vm.ctx, vm.warpDB, warpSignatureCacheSize)

	if err := vm.initializeChain(lastAcceptedHash, vm.ethConfig); err != nil {
		return err
	}

	go vm.ctx.Log.RecoverAndPanic(vm.startContinuousProfiler)

	vm.initializeStateSyncServer()
	return vm.initializeStateSyncClient(lastAcceptedHeight)
}

func (vm *VM) initializeMetrics() error {
	vm.multiGatherer = avalanchegoMetrics.NewMultiGatherer()
	// If metrics are enabled, register the default metrics regitry
	if metrics.Enabled {
		gatherer := subnetEVMPrometheus.Gatherer(metrics.DefaultRegistry)
		if err := vm.multiGatherer.Register(ethMetricsPrefix, gatherer); err != nil {
			return err
		}
		// Register [multiGatherer] after registerers have been registered to it
		if err := vm.ctx.Metrics.Register(vm.multiGatherer); err != nil {
			return err
		}
	}
	return nil
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
	vm.eth, err = eth.New(
		node,
		&vm.ethConfig,
		vm.chaindb,
		vm.config.EthBackendSettings(),
		lastAcceptedHash,
		&vm.clock,
	)
	if err != nil {
		return err
	}
	vm.eth.SetEtherbase(ethConfig.Miner.Etherbase)
	vm.txPool = vm.eth.TxPool()
	vm.txPool.SetMinFee(vm.chainConfig.FeeConfig.MinBaseFee)
	vm.txPool.SetGasPrice(big.NewInt(0))
	vm.blockChain = vm.eth.BlockChain()
	vm.miner = vm.eth.Miner()

	vm.eth.Start()
	return vm.initChainState(vm.blockChain.LastAcceptedBlock())
}

// initializeStateSyncClient initializes the client for performing state sync.
// If state sync is disabled, this function will wipe any ongoing summary from
// disk to ensure that we do not continue syncing from an invalid snapshot.
func (vm *VM) initializeStateSyncClient(lastAcceptedHeight uint64) error {
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
		enabled:              vm.config.StateSyncEnabled,
		skipResume:           vm.config.StateSyncSkipResume,
		stateSyncMinBlocks:   vm.config.StateSyncMinBlocks,
		stateSyncRequestSize: vm.config.StateSyncRequestSize,
		lastAcceptedHeight:   lastAcceptedHeight, // TODO clean up how this is passed around
		chaindb:              vm.chaindb,
		metadataDB:           vm.metadataDB,
		acceptedBlockDB:      vm.acceptedBlockDB,
		db:                   vm.db,
		toEngine:             vm.toEngine,
	})

	// If StateSync is disabled, clear any ongoing summary so that we will not attempt to resume
	// sync using a snapshot that has been modified by the node running normal operations.
	if !vm.config.StateSyncEnabled {
		return vm.StateSyncClient.StateSyncClearOngoingSummary()
	}

	return nil
}

// initializeStateSyncServer should be called after [vm.chain] is initialized.
func (vm *VM) initializeStateSyncServer() {
	vm.StateSyncServer = NewStateSyncServer(&stateSyncServerConfig{
		Chain:            vm.blockChain,
		SyncableInterval: vm.config.StateSyncCommitInterval,
	})

	vm.setAppRequestHandlers()
	vm.setCrossChainAppRequestHandler()
}

func (vm *VM) initChainState(lastAcceptedBlock *types.Block) error {
	block := vm.newBlock(lastAcceptedBlock)
	block.status = choices.Accepted

	config := &chain.Config{
		DecidedCacheSize:      decidedCacheSize,
		MissingCacheSize:      missingCacheSize,
		UnverifiedCacheSize:   unverifiedCacheSize,
		GetBlockIDAtHeight:    vm.GetBlockIDAtHeight,
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

	return vm.multiGatherer.Register(chainStateMetricsPrefix, chainStateRegisterer)
}

func (vm *VM) SetState(_ context.Context, state snow.State) error {
	switch state {
	case snow.StateSyncing:
		vm.bootstrapped = false
		return nil
	case snow.Bootstrapping:
		vm.bootstrapped = false
		if err := vm.StateSyncClient.Error(); err != nil {
			return err
		}
		return nil
	case snow.NormalOp:
		// Initialize gossip handling once we enter normal operation as there is no need to handle mempool gossip before this point.
		vm.initBlockBuilding()
		vm.bootstrapped = true
		return nil
	default:
		return snow.ErrUnknownState
	}
}

// initBlockBuilding starts goroutines to manage block building
func (vm *VM) initBlockBuilding() {
	// NOTE: gossip network must be initialized first otherwise ETH tx gossip will not work.
	gossipStats := NewGossipStats()
	vm.gossiper = vm.createGossiper(gossipStats)
	vm.builder = vm.NewBlockBuilder(vm.toEngine)
	vm.builder.awaitSubmittedTxs()
	vm.Network.SetGossipHandler(NewGossipHandler(vm, gossipStats))
}

// setAppRequestHandlers sets the request handlers for the VM to serve state sync
// requests.
func (vm *VM) setAppRequestHandlers() {
	// Create separate EVM TrieDB (read only) for serving leafs requests.
	// We create a separate TrieDB here, so that it has a separate cache from the one
	// used by the node when processing blocks.
	evmTrieDB := trie.NewDatabaseWithConfig(
		vm.chaindb,
		&trie.Config{
			Cache: vm.config.StateSyncServerTrieCache,
		},
	)

	networkHandler := newNetworkHandler(vm.blockChain, evmTrieDB, vm.networkCodec)
	vm.Network.SetRequestHandler(networkHandler)
}

// setCrossChainAppRequestHandler sets the request handlers for the VM to serve cross chain
// requests.
func (vm *VM) setCrossChainAppRequestHandler() {
	crossChainRequestHandler := message.NewCrossChainHandler(vm.eth.APIBackend, message.CrossChainCodec)
	vm.Network.SetCrossChainRequestHandler(crossChainRequestHandler)
}

// Shutdown implements the snowman.ChainVM interface
func (vm *VM) Shutdown(context.Context) error {
	if vm.ctx == nil {
		return nil
	}
	vm.Network.Shutdown()
	if err := vm.StateSyncClient.Shutdown(); err != nil {
		log.Error("error stopping state syncer", "err", err)
	}
	close(vm.shutdownChan)
	vm.eth.Stop()
	log.Info("Ethereum backend stop completed")
	vm.shutdownWg.Wait()
	log.Info("Subnet-EVM Shutdown completed")
	return nil
}

func (vm *VM) buildBlock(ctx context.Context) (snowman.Block, error) {
	return vm.buildBlockWithContext(ctx, nil)
}

func (vm *VM) buildBlockWithContext(ctx context.Context, proposerVMBlockCtx *block.Context) (snowman.Block, error) {
	if proposerVMBlockCtx != nil {
		log.Debug("Building block with context", "pChainBlockHeight", proposerVMBlockCtx.PChainHeight)
	} else {
		log.Debug("Building block without context")
	}
	predicateCtx := &precompileconfig.ProposerPredicateContext{
		PrecompilePredicateContext: precompileconfig.PrecompilePredicateContext{
			SnowCtx: vm.ctx,
		},
		ProposerVMBlockCtx: proposerVMBlockCtx,
	}

	block, err := vm.miner.GenerateBlock(predicateCtx)
	vm.builder.handleGenerateBlock()
	if err != nil {
		return nil, err
	}

	// Note: the status of block is set by ChainState
	blk := vm.newBlock(block)

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

	log.Debug(fmt.Sprintf("Built block %s", blk.ID()))
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
	block := vm.newBlock(ethBlock)
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
	return vm.newBlock(ethBlock), nil
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

// GetBlockAtHeight implements the HeightIndexedChainVM interface and returns the
// canonical block at [blkHeight].
// If [blkHeight] is less than the height of the last accepted block, this will return
// the block accepted at that height. Otherwise, it may return a blkID that has not yet
// been accepted.
// Note: the engine assumes that if a block is not found at [blkHeight], then
// [database.ErrNotFound] will be returned. This indicates that the VM has state synced
// and does not have all historical blocks available.
func (vm *VM) GetBlockIDAtHeight(_ context.Context, blkHeight uint64) (ids.ID, error) {
	ethBlock := vm.blockChain.GetBlockByNumber(blkHeight)
	if ethBlock == nil {
		return ids.ID{}, database.ErrNotFound
	}

	return ids.ID(ethBlock.Hash()), nil
}

func (vm *VM) Version(context.Context) (string, error) {
	return Version, nil
}

// NewHandler returns a new Handler for a service where:
//   - The handler's functionality is defined by [service]
//     [service] should be a gorilla RPC service (see https://www.gorillatoolkit.org/pkg/rpc/v2)
//   - The name of the service is [name]
//   - The LockOption is the first element of [lockOption]
//     By default the LockOption is WriteLock
//     [lockOption] should have either 0 or 1 elements. Elements beside the first are ignored.
func newHandler(name string, service interface{}, lockOption ...commonEng.LockOption) (*commonEng.HTTPHandler, error) {
	server := avalancheRPC.NewServer()
	server.RegisterCodec(avalancheJSON.NewCodec(), "application/json")
	server.RegisterCodec(avalancheJSON.NewCodec(), "application/json;charset=UTF-8")
	if err := server.RegisterService(service, name); err != nil {
		return nil, err
	}

	var lock commonEng.LockOption = commonEng.WriteLock
	if len(lockOption) != 0 {
		lock = lockOption[0]
	}
	return &commonEng.HTTPHandler{LockOptions: lock, Handler: server}, nil
}

// CreateHandlers makes new http handlers that can handle API calls
func (vm *VM) CreateHandlers(context.Context) (map[string]*commonEng.HTTPHandler, error) {
	handler := rpc.NewServer(vm.config.APIMaxDuration.Duration)
	enabledAPIs := vm.config.EthAPIs()
	if err := attachEthService(handler, vm.eth.APIs(), enabledAPIs); err != nil {
		return nil, err
	}

	primaryAlias, err := vm.ctx.BCLookup.PrimaryAlias(vm.ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get primary alias for chain due to %w", err)
	}
	apis := make(map[string]*commonEng.HTTPHandler)
	if vm.config.AdminAPIEnabled {
		adminAPI, err := newHandler("admin", NewAdminService(vm, os.ExpandEnv(fmt.Sprintf("%s_subnet_evm_performance_%s", vm.config.AdminAPIDir, primaryAlias))))
		if err != nil {
			return nil, fmt.Errorf("failed to register service for admin API due to %w", err)
		}
		apis[adminEndpoint] = adminAPI
		enabledAPIs = append(enabledAPIs, "subnet-evm-admin")
	}

	if vm.config.SnowmanAPIEnabled {
		if err := handler.RegisterName("snowman", &SnowmanAPI{vm}); err != nil {
			return nil, err
		}
		enabledAPIs = append(enabledAPIs, "snowman")
	}

	if vm.config.WarpAPIEnabled {
		if err := handler.RegisterName("warp", &warp.WarpAPI{Backend: vm.warpBackend}); err != nil {
			return nil, err
		}
		enabledAPIs = append(enabledAPIs, "warp")
	}

	log.Info(fmt.Sprintf("Enabled APIs: %s", strings.Join(enabledAPIs, ", ")))
	apis[ethRPCEndpoint] = &commonEng.HTTPHandler{
		LockOptions: commonEng.NoLock,
		Handler:     handler,
	}
	apis[ethWSEndpoint] = &commonEng.HTTPHandler{
		LockOptions: commonEng.NoLock,
		Handler: handler.WebsocketHandlerWithDuration(
			[]string{"*"},
			vm.config.APIMaxDuration.Duration,
			vm.config.WSCPURefillRate.Duration,
			vm.config.WSCPUMaxStored.Duration,
		),
	}

	return apis, nil
}

// CreateStaticHandlers makes new http handlers that can handle API calls
func (vm *VM) CreateStaticHandlers(context.Context) (map[string]*commonEng.HTTPHandler, error) {
	server := avalancheRPC.NewServer()
	codec := cjson.NewCodec()
	server.RegisterCodec(codec, "application/json")
	server.RegisterCodec(codec, "application/json;charset=UTF-8")
	serviceName := "subnetevm"
	if err := server.RegisterService(&StaticService{}, serviceName); err != nil {
		return nil, err
	}

	return map[string]*commonEng.HTTPHandler{
		"/rpc": {LockOptions: commonEng.NoLock, Handler: server},
	}, nil
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
// Note: assumes chaindb, ethConfig, and genesisHash have been initialized.
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
