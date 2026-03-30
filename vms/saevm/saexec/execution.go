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
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
)

var errExecutorClosed = errors.New("saexec.Executor closed")

// Enqueue pushes a new block to the FIFO queue. If [Executor.Close] is called
// before [blocks.Block.Executed] returns true then there is no guarantee that
// the block will be executed.
func (e *Executor) Enqueue(ctx context.Context, block *blocks.Block) error {
	e.createReceiptBuffers(block)
	select {
	case e.queue <- block:
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

		case block := <-e.queue:
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

	result, err := Execute(b, e, math.MaxInt, e.hooks, e.chainConfig, e.chainContext, e.receipts, log)
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
		BaseFee  *uint256.Int
		StateDB  *state.StateDB
		Signer   types.Signer
		BlockCtx vm.BlockContext
		Receipts types.Receipts
		FinishBy struct {
			Gas  *gastime.Time
			Wall time.Time
		}
	}
)

// Execute executes the transactions in the [blocks.Block], beginning from the
// post-execution state of the [blocks.Block.ParentBlock]. `maxNumTxs` limits
// the number of transactions to process, allowing partial execution for
// intra-block inspection.
//
// Although Execute does not call [blocks.Block.MarkExecuted] it does mutate
// consensus-critical internal values (e.g. interim execution time). A "live"
// accepted block (as against one recovered from the database) MUST NOT be
// passed directly to [Execute], only to [Executor.Enqueue].
func Execute(
	b *blocks.Block,
	sdbo saedb.StateDBOpener,
	maxNumTxs int,
	hooks hook.Points,
	config *params.ChainConfig,
	chainCtx core.ChainContext,
	receiptStore ReceiptStore,
	log logging.Logger,
) (*ExecutionResults, error) {
	log.Debug("Executing block")

	parent := b.ParentBlock()

	gasClock := parent.ExecutedByGasTime().Clone()
	gasClock.BeforeBlock(b.Header().Time, hooks.SubSecondBlockTime(b.Header()))
	perTxClock := gasClock.Time.Clone()

	stateDB, err := sdbo.StateDB(parent.PostExecutionStateRoot())
	if err != nil {
		return nil, err
	}

	rules := config.Rules(b.Number(), true /*isMerge*/, b.BuildTime())
	if err := hooks.BeforeExecutingBlock(rules, stateDB, b.EthBlock()); err != nil {
		return nil, fmt.Errorf("before-block hook: %v", err)
	}

	baseFee := gasClock.BaseFee()
	b.CheckBaseFeeBound(baseFee)
	header := types.CopyHeader(b.Header())
	header.BaseFee = baseFee.ToBig()

	signer := b.Signer(config)
	gasPool := core.GasPool(math.MaxUint64) // required by geth but irrelevant so max it out
	var blockGasConsumed gas.Gas

	txs := b.Transactions()
	txs = txs[:min(len(txs), maxNumTxs)]
	receipts := make(types.Receipts, len(txs))

	for ti, tx := range txs {
		stateDB.SetTxContext(tx.Hash(), ti)
		b.CheckSenderBalanceBound(stateDB, signer, tx)

		receipt, err := core.ApplyTransaction(
			config,
			chainCtx,
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
		b.SetInterimExecutionTime(perTxClock)
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

	numTxs := len(b.Transactions())
	ops, err := hooks.EndOfBlockOps(b.EthBlock())
	if err != nil {
		return nil, fmt.Errorf("%w: %T.EndOfBlockOps(%#x): %v", errFatal, hooks, b.Hash(), err)
	}
	for i, o := range ops {
		b.CheckOpBurnerBalanceBounds(stateDB, numTxs+i, o)
		blockGasConsumed += o.Gas
		perTxClock.Tick(o.Gas)
		b.SetInterimExecutionTime(perTxClock)

		if err := o.ApplyTo(stateDB); err != nil {
			return nil, fmt.Errorf("%w: applying end-of-block operation [%d](%v): %v", errFatal, i, o.ID, err)
		}
	}

	if err := hooks.AfterExecutingBlock(stateDB, b.EthBlock(), receipts); err != nil {
		return nil, fmt.Errorf("after-block hook: %v", err)
	}

	endTime := time.Now()
	target, gasCfg := hooks.GasConfigAfter(b.Header())
	if err := gasClock.AfterBlock(blockGasConsumed, target, gasCfg); err != nil {
		return nil, fmt.Errorf("after-block gas time update: %w", err)
	}

	log.Debug(
		"Block execution complete",
		zap.Uint64("gas_consumed", uint64(blockGasConsumed)),
		zap.Time("gas_time", gasClock.AsTime()),
		zap.Time("wall_time", endTime),
	)

	r := &ExecutionResults{
		BaseFee:  baseFee,
		StateDB:  stateDB,
		Signer:   signer,
		BlockCtx: core.NewEVMBlockContext(header, chainCtx, &header.Coinbase),
		Receipts: receipts,
	}
	r.FinishBy.Gas = gasClock
	r.FinishBy.Wall = endTime
	return r, nil
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
	e.sendPostExecutionEvents(b.EthBlock(), r.Receipts) // (3)
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
