// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	chainState "github.com/ava-labs/coreth/plugin/evm/state"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	avalancheRPC "github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
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
	x2cRate = big.NewInt(1000000000)
	// GitCommit is set by the build script
	GitCommit string
	// Version is the version of Coreth
	Version = "coreth-v0.3.26"

	_ block.ChainVM = &VM{}
)

var (
	// Set last accepted key to be longer than the keys used to store accepted block IDs.
	lastAcceptedKey              = []byte("last_accepted_key")
	acceptedPrefix               = []byte("snowman_accepted")
	historicalCanonicalRepairKey = []byte("chain_repaired_20210212")
	tipCanonicalRepairKey        = []byte("chain_repaired_20210305")
)

const (
	minBlockTime = 2 * time.Second
	maxBlockTime = 3 * time.Second
	// maxFutureBlockTime should be smaller than the max allowed future time (15s) used
	// in dummy consensus engine's verifyHeader
	maxFutureBlockTime  = 10 * time.Second
	batchSize           = 250
	maxUTXOsToFetch     = 1024
	chainStateCacheSize = 1024
	codecVersion        = uint16(0)
	txFee               = units.MilliAvax
)

var (
	ethDBPrefix    = []byte("ethdb")
	atomicTxPrefix = []byte("atomicTxDB")
)

