// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/coreth/plugin/evm/message"

	avalanchegoMetrics "github.com/ava-labs/avalanchego/api/metrics"
	coreth "github.com/ava-labs/coreth/chain"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth/ethconfig"
	corethPrometheus "github.com/ava-labs/coreth/metrics/prometheus"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/peer"
	"github.com/ava-labs/coreth/rpc"

	"github.com/prometheus/client_golang/prometheus"
	// Force-load tracer engine to trigger registration
	//
	// We must import this package (not referenced elsewhere) so that the native "callTracer"
	// is added to a map of client-accessible tracers. In geth, this is done
	// inside of cmd/geth.
	_ "github.com/ava-labs/coreth/eth/tracers/native"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/rlp"

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
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/profiler"
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

	_ block.ChainVM              = &VM{}
	_ block.HeightIndexedChainVM = &VM{}
)

const (
	// Max time from current time allowed for blocks, before they're considered future blocks
	// and fail verification
	maxFutureBlockTime   = 10 * time.Second
	maxUTXOsToFetch      = 1024
	defaultMempoolSize   = 4096
	codecVersion         = uint16(0)
	secpFactoryCacheSize = 1024

	decidedCacheSize    = 100
	missingCacheSize    = 50
	unverifiedCacheSize = 50

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
	ethDBPrefix     = []byte("ethdb")

	// Prefixes for atomic trie
	atomicTrieDBPrefix     = []byte("atomicTrieDB")
	atomicTrieMetaDBPrefix = []byte("atomicTrieMetaDB")

	// Prefixes for pruning
	pruneRejectedBlocksKey = []byte("pruned_rejected_blocks")
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
	errTxHashMismatch                 = errors.New("txs hash does not match header")
	errUncleHashMismatch              = errors.New("uncle hash mismatch")
	errRejectedParent                 = errors.New("rejected parent")
	errInvalidDifficulty              = errors.New("invalid difficulty")
	errInvalidBlockVersion            = errors.New("invalid block version")
	errInvalidMixDigest               = errors.New("invalid mix digest")
	errInvalidExtDataHash             = errors.New("invalid extra data hash")
	errHeaderExtraDataTooBig          = errors.New("header extra data too big")
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

func init() {
	// Preserve [os.Stderr] prior to the call in plugin/main.go to plugin.Serve(...).
	// Preserving the log level allows us to update the root handler while writing to the original
	// [os.Stderr] that is being piped through to the logger via the rpcchainvm.
	originalStderr = os.Stderr
	log.Root().SetHandler(log.LvlFilterHandler(log.LvlDebug, log.StreamHandler(originalStderr, log.TerminalFormat(false))))
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
	chain       *coreth.ETHChain
	chainConfig *params.ChainConfig
	// [db] is the VM's current database managed by ChainState
	db *versiondb.Database
	// [chaindb] is the database supplied to the Ethereum backend
	chaindb Database
	// [acceptedBlockDB] is the database to store the last accepted
	// block.
	acceptedBlockDB database.Database

	// [atomicTxRepository] maintains two indexes on accepted atomic txs.
	// - txID to accepted atomic tx
	// - block height to list of atomic txs accepted on block at that height
	atomicTxRepository AtomicTxRepository
	// [atomicTrie] maintains a merkle forest of [height]=>[atomic txs].
	//  Used to state sync clients.
	atomicTrie AtomicTrie

	builder *blockBuilder

	gossiper Gossiper

	baseCodec codec.Registry
	codec     codec.Manager
	clock     mockable.Clock
	mempool   *Mempool

	shutdownChan chan struct{}
	shutdownWg   sync.WaitGroup

	fx          secp256k1fx.Fx
	secpFactory crypto.FactorySECP256K1R

	// Continuous Profiler
	profiler profiler.ContinuousProfiler

	peer.Network
	client       peer.Client
	networkCodec codec.Manager

	// Metrics
	multiGatherer avalanchegoMetrics.MultiGatherer

	bootstrapped bool
	IsPlugin     bool
}

// Codec implements the secp256k1fx interface
func (vm *VM) Codec() codec.Manager { return vm.codec }

// CodecRegistry implements the secp256k1fx interface
func (vm *VM) CodecRegistry() codec.Registry { return vm.baseCodec }

// Clock implements the secp256k1fx interface
func (vm *VM) Clock() *mockable.Clock { return &vm.clock }

// Logger implements the secp256k1fx interface
func (vm *VM) Logger() logging.Logger { return vm.ctx.Log }

// setLogLevel initializes logger and sets the log level with the original [os.StdErr] interface
// along with the context logger.
func (vm *VM) setLogLevel(logLevel log.Lvl) {
	prefix, err := vm.ctx.BCLookup.PrimaryAlias(vm.ctx.ChainID)
	if err != nil {
		prefix = vm.ctx.ChainID.String()
	}
	prefix = fmt.Sprintf("<%s Chain>", prefix)
	format := CorethFormat(prefix, vm.IsPlugin)
	if vm.IsPlugin {
		log.Root().SetHandler(log.LvlFilterHandler(logLevel, log.StreamHandler(originalStderr, format)))
	} else {
		log.Root().SetHandler(log.LvlFilterHandler(logLevel, log.StreamHandler(vm.ctx.Log, format)))
	}
}

func CorethFormat(prefix string, doCopy bool) log.Format {
	return log.FormatFunc(func(r *log.Record) []byte {
		location := fmt.Sprintf("%+v", r.Call)
		newMsg := fmt.Sprintf("%s %s: %s", prefix, location, r.Msg)
		var b []byte
		if doCopy {
			// need to deep copy since we're using a multihandler
			// as a result it will alter R.msg twice.
			newRecord := log.Record{
				Time:     r.Time,
				Lvl:      r.Lvl,
				Msg:      newMsg,
				Ctx:      r.Ctx,
				Call:     r.Call,
				KeyNames: r.KeyNames,
			}
			b = log.TerminalFormat(false).Format(&newRecord)
			return b
		}
		r.Msg = newMsg
		b = log.TerminalFormat(false).Format(r)
		return b
	})
}

/*
 ******************************************************************************
 ********************************* Snowman API ********************************
 ******************************************************************************
 */

// implements SnowmanPlusPlusVM interface
func (vm *VM) GetActivationTime() time.Time {
	return time.Unix(vm.chainConfig.ApricotPhase4BlockTimestamp.Int64(), 0)
}

// Initialize implements the snowman.ChainVM interface
func (vm *VM) Initialize(
	ctx *snow.Context,
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

	// Set log level
	logLevel, err := log.LvlFromString(vm.config.LogLevel)
	if err != nil {
		return fmt.Errorf("failed to initialize logger due to: %w ", err)
	}

	vm.ctx = ctx
	vm.setLogLevel(logLevel)
	if b, err := json.Marshal(vm.config); err == nil {
		log.Info("Initializing Coreth VM", "Version", Version, "Config", string(b))
	} else {
		// Log a warning message since we have already successfully unmarshalled into the struct
		log.Warn("Problem initializing Coreth VM", "Version", Version, "Config", string(b), "err", err)
	}

	if len(fxs) > 0 {
		return errUnsupportedFXs
	}

	metrics.Enabled = vm.config.MetricsEnabled
	metrics.EnabledExpensive = vm.config.MetricsExpensiveEnabled

	vm.shutdownChan = make(chan struct{}, 1)
	baseDB := dbManager.Current().Database
	// Use NewNested rather than New so that the structure of the database
	// remains the same regardless of the provided baseDB type.
	vm.chaindb = Database{prefixdb.NewNested(ethDBPrefix, baseDB)}
	vm.db = versiondb.New(baseDB)
	vm.acceptedBlockDB = prefixdb.New(acceptedPrefix, vm.db)
	g := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, g); err != nil {
		return err
	}

	// Set the chain config for mainnet/fuji chain IDs
	switch {
	case g.Config.ChainID.Cmp(params.AvalancheMainnetChainID) == 0:
		g.Config = params.AvalancheMainnetChainConfig
		phase0BlockValidator.extDataHashes = mainnetExtDataHashes
	case g.Config.ChainID.Cmp(params.AvalancheFujiChainID) == 0:
		g.Config = params.AvalancheFujiChainConfig
		phase0BlockValidator.extDataHashes = fujiExtDataHashes
	case g.Config.ChainID.Cmp(params.AvalancheLocalChainID) == 0:
		g.Config = params.AvalancheLocalChainConfig
	}

	// Free the memory of the extDataHash map that is not used (i.e. if mainnet
	// config, free fuji)
	fujiExtDataHashes = nil
	mainnetExtDataHashes = nil

	vm.chainID = g.Config.ChainID

	ethConfig := ethconfig.NewDefaultConfig()
	ethConfig.Genesis = g
	ethConfig.NetworkId = vm.chainID.Uint64()

	// Set minimum price for mining and default gas price oracle value to the min
	// gas price to prevent so transactions and blocks all use the correct fees
	ethConfig.RPCGasCap = vm.config.RPCGasCap
	ethConfig.RPCEVMTimeout = vm.config.APIMaxDuration.Duration
	ethConfig.RPCTxFeeCap = vm.config.RPCTxFeeCap
	ethConfig.TxPool.NoLocals = !vm.config.LocalTxsEnabled
	ethConfig.AllowUnfinalizedQueries = vm.config.AllowUnfinalizedQueries
	ethConfig.AllowUnprotectedTxs = vm.config.AllowUnprotectedTxs
	ethConfig.Preimages = vm.config.Preimages
	ethConfig.Pruning = vm.config.Pruning
	ethConfig.PopulateMissingTries = vm.config.PopulateMissingTries
	ethConfig.PopulateMissingTriesParallelism = vm.config.PopulateMissingTriesParallelism
	ethConfig.AllowMissingTries = vm.config.AllowMissingTries
	ethConfig.SnapshotAsync = vm.config.SnapshotAsync
	ethConfig.SnapshotVerify = vm.config.SnapshotVerify
	ethConfig.OfflinePruning = vm.config.OfflinePruning
	ethConfig.OfflinePruningBloomFilterSize = vm.config.OfflinePruningBloomFilterSize
	ethConfig.OfflinePruningDataDirectory = vm.config.OfflinePruningDataDirectory

	// Create directory for offline pruning
	if len(ethConfig.OfflinePruningDataDirectory) != 0 {
		if err := os.MkdirAll(ethConfig.OfflinePruningDataDirectory, perms.ReadWriteExecute); err != nil {
			log.Error("failed to create offline pruning data directory", "error", err)
			return err
		}
	}

	vm.chainConfig = g.Config
	vm.networkID = ethConfig.NetworkId
	vm.secpFactory = crypto.FactorySECP256K1R{Cache: cache.LRU{Size: secpFactoryCacheSize}}

	nodecfg := node.Config{
		CorethVersion:         Version,
		KeyStoreDir:           vm.config.KeystoreDirectory,
		ExternalSigner:        vm.config.KeystoreExternalSigner,
		InsecureUnlockAllowed: vm.config.KeystoreInsecureUnlockAllowed,
	}

	vm.codec = Codec

	// TODO: read size from settings
	vm.mempool = NewMempool(ctx.AVAXAssetID, defaultMempoolSize)

	// Attempt to load last accepted block to determine if it is necessary to
	// initialize state with the genesis block.
	lastAcceptedBytes, lastAcceptedErr := vm.acceptedBlockDB.Get(lastAcceptedKey)
	var lastAcceptedHash common.Hash
	switch {
	case lastAcceptedErr == database.ErrNotFound:
		// Set [lastAcceptedHash] to the genesis block hash.
		lastAcceptedHash = ethConfig.Genesis.ToBlock(nil).Hash()
	case lastAcceptedErr != nil:
		return fmt.Errorf("failed to get last accepted block ID due to: %w", lastAcceptedErr)
	case len(lastAcceptedBytes) != common.HashLength:
		return fmt.Errorf("last accepted bytes should have been length %d, but found %d", common.HashLength, len(lastAcceptedBytes))
	default:
		lastAcceptedHash = common.BytesToHash(lastAcceptedBytes)
	}
	ethChain, err := coreth.NewETHChain(&ethConfig, &nodecfg, vm.chaindb, vm.config.EthBackendSettings(), vm.createConsensusCallbacks(), lastAcceptedHash, &vm.clock)
	if err != nil {
		return err
	}
	vm.chain = ethChain
	lastAccepted := vm.chain.LastAcceptedBlock()

	vm.atomicTxRepository, err = NewAtomicTxRepository(vm.db, vm.codec, lastAccepted.NumberU64())
	if err != nil {
		return fmt.Errorf("failed to create atomic repository: %w", err)
	}

	bonusBlockHeights := make(map[uint64]ids.ID)
	if vm.chainID.Cmp(params.AvalancheMainnetChainID) == 0 {
		bonusBlockHeights = bonusBlockMainnetHeights
	}
	if err := repairAtomicRepositoryForBonusBlockTxs(
		vm.atomicTxRepository,
		vm.db,
		getAtomicRepositoryRepairHeights(vm.chainID),
		vm.getAtomicTxFromPreApricot5BlockByHeight,
	); err != nil {
		return fmt.Errorf("failed to repair atomic repository: %w", err)
	}
	vm.atomicTrie, err = NewAtomicTrie(vm.db, vm.ctx.SharedMemory, bonusBlockHeights, vm.atomicTxRepository, vm.codec, lastAccepted.NumberU64())
	if err != nil {
		return fmt.Errorf("failed to create atomic trie: %w", err)
	}

	// start goroutines to update the tx pool gas minimum gas price when upgrades go into effect
	vm.handleGasPriceUpdates()

	vm.networkCodec, err = message.BuildCodec()
	if err != nil {
		return err
	}

	// initialize peer network
	vm.Network = peer.NewNetwork(appSender, vm.networkCodec, ctx.NodeID, vm.config.MaxOutboundActiveRequests)
	vm.client = peer.NewClient(vm.Network)
	vm.initGossipHandling()

	// start goroutines to manage block building
	//
	// NOTE: gossip network must be initialized first otherwie ETH tx gossip will
	// not work.
	vm.builder = vm.NewBlockBuilder(toEngine)

	vm.chain.Start()

	vm.genesisHash = vm.chain.GetGenesisBlock().Hash()
	log.Info(fmt.Sprintf("lastAccepted = %s", lastAccepted.Hash().Hex()))

	isApricotPhase5 := vm.chainConfig.IsApricotPhase5(new(big.Int).SetUint64(lastAccepted.Time()))
	atomicTxs, err := ExtractAtomicTxs(lastAccepted.ExtData(), isApricotPhase5, vm.codec)
	if err != nil {
		return err
	}

	vm.multiGatherer = avalanchegoMetrics.NewMultiGatherer()

	// Initialize [vm.State]
	if err := vm.initChainState(&Block{
		id:        ids.ID(lastAccepted.Hash()),
		ethBlock:  lastAccepted,
		vm:        vm,
		status:    choices.Accepted,
		atomicTxs: atomicTxs,
	}, metrics.Enabled); err != nil {
		return err
	}

	vm.builder.awaitSubmittedTxs()
	go vm.ctx.Log.RecoverAndPanic(vm.startContinuousProfiler)

	// The Codec explicitly registers the types it requires from the secp256k1fx
	// so [vm.baseCodec] is a dummy codec use to fulfill the secp256k1fx VM
	// interface. The fx will register all of its types, which can be safely
	// ignored by the VM's codec.
	vm.baseCodec = linearcodec.NewDefault()

	// pruneChain removes all rejected blocks stored in the database.
	//
	// TODO: This function can take over 60 minutes to run on mainnet and
	// should be converted to run asynchronously.
	// if err := vm.pruneChain(); err != nil {
	// 	return err
	// }

	// If metrics are enabled, register the default metrics regitry
	if metrics.Enabled {
		gatherer := corethPrometheus.Gatherer(metrics.DefaultRegistry)
		if err := vm.multiGatherer.Register(ethMetricsPrefix, gatherer); err != nil {
			return err
		}
	}
	// Register [multiGatherer] after registerers have been registered to it
	if err := ctx.Metrics.Register(vm.multiGatherer); err != nil {
		return err
	}

	return vm.fx.Initialize(vm)
}

