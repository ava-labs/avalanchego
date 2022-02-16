// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	schain "github.com/ava-labs/subnet-evm/chain"
	subnetEVM "github.com/ava-labs/subnet-evm/chain"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/eth/ethconfig"
	"github.com/ava-labs/subnet-evm/metrics/prometheus"
	"github.com/ava-labs/subnet-evm/node"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/peer"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"

	// Force-load tracer engine to trigger registration
	//
	// We must import this package (not referenced elsewhere) so that the native "callTracer"
	// is added to a map of client-accessible tracers. In geth, this is done
	// inside of cmd/geth.
	_ "github.com/ava-labs/subnet-evm/eth/tracers/js"
	_ "github.com/ava-labs/subnet-evm/eth/tracers/native"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
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
	_ block.ChainVM              = &VM{}
	_ block.HeightIndexedChainVM = &VM{}
)

const (
	// Max time from current time allowed for blocks, before they're considered future blocks
	// and fail verification
	maxFutureBlockTime = 10 * time.Second

	decidedCacheSize    = 100
	missingCacheSize    = 50
	unverifiedCacheSize = 50
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
	ethDBPrefix     = []byte("ethdb")
)

var (
	errEmptyBlock               = errors.New("empty block")
	errUnsupportedFXs           = errors.New("unsupported feature extensions")
	errInvalidBlock             = errors.New("invalid block")
	errInvalidNonce             = errors.New("invalid nonce")
	errUnclesUnsupported        = errors.New("uncles unsupported")
	errTxHashMismatch           = errors.New("txs hash does not match header")
	errUncleHashMismatch        = errors.New("uncle hash mismatch")
	errInvalidDifficulty        = errors.New("invalid difficulty")
	errInvalidMixDigest         = errors.New("invalid mix digest")
	errHeaderExtraDataTooBig    = errors.New("header extra data too big")
	errNilBaseFeeSubnetEVM      = errors.New("nil base fee is invalid after subnetEVM")
	errNilBlockGasCostSubnetEVM = errors.New("nil blockGasCost is invalid after subnetEVM")
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

	networkID   uint64
	genesisHash common.Hash
	chain       *subnetEVM.ETHChain
	chainConfig *params.ChainConfig
	// [db] is the VM's current database managed by ChainState
	db *versiondb.Database
	// [chaindb] is the database supplied to the Ethereum backend
	chaindb Database
	// [acceptedBlockDB] is the database to store the last accepted
	// block.
	acceptedBlockDB database.Database

	builder *blockBuilder

	gossiper Gossiper

	clock mockable.Clock

	shutdownChan chan struct{}
	shutdownWg   sync.WaitGroup

	// Continuous Profiler
	profiler profiler.ContinuousProfiler

	peer.Network
	client       peer.Client
	networkCodec codec.Manager

	bootstrapped bool
}

// setLogLevel sets the log level with the original [os.StdErr] interface along
// with the context logger.
func (vm *VM) setLogLevel(logLevel log.Lvl) {
	format := log.TerminalFormat(false)
	log.Root().SetHandler(log.LvlFilterHandler(logLevel, log.MultiHandler(
		log.StreamHandler(originalStderr, format),
		log.StreamHandler(vm.ctx.Log, format),
	)))
}

/*
 ******************************************************************************
 ********************************* Snowman API ********************************
 ******************************************************************************
 */

