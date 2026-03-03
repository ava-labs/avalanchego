// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"go.uber.org/zap"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/hook"
	saeparams "github.com/ava-labs/strevm/params"
	"github.com/ava-labs/strevm/saexec"
	"github.com/ava-labs/strevm/txgossip"
	"github.com/ava-labs/strevm/worstcase"
)

type blockBuilder struct {
	hooks   hook.Points
	now     func() time.Time
	log     logging.Logger
	exec    *saexec.Executor
	mempool *txgossip.Set
}

func (b *blockBuilder) new(eth *types.Block, parent, lastSettled *blocks.Block) (*blocks.Block, error) {
	return blocks.New(eth, parent, lastSettled, b.log)
}

// build a new block on top of the provided parent. The block context MAY be
// nil.
func (b *blockBuilder) build(
	ctx context.Context,
	bCtx *block.Context,
	parent *blocks.Block,
) (*blocks.Block, error) {
	return b.buildWithTxs(
		ctx,
		bCtx,
		parent,
		b.mempool.TransactionsByPriority,
		b.hooks,
	)
}

// rebuild attempts to build a block identical to the provided block. If the
// provided block contains any invalid components, those components will be set
// to their valid counterparts in the returned block. The block context MAY be
// nil.
func (b *blockBuilder) rebuild(
	ctx context.Context,
	bCtx *block.Context,
	parent *blocks.Block,
	block *blocks.Block,
) (*blocks.Block, error) {
	// Moving sender caching into [VM.ParseBlock] is not robust as there is
	// insufficient spam protection, so it must be done here.
	signer := b.exec.SignerForBlock(block.EthBlock())
	core.SenderCacher.Recover(signer, block.Transactions()) // asynchronous

	txs := make([]*txgossip.LazyTransaction, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		s, err := types.Sender(signer, tx)
		if err != nil {
			return nil, fmt.Errorf("recovering sender of tx %#x: %v", tx.Hash(), err)
		}

		feeCap, err := uint256FromBig(tx.GasFeeCap())
		if err != nil {
			return nil, fmt.Errorf("tx %#x fee cap: %v", tx.Hash(), err)
		}
		tipCap, err := uint256FromBig(tx.GasTipCap())
		if err != nil {
			return nil, fmt.Errorf("tx %#x tip cap: %v", tx.Hash(), err)
		}

		txs[i] = &txgossip.LazyTransaction{
			LazyTransaction: &txpool.LazyTransaction{
				Hash:      tx.Hash(),
				Tx:        tx,
				GasFeeCap: feeCap,
				GasTipCap: tipCap,
				Gas:       tx.Gas(),
			},
			Sender: s,
		}
	}

	return b.buildWithTxs(
		ctx,
		bCtx,
		parent,
		func(f txpool.PendingFilter) []*txgossip.LazyTransaction { return txs },
		b.hooks.BlockRebuilderFrom(block.EthBlock()),
	)
}

var (
	errBlockTimeUnderMinimum = errors.New("block time under minimum allowed time")
	errBlockTimeBeforeParent = errors.New("block time before parent time")
	errBlockTimeAfterMaximum = errors.New("block time after maximum allowed time")
	errExecutionLagging      = errors.New("execution lagging for settlement")
)

