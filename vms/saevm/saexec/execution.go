// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm/eventual"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

var errExecutorClosed = errors.New("saexec.Executor closed")

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
	result, err := e.executeBlock(b, stateDB, e.receipts, log, true)
	if err != nil {
		return err
	}
	return e.afterExecution(b, result)
}

type (
	// receiptStore receives per-transaction receipts during block execution.
	// Only canonical execution provides a real implementation.
	receiptStore interface {
		Load(common.Hash) (eventual.Value[*Receipt], bool)
	}

	// ExecutionResults holds block execution outputs.
	ExecutionResults struct {
		BaseFee     *uint256.Int
		StateDB     *state.StateDB
		Signer      types.Signer
		BlockCtx    vm.BlockContext
		Receipts    types.Receipts
		GasConsumed gas.Gas
		FinishBy    struct {
			Gas  *gastime.Time
			Wall time.Time
		}
	}
)

// startExecutingBlock applies the state changes required before executing b's
// transactions, specifically the start-executing-block hook and the EIP-4788
// beacon root, mirroring [core.StateProcessor.Process].
func startExecutingBlock(hooks hook.Points, rules params.Rules, stateDB *state.StateDB, parent *types.Header, b *types.Block) error {
	if err := hooks.StartExecutingBlock(rules, stateDB, parent, b); err != nil {
		return fmt.Errorf("start-executing-block hook: %v", err)
	}

	core.SetBeaconBlockRoot(stateDB, b.Header())

	// SetBeaconRoot only finalizes when it applies the root, so we want to
	// finalize last. This mirrors the finalization performed by
	// [core.ApplyTransaction].
	stateDB.Finalise(rules.IsEIP158)
	return nil
}

// ExecuteBlock executes all deterministic state changes in b against stateDB.
// It does not commit stateDB or run canonical-only hooks.
func (e *Executor) ExecuteBlock(b *blocks.Block, stateDB *state.StateDB) (*ExecutionResults, error) {
	return e.executeBlock(b, stateDB, nullReceiptStore{}, e.log, false)
}

// ExecuteTransactionPrefix executes the first numTxs transactions in b against
// stateDB. It does not run end-of-block operations or finish-executing-block
// hooks.
func (e *Executor) ExecuteTransactionPrefix(b *blocks.Block, stateDB *state.StateDB, numTxs int) (*ExecutionResults, error) {
	if numTxs < 0 || numTxs > len(b.Transactions()) {
		return nil, fmt.Errorf("transaction count %d out of range [0, %d]", numTxs, len(b.Transactions()))
	}
	return e.executeTransactions(b, stateDB, numTxs, nullReceiptStore{}, e.log, false)
}

// executeBlock executes all deterministic block state changes.
func (e *Executor) executeBlock(
	b *blocks.Block,
	stateDB *state.StateDB,
	receiptStore receiptStore,
	log logging.Logger,
	recordProgress bool,
) (*ExecutionResults, error) {
	r, err := e.executeTransactions(b, stateDB, len(b.Transactions()), receiptStore, log, recordProgress)
	if err != nil {
		return nil, err
	}

	numTxs := len(b.Transactions())
	interimExecutionTime := r.FinishBy.Gas.Time.Clone()
	interimExecutionTime.Tick(r.GasConsumed)
	ops, err := e.hooks.EndOfBlockOps(b.EthBlock())
	if err != nil {
		return nil, fmt.Errorf("%w: %T.EndOfBlockOps(%#x): %v", errFatal, e.hooks, b.Hash(), err)
	}
	for i, o := range ops {
		b.CheckOpBurnerBalanceBounds(stateDB, numTxs+i, o)
		r.GasConsumed += o.Gas
		interimExecutionTime.Tick(o.Gas)
		if recordProgress {
			b.SetInterimExecutionTime(interimExecutionTime)
		}

		if err := o.ApplyTo(stateDB); err != nil {
			return nil, fmt.Errorf("%w: applying end-of-block operation [%d](%v): %v", errFatal, i, o.ID, err)
		}
	}

	if err := e.hooks.FinishExecutingBlock(stateDB, b.EthBlock(), r.Receipts); err != nil {
		return nil, fmt.Errorf("finish-executing-block hook: %v", err)
	}

	target, gasCfg := e.hooks.GasConfigAfter(b.Header())
	if err := r.FinishBy.Gas.AfterBlock(r.GasConsumed, target, gasCfg); err != nil {
		return nil, fmt.Errorf("after-block gas time update: %w", err)
	}

	r.FinishBy.Wall = time.Now()
	log.Trace(
		"Block execution complete",
		zap.Uint64("gas_consumed", uint64(r.GasConsumed)),
		zap.Time("gas_time", r.FinishBy.Gas.AsTime()),
		zap.Time("wall_time", r.FinishBy.Wall),
	)
	return r, nil
}

