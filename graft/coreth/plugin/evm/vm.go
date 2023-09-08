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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	avalanchegoMetrics "github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/network/p2p"

	"github.com/ava-labs/coreth/consensus/dummy"
	corethConstants "github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/eth/ethconfig"
	"github.com/ava-labs/coreth/ethdb"
	corethPrometheus "github.com/ava-labs/coreth/metrics/prometheus"
	"github.com/ava-labs/coreth/miner"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/rpc"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/client/stats"
	"github.com/ava-labs/coreth/sync/handlers"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/utils"

	"github.com/prometheus/client_golang/prometheus"
	// Force-load tracer engine to trigger registration
	//
	// We must import this package (not referenced elsewhere) so that the native "callTracer"
	// is added to a map of client-accessible tracers. In geth, this is done
	// inside of cmd/geth.
	_ "github.com/ava-labs/coreth/eth/tracers/js"
	_ "github.com/ava-labs/coreth/eth/tracers/native"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/coreth/metrics"

	avalancheRPC "github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"

	avalancheJSON "github.com/ava-labs/avalanchego/utils/json"
)

const (
	x2cRateInt64       int64 = 1_000_000_000
	x2cRateMinus1Int64 int64 = x2cRateInt64 - 1

	// Prefixes for metrics gatherers
	ethMetricsPrefix        = "eth"
	chainStateMetricsPrefix = "chain_state"
)

var (
	// x2cRate is the conversion rate between the smallest denomination on the X-Chain
	// 1 nAVAX and the smallest denomination on the C-Chain 1 wei. Where 1 nAVAX = 1 gWei.
	// This is only required for AVAX because the denomination of 1 AVAX is 9 decimal
	// places on the X and P chains, but is 18 decimal places within the EVM.
	x2cRate       = big.NewInt(x2cRateInt64)
	x2cRateMinus1 = big.NewInt(x2cRateMinus1Int64)

	_ block.ChainVM                  = &VM{}
	_ block.StateSyncableVM          = &VM{}
	_ statesyncclient.EthBlockParser = &VM{}
)

const (
	// Max time from current time allowed for blocks, before they're considered future blocks
	// and fail verification
	maxFutureBlockTime   = 10 * time.Second
	maxUTXOsToFetch      = 1024
	defaultMempoolSize   = 4096
	codecVersion         = uint16(0)
	secpFactoryCacheSize = 1024

	decidedCacheSize    = 10 * units.MiB
	missingCacheSize    = 50
	unverifiedCacheSize = 5 * units.MiB
	bytesToIDCacheSize  = 5 * units.MiB

	targetAtomicTxsSize = 40 * units.KiB
)

// Define the API endpoints for the VM
const (
	avaxEndpoint   = "/avax"
	adminEndpoint  = "/admin"
	ethRPCEndpoint = "/rpc"
	ethWSEndpoint  = "/ws"
)

var (
	// Set last accepted key to be longer than the keys used to store accepted block IDs.
	lastAcceptedKey = []byte("last_accepted_key")
	acceptedPrefix  = []byte("snowman_accepted")
	metadataPrefix  = []byte("metadata")
	ethDBPrefix     = []byte("ethdb")

	// Prefixes for atomic trie
	atomicTrieDBPrefix     = []byte("atomicTrieDB")
	atomicTrieMetaDBPrefix = []byte("atomicTrieMetaDB")
)