func (vm *VM) initChainState(lastAcceptedBlock *Block, metricsEnabled bool) error {
	config := &chain.Config{
		DecidedCacheSize:    decidedCacheSize,
		MissingCacheSize:    missingCacheSize,
		UnverifiedCacheSize: unverifiedCacheSize,
		LastAcceptedBlock:   lastAcceptedBlock,
		GetBlockIDAtHeight:  vm.GetBlockIDAtHeight,
		GetBlock:            vm.getBlock,
		UnmarshalBlock:      vm.parseBlock,
		BuildBlock:          vm.buildBlock,
	}
	if !metricsEnabled {
		vm.State = chain.NewState(config)
		return nil
	}

	// Register chain state metrics
	chainStateRegisterer := prometheus.NewRegistry()
	state, err := chain.NewMeteredState(chainStateRegisterer, config)
	if err != nil {
		return err
	}
	vm.State = state

	return vm.multiGatherer.Register(chainStateMetricsPrefix, chainStateRegisterer)
}

func (vm *VM) initGossipHandling() {
	if vm.chainConfig.ApricotPhase4BlockTimestamp != nil {
		vm.gossiper = vm.newPushGossiper()
		vm.Network.SetGossipHandler(NewGossipHandler(vm))
	} else {
		vm.gossiper = &noopGossiper{}
		vm.Network.SetGossipHandler(message.NoopMempoolGossipHandler{})
	}
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
		rules := vm.chainConfig.AvalancheRules(header.Number, new(big.Int).SetUint64(header.Time))
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
		batchAtomicUTXOs  ids.Set
		batchContribution *big.Int = new(big.Int).Set(common.Big0)
		batchGasUsed      *big.Int = new(big.Int).Set(common.Big0)
		rules                      = vm.chainConfig.AvalancheRules(header.Number, new(big.Int).SetUint64(header.Time))
		size              int
	)

	for {
		tx, exists := vm.mempool.NextTx()
		if !exists {
			break
		}

		// Ensure that adding [tx] to the block will not exceed the block size soft limit.
		txSize := len(tx.Bytes())
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
	if !vm.chainConfig.IsApricotPhase5(new(big.Int).SetUint64(header.Time)) {
		return vm.preBatchOnFinalizeAndAssemble(header, state, txs)
	}
	return vm.postBatchOnFinalizeAndAssemble(header, state, txs)
}

