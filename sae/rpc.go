// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"sync"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/version"
	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/accounts"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/eth/filters"
	"github.com/ava-labs/libevm/eth/tracers"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/libevm/debug"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/saexec"
	"github.com/ava-labs/strevm/txgossip"
)

// APIBackend is the union of all interfaces required to implement the SAE APIs.
type APIBackend interface {
	ethapi.Backend
	// TODO(ceyonur): Add gasprice.Backend interface.
	tracers.Backend
	filters.BloomOverrider
}

// APIBackend returns an API backend backed by the [VM].
func (vm *VM) APIBackend() APIBackend {
	return vm.apiBackend
}

func (vm *VM) ethRPCServer() (*rpc.Server, error) {
	b := vm.apiBackend

	filterSystem := filters.NewFilterSystem(b, filters.Config{})
	filterAPI := filters.NewFilterAPI(filterSystem, false /*isLightClient*/)
	vm.toClose = append(vm.toClose, closerFunc(func() error {
		filters.CloseAPI(filterAPI)
		return nil
	}))

	type api struct {
		namespace string
		api       any
	}

	// Standard Ethereum APIs are documented at: https://ethereum.org/developers/docs/apis/json-rpc
	// Geth-specific APIs are documented at: https://geth.ethereum.org/docs/interacting-with-geth/rpc
	apis := []api{
		// Standard Ethereum node APIs:
		// - web3_clientVersion
		// - web3_sha3
		{"web3", newWeb3API()},
		// Standard Ethereum node APIs:
		// - net_listening
		// - net_peerCount
		// - net_version
		{"net", newNetAPI(vm.peers, vm.exec.ChainConfig().ChainID.Uint64())},
		// Geth-specific APIs:
		// - txpool_content
		// - txpool_contentFrom
		// - txpool_inspect
		// - txpool_status
		{"txpool", ethapi.NewTxPoolAPI(b)},
		// Standard Ethereum node APIs:
		// - eth_syncing
		{"eth", ethapi.NewEthereumAPI(b)},
		// Standard Ethereum node APIs:
		// - eth_blockNumber
		// - eth_chainId
		// - eth_getBlockByHash
		// - eth_getBlockByNumber
		// - eth_getBlockReceipts
		// - eth_getUncleByBlockHashAndIndex
		// - eth_getUncleByBlockNumberAndIndex
		// - eth_getUncleCountByBlockHash
		// - eth_getUncleCountByBlockNumber
		//
		// Geth-specific APIs:
		// - eth_getHeaderByHash
		// - eth_getHeaderByNumber
		{"eth", &blockChainAPI{ethapi.NewBlockChainAPI(b), b}},
		// Standard Ethereum node APIs:
		// - eth_getBlockTransactionCountByHash
		// - eth_getBlockTransactionCountByNumber
		// - eth_getTransactionByBlockHashAndIndex
		// - eth_getTransactionByBlockNumberAndIndex
		// - eth_getTransactionByHash
		// - eth_getTransactionReceipt
		// - eth_sendRawTransaction
		// - eth_sendTransaction
		// - eth_sign
		// - eth_signTransaction
		//
		// Undocumented APIs:
		// - eth_getRawTransactionByBlockHashAndIndex
		// - eth_getRawTransactionByBlockNumberAndIndex
		// - eth_getRawTransactionByHash
		// - eth_pendingTransactions
		{
			"eth",
			immediateReceipts{
				vm.exec,
				ethapi.NewTransactionAPI(b, new(ethapi.AddrLocker)),
			},
		},
		// Standard Ethereum node APIS:
		// - eth_getFilterChanges
		// - eth_getFilterLogs
		// - eth_getLogs
		// - eth_newBlockFilter
		// - eth_newFilter
		// - eth_newPendingTransactionFilter
		// - eth_uninstallFilter
		//
		// Geth-specific APIs:
		// - eth_subscribe
		//  - newHeads
		//  - newPendingTransactions
		//  - logs
		{"eth", filterAPI},
	}

	if vm.config.RPCConfig.EnableDBInspecting {
		apis = append(apis, api{
			// Geth-specific APIs:
			// - debug_chaindbCompact
			// - debug_chaindbProperty
			// - debug_dbAncient
			// - debug_dbAncients
			// - debug_dbGet
			// - debug_getRawTransaction
			// - debug_printBlock
			// - debug_setHead          (no-op, logs info)
			//
			// TODO: implement once BlockByNumberOrHash and GetReceipts exist:
			// - debug_getRawBlock
			// - debug_getRawHeader
			// - debug_getRawReceipts
			"debug", ethapi.NewDebugAPI(b),
		})
	}

	if vm.config.RPCConfig.EnableProfiling {
		apis = append(apis, api{
			// Geth-specific APIs:
			// - debug_blockProfile
			// - debug_cpuProfile
			// - debug_freeOSMemory
			// - debug_gcStats
			// - debug_goTrace
			// - debug_memStats
			// - debug_mutexProfile
			// - debug_setBlockProfileRate
			// - debug_setGCPercent
			// - debug_setMutexProfileFraction
			// - debug_stacks
			// - debug_startCPUProfile
			// - debug_startGoTrace
			// - debug_stopCPUProfile
			// - debug_stopGoTrace
			// - debug_verbosity
			// - debug_vmodule
			// - debug_writeBlockProfile
			// - debug_writeMemProfile
			// - debug_writeMutexProfile
			"debug", debug.Handler,
		})
	}

	if !vm.config.RPCConfig.DisableTracing {
		apis = append(apis, api{
			// Geth-specific APIs:
			"debug", tracers.NewAPI(b),
		})
	}

	s := rpc.NewServer()
	for _, api := range apis {
		if err := s.RegisterName(api.namespace, api.api); err != nil {
			return nil, fmt.Errorf("%T.RegisterName(%q, %T): %v", s, api.namespace, api.api, err)
		}
	}
	return s, nil
}

