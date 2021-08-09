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
//
// NOTE: this piece of code is modified by Ted Yin.
// The modification is also licensed under the same LGPL.

package miner

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/consensus/misc"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
)

// environment is the worker's current environment and holds all of the current state information.
type environment struct {
	signer types.Signer

	state   *state.StateDB // apply state changes here
	tcount  int            // tx count in cycle
	gasPool *core.GasPool  // available gas used to pack transactions

	header   *types.Header
	txs      []*types.Transaction
	receipts []*types.Receipt

	start time.Time // Time that block building began
}

// worker is the main object which takes care of submitting new work to consensus engine
// and gathering the sealing result.
type worker struct {
	config      *Config
	chainConfig *params.ChainConfig
	engine      consensus.Engine
	eth         Backend
	chain       *core.BlockChain

	// Feeds
	// TODO remove since this will never be written to
	pendingLogsFeed event.Feed

	// Subscriptions
	mux      *event.TypeMux // TODO replace
	mu       sync.RWMutex   // The lock used to protect the coinbase and extra fields
	coinbase common.Address
}

func newWorker(config *Config, chainConfig *params.ChainConfig, engine consensus.Engine, eth Backend, mux *event.TypeMux) *worker {
	worker := &worker{
		config:      config,
		chainConfig: chainConfig,
		engine:      engine,
		eth:         eth,
		mux:         mux,
		chain:       eth.BlockChain(),
	}

	return worker
}

// setEtherbase sets the etherbase used to initialize the block coinbase field.
func (w *worker) setEtherbase(addr common.Address) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.coinbase = addr
}

// commitNewWork generates several new sealing tasks based on the parent block.
func (w *worker) commitNewWork() (*types.Block, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	tstart := time.Now()
	timestamp := tstart.Unix()
	parent := w.chain.CurrentBlock()
	// Note: in order to support asynchronous block production, blocks are allowed to have
	// the same timestamp as their parent. This allows more than one block to be produced
	// per second.
	if parent.Time() >= uint64(timestamp) {
		timestamp = int64(parent.Time())
	}

	var gasLimit uint64
	if w.chainConfig.IsApricotPhase1(big.NewInt(timestamp)) {
		gasLimit = params.ApricotPhase1GasLimit
	} else {
		// The gas limit is set in phase1 to ApricotPhase1GasLimit because the ceiling and floor were set to the same value
		// such that the gas limit converged to it. Since this is hardbaked now, we remove the ability to configure it.
		gasLimit = core.CalcGasLimit(parent.GasUsed(), parent.GasLimit(), params.ApricotPhase1GasLimit, params.ApricotPhase1GasLimit)
	}
	num := parent.Number()
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     num.Add(num, common.Big1),
		GasLimit:   gasLimit,
		Extra:      nil,
		Time:       uint64(timestamp),
	}
	// Set BaseFee and Extra data field if we are post ApricotPhase3
	bigTimestamp := big.NewInt(timestamp)
	if w.chainConfig.IsApricotPhase3(bigTimestamp) {
		var err error
		header.Extra, header.BaseFee, err = dummy.CalcBaseFee(w.chainConfig, parent.Header(), uint64(timestamp))
		if err != nil {
			return nil, fmt.Errorf("failed to calculate new base fee: %w", err)
		}
	}
	if w.coinbase == (common.Address{}) {
		return nil, errors.New("cannot mine without etherbase")
	}
	header.Coinbase = w.coinbase
	if err := w.engine.Prepare(w.chain, header); err != nil {
		return nil, fmt.Errorf("failed to prepare header for mining: %w", err)
	}

	env, err := w.createCurrentEnvironment(parent, header, tstart)
	if err != nil {
		return nil, fmt.Errorf("failed to create new current environment: %w", err)
	}
	if w.chainConfig.DAOForkSupport && w.chainConfig.DAOForkBlock != nil && w.chainConfig.DAOForkBlock.Cmp(header.Number) == 0 {
		misc.ApplyDAOHardFork(env.state)
	}

	// Fill the block with all available pending transactions.
	pending, err := w.eth.TxPool().Pending(true)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pending transactions: %w", err)
	}

	// Split the pending transactions into locals and remotes
	localTxs := make(map[common.Address]types.Transactions)
	remoteTxs := pending
	for _, account := range w.eth.TxPool().Locals() {
		if txs := remoteTxs[account]; len(txs) > 0 {
			delete(remoteTxs, account)
			localTxs[account] = txs
		}
	}
	if len(localTxs) > 0 {
		txs := types.NewTransactionsByPriceAndNonce(env.signer, localTxs, header.BaseFee)
		w.commitTransactions(env, txs, w.coinbase)
	}
	if len(remoteTxs) > 0 {
		txs := types.NewTransactionsByPriceAndNonce(env.signer, remoteTxs, header.BaseFee)
		w.commitTransactions(env, txs, w.coinbase)
	}

	return w.commit(env)
}

func (w *worker) createCurrentEnvironment(parent *types.Block, header *types.Header, tstart time.Time) (*environment, error) {
	state, err := w.chain.StateAt(parent.Root())
	if err != nil {
		return nil, err
	}
	return &environment{
		signer:  types.MakeSigner(w.chainConfig, header.Number, new(big.Int).SetUint64(header.Time)),
		state:   state,
		header:  header,
		tcount:  0,
		gasPool: new(core.GasPool).AddGas(header.GasLimit),
		start:   tstart,
	}, nil
}

