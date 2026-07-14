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
	"github.com/ava-labs/avalanchego/vms/saevm/proxytime"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
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
		e.metrics.markEnqueued(block.EthBlock().GasLimit())
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

const emergencyPlaybookLink = "https://github.com/ava-labs/strevm/issues/28"

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
	result, err := Execute(b, e, e.hooks, e.chainConfig, e.chainContext, e.receipts, log)
	if err != nil {
		return err
	}
	return e.afterExecution(b, result)
}

type (
	// ReceiptStore receives per-transaction receipts during block execution.
	// Only the [Executor] needs to provide a real implementation to [Execute]
	// and all other callers MUST use [NullReceiptStore].
	ReceiptStore interface {
		Load(common.Hash) (eventual.Value[*Receipt], bool)
	}

	// ExecutionResults holds the outputs of [Execute].
	ExecutionResults struct {
		BaseFee     *uint256.Int
		StateDB     *state.StateDB
		Receipts    types.Receipts
		GasConsumed gas.Gas
		FinishBy    struct {
			Gas  *gastime.Time
			Wall time.Time
		}
	}
)

// Execute executes all transactions in the [blocks.Block] followed by
// end-of-block processing, equivalent to driving an [Execution] to completion.
// See [NewExecution] for restrictions on `b`.
func Execute(
	b *blocks.Block,
	sdbo saedb.StateDBOpener,
	hooks hook.Points,
	config *params.ChainConfig,
	chainCtx core.ChainContext,
	receiptStore ReceiptStore,
	log logging.Logger,
) (*ExecutionResults, error) {
	e, err := NewExecution(b, sdbo, hooks, config, chainCtx, receiptStore, log, nil)
	if err != nil {
		return nil, err
	}
	for range b.Transactions() {
		if _, err := e.ExecuteNextTransaction(vm.Config{}); err != nil {
			return nil, fmt.Errorf("%w: %v", errFatal, err)
		}
	}
	return e.Finish()
}

// An Execution executes a single block, transaction by transaction, beginning
// from the post-execution state of the [blocks.Block.ParentBlock].
//
// Methods MUST NOT be called concurrently.
type Execution struct {
	b            *blocks.Block
	hooks        hook.Points
	config       *params.ChainConfig
	chainCtx     core.ChainContext
	receiptStore ReceiptStore
	log          logging.Logger

	header      *types.Header
	stateDB     *state.StateDB
	gasClock    *gastime.Time
	perTxClock  *proxytime.Time[gas.Gas]
	baseFee     *uint256.Int
	signer      types.Signer
	isEIP158    bool
	gasPool     core.GasPool
	gasConsumed gas.Gas
	receipts    types.Receipts
}

// NewExecution performs all block-level setup preceding transaction execution:
// the before-block hook, base-fee derivation, and the EIP-4788 beacon-root
// write.
//
// Although an Execution never calls [blocks.Block.MarkExecuted], it does
// mutate consensus-critical internal values of `b` (e.g. interim execution
// time). A "live" accepted block (as against one recovered from the database)
// MUST NOT be passed to NewExecution, only to [Executor.Enqueue].
func NewExecution(
	b *blocks.Block,
	sdbo saedb.StateDBOpener,
	hooks hook.Points,
	config *params.ChainConfig,
	chainCtx core.ChainContext,
	receiptStore ReceiptStore,
	log logging.Logger,
	baseFee *uint256.Int,
) (*Execution, error) {
	log.Trace("Executing block")

	parent := b.ParentBlock()
	header := b.Header()

	gasClock := parent.ExecutedByGasTime().Clone()
	gasClock.BeforeBlock(hooks.BlockTime(header))
	perTxClock := gasClock.Time.Clone()

	stateDB, err := sdbo.StateDB(parent.PostExecutionStateRoot())
	if err != nil {
		return nil, err
	}

	if err := hooks.BeforeExecutingBlock(stateDB, parent.Header(), header); err != nil {
		return nil, fmt.Errorf("before-block hook: %v", err)
	}

	if baseFee == nil {
		baseFee = gasClock.BaseFee()
	}
	b.CheckBaseFeeBound(baseFee)
	header.BaseFee = baseFee.ToBig()

	// EIP-4788: before processing any transactions, store the parent beacon
	// block root, mirroring [core.StateProcessor.Process].
	core.SetBeaconBlockRoot(stateDB, header)

	return &Execution{
		b:            b,
		hooks:        hooks,
		config:       config,
		chainCtx:     chainCtx,
		receiptStore: receiptStore,
		log:          log,
		header:       header,
		stateDB:      stateDB,
		gasClock:     gasClock,
		perTxClock:   perTxClock,
		baseFee:      baseFee,
		signer:       b.Signer(config),
		isEIP158:     config.IsEIP158(b.Number()),
		gasPool:      core.GasPool(math.MaxUint64), // required by geth but irrelevant so max it out
		receipts:     make(types.Receipts, 0, len(b.Transactions())),
	}, nil
}

var errNoRemainingTransactions = errors.New("no remaining transactions to execute")

