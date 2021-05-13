// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	coreth "github.com/ava-labs/coreth/chain"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth/ethconfig"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	avalancheRPC "github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheJSON "github.com/ava-labs/avalanchego/utils/json"
)

var (
	// x2cRate is the conversion rate between the smallest denomination on the X-Chain
	// 1 nAVAX and the smallest denomination on the C-Chain 1 wei. Where 1 nAVAX = 1 gWei.
	// This is only required for AVAX because the denomination of 1 AVAX is 9 decimal
	// places on the X and P chains, but is 18 decimal places within the EVM.
	x2cRate = big.NewInt(1000000000)
	// GitCommit is set by the build script
	GitCommit string
	// Version is the version of Coreth
	Version = "coreth-v0.5.1"

	_ block.ChainVM = &VM{}
)

var (
	lastAcceptedKey = []byte("snowman_lastAccepted")
	acceptedPrefix  = []byte("snowman_accepted")
)

const (
	minBlockTime = 2 * time.Second
	maxBlockTime = 3 * time.Second
	// Max time from current time allowed for blocks, before they're considered future blocks
	// and fail verification
	maxFutureBlockTime   = 10 * time.Second
	batchSize            = 250
	maxUTXOsToFetch      = 1024
	blockCacheSize       = 1024
	codecVersion         = uint16(0)
	secpFactoryCacheSize = 1024
)

const (
	txFee = units.MilliAvax
)

var (
	errEmptyBlock                 = errors.New("empty block")
	errCreateBlock                = errors.New("couldn't create block")
	errUnknownBlock               = errors.New("unknown block")
	errBlockFrequency             = errors.New("too frequent block issuance")
	errUnsupportedFXs             = errors.New("unsupported feature extensions")
	errInvalidBlock               = errors.New("invalid block")
	errInvalidAddr                = errors.New("invalid hex address")
	errTooManyAtomicTx            = errors.New("too many pending atomic txs")
	errAssetIDMismatch            = errors.New("asset IDs in the input don't match the utxo")
	errNoImportInputs             = errors.New("tx has no imported inputs")
	errInputsNotSortedUnique      = errors.New("inputs not sorted and unique")
	errPublicKeySignatureMismatch = errors.New("signature doesn't match public key")
	errSignatureInputsMismatch    = errors.New("number of inputs does not match number of signatures")
	errWrongChainID               = errors.New("tx has wrong chain ID")
	errInsufficientFunds          = errors.New("insufficient funds")
	errNoExportOutputs            = errors.New("tx has no export outputs")
	errOutputsNotSorted           = errors.New("tx outputs not sorted")
	errOutputsNotSortedUnique     = errors.New("outputs not sorted and unique")
	errOverflowExport             = errors.New("overflow when computing export amount + txFee")
	errInvalidNonce               = errors.New("invalid nonce")
	errConflictingAtomicInputs    = errors.New("invalid block due to conflicting atomic inputs")
	errUnknownAtomicTx            = errors.New("unknown atomic tx type")
	errUnclesUnsupported          = errors.New("uncles unsupported")
	errTxHashMismatch             = errors.New("txs hash does not match header")
	errUncleHashMismatch          = errors.New("uncle hash mismatch")
	errRejectedParent             = errors.New("rejected parent")
	errInvalidDifficulty          = errors.New("invalid difficulty")
	errInvalidBlockVersion        = errors.New("invalid block version")
	errInvalidMixDigest           = errors.New("invalid mix digest")
	errInvalidExtDataHash         = errors.New("invalid extra data hash")
	errHeaderExtraDataTooBig      = errors.New("header extra data too big")
	errInsufficientFundsForFee    = errors.New("insufficient AVAX funds to pay transaction fee")
	errNoEVMOutputs               = errors.New("tx has no EVM outputs")
)

// mayBuildBlockStatus denotes whether the engine should be notified
// that a block should be built, or whether more time has to pass
// before doing so. See VM's [mayBuildBlock].
type mayBuildBlockStatus uint8

const (
	waitToBuild mayBuildBlockStatus = iota
	conditionalWaitToBuild
	mayBuild
)

func maxDuration(x, y time.Duration) time.Duration {
	if x > y {
		return x
	}
	return y
}

// Codec does serialization and deserialization
var Codec codec.Manager

