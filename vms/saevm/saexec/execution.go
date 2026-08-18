// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/proxytime"
)

var (
	errExecutorClosed             = errors.New("saexec.Executor closed")
	errTransactionCountOutOfRange = errors.New("transaction count out of range")
)

// queuedBlock pairs a queued block with the time it was enqueued so that
// [Executor.processQueue] can record how long it spent in the queue, from
// acceptance until its execution completed.
type queuedBlock struct {
	block      *blocks.Block
	enqueuedAt time.Time
}

// Enqueue pushes a new block to the FIFO queue. If [Executor.Close] is called
// before [blocks.Block.Executed] returns true then there is no guarantee that
// the block will be executed.
func (e *Executor) Enqueue(ctx context.Context, block *blocks.Block) error {
	e.createReceiptBuffers(block)

	select {
	case e.queue <- queuedBlock{block: block, enqueuedAt: time.Now()}:
		e.metrics.markEnqueued(block)
		if n := len(e.queue); n == cap(e.queue) {
			// If this happens then increase the channel's buffer size.
			e.log.Warn(
				"Execution queue buffer full",
				zap.Uint64("block_height", block.Height()),
				zap.Int("queue_capacity", n),
			)
		}
		return nil

	case <-ctx.Done():
		return ctx.Err()
	case <-e.quit:
		return errExecutorClosed
	case <-e.done:
		// `e.done` can also close due to [Executor.execute] errors.
		return errExecutorClosed
	}
}

const emergencyPlaybookLink = "https://github.com/ava-labs/avalanchego/issues/5276"

func (e *Executor) processQueue() {
	defer close(e.done)

	for {
		select {
		case <-e.quit:
			return

		case qb := <-e.queue:
			block := qb.block
			log := e.log.With(
				zap.Uint64("block_height", block.Height()),
				zap.Uint64("block_time", block.BuildTime()),
				zap.Stringer("block_hash", block.Hash()),
				zap.Int("tx_count", len(block.Transactions())),
			)

			err := e.execute(block, log)
			switch {
			case errors.Is(err, errFatal):
				log.Fatal( //nolint:gocritic // False positive, will not terminate the process
					"Block execution failed",
					zap.String("playbook", emergencyPlaybookLink),
					zap.Error(err),
				)
			case err != nil:
				log.Error(
					"Error of unknown severity in block execution",
					zap.String("if_escalation_required", emergencyPlaybookLink),
					zap.Error(err),
				)
			}
			if err != nil {
				return
			}
			e.metrics.observeQueueDuration(time.Since(qb.enqueuedAt))
		}
	}
}

var errFatal = errors.New("fatal execution error")

func (e *Executor) execute(b *blocks.Block, log logging.Logger) error {
	// If the VM were to encounter an error after enqueuing the block, we would
	// receive the same block twice for execution should consensus retry
	// acceptance.
	if last := e.lastExecuted.Load().Hash(); last != b.ParentHash() {
		return fmt.Errorf("executing block built on parent %#x when last executed %#x", b.ParentHash(), last)
	}

	start := time.Now()
	defer func() {
		e.metrics.observeExecuteDuration(time.Since(start))
	}()
	stateDB, err := e.StateDB(b.ParentBlock().PostExecutionStateRoot())
	if err != nil {
		return err
	}
	result, err := e.executeBlock(b, stateDB, true /*canonical*/, log)
	if err != nil {
		return err
	}
	return e.afterExecution(b, stateDB, result)
}

