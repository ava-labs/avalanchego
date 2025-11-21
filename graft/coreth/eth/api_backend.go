// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package eth

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/eth/gasprice"
	"github.com/ava-labs/coreth/eth/tracers"
	"github.com/ava-labs/coreth/internal/ethapi"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ava-labs/libevm/accounts"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/bloombits"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/event"
)

var ErrUnfinalizedData = errors.New("cannot query unfinalized data")

// EthAPIBackend implements ethapi.Backend and tracers.Backend for full nodes
type EthAPIBackend struct {
	extRPCEnabled            bool
	allowUnprotectedTxs      bool
	allowUnprotectedTxHashes map[common.Hash]struct{} // Invariant: read-only after creation.
	allowUnfinalizedQueries  bool
	eth                      *Ethereum
	gpo                      *gasprice.Oracle

	// historicalProofQueryWindow is the number of blocks before the last accepted block to be accepted for
	// state queries when running archive mode.
	historicalProofQueryWindow uint64
}

// ChainConfig returns the active chain configuration.
func (b *EthAPIBackend) ChainConfig() *params.ChainConfig {
	return b.eth.blockchain.Config()
}

// IsArchive returns true if the node is running in archive mode, false otherwise.
func (b *EthAPIBackend) IsArchive() bool {
	return !b.eth.config.Pruning
}

// HistoricalProofQueryWindow returns the number of blocks before the last accepted block to be accepted for state queries.
// It returns 0 to indicate to accept any block number for state queries.
func (b *EthAPIBackend) HistoricalProofQueryWindow() uint64 {
	return b.historicalProofQueryWindow
}

func (b *EthAPIBackend) IsAllowUnfinalizedQueries() bool {
	return b.allowUnfinalizedQueries
}

func (b *EthAPIBackend) SetAllowUnfinalizedQueries(allow bool) {
	b.allowUnfinalizedQueries = allow
}

func (b *EthAPIBackend) CurrentBlock() *types.Header {
	return b.eth.blockchain.CurrentBlock()
}

func (b *EthAPIBackend) LastAcceptedBlock() *types.Block {
	return b.eth.LastAcceptedBlock()
}

func (b *EthAPIBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Treat requests for the pending, latest, or accepted block
	// identically.
	acceptedBlock := b.eth.LastAcceptedBlock()
	if number.IsAccepted() {
		if b.isLatestAndAllowed(number) {
			return b.eth.blockchain.CurrentHeader(), nil
		}
		return acceptedBlock.Header(), nil
	}

	if !b.IsAllowUnfinalizedQueries() && acceptedBlock != nil {
		if number.Int64() > acceptedBlock.Number().Int64() {
			return nil, ErrUnfinalizedData
		}
	}

	return b.eth.blockchain.GetHeaderByNumber(uint64(number)), nil
}

func (b *EthAPIBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	header := b.eth.blockchain.GetHeaderByHash(hash)
	if header == nil {
		return nil, nil
	}

	if b.eth.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
		return nil, nil
	}

	acceptedBlock := b.eth.LastAcceptedBlock()
	if !b.IsAllowUnfinalizedQueries() && acceptedBlock != nil {
		if header.Number.Cmp(acceptedBlock.Number()) > 0 {
			return nil, ErrUnfinalizedData
		}
	}
	return header, nil
}

func (b *EthAPIBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.HeaderByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header, err := b.HeaderByHash(ctx, hash)
		if err != nil {
			return nil, err
		}
		if header == nil {
			return nil, errors.New("header for hash not found")
		}
		return header, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *EthAPIBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Treat requests for the pending, latest, or accepted block
	// identically.
	acceptedBlock := b.eth.LastAcceptedBlock()
	if number.IsAccepted() {
		if b.isLatestAndAllowed(number) {
			header := b.eth.blockchain.CurrentBlock()
			return b.eth.blockchain.GetBlock(header.Hash(), header.Number.Uint64()), nil
		}
		return acceptedBlock, nil
	}

	if !b.IsAllowUnfinalizedQueries() && acceptedBlock != nil {
		if number.Int64() > acceptedBlock.Number().Int64() {
			return nil, ErrUnfinalizedData
		}
	}

	return b.eth.blockchain.GetBlockByNumber(uint64(number)), nil
}

func (b *EthAPIBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	block := b.eth.blockchain.GetBlockByHash(hash)
	if block == nil {
		return nil, nil
	}

	number := block.Number()
	if b.eth.blockchain.GetCanonicalHash(number.Uint64()) != hash {
		return nil, nil
	}

	acceptedBlock := b.eth.LastAcceptedBlock()
	if !b.IsAllowUnfinalizedQueries() && acceptedBlock != nil {
		if number.Cmp(acceptedBlock.Number()) > 0 {
			return nil, ErrUnfinalizedData
		}
	}
	return block, nil
}