func init() {
	Codec = codec.NewDefaultManager()
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&UnsignedImportTx{}),
		c.RegisterType(&UnsignedExportTx{}),
	)
	c.SkipRegistrations(3)
	errs.Add(
		c.RegisterType(&secp256k1fx.TransferInput{}),
		c.RegisterType(&secp256k1fx.MintOutput{}),
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		c.RegisterType(&secp256k1fx.MintOperation{}),
		c.RegisterType(&secp256k1fx.Credential{}),
		c.RegisterType(&secp256k1fx.Input{}),
		c.RegisterType(&secp256k1fx.OutputOwners{}),
		Codec.RegisterCodec(codecVersion, c),
	)

	if len(GitCommit) != 0 {
		Version = fmt.Sprintf("%s@%s", Version, GitCommit)
	}

	if errs.Errored() {
		panic(errs.Err)
	}
}

// VM implements the snowman.ChainVM interface
type VM struct {
	ctx *snow.Context

	CLIConfig CommandLineConfig

	chainID      *big.Int
	networkID    uint64
	genesisHash  common.Hash
	chain        *coreth.ETHChain
	chainConfig  *params.ChainConfig
	db           database.Database
	chaindb      Database
	newBlockChan chan *Block
	// A message is sent on this channel when a new block
	// is ready to be build. This notifies the consensus engine.
	notifyBuildBlockChan chan<- commonEng.Message
	newMinedBlockSub     *event.TypeMuxSubscription

	acceptedDB database.Database

	txPoolStabilizedLock sync.Mutex
	txPoolStabilizedHead common.Hash
	txPoolStabilizedOk   chan struct{}

	metalock                     sync.Mutex
	blockCache, blockStatusCache cache.LRU
	lastAccepted                 *Block

	// [buildBlockLock] must be held when accessing [mayBuildBlock],
	// [tryToBuildBlock] or [awaitingBuildBlock].
	buildBlockLock sync.Mutex
	// [buildBlockTimer] periodically fires in order to update [mayBuildBlock]
	// and to try to build a block, if applicable.
	buildBlockTimer *timer.Timer
	// [mayBuildBlock] == [wait] means that the next block may be built
	// only after more time has elapsed.
	// [mayBuildBlock] == [conditionalWait] means that the next block may be built
	// only if it has more than [batchSize] txs in it. Otherwise, wait until more
	// time has elapsed.
	// [mayBuildBlock] == [build] means that the next block may be built
	// at any time.
	mayBuildBlock mayBuildBlockStatus
	// If true, try to notify the engine that a block should be built.
	// Engine may not be notified because [mayBuildBlock] says to wait.
	tryToBuildBlock bool
	// If true, the engine has been notified that it should build a block
	// but has not done so yet. If this is the case, wait until it has
	// built a block before notifying it again.
	awaitingBuildBlock bool

	genlock            sync.Mutex
	atomicTxSubmitChan chan struct{}
	baseCodec          codec.Registry
	codec              codec.Manager
	clock              timer.Clock
	txFee              uint64
	pendingAtomicTxs   chan *Tx

	shutdownChan chan struct{}
	shutdownWg   sync.WaitGroup

	fx          secp256k1fx.Fx
	secpFactory crypto.FactorySECP256K1R
}

func (vm *VM) getAtomicTx(block *types.Block) (*Tx, error) {
	extdata := block.ExtData()
	if len(extdata) == 0 {
		return nil, nil
	}
	atx := new(Tx)
	if _, err := vm.codec.Unmarshal(extdata, atx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal atomic tx due to %w", err)
	}
	if err := atx.Sign(vm.codec, nil); err != nil {
		return nil, fmt.Errorf("failed to initialize atomic tx in block %s", block.Hash().Hex())
	}

	return atx, nil
}

// Codec implements the secp256k1fx interface
func (vm *VM) Codec() codec.Manager { return vm.codec }

// CodecRegistry implements the secp256k1fx interface
func (vm *VM) CodecRegistry() codec.Registry { return vm.baseCodec }

// Clock implements the secp256k1fx interface
func (vm *VM) Clock() *timer.Clock { return &vm.clock }

// Logger implements the secp256k1fx interface
func (vm *VM) Logger() logging.Logger { return vm.ctx.Log }

/*
 ******************************************************************************
 ********************************* Snowman API ********************************
 ******************************************************************************
 */