var (
	errEmptyBlock                 = errors.New("empty block")
	errCreateBlock                = errors.New("couldn't create block")
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
	errOverflowExport             = errors.New("overflow when computing export amount + txFee")
	errInvalidNonce               = errors.New("invalid nonce")
	errInvalidGas                 = errors.New("invalid block due to low gas")
	errConflictingAtomicInputs    = errors.New("invalid block due to conflicting atomic inputs")
	errUnknownAtomicTx            = errors.New("unknown atomic tx type")
	errUnclesUnsupported          = errors.New("uncles unsupported")
	errTxHashMismatch             = errors.New("txs hash does not match header")
	errUncleHashMismatch          = errors.New("uncle hash mismatch")
	errRejectedParent             = errors.New("rejected parent")
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
	// ChainState helps to implement the VM interface by wrapping fetching
	// with an efficient cache.
	*chainState.ChainState

	CLIConfig CommandLineConfig

	chainID            *big.Int
	networkID          uint64
	genesisHash        common.Hash
	chain              *coreth.ETHChain
	db                 database.Database
	chaindb            Database
	acceptedBlockDB    database.Database
	acceptedAtomicTxDB database.Database

	newBlockChan chan *Block
	// A message is sent on this channel when a new block
	// is ready to be build. This notifies the consensus engine.
	notifyBuildBlockChan chan<- commonEng.Message
	newMinedBlockSub     *event.TypeMuxSubscription

	txPoolStabilizedLock sync.Mutex
	txPoolStabilizedHead common.Hash
	txPoolStabilizedOk   chan struct{}

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
	txSubmitChan       <-chan struct{}
	atomicTxSubmitChan chan struct{}
	baseCodec          codec.Registry
	codec              codec.Manager
	clock              timer.Clock
	txFee              uint64
	pendingAtomicTxs   chan *Tx

	shutdownChan chan struct{}
	shutdownWg   sync.WaitGroup

	fx secp256k1fx.Fx
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
	dbManager manager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
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

	db := dbManager.Current()
	vm.ChainState = chainState.NewChainState(db, chainStateCacheSize)
	vm.shutdownChan = make(chan struct{}, 1)
	vm.ctx = ctx
	vm.db = vm.ChainState.ExternalDB()
	vm.chaindb = Database{prefixdb.New(ethDBPrefix, vm.db)}
	vm.acceptedBlockDB = prefixdb.New(acceptedPrefix, vm.db)
	vm.acceptedAtomicTxDB = prefixdb.New(atomicTxPrefix, vm.db)
	g := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, g); err != nil {
		return err
	}

	vm.chainID = g.Config.ChainID
	vm.txFee = txFee

	config := eth.DefaultConfig
	config.Genesis = g
	// disable the experimental snapshot feature from geth
	config.TrieCleanCache += config.SnapshotCache
	config.SnapshotCache = 0

	config.Miner.ManualMining = true

	// Set minimum price for mining and default gas price oracle value to the min
	// gas price to prevent so transactions and blocks all use the correct fees
	config.Miner.GasPrice = params.MinGasPrice
	config.RPCGasCap = vm.CLIConfig.RPCGasCap
	config.RPCTxFeeCap = vm.CLIConfig.RPCTxFeeCap
	config.GPO.Default = params.MinGasPrice
	config.TxPool.PriceLimit = params.MinGasPrice.Uint64()
	config.TxPool.NoLocals = !vm.CLIConfig.LocalTxsEnabled

	if err := config.SetGCMode("archive"); err != nil {
		panic(err)
	}
	nodecfg := node.Config{NoUSB: true}
	chain := coreth.NewETHChain(&config, &nodecfg, nil, vm.chaindb, vm.CLIConfig.EthBackendSettings())
	vm.chain = chain
	vm.networkID = config.NetworkId
	chain.SetOnHeaderNew(func(header *types.Header) {
		hid := make([]byte, 32)
		_, err := rand.Read(hid)
		if err != nil {
			panic("cannot generate hid")
		}
		header.Extra = append(header.Extra, hid...)
	})
	chain.SetOnFinalizeAndAssemble(func(state *state.StateDB, txs []*types.Transaction) ([]byte, error) {
		select {
		case atx := <-vm.pendingAtomicTxs:
			if err := atx.UnsignedTx.(UnsignedAtomicTx).EVMStateTransfer(vm, state); err != nil {
				vm.newBlockChan <- nil
				return nil, err
			}
			raw, _ := vm.codec.Marshal(codecVersion, atx)
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
	chain.SetOnSealFinish(func(block *types.Block) error {
		log.Trace("EVM sealed a block")

		blk := &Block{
			id:       ids.ID(block.Hash()),
			ethBlock: block,
			vm:       vm,
			status:   choices.Processing,
		}
		// Verify is called on a non-wrapped block here, such that this
		// does not add [blk] to the processing blocks map in ChainState.
		// TODO cache verification since Verify() will be called by the
		// consensus engine as well.
		// Note: this is only called when building a new block, so caching
		// verification will only be a significant optimization for nodes
		// that produce a large number of blocks.
		if err := blk.Verify(); err != nil {
			vm.newBlockChan <- nil
			return fmt.Errorf("block failed verify: %w", err)
		}
		vm.newBlockChan <- blk
		vm.txPoolStabilizedLock.Lock()
		vm.txPoolStabilizedHead = block.Hash()
		vm.txPoolStabilizedLock.Unlock()
		return nil
	})
	chain.SetOnExtraStateChange(func(block *types.Block, state *state.StateDB) error {
		tx := vm.extractAtomicTx(block)
		if tx == nil {
			return nil
		}
		return tx.UnsignedTx.(UnsignedAtomicTx).EVMStateTransfer(vm, state)
	})
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
	chain.Start()

	var lastAcceptedBlk *Block
	ethGenesisBlock := chain.GetGenesisBlock()
	vm.genesisHash = chain.GetGenesisBlock().Hash()
	blkIDBytes, err := vm.acceptedBlockDB.Get(lastAcceptedKey)
	if err == nil {
		blkHash := common.BytesToHash(blkIDBytes)
		ethLastAcceptedBlock := vm.chain.GetBlockByHash(blkHash)
		if ethLastAcceptedBlock == nil {
			return fmt.Errorf("failed to get block by hash of last accepted blk %s", blkHash)
		}
		lastAcceptedBlk = &Block{
			ethBlock: ethLastAcceptedBlock,
			id:       ids.ID(ethLastAcceptedBlock.Hash()),
			vm:       vm,
			status:   choices.Accepted,
		}
	} else if err == database.ErrNotFound {
		// The VM is being initialized for the first time. Create the genesis block and mark it as accepted.
		lastAcceptedBlk = &Block{
			ethBlock: ethGenesisBlock,
			id:       ids.ID(vm.genesisHash),
			vm:       vm,
			status:   choices.Accepted,
		}
	} else {
		return fmt.Errorf("failed to get last accepted block due to %w", err)
	}

	vm.ChainState.Initialize(lastAcceptedBlk, vm.internalGetBlockIDAtHeight, vm.internalGetBlock, vm.internalParseBlock, vm.internalBuildBlock)
	if err := vm.chain.Accept(lastAcceptedBlk.ethBlock); err != nil {
		return fmt.Errorf("could not initialize VM with last accepted blkID %s: %w", lastAcceptedBlk.ethBlock.Hash().Hex(), err)
	}
	log.Info("Initializing Coreth VM", "Last Accepted", lastAcceptedBlk.ethBlock.Hash().Hex())

	vm.shutdownWg.Add(1)
	go vm.ctx.Log.RecoverAndPanic(vm.awaitSubmittedTxs)
	vm.codec = Codec
	if err := vm.repairCanonicalChain(); err != nil {
		return fmt.Errorf("failed to repair the canonical chain: %w", err)
	}

	log.Debug("unlocking indexing")
	chain.BlockChain().UnlockIndexing()

	// The Codec explicitly registers the types it requires from the secp256k1fx
	// so [vm.baseCodec] is a dummy codec use to fulfill the secp256k1fx VM
	// interface. The fx will register all of its types, which can be safely
	// ignored by the VM's codec.
	vm.baseCodec = linearcodec.NewDefault()

	return vm.fx.Initialize(vm)
}

// repairCanonicalChain writes the canonical chain index from the last accepted
// block back to the genesis block to overwrite any corruption that might have
// occurred.
// assumes that the genesisHash and [lastAccepted] block have already been set.
func (vm *VM) repairCanonicalChain() error {
	if ran, err := vm.repairHistorical(); err != nil {
		return err
	} else if ran {
		return nil
	}

	_, err := vm.repairTip()
	return err
}

// repairHistorical writes the canonical chain index from the last accepted block back to the
// genesis block to overwrite any corruption that might have occurred.
// assumes that the genesis hash and [lastAccepted] block have already been set.
// returns true if the repair occurs during this call. If the repair occurred on a
// prior run, then false is returned.
func (vm *VM) repairHistorical() (bool, error) {
	// Check if the historical canonical chain repair has already occurred.
	if has, err := vm.db.Has(historicalCanonicalRepairKey); err != nil {
		return false, err
	} else if has {
		return false, nil
	}

	start := time.Now()
	log.Info("starting historical canonical chain repair", "startTime", start)
	genesisBlock := vm.chain.GetGenesisBlock()
	if genesisBlock == nil {
		return false, fmt.Errorf("failed to fetch genesis block from chain during historical chain repair")
	}
	if err := vm.chain.WriteCanonicalFromCurrentBlock(genesisBlock); err != nil {
		return false, fmt.Errorf("historical canonical chain repair failed after %v due to: %w", time.Since(start), err)
	}
	log.Info("finished historical canonical chain repair", "timeElapsed", time.Since(start))
	if err := vm.db.Put(historicalCanonicalRepairKey, []byte("finished")); err != nil {
		return false, fmt.Errorf("failed to mark flag for historical canonical chain repair: %w", err)
	}
	if err := vm.chain.ValidateCanonicalChain(); err != nil {
		return false, fmt.Errorf("failed to validate historical canonical chain repair due to: %w", err)
	}

	return true, nil
}

// repairTip writes the canonical chain index from the current block in the canonical chain
// back to the last accepted block to overwrite any corruption of non-finalized blocks that
// might have occurred.
// assumes that the genesis hash and [lastAccepted] block have already been set.
// returns true if the repair occurs during this call. If the repair occurred on a
// prior run, then false is returned.
func (vm *VM) repairTip() (bool, error) {
	// Check if the canonical chain tip repair has already occurred.
	if has, err := vm.db.Has(tipCanonicalRepairKey); err != nil {
		return false, err
	} else if has {
		return false, nil
	}

	start := time.Now()
	log.Info("starting canonical chain tip repair", "startTime", start)
	blk := vm.LastAcceptedBlockInternal().(*Block)
	if err := vm.chain.WriteCanonicalFromCurrentBlock(blk.ethBlock); err != nil {
		return false, fmt.Errorf("canonical chain tip repair failed after %v due to: %w", time.Since(start), err)
	}
	log.Info("finished canonical chain tip repair", "timeElapsed", time.Since(start))
	if err := vm.db.Put(tipCanonicalRepairKey, []byte("finished")); err != nil {
		return false, fmt.Errorf("failed to mark flag for canonical chain tip repair due to: %w", err)
	}

	return true, nil
}

// TODO remove once unused by repairCanonicalChain
func (vm *VM) lastAcceptedEthBlock() *types.Block {
	return vm.LastAcceptedBlockInternal().(*Block).ethBlock
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

// internalBuildBlock ...
func (vm *VM) internalBuildBlock() (chainState.Block, error) {
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

// internalParseBlock
func (vm *VM) internalParseBlock(b []byte) (chainState.Block, error) {
	ethBlock := new(types.Block)
	if err := rlp.DecodeBytes(b, ethBlock); err != nil {
		return nil, err
	}
	block := &Block{
		id:       ids.ID(ethBlock.Hash()),
		ethBlock: ethBlock,
		vm:       vm,
		status:   choices.Processing,
	}
	// Performing syntactic verification in ParseBlock allows for
	// short-circuiting bad blocks before they are processed by the VM.
	if err := block.syntacticVerify(); err != nil {
		return nil, fmt.Errorf("syntactic block verification failed: %w", err)
	}
	return block, nil
}

func (vm *VM) internalGetBlock(id ids.ID) (chainState.Block, error) {
	ethBlock := vm.chain.GetBlockByHash(common.Hash(id))
	if ethBlock == nil {
		return nil, fmt.Errorf("block %s not found", id)
	}
	blk := &Block{
		id:       ids.ID(ethBlock.Hash()),
		ethBlock: ethBlock,
		vm:       vm,
		status:   choices.Processing,
	}
	return blk, nil
}

// SetPreference sets what the current tail of the chain is
func (vm *VM) SetPreference(blkID ids.ID) error {
	block, err := vm.GetBlockInternal(blkID)
	if err != nil {
		return fmt.Errorf("failed to set preference to %s: %w", blkID, err)
	}
	return vm.chain.SetPreference(block.(*Block).ethBlock)
}

// internalGetBlockIDAtHeight retrieves the blkID of the canonical block at [blkHeight]
// if [blkHeight] is less than the height of the last accepted block, this will return
// a canonical block. Otherwise, it may return a blkID that has not yet been accepted.
func (vm *VM) internalGetBlockIDAtHeight(blkHeight uint64) (ids.ID, error) {
	ethBlock := vm.chain.GetBlockByNumber(blkHeight)
	if ethBlock == nil {
		return ids.ID{}, fmt.Errorf("could not find block at height: %d", blkHeight)
	}

	return ids.ID(ethBlock.Hash()), nil
}

// NewHandler returns a new Handler for a service where:
//   * The handler's functionality is defined by [service]
//     [service] should be a gorilla RPC service (see https://www.gorillatoolkit.org/pkg/rpc/v2)
//   * The name of the service is [name]
//   * The LockOption is the first element of [lockOption]
//     By default the LockOption is WriteLock
//     [lockOption] should have either 0 or 1 elements. Elements beside the first are ignored.
func newHandler(name string, service interface{}, lockOption ...commonEng.LockOption) *commonEng.HTTPHandler {
	server := avalancheRPC.NewServer()
	server.RegisterCodec(avalancheJSON.NewCodec(), "application/json")
	server.RegisterCodec(avalancheJSON.NewCodec(), "application/json;charset=UTF-8")
	server.RegisterService(service, name)

	var lock commonEng.LockOption = commonEng.WriteLock
	if len(lockOption) != 0 {
		lock = lockOption[0]
	}
	return &commonEng.HTTPHandler{LockOptions: lock, Handler: server}
}

// CreateHandlers makes new http handlers that can handle API calls
func (vm *VM) CreateHandlers() (map[string]*commonEng.HTTPHandler, error) {
	handler := vm.chain.NewRPCHandler(time.Duration(vm.CLIConfig.APIMaxDuration))
	enabledAPIs := vm.CLIConfig.EthAPIs()
	vm.chain.AttachEthService(handler, vm.CLIConfig.EthAPIs())

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

	log.Info(fmt.Sprintf("Enabled APIs: %s", strings.Join(enabledAPIs, ", ")))

	return map[string]*commonEng.HTTPHandler{
		"/rpc":  {LockOptions: commonEng.NoLock, Handler: handler},
		"/avax": newHandler("avax", &AvaxAPI{vm}),
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

// extractAtomicTx returns the atomic transaction in [block] if
// one exists.
func (vm *VM) extractAtomicTx(block *types.Block) *Tx {
	extdata := block.ExtraData()
	atx := new(Tx)
	if _, err := vm.codec.Unmarshal(extdata, atx); err != nil {
		return nil
	}
	atx.Sign(vm.codec, nil)
	return atx
}

// getAtomicTx attempts to get [txID] from the database.
func (vm *VM) getAtomicTx(txID ids.ID) (*Tx, uint64, error) {
	indexedTxBytes, err := vm.acceptedAtomicTxDB.Get(txID[:])
	if err != nil {
		return nil, 0, err
	}

	packer := wrappers.Packer{Bytes: indexedTxBytes}
	height := packer.UnpackLong()
	txBytes := packer.UnpackBytes()

	tx := &Tx{}
	if _, err := vm.codec.Unmarshal(txBytes, tx); err != nil {
		return nil, 0, fmt.Errorf("problem parsing atomic transaction from db: %w", err)
	}
	if err := tx.Sign(vm.codec, nil); err != nil {
		return nil, 0, fmt.Errorf("problem initializing atomic transaction from db: %w", err)
	}

	return tx, height, nil
}

// writeAtomicTx writes indexes [tx] in [blk]
func (vm *VM) writeAtomicTx(blk *Block, tx *Tx) error {
	// 8 bytes
	height := blk.ethBlock.NumberU64()
	// 4 + len(txBytes)
	txBytes := tx.Bytes()
	packer := wrappers.Packer{Bytes: make([]byte, 12+len(txBytes))}
	packer.PackLong(height)
	packer.PackBytes(txBytes)
	txID := tx.ID()

	return vm.acceptedAtomicTxDB.Put(txID[:], packer.Bytes)
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

// awaitSubmittedTxs waits for new transactions to be submitted
// and notifies the VM when the tx pool has transactions to be
// put into a new block.
func (vm *VM) awaitSubmittedTxs() {
	defer vm.shutdownWg.Done()
	vm.txSubmitChan = vm.chain.GetTxSubmitCh()
	for {
		select {
		case <-vm.txSubmitChan:
			log.Trace("New tx detected, trying to generate a block")
			vm.tryBlockGen()
		case <-vm.atomicTxSubmitChan:
			log.Trace("New atomic Tx detected, trying to generate a block")
			vm.tryBlockGen()
		case <-time.After(1 * time.Second):
			vm.tryBlockGen()
		case <-vm.shutdownChan:
			return
		}
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
// TODO switch to returning a list of private keys
// since there are no multisig inputs in Ethereum
func (vm *VM) GetSpendableFunds(keys []*crypto.PrivateKeySECP256K1R, assetID ids.ID, amount uint64) ([]EVMInput, [][]*crypto.PrivateKeySECP256K1R, error) {
	// NOTE: should we use HEAD block or lastAccepted?
	state, err := vm.chain.CurrentState()
	if err != nil {
		return nil, nil, err
	}
	inputs := []EVMInput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}
	// NOTE: we assume all keys correspond to distinct accounts here (so the
	// nonce handling in export_tx.go is correct)
	for _, key := range keys {
		if amount == 0 {
			break
		}
		addr := GetEthAddress(key)
		var balance uint64
		if assetID == vm.ctx.AVAXAssetID {
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
		nonce, err := vm.GetAcceptedNonce(addr)
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

// GetAcceptedNonce returns the nonce associated with the address at the last accepted block
func (vm *VM) GetAcceptedNonce(address common.Address) (uint64, error) {
	state, err := vm.chain.CurrentState()
	if err != nil {
		return 0, err
	}
	return state.GetNonce(address), nil
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
	return PublicKeyToEthAddress(privKey.PublicKey())
}

// PublicKeyToEthAddress returns the ethereum address derived from [pubKey]
func PublicKeyToEthAddress(pubKey crypto.PublicKey) common.Address {
	return ethcrypto.PubkeyToAddress(
		(*pubKey.(*crypto.PublicKeySECP256K1R).ToECDSA()))
}