var (
	errEmptyBlock                     = errors.New("empty block")
	errUnsupportedFXs                 = errors.New("unsupported feature extensions")
	errInvalidBlock                   = errors.New("invalid block")
	errInvalidAddr                    = errors.New("invalid hex address")
	errInsufficientAtomicTxFee        = errors.New("atomic tx fee too low for atomic mempool")
	errAssetIDMismatch                = errors.New("asset IDs in the input don't match the utxo")
	errNoImportInputs                 = errors.New("tx has no imported inputs")
	errInputsNotSortedUnique          = errors.New("inputs not sorted and unique")
	errPublicKeySignatureMismatch     = errors.New("signature doesn't match public key")
	errWrongChainID                   = errors.New("tx has wrong chain ID")
	errInsufficientFunds              = errors.New("insufficient funds")
	errNoExportOutputs                = errors.New("tx has no export outputs")
	errOutputsNotSorted               = errors.New("tx outputs not sorted")
	errOutputsNotSortedUnique         = errors.New("outputs not sorted and unique")
	errOverflowExport                 = errors.New("overflow when computing export amount + txFee")
	errInvalidNonce                   = errors.New("invalid nonce")
	errConflictingAtomicInputs        = errors.New("invalid block due to conflicting atomic inputs")
	errUnclesUnsupported              = errors.New("uncles unsupported")
	errRejectedParent                 = errors.New("rejected parent")
	errInsufficientFundsForFee        = errors.New("insufficient AVAX funds to pay transaction fee")
	errNoEVMOutputs                   = errors.New("tx has no EVM outputs")
	errNilBaseFeeApricotPhase3        = errors.New("nil base fee is invalid after apricotPhase3")
	errNilExtDataGasUsedApricotPhase4 = errors.New("nil extDataGasUsed is invalid after apricotPhase4")
	errNilBlockGasCostApricotPhase4   = errors.New("nil blockGasCost is invalid after apricotPhase4")
	errConflictingAtomicTx            = errors.New("conflicting atomic tx present")
	errTooManyAtomicTx                = errors.New("too many atomic tx")
	errMissingAtomicTxs               = errors.New("cannot build a block with non-empty extra data and zero atomic transactions")
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
	// *chain.State helps to implement the VM interface by wrapping blocks
	// with an efficient caching layer.
	*chain.State

	config Config

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

	// [db] is the VM's current database managed by ChainState
	db *versiondb.Database

	// metadataDB is used to store one off keys.
	metadataDB database.Database

	// [chaindb] is the database supplied to the Ethereum backend
	chaindb ethdb.Database

	// [acceptedBlockDB] is the database to store the last accepted
	// block.
	acceptedBlockDB database.Database

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

	gossiper Gossiper

	baseCodec codec.Registry
	codec     codec.Manager
	clock     mockable.Clock
	mempool   *Mempool

	shutdownChan chan struct{}
	shutdownWg   sync.WaitGroup

	fx          secp256k1fx.Fx
	secpFactory secp256k1.Factory

	// Continuous Profiler
	profiler profiler.ContinuousProfiler

	peer.Network
	client       peer.NetworkClient
	networkCodec codec.Manager

	router *p2p.Router

	// Metrics
	multiGatherer avalanchegoMetrics.MultiGatherer

	bootstrapped bool
	IsPlugin     bool

	logger CorethLogger
	// State sync server and client
	StateSyncServer
	StateSyncClient
}