// Initialize implements the snowman.ChainVM interface
func (vm *VM) Initialize(
	ctx *snow.Context,
	db database.Database,
	b []byte,
	toEngine chan<- commonEng.Message,
	fxs []*commonEng.Fx,
) error {
	log.Info("Initializing Coreth VM", "Version", Version)
	if vm.CLIConfig.ParsingError != nil {
		return vm.CLIConfig.ParsingError
	}

	if len(fxs) > 0 {
		return errUnsupportedFXs
	}

	vm.shutdownChan = make(chan struct{}, 1)
	vm.ctx = ctx
	vm.db = db
	vm.chaindb = Database{db}
	g := new(core.Genesis)
	if err := json.Unmarshal(b, g); err != nil {
		return err
	}

	vm.acceptedDB = prefixdb.New(acceptedPrefix, db)

	// Set the ApricotPhase1BlockTimestamp for mainnet/fuji
	switch {
	case g.Config.ChainID.Cmp(params.AvalancheMainnetChainID) == 0:
		g.Config = params.AvalancheMainnetChainConfig
		phase0BlockValidator.extDataHashes = mainnetExtDataHashes
	case g.Config.ChainID.Cmp(params.AvalancheFujiChainID) == 0:
		g.Config = params.AvalancheFujiChainConfig
		phase0BlockValidator.extDataHashes = fujiExtDataHashes
	}

	// Allow ExtDataHashes to be garbage collected as soon as freed from block
	// validator
	fujiExtDataHashes = nil
	mainnetExtDataHashes = nil

	vm.chainID = g.Config.ChainID
	vm.txFee = txFee

	config := ethconfig.NewDefaultConfig()
	config.Genesis = g

	// Set minimum gas price and launch goroutine to sleep until
	// network upgrade when the gas price must be changed
	var gasPriceUpdate func() // must call after coreth.NewETHChain to avoid race
	if g.Config.ApricotPhase1BlockTimestamp == nil {
		config.Miner.GasPrice = params.LaunchMinGasPrice
		config.GPO.Default = params.LaunchMinGasPrice
		config.TxPool.PriceLimit = params.LaunchMinGasPrice.Uint64()
	} else {
		apricotTime := time.Unix(g.Config.ApricotPhase1BlockTimestamp.Int64(), 0)
		log.Info(fmt.Sprintf("Apricot Upgrade Time %v.", apricotTime))
		if time.Now().Before(apricotTime) {
			untilApricot := time.Until(apricotTime)
			log.Info(fmt.Sprintf("Upgrade will occur in %v", untilApricot))
			config.Miner.GasPrice = params.LaunchMinGasPrice
			config.GPO.Default = params.LaunchMinGasPrice
			config.TxPool.PriceLimit = params.LaunchMinGasPrice.Uint64()
			gasPriceUpdate = func() {
				time.Sleep(untilApricot)
				vm.chain.SetGasPrice(params.ApricotPhase1MinGasPrice)
			}
		} else {
			config.Miner.GasPrice = params.ApricotPhase1MinGasPrice
			config.GPO.Default = params.ApricotPhase1MinGasPrice
			config.TxPool.PriceLimit = params.ApricotPhase1MinGasPrice.Uint64()
		}
	}

	// Set minimum price for mining and default gas price oracle value to the min
	// gas price to prevent so transactions and blocks all use the correct fees
	config.RPCGasCap = vm.CLIConfig.RPCGasCap
	config.RPCTxFeeCap = vm.CLIConfig.RPCTxFeeCap
	config.TxPool.NoLocals = !vm.CLIConfig.LocalTxsEnabled
	config.AllowUnfinalizedQueries = vm.CLIConfig.AllowUnfinalizedQueries
	vm.chainConfig = g.Config
	vm.secpFactory = crypto.FactorySECP256K1R{Cache: cache.LRU{Size: secpFactoryCacheSize}}

	if err := config.SetGCMode("archive"); err != nil {
		panic(err)
	}
	nodecfg := node.Config{
		CorethVersion:         Version,
		KeyStoreDir:           vm.CLIConfig.KeystoreDirectory,
		ExternalSigner:        vm.CLIConfig.KeystoreExternalSigner,
		InsecureUnlockAllowed: vm.CLIConfig.KeystoreInsecureUnlockAllowed,
	}

	// Attempt to load last accepted block to determine if it is necessary to
	// initialize state with the genesis block.
	lastAcceptedBytes, lastAcceptedErr := vm.chaindb.Get(lastAcceptedKey)
	initGenesis := lastAcceptedErr == database.ErrNotFound
	chain := coreth.NewETHChain(&config, &nodecfg, vm.chaindb, vm.CLIConfig.EthBackendSettings(), initGenesis)
	vm.chain = chain
	vm.networkID = config.NetworkId

	// Kickoff gasPriceUpdate goroutine once the backend is initialized, if it
	// exists
	if gasPriceUpdate != nil {
		go gasPriceUpdate()
	}

	chain.SetOnFinalizeAndAssemble(func(state *state.StateDB, txs []*types.Transaction) ([]byte, error) {
		select {
		case atx := <-vm.pendingAtomicTxs:
			if err := atx.UnsignedAtomicTx.EVMStateTransfer(vm, state); err != nil {
				vm.newBlockChan <- nil
				return nil, err
			}
			raw, err := vm.codec.Marshal(codecVersion, atx)
			if err != nil {
				vm.newBlockChan <- nil
				return nil, fmt.Errorf("couldn't marshal atomic tx: %s", err)
			}
			return raw, nil
		default:
			if len(txs) == 0 {
				// this could happen due to the async logic of geth tx pool
				vm.newBlockChan <- nil
				return nil, errEmptyBlock
			}
		}
		return nil, nil
	})
	chain.SetOnBuild(func(block *types.Block) error {
		log.Trace("EVM built a block")

		blk := &Block{
			id:       ids.ID(block.Hash()),
			ethBlock: block,
			vm:       vm,
		}
		if err := blk.VerifyWithoutWrites(); err != nil {
			vm.newBlockChan <- nil
			return fmt.Errorf("block failed verify: %w", err)
		}
		return nil
	})
	chain.SetOnSealFinish(func(block *types.Block) error {
		log.Trace("EVM sealed a block")

		blk := &Block{
			id:       ids.ID(block.Hash()),
			ethBlock: block,
			vm:       vm,
		}
		if err := blk.Verify(); err != nil {
			vm.newBlockChan <- nil
			return fmt.Errorf("block failed verify: %w", err)
		}
		vm.newBlockChan <- blk
		if err := vm.updateStatus(ids.ID(block.Hash()), choices.Processing); err != nil {
			return fmt.Errorf("cannot update block status: %w", err)
		}
		vm.txPoolStabilizedLock.Lock()
		vm.txPoolStabilizedHead = block.Hash()
		vm.txPoolStabilizedLock.Unlock()
		return nil
	})
	chain.SetOnExtraStateChange(func(block *types.Block, state *state.StateDB) error {
		tx, err := vm.getAtomicTx(block)
		if err != nil {
			return err
		}
		if tx == nil {
			return nil
		}
		return tx.UnsignedAtomicTx.EVMStateTransfer(vm, state)
	})
	vm.blockCache = cache.LRU{Size: blockCacheSize}
	vm.blockStatusCache = cache.LRU{Size: blockCacheSize}
	vm.newBlockChan = make(chan *Block)
	vm.notifyBuildBlockChan = toEngine

	// Periodically updates [vm.mayBuildBlock] and tries to notify the engine to build
	// a new block, if applicable.
	vm.buildBlockTimer = timer.NewTimer(func() {
		vm.buildBlockLock.Lock()
		switch vm.mayBuildBlock {
		case waitToBuild:
			// Some time has passed. Allow block to be built if it has enough txs in it.
			vm.mayBuildBlock = conditionalWaitToBuild
			vm.buildBlockTimer.SetTimeoutIn(maxDuration(maxBlockTime-minBlockTime, 0))
		case conditionalWaitToBuild:
			// More time has passed. Allow block to be built regardless of tx count.
			vm.mayBuildBlock = mayBuild
		}
		tryBuildBlock := vm.tryToBuildBlock
		vm.buildBlockLock.Unlock()
		if tryBuildBlock {
			vm.tryBlockGen()
		}
	})
	go ctx.Log.RecoverAndPanic(vm.buildBlockTimer.Dispatch)

	vm.mayBuildBlock = mayBuild
	vm.tryToBuildBlock = true
	vm.txPoolStabilizedOk = make(chan struct{}, 1)
	// TODO: read size from options
	vm.pendingAtomicTxs = make(chan *Tx, 1024)
	vm.atomicTxSubmitChan = make(chan struct{}, 1)
	vm.newMinedBlockSub = vm.chain.SubscribeNewMinedBlockEvent()
	vm.shutdownWg.Add(1)
	go ctx.Log.RecoverAndPanic(vm.awaitTxPoolStabilized)
	if err := chain.Start(); err != nil {
		return fmt.Errorf("failed to start ETH Chain due to %w", err)
	}

	var lastAccepted *types.Block
	if lastAcceptedErr == nil {
		var hash common.Hash
		if err := rlp.DecodeBytes(lastAcceptedBytes, &hash); err == nil {
			if block := chain.GetBlockByHash(hash); block == nil {
				log.Debug("lastAccepted block not found in chaindb")
			} else {
				lastAccepted = block
			}
		}
	}

	// Determine if db corruption has occurred.
	switch {
	case lastAccepted != nil && initGenesis:
		return errors.New("database corruption detected, should be initializing genesis")
	case lastAccepted == nil && !initGenesis:
		return errors.New("database corruption detected, should not be initializing genesis")
	case lastAccepted == nil && initGenesis:
		log.Debug("lastAccepted is unavailable, setting to the genesis block")
		lastAccepted = chain.GetGenesisBlock()
	}
	vm.lastAccepted = &Block{
		id:       ids.ID(lastAccepted.Hash()),
		ethBlock: lastAccepted,
		vm:       vm,
	}
	if err := vm.chain.Accept(lastAccepted); err != nil {
		return fmt.Errorf("could not initialize VM with last accepted blkID %s: %w", vm.lastAccepted.ID(), err)
	}
	vm.genesisHash = chain.GetGenesisBlock().Hash()
	log.Info(fmt.Sprintf("lastAccepted = %s", vm.lastAccepted.ethBlock.Hash().Hex()))

	vm.shutdownWg.Add(1)
	go vm.ctx.Log.RecoverAndPanic(vm.awaitSubmittedTxs)
	vm.codec = Codec

	// The Codec explicitly registers the types it requires from the secp256k1fx
	// so [vm.baseCodec] is a dummy codec use to fulfill the secp256k1fx VM
	// interface. The fx will register all of its types, which can be safely
	// ignored by the VM's codec.
	vm.baseCodec = linearcodec.NewDefault()

	return vm.fx.Initialize(vm)
}

