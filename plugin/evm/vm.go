// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/go-ethereum/common"
	ethcrypto "github.com/ava-labs/go-ethereum/crypto"
	"github.com/ava-labs/go-ethereum/rlp"
	"github.com/ava-labs/go-ethereum/rpc"
	geckorpc "github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanche-go/api/admin"
	"github.com/ava-labs/avalanche-go/cache"
	"github.com/ava-labs/avalanche-go/database"
	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/snow"
	"github.com/ava-labs/avalanche-go/snow/choices"
	"github.com/ava-labs/avalanche-go/snow/consensus/snowman"
	"github.com/ava-labs/avalanche-go/utils/codec"
	"github.com/ava-labs/avalanche-go/utils/constants"
	"github.com/ava-labs/avalanche-go/utils/crypto"
	"github.com/ava-labs/avalanche-go/utils/formatting"
	geckojson "github.com/ava-labs/avalanche-go/utils/json"
	"github.com/ava-labs/avalanche-go/utils/logging"
	"github.com/ava-labs/avalanche-go/utils/timer"
	"github.com/ava-labs/avalanche-go/utils/units"
	"github.com/ava-labs/avalanche-go/utils/wrappers"
	"github.com/ava-labs/avalanche-go/vms/components/avax"
	"github.com/ava-labs/avalanche-go/vms/secp256k1fx"

	commonEng "github.com/ava-labs/avalanche-go/snow/engine/common"
)

var (
	zeroAddr = common.Address{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	}
	x2cRate = big.NewInt(1000000000)
)

const (
	lastAcceptedKey = "snowman_lastAccepted"
)

const (
	minBlockTime    = 250 * time.Millisecond
	maxBlockTime    = 1000 * time.Millisecond
	batchSize       = 250
	maxUTXOsToFetch = 1024
	blockCacheSize  = 1 << 17 // 131072
)

const (
	bdTimerStateMin = iota
	bdTimerStateMax
	bdTimerStateLong
)

const (
	addressSep = "-"
)

var (
	// minGasPrice is the number of nAVAX required per gas unit for a transaction
	// to be valid, measured in wei
	minGasPrice = big.NewInt(47 * params.GWei)

	txFee = units.MilliAvax

	errEmptyBlock                 = errors.New("empty block")
	errCreateBlock                = errors.New("couldn't create block")
	errUnknownBlock               = errors.New("unknown block")
	errBlockFrequency             = errors.New("too frequent block issuance")
	errUnsupportedFXs             = errors.New("unsupported feature extensions")
	errInvalidBlock               = errors.New("invalid block")
	errInvalidAddr                = errors.New("invalid hex address")
	errTooManyAtomicTx            = errors.New("too many pending atomic txs")
	errAssetIDMismatch            = errors.New("asset IDs in the input don't match the utxo")
	errWrongNumberOfCredentials   = errors.New("should have the same number of credentials as inputs")
	errNoInputs                   = errors.New("tx has no inputs")
	errNoImportInputs             = errors.New("tx has no imported inputs")
	errInputsNotSortedUnique      = errors.New("inputs not sorted and unique")
	errPublicKeySignatureMismatch = errors.New("signature doesn't match public key")
	errUnknownAsset               = errors.New("unknown asset ID")
	errNoFunds                    = errors.New("no spendable funds were found")
	errWrongChainID               = errors.New("tx has wrong chain ID")
	errInsufficientFunds          = errors.New("insufficient funds")
	errNoExportOutputs            = errors.New("no export outputs")
	errOutputsNotSorted           = errors.New("outputs not sorted")
	errOverflowExport             = errors.New("overflow when computing export amount + txFee")
	errInvalidNonce               = errors.New("invalid nonce")
)

func maxDuration(x, y time.Duration) time.Duration {
	if x > y {
		return x
	}
	return y
}

// Codec does serialization and deserialization
var Codec codec.Codec