// web3API offers the `web3` RPCs.
type web3API struct {
	clientVersion string
}

func newWeb3API() *web3API {
	return &web3API{
		clientVersion: version.GetVersions().String(),
	}
}

func (w *web3API) ClientVersion() string {
	return w.clientVersion
}

func (*web3API) Sha3(input hexutil.Bytes) hexutil.Bytes {
	return crypto.Keccak256(input)
}

// netAPI offers the `net` RPCs.
type netAPI struct {
	peers   *p2p.Peers
	chainID string
}

func newNetAPI(peers *p2p.Peers, chainID uint64) *netAPI {
	return &netAPI{
		peers:   peers,
		chainID: strconv.FormatUint(chainID, 10),
	}
}

func (s *netAPI) Listening() bool {
	return true // The node is always listening for p2p connections.
}

func (s *netAPI) PeerCount() hexutil.Uint {
	c := s.peers.Len()
	if c <= 0 {
		return 0
	}
	// Peers includes ourself, so we subtract one.
	return hexutil.Uint(c) - 1
}

func (s *netAPI) Version() string {
	return s.chainID
}

type blockChainAPI struct {
	*ethapi.BlockChainAPI
	b *apiBackend
}

// We override [ethapi.BlockChainAPI.GetBlockReceipts] so that we do not return
// an error when a user queries a known, but not yet executed, block.
func (b *blockChainAPI) GetBlockReceipts(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) ([]map[string]any, error) {
	receipts, blk, err := b.b.getReceipts(blockNrOrHash)
	if err != nil || blk == nil {
		return nil, nil //nolint:nilerr // This follows Geth behavior for [ethapi.BlockChainAPI.GetBlockReceipts]
	}

	hash := blk.Hash()
	num := blk.NumberU64()
	signer := b.b.vm.exec.SignerForBlock(blk)
	txs := blk.Transactions()

	result := make([]map[string]any, len(txs))
	for i, receipt := range receipts {
		result[i] = ethapi.MarshalReceipt(receipt, hash, num, signer, txs[i], i)
	}
	return result, nil
}

// chainIndexer implements the subset of [ethapi.Backend] required to back a
// [core.ChainIndexer].
type chainIndexer struct {
	exec *saexec.Executor
}

var _ core.ChainIndexerChain = chainIndexer{}

func (c chainIndexer) CurrentHeader() *types.Header {
	return types.CopyHeader(c.exec.LastExecuted().Header())
}

func (c chainIndexer) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return c.exec.SubscribeChainHeadEvent(ch)
}

// A bloomOverrider constructs Bloom filters from persisted receipts instead of
// relying on the [types.Header] field.
type bloomOverrider struct {
	db ethdb.Database
}

var _ filters.BloomOverrider = bloomOverrider{}

// OverrideHeaderBloom returns the Bloom filter of the receipts generated when
// executing the respective block, whereas the [types.Header] carries those
// settled by the block.
func (b bloomOverrider) OverrideHeaderBloom(header *types.Header) types.Bloom {
	return types.CreateBloom(rawdb.ReadRawReceipts(
		b.db,
		header.Hash(),
		header.Number.Uint64(),
	))
}