// Codec implements the secp256k1fx interface
func (vm *VM) Codec() codec.Manager { return vm.codec }

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
	// We should deprecate config flags as the first thing, before we do anything else
	// because this can set old flags to new flags. log the message after we have
	// initialized the logger.
	deprecateMsg := vm.config.Deprecate()

	vm.ctx = chainCtx

	// Create logger
	alias, err := vm.ctx.BCLookup.PrimaryAlias(vm.ctx.ChainID)
	if err != nil {
		alias = vm.ctx.ChainID.String()
	}

	var writer io.Writer = vm.ctx.Log
	if vm.IsPlugin {
		writer = originalStderr
	}

	corethLogger, err := InitLogger(alias, vm.config.LogLevel, vm.config.LogJSONFormat, writer)
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
	baseDB := dbManager.Current().Database
	// Use NewNested rather than New so that the structure of the database
	// remains the same regardless of the provided baseDB type.
	vm.chaindb = Database{prefixdb.NewNested(ethDBPrefix, baseDB)}
	vm.db = versiondb.New(baseDB)
	vm.acceptedBlockDB = prefixdb.New(acceptedPrefix, vm.db)
	vm.metadataDB = prefixdb.New(metadataPrefix, vm.db)

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

	var extDataHashes map[common.Hash]common.Hash
	// Set the chain config for mainnet/fuji chain IDs
	switch {
	case g.Config.ChainID.Cmp(params.AvalancheMainnetChainID) == 0:
		g.Config = params.AvalancheMainnetChainConfig
		extDataHashes = mainnetExtDataHashes
	case g.Config.ChainID.Cmp(params.AvalancheFujiChainID) == 0:
		g.Config = params.AvalancheFujiChainConfig
		extDataHashes = fujiExtDataHashes
	case g.Config.ChainID.Cmp(params.AvalancheLocalChainID) == 0:
		g.Config = params.AvalancheLocalChainConfig
	}
	// Set the Avalanche Context on the ChainConfig
	g.Config.AvalancheContext = params.AvalancheContext{
		BlockchainID: common.Hash(chainCtx.ChainID),
	}
	vm.syntacticBlockValidator = NewBlockValidator(extDataHashes)

	// Ensure that non-standard commit interval is only allowed for the local network
	if g.Config.ChainID.Cmp(params.AvalancheLocalChainID) != 0 {
		if vm.config.CommitInterval != defaultCommitInterval {
			return fmt.Errorf("cannot start non-local network with commit interval %d", vm.config.CommitInterval)
		}
		if vm.config.StateSyncCommitInterval != defaultSyncableCommitInterval {
			return fmt.Errorf("cannot start non-local network with syncable interval %d", vm.config.StateSyncCommitInterval)
		}
	}

	// Free the memory of the extDataHash map that is not used (i.e. if mainnet
	// config, free fuji)
	fujiExtDataHashes = nil
	mainnetExtDataHashes = nil

	vm.chainID = g.Config.ChainID

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
	vm.ethConfig.TrieCleanCache = vm.config.TrieCleanCache
	vm.ethConfig.TrieCleanJournal = vm.config.TrieCleanJournal
	vm.ethConfig.TrieCleanRejournal = vm.config.TrieCleanRejournal.Duration
	vm.ethConfig.TrieDirtyCache = vm.config.TrieDirtyCache
	vm.ethConfig.TrieDirtyCommitTarget = vm.config.TrieDirtyCommitTarget
	vm.ethConfig.SnapshotCache = vm.config.SnapshotCache
	vm.ethConfig.Pruning = vm.config.Pruning
	vm.ethConfig.AcceptorQueueLimit = vm.config.AcceptorQueueLimit
	vm.ethConfig.PopulateMissingTries = vm.config.PopulateMissingTries
	vm.ethConfig.PopulateMissingTriesParallelism = vm.config.PopulateMissingTriesParallelism
	vm.ethConfig.AllowMissingTries = vm.config.AllowMissingTries
	vm.ethConfig.SnapshotDelayInit = vm.stateSyncEnabled(lastAcceptedHeight)
	vm.ethConfig.SnapshotWait = vm.config.SnapshotWait
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

	vm.chainConfig = g.Config
	vm.networkID = vm.ethConfig.NetworkId
	vm.secpFactory = secp256k1.Factory{
		Cache: cache.LRU[ids.ID, *secp256k1.PublicKey]{
			Size: secpFactoryCacheSize,
		},
	}

	vm.codec = Codec

	// TODO: read size from settings
	vm.mempool = NewMempool(chainCtx.AVAXAssetID, defaultMempoolSize)

	if err := vm.initializeMetrics(); err != nil {
		return err
	}

	// initialize peer network
	vm.router = p2p.NewRouter(vm.ctx.Log, appSender)
	vm.networkCodec = message.Codec
	vm.Network = peer.NewNetwork(vm.router, appSender, vm.networkCodec, message.CrossChainCodec, chainCtx.NodeID, vm.config.MaxOutboundActiveRequests, vm.config.MaxOutboundActiveCrossChainRequests)
	vm.client = peer.NewNetworkClient(vm.Network)

	if err := vm.initializeChain(lastAcceptedHash); err != nil {
		return err
	}
	// initialize bonus blocks on mainnet
	var (
		bonusBlockHeights     map[uint64]ids.ID
		canonicalBlockHeights []uint64
	)
	if vm.chainID.Cmp(params.AvalancheMainnetChainID) == 0 {
		bonusBlockHeights = bonusBlockMainnetHeights
		canonicalBlockHeights = canonicalBlockMainnetHeights
	}

	// initialize atomic repository
	vm.atomicTxRepository, err = NewAtomicTxRepository(
		vm.db, vm.codec, lastAcceptedHeight,
		bonusBlockHeights, canonicalBlockHeights,
		vm.getAtomicTxFromPreApricot5BlockByHeight,
	)
	if err != nil {
		return fmt.Errorf("failed to create atomic repository: %w", err)
	}
	vm.atomicBackend, err = NewAtomicBackend(
		vm.db, vm.ctx.SharedMemory, bonusBlockHeights, vm.atomicTxRepository, lastAcceptedHeight, lastAcceptedHash, vm.config.CommitInterval,
	)
	if err != nil {
		return fmt.Errorf("failed to create atomic backend: %w", err)
	}
	vm.atomicTrie = vm.atomicBackend.AtomicTrie()

	go vm.ctx.Log.RecoverAndPanic(vm.startContinuousProfiler)

	// The Codec explicitly registers the types it requires from the secp256k1fx
	// so [vm.baseCodec] is a dummy codec use to fulfill the secp256k1fx VM
	// interface. The fx will register all of its types, which can be safely
	// ignored by the VM's codec.
	vm.baseCodec = linearcodec.NewDefault()

	if err := vm.fx.Initialize(vm); err != nil {
		return err
	}

	vm.initializeStateSyncServer()
	return vm.initializeStateSyncClient(lastAcceptedHeight)
}

