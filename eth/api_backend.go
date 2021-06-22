// (c) 2019-2020, Ava Labs, Inc.
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

	"github.com/ava-labs/coreth/accounts"
	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/bloombits"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/eth/gasprice"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
)

var (
	ErrUnfinalizedData = errors.New("cannot query unfinalized data")
	errExpired         = errors.New("request expired")
)

// EthAPIBackend implements ethapi.Backend for full nodes
type EthAPIBackend struct {
	extRPCEnabled       bool
	allowUnprotectedTxs bool
	eth                 *Ethereum
	gpo                 *gasprice.Oracle
}

// ChainConfig returns the active chain configuration.
func (b *EthAPIBackend) ChainConfig() *params.ChainConfig {
	return b.eth.blockchain.Config()
}

func (b *EthAPIBackend) GetVMConfig() *vm.Config {
	return b.eth.blockchain.GetVMConfig()
}

func (b *EthAPIBackend) CurrentBlock() *types.Block {
	return b.eth.blockchain.CurrentBlock()
}

func (b *EthAPIBackend) LastAcceptedBlock() *types.Block {
	return b.eth.LastAcceptedBlock()
}

func (b *EthAPIBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	if deadline, exists := ctx.Deadline(); exists && time.Until(deadline) < 0 {
		return nil, errExpired
	}
	// Treat requests for the pending, latest, or accepted block
	// identically.
	acceptedBlock := b.eth.LastAcceptedBlock()
	if number.IsAccepted() {
		return acceptedBlock.Header(), nil
	}

	if !b.GetVMConfig().AllowUnfinalizedQueries && acceptedBlock != nil {
		if number.Int64() > acceptedBlock.Number().Int64() {
			return nil, ErrUnfinalizedData
		}
	}

	return b.eth.blockchain.GetHeaderByNumber(uint64(number)), nil
}

func (b *EthAPIBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.HeaderByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header := b.eth.blockchain.GetHeaderByHash(hash)
		if header == nil {
			return nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.eth.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, errors.New("hash is not currently canonical")
		}
		return header, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *EthAPIBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	if deadline, exists := ctx.Deadline(); exists && time.Until(deadline) < 0 {
		return nil, errExpired
	}
	return b.eth.blockchain.GetHeaderByHash(hash), nil
}

func (b *EthAPIBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	if deadline, exists := ctx.Deadline(); exists && time.Until(deadline) < 0 {
		return nil, errExpired
	}
	// Treat requests for the pending, latest, or accepted block
	// identically.
	acceptedBlock := b.eth.LastAcceptedBlock()
	if number.IsAccepted() {
		return acceptedBlock, nil
	}

	if !b.GetVMConfig().AllowUnfinalizedQueries && acceptedBlock != nil {
		if number.Int64() > acceptedBlock.Number().Int64() {
			return nil, ErrUnfinalizedData
		}
	}

	return b.eth.blockchain.GetBlockByNumber(uint64(number)), nil
}

func (b *EthAPIBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	if deadline, exists := ctx.Deadline(); exists && time.Until(deadline) < 0 {
		return nil, errExpired
	}
	return b.eth.blockchain.GetBlockByHash(hash), nil
}

func (b *EthAPIBackend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.BlockByNumber(ctx, blockNr)
	}
	if deadline, exists := ctx.Deadline(); exists && time.Until(deadline) < 0 {
		return nil, errExpired
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header := b.eth.blockchain.GetHeaderByHash(hash)
		if header == nil {
			return nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.eth.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, errors.New("hash is not currently canonical")
		}
		block := b.eth.blockchain.GetBlock(hash, header.Number.Uint64())
		if block == nil {
			return nil, errors.New("header found, but block body is missing")
		}
		return block, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
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
	return stateDb, header, err
}

func (b *EthAPIBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.StateAndHeaderByNumber(ctx, blockNr)
	}
	if deadline, exists := ctx.Deadline(); exists && time.Until(deadline) < 0 {
		return nil, nil, errExpired
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header, err := b.HeaderByHash(ctx, hash)
		if err != nil {
			return nil, nil, err
		}
		if header == nil {
			return nil, nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.eth.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, nil, errors.New("hash is not currently canonical")
		}
		stateDb, err := b.eth.BlockChain().StateAt(header.Root)
		return stateDb, header, err
	}
	return nil, nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *EthAPIBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	if deadline, exists := ctx.Deadline(); exists && time.Until(deadline) < 0 {
		return nil, errExpired
	}
	return b.eth.blockchain.GetReceiptsByHash(hash), nil
}