type apiBackend struct {
	vm             *VM
	accountManager *accounts.Manager

	*txgossip.Set
	chainIndexer
	bloomOverrider
	*bloomIndexer
}

var _ APIBackend = (*apiBackend)(nil)

func (b *apiBackend) ChainDb() ethdb.Database { //nolint:staticcheck // this name required by ethapi.Backend interface
	return b.vm.db
}

func (b *apiBackend) ChainConfig() *params.ChainConfig {
	return b.vm.exec.ChainConfig()
}

func (b *apiBackend) RPCTxFeeCap() float64 {
	return b.vm.config.RPCConfig.TxFeeCap
}

func (b *apiBackend) UnprotectedAllowed() bool {
	return false
}

// ExtRPCEnabled reports that external RPC access is enabled. This adds an
// additional security measure in case we add support for the personal API.
func (*apiBackend) ExtRPCEnabled() bool {
	return true
}

// PendingBlockAndReceipts returns a nil block and receipts. Returning nil tells
// geth that this backend does not support pending blocks. In SAE, the pending
// block is defined as the most recently accepted block, but receipts are only
// available after execution. Returning a non-nil block with incorrect or empty
// receipts could cause geth to encounter errors.
func (*apiBackend) PendingBlockAndReceipts() (*types.Block, types.Receipts) {
	return nil, nil
}

func (b *apiBackend) AccountManager() *accounts.Manager {
	return b.accountManager
}

func (b *apiBackend) CurrentBlock() *types.Header {
	return b.CurrentHeader()
}

// Total difficulty does not make sense in snowman consensus, as it is not PoW.
// Ethereum, post merge (switch to PoS), sets the difficulty of each block to 0
// (see: https://github.com/ethereum/go-ethereum/blob/be92f5487e67939b8dbbc9675d6c15be76ffd18d/consensus/beacon/consensus.go#L228-L231)
// and no longer exposes the total difficulty of the chain at all via the API.
//
// TODO(JonathanOppenheimer): Once we update libevm, remove GetTd.
func (b *apiBackend) GetTd(ctx context.Context, hash common.Hash) *big.Int {
	return common.Big0
}

func (b *apiBackend) SyncProgress() ethereum.SyncProgress {
	// Avalanchego does not expose APIs until after the node has fully synced.
	return ethereum.SyncProgress{}
}

func (b *apiBackend) HeaderByNumber(ctx context.Context, n rpc.BlockNumber) (*types.Header, error) {
	return readByNumber(b, n, neverErrs(rawdb.ReadHeader))
}

func (b *apiBackend) BlockByNumber(ctx context.Context, n rpc.BlockNumber) (*types.Block, error) {
	return readByNumber(b, n, neverErrs(rawdb.ReadBlock))
}

func (b *apiBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return readByHash(b.vm, hash, (*blocks.Block).Header, neverErrs(rawdb.ReadHeader), nil /* errWhenNotFound */)
}

func (b *apiBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return readByHash(b.vm, hash, (*blocks.Block).EthBlock, neverErrs(rawdb.ReadBlock), nil /* errWhenNotFound */)
}

func (b *apiBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	return readByNumberOrHash(b, blockNrOrHash, (*blocks.Block).Header, neverErrs(rawdb.ReadHeader))
}

func (b *apiBackend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	return readByNumberOrHash(b, blockNrOrHash, (*blocks.Block).EthBlock, neverErrs(rawdb.ReadBlock))
}

func (b *apiBackend) GetTransaction(ctx context.Context, txHash common.Hash) (exists bool, tx *types.Transaction, blockHash common.Hash, blockNumber uint64, index uint64, err error) {
	tx, blockHash, blockNumber, index = rawdb.ReadTransaction(b.vm.db, txHash)
	if tx == nil {
		return false, nil, common.Hash{}, 0, 0, nil
	}
	return true, tx, blockHash, blockNumber, index, nil
}

func (b *apiBackend) GetPoolTransaction(txHash common.Hash) *types.Transaction {
	return b.Set.Pool.Get(txHash)
}

func (b *apiBackend) GetBody(ctx context.Context, hash common.Hash, number rpc.BlockNumber) (*types.Body, error) {
	if hash == (common.Hash{}) {
		return nil, errors.New("empty block hash")
	}
	n, err := b.ResolveBlockNumber(number)
	if err != nil {
		return nil, err
	}

	if block, ok := b.vm.blocks.Load(hash); ok {
		if block.NumberU64() != n {
			return nil, fmt.Errorf("found block number %d for hash %#x, expected %d", block.NumberU64(), hash, number)
		}
		return block.EthBlock().Body(), nil
	}

	return rawdb.ReadBody(b.vm.db, hash, n), nil
}