type (
	// blockContext holds the values that [Executor.beforeTransactions]
	// derives. The later phases of execution require all of them.
	blockContext struct {
		BlockExecutionState
		// header carries the executed base fee in place of the worst-case fee
		// that consensus agreed. Later phases MUST use this header, because
		// [blocks.Block.Header] returns a new copy on every call.
		header      *types.Header
		gasClock    *gastime.Time
		interimTime *proxytime.Time[gas.Gas]
	}

	// executionResults holds the outputs of executing an entire block.
	executionResults struct {
		BaseFee     *uint256.Int
		Receipts    types.Receipts
		GasConsumed gas.Gas
		FinishBy    finishTimes
	}

	// finishTimes records when a block finished executing, on both the gas
	// clock and the wall clock.
	finishTimes struct {
		Gas  *gastime.Time
		Wall time.Time
	}

	// BlockExecutionState holds the state and execution context at a point in
	// block execution.
	BlockExecutionState struct {
		BaseFee  *uint256.Int
		Signer   types.Signer
		BlockCtx vm.BlockContext
	}
)

// executeBlock applies every deterministic state change in b to stateDB, in
// the three phases of block execution:
//
//  1. [Executor.beforeTransactions]
//  2. [Executor.executeTransactions], for all of b's transactions
//  3. [Executor.endOfBlock]
//
// Only canonical execution publishes receipts and records block progress.
// Canonical-only side effects belong in [Executor.afterExecution].
func (e *Executor) executeBlock(b *blocks.Block, stateDB *state.StateDB, canonical bool, log logging.Logger) (*executionResults, error) {
	log.Trace("Executing block")

	bc, err := e.beforeTransactions(b, stateDB)
	if err != nil {
		return nil, err
	}
	receipts, gasConsumed, err := e.executeTransactions(b, stateDB, bc, len(b.Transactions()), canonical)
	if err != nil {
		return nil, err
	}
	return e.endOfBlock(b, stateDB, bc, receipts, gasConsumed, canonical, log)
}

// beforeTransactions performs the steps that b requires before any of its
// transactions can execute:
//
//  1. It advances the parent's gas clock to the start of b.
//  2. It applies b's pre-transaction state changes to stateDB.
//  3. It replaces b's worst-case base fee with the fee that the gas clock
//     reached.
//
// stateDB MUST represent b's parent's post-execution state.
func (e *Executor) beforeTransactions(b *blocks.Block, stateDB *state.StateDB) (*blockContext, error) {
	header := b.Header()

	gasClock := b.ParentBlock().ExecutedByGasTime()
	gasClock.BeforeBlock(e.hooks.BlockTime(header))

	if err := e.StateBeforeTransactions(b, stateDB); err != nil {
		return nil, err
	}

	baseFee := gasClock.BaseFee()
	if hook.Synchronous(e.hooks, header) {
		baseFee = b.WorstCaseBaseFee()
	} else {
		b.CheckBaseFeeBound(baseFee)
	}
	header.BaseFee = baseFee.ToBig()

	return &blockContext{
		BlockExecutionState: BlockExecutionState{
			BaseFee:  baseFee,
			Signer:   b.Signer(e.chainConfig),
			BlockCtx: core.NewEVMBlockContext(header, e.chainContext, &header.Coinbase),
		},
		header:      header,
		gasClock:    gasClock,
		interimTime: gasClock.Time.Clone(),
	}, nil
}