// Bootstrapping notifies this VM that the consensus engine is performing
// bootstrapping
func (vm *VM) Bootstrapping() error { return vm.fx.Bootstrapping() }

// Bootstrapped notifies this VM that the consensus engine has finished
// bootstrapping
func (vm *VM) Bootstrapped() error {
	vm.ctx.Bootstrapped()
	return vm.fx.Bootstrapped()
}

// Shutdown implements the snowman.ChainVM interface
func (vm *VM) Shutdown() error {
	if vm.ctx == nil {
		return nil
	}

	vm.buildBlockTimer.Stop()
	close(vm.shutdownChan)
	vm.chain.Stop()
	vm.shutdownWg.Wait()
	return nil
}

// BuildBlock implements the snowman.ChainVM interface
func (vm *VM) BuildBlock() (snowman.Block, error) {
	vm.chain.GenBlock()
	block := <-vm.newBlockChan

	vm.buildBlockLock.Lock()
	// Specify that we should wait before trying to build another block.
	vm.mayBuildBlock = waitToBuild
	vm.tryToBuildBlock = false
	vm.awaitingBuildBlock = false
	vm.buildBlockTimer.SetTimeoutIn(minBlockTime)
	vm.buildBlockLock.Unlock()

	if block == nil {
		return nil, errCreateBlock
	}

	log.Debug(fmt.Sprintf("Built block %s", block.ID()))
	// make sure Tx Pool is updated
	<-vm.txPoolStabilizedOk
	return block, nil
}