// executeTransactions executes the first numTxs transactions in b.
//
// The gas clock and base fee come from the parent's post-execution clock,
// except pre-SAE blocks, which use their own header's fee.
func (e *Executor) executeTransactions(
	b *blocks.Block,
	stateDB *state.StateDB,
	numTxs int,
	receiptStore receiptStore,
	log logging.Logger,
	recordProgress bool,
) (*ExecutionResults, error) {
	log.Trace("Executing block")

	parent := b.ParentBlock()
	header := b.Header()

	gasClock := parent.ExecutedByGasTime()
	gasClock.BeforeBlock(e.hooks.BlockTime(header))
	perTxClock := gasClock.Time.Clone()

	rules := e.chainConfig.Rules(header.Number, true /*isMerge*/, header.Time)
	if err := startExecutingBlock(e.hooks, rules, stateDB, parent.Header(), b.EthBlock()); err != nil {
		return nil, err
	}

	baseFee := gasClock.BaseFee()
	if hook.Synchronous(e.hooks, header) {
		baseFee = b.WorstCaseBaseFee()
	} else {
		b.CheckBaseFeeBound(baseFee)
	}
	header.BaseFee = baseFee.ToBig()

	signer := b.Signer(e.chainConfig)
	gasPool := core.GasPool(math.MaxUint64) // required by geth but irrelevant so max it out
	var blockGasConsumed gas.Gas

	txs := b.Transactions()[:numTxs]
	receipts := make(types.Receipts, len(txs))

	for ti, tx := range txs {
		stateDB.SetTxContext(tx.Hash(), ti)
		b.CheckSenderBalanceBound(stateDB, signer, tx)

		// Executes the transaction and calls [state.StateDB.Finalise].
		receipt, err := core.ApplyTransaction(
			e.chainConfig,
			e.chainContext,
			&header.Coinbase,
			&gasPool,
			stateDB,
			header,
			tx,
			(*uint64)(&blockGasConsumed),
			vm.Config{},
		)
		if err != nil {
			return nil, fmt.Errorf("%w: transaction execution errored (not reverted) [%d](%#x): %v", errFatal, ti, tx.Hash(), err)
		}

		perTxClock.Tick(gas.Gas(receipt.GasUsed))
		if recordProgress {
			b.SetInterimExecutionTime(perTxClock)
		}
		// TODO(arr4n) investigate calling the same method on pending blocks in
		// the queue. It's only worth it if [blocks.LastToSettleAt] regularly
		// returns false, meaning that execution is blocking consensus.

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

		if r, ok := receiptStore.Load(tx.Hash()); ok {
			r.Put(&Receipt{receipt, signer, tx})
		}
		receipts[ti] = receipt
	}

	r := &ExecutionResults{
		BaseFee:     baseFee,
		StateDB:     stateDB,
		Signer:      signer,
		BlockCtx:    core.NewEVMBlockContext(header, e.chainContext, &header.Coinbase),
		Receipts:    receipts,
		GasConsumed: blockGasConsumed,
	}
	r.FinishBy.Gas = gasClock
	return r, nil
}

func (e *Executor) afterExecution(b *blocks.Block, r *ExecutionResults) error {
	if err := e.hooks.AfterExecutingBlock(b.EthBlock(), r.Receipts); err != nil {
		return fmt.Errorf("after-executing-block hook: %v", err)
	}

	e.chainContext.recent.Put(b.NumberU64(), b.Header())

	root, err := r.StateDB.Commit(b.NumberU64(), true)
	if err != nil {
		return fmt.Errorf("%T.Commit() at end of block %d: %w", r.StateDB, b.NumberU64(), err)
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

// nullReceiptStore discards receipt notifications.
type nullReceiptStore struct{}

var _ receiptStore = nullReceiptStore{}

// Load always returns the zero value and false.
func (nullReceiptStore) Load(common.Hash) (eventual.Value[*Receipt], bool) {
	return eventual.Value[*Receipt]{}, false
}
