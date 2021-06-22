// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
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
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	avalancheRPC "github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versionabledb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/chain"
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
	Version = "coreth-v0.5.4"

	_ block.ChainVM = &VM{}
)

const (
	minBlockTime = 2 * time.Second
	maxBlockTime = 3 * time.Second
	// Max time from current time allowed for blocks, before they're considered future blocks
	// and fail verification
	maxFutureBlockTime   = 10 * time.Second
	batchSize            = 250
	maxUTXOsToFetch      = 1024
	defaultMempoolSize   = 1024
	codecVersion         = uint16(0)
	txFee                = units.MilliAvax
	secpFactoryCacheSize = 1024

	decidedCacheSize    = 100
	missingCacheSize    = 50
	unverifiedCacheSize = 50
)

var (
	// Set last accepted key to be longer than the keys used to store accepted block IDs.
	lastAcceptedKey        = []byte("last_accepted_key")
	acceptedPrefix         = []byte("snowman_accepted")
	ethDBPrefix            = []byte("ethdb")
	atomicTxPrefix         = []byte("atomicTxDB")
	pruneRejectedBlocksKey = []byte("pruned_rejected_blocks")
)

var (
	errEmptyBlock                 = errors.New("empty block")
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

// buildingBlkStatus denotes the current status of the VM in block production.
type buildingBlkStatus uint8

const (
	dontBuild buildingBlkStatus = iota
	conditionalBuild
	mayBuild
	building
)

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
	db *versionabledb.Database
	// [chaindb] is the database supplied to the Ethereum backend
	chaindb Database
	// [acceptedBlockDB] is the database to store the last accepted
	// block.
	acceptedBlockDB database.Database
	// [acceptedAtomicTxDB] maintains an index of accepted atomic txs.
	acceptedAtomicTxDB database.Database

	// A message is sent on this channel when a new block
	// is ready to be build. This notifies the consensus engine.
	notifyBuildBlockChan chan<- commonEng.Message

	// [buildBlockLock] must be held when accessing [buildStatus]
	buildBlockLock sync.Mutex
	// [buildBlockTimer] is a two stage timer handling block production.
	// Stage1 build a block if the batch size has been reached.
	// Stage2 build a block regardless of the size.
	buildBlockTimer *timer.Timer
	// buildStatus signals the phase of block building the VM is currently in.
	// [dontBuild] indicates there's no need to build a block.
	// [conditionalBuild] indicates build a block if the batch size has been reached.
	// [mayBuild] indicates the VM should proceed to build a block.
	// [building] indicates the VM has sent a request to the engine to build a block.
	buildStatus buildingBlkStatus

	baseCodec codec.Registry
	codec     codec.Manager
	clock     timer.Clock
	txFee     uint64
	mempool   *Mempool

	shutdownChan chan struct{}
	shutdownWg   sync.WaitGroup

	fx          secp256k1fx.Fx
	secpFactory crypto.FactorySECP256K1R

	// Continuous Profiler
	profiler profiler.ContinuousProfiler
}

func (vm *VM) Connected(id ids.ShortID) error {
	return nil // noop
}