func (w *worker) commitTransaction(env *environment, tx *types.Transaction, coinbase common.Address) ([]*types.Log, error) {
	snap := env.state.Snapshot()

	receipt, err := core.ApplyTransaction(w.chainConfig, w.chain, &coinbase, env.gasPool, env.state, env.header, tx, &env.header.GasUsed, *w.chain.GetVMConfig())
	if err != nil {
		env.state.RevertToSnapshot(snap)
		return nil, err
	}
	env.txs = append(env.txs, tx)
	env.receipts = append(env.receipts, receipt)

	return receipt.Logs, nil
}

func (w *worker) commitTransactions(env *environment, txs *types.TransactionsByPriceAndNonce, coinbase common.Address) {
	for {
		// If we don't have enough gas for any further transactions then we're done
		if env.gasPool.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "have", env.gasPool, "want", params.TxGas)
			break
		}
		// Retrieve the next transaction and abort if all done
		tx := txs.Peek()
		if tx == nil {
			break
		}
		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		//
		// We use the eip155 signer regardless of the current hf.
		from, _ := types.Sender(env.signer, tx)
		// Check whether the tx is replay protected. If we're not in the EIP155 hf
		// phase, start ignoring the sender until we do.
		if tx.Protected() && !w.chainConfig.IsEIP155(env.header.Number) {
			log.Trace("Ignoring reply protected transaction", "hash", tx.Hash(), "eip155", w.chainConfig.EIP155Block)

			txs.Pop()
			continue
		}
		// Start executing the transaction
		env.state.Prepare(tx.Hash(), env.tcount)

		_, err := w.commitTransaction(env, tx, coinbase)
		switch {
		case errors.Is(err, core.ErrGasLimitReached):
			// Pop the current out-of-gas transaction without shifting in the next from the account
			log.Trace("Gas limit exceeded for current block", "sender", from)
			txs.Pop()

		case errors.Is(err, core.ErrNonceTooLow):
			// New head notification data race between the transaction pool and miner, shift
			log.Trace("Skipping transaction with low nonce", "sender", from, "nonce", tx.Nonce())
			txs.Shift()

		case errors.Is(err, core.ErrNonceTooHigh):
			// Reorg notification data race between the transaction pool and miner, skip account =
			log.Trace("Skipping account with high nonce", "sender", from, "nonce", tx.Nonce())
			txs.Pop()

		case errors.Is(err, nil):
			env.tcount++
			txs.Shift()

		case errors.Is(err, core.ErrTxTypeNotSupported):
			// Pop the unsupported transaction without shifting in the next from the account
			log.Trace("Skipping unsupported transaction type", "sender", from, "type", tx.Type())
			txs.Pop()

		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
			txs.Shift()
		}
	}
}

// commit runs any post-transaction state modifications, assembles the final block
// and commits new work if consensus engine is running.
func (w *worker) commit(env *environment) (*types.Block, error) {
	// Deep copy receipts here to avoid interaction between different tasks.
	receipts := copyReceipts(env.receipts)
	block, err := w.engine.FinalizeAndAssemble(w.chain, env.header, env.state, env.txs, nil, receipts)
	if err != nil {
		return nil, err
	}

	return w.handleResult(env, block, time.Now(), receipts)
}

func (w *worker) handleResult(env *environment, block *types.Block, createdAt time.Time, unfinishedReceipts []*types.Receipt) (*types.Block, error) {
	// Short circuit when receiving duplicate result caused by resubmitting.
	if w.chain.HasBlock(block.Hash(), block.NumberU64()) {
		return nil, fmt.Errorf("produced duplicate block (Hash: %s, Number %d)", block.Hash(), block.NumberU64())
	}
	var (
		hash = block.Hash()
	)
	// Different block could share same sealhash, deep copy here to prevent write-write conflict.
	var (
		receipts = make([]*types.Receipt, len(unfinishedReceipts))
		logs     []*types.Log
	)
	for i, receipt := range unfinishedReceipts {
		// add block location fields
		receipt.BlockHash = hash
		receipt.BlockNumber = block.Number()
		receipt.TransactionIndex = uint(i)

		receipts[i] = new(types.Receipt)
		*receipts[i] = *receipt
		// Update the block hash in all logs since it is now available and not when the
		// receipt/log of individual transactions were created.
		for _, log := range receipt.Logs {
			log.BlockHash = hash
		}
		logs = append(logs, receipt.Logs...)
	}

	log.Info("Commit new mining work", "number", block.Number(), "hash", hash, "uncles", 0, "txs", env.tcount,
		"gas", block.GasUsed(), "fees", totalFees(block, receipts), "elapsed", common.PrettyDuration(time.Since(env.start)))

	// Note: the miner no longer emits a NewMinedBlock event. Instead the caller
	// is responsible for running any additional verification and then inserting
	// the block with InsertChain, which will also emit a new head event.
	return block, nil
}

// copyReceipts makes a deep copy of the given receipts.
func copyReceipts(receipts []*types.Receipt) []*types.Receipt {
	result := make([]*types.Receipt, len(receipts))
	for i, l := range receipts {
		cpy := *l
		result[i] = &cpy
	}
	return result
}

// totalFees computes total consumed fees in ETH. Block transactions and receipts have to have the same order.
func totalFees(block *types.Block, receipts []*types.Receipt) *big.Float {
	feesWei := new(big.Int)
	for i, tx := range block.Transactions() {
		feesWei.Add(feesWei, new(big.Int).Mul(new(big.Int).SetUint64(receipts[i].GasUsed), tx.GasPrice()))
	}
	return new(big.Float).Quo(new(big.Float).SetInt(feesWei), new(big.Float).SetInt(big.NewInt(params.Ether)))
}