// ExecuteNextTransaction executes the block's next unexecuted transaction. A
// [vm.Config.Tracer] MAY be attached and it observes the same execution that
// advances the block's state.
func (e *Execution) ExecuteNextTransaction(vmCfg vm.Config) (*types.Receipt, error) {
	ti := len(e.receipts)
	txs := e.b.Transactions()
	if ti >= len(txs) {
		return nil, fmt.Errorf("%w: all %d executed", errNoRemainingTransactions, len(txs))
	}
	tx := txs[ti]

	e.stateDB.SetTxContext(tx.Hash(), ti)
	e.b.CheckSenderBalanceBound(e.stateDB, e.signer, tx)

	receipt, err := core.ApplyTransaction(
		e.config,
		e.chainCtx,
		&e.header.Coinbase,
		&e.gasPool,
		e.stateDB,
		e.header,
		tx,
		(*uint64)(&e.gasConsumed),
		vmCfg,
	)
	if err != nil {
		return nil, fmt.Errorf("transaction execution errored (not reverted) [%d](%#x): %v", ti, tx.Hash(), err)
	}

	e.perTxClock.Tick(gas.Gas(receipt.GasUsed))
	e.b.SetInterimExecutionTime(e.perTxClock)
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
	blockHash := e.b.Hash()
	receipt.BlockHash = blockHash
	for _, l := range receipt.Logs {
		l.BlockHash = blockHash
	}
	tip := tx.EffectiveGasTipValue(e.header.BaseFee)
	receipt.EffectiveGasPrice = tip.Add(e.header.BaseFee, tip)

	if r, ok := e.receiptStore.Load(tx.Hash()); ok {
		r.Put(&Receipt{receipt, e.signer, tx})
	}
	e.receipts = append(e.receipts, receipt)

	if err := e.hooks.AfterExecutingTransaction(e.stateDB, *e.baseFee, receipt); err != nil {
		return nil, fmt.Errorf("after-transaction hook [%d](%#x): %w", ti, tx.Hash(), err)
	}
	// Finalise any state changes made by the hook as part of this
	// transaction, mirroring the finalisation performed by
	// [core.ApplyTransaction].
	e.stateDB.Finalise(e.isEIP158)

	return receipt, nil
}

var errUnexecutedTransactions = errors.New("unexecuted transactions at end of block")

// Finish applies end-of-block operations, runs the after-block hook, updates
// the gas clock, and returns the block's execution results. Every transaction
// MUST already have been executed, and Finish MUST be called at most once.
func (e *Execution) Finish() (*ExecutionResults, error) {
	b := e.b

	numTxs := len(b.Transactions())
	if got := len(e.receipts); got != numTxs {
		return nil, fmt.Errorf("%w: %d of %d executed", errUnexecutedTransactions, got, numTxs)
	}

	ops, err := e.hooks.EndOfBlockOps(b.EthBlock())
	if err != nil {
		return nil, fmt.Errorf("%w: %T.EndOfBlockOps(%#x): %v", errFatal, e.hooks, b.Hash(), err)
	}
	for i, o := range ops {
		b.CheckOpBurnerBalanceBounds(e.stateDB, numTxs+i, o)
		e.gasConsumed += o.Gas
		e.perTxClock.Tick(o.Gas)
		b.SetInterimExecutionTime(e.perTxClock)

		if err := o.ApplyTo(e.stateDB); err != nil {
			return nil, fmt.Errorf("%w: applying end-of-block operation [%d](%v): %v", errFatal, i, o.ID, err)
		}
	}

	if err := e.hooks.AfterExecutingBlock(e.stateDB, b.EthBlock(), e.receipts); err != nil {
		return nil, fmt.Errorf("after-block hook: %v", err)
	}

	endTime := time.Now()
	target, gasCfg := e.hooks.GasConfigAfter(b.Header())
	if err := e.gasClock.AfterBlock(e.gasConsumed, target, gasCfg); err != nil {
		return nil, fmt.Errorf("after-block gas time update: %w", err)
	}

	e.log.Trace(
		"Block execution complete",
		zap.Uint64("gas_consumed", uint64(e.gasConsumed)),
		zap.Time("gas_time", e.gasClock.AsTime()),
		zap.Time("wall_time", endTime),
	)

	r := &ExecutionResults{
		BaseFee:     e.baseFee,
		StateDB:     e.stateDB,
		Receipts:    e.receipts,
		GasConsumed: e.gasConsumed,
	}
	r.FinishBy.Gas = e.gasClock
	r.FinishBy.Wall = endTime
	return r, nil
}

// StateDB returns the live state database, reflecting all execution performed
// so far.
func (e *Execution) StateDB() *state.StateDB {
	return e.stateDB
}

// IntermediateRoot computes the state root reflecting all execution performed
// so far, under the same EIP-158 ruleset used to finalise transactions.
func (e *Execution) IntermediateRoot() common.Hash {
	return e.stateDB.IntermediateRoot(e.isEIP158)
}

// Signer returns the [types.Signer] used to derive transaction senders.
func (e *Execution) Signer() types.Signer {
	return e.signer
}

// BaseFee returns the base fee the block executes with; see [NewExecution].
func (e *Execution) BaseFee() *uint256.Int {
	return e.baseFee
}

// BlockContext returns the [vm.BlockContext] in which the block's
// transactions execute.
func (e *Execution) BlockContext() vm.BlockContext {
	return core.NewEVMBlockContext(e.header, e.chainCtx, &e.header.Coinbase)
}

func (e *Executor) afterExecution(b *blocks.Block, r *ExecutionResults) error {
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
	e.sendPostExecutionEvents(b.EthBlock(), r) // (3)
	return nil
}

// NullReceiptStore is a no-op [ReceiptStore] for use when receipt broadcasting
// is not needed (e.g. state tracing).
type NullReceiptStore struct{}

var _ ReceiptStore = (*NullReceiptStore)(nil)

// Load always returns the zero value and false.
func (*NullReceiptStore) Load(common.Hash) (eventual.Value[*Receipt], bool) {
	return eventual.Value[*Receipt]{}, false
}