func (b *apiBackend) GetLogs(ctx context.Context, blockHash common.Hash, number uint64) ([][]*types.Log, error) {
	return rawdb.ReadLogs(b.vm.db, blockHash, number), nil
}

func (b *apiBackend) GetPoolTransactions() (types.Transactions, error) {
	pending := b.Pool.Pending(txpool.PendingFilter{})

	var pendingCount int
	for _, batch := range pending {
		pendingCount += len(batch)
	}

	txs := make(types.Transactions, 0, pendingCount)
	for _, batch := range pending {
		for _, lazy := range batch {
			if tx := lazy.Resolve(); tx != nil {
				txs = append(txs, tx)
			}
		}
	}
	return txs, nil
}

type (
	canonicalReader[T any]        func(ethdb.Reader, common.Hash, uint64) *T
	canonicalReaderWithErr[T any] func(ethdb.Reader, common.Hash, uint64) (*T, error)
	blockAccessor[T any]          func(*blocks.Block) *T
)

func neverErrs[T any](fn func(ethdb.Reader, common.Hash, uint64) *T) canonicalReaderWithErr[T] {
	return func(r ethdb.Reader, h common.Hash, n uint64) (*T, error) {
		return fn(r, h, n), nil
	}
}

func readByNumber[T any](b *apiBackend, n rpc.BlockNumber, read canonicalReaderWithErr[T]) (*T, error) {
	num, err := b.ResolveBlockNumber(n)
	if errors.Is(err, errFutureBlockNotResolved) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return read(b.vm.db, rawdb.ReadCanonicalHash(b.vm.db, num), num)
}

// readByHash returns `fromMem(b)` if a block with the specified hash is in the
// VM's memory, otherwise it returns `fromDB()` i.f.f. the block was previously
// accepted. If `fromDB()` is called then the block is guaranteed to exist if
// read with [rawdb] functions.
//
// A hash that is in neither of the VM's memory nor the database will result in
// a return of `(nil, errWhenNotFound)` to allow for usage with the [rawdb]
// pattern of returning `(nil, nil)`.
func readByHash[T any](vm *VM, hash common.Hash, fromMem blockAccessor[T], fromDB canonicalReaderWithErr[T], errWhenNotFound error) (*T, error) {
	if blk, ok := vm.blocks.Load(hash); ok {
		return fromMem(blk), nil
	}
	num := rawdb.ReadHeaderNumber(vm.db, hash)
	if num == nil {
		return nil, errWhenNotFound
	}
	return fromDB(vm.db, hash, *num)
}

// TODO(arr4n) DRY [readByHash] and [readByNumberOrHash]

func readByNumberOrHash[T any](b *apiBackend, blockNrOrHash rpc.BlockNumberOrHash, fromMem blockAccessor[T], fromDB canonicalReaderWithErr[T]) (*T, error) {
	n, hash, err := b.resolveBlockNumberOrHash(blockNrOrHash)
	if err != nil {
		return nil, err
	}
	if blk, ok := b.vm.blocks.Load(hash); ok {
		return fromMem(blk), nil
	}
	return fromDB(b.vm.db, hash, n)
}

var (
	errNeitherNumberNorHash = fmt.Errorf("%T carrying neither number nor hash", rpc.BlockNumberOrHash{})
	errBothNumberAndHash    = fmt.Errorf("%T carrying both number and hash", rpc.BlockNumberOrHash{})
	errNonCanonicalBlock    = errors.New("non-canonical block")
)

func (b *apiBackend) resolveBlockNumberOrHash(numOrHash rpc.BlockNumberOrHash) (uint64, common.Hash, error) {
	rpcNum, isNum := numOrHash.Number()
	hash, isHash := numOrHash.Hash()

	switch {
	case isNum && isHash:
		return 0, common.Hash{}, errBothNumberAndHash

	case isNum:
		num, err := b.ResolveBlockNumber(rpcNum)
		if err != nil {
			return 0, common.Hash{}, err
		}

		hash := rawdb.ReadCanonicalHash(b.db, num)
		if hash == (common.Hash{}) {
			return 0, common.Hash{}, fmt.Errorf("block %d not found", num)
		}
		return num, hash, nil

	case isHash:
		if bl, ok := b.vm.blocks.Load(hash); ok {
			n := bl.NumberU64()
			if numOrHash.RequireCanonical && hash != rawdb.ReadCanonicalHash(b.db, n) {
				return 0, common.Hash{}, errNonCanonicalBlock
			}
			return n, hash, nil
		}

		numPtr := rawdb.ReadHeaderNumber(b.db, hash)
		if numPtr == nil {
			return 0, common.Hash{}, fmt.Errorf("block %#x not found", hash)
		}
		// We only write canonical blocks to the database so there's no need to
		// perform a check.
		return *numPtr, hash, nil

	default:
		return 0, common.Hash{}, errNeitherNumberNorHash
	}
}