func init() {
	Codec = codec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		Codec.RegisterType(&UnsignedImportTx{}),
		Codec.RegisterType(&UnsignedExportTx{}),
	)
	Codec.Skip(3)
	errs.Add(
		Codec.RegisterType(&secp256k1fx.TransferInput{}),
		Codec.RegisterType(&secp256k1fx.MintOutput{}),
		Codec.RegisterType(&secp256k1fx.TransferOutput{}),
		Codec.RegisterType(&secp256k1fx.MintOperation{}),
		Codec.RegisterType(&secp256k1fx.Credential{}),
		Codec.RegisterType(&secp256k1fx.Input{}),
		Codec.RegisterType(&secp256k1fx.OutputOwners{}),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// VM implements the snowman.ChainVM interface
type VM struct {
	ctx *snow.Context

	chainID           *big.Int
	networkID         uint64
	genesisHash       common.Hash
	chain             *coreth.ETHChain
	chaindb           Database
	newBlockChan      chan *Block
	networkChan       chan<- commonEng.Message
	newTxPoolHeadChan chan core.NewTxPoolHeadEvent

	txPoolStabilizedHead common.Hash
	txPoolStabilizedOk   chan struct{}
	txPoolStabilizedLock sync.Mutex

	metalock                     sync.Mutex
	blockCache, blockStatusCache cache.LRU
	lastAccepted                 *Block
	writingMetadata              uint32

	bdlock          sync.Mutex
	blockDelayTimer *timer.Timer
	bdTimerState    int8
	bdGenWaitFlag   bool
	bdGenFlag       bool

	genlock               sync.Mutex
	txSubmitChan          <-chan struct{}
	atomicTxSubmitChan    chan struct{}
	codec                 codec.Codec
	clock                 timer.Clock
	txFee                 uint64
	pendingAtomicTxs      chan *Tx
	blockAtomicInputCache cache.LRU

	fx secp256k1fx.Fx
}

func (vm *VM) getAtomicTx(block *types.Block) *Tx {
	extdata := block.ExtraData()
	atx := new(Tx)
	if err := vm.codec.Unmarshal(extdata, atx); err != nil {
		return nil
	}
	atx.Sign(vm.codec, nil)
	return atx
}

// Codec implements the secp256k1fx interface
func (vm *VM) Codec() codec.Codec { return codec.NewDefault() }

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
	if len(fxs) > 0 {
		return errUnsupportedFXs
	}

	vm.ctx = ctx
	vm.chaindb = Database{db}
	g := new(core.Genesis)
	err := json.Unmarshal(b, g)
	if err != nil {
		return err
	}

	vm.chainID = g.Config.ChainID
	vm.txFee = txFee

	config := eth.DefaultConfig
	config.ManualCanonical = true
	config.Genesis = g
	config.Miner.ManualMining = true
	config.Miner.DisableUncle = true

	// Set minimum price for mining and default gas price oracle value to the min
	// gas price to prevent so transactions and blocks all use the correct fees
	config.Miner.GasPrice = minGasPrice
	config.GPO.Default = minGasPrice
	config.TxPool.PriceLimit = minGasPrice.Uint64()

	if err := config.SetGCMode("archive"); err != nil {
		panic(err)
	}
	nodecfg := node.Config{NoUSB: true}
	chain := coreth.NewETHChain(&config, &nodecfg, nil, vm.chaindb)
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
			if err := atx.UnsignedTx.(UnsignedAtomicTx).EVMStateTransfer(state); err != nil {
				vm.newBlockChan <- nil
				return nil, err
			}
			raw, _ := vm.codec.Marshal(atx)
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
		vm.ctx.Log.Verbo("EVM sealed a block")

		blk := &Block{
			id:       ids.NewID(block.Hash()),
			ethBlock: block,
			vm:       vm,
		}
		if blk.Verify() != nil {
			return errInvalidBlock
		}
		vm.newBlockChan <- blk
		vm.updateStatus(ids.NewID(block.Hash()), choices.Processing)
		vm.txPoolStabilizedLock.Lock()
		vm.txPoolStabilizedHead = block.Hash()
		vm.txPoolStabilizedLock.Unlock()
		return nil
	})
	chain.SetOnQueryAcceptedBlock(func() *types.Block {
		return vm.getLastAccepted().ethBlock
	})
	chain.SetOnExtraStateChange(func(block *types.Block, state *state.StateDB) error {
		tx := vm.getAtomicTx(block)
		if tx == nil {
			return nil
		}
		return tx.UnsignedTx.(UnsignedAtomicTx).EVMStateTransfer(state)
	})
	vm.blockCache = cache.LRU{Size: blockCacheSize}
	vm.blockStatusCache = cache.LRU{Size: blockCacheSize}
	vm.blockAtomicInputCache = cache.LRU{Size: blockCacheSize}
	vm.newBlockChan = make(chan *Block)
	vm.networkChan = toEngine
	vm.blockDelayTimer = timer.NewTimer(func() {
		vm.bdlock.Lock()
		switch vm.bdTimerState {
		case bdTimerStateMin:
			vm.bdTimerState = bdTimerStateMax
			vm.blockDelayTimer.SetTimeoutIn(maxDuration(maxBlockTime-minBlockTime, 0))
		case bdTimerStateMax:
			vm.bdTimerState = bdTimerStateLong
		}
		tryAgain := vm.bdGenWaitFlag
		vm.bdlock.Unlock()
		if tryAgain {
			vm.tryBlockGen()
		}
	})
	go ctx.Log.RecoverAndPanic(vm.blockDelayTimer.Dispatch)

	vm.bdTimerState = bdTimerStateLong
	vm.bdGenWaitFlag = true
	vm.newTxPoolHeadChan = make(chan core.NewTxPoolHeadEvent, 1)
	vm.txPoolStabilizedOk = make(chan struct{}, 1)
	// TODO: read size from options
	vm.pendingAtomicTxs = make(chan *Tx, 1024)
	vm.atomicTxSubmitChan = make(chan struct{}, 1)
	chain.GetTxPool().SubscribeNewHeadEvent(vm.newTxPoolHeadChan)
	// TODO: shutdown this go routine
	go ctx.Log.RecoverAndPanic(func() {
		for {
			select {
			case h := <-vm.newTxPoolHeadChan:
				vm.txPoolStabilizedLock.Lock()
				if vm.txPoolStabilizedHead == h.Block.Hash() {
					vm.txPoolStabilizedOk <- struct{}{}
					vm.txPoolStabilizedHead = common.Hash{}
				}
				vm.txPoolStabilizedLock.Unlock()
			}
		}
	})
	chain.Start()

	var lastAccepted *types.Block
	if b, err := vm.chaindb.Get([]byte(lastAcceptedKey)); err == nil {
		var hash common.Hash
		if err = rlp.DecodeBytes(b, &hash); err == nil {
			if block := chain.GetBlockByHash(hash); block == nil {
				vm.ctx.Log.Debug("lastAccepted block not found in chaindb")
			} else {
				lastAccepted = block
			}
		}
	}
	if lastAccepted == nil {
		vm.ctx.Log.Debug("lastAccepted is unavailable, setting to the genesis block")
		lastAccepted = chain.GetGenesisBlock()
	}
	vm.lastAccepted = &Block{
		id:       ids.NewID(lastAccepted.Hash()),
		ethBlock: lastAccepted,
		vm:       vm,
	}
	vm.genesisHash = chain.GetGenesisBlock().Hash()
	vm.ctx.Log.Info(fmt.Sprintf("lastAccepted = %s", vm.lastAccepted.ethBlock.Hash().Hex()))

	// TODO: shutdown this go routine
	go vm.ctx.Log.RecoverAndPanic(func() {
		vm.txSubmitChan = vm.chain.GetTxSubmitCh()
		for {
			select {
			case <-vm.txSubmitChan:
				vm.ctx.Log.Verbo("New tx detected, trying to generate a block")
				vm.tryBlockGen()
			case <-vm.atomicTxSubmitChan:
				vm.ctx.Log.Verbo("New atomic Tx detected, trying to generate a block")
				vm.tryBlockGen()
			case <-time.After(5 * time.Second):
				vm.tryBlockGen()
			}
		}
	})
	vm.codec = Codec

	return vm.fx.Initialize(vm)
}