func (vm *VM) onExtraStateChange(block *types.Block, state *state.StateDB) (*big.Int, *big.Int, error) {
	var (
		batchContribution *big.Int = big.NewInt(0)
		batchGasUsed      *big.Int = big.NewInt(0)
		timestamp                  = new(big.Int).SetUint64(block.Time())
		isApricotPhase4            = vm.chainConfig.IsApricotPhase4(timestamp)
		isApricotPhase5            = vm.chainConfig.IsApricotPhase5(timestamp)
	)

	txs, err := ExtractAtomicTxs(block.ExtData(), isApricotPhase5, vm.codec)
	if err != nil {
		return nil, nil, err
	}

	// If there are no transactions, we can return early
	if len(txs) == 0 {
		return nil, nil, nil
	}

	for _, tx := range txs {
		if err := tx.UnsignedAtomicTx.EVMStateTransfer(vm.ctx, state); err != nil {
			return nil, nil, err
		}
		// If ApricotPhase4 is enabled, calculate the block fee contribution
		if isApricotPhase4 {
			contribution, gasUsed, err := tx.BlockFeeContribution(isApricotPhase5, vm.ctx.AVAXAssetID, block.BaseFee())
			if err != nil {
				return nil, nil, err
			}

			batchContribution.Add(batchContribution, contribution)
			batchGasUsed.Add(batchGasUsed, gasUsed)
		}

		// If ApricotPhase5 is enabled, enforce that the atomic gas used does not exceed the
		// atomic gas limit.
		if vm.chainConfig.IsApricotPhase5(timestamp) {
			// Ensure that [tx] does not push [block] above the atomic gas limit.
			if batchGasUsed.Cmp(params.AtomicGasLimit) == 1 {
				return nil, nil, fmt.Errorf("atomic gas used (%d) by block (%s), exceeds atomic gas limit (%d)", batchGasUsed, block.Hash().Hex(), params.AtomicGasLimit)
			}
		}
	}
	return batchContribution, batchGasUsed, nil
}