var errFutureBlockNotResolved = errors.New("not accepted yet")

func (b *apiBackend) ResolveBlockNumber(bn rpc.BlockNumber) (uint64, error) {
	head := b.vm.last.accepted.Load().Height()

	switch bn {
	case rpc.PendingBlockNumber: // i.e. pending execution
		return head, nil
	case rpc.LatestBlockNumber:
		return b.vm.exec.LastExecuted().Height(), nil
	case rpc.SafeBlockNumber, rpc.FinalizedBlockNumber:
		return b.vm.last.settled.Load().Height(), nil
	}

	if bn < 0 {
		// Any future definitions should be added above.
		return 0, fmt.Errorf("%s block unsupported", bn.String())
	}
	n := uint64(bn) //nolint:gosec // Non-negative check performed above
	if n > head {
		return 0, fmt.Errorf("%w: block %d", errFutureBlockNotResolved, n)
	}
	return n, nil
}

func (b *apiBackend) Stats() (pending int, queued int) {
	return b.Set.Pool.Stats()
}

func (b *apiBackend) TxPoolContent() (map[common.Address][]*types.Transaction, map[common.Address][]*types.Transaction) {
	return b.Set.Pool.Content()
}

func (b *apiBackend) TxPoolContentFrom(addr common.Address) ([]*types.Transaction, []*types.Transaction) {
	return b.Set.Pool.ContentFrom(addr)
}

func (b *apiBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.vm.exec.SubscribeChainEvent(ch)
}

func (b *apiBackend) SubscribeChainSideEvent(chan<- core.ChainSideEvent) event.Subscription {
	// SAE never reorgs, so there are no side events.
	return newNoopSubscription()
}

func (b *apiBackend) LastAcceptedBlock() *blocks.Block {
	return b.vm.last.accepted.Load()
}

func (b *apiBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.Set.Pool.SubscribeTransactions(ch, true)
}

func (b *apiBackend) SubscribeRemovedLogsEvent(chan<- core.RemovedLogsEvent) event.Subscription {
	// SAE never reorgs, so no logs are ever removed.
	return newNoopSubscription()
}

func (b *apiBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.vm.exec.SubscribeLogsEvent(ch)
}

func (b *apiBackend) SubscribePendingLogsEvent(chan<- []*types.Log) event.Subscription {
	// In SAE, "pending" refers to the execution status. There are no logs known
	// for transactions pending execution.
	return newNoopSubscription()
}

func (b *apiBackend) SetHead(uint64) {
	b.vm.log().Info("debug_setHead called but not supported by SAE")
}

func (b *apiBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	receipts, _, err := b.getReceipts(rpc.BlockNumberOrHashWithHash(hash, false))
	if err != nil {
		return nil, nil //nolint:nilerr // This follows Geth behavior for [ethapi.Backend.GetReceipts]
	}
	return receipts, nil
}

// getReceipts resolves receipts and the underlying [types.Block] by number or
// hash, checking in-memory blocks first then falling back to the database.
// Returns nils for blocks that are not yet executed.
func (b *apiBackend) getReceipts(numOrHash rpc.BlockNumberOrHash) (types.Receipts, *types.Block, error) {
	blk, err := readByNumberOrHash(
		b,
		numOrHash,
		func(b *blocks.Block) *blocks.Block {
			return b
		},
		b.vm.settledBlockFromDB,
	)
	if err != nil {
		return nil, nil, err
	}
	if !blk.Executed() {
		return nil, nil, nil
	}
	return blk.Receipts(), blk.EthBlock(), nil
}

type noopSubscription struct {
	once sync.Once
	err  chan error
}

func newNoopSubscription() *noopSubscription {
	return &noopSubscription{
		err: make(chan error),
	}
}

func (s *noopSubscription) Err() <-chan error {
	return s.err
}

func (s *noopSubscription) Unsubscribe() {
	s.once.Do(func() {
		close(s.err)
	})
}