// Bootstrapping notifies this VM that the consensus engine is performing
// bootstrapping
func (vm *VM) Bootstrapping() error { return vm.fx.Bootstrapping() }

// Bootstrapped notifies this VM that the consensus engine has finished
// bootstrapping
func (vm *VM) Bootstrapped() error { return vm.fx.Bootstrapped() }

// Shutdown implements the snowman.ChainVM interface
func (vm *VM) Shutdown() error {
	if vm.ctx == nil {
		return nil
	}

	vm.writeBackMetadata()
	vm.chain.Stop()
	return nil
}

// BuildBlock implements the snowman.ChainVM interface
func (vm *VM) BuildBlock() (snowman.Block, error) {
	vm.chain.GenBlock()
	block := <-vm.newBlockChan
	if block == nil {
		return nil, errCreateBlock
	}
	// reset the min block time timer
	vm.bdlock.Lock()
	vm.bdTimerState = bdTimerStateMin
	vm.bdGenWaitFlag = false
	vm.bdGenFlag = false
	vm.blockDelayTimer.SetTimeoutIn(minBlockTime)
	vm.bdlock.Unlock()

	vm.ctx.Log.Debug("built block 0x%x", block.ID().Bytes())
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
	if !vm.chain.VerifyBlock(ethBlock) {
		return nil, errInvalidBlock
	}
	blockHash := ethBlock.Hash()
	// Coinbase must be zero on C-Chain
	if bytes.Compare(blockHash.Bytes(), vm.genesisHash.Bytes()) != 0 &&
		bytes.Compare(ethBlock.Coinbase().Bytes(), coreth.BlackholeAddr.Bytes()) != 0 {
		return nil, errInvalidBlock
	}
	block := &Block{
		id:       ids.NewID(blockHash),
		ethBlock: ethBlock,
		vm:       vm,
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
func (vm *VM) SetPreference(blkID ids.ID) {
	err := vm.chain.SetTail(blkID.Key())
	vm.ctx.Log.AssertNoError(err)
}

// LastAccepted returns the ID of the block that was last accepted
func (vm *VM) LastAccepted() ids.ID {
	vm.metalock.Lock()
	defer vm.metalock.Unlock()

	return vm.lastAccepted.ID()
}

// NewHandler returns a new Handler for a service where:
//   * The handler's functionality is defined by [service]
//     [service] should be a gorilla RPC service (see https://www.gorillatoolkit.org/pkg/rpc/v2)
//   * The name of the service is [name]
//   * The LockOption is the first element of [lockOption]
//     By default the LockOption is WriteLock
//     [lockOption] should have either 0 or 1 elements. Elements beside the first are ignored.
func newHandler(name string, service interface{}, lockOption ...commonEng.LockOption) *commonEng.HTTPHandler {
	server := geckorpc.NewServer()
	server.RegisterCodec(geckojson.NewCodec(), "application/json")
	server.RegisterCodec(geckojson.NewCodec(), "application/json;charset=UTF-8")
	server.RegisterService(service, name)

	var lock commonEng.LockOption = commonEng.WriteLock
	if len(lockOption) != 0 {
		lock = lockOption[0]
	}
	return &commonEng.HTTPHandler{LockOptions: lock, Handler: server}
}

// CreateHandlers makes new http handlers that can handle API calls
func (vm *VM) CreateHandlers() map[string]*commonEng.HTTPHandler {
	handler := vm.chain.NewRPCHandler()
	vm.chain.AttachEthService(handler, []string{"eth", "personal", "txpool"})
	handler.RegisterName("net", &NetAPI{vm})
	handler.RegisterName("snowman", &SnowmanAPI{vm})
	handler.RegisterName("web3", &Web3API{})
	handler.RegisterName("debug", &DebugAPI{vm})
	handler.RegisterName("admin", &admin.Performance{})

	return map[string]*commonEng.HTTPHandler{
		"/rpc":  {LockOptions: commonEng.NoLock, Handler: handler},
		"/avax": newHandler("avax", &AvaxAPI{vm}),
		"/ws":   {LockOptions: commonEng.NoLock, Handler: handler.WebsocketHandler([]string{"*"})},
	}
}

// CreateStaticHandlers makes new http handlers that can handle API calls
func (vm *VM) CreateStaticHandlers() map[string]*commonEng.HTTPHandler {
	handler := rpc.NewServer()
	handler.RegisterName("static", &StaticService{})
	return map[string]*commonEng.HTTPHandler{
		"/rpc": {LockOptions: commonEng.NoLock, Handler: handler},
		"/ws":  {LockOptions: commonEng.NoLock, Handler: handler.WebsocketHandler([]string{"*"})},
	}
}

/*
 ******************************************************************************
 *********************************** Helpers **********************************
 ******************************************************************************
 */

func (vm *VM) updateStatus(blockID ids.ID, status choices.Status) {
	vm.metalock.Lock()
	defer vm.metalock.Unlock()

	if status == choices.Accepted {
		vm.lastAccepted = vm.getBlock(blockID)
		// TODO: improve this naive implementation
		if atomic.SwapUint32(&vm.writingMetadata, 1) == 0 {
			go vm.ctx.Log.RecoverAndPanic(vm.writeBackMetadata)
		}
	}
	vm.blockStatusCache.Put(blockID, status)
}

func (vm *VM) tryBlockGen() error {
	vm.bdlock.Lock()
	defer vm.bdlock.Unlock()
	if vm.bdGenFlag {
		// skip if one call already generates a block in this round
		return nil
	}
	vm.bdGenWaitFlag = true

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

	switch vm.bdTimerState {
	case bdTimerStateMin:
		return nil
	case bdTimerStateMax:
		if size < batchSize {
			return nil
		}
	case bdTimerStateLong:
		// timeout; go ahead and generate a new block anyway
	}
	select {
	case vm.networkChan <- commonEng.PendingTxs:
		// successfully push out the notification; this round ends
		vm.bdGenFlag = true
	default:
		return errBlockFrequency
	}
	return nil
}

func (vm *VM) getCachedStatus(blockID ids.ID) choices.Status {
	vm.metalock.Lock()
	defer vm.metalock.Unlock()
	status := choices.Processing

	if statusIntf, ok := vm.blockStatusCache.Get(blockID); ok {
		status = statusIntf.(choices.Status)
	} else {
		wrappedBlk := vm.getBlock(blockID)
		if wrappedBlk == nil {
			return choices.Unknown
		}
		blk := wrappedBlk.ethBlock
		acceptedBlk := vm.lastAccepted.ethBlock

		// TODO: There must be a better way of doing this.
		// Traverse up the chain from the lower block until the indices match
		highBlock := blk
		lowBlock := acceptedBlk
		if highBlock.Number().Cmp(lowBlock.Number()) < 0 {
			highBlock, lowBlock = lowBlock, highBlock
		}
		for highBlock.Number().Cmp(lowBlock.Number()) > 0 {
			parentBlock := vm.getBlock(ids.NewID(highBlock.ParentHash()))
			if parentBlock == nil {
				vm.blockStatusCache.Put(blockID, choices.Processing)
				return choices.Processing
			}
			highBlock = parentBlock.ethBlock
		}

		if highBlock.Hash() == lowBlock.Hash() { // on the same branch
			if blk.Number().Cmp(acceptedBlk.Number()) <= 0 {
				status = choices.Accepted
			}
		} else { // on different branches
			status = choices.Rejected
		}
	}

	vm.blockStatusCache.Put(blockID, status)
	return status
}

func (vm *VM) getBlock(id ids.ID) *Block {
	if blockIntf, ok := vm.blockCache.Get(id); ok {
		return blockIntf.(*Block)
	}
	ethBlock := vm.chain.GetBlockByHash(id.Key())
	if ethBlock == nil {
		return nil
	}
	block := &Block{
		id:       ids.NewID(ethBlock.Hash()),
		ethBlock: ethBlock,
		vm:       vm,
	}
	vm.blockCache.Put(id, block)
	return block
}

func (vm *VM) issueRemoteTxs(txs []*types.Transaction) error {
	errs := vm.chain.AddRemoteTxs(txs)
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return vm.tryBlockGen()
}

func (vm *VM) writeBackMetadata() {
	vm.metalock.Lock()
	defer vm.metalock.Unlock()

	b, err := rlp.EncodeToBytes(vm.lastAccepted.ethBlock.Hash())
	if err != nil {
		vm.ctx.Log.Error("snowman-eth: error while writing back metadata")
		return
	}
	vm.ctx.Log.Debug("writing back metadata")
	vm.chaindb.Put([]byte(lastAcceptedKey), b)
	atomic.StoreUint32(&vm.writingMetadata, 0)
}

func (vm *VM) getLastAccepted() *Block {
	vm.metalock.Lock()
	defer vm.metalock.Unlock()

	return vm.lastAccepted
}

func (vm *VM) ParseEthAddress(addrStr string) (common.Address, error) {
	if !common.IsHexAddress(addrStr) {
		return common.Address{}, errInvalidAddr
	}
	return common.HexToAddress(addrStr), nil
}

func (vm *VM) FormatEthAddress(addr common.Address) (string, error) {
	return addr.Hex(), nil
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
		startUTXOID.Bytes(),
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
		if err := vm.codec.Unmarshal(utxoBytes, utxo); err != nil {
			return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("error parsing UTXO: %w", err)
		}
		utxos[i] = utxo
	}
	return utxos, lastAddrID, lastUTXOID, nil
}

func GetEthAddress(privKey *crypto.PrivateKeySECP256K1R) common.Address {
	return PublicKeyToEthAddress(privKey.PublicKey())
}

func PublicKeyToEthAddress(pubKey crypto.PublicKey) common.Address {
	return ethcrypto.PubkeyToAddress(
		(*pubKey.(*crypto.PublicKeySECP256K1R).ToECDSA()))
}

func (vm *VM) GetSpendableCanonical(keys []*crypto.PrivateKeySECP256K1R, amount uint64) ([]EVMInput, [][]*crypto.PrivateKeySECP256K1R, error) {
	// NOTE: should we use HEAD block or lastAccepted?
	state, err := vm.chain.BlockState(vm.lastAccepted.ethBlock)
	if err != nil {
		return nil, nil, err
	}
	inputs := []EVMInput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}
	for _, key := range keys {
		if amount == 0 {
			break
		}
		addr := GetEthAddress(key)
		balance := new(big.Int).Div(state.GetBalance(addr), x2cRate).Uint64()
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

func (vm *VM) GetAcceptedNonce(address common.Address) (uint64, error) {
	state, err := vm.chain.BlockState(vm.lastAccepted.ethBlock)
	if err != nil {
		return 0, err
	}
	return state.GetNonce(address), nil
}