// GetBody returns body of a block. It does not resolve special block numbers.
func (b *EthAPIBackend) GetBody(ctx context.Context, hash common.Hash, number rpc.BlockNumber) (*types.Body, error) {
	if number < 0 || hash == (common.Hash{}) {
		return nil, errors.New("invalid arguments; expect hash and no special block numbers")
	}
	if body := b.eth.blockchain.GetBody(hash); body != nil {
		return body, nil
	}
	return nil, errors.New("block body not found")
}

func (b *EthAPIBackend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.BlockByNumber(ctx, blockNr)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		block, err := b.BlockByHash(ctx, hash)
		if err != nil {
			return nil, err
		}
		if block == nil {
			return nil, errors.New("header for hash not found")
		}
		return block, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *EthAPIBackend) BadBlocks() ([]*types.Block, []*core.BadBlockReason) {
	return b.eth.blockchain.BadBlocks()
}

func (b *EthAPIBackend) StateAndHeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	// Request the block by its number and retrieve its state
	header, err := b.HeaderByNumber(ctx, number)
	if err != nil {
		return nil, nil, err
	}
	if header == nil {
		return nil, nil, errors.New("header not found")
	}
	stateDb, err := b.eth.BlockChain().StateAt(header.Root)
	if err != nil {
		return nil, nil, err
	}
	return stateDb, header, nil
}

func (b *EthAPIBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.StateAndHeaderByNumber(ctx, blockNr)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header, err := b.HeaderByHash(ctx, hash)
		if err != nil {
			return nil, nil, err
		}
		if header == nil {
			return nil, nil, errors.New("header for hash not found")
		}
		stateDb, err := b.eth.BlockChain().StateAt(header.Root)
		if err != nil {
			return nil, nil, err
		}
		return stateDb, header, nil
	}
	return nil, nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *EthAPIBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b.eth.blockchain.GetReceiptsByHash(hash), nil
}

func (b *EthAPIBackend) GetLogs(ctx context.Context, hash common.Hash, number uint64) ([][]*types.Log, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b.eth.blockchain.GetLogs(hash, number), nil
}

func (b *EthAPIBackend) GetEVM(ctx context.Context, msg *core.Message, state *state.StateDB, header *types.Header, vmConfig *vm.Config, blockCtx *vm.BlockContext) *vm.EVM {
	if vmConfig == nil {
		vmConfig = b.eth.blockchain.GetVMConfig()
	}
	txContext := core.NewEVMTxContext(msg)
	var context vm.BlockContext
	if blockCtx != nil {
		context = *blockCtx
	} else {
		context = core.NewEVMBlockContext(header, b.eth.BlockChain(), nil)
	}
	return vm.NewEVM(context, txContext, state, b.ChainConfig(), *vmConfig)
}

func (b *EthAPIBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeRemovedLogsEvent(ch)
}

func (b *EthAPIBackend) SubscribePendingLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.eth.miner.SubscribePendingLogs(ch)
}

func (b *EthAPIBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainEvent(ch)
}

func (b *EthAPIBackend) SubscribeChainAcceptedEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainAcceptedEvent(ch)
}

func (b *EthAPIBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainHeadEvent(ch)
}

func (b *EthAPIBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainSideEvent(ch)
}

func (b *EthAPIBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.eth.BlockChain().SubscribeLogsEvent(ch)
}

func (b *EthAPIBackend) SubscribeAcceptedLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.eth.BlockChain().SubscribeAcceptedLogsEvent(ch)
}

func (b *EthAPIBackend) SubscribeAcceptedTransactionEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeAcceptedTransactionEvent(ch)
}

func (b *EthAPIBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := b.eth.txPool.Add([]*types.Transaction{signedTx}, true, false)[0]; err != nil {
		return err
	}

	// We only enqueue transactions for push gossip if they were submitted over the RPC and
	// added to the mempool.
	b.eth.gossiper.Add(signedTx)
	return nil
}

func (b *EthAPIBackend) GetPoolTransactions() (types.Transactions, error) {
	pending := b.eth.txPool.Pending(txpool.PendingFilter{})
	var txs types.Transactions
	for _, batch := range pending {
		for _, lazy := range batch {
			if tx := lazy.Resolve(); tx != nil {
				txs = append(txs, tx)
			}
		}
	}
	return txs, nil
}

func (b *EthAPIBackend) GetPoolTransaction(hash common.Hash) *types.Transaction {
	return b.eth.txPool.Get(hash)
}

func (b *EthAPIBackend) GetTransaction(ctx context.Context, txHash common.Hash) (bool, *types.Transaction, common.Hash, uint64, uint64, error) {
	lookup, tx, err := b.eth.blockchain.GetTransactionLookup(txHash)
	if err != nil {
		return false, nil, common.Hash{}, 0, 0, err
	}
	if lookup == nil || tx == nil {
		return false, nil, common.Hash{}, 0, 0, nil
	}

	// Respond as if the transaction does not exist if it is not yet in an
	// accepted block. We explicitly choose not to error here to avoid breaking
	// expectations with clients (expect an empty response when a transaction
	// does not exist).
	acceptedBlock := b.eth.LastAcceptedBlock()
	if !b.IsAllowUnfinalizedQueries() && acceptedBlock != nil && tx != nil {
		if lookup.BlockIndex > acceptedBlock.NumberU64() {
			return false, nil, common.Hash{}, 0, 0, nil
		}
	}

	return true, tx, lookup.BlockHash, lookup.BlockIndex, lookup.Index, nil
}