func (vm *VM) Disconnected(id ids.ShortID) error {
	return nil // noop
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
	vm.config.SetDefaults()
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &vm.config); err != nil {
			return fmt.Errorf("failed to unmarshal config %s: %w", string(configBytes), err)
		}
	}

	if len(fxs) > 0 {
		return errUnsupportedFXs
	}

	vm.shutdownChan = make(chan struct{}, 1)
	vm.ctx = ctx
	vm.db = versionabledb.New(dbManager.Current().Database)
	vm.chaindb = Database{prefixdb.New(ethDBPrefix, vm.db)}
	vm.acceptedBlockDB = prefixdb.New(acceptedPrefix, vm.db)
	vm.acceptedAtomicTxDB = prefixdb.New(atomicTxPrefix, vm.db)
	g := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, g); err != nil {
		return err
	}

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

	ethConfig := ethconfig.NewDefaultConfig()
	ethConfig.Genesis = g

	// Set minimum gas price and launch goroutine to sleep until
	// network upgrade when the gas price must be changed
	var gasPriceUpdate func() // must call after coreth.NewETHChain to avoid race
	if g.Config.ApricotPhase1BlockTimestamp == nil {
		ethConfig.Miner.GasPrice = params.LaunchMinGasPrice
		ethConfig.GPO.Default = params.LaunchMinGasPrice
		ethConfig.TxPool.PriceLimit = params.LaunchMinGasPrice.Uint64()
	} else {
		apricotTime := time.Unix(g.Config.ApricotPhase1BlockTimestamp.Int64(), 0)
		log.Info(fmt.Sprintf("Apricot Upgrade Time %v.", apricotTime))
		if time.Now().Before(apricotTime) {
			untilApricot := time.Until(apricotTime)
			log.Info(fmt.Sprintf("Upgrade will occur in %v", untilApricot))
			ethConfig.Miner.GasPrice = params.LaunchMinGasPrice
			ethConfig.GPO.Default = params.LaunchMinGasPrice
			ethConfig.TxPool.PriceLimit = params.LaunchMinGasPrice.Uint64()
			gasPriceUpdate = func() {
				time.Sleep(untilApricot)
				vm.chain.SetGasPrice(params.ApricotPhase1MinGasPrice)
			}
		} else {
			ethConfig.Miner.GasPrice = params.ApricotPhase1MinGasPrice
			ethConfig.GPO.Default = params.ApricotPhase1MinGasPrice
			ethConfig.TxPool.PriceLimit = params.ApricotPhase1MinGasPrice.Uint64()
		}
	}

	// Set minimum price for mining and default gas price oracle value to the min
	// gas price to prevent so transactions and blocks all use the correct fees
	ethConfig.RPCGasCap = vm.config.RPCGasCap
	ethConfig.RPCTxFeeCap = vm.config.RPCTxFeeCap
	ethConfig.TxPool.NoLocals = !vm.config.LocalTxsEnabled
	ethConfig.AllowUnfinalizedQueries = vm.config.AllowUnfinalizedQueries
	ethConfig.Pruning = vm.config.Pruning
	vm.chainConfig = g.Config
	vm.networkID = ethConfig.NetworkId
	vm.secpFactory = crypto.FactorySECP256K1R{Cache: cache.LRU{Size: secpFactoryCacheSize}}

	nodecfg := node.Config{
		CorethVersion:         Version,
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
		// Leave [lastAcceptedHash] as the empty value to indicate the chain should be built from genesis.
	case lastAcceptedErr != nil:
		return fmt.Errorf("failed to get last accepted block ID due to: %w", lastAcceptedErr)
	case len(lastAcceptedBytes) != common.HashLength:
		return fmt.Errorf("last accepted bytes should have been length %d, but found %d", common.HashLength, len(lastAcceptedBytes))
	default:
		lastAcceptedHash = common.BytesToHash(lastAcceptedBytes)
	}
	ethChain, err := coreth.NewETHChain(&ethConfig, &nodecfg, vm.chaindb, vm.config.EthBackendSettings(), lastAcceptedHash)
	if err != nil {
		return err
	}
	vm.chain = ethChain
	lastAccepted := vm.chain.LastAcceptedBlock()

	// Kickoff gasPriceUpdate goroutine once the backend is initialized, if it
	// exists
	if gasPriceUpdate != nil {
		go gasPriceUpdate()
	}

	vm.chain.SetOnFinalizeAndAssemble(func(header *types.Header, state *state.StateDB, txs []*types.Transaction) ([]byte, error) {
		snapshot := state.Snapshot()
		for {
			tx, exists := vm.mempool.NextTx()
			if !exists {
				break
			}
			if err := tx.UnsignedAtomicTx.EVMStateTransfer(vm, state); err != nil {
				// Discard the transaction from the mempool on failed verification.
				vm.mempool.DiscardCurrentTx()
				state.RevertToSnapshot(snapshot)
				continue
			}
			rules := vm.chainConfig.AvalancheRules(header.Number, new(big.Int).SetUint64(header.Time))
			parentIntf, err := vm.GetBlockInternal(ids.ID(header.ParentHash))
			if err != nil {
				// Discard the transaction from the mempool on failed verification.
				vm.mempool.DiscardCurrentTx()
				return nil, fmt.Errorf("failed to get parent block: %w", err)
			}
			parent, ok := parentIntf.(*Block)
			if !ok {
				// Discard the transaction from the mempool on failed verification.
				vm.mempool.DiscardCurrentTx()
				return nil, fmt.Errorf("parent block %s had unexpected type %T", parentIntf.ID(), parentIntf)
			}

			if err := tx.UnsignedAtomicTx.SemanticVerify(vm, tx, parent, rules); err != nil {
				// Discard the transaction from the mempool on failed verification.
				vm.mempool.DiscardCurrentTx()
				state.RevertToSnapshot(snapshot)
				continue
			}

			atomicTxBytes, err := vm.codec.Marshal(codecVersion, tx)
			if err != nil {
				// Discard the transaction from the mempool and error if the transaction
				// cannot be marshalled. This should never happen.
				vm.mempool.DiscardCurrentTx()
				return nil, fmt.Errorf("failed to marshal atomic transaction %s due to %w", tx.ID(), err)
			}
			return atomicTxBytes, nil
		}

		if len(txs) == 0 {
			// this could happen due to the async logic of geth tx pool
			return nil, errEmptyBlock
		}

		return nil, nil
	})
	vm.chain.SetOnExtraStateChange(func(block *types.Block, state *state.StateDB) error {
		tx, err := vm.extractAtomicTx(block)
		if err != nil {
			return err
		}
		if tx == nil {
			return nil
		}
		return tx.UnsignedAtomicTx.EVMStateTransfer(vm, state)
	})
	vm.notifyBuildBlockChan = toEngine

	// buildBlockTimer handles passing PendingTxs messages to the consensus engine.
	vm.buildBlockTimer = timer.NewStagedTimer(vm.buildBlockTwoStageTimer)
	vm.buildStatus = dontBuild
	go ctx.Log.RecoverAndPanic(vm.buildBlockTimer.Dispatch)

	// TODO: read size from settings
	vm.mempool = NewMempool(defaultMempoolSize)
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
		GetBlockIDAtHeight: vm.getBlockIDAtHeight,
		GetBlock:           vm.getBlock,
		UnmarshalBlock:     vm.parseBlock,
		BuildBlock:         vm.buildBlock,
	})

	vm.shutdownWg.Add(1)
	go vm.ctx.Log.RecoverAndPanic(vm.awaitSubmittedTxs)
	vm.codec = Codec

	go vm.ctx.Log.RecoverAndPanic(vm.startContinuousProfiler)

	// The Codec explicitly registers the types it requires from the secp256k1fx
	// so [vm.baseCodec] is a dummy codec use to fulfill the secp256k1fx VM
	// interface. The fx will register all of its types, which can be safely
	// ignored by the VM's codec.
	vm.baseCodec = linearcodec.NewDefault()

	if err := vm.pruneChain(); err != nil {
		return err
	}

	return vm.fx.Initialize(vm)
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
	if err := vm.db.Put(pruneRejectedBlocksKey, heightBytes); err == nil {
		return nil
	} else {
		return fmt.Errorf("failed to write pruned rejected blocks to db: %w", err)
	}
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