func (vm *VM) initializeMetrics() error {
	vm.multiGatherer = avalanchegoMetrics.NewMultiGatherer()
	// If metrics are enabled, register the default metrics regitry
	if metrics.Enabled {
		gatherer := corethPrometheus.Gatherer(metrics.DefaultRegistry)
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
	vm.eth, err = eth.New(
		node,
		&vm.ethConfig,
		vm.createConsensusCallbacks(),
		vm.chaindb,
		vm.config.EthBackendSettings(),
		lastAcceptedHash,
		&vm.clock,
	)
	if err != nil {
		return err
	}
	vm.eth.SetEtherbase(corethConstants.BlackholeAddr)
	vm.txPool = vm.eth.TxPool()
	vm.blockChain = vm.eth.BlockChain()
	vm.miner = vm.eth.Miner()

	// start goroutines to update the tx pool gas minimum gas price when upgrades go into effect
	vm.handleGasPriceUpdates()

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
		db:                   vm.db,
		atomicBackend:        vm.atomicBackend,
		toEngine:             vm.toEngine,
	})

	// If StateSync is disabled, clear any ongoing summary so that we will not attempt to resume
	// sync using a snapshot that has been modified by the node running normal operations.
	if !stateSyncEnabled {
		return vm.StateSyncClient.StateSyncClearOngoingSummary()
	}

	return nil
}

// initializeStateSyncServer should be called after [vm.chain] is initialized.
func (vm *VM) initializeStateSyncServer() {
	vm.StateSyncServer = NewStateSyncServer(&stateSyncServerConfig{
		Chain:            vm.blockChain,
		AtomicTrie:       vm.atomicTrie,
		SyncableInterval: vm.config.StateSyncCommitInterval,
	})

	vm.setAppRequestHandlers()
	vm.setCrossChainAppRequestHandler()
}