// ParseBlock implements the snowman.ChainVM interface
func (vm *VM) ParseBlock(b []byte) (snowman.Block, error) {
	vm.metalock.Lock()
	defer vm.metalock.Unlock()

	ethBlock := new(types.Block)
	if err := rlp.DecodeBytes(b, ethBlock); err != nil {
		return nil, err
	}
	block := &Block{
		id:       ids.ID(ethBlock.Hash()),
		ethBlock: ethBlock,
		vm:       vm,
	}
	// Performing syntactic verification in ParseBlock allows for
	// short-circuiting bad blocks before they are processed by the VM.
	if _, err := block.syntacticVerify(); err != nil {
		return nil, fmt.Errorf("syntactic block verification failed: %w", err)
	}
	vm.blockCache.Put(block.ID(), block)
	return block, nil
}

// GetBlock implements the snowman.ChainVM interface
func (vm *VM) GetBlock(id ids.ID) (snowman.Block, error) {
	vm.metalock.Lock()
	defer vm.metalock.Unlock()

	block := vm.getBlock(id)
	if block == nil {
		return nil, errUnknownBlock
	}
	return block, nil
}

// SetPreference sets what the current tail of the chain is
func (vm *VM) SetPreference(blkID ids.ID) error {
	block := vm.getBlock(blkID)
	if block == nil {
		return errUnknownBlock
	}
	return vm.chain.SetPreference(block.ethBlock)
}