// executeTransactions executes b's first numTxs transactions against stateDB
// and performs no end-of-block operation. [Executor.beforeTransactions] MUST
// apply b's pre-transaction changes to stateDB first.
//
// Only canonical execution publishes receipts and records block progress.
func (e *Executor) executeTransactions(
	b *blocks.Block,
	stateDB *state.StateDB,
	bc *blockContext,
	numTxs int,
	canonical bool,
) (types.Receipts, gas.Gas, error) {
	header := bc.header
	gasPool := core.GasPool(math.MaxUint64) // required by geth but irrelevant so max it out
	var gasConsumed gas.Gas

	txs := b.Transactions()[:numTxs]
	receipts := make(types.Receipts, len(txs))

	for ti, tx := range txs {
		stateDB.SetTxContext(tx.Hash(), ti)
		b.CheckSenderBalanceBound(stateDB, bc.Signer, tx)

		// Executes the transaction and calls [state.StateDB.Finalise].
		receipt, err := core.ApplyTransaction(
			e.chainConfig,
			e.chainContext,
			&header.Coinbase,
			&gasPool,
			stateDB,
			header,
			tx,
			(*uint64)(&gasConsumed),
			vm.Config{},
		)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: transaction execution errored (not reverted) [%d](%#x): %v", errFatal, ti, tx.Hash(), err)
		}

		bc.interimTime.Tick(gas.Gas(receipt.GasUsed))
		if canonical {
			// Reporting progress allows settlement to proceed while the block
			// is still executing.
			b.SetInterimExecutionTime(bc.interimTime)
			// TODO(arr4n) investigate calling the same method on pending blocks
			// in the queue. It's only worth it if [blocks.LastToSettleAt]
			// regularly returns false, meaning that execution is blocking
			// consensus.
		}

		// The [types.Header] that we pass to [core.ApplyTransaction] is
		// modified to reduce gas price from the worst-case value agreed by
		// consensus. This changes the hash, which is what is copied to receipts
		// and logs.
		//
		// [core.ApplyTransaction] also doesn't set [types.Receipt.EffectiveGasPrice].
		// Fixing both here avoids needing to call [types.Receipt.DeriveFields].
		receipt.BlockHash = b.Hash()
		for _, l := range receipt.Logs {
			l.BlockHash = b.Hash()
		}
		tip := tx.EffectiveGasTipValue(header.BaseFee)
		receipt.EffectiveGasPrice = tip.Add(header.BaseFee, tip)

		if canonical {
			if r, ok := e.receipts.Load(tx.Hash()); ok {
				r.Put(&Receipt{receipt, bc.Signer, tx})
			}
		}
		receipts[ti] = receipt
	}

	return receipts, gasConsumed, nil
}

// endOfBlock applies b's end-of-block operations to stateDB. It then stops
// both of b's clocks and returns the results of executing b in full. Only
// canonical execution records block progress.
//
// receipts and gasConsumed MUST cover every one of b's transactions.
func (e *Executor) endOfBlock(
	b *blocks.Block,
	stateDB *state.StateDB,
	bc *blockContext,
	receipts types.Receipts,
	gasConsumed gas.Gas,
	canonical bool,
	log logging.Logger,
) (*executionResults, error) {
	ops, err := e.hooks.EndOfBlockOps(b.EthBlock())
	if err != nil {
		return nil, fmt.Errorf("%w: %T.EndOfBlockOps(%#x): %v", errFatal, e.hooks, b.Hash(), err)
	}

	numTxs := len(b.Transactions())
	for i, o := range ops {
		b.CheckOpBurnerBalanceBounds(stateDB, numTxs+i, o)
		gasConsumed += o.Gas
		bc.interimTime.Tick(o.Gas)
		if canonical {
			b.SetInterimExecutionTime(bc.interimTime)
		}

		if err := o.ApplyTo(stateDB); err != nil {
			return nil, fmt.Errorf("%w: applying end-of-block operation [%d](%v): %v", errFatal, i, o.ID, err)
		}
	}

	if err := e.hooks.FinishExecutingBlock(stateDB, b.EthBlock(), receipts); err != nil {
		return nil, fmt.Errorf("finish-executing-block hook: %v", err)
	}

	target, gasCfg := e.hooks.GasConfigAfter(b.Header())
	if err := bc.gasClock.AfterBlock(gasConsumed, target, gasCfg); err != nil {
		return nil, fmt.Errorf("after-block gas time update: %w", err)
	}

	r := &executionResults{
		BaseFee:     bc.BaseFee,
		Receipts:    receipts,
		GasConsumed: gasConsumed,
		FinishBy: finishTimes{
			Gas:  bc.gasClock,
			Wall: time.Now(),
		},
	}
	log.Trace(
		"Block execution complete",
		zap.Uint64("gas_consumed", uint64(r.GasConsumed)),
		zap.Time("gas_time", r.FinishBy.Gas.AsTime()),
		zap.Time("wall_time", r.FinishBy.Wall),
	)
	return r, nil
}