func (b *EthAPIBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.eth.txPool.Nonce(addr), nil
}

func (b *EthAPIBackend) Stats() (runnable int, blocked int) {
	return b.eth.txPool.Stats()
}

func (b *EthAPIBackend) TxPoolContent() (map[common.Address][]*types.Transaction, map[common.Address][]*types.Transaction) {
	return b.eth.txPool.Content()
}

func (b *EthAPIBackend) TxPoolContentFrom(addr common.Address) ([]*types.Transaction, []*types.Transaction) {
	return b.eth.txPool.ContentFrom(addr)
}

func (b *EthAPIBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.eth.txPool.SubscribeTransactions(ch, true)
}

func (b *EthAPIBackend) EstimateBaseFee(ctx context.Context) (*big.Int, error) {
	return b.gpo.EstimateBaseFee(ctx)
}

func (b *EthAPIBackend) SuggestPrice(ctx context.Context) (*big.Int, error) {
	return b.gpo.SuggestPrice(ctx)
}

func (b *EthAPIBackend) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return b.gpo.SuggestTipCap(ctx)
}

func (b *EthAPIBackend) FeeHistory(ctx context.Context, blockCount uint64, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (firstBlock *big.Int, reward [][]*big.Int, baseFee []*big.Int, gasUsedRatio []float64, err error) {
	return b.gpo.FeeHistory(ctx, blockCount, lastBlock, rewardPercentiles)
}

func (b *EthAPIBackend) ChainDb() ethdb.Database {
	return b.eth.ChainDb()
}

func (b *EthAPIBackend) EventMux() *event.TypeMux {
	return b.eth.EventMux()
}

func (b *EthAPIBackend) AccountManager() *accounts.Manager {
	return b.eth.AccountManager()
}

func (b *EthAPIBackend) ExtRPCEnabled() bool {
	return b.extRPCEnabled
}

func (b *EthAPIBackend) UnprotectedAllowed(tx *types.Transaction) bool {
	if b.allowUnprotectedTxs {
		return true
	}

	// Check for special cased transaction hashes:
	// Note: this map is read-only after creation, so it is safe to read from it on multiple threads.
	if _, ok := b.allowUnprotectedTxHashes[tx.Hash()]; ok {
		return true
	}

	// Check for "predictable pattern" (Nick's Signature: https://weka.medium.com/how-to-send-ether-to-11-440-people-187e332566b7)
	v, r, s := tx.RawSignatureValues()
	if v == nil || r == nil || s == nil {
		return false
	}

	return tx.Nonce() == 0 && r.Cmp(s) == 0
}

func (b *EthAPIBackend) RPCGasCap() uint64 {
	return b.eth.config.RPCGasCap
}

func (b *EthAPIBackend) RPCEVMTimeout() time.Duration {
	return b.eth.config.RPCEVMTimeout
}

func (b *EthAPIBackend) RPCTxFeeCap() float64 {
	return b.eth.config.RPCTxFeeCap
}

func (b *EthAPIBackend) PriceOptionsConfig() ethapi.PriceOptionConfig {
	return b.eth.config.PriceOptionConfig
}

func (b *EthAPIBackend) BloomStatus() (uint64, uint64) {
	sections, _, _ := b.eth.bloomIndexer.Sections()
	return params.BloomBitsBlocks, sections
}

func (b *EthAPIBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.eth.bloomRequests)
	}
}

func (b *EthAPIBackend) Engine() consensus.Engine {
	return b.eth.engine
}

func (b *EthAPIBackend) CurrentHeader() *types.Header {
	return b.eth.blockchain.CurrentHeader()
}

func (b *EthAPIBackend) GetMaxBlocksPerRequest() int64 {
	return b.eth.settings.MaxBlocksPerRequest
}

func (b *EthAPIBackend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	return b.eth.stateAtBlock(ctx, block, reexec, base, readOnly, preferDisk)
}

func (b *EthAPIBackend) StateAtNextBlock(ctx context.Context, parent, nextBlock *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	return b.eth.StateAtNextBlock(ctx, parent, nextBlock, reexec, base, readOnly, preferDisk)
}

func (b *EthAPIBackend) StateAtTransaction(ctx context.Context, block *types.Block, txIndex int, reexec uint64) (*core.Message, vm.BlockContext, *state.StateDB, tracers.StateReleaseFunc, error) {
	return b.eth.stateAtTransaction(ctx, block, txIndex, reexec)
}

func (b *EthAPIBackend) isLatestAndAllowed(number rpc.BlockNumber) bool {
	return number.IsLatest() && b.IsAllowUnfinalizedQueries()
}