// LastAccepted returns the ID of the block that was last accepted
func (vm *VM) LastAccepted() (ids.ID, error) {
	vm.metalock.Lock()
	defer vm.metalock.Unlock()

	return vm.lastAccepted.ID(), nil
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
	handler := vm.chain.NewRPCHandler(time.Duration(vm.CLIConfig.APIMaxDuration))
	enabledAPIs := vm.CLIConfig.EthAPIs()
	vm.chain.AttachEthService(handler, enabledAPIs)

	errs := wrappers.Errs{}
	if vm.CLIConfig.SnowmanAPIEnabled {
		errs.Add(handler.RegisterName("snowman", &SnowmanAPI{vm}))
		enabledAPIs = append(enabledAPIs, "snowman")
	}
	if vm.CLIConfig.CorethAdminAPIEnabled {
		primaryAlias, err := vm.ctx.BCLookup.PrimaryAlias(vm.ctx.ChainID)
		if err != nil {
			return nil, fmt.Errorf("failed to get primary alias for chain due to %w", err)
		}
		errs.Add(handler.RegisterName("admin", NewPerformanceService(fmt.Sprintf("coreth_%s_", primaryAlias))))
		enabledAPIs = append(enabledAPIs, "coreth-admin")
	}
	if vm.CLIConfig.NetAPIEnabled {
		errs.Add(handler.RegisterName("net", &NetAPI{vm}))
		enabledAPIs = append(enabledAPIs, "net")
	}
	if vm.CLIConfig.Web3APIEnabled {
		errs.Add(handler.RegisterName("web3", &Web3API{}))
		enabledAPIs = append(enabledAPIs, "web3")
	}
	if errs.Errored() {
		return nil, errs.Err
	}

	avaxAPI, err := newHandler("avax", &AvaxAPI{vm})
	if err != nil {
		return nil, fmt.Errorf("failed to register service for AVAX API due to %w", err)
	}

	log.Info(fmt.Sprintf("Enabled APIs: %s", strings.Join(enabledAPIs, ", ")))

	return map[string]*commonEng.HTTPHandler{
		"/rpc":  {LockOptions: commonEng.NoLock, Handler: handler},
		"/avax": avaxAPI,
		"/ws":   {LockOptions: commonEng.NoLock, Handler: handler.WebsocketHandler([]string{"*"})},
	}, nil
}

// CreateStaticHandlers makes new http handlers that can handle API calls
func (vm *VM) CreateStaticHandlers() (map[string]*commonEng.HTTPHandler, error) {
	handler := rpc.NewServer()
	if err := handler.RegisterName("static", &StaticService{}); err != nil {
		return nil, err
	}

	return map[string]*commonEng.HTTPHandler{
		"/rpc": {LockOptions: commonEng.NoLock, Handler: handler},
		"/ws":  {LockOptions: commonEng.NoLock, Handler: handler.WebsocketHandler([]string{"*"})},
	}, nil
}

/*
 ******************************************************************************
 *********************************** Helpers **********************************
 ******************************************************************************
 */

func (vm *VM) updateStatus(blkID ids.ID, status choices.Status) error {
	vm.metalock.Lock()
	defer vm.metalock.Unlock()

	if status == choices.Accepted {
		blk := vm.getBlock(blkID)
		if blk == nil {
			return errUnknownBlock
		}
		ethBlock := blk.ethBlock
		if err := vm.chain.Accept(ethBlock); err != nil {
			return fmt.Errorf("could not accept %s: %w", blkID, err)
		}
		if err := vm.setLastAccepted(blk); err != nil {
			return fmt.Errorf("could not set %s as last accepted: %w", blkID, err)
		}
	}

	vm.blockStatusCache.Put(blkID, status)
	return nil
}

func (vm *VM) tryBlockGen() error {
	vm.buildBlockLock.Lock()
	defer vm.buildBlockLock.Unlock()
	if vm.awaitingBuildBlock {
		// We notified the engine that a block should be built but it hasn't
		// done so yet. Wait until it has done so before notifying again.
		return nil
	}
	vm.tryToBuildBlock = true

	vm.genlock.Lock()
	defer vm.genlock.Unlock()
	// get pending size
	size, err := vm.chain.PendingSize()
	if err != nil {
		return err
	}
	if size == 0 && len(vm.pendingAtomicTxs) == 0 {
		return nil
	}

	switch vm.mayBuildBlock {
	case waitToBuild: // Wait more time before notifying engine to building a block
		return nil
	case conditionalWaitToBuild: // Notify engine only if there are enough pending txs
		if size < batchSize {
			return nil
		}
	case mayBuild: // Notify engine
	default:
		panic(fmt.Sprintf("mayBuildBlock has unexpected value %d", vm.mayBuildBlock))
	}
	select {
	case vm.notifyBuildBlockChan <- commonEng.PendingTxs:
		// Notify engine to build a block
		vm.awaitingBuildBlock = true
	default:
		return errBlockFrequency
	}
	return nil
}