func (vm *VM) initChainState(lastAcceptedBlock *types.Block) error {
	block, err := vm.newBlock(lastAcceptedBlock)
	if err != nil {
		return fmt.Errorf("failed to create block wrapper for the last accepted block: %w", err)
	}
	block.status = choices.Accepted

	config := &chain.Config{
		DecidedCacheSize:    decidedCacheSize,
		MissingCacheSize:    missingCacheSize,
		UnverifiedCacheSize: unverifiedCacheSize,
		BytesToIDCacheSize:  bytesToIDCacheSize,
		GetBlockIDAtHeight:  vm.GetBlockIDAtHeight,
		GetBlock:            vm.getBlock,
		UnmarshalBlock:      vm.parseBlock,
		BuildBlock:          vm.buildBlock,
		LastAcceptedBlock:   block,
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

func (vm *VM) createConsensusCallbacks() *dummy.ConsensusCallbacks {
	return &dummy.ConsensusCallbacks{
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
		rules := vm.chainConfig.AvalancheRules(header.Number, header.Time)
		if err := vm.verifyTx(tx, header.ParentHash, header.BaseFee, state, rules); err != nil {
			// Discard the transaction from the mempool on failed verification.
			vm.mempool.DiscardCurrentTx(tx.ID())
			state.RevertToSnapshot(snapshot)
			continue
		}

		atomicTxBytes, err := vm.codec.Marshal(codecVersion, tx)
		if err != nil {
			// Discard the transaction from the mempool and error if the transaction
			// cannot be marshalled. This should never happen.
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
		batchAtomicTxs    []*Tx
		batchAtomicUTXOs  set.Set[ids.ID]
		batchContribution *big.Int = new(big.Int).Set(common.Big0)
		batchGasUsed      *big.Int = new(big.Int).Set(common.Big0)
		rules                      = vm.chainConfig.AvalancheRules(header.Number, header.Time)
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
		if totalGasUsed := new(big.Int).Add(batchGasUsed, txGasUsed); totalGasUsed.Cmp(params.AtomicGasLimit) > 0 {
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
			vm.mempool.DiscardCurrentTx(tx.ID())
			continue
		}

		snapshot := state.Snapshot()
		if err := vm.verifyTx(tx, header.ParentHash, header.BaseFee, state, rules); err != nil {
			// Discard the transaction from the mempool and reset the state to [snapshot]
			// if it fails verification here.
			// Note: prior to this point, we have not modified [state] so there is no need to
			// revert to a snapshot if we discard the transaction prior to this point.
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
		atomicTxBytes, err := vm.codec.Marshal(codecVersion, batchAtomicTxs)
		if err != nil {
			// If we fail to marshal the batch of atomic transactions for any reason,
			// discard the entire set of current transactions.
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
		rules                      = vm.chainConfig.AvalancheRules(header.Number, header.Time)
	)

	txs, err := ExtractAtomicTxs(block.ExtData(), rules.IsApricotPhase5, vm.codec)
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
			if batchGasUsed.Cmp(params.AtomicGasLimit) == 1 {
				return nil, nil, fmt.Errorf("atomic gas used (%d) by block (%s), exceeds atomic gas limit (%d)", batchGasUsed, block.Hash().Hex(), params.AtomicGasLimit)
			}
		}
	}
	return batchContribution, batchGasUsed, nil
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
		return vm.fx.Bootstrapping()
	case snow.NormalOp:
		// Initialize goroutines related to block building once we enter normal operation as there is no need to handle mempool gossip before this point.
		vm.initBlockBuilding()
		vm.bootstrapped = true
		return vm.fx.Bootstrapped()
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
	syncRequestHandler := handlers.NewSyncHandler(
		vm.blockChain,
		vm.chaindb,
		evmTrieDB,
		vm.atomicTrie.TrieDB(),
		vm.networkCodec,
		handlerstats.NewHandlerStats(metrics.Enabled),
	)
	vm.Network.SetRequestHandler(syncRequestHandler)
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
	vm.shutdownWg.Wait()
	return nil
}

// buildBlock builds a block to be wrapped by ChainState
func (vm *VM) buildBlock(_ context.Context) (snowman.Block, error) {
	block, err := vm.miner.GenerateBlock()
	vm.builder.handleGenerateBlock()
	if err != nil {
		vm.mempool.CancelCurrentTxs()
		return nil, err
	}

	// Note: the status of block is set by ChainState
	blk, err := vm.newBlock(block)
	if err != nil {
		vm.mempool.DiscardCurrentTxs()
		return nil, err
	}
	if err != nil {
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
	if err := blk.verify(false /*=writes*/); err != nil {
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
	avaxAPI, err := newHandler("avax", &AvaxAPI{vm})
	if err != nil {
		return nil, fmt.Errorf("failed to register service for AVAX API due to %w", err)
	}
	enabledAPIs = append(enabledAPIs, "avax")
	apis[avaxEndpoint] = avaxAPI

	if vm.config.AdminAPIEnabled {
		adminAPI, err := newHandler("admin", NewAdminService(vm, os.ExpandEnv(fmt.Sprintf("%s_coreth_performance_%s", vm.config.AdminAPIDir, primaryAlias))))
		if err != nil {
			return nil, fmt.Errorf("failed to register service for admin API due to %w", err)
		}
		apis[adminEndpoint] = adminAPI
		enabledAPIs = append(enabledAPIs, "coreth-admin")
	}

	if vm.config.SnowmanAPIEnabled {
		if err := handler.RegisterName("snowman", &SnowmanAPI{vm}); err != nil {
			return nil, err
		}
		enabledAPIs = append(enabledAPIs, "snowman")
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
	handler := rpc.NewServer(0)
	if err := handler.RegisterName("static", &StaticService{}); err != nil {
		return nil, err
	}

	return map[string]*commonEng.HTTPHandler{
		"/rpc": {LockOptions: commonEng.NoLock, Handler: handler},
	}, nil
}

/*
 ******************************************************************************
 *********************************** Helpers **********************************
 ******************************************************************************
 */

// conflicts returns an error if [inputs] conflicts with any of the atomic inputs contained in [ancestor]
// or any of its ancestor blocks going back to the last accepted block in its ancestry. If [ancestor] is
// accepted, then nil will be returned immediately.
// If the ancestry of [ancestor] cannot be fetched, then [errRejectedParent] may be returned.
func (vm *VM) conflicts(inputs set.Set[ids.ID], ancestor *Block) error {
	for ancestor.Status() != choices.Accepted {
		// If any of the atomic transactions in the ancestor conflict with [inputs]
		// return an error.
		for _, atomicTx := range ancestor.atomicTxs {
			if inputs.Overlaps(atomicTx.InputUTXOs()) {
				return errConflictingAtomicInputs
			}
		}

		// Move up the chain.
		nextAncestorID := ancestor.Parent()
		// If the ancestor is unknown, then the parent failed
		// verification when it was called.
		// If the ancestor is rejected, then this block shouldn't be
		// inserted into the canonical chain because the parent is
		// will be missing.
		// If the ancestor is processing, then the block may have
		// been verified.
		nextAncestorIntf, err := vm.GetBlockInternal(context.TODO(), nextAncestorID)
		if err != nil {
			return errRejectedParent
		}

		if blkStatus := nextAncestorIntf.Status(); blkStatus == choices.Unknown || blkStatus == choices.Rejected {
			return errRejectedParent
		}
		nextAncestor, ok := nextAncestorIntf.(*Block)
		if !ok {
			return fmt.Errorf("ancestor block %s had unexpected type %T", nextAncestor.ID(), nextAncestorIntf)
		}
		ancestor = nextAncestor
	}

	return nil
}

// getAtomicTx returns the requested transaction, status, and height.
// If the status is Unknown, then the returned transaction will be nil.
func (vm *VM) getAtomicTx(txID ids.ID) (*Tx, Status, uint64, error) {
	if tx, height, err := vm.atomicTxRepository.GetByTxID(txID); err == nil {
		return tx, Accepted, height, nil
	} else if err != database.ErrNotFound {
		return nil, Unknown, 0, err
	}
	tx, dropped, found := vm.mempool.GetTx(txID)
	switch {
	case found && dropped:
		return tx, Dropped, 0, nil
	case found:
		return tx, Processing, 0, nil
	default:
		return nil, Unknown, 0, nil
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

	expectedHRP := constants.GetHRP(vm.ctx.NetworkID)
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

// issueTx verifies [tx] as valid to be issued on top of the currently preferred block
// and then issues [tx] into the mempool if valid.
func (vm *VM) issueTx(tx *Tx, local bool) error {
	if err := vm.verifyTxAtTip(tx); err != nil {
		if !local {
			// unlike local txs, invalid remote txs are recorded as discarded
			// so that they won't be requested again
			txID := tx.ID()
			vm.mempool.discardedTxs.Put(txID, tx)
			log.Debug("failed to verify remote tx being issued to the mempool",
				"txID", txID,
				"err", err,
			)
			return nil
		}
		return err
	}
	// add to mempool and possibly re-gossip
	if err := vm.mempool.AddTx(tx); err != nil {
		if !local {
			// unlike local txs, invalid remote txs are recorded as discarded
			// so that they won't be requested again
			txID := tx.ID()
			vm.mempool.discardedTxs.Put(tx.ID(), tx)
			log.Debug("failed to issue remote tx to mempool",
				"txID", txID,
				"err", err,
			)
			return nil
		}
		return err
	}
	// NOTE: Gossiping of the issued [Tx] is handled in [AddTx]
	return nil
}

// verifyTxAtTip verifies that [tx] is valid to be issued on top of the currently preferred block
func (vm *VM) verifyTxAtTip(tx *Tx) error {
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
		_, nextBaseFee, err = dummy.EstimateNextBaseFee(vm.chainConfig, parentHeader, timestamp)
		if err != nil {
			// Return extremely detailed error since CalcBaseFee should never encounter an issue here
			return fmt.Errorf("failed to calculate base fee with parent timestamp (%d), parent ExtraData: (0x%x), and current timestamp (%d): %w", parentHeader.Time, parentHeader.Extra, timestamp, err)
		}
	}

	return vm.verifyTx(tx, parentHeader.Hash(), nextBaseFee, preferredState, rules)
}

// verifyTx verifies that [tx] is valid to be issued into a block with parent block [parentHash]
// and validated at [state] using [rules] as the current rule set.
// Note: verifyTx may modify [state]. If [state] needs to be properly maintained, the caller is responsible
// for reverting to the correct snapshot after calling this function. If this function is called with a
// throwaway state, then this is not necessary.
func (vm *VM) verifyTx(tx *Tx, parentHash common.Hash, baseFee *big.Int, state *state.StateDB, rules params.Rules) error {
	parentIntf, err := vm.GetBlockInternal(context.TODO(), ids.ID(parentHash))
	if err != nil {
		return fmt.Errorf("failed to get parent block: %w", err)
	}
	parent, ok := parentIntf.(*Block)
	if !ok {
		return fmt.Errorf("parent block %s had unexpected type %T", parentIntf.ID(), parentIntf)
	}
	if err := tx.UnsignedAtomicTx.SemanticVerify(vm, tx, parent, baseFee, rules); err != nil {
		return err
	}
	return tx.UnsignedAtomicTx.EVMStateTransfer(vm.ctx, state)
}

// verifyTxs verifies that [txs] are valid to be issued into a block with parent block [parentHash]
// using [rules] as the current rule set.
func (vm *VM) verifyTxs(txs []*Tx, parentHash common.Hash, baseFee *big.Int, height uint64, rules params.Rules) error {
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
	if blkStatus := ancestorInf.Status(); blkStatus == choices.Unknown || blkStatus == choices.Rejected {
		return errRejectedParent
	}
	ancestor, ok := ancestorInf.(*Block)
	if !ok {
		return fmt.Errorf("expected parent block %s, to be *Block but is %T", ancestor.ID(), ancestorInf)
	}

	// Ensure each tx in [txs] doesn't conflict with any other atomic tx in
	// a processing ancestor block.
	inputs := set.Set[ids.ID]{}
	for _, atomicTx := range txs {
		utx := atomicTx.UnsignedAtomicTx
		if err := utx.SemanticVerify(vm, atomicTx, ancestor, baseFee, rules); err != nil {
			return fmt.Errorf("invalid block due to failed semanatic verify: %w at height %d", err, height)
		}
		txInputs := utx.InputUTXOs()
		if inputs.Overlaps(txInputs) {
			return errConflictingAtomicInputs
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

	addrsList := make([][]byte, addrs.Len())
	for i, addr := range addrs.List() {
		addrsList[i] = addr.Bytes()
	}

	allUTXOBytes, lastAddr, lastUTXO, err := vm.ctx.SharedMemory.Indexed(
		chainID,
		addrsList,
		startAddr.Bytes(),
		startUTXOID[:],
		limit,
	)
	if err != nil {
		return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("error fetching atomic UTXOs: %w", err)
	}

	lastAddrID, err := ids.ToShortID(lastAddr)
	if err != nil {
		lastAddrID = ids.ShortEmpty
	}
	lastUTXOID, err := ids.ToID(lastUTXO)
	if err != nil {
		lastUTXOID = ids.Empty
	}

	utxos := make([]*avax.UTXO, len(allUTXOBytes))
	for i, utxoBytes := range allUTXOBytes {
		utxo := &avax.UTXO{}
		if _, err := vm.codec.Unmarshal(utxoBytes, utxo); err != nil {
			return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("error parsing UTXO: %w", err)
		}
		utxos[i] = utxo
	}
	return utxos, lastAddrID, lastUTXOID, nil
}

// GetSpendableFunds returns a list of EVMInputs and keys (in corresponding
// order) to total [amount] of [assetID] owned by [keys].
// Note: we return [][]*secp256k1.PrivateKey even though each input
// corresponds to a single key, so that the signers can be passed in to
// [tx.Sign] which supports multiple keys on a single input.
func (vm *VM) GetSpendableFunds(
	keys []*secp256k1.PrivateKey,
	assetID ids.ID,
	amount uint64,
) ([]EVMInput, [][]*secp256k1.PrivateKey, error) {
	// Note: current state uses the state of the preferred block.
	state, err := vm.blockChain.State()
	if err != nil {
		return nil, nil, err
	}
	inputs := []EVMInput{}
	signers := [][]*secp256k1.PrivateKey{}
	// Note: we assume that each key in [keys] is unique, so that iterating over
	// the keys will not produce duplicated nonces in the returned EVMInput slice.
	for _, key := range keys {
		if amount == 0 {
			break
		}
		addr := GetEthAddress(key)
		var balance uint64
		if assetID == vm.ctx.AVAXAssetID {
			// If the asset is AVAX, we divide by the x2cRate to convert back to the correct
			// denomination of AVAX that can be exported.
			balance = new(big.Int).Div(state.GetBalance(addr), x2cRate).Uint64()
		} else {
			balance = state.GetBalanceMultiCoin(addr, common.Hash(assetID)).Uint64()
		}
		if balance == 0 {
			continue
		}
		if amount < balance {
			balance = amount
		}
		nonce, err := vm.GetCurrentNonce(addr)
		if err != nil {
			return nil, nil, err
		}
		inputs = append(inputs, EVMInput{
			Address: addr,
			Amount:  balance,
			AssetID: assetID,
			Nonce:   nonce,
		})
		signers = append(signers, []*secp256k1.PrivateKey{key})
		amount -= balance
	}

	if amount > 0 {
		return nil, nil, errInsufficientFunds
	}

	return inputs, signers, nil
}

// GetSpendableAVAXWithFee returns a list of EVMInputs and keys (in corresponding
// order) to total [amount] + [fee] of [AVAX] owned by [keys].
// This function accounts for the added cost of the additional inputs needed to
// create the transaction and makes sure to skip any keys with a balance that is
// insufficient to cover the additional fee.
// Note: we return [][]*secp256k1.PrivateKey even though each input
// corresponds to a single key, so that the signers can be passed in to
// [tx.Sign] which supports multiple keys on a single input.
func (vm *VM) GetSpendableAVAXWithFee(
	keys []*secp256k1.PrivateKey,
	amount uint64,
	cost uint64,
	baseFee *big.Int,
) ([]EVMInput, [][]*secp256k1.PrivateKey, error) {
	// Note: current state uses the state of the preferred block.
	state, err := vm.blockChain.State()
	if err != nil {
		return nil, nil, err
	}

	initialFee, err := CalculateDynamicFee(cost, baseFee)
	if err != nil {
		return nil, nil, err
	}

	newAmount, err := math.Add64(amount, initialFee)
	if err != nil {
		return nil, nil, err
	}
	amount = newAmount

	inputs := []EVMInput{}
	signers := [][]*secp256k1.PrivateKey{}
	// Note: we assume that each key in [keys] is unique, so that iterating over
	// the keys will not produce duplicated nonces in the returned EVMInput slice.
	for _, key := range keys {
		if amount == 0 {
			break
		}

		prevFee, err := CalculateDynamicFee(cost, baseFee)
		if err != nil {
			return nil, nil, err
		}

		newCost := cost + EVMInputGas
		newFee, err := CalculateDynamicFee(newCost, baseFee)
		if err != nil {
			return nil, nil, err
		}

		additionalFee := newFee - prevFee

		addr := GetEthAddress(key)
		// Since the asset is AVAX, we divide by the x2cRate to convert back to
		// the correct denomination of AVAX that can be exported.
		balance := new(big.Int).Div(state.GetBalance(addr), x2cRate).Uint64()
		// If the balance for [addr] is insufficient to cover the additional cost
		// of adding an input to the transaction, skip adding the input altogether
		if balance <= additionalFee {
			continue
		}

		// Update the cost for the next iteration
		cost = newCost

		newAmount, err := math.Add64(amount, additionalFee)
		if err != nil {
			return nil, nil, err
		}
		amount = newAmount

		// Use the entire [balance] as an input, but if the required [amount]
		// is less than the balance, update the [inputAmount] to spend the
		// minimum amount to finish the transaction.
		inputAmount := balance
		if amount < balance {
			inputAmount = amount
		}
		nonce, err := vm.GetCurrentNonce(addr)
		if err != nil {
			return nil, nil, err
		}
		inputs = append(inputs, EVMInput{
			Address: addr,
			Amount:  inputAmount,
			AssetID: vm.ctx.AVAXAssetID,
			Nonce:   nonce,
		})
		signers = append(signers, []*secp256k1.PrivateKey{key})
		amount -= inputAmount
	}

	if amount > 0 {
		return nil, nil, errInsufficientFunds
	}

	return inputs, signers, nil
}

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

// currentRules returns the chain rules for the current block.
func (vm *VM) currentRules() params.Rules {
	header := vm.eth.APIBackend.CurrentHeader()
	return vm.chainConfig.AvalancheRules(header.Number, header.Time)
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

func (vm *VM) estimateBaseFee(ctx context.Context) (*big.Int, error) {
	// Get the base fee to use
	baseFee, err := vm.eth.APIBackend.EstimateBaseFee(ctx)
	if err != nil {
		return nil, err
	}
	if baseFee == nil {
		baseFee = initialBaseFee
	} else {
		// give some breathing room
		baseFee.Mul(baseFee, big.NewInt(11))
		baseFee.Div(baseFee, big.NewInt(10))
	}

	return baseFee, nil
}

func (vm *VM) getAtomicTxFromPreApricot5BlockByHeight(height uint64) (*Tx, error) {
	blk := vm.blockChain.GetBlockByNumber(height)
	if blk == nil {
		return nil, nil
	}
	return ExtractAtomicTx(blk.ExtData(), vm.codec)
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