func (b *EthAPIBackend) GetLogs(ctx context.Context, hash common.Hash) ([][]*types.Log, error) {
	if deadline, exists := ctx.Deadline(); exists && time.Until(deadline) < 0 {
		return nil, errExpired
	}
	receipts := b.eth.blockchain.GetReceiptsByHash(hash)
	if receipts == nil {
		return nil, nil
	}
	logs := make([][]*types.Log, len(receipts))
	for i, receipt := range receipts {
		logs[i] = receipt.Logs
	}
	return logs, nil
}

func (b *EthAPIBackend) GetTd(ctx context.Context, hash common.Hash) *big.Int {
	return b.eth.blockchain.GetTdByHash(hash)
}

func (b *EthAPIBackend) GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmConfig *vm.Config) (*vm.EVM, func() error, error) {
	vmError := func() error { return nil }
	if vmConfig == nil {
		vmConfig = b.eth.blockchain.GetVMConfig()
	}
	txContext := core.NewEVMTxContext(msg)
	context := core.NewEVMBlockContext(header, b.eth.BlockChain(), nil)
	return vm.NewEVM(context, txContext, state, b.eth.blockchain.Config(), *vmConfig), vmError, nil
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
	if deadline, exists := ctx.Deadline(); exists && time.Until(deadline) < 0 {
		return errExpired
	}
	err := b.eth.txPool.AddLocal(signedTx)
	return err
}

func (b *EthAPIBackend) GetPoolTransactions() (types.Transactions, error) {
	pending, err := b.eth.txPool.Pending()
	if err != nil {
		return nil, err
	}
	var txs types.Transactions
	for _, batch := range pending {
		txs = append(txs, batch...)
	}
	return txs, nil
}

func (b *EthAPIBackend) GetPoolTransaction(hash common.Hash) *types.Transaction {
	return b.eth.txPool.Get(hash)
}

func (b *EthAPIBackend) GetTransaction(ctx context.Context, txHash common.Hash) (*types.Transaction, common.Hash, uint64, uint64, error) {
	tx, blockHash, blockNumber, index := rawdb.ReadTransaction(b.eth.ChainDb(), txHash)

	// Respond as if the transaction does not exist if it is not yet in an
	// accepted block. We explicitly choose not to error here to avoid breaking
	// expectations with clients (expect an empty response when a transaction
	// does not exist).
	acceptedBlock := b.eth.LastAcceptedBlock()
	if !b.GetVMConfig().AllowUnfinalizedQueries && acceptedBlock != nil && tx != nil {
		if blockNumber > acceptedBlock.NumberU64() {
			return nil, common.Hash{}, 0, 0, nil
		}
	}

	return tx, blockHash, blockNumber, index, nil
}

func (b *EthAPIBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.eth.txPool.Nonce(addr), nil
}

func (b *EthAPIBackend) Stats() (pending int, queued int) {
	return b.eth.txPool.Stats()
}

func (b *EthAPIBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	return b.eth.TxPool().Content()
}

func (b *EthAPIBackend) TxPool() *core.TxPool {
	return b.eth.TxPool()
}

func (b *EthAPIBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.eth.TxPool().SubscribeNewTxsEvent(ch)
}

func (b *EthAPIBackend) Downloader() *downloader.Downloader {
	return b.eth.Downloader()
}

func (b *EthAPIBackend) SuggestPrice(ctx context.Context) (*big.Int, error) {
	return b.gpo.SuggestPrice(ctx)
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

func (b *EthAPIBackend) UnprotectedAllowed() bool {
	return b.allowUnprotectedTxs
}

func (b *EthAPIBackend) RPCGasCap() uint64 {
	return b.eth.config.RPCGasCap
}

func (b *EthAPIBackend) RPCTxFeeCap() float64 {
	return b.eth.config.RPCTxFeeCap
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

func (b *EthAPIBackend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, checkLive bool) (*state.StateDB, error) {
	return b.eth.stateAtBlock(block, reexec, base, checkLive)
}

func (b *EthAPIBackend) StateAtTransaction(ctx context.Context, block *types.Block, txIndex int, reexec uint64) (core.Message, vm.BlockContext, *state.StateDB, error) {
	return b.eth.stateAtTransaction(block, txIndex, reexec)
}