func (vm *VM) getCachedStatus(blockID ids.ID) choices.Status {
	vm.metalock.Lock()
	defer vm.metalock.Unlock()

	if statusIntf, ok := vm.blockStatusCache.Get(blockID); ok {
		return statusIntf.(choices.Status)
	}

	wrappedBlk := vm.getBlock(blockID)
	if wrappedBlk == nil {
		return choices.Unknown
	}
	blk := wrappedBlk.ethBlock

	heightKey := blk.Number().Bytes()
	acceptedIDBytes, err := vm.acceptedDB.Get(heightKey)
	if err == nil {
		if acceptedID, err := ids.ToID(acceptedIDBytes); err != nil {
			log.Error(fmt.Sprintf("snowman-eth: acceptedID bytes didn't match expected value: %s", err))
		} else {
			if acceptedID == blockID {
				vm.blockStatusCache.Put(blockID, choices.Accepted)
				return choices.Accepted
			}
			vm.blockStatusCache.Put(blockID, choices.Rejected)
			return choices.Rejected
		}
	}

	status := vm.getUncachedStatus(blk)
	if status == choices.Accepted {
		err := vm.acceptedDB.Put(heightKey, blockID[:])
		if err != nil {
			log.Error(fmt.Sprintf("snowman-eth: failed to write back acceptedID bytes: %s", err))
		}

		tempBlock := wrappedBlk
		for tempBlock.ethBlock != nil {
			parentID := ids.ID(tempBlock.ethBlock.ParentHash())
			tempBlock = vm.getBlock(parentID)
			if tempBlock == nil || tempBlock.ethBlock == nil {
				break
			}

			heightKey := tempBlock.ethBlock.Number().Bytes()
			_, err := vm.acceptedDB.Get(heightKey)
			if err == nil {
				break
			}

			if err := vm.acceptedDB.Put(heightKey, parentID[:]); err != nil {
				log.Error(fmt.Sprintf("snowman-eth: failed to write back acceptedID bytes: %s", err))
			}
		}
	}

	vm.blockStatusCache.Put(blockID, status)
	return status
}

func (vm *VM) getUncachedStatus(blk *types.Block) choices.Status {
	acceptedBlk := vm.lastAccepted.ethBlock

	// TODO: There must be a better way of doing this.
	// Traverse up the chain from the lower block until the indices match
	highBlock := blk
	lowBlock := acceptedBlk
	if highBlock.Number().Cmp(lowBlock.Number()) < 0 {
		highBlock, lowBlock = lowBlock, highBlock
	}
	for highBlock.Number().Cmp(lowBlock.Number()) > 0 {
		parentBlock := vm.getBlock(ids.ID(highBlock.ParentHash()))
		if parentBlock == nil {
			return choices.Processing
		}
		highBlock = parentBlock.ethBlock
	}

	if highBlock.Hash() != lowBlock.Hash() { // on different branches
		return choices.Rejected
	}
	// on the same branch
	if blk.Number().Cmp(acceptedBlk.Number()) <= 0 {
		return choices.Accepted
	}
	return choices.Processing
}

func (vm *VM) getBlock(id ids.ID) *Block {
	if blockIntf, ok := vm.blockCache.Get(id); ok {
		return blockIntf.(*Block)
	}
	ethBlock := vm.chain.GetBlockByHash(common.Hash(id))
	if ethBlock == nil {
		return nil
	}
	block := &Block{
		id:       ids.ID(ethBlock.Hash()),
		ethBlock: ethBlock,
		vm:       vm,
	}
	vm.blockCache.Put(id, block)
	return block
}

// setLastAccepted sets [blk] to be the VM's [lastAccepted] block
// and stores its hash at [lastAcceptedKey].
//
// Assumes [metalock] is held.
func (vm *VM) setLastAccepted(blk *Block) error {
	vm.lastAccepted = blk

	b, err := rlp.EncodeToBytes(blk.ethBlock.Hash())
	if err != nil {
		return err
	}
	return vm.chaindb.Put(lastAcceptedKey, b)
}