// buildWithTxs implements the block-building logic shared by [blockBuilder.Build]
// and [blockBuilder.Rebuild]. The block context MAY be nil.
func (b *blockBuilder) buildWithTxs(
	ctx context.Context,
	bCtx *block.Context,
	parent *blocks.Block,
	pendingTxs func(txpool.PendingFilter) []*txgossip.LazyTransaction,
	builder hook.BlockBuilder,
) (*blocks.Block, error) {
	hdr := builder.BuildHeader(parent.Header())
	log := b.log.With(
		zap.Uint64("parent_height", parent.Height()),
		zap.Stringer("parent_hash", parent.Hash()),
		zap.Uint64("block_time", hdr.Time),
	)
	if hdr.Root != (common.Hash{}) || hdr.GasLimit != 0 || hdr.BaseFee != nil || hdr.GasUsed != 0 {
		log.Warn("Block builder returned header with at least one reserved field set",
			zap.Stringer("root", hdr.Root),
			zap.Uint64("gas_limit", hdr.GasLimit),
			zap.Stringer("base_fee", hdr.BaseFee),
			zap.Uint64("gas_used", hdr.GasUsed),
		)
	}

	bTime := blocks.PreciseTime(b.hooks, hdr)
	pTime := blocks.PreciseTime(b.hooks, parent.Header())

	// It is allowed for [hook.Points] to further constrain the allowed block
	// times. However, every block MUST at least satisfy these basic sanity
	// checks.
	if bTime.Unix() < saeparams.TauSeconds {
		return nil, fmt.Errorf("%w: %d < %d", errBlockTimeUnderMinimum, hdr.Time, saeparams.TauSeconds)
	}
	if bTime.Compare(pTime) < 0 {
		return nil, fmt.Errorf("%w: %s < %s", errBlockTimeBeforeParent, bTime.String(), pTime.String())
	}
	maxTime := b.now().Add(maxFutureBlockDuration)
	if bTime.Compare(maxTime) > 0 {
		return nil, fmt.Errorf("%w: %s > %s", errBlockTimeAfterMaximum, bTime.String(), maxTime.String())
	}

	// Underflow of Add(-tau) is prevented by the above check.
	lastSettled, ok, err := blocks.LastToSettleAt(b.hooks, bTime.Add(-saeparams.Tau), parent)
	if err != nil {
		return nil, err
	}
	if !ok {
		log.Warn("Execution lagging when determining last block to settle")
		return nil, errExecutionLagging
	}

	log = log.With(
		zap.Uint64("last_settled_height", lastSettled.Height()),
		zap.Stringer("last_settled_hash", lastSettled.Hash()),
	)

	state, err := worstcase.NewState(b.hooks, b.exec.ChainConfig(), b.exec.StateCache(), lastSettled, b.exec.SnapshotTree())
	if err != nil {
		log.Warn("Worst-case state not able to be created",
			zap.Error(err),
		)
		return nil, err
	}

	unsettled := blocks.Range(lastSettled, parent)
	for _, block := range unsettled {
		log := log.With(
			zap.Uint64("block_height", block.Height()),
			zap.Stringer("block_hash", block.Hash()),
		)
		if err := state.StartBlock(block.Header()); err != nil {
			log.Warn("Could not start historical worst-case calculation",
				zap.Error(err),
			)
			return nil, fmt.Errorf("starting worst-case state for block %d: %v", block.Height(), err)
		}
		for i, tx := range block.Transactions() {
			if err := state.ApplyTx(tx); err != nil {
				log.Warn("Could not apply tx during historical worst-case calculation",
					zap.Int("tx_index", i),
					zap.Stringer("tx_hash", tx.Hash()),
					zap.Error(err),
				)
				return nil, fmt.Errorf("applying tx %#x in block %d to worst-case state: %v", tx.Hash(), block.Height(), err)
			}
		}
		for i, op := range b.hooks.EndOfBlockOps(block.EthBlock()) {
			if err := state.Apply(op); err != nil {
				log.Warn("Could not apply op during historical worst-case calculation",
					zap.Int("op_index", i),
					zap.Stringer("op_id", op.ID),
					zap.Error(err),
				)
				return nil, fmt.Errorf("applying op at end of block %d to worst-case state: %v", block.Height(), err)
			}
		}
		if _, err := state.FinishBlock(); err != nil {
			log.Warn("Could not finish historical worst-case calculation",
				zap.Error(err),
			)
			return nil, fmt.Errorf("finishing worst-case state for block %d: %v", block.Height(), err)
		}
	}

	hdr.Root = lastSettled.PostExecutionStateRoot()
	if err := state.StartBlock(hdr); err != nil {
		// A full queue is a normal mode of operation (backpressure working as
		// intended) so should not be a warning.
		logTo := log.Warn
		if errors.Is(err, worstcase.ErrQueueFull) {
			logTo = log.Debug
		}
		logTo("Could not start worst-case block calculation",
			zap.Error(err),
		)
		return nil, fmt.Errorf("starting worst-case state for new block: %w", err)
	}

	hdr.GasLimit = state.GasLimit()
	hdr.BaseFee = state.BaseFee().ToBig()

	var (
		candidates = pendingTxs(txpool.PendingFilter{
			BaseFee: state.BaseFee(),
		})
		included []*types.Transaction
	)
	for _, ltx := range candidates {
		// If we don't have enough gas remaining in the block for the minimum
		// gas amount, we are done including transactions.
		if remainingGas := state.GasLimit() - state.GasUsed(); remainingGas < params.TxGas {
			break
		}
		txLog := log.With(
			zap.Stringer("tx_hash", ltx.Hash),
			zap.Int("tx_index", len(included)),
			zap.Stringer("sender", ltx.Sender),
		)

		tx, ok := ltx.Resolve()
		if !ok {
			txLog.Debug("Could not resolve lazy transaction")
			continue
		}

		// The [saexec.Executor] checks the worst-case balance before tx
		// execution so we MUST record it at the equivalent point, before
		// ApplyTx().
		if err := state.ApplyTx(tx); err != nil {
			txLog.Debug("Could not apply transaction", zap.Error(err))
			continue
		}
		txLog.Trace("Including transaction")
		included = append(included, tx)
	}

	// TODO: Should the [hook.BlockBuilder] populate [types.Header.GasUsed] so
	// that [hook.Op.Gas] can be included?
	hdr.GasUsed = state.GasUsed()

	bounds, err := state.FinishBlock()
	if err != nil {
		log.Warn("Could not finish worst-case block calculation",
			zap.Error(err),
		)
		return nil, fmt.Errorf("finishing worst-case state for new block: %v", err)
	}

	var receipts types.Receipts
	settling := blocks.Range(parent.LastSettled(), lastSettled)
	for _, b := range settling {
		receipts = append(receipts, b.Receipts()...)
	}

	ethB, err := builder.BuildBlock(
		hdr,
		included,
		receipts,
	)
	if err != nil {
		return nil, err
	}

	block, err := b.new(ethB, parent, lastSettled)
	if err != nil {
		return nil, err
	}
	block.SetWorstCaseBounds(bounds)
	return block, nil
}