func (e *Executor) afterExecution(b *blocks.Block, stateDB *state.StateDB, r *executionResults) error {
	if err := e.hooks.AfterExecutingBlock(b.EthBlock(), r.Receipts); err != nil {
		return fmt.Errorf("after-executing-block hook: %v", err)
	}

	e.chainContext.recent.Put(b.NumberU64(), b.Header())

	root, err := stateDB.Commit(b.NumberU64(), true)
	if err != nil {
		return fmt.Errorf("%T.Commit() at end of block %d: %w", stateDB, b.NumberU64(), err)
	}
	if err := e.Tracker.MaybeCommit(b.SettledStateRoot(), root, b.NumberU64()); err != nil {
		return err
	}

	// Responsibility for untracking lies with the VM once it deems this block's
	// post-execution state to no longer be consensus-critical.
	e.Tracker.Track(root)

	// The strict ordering of the next 3 calls guarantees invariants that MUST
	// NOT be broken:
	//
	// 1. [blocks.Block.MarkExecuted] guarantees disk then in-memory changes.
	// 2. Internal indicator of last executed MUST follow in-memory change.
	// 3. External indicator of last executed MUST follow internal indicator.
	if err := b.MarkExecuted(e.db, e.xdb, r.FinishBy.Gas.Clone(), r.FinishBy.Wall, r.BaseFee.ToBig(), r.Receipts, root, &e.lastExecuted /* (2) */); err != nil {
		return err
	}
	e.sendPostExecutionEvents(b, r) // (3)
	return nil
}

// StateBeforeTransactions applies b's pre-transaction state changes to
// stateDB: the start-executing-block hook, then the EIP-4788 beacon root.
// This mirrors [core.StateProcessor.Process]. stateDB MUST represent b's
// parent's post-execution state.
//
// It executes no transaction and no end-of-block operation, and it leaves b's
// worst-case base fee in place.
func (e *Executor) StateBeforeTransactions(b *blocks.Block, stateDB *state.StateDB) error {
	header := b.Header()
	rules := e.chainConfig.Rules(header.Number, true /*isMerge*/, header.Time)
	if err := e.hooks.StartExecutingBlock(rules, stateDB, b.ParentBlock().Header(), b.EthBlock()); err != nil {
		return fmt.Errorf("start-executing-block hook: %v", err)
	}

	core.SetBeaconBlockRoot(stateDB, header)

	// SetBeaconRoot only finalizes when it applies the root, so we want to
	// finalize last. This mirrors the finalization performed by
	// [core.ApplyTransaction].
	stateDB.Finalise(rules.IsEIP158)
	return nil
}

// ExecuteBlockUntil applies b's pre-transaction state changes to stateDB and
// then executes b's first numTxs transactions. It returns the context in
// which the next transaction would execute.
//
// It skips b's remaining transactions and all of its end-of-block operations.
// It records no block progress and does not mark b as executed.
//
// numTxs MUST be in the range [0, len(b.Transactions())].
func (e *Executor) ExecuteBlockUntil(b *blocks.Block, stateDB *state.StateDB, numTxs int) (*BlockExecutionState, error) {
	if numTxs < 0 || numTxs > len(b.Transactions()) {
		return nil, fmt.Errorf("%w: %d not in [0, %d]", errTransactionCountOutOfRange, numTxs, len(b.Transactions()))
	}
	bc, err := e.beforeTransactions(b, stateDB)
	if err != nil {
		return nil, err
	}
	if _, _, err := e.executeTransactions(b, stateDB, bc, numTxs, false /*canonical*/); err != nil {
		return nil, err
	}
	return &bc.BlockExecutionState, nil
}

// ExecuteHistoricalBlock executes every deterministic phase of b against
// stateDB. stateDB MUST represent b's parent's post-execution state.
//
// It records no block progress and publishes no canonical execution artifacts.
func (e *Executor) ExecuteHistoricalBlock(b *blocks.Block, stateDB *state.StateDB) error {
	_, err := e.executeBlock(b, stateDB, false /*canonical*/, e.log)
	return err
}