// awaitTxPoolStabilized waits for a txPoolHead channel event
// and notifies the VM when the tx pool has stabilized to the
// expected block hash
// Waits for signal to shutdown from [vm.shutdownChan]
func (vm *VM) awaitTxPoolStabilized() {
	defer vm.shutdownWg.Done()
	for {
		select {
		case e, ok := <-vm.newMinedBlockSub.Chan():
			if !ok {
				return
			}
			if e == nil {
				continue
			}
			switch h := e.Data.(type) {
			case core.NewMinedBlockEvent:
				vm.txPoolStabilizedLock.Lock()
				if vm.txPoolStabilizedHead == h.Block.Hash() {
					vm.txPoolStabilizedOk <- struct{}{}
					vm.txPoolStabilizedHead = common.Hash{}
				}
				vm.txPoolStabilizedLock.Unlock()
			default:
			}
		case <-vm.shutdownChan:
			return
		}
	}
}

func (vm *VM) awaitSubmittedTxs() {
	defer vm.shutdownWg.Done()
	txSubmitChan := vm.chain.GetTxSubmitCh()
	for {
		select {
		case <-txSubmitChan:
			log.Trace("New tx detected, trying to generate a block")
			vm.tryBlockGen()
		case <-vm.atomicTxSubmitChan:
			log.Trace("New atomic Tx detected, trying to generate a block")
			vm.tryBlockGen()
		case <-time.After(5 * time.Second):
			vm.tryBlockGen()
		case <-vm.shutdownChan:
			return
		}
	}
}

func (vm *VM) getLastAccepted() *Block {
	vm.metalock.Lock()
	defer vm.metalock.Unlock()

	return vm.lastAccepted
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

func (vm *VM) issueTx(tx *Tx) error {
	select {
	case vm.pendingAtomicTxs <- tx:
		select {
		case vm.atomicTxSubmitChan <- struct{}{}:
		default:
		}
	default:
		return errTooManyAtomicTx
	}
	return nil
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

// GetSpendableFunds returns a list of EVMInputs and keys (in corresponding order)
// to total [amount] of [assetID] owned by [keys]
// Note: we return [][]*crypto.PrivateKeySECP256K1R even though each input corresponds
// to a single key, so that the signers can be passed in to [tx.Sign] which supports
// multiple keys on a single input.
func (vm *VM) GetSpendableFunds(keys []*crypto.PrivateKeySECP256K1R, assetID ids.ID, amount uint64) ([]EVMInput, [][]*crypto.PrivateKeySECP256K1R, error) {
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
	case rules.IsApricotPhase2:
		// Note: the phase1BlockValidator is used in both apricot phase1 and phase2
		return phase1BlockValidator
	case rules.IsApricotPhase1:
		return phase1BlockValidator
	default:
		return phase0BlockValidator
	}
}

// ParseLocalAddress takes in an address for this chain and produces the ID
func (vm *VM) ParseLocalAddress(addrStr string) (ids.ShortID, error) {
	chainID, addr, err := vm.ParseAddress(addrStr)
	if err != nil {
		return ids.ShortID{}, err
	}
	if chainID != vm.ctx.ChainID {
		return ids.ShortID{}, fmt.Errorf("expected chainID to be %q but was %q",
			vm.ctx.ChainID, chainID)
	}
	return addr, nil
}

// FormatLocalAddress takes in a raw address and produces the formatted address
func (vm *VM) FormatLocalAddress(addr ids.ShortID) (string, error) {
	return vm.FormatAddress(vm.ctx.ChainID, addr)
}

// FormatAddress takes in a chainID and a raw address and produces the formatted
// address
func (vm *VM) FormatAddress(chainID ids.ID, addr ids.ShortID) (string, error) {
	chainIDAlias, err := vm.ctx.BCLookup.PrimaryAlias(chainID)
	if err != nil {
		return "", err
	}
	hrp := constants.GetHRP(vm.ctx.NetworkID)
	return formatting.FormatAddress(chainIDAlias, hrp, addr.Bytes())
}

// ParseEthAddress parses [addrStr] and returns an Ethereum address
func ParseEthAddress(addrStr string) (common.Address, error) {
	if !common.IsHexAddress(addrStr) {
		return common.Address{}, errInvalidAddr
	}
	return common.HexToAddress(addrStr), nil
}

// FormatEthAddress formats [addr] into a string
func FormatEthAddress(addr common.Address) string {
	return addr.Hex()
}

// GetEthAddress returns the ethereum address derived from [privKey]
func GetEthAddress(privKey *crypto.PrivateKeySECP256K1R) common.Address {
	return PublicKeyToEthAddress(privKey.PublicKey().(*crypto.PublicKeySECP256K1R))
}

// PublicKeyToEthAddress returns the ethereum address derived from [pubKey]
func PublicKeyToEthAddress(pubKey *crypto.PublicKeySECP256K1R) common.Address {
	return ethcrypto.PubkeyToAddress(*(pubKey.ToECDSA()))
}
