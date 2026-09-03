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
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/prefetch"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

var (
	errExecutorClosed                = errors.New("saexec.Executor closed")
	errTransactionCountOutOfRange    = errors.New("transaction count out of range")
	errPartialEndOfBlockExecution    = errors.New("end-of-block operations require all transactions to have been executed")
	errCanonicalWithoutEndOfBlockOps = errors.New("canonical execution requires end-of-block operations")
	errNilReceiptStore               = errors.New("receipt store is nil")
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

const (
	// triePrefetcherNamespace names the prefetcher's metrics. libevm records
	// them as `trie/prefetch/sae/*` on its global registry.
	//
	// TODO(JonathanOppenheimer): We need to register this namespace within
	// SAE.
	triePrefetcherNamespace = "sae"
	// triePrefetcherParallelism limits the prefetcher to 16 goroutines.
	triePrefetcherParallelism = 16
)

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

	// The prefetcher loads trie nodes during execution which removes the loads
	// at Commit time. On historical mainnet C-Chain it cut mean block insertion
	// from 99.76ms to 44.84ms:
	// https://github.com/ava-labs/avalanchego/issues/5665#issuecomment-5372800462
	//
	// TODO(JonathanOppenheimer): measure this again after SAE is live!
	stateDB.StartPrefetcher(triePrefetcherNamespace, prefetch.WithConcurrentWorkers(triePrefetcherParallelism))
	defer stateDB.StopPrefetcher()

	result, err := Execute(
		b,
		stateDB,
		e.hooks,
		e.chainConfig,
		e.chainContext,
		log,
		asCanonical(),
		WithReceiptStore(e.receipts),
	)
	if err != nil {
		return err
	}
	return e.afterExecution(b, stateDB, result)
}

