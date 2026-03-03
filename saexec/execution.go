// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"go.uber.org/zap"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/saedb"
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

func (e *Executor) processQueue() {
	defer close(e.done)

	for {
		select {
		case <-e.quit:
			return

		case block := <-e.queue:
			logger := e.log.With(
				zap.Uint64("block_height", block.Height()),
				zap.Uint64("block_time", block.BuildTime()),
				zap.Stringer("block_hash", block.Hash()),
				zap.Int("tx_count", len(block.Transactions())),
			)

			if err := e.execute(block, logger); err != nil {
				logger.Fatal(
					"Block execution failed; see emergency playbook",
					zap.Error(err),
					zap.String("playbook", "https://github.com/ava-labs/strevm/issues/28"),
				)
				return
			}
		}
	}
}

func (e *Executor) execute(b *blocks.Block, logger logging.Logger) error {
	logger.Debug("Executing block")

	// Since `b` hasn't been executed, it definitely hasn't been settled, so we
	// are guaranteed to have a non-nil parent available.
	parent := b.ParentBlock()
	// If the VM were to encounter an error after enqueuing the block, we would
	// receive the same block twice for execution should consensus retry
	// acceptance.
	if last := e.lastExecuted.Load().Hash(); last != parent.Hash() {
		return fmt.Errorf("executing block built on parent %#x when last executed %#x", parent.Hash(), last)
	}

	stateDB, err := state.New(parent.PostExecutionStateRoot(), e.stateCache, e.snaps)
	if err != nil {
		return fmt.Errorf("state.New(%#x, ...): %v", parent.PostExecutionStateRoot(), err)
	}

	gasClock := parent.ExecutedByGasTime().Clone()
	gasClock.BeforeBlock(e.hooks, b.Header())
	perTxClock := gasClock.Time.Clone()

	rules := e.chainConfig.Rules(b.Number(), true /*isMerge*/, b.BuildTime())
	if err := e.hooks.BeforeExecutingBlock(rules, stateDB, b.EthBlock()); err != nil {
		return fmt.Errorf("before-block hook: %v", err)
	}

	baseFee := gasClock.BaseFee()
	b.CheckBaseFeeBound(baseFee)
	header := types.CopyHeader(b.Header())
	header.BaseFee = baseFee.ToBig()

	gasPool := core.GasPool(math.MaxUint64) // required by geth but irrelevant so max it out
	var blockGasConsumed gas.Gas

	signer := e.SignerForBlock(b.EthBlock())
	receipts := make(types.Receipts, len(b.Transactions()))
	for ti, tx := range b.Transactions() {
		stateDB.SetTxContext(tx.Hash(), ti)
		b.CheckSenderBalanceBound(stateDB, signer, tx)

		logger = logger.With(
			zap.Int("tx_index", ti),
			zap.Stringer("tx_hash", tx.Hash()),
		)

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
			logger.Fatal(
				"Transaction execution errored (not reverted); see emergency playbook",
				zap.String("playbook", "https://github.com/ava-labs/strevm/issues/28"),
				zap.Error(err),
			)
			return err
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

		// Even though we populated the value ourselves and `ok == true` is
		// guaranteed when using the [Executor] via the public API, it's clearer
		// to check than to require the reader to reason about dropping the
		// flag.
		if ch, ok := e.receipts.Load(tx.Hash()); ok {
			ch <- &Receipt{receipt, signer, tx}
		}
		receipts[ti] = receipt
	}

	numTxs := len(b.Transactions())
	for i, o := range e.hooks.EndOfBlockOps(b.EthBlock()) {
		b.CheckOpBurnerBalanceBounds(stateDB, numTxs+i, o)
		blockGasConsumed += o.Gas
		perTxClock.Tick(o.Gas)
		b.SetInterimExecutionTime(perTxClock)

		if err := o.ApplyTo(stateDB); err != nil {
			logger.Fatal(
				"Extra block operation errored; see emergency playbook",
				zap.Int("op_index", i),
				zap.Stringer("op_id", o.ID),
				zap.String("playbook", "https://github.com/ava-labs/strevm/issues/28"),
				zap.Error(err),
			)
			return err
		}
	}

	e.hooks.AfterExecutingBlock(stateDB, b.EthBlock(), receipts)
	endTime := time.Now()
	if err := gasClock.AfterBlock(blockGasConsumed, e.hooks, b.Header()); err != nil {
		return fmt.Errorf("after-block gas time update: %w", err)
	}

	logger.Debug(
		"Block execution complete",
		zap.Uint64("gas_consumed", uint64(blockGasConsumed)),
		zap.Time("gas_time", gasClock.AsTime()),
		zap.Time("wall_time", endTime),
	)

	e.chainContext.recent.Put(b.NumberU64(), b.Header())

	root, err := stateDB.Commit(b.NumberU64(), true)
	if err != nil {
		return fmt.Errorf("%T.Commit() at end of block %d: %w", stateDB, b.NumberU64(), err)
	}
	if num := b.NumberU64(); saedb.ShouldCommitTrieDB(num) {
		tdb := e.stateCache.TrieDB()
		if err := tdb.Commit(root, false /* log */); err != nil {
			return fmt.Errorf("%T.Commit(%#x) at end of block %d: %v", tdb, root, num, err)
		}
	}
	// The strict ordering of the next 3 calls guarantees invariants that MUST
	// NOT be broken:
	//
	// 1. [blocks.Block.MarkExecuted] guarantees disk then in-memory changes.
	// 2. Internal indicator of last executed MUST follow in-memory change.
	// 3. External indicator of last executed MUST follow internal indicator.
	if err := b.MarkExecuted(e.db, e.xdb, gasClock.Clone(), endTime, header.BaseFee, receipts, root, &e.lastExecuted /* (2) */); err != nil {
		return err
	}
	e.sendPostExecutionEvents(b.EthBlock(), receipts) // (3)
	return nil
}