// implements SnowmanPlusPlusVM interface
func (vm *VM) GetActivationTime() time.Time {
	return time.Unix(vm.chainConfig.SubnetEVMTimestamp.Int64(), 0)
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
	if b, err := json.Marshal(vm.config); err == nil {
		log.Info("Initializing Subnet EVM VM", "Version", Version, "Config", string(b))
	} else {
		// Log a warning message since we have already successfully unmarshalled into the struct
		log.Warn("Problem initializing Subnet EVM VM", "Version", Version, "Config", string(b), "err", err)
	}

	if len(fxs) > 0 {
		return errUnsupportedFXs
	}

	metrics.Enabled = vm.config.MetricsEnabled
	metrics.EnabledExpensive = vm.config.MetricsExpensiveEnabled

	vm.shutdownChan = make(chan struct{}, 1)
	vm.ctx = ctx
	baseDB := dbManager.Current().Database
	// Use NewNested rather than New so that the structure of the database
	// remains the same regardless of the provided baseDB type.
	vm.chaindb = Database{prefixdb.NewNested(ethDBPrefix, baseDB)}
	vm.db = versiondb.New(baseDB)
	vm.acceptedBlockDB = prefixdb.New(acceptedPrefix, vm.db)

	// Set log level
	logLevel, err := log.LvlFromString(vm.config.LogLevel)
	if err != nil {
		return fmt.Errorf("failed to initialize logger due to: %w ", err)
	}
	vm.setLogLevel(logLevel)

	g := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, g); err != nil {
		return err
	}

	if g.Config == nil {
		g.Config = params.SubnetEVMDefaultChainConfig
	}

	if g.Config.FeeConfig == nil {
		g.Config.FeeConfig = params.DefaultFeeConfig
	}

	ethConfig := ethconfig.NewDefaultConfig()
	// change network ID
	ethConfig.NetworkId = g.Config.ChainID.Uint64()
	ethConfig.Genesis = g
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
	ethConfig.SnapshotAsync = vm.config.SnapshotAsync
	ethConfig.SnapshotVerify = vm.config.SnapshotVerify

	// Handle custom fee recipient
	ethConfig.Miner.Etherbase = schain.BlackholeAddr
	switch {
	case common.IsHexAddress(vm.config.FeeRecipient):
		if g.Config.AllowFeeRecipients {
			address := common.HexToAddress(vm.config.FeeRecipient)
			log.Info("Setting fee recipient", "address", address)
			ethConfig.Miner.Etherbase = address
			break
		}
		return errors.New("cannot specify a custom fee recipient on this blockchain")
	case g.Config.AllowFeeRecipients:
		log.Warn("Chain enabled `AllowFeeRecipients`, but chain config has not specified any coinbase address. Defaulting to the blackhole address.")
	}

	// Handle offline pruning
	ethConfig.OfflinePruning = vm.config.OfflinePruning
	ethConfig.OfflinePruningBloomFilterSize = vm.config.OfflinePruningBloomFilterSize
	ethConfig.OfflinePruningDataDirectory = vm.config.OfflinePruningDataDirectory
	if len(ethConfig.OfflinePruningDataDirectory) != 0 {
		if err := os.MkdirAll(ethConfig.OfflinePruningDataDirectory, perms.ReadWriteExecute); err != nil {
			log.Error("failed to create offline pruning data directory", "error", err)
			return err
		}
	}

	vm.chainConfig = g.Config
	vm.networkID = ethConfig.NetworkId

	nodecfg := node.Config{
		SubnetEVMVersion:      Version,
		KeyStoreDir:           vm.config.KeystoreDirectory,
		ExternalSigner:        vm.config.KeystoreExternalSigner,
		InsecureUnlockAllowed: vm.config.KeystoreInsecureUnlockAllowed,
	}

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
	ethChain, err := subnetEVM.NewETHChain(&ethConfig, &nodecfg, vm.chaindb, vm.config.EthBackendSettings(), lastAcceptedHash, &vm.clock)
	if err != nil {
		return err
	}
	vm.chain = ethChain
	lastAccepted := vm.chain.LastAcceptedBlock()

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

	vm.State = chain.NewState(&chain.Config{
		DecidedCacheSize:    decidedCacheSize,
		MissingCacheSize:    missingCacheSize,
		UnverifiedCacheSize: unverifiedCacheSize,
		LastAcceptedBlock: &Block{
			id:       ids.ID(lastAccepted.Hash()),
			ethBlock: lastAccepted,
			vm:       vm,
			status:   choices.Accepted,
		},
		GetBlockIDAtHeight: vm.GetBlockIDAtHeight,
		GetBlock:           vm.getBlock,
		UnmarshalBlock:     vm.parseBlock,
		BuildBlock:         vm.buildBlock,
	})

	vm.builder.awaitSubmittedTxs()
	go vm.ctx.Log.RecoverAndPanic(vm.startContinuousProfiler)

	// Only provide metrics if they are being populated.
	if metrics.Enabled {
		gatherer := prometheus.Gatherer(metrics.DefaultRegistry)
		if err := ctx.Metrics.Register(gatherer); err != nil {
			return err
		}
	}

	return nil
}

func (vm *VM) initGossipHandling() {
	if vm.chainConfig.SubnetEVMTimestamp != nil {
		vm.gossiper = vm.newPushGossiper()
		vm.Network.SetGossipHandler(NewGossipHandler(vm))
	} else {
		vm.gossiper = &noopGossiper{}
		vm.Network.SetGossipHandler(message.NoopMempoolGossipHandler{})
	}
}

func (vm *VM) SetState(state snow.State) error {
	switch state {
	case snow.Bootstrapping:
		vm.bootstrapped = false
		return nil
	case snow.NormalOp:
		vm.bootstrapped = true
		return nil
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
		return nil, err
	}

	// Note: the status of block is set by ChainState
	blk := &Block{
		id:       ids.ID(block.Hash()),
		ethBlock: block,
		vm:       vm,
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
		return nil, fmt.Errorf("block failed verification due to: %w", err)
	}

	log.Debug(fmt.Sprintf("Built block %s", blk.ID()))
	// Marks the current transactions from the mempool as being successfully issued
	// into a block.
	return blk, nil
}

// parseBlock parses [b] into a block to be wrapped by ChainState.
func (vm *VM) parseBlock(b []byte) (snowman.Block, error) {
	ethBlock := new(types.Block)
	if err := rlp.DecodeBytes(b, ethBlock); err != nil {
		return nil, err
	}

	// Note: the status of block is set by ChainState
	block := &Block{
		id:       ids.ID(ethBlock.Hash()),
		ethBlock: ethBlock,
		vm:       vm,
	}
	// Performing syntactic verification in ParseBlock allows for
	// short-circuiting bad blocks before they are processed by the VM.
	if err := block.syntacticVerify(); err != nil {
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
	// Note: the status of block is set by ChainState
	blk := &Block{
		id:       ids.ID(ethBlock.Hash()),
		ethBlock: ethBlock,
		vm:       vm,
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
	if rules.IsSubnetEVM {
		return subnetEVMBlockValidator
	}

	return legacyBlockValidator
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