// buildBlock builds a block to be wrapped by ChainState
func (vm *VM) buildBlock() (snowman.Block, error) {
	block, err := vm.chain.GenerateBlock()
	// Set the buildStatus before calling Cancel or Issue on
	// the mempool and after generating the block.
	// This prevents [needToBuild] from returning true when the
	// produced block will change whether or not we need to produce
	// another block and also ensures that when the mempool adds a
	// new item to Pending it will be handled appropriately by [signalTxsReady]
	vm.buildBlockLock.Lock()
	if vm.needToBuild() {
		vm.buildStatus = conditionalBuild
		vm.buildBlockTimer.SetTimeoutIn(minBlockTime)
	} else {
		vm.buildStatus = dontBuild
	}
	vm.buildBlockLock.Unlock()

	if err != nil {
		vm.mempool.CancelCurrentTx()
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
	// TODO cache verification since Verify() will be called by the
	// consensus engine as well.
	// Note: this is only called when building a new block, so caching
	// verification will only be a significant optimization for nodes
	// that produce a large number of blocks.
	if err := blk.Verify(); err != nil {
		vm.mempool.CancelCurrentTx()
		return nil, fmt.Errorf("block failed verification due to: %w", err)
	}

	log.Debug(fmt.Sprintf("Built block %s", blk.ID()))
	// Marks the current tx from the mempool as being successfully issued
	// into a block.
	vm.mempool.IssueCurrentTx()
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

// getBlockIDAtHeight retrieves the blkID of the canonical block at [blkHeight]
// if [blkHeight] is less than the height of the last accepted block, this will return
// a canonical block. Otherwise, it may return a blkID that has not yet been accepted.
func (vm *VM) getBlockIDAtHeight(blkHeight uint64) (ids.ID, error) {
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
	vm.chain.AttachEthService(handler, enabledAPIs)

	errs := wrappers.Errs{}
	if vm.config.SnowmanAPIEnabled {
		errs.Add(handler.RegisterName("snowman", &SnowmanAPI{vm}))
		enabledAPIs = append(enabledAPIs, "snowman")
	}
	if vm.config.CorethAdminAPIEnabled {
		primaryAlias, err := vm.ctx.BCLookup.PrimaryAlias(vm.ctx.ChainID)
		if err != nil {
			return nil, fmt.Errorf("failed to get primary alias for chain due to %w", err)
		}
		errs.Add(handler.RegisterName("performance", NewPerformanceService(fmt.Sprintf("coreth_performance_%s", primaryAlias))))
		enabledAPIs = append(enabledAPIs, "coreth-admin")
	}
	if vm.config.NetAPIEnabled {
		errs.Add(handler.RegisterName("net", &NetAPI{vm}))
		enabledAPIs = append(enabledAPIs, "net")
	}
	if vm.config.Web3APIEnabled {
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
// extractAtomicTx returns the atomic transaction in [block] if
// one exists.
func (vm *VM) extractAtomicTx(block *types.Block) (*Tx, error) {
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

func (vm *VM) conflicts(inputs ids.Set, ancestor *Block) error {
	for ancestor.Status() != choices.Accepted {
		atx, err := vm.extractAtomicTx(ancestor.ethBlock)
		if err != nil {
			return fmt.Errorf("problem parsing atomic tx of ancestor block %s: %w", ancestor.ID(), err)
		}
		// If the ancestor isn't an atomic block, it can't conflict with
		// the import tx.
		if atx != nil {
			ancestorInputs := atx.UnsignedAtomicTx.InputUTXOs()
			if inputs.Overlaps(ancestorInputs) {
				return errConflictingAtomicInputs
			}
		}

		// Move up the chain.
		nextAncestorIntf := ancestor.Parent()
		// If the ancestor is unknown, then the parent failed
		// verification when it was called.
		// If the ancestor is rejected, then this block shouldn't be
		// inserted into the canonical chain because the parent is
		// will be missing.
		// If the ancestor is processing, then the block may have
		// been verified.
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

// getAcceptedAtomicTx attempts to get [txID] from the database.
func (vm *VM) getAcceptedAtomicTx(txID ids.ID) (*Tx, uint64, error) {
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

// getAtomicTx returns the requested transaction, status, and height.
// If the status is Unknown, then the returned transaction will be nil.
func (vm *VM) getAtomicTx(txID ids.ID) (*Tx, Status, uint64, error) {
	if tx, height, err := vm.getAcceptedAtomicTx(txID); err == nil {
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

// needToBuild returns true if there are outstanding transactions to be issued
// into a block.
func (vm *VM) needToBuild() bool {
	size, err := vm.chain.PendingSize()
	if err != nil {
		log.Error("Failed to get chain pending size", "error", err)
		return false
	}
	return size > 0 || vm.mempool.Len() > 0
}

// buildEarly returns true if there are sufficient outstanding transactions to
// be issued into a block to build a block early.
func (vm *VM) buildEarly() bool {
	size, err := vm.chain.PendingSize()
	if err != nil {
		log.Error("Failed to get chain pending size", "error", err)
		return false
	}
	return size > batchSize || vm.mempool.Len() > 1
}

// buildBlockTwoStageTimer is a two stage timer that sends a notification
// to the engine when the VM is ready to build a block.
// If it should be called back again, it returns the timeout duration at
// which it should be called again.
func (vm *VM) buildBlockTwoStageTimer() (time.Duration, bool) {
	vm.buildBlockLock.Lock()
	defer vm.buildBlockLock.Unlock()

	switch vm.buildStatus {
	case dontBuild:
		return 0, false
	case conditionalBuild:
		if !vm.buildEarly() {
			vm.buildStatus = mayBuild
			return (maxBlockTime - minBlockTime), true
		}
	case mayBuild:
	case building:
		// If the status has already been set to building, there is no need
		// to send an additional request to the consensus engine until the call
		// to BuildBlock resets the block status.
		return 0, false
	default:
		// Log an error if an invalid status is found.
		log.Error("Found invalid build status in build block timer", "buildStatus", vm.buildStatus)
	}

	select {
	case vm.notifyBuildBlockChan <- commonEng.PendingTxs:
		vm.buildStatus = building
	default:
		log.Error("Failed to push PendingTxs notification to the consensus engine.")
	}

	// No need for the timeout to fire again until BuildBlock is called.
	return 0, false
}

// signalTxsReady sets the initial timeout on the two stage timer if the process
// has not already begun from an earlier notification. If [buildStatus] is anything
// other than [dontBuild], then the attempt has already begun and this notification
// can be safely skipped.
func (vm *VM) signalTxsReady() {
	vm.buildBlockLock.Lock()
	defer vm.buildBlockLock.Unlock()

	// Set the build block timer in motion if it has not been started.
	if vm.buildStatus == dontBuild {
		vm.buildStatus = conditionalBuild
		vm.buildBlockTimer.SetTimeoutIn(minBlockTime)
	}
}

// awaitSubmittedTxs waits for new transactions to be submitted
// and notifies the VM when the tx pool has transactions to be
// put into a new block.
func (vm *VM) awaitSubmittedTxs() {
	defer vm.shutdownWg.Done()
	txSubmitChan := vm.chain.GetTxSubmitCh()
	for {
		select {
		case <-txSubmitChan:
			log.Trace("New tx detected, trying to generate a block")
			vm.signalTxsReady()
		case <-vm.mempool.Pending:
			log.Trace("New atomic Tx detected, trying to generate a block")
			vm.signalTxsReady()
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

// issueTx adds [tx] to the mempool and signals the goroutine waiting on
// atomic transactions that there is an atomic transaction ready to be
// put into a block.
func (vm *VM) issueTx(tx *Tx) error {
	return vm.mempool.AddTx(tx)
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