func (vm *VM) pruneChain() error {
	if !vm.config.Pruning {
		return nil
	}
	pruned, err := vm.db.Has(pruneRejectedBlocksKey)
	if err != nil {
		return fmt.Errorf("failed to check if the VM has pruned rejected blocks: %w", err)
	}
	if pruned {
		return nil
	}

	lastAcceptedHeight := vm.LastAcceptedBlock().Height()
	if err := vm.chain.RemoveRejectedBlocks(0, lastAcceptedHeight); err != nil {
		return err
	}
	heightBytes := make([]byte, 8)
	binary.PutUvarint(heightBytes, lastAcceptedHeight)
	if err := vm.db.Put(pruneRejectedBlocksKey, heightBytes); err != nil {
		return err
	}
	return vm.db.Commit()
}

func (vm *VM) SetState(state snow.State) error {
	switch state {
	case snow.Bootstrapping:
		vm.bootstrapped = false
		return vm.fx.Bootstrapping()
	case snow.NormalOp:
		vm.bootstrapped = true
		return vm.fx.Bootstrapped()
	default:
		return snow.ErrUnknownState
	}
}

// Shutdown implements the snowman.ChainVM interface
func (vm *VM) Shutdown() error {
	if vm.ctx == nil {
		return nil
	}

	close(vm.shutdownChan)
	vm.chain.Stop()
	vm.shutdownWg.Wait()
	return nil
}