type (
	// ReceiptStore receives per-transaction receipts during block execution.
	ReceiptStore interface {
		Load(common.Hash) (eventual.Value[*Receipt], bool)
	}

	executionConfig struct {
		maxNumTxs         uint
		skipEndOfBlockOps bool
		canonical         bool
		receiptStore      ReceiptStore
	}

	// ExecutionResults holds the outputs of [Execute].
	ExecutionResults struct {
		BaseFee     *uint256.Int
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

// An Option configures [Execute].
type Option = options.Option[executionConfig]

// WithMaxNumTxs limits execution to maxNumTxs transactions from the start of
// the block. A value of 0 executes no transactions.
func WithMaxNumTxs(maxNumTxs uint) Option {
	return options.Func[executionConfig](func(c *executionConfig) {
		c.maxNumTxs = maxNumTxs
	})
}

// SkipEndOfBlockOps prevents execution of the block's end-of-block operations
// and finish-executing-block hook. [ExecutionResults.FinishBy] is not populated
// because execution does not complete the block.
func SkipEndOfBlockOps() Option {
	return options.Func[executionConfig](func(c *executionConfig) {
		c.skipEndOfBlockOps = true
	})
}

// asCanonical marks execution as canonical. It is unexported because
// canonical execution mutates the block's shared progress and is exclusive to
// Executor.
func asCanonical() Option {
	return options.Func[executionConfig](func(c *executionConfig) {
		c.canonical = true
	})
}

// WithReceiptStore configures where Execute publishes transaction receipts.
func WithReceiptStore(receiptStore ReceiptStore) Option {
	return options.Func[executionConfig](func(c *executionConfig) {
		c.receiptStore = receiptStore
	})
}

func (c *executionConfig) verify(numTxs uint) error {
	if c.maxNumTxs > numTxs {
		return fmt.Errorf("%w: %d not in [0, %d]", errTransactionCountOutOfRange, c.maxNumTxs, numTxs)
	}
	if !c.skipEndOfBlockOps && c.maxNumTxs != numTxs {
		return fmt.Errorf("%w: executing %d of %d transactions", errPartialEndOfBlockExecution, c.maxNumTxs, numTxs)
	}
	if c.canonical && c.skipEndOfBlockOps {
		return errCanonicalWithoutEndOfBlockOps
	}
	if c.receiptStore == nil {
		return errNilReceiptStore
	}
	return nil
}

// stateBeforeTransactions applies the EIP-4788 beacon root and finalizes the
// state before calling the start-executing-block hook, mirroring
// [core.StateProcessor.Process].
func stateBeforeTransactions(hooks hook.Points, rules params.Rules, stateDB *state.StateDB, parent *types.Header, b *types.Block) error {
	core.SetBeaconBlockRoot(stateDB, b.Header())

	// SetBeaconBlockRoot only finalizes when it applies the root. Finalize
	// unconditionally before exposing the state to the hook. This mirrors the
	// finalization performed by [core.ApplyTransaction].
	stateDB.Finalise(rules.IsEIP158)

	if err := hooks.StartExecutingBlock(rules, stateDB, parent, b); err != nil {
		return fmt.Errorf("start-executing-block hook: %v", err)
	}
	return nil
}

// Execute applies b's deterministic state changes to stateDB. By default, it
// executes every transaction and all end-of-block operations. Options can stop
// execution after a transaction prefix for intra-block inspection.
//
// The gas clock and base fee come from the parent's post-execution clock,
// except pre-SAE blocks, which use their own header's fee.
//
// Execute only runs the deterministic hooks, so it is also safe to use for
// historical execution. Canonical-only side effects belong in
// [hook.Points.AfterExecutingBlock], which only the [Executor] calls.
//
// Execute does not call [blocks.Block.MarkExecuted]. Only canonical execution
// records block progress. Receipts are always returned in [ExecutionResults]
// but are only published to a [ReceiptStore] when configured with
// [WithReceiptStore].
func Execute(
	b *blocks.Block,
	stateDB *state.StateDB,
	hooks hook.Points,
	chainConfig *params.ChainConfig,
	chainCtx core.ChainContext,
	log logging.Logger,
	opts ...Option,
) (*ExecutionResults, error) {
	txs := b.Transactions()
	config := options.ApplyTo(&executionConfig{
		maxNumTxs:    uint(len(txs)),
		receiptStore: &NullReceiptStore{},
	}, opts...)
	if err := config.verify(uint(len(txs))); err != nil {
		return nil, err
	}

	log.Trace("Executing block")

	parent := b.ParentBlock()
	header := b.Header()

	gasClock := parent.ExecutedByGasTime()
	gasClock.BeforeBlock(hooks.BlockTime(header))
	perTxClock := gasClock.Time.Clone()

	rules := chainConfig.Rules(header.Number, true /*isMerge*/, header.Time)
	if err := stateBeforeTransactions(hooks, rules, stateDB, parent.Header(), b.EthBlock()); err != nil {
		return nil, err
	}

	baseFee := gasClock.BaseFee()
	if hook.Synchronous(hooks, header) {
		baseFee = b.WorstCaseBaseFee()
	} else {
		b.CheckBaseFeeBound(baseFee)
	}
	header.BaseFee = baseFee.ToBig()

	signer := b.Signer(chainConfig)
	gasPool := core.GasPool(math.MaxUint64) // required by geth but irrelevant so max it out

	txs = txs[:config.maxNumTxs]
	res := &ExecutionResults{
		BaseFee:  baseFee,
		Signer:   signer,
		BlockCtx: core.NewEVMBlockContext(header, chainCtx, &header.Coinbase),
		Receipts: make(types.Receipts, len(txs)),
	}

	for ti, tx := range txs {
		stateDB.SetTxContext(tx.Hash(), ti)
		b.CheckSenderBalanceBound(stateDB, signer, tx)

		// Executes the transaction and calls [state.StateDB.Finalise].
		receipt, err := core.ApplyTransaction(
			chainConfig,
			chainCtx,
			&header.Coinbase,
			&gasPool,
			stateDB,
			header,
			tx,
			(*uint64)(&res.GasConsumed),
			vm.Config{},
		)
		if err != nil {
			return nil, fmt.Errorf("%w: transaction execution errored (not reverted) [%d](%#x): %v", errFatal, ti, tx.Hash(), err)
		}

		perTxClock.Tick(gas.Gas(receipt.GasUsed))
		// Interim execution time reports live canonical progress. Historical
		// execution can run only part of the same in-memory block and overwrite
		// that progress with an earlier time. This violates monotonicity and can
		// change the settlement decision made by LastToSettleAt.
		if config.canonical {
			b.SwapInterimExecutionTime(perTxClock)
			// TODO(arr4n) investigate calling the same method on pending blocks in
			// the queue. It's only worth it if [blocks.LastToSettleAt] regularly
			// returns false, meaning that execution is blocking consensus.
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

		if r, ok := config.receiptStore.Load(tx.Hash()); ok {
			r.Put(&Receipt{receipt, signer, tx})
		}
		res.Receipts[ti] = receipt
	}

	if config.skipEndOfBlockOps {
		// TODO(JonathanOppenheimer): skipping the FinishExecutingBlock hook
		// leaks goroutines from an in-flight parallel.Processor, which requires
		// a FinishBlock call.
		return res, nil
	}

	numTxs := len(b.Transactions())
	ops, err := hooks.EndOfBlockOps(b.EthBlock())
	if err != nil {
		return nil, fmt.Errorf("%w: %T.EndOfBlockOps(%#x): %v", errFatal, hooks, b.Hash(), err)
	}
	for i, o := range ops {
		b.CheckOpBurnerBalanceBounds(stateDB, numTxs+i, o)
		res.GasConsumed += o.Gas
		perTxClock.Tick(o.Gas)
		if config.canonical {
			b.SwapInterimExecutionTime(perTxClock)
		}

		if err := o.ApplyTo(stateDB); err != nil {
			return nil, fmt.Errorf("%w: applying end-of-block operation [%d](%v): %v", errFatal, i, o.ID, err)
		}
	}

	if err := hooks.FinishExecutingBlock(stateDB, b.EthBlock(), res.Receipts); err != nil {
		return nil, fmt.Errorf("finish-executing-block hook: %v", err)
	}

	endTime := time.Now()
	target, gasCfg := hooks.GasConfigAfter(b.Header())
	if err := gasClock.AfterBlock(res.GasConsumed, target, gasCfg); err != nil {
		return nil, fmt.Errorf("after-block gas time update: %w", err)
	}

	log.Trace(
		"Block execution complete",
		zap.Uint64("gas_consumed", uint64(res.GasConsumed)),
		zap.Time("gas_time", gasClock.AsTime()),
		zap.Time("wall_time", endTime),
	)

	res.FinishBy.Gas = gasClock
	res.FinishBy.Wall = endTime
	return res, nil
}

func (e *Executor) afterExecution(b *blocks.Block, stateDB *state.StateDB, r *ExecutionResults) error {
	if err := e.hooks.AfterExecutingBlock(b.EthBlock(), r.Receipts); err != nil {
		return fmt.Errorf("after-executing-block hook: %v", err)
	}

	e.chainContext.recent.Put(b.NumberU64(), b.Header())

	root, err := stateDB.Commit(b.NumberU64(), true, e.Tracker.StateDBCommitOptions()...)
	if err != nil {
		return fmt.Errorf("%T.Commit() at end of block %d: %w", stateDB, b.NumberU64(), err)
	}
	if err := e.Tracker.MaybeCommit(b.SettledStateRoot(), root, b.NumberU64()); err != nil {
		return err
	}

	// Responsibility for untracking lies with the VM once it deems this block's
	// post-execution state to no longer be consensus-critical.
	e.Tracker.Track(root)

	// The commit above may have flattened a snapshot diff layer to disk
	// ([saedb.SnapshotCapLayers]); keep the new disk root's trie referenced
	// for the snapshot generator.
	e.Tracker.PinSnapshotDiskRoot()

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

// NullReceiptStore discards transaction receipts.
type NullReceiptStore struct{}

var _ ReceiptStore = (*NullReceiptStore)(nil)

// Load always returns the zero value and false.
func (*NullReceiptStore) Load(common.Hash) (eventual.Value[*Receipt], bool) {
	return eventual.Value[*Receipt]{}, false
}