// buildBlock builds a block to be wrapped by ChainState
func (vm *VM) buildBlock() (snowman.Block, error) {
	block, err := vm.chain.GenerateBlock()
	vm.builder.handleGenerateBlock()
	if err != nil {
		vm.mempool.CancelCurrentTxs()
		return nil, err
	}

	isApricotPhase5 := vm.chainConfig.IsApricotPhase5(new(big.Int).SetUint64(block.Time()))
	atomicTxs, err := ExtractAtomicTxs(block.ExtData(), isApricotPhase5, vm.codec)
	if err != nil {
		vm.mempool.DiscardCurrentTxs()
		return nil, err
	}
	// Note: the status of block is set by ChainState
	blk := &Block{
		id:        ids.ID(block.Hash()),
		ethBlock:  block,
		vm:        vm,
		atomicTxs: atomicTxs,
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
func (vm *VM) parseBlock(b []byte) (snowman.Block, error) {
	ethBlock := new(types.Block)
	if err := rlp.DecodeBytes(b, ethBlock); err != nil {
		return nil, err
	}

	isApricotPhase5 := vm.chainConfig.IsApricotPhase5(new(big.Int).SetUint64(ethBlock.Time()))
	atomicTxs, err := ExtractAtomicTxs(ethBlock.ExtData(), isApricotPhase5, vm.codec)
	if err != nil {
		return nil, err
	}
	// Note: the status of block is set by ChainState
	block := &Block{
		id:        ids.ID(ethBlock.Hash()),
		ethBlock:  ethBlock,
		vm:        vm,
		atomicTxs: atomicTxs,
	}
	// Performing syntactic verification in ParseBlock allows for
	// short-circuiting bad blocks before they are processed by the VM.
	if _, err := block.syntacticVerify(); err != nil {
		return nil, fmt.Errorf("syntactic block verification failed: %w", err)
	}
	return block, nil
}

// getBlock attempts to retrieve block [id] from the VM to be wrapped
// by ChainState.
func (vm *VM) getBlock(id ids.ID) (snowman.Block, error) {
	ethBlock := vm.chain.GetBlockByHash(common.Hash(id))
	// If [ethBlock] is nil, return [database.ErrNotFound] here
	// so that the miss is considered cacheable.
	if ethBlock == nil {
		return nil, database.ErrNotFound
	}
	isApricotPhase5 := vm.chainConfig.IsApricotPhase5(new(big.Int).SetUint64(ethBlock.Time()))
	atomicTxs, err := ExtractAtomicTxs(ethBlock.ExtData(), isApricotPhase5, vm.codec)
	if err != nil {
		return nil, err
	}
	// Note: the status of block is set by ChainState
	blk := &Block{
		id:        ids.ID(ethBlock.Hash()),
		ethBlock:  ethBlock,
		vm:        vm,
		atomicTxs: atomicTxs,
	}
	return blk, nil
}

// SetPreference sets what the current tail of the chain is
func (vm *VM) SetPreference(blkID ids.ID) error {
	// Since each internal handler used by [vm.State] always returns a block
	// with non-nil ethBlock value, GetBlockInternal should never return a
	// (*Block) with a nil ethBlock value.
	block, err := vm.GetBlockInternal(blkID)
	if err != nil {
		return fmt.Errorf("failed to set preference to %s: %w", blkID, err)
	}

	return vm.chain.SetPreference(block.(*Block).ethBlock)
}

func (vm *VM) VerifyHeightIndex() error {
	// our index is vm.chain.GetBlockByNumber
	return nil
}

// GetBlockIDAtHeight retrieves the blkID of the canonical block at [blkHeight]
// if [blkHeight] is less than the height of the last accepted block, this will return
// a canonical block. Otherwise, it may return a blkID that has not yet been accepted.
func (vm *VM) GetBlockIDAtHeight(blkHeight uint64) (ids.ID, error) {
	ethBlock := vm.chain.GetBlockByNumber(blkHeight)
	if ethBlock == nil {
		return ids.ID{}, fmt.Errorf("could not find block at height: %d", blkHeight)
	}

	return ids.ID(ethBlock.Hash()), nil
}

func (vm *VM) Version() (string, error) {
	return Version, nil
}

// NewHandler returns a new Handler for a service where:
//   * The handler's functionality is defined by [service]
//     [service] should be a gorilla RPC service (see https://www.gorillatoolkit.org/pkg/rpc/v2)
//   * The name of the service is [name]
//   * The LockOption is the first element of [lockOption]
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
func (vm *VM) CreateHandlers() (map[string]*commonEng.HTTPHandler, error) {
	handler := vm.chain.NewRPCHandler(vm.config.APIMaxDuration.Duration)
	enabledAPIs := vm.config.EthAPIs()
	if err := vm.chain.AttachEthService(handler, enabledAPIs); err != nil {
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

	if vm.config.CorethAdminAPIEnabled {
		adminAPI, err := newHandler("admin", NewAdminService(vm, os.ExpandEnv(fmt.Sprintf("%s_coreth_performance_%s", vm.config.CorethAdminAPIDir, primaryAlias))))
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
func (vm *VM) CreateStaticHandlers() (map[string]*commonEng.HTTPHandler, error) {
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
func (vm *VM) conflicts(inputs ids.Set, ancestor *Block) error {
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
		nextAncestorIntf, err := vm.GetBlockInternal(nextAncestorID)
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
	chainIDAlias, hrp, addrBytes, err := formatting.ParseAddress(addrStr)
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
	preferredBlock := vm.chain.CurrentBlock()
	preferredState, err := vm.chain.BlockState(preferredBlock)
	if err != nil {
		return fmt.Errorf("failed to retrieve block state at tip while verifying atomic tx: %w", err)
	}
	rules := vm.currentRules()
	parentHeader := preferredBlock.Header()
	var nextBaseFee *big.Int
	timestamp := vm.clock.Time().Unix()
	bigTimestamp := big.NewInt(timestamp)
	if vm.chainConfig.IsApricotPhase3(bigTimestamp) {
		_, nextBaseFee, err = dummy.EstimateNextBaseFee(vm.chainConfig, parentHeader, uint64(timestamp))
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
	parentIntf, err := vm.GetBlockInternal(ids.ID(parentHash))
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

// GetAtomicUTXOs returns the utxos that at least one of the provided addresses is
// referenced in.
func (vm *VM) GetAtomicUTXOs(
	chainID ids.ID,
	addrs ids.ShortSet,
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
// Note: we return [][]*crypto.PrivateKeySECP256K1R even though each input
// corresponds to a single key, so that the signers can be passed in to
// [tx.Sign] which supports multiple keys on a single input.
func (vm *VM) GetSpendableFunds(
	keys []*crypto.PrivateKeySECP256K1R,
	assetID ids.ID,
	amount uint64,
) ([]EVMInput, [][]*crypto.PrivateKeySECP256K1R, error) {
	// Note: current state uses the state of the preferred block.
	state, err := vm.chain.CurrentState()
	if err != nil {
		return nil, nil, err
	}
	inputs := []EVMInput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}
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
		signers = append(signers, []*crypto.PrivateKeySECP256K1R{key})
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
// Note: we return [][]*crypto.PrivateKeySECP256K1R even though each input
// corresponds to a single key, so that the signers can be passed in to
// [tx.Sign] which supports multiple keys on a single input.
func (vm *VM) GetSpendableAVAXWithFee(
	keys []*crypto.PrivateKeySECP256K1R,
	amount uint64,
	cost uint64,
	baseFee *big.Int,
) ([]EVMInput, [][]*crypto.PrivateKeySECP256K1R, error) {
	// Note: current state uses the state of the preferred block.
	state, err := vm.chain.CurrentState()
	if err != nil {
		return nil, nil, err
	}

	initialFee, err := calculateDynamicFee(cost, baseFee)
	if err != nil {
		return nil, nil, err
	}

	newAmount, err := math.Add64(amount, initialFee)
	if err != nil {
		return nil, nil, err
	}
	amount = newAmount

	inputs := []EVMInput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}
	// Note: we assume that each key in [keys] is unique, so that iterating over
	// the keys will not produce duplicated nonces in the returned EVMInput slice.
	for _, key := range keys {
		if amount == 0 {
			break
		}

		prevFee, err := calculateDynamicFee(cost, baseFee)
		if err != nil {
			return nil, nil, err
		}

		newCost := cost + EVMInputGas
		newFee, err := calculateDynamicFee(newCost, baseFee)
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
		signers = append(signers, []*crypto.PrivateKeySECP256K1R{key})
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
	state, err := vm.chain.CurrentState()
	if err != nil {
		return 0, err
	}
	return state.GetNonce(address), nil
}

// currentRules returns the chain rules for the current block.
func (vm *VM) currentRules() params.Rules {
	header := vm.chain.APIBackend().CurrentHeader()
	return vm.chainConfig.AvalancheRules(header.Number, big.NewInt(int64(header.Time)))
}

// getBlockValidator returns the block validator that should be used for a block that
// follows the ruleset defined by [rules]
func (vm *VM) getBlockValidator(rules params.Rules) BlockValidator {
	switch {
	case rules.IsApricotPhase5:
		return phase5BlockValidator
	case rules.IsApricotPhase4:
		return phase4BlockValidator
	case rules.IsApricotPhase3:
		return phase3BlockValidator
	case rules.IsApricotPhase2, rules.IsApricotPhase1:
		// Note: the phase1BlockValidator is used in both apricot phase1 and phase2
		return phase1BlockValidator
	default:
		return phase0BlockValidator
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

func (vm *VM) estimateBaseFee(ctx context.Context) (*big.Int, error) {
	// Get the base fee to use
	baseFee, err := vm.chain.APIBackend().EstimateBaseFee(ctx)
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

func getAtomicRepositoryRepairHeights(chainID *big.Int) []uint64 {
	if chainID.Cmp(params.AvalancheMainnetChainID) != 0 {
		return nil
	}
	repairHeights := make([]uint64, 0, len(bonusBlockMainnetHeights)+len(canonicalBonusBlocks))
	for height := range bonusBlockMainnetHeights {
		repairHeights = append(repairHeights, height)
	}
	for _, height := range canonicalBonusBlocks {
		if _, exists := bonusBlockMainnetHeights[height]; !exists {
			repairHeights = append(repairHeights, height)
		}
	}
	sort.Slice(repairHeights, func(i, j int) bool { return repairHeights[i] < repairHeights[j] })
	return repairHeights
}

func (vm *VM) getAtomicTxFromPreApricot5BlockByHeight(height uint64) (*Tx, error) {
	blk := vm.chain.GetBlockByNumber(height)
	if blk == nil {
		return nil, nil
	}
	return ExtractAtomicTx(blk.ExtData(), vm.codec)
}

// repairAtomicRepositoryForBonusBlockTxs ensures that atomic txs that were processed
// on more than one block (canonical block + a number of bonus blocks) are indexed to
// the first height they were processed on (canonical block).
// [sortedHeights] should include all canonical block + bonus block heights in ascending
// order, and will only be passed as non-empty on mainnet.
func repairAtomicRepositoryForBonusBlockTxs(
	atomicTxRepository AtomicTxRepository, db *versiondb.Database,
	sortedHeights []uint64, getAtomicTxFromBlockByHeight func(height uint64) (*Tx, error),
) error {
	done, err := atomicTxRepository.IsBonusBlocksRepaired()
	if err != nil {
		return err
	}
	if done {
		return nil
	}
	repairedEntries := uint64(0)
	seenTxs := make(map[ids.ID][]uint64)
	for _, height := range sortedHeights {
		// get atomic tx from block
		tx, err := getAtomicTxFromBlockByHeight(height)
		if err != nil {
			return err
		}
		if tx == nil {
			continue
		}

		// get the tx by txID and update it, the first time we encounter
		// a given [txID], overwrite the previous [txID] => [height]
		// mapping. This provides a canonical mapping across nodes.
		heights, seen := seenTxs[tx.ID()]
		_, foundHeight, err := atomicTxRepository.GetByTxID(tx.ID())
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			return err
		}
		if !seen {
			if err := atomicTxRepository.Write(height, []*Tx{tx}); err != nil {
				return err
			}
		} else {
			if err := atomicTxRepository.WriteBonus(height, []*Tx{tx}); err != nil {
				return err
			}
		}
		if foundHeight != height && !seen {
			repairedEntries++
		}
		seenTxs[tx.ID()] = append(heights, height)
	}
	if err := atomicTxRepository.MarkBonusBlocksRepaired(repairedEntries); err != nil {
		return err
	}
	log.Info("repairAtomicRepositoryForBonusBlockTxs complete", "repairedEntries", repairedEntries)
	return db.Commit()
}
