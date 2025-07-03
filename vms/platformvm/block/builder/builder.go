// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

const (
	// targetBlockSize is maximum number of transaction bytes to place into a
	// StandardBlock
	targetBlockSize = 128 * units.KiB

	// maxTimeToSleep is the maximum time to sleep between checking if a block
	// should be produced.
	maxTimeToSleep = time.Hour
)

var (
	_ Builder = (*builder)(nil)

	ErrEndOfTime                 = errors.New("program time is suspiciously far in the future")
	ErrNoPendingBlocks           = errors.New("no pending blocks")
	errMissingPreferredState     = errors.New("missing preferred block state")
	errCalculatingNextStakerTime = errors.New("failed calculating next staker time")
)

type Builder interface {
	smblock.BuildBlockWithContextChainVM
	mempool.Mempool[*txs.Tx]

	// BuildBlock can be called to attempt to create a new block
	BuildBlock(context.Context) (snowman.Block, error)

	// PackAllBlockTxs returns an array of all txs that could be packed into a
	// valid block of infinite size. The returned txs are all verified against
	// the preferred state.
	//
	// Note: This function does not call the consensus engine.
	PackAllBlockTxs() ([]*txs.Tx, error)
}

// builder implements a simple builder to convert txs into valid blocks
type builder struct {
	mempool.Mempool[*txs.Tx]

	txExecutorBackend *txexecutor.Backend
	blkManager        blockexecutor.Manager
}

func New(
	mempool mempool.Mempool[*txs.Tx],
	txExecutorBackend *txexecutor.Backend,
	blkManager blockexecutor.Manager,
) Builder {
	return &builder{
		Mempool:           mempool,
		txExecutorBackend: txExecutorBackend,
		blkManager:        blkManager,
	}
}

func (b *builder) WaitForEvent(ctx context.Context) (common.Message, error) {
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		duration, err := b.durationToSleep()
		if err != nil {
			b.txExecutorBackend.Ctx.Log.Error("block builder failed to calculate next staker change time",
				zap.Error(err),
			)
			return 0, err
		}
		if duration <= 0 {
			b.txExecutorBackend.Ctx.Log.Debug("Skipping block build wait, next staker change is ready")
			// The next staker change is ready to be performed.
			return common.PendingTxs, nil
		}

		b.txExecutorBackend.Ctx.Log.Debug("Will wait until a transaction comes", zap.Duration("maxWait", duration))

		// Wait for a transaction in the mempool until there is a next staker
		// change ready to be performed.
		newCtx, cancel := context.WithTimeout(ctx, duration)
		msg, err := b.Mempool.WaitForEvent(newCtx)
		cancel()

		switch {
		case err == nil:
			b.txExecutorBackend.Ctx.Log.Debug("New transaction received", zap.Stringer("msg", msg))
			return msg, nil
		case errors.Is(err, context.DeadlineExceeded):
			continue // Recheck the staker change time before returning
		default:
			// Error could have been due to the parent context being cancelled
			// or another unexpected error.
			return 0, err
		}
	}
}

func (b *builder) durationToSleep() (time.Duration, error) {
	// Grabbing the lock here enforces that this function is not called mid-way
	// through modifying of the state.
	b.txExecutorBackend.Ctx.Lock.Lock()
	defer b.txExecutorBackend.Ctx.Lock.Unlock()

	preferredID := b.blkManager.Preferred()
	preferredState, ok := b.blkManager.GetState(preferredID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", errMissingPreferredState, preferredID)
	}

	now := b.txExecutorBackend.Clk.Time()
	maxTimeToAwake := now.Add(maxTimeToSleep)
	nextStakerChangeTime, err := state.GetNextStakerChangeTime(
		b.txExecutorBackend.Config.ValidatorFeeConfig,
		preferredState,
		maxTimeToAwake,
	)
	if err != nil {
		return 0, fmt.Errorf("%w of %s: %w", errCalculatingNextStakerTime, preferredID, err)
	}

	return nextStakerChangeTime.Sub(now), nil
}

func (b *builder) BuildBlock(ctx context.Context) (snowman.Block, error) {
	return b.BuildBlockWithContext(
		ctx,
		&smblock.Context{
			PChainHeight: 0,
		},
	)
}

func (b *builder) BuildBlockWithContext(
	ctx context.Context,
	blockContext *smblock.Context,
) (snowman.Block, error) {
	b.txExecutorBackend.Ctx.Log.Debug("starting to attempt to build a block")

	// Get the block to build on top of and retrieve the new block's context.
	preferredID := b.blkManager.Preferred()
	preferred, err := b.blkManager.GetBlock(preferredID)
	if err != nil {
		return nil, err
	}
	nextHeight := preferred.Height() + 1
	preferredState, ok := b.blkManager.GetState(preferredID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", state.ErrMissingParentState, preferredID)
	}

	timestamp, timeWasCapped, err := state.NextBlockTime(
		b.txExecutorBackend.Config.ValidatorFeeConfig,
		preferredState,
		b.txExecutorBackend.Clk,
	)
	if err != nil {
		return nil, fmt.Errorf("could not calculate next staker change time: %w", err)
	}

	statelessBlk, err := buildBlock(
		ctx,
		b,
		preferredID,
		nextHeight,
		timestamp,
		timeWasCapped,
		preferredState,
		blockContext.PChainHeight,
	)
	if err != nil {
		return nil, err
	}

	return b.blkManager.NewBlock(statelessBlk), nil
}

func (b *builder) PackAllBlockTxs() ([]*txs.Tx, error) {
	preferredID := b.blkManager.Preferred()
	preferredState, ok := b.blkManager.GetState(preferredID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errMissingPreferredState, preferredID)
	}

	timestamp, _, err := state.NextBlockTime(
		b.txExecutorBackend.Config.ValidatorFeeConfig,
		preferredState,
		b.txExecutorBackend.Clk,
	)
	if err != nil {
		return nil, fmt.Errorf("could not calculate next staker change time: %w", err)
	}

	recommendedPChainHeight, err := b.txExecutorBackend.Ctx.ValidatorState.GetMinimumHeight(context.TODO())
	if err != nil {
		return nil, err
	}

	if !b.txExecutorBackend.Config.UpgradeConfig.IsEtnaActivated(timestamp) {
		return packDurangoBlockTxs(
			context.TODO(),
			preferredID,
			preferredState,
			b.Mempool,
			b.txExecutorBackend,
			b.blkManager,
			timestamp,
			recommendedPChainHeight,
			math.MaxInt,
		)
	}
	return packEtnaBlockTxs(
		context.TODO(),
		preferredID,
		preferredState,
		b.Mempool,
		b.txExecutorBackend,
		b.blkManager,
		timestamp,
		recommendedPChainHeight,
		math.MaxUint64,
	)
}

// [timestamp] is min(max(now, parent timestamp), next staker change time)
func buildBlock(
	ctx context.Context,
	builder *builder,
	parentID ids.ID,
	height uint64,
	timestamp time.Time,
	forceAdvanceTime bool,
	parentState state.Chain,
	pChainHeight uint64,
) (block.Block, error) {
	var (
		blockTxs []*txs.Tx
		err      error
	)
	if builder.txExecutorBackend.Config.UpgradeConfig.IsEtnaActivated(timestamp) {
		blockTxs, err = packEtnaBlockTxs(
			ctx,
			parentID,
			parentState,
			builder.Mempool,
			builder.txExecutorBackend,
			builder.blkManager,
			timestamp,
			pChainHeight,
			0, // minCapacity is 0 as we want to honor the capacity in state.
		)
	} else {
		blockTxs, err = packDurangoBlockTxs(
			ctx,
			parentID,
			parentState,
			builder.Mempool,
			builder.txExecutorBackend,
			builder.blkManager,
			timestamp,
			pChainHeight,
			targetBlockSize,
		)
	}
	if err != nil {
		builder.txExecutorBackend.Ctx.Log.Warn("failed to pack block transactions",
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to pack block txs: %w", err)
	}

	// Try rewarding stakers whose staking period ends at the new chain time.
	// This is done first to prioritize advancing the timestamp as quickly as
	// possible.
	stakerTxID, shouldReward, err := getNextStakerToReward(timestamp, parentState)
	if err != nil {
		return nil, fmt.Errorf("could not find next staker to reward: %w", err)
	}
	if shouldReward {
		rewardValidatorTx, err := NewRewardValidatorTx(builder.txExecutorBackend.Ctx, stakerTxID)
		if err != nil {
			return nil, fmt.Errorf("could not build tx to reward staker: %w", err)
		}

		return block.NewBanffProposalBlock(
			timestamp,
			parentID,
			height,
			rewardValidatorTx,
			blockTxs,
		)
	}

	// If there is no reason to build a block, don't.
	if len(blockTxs) == 0 && !forceAdvanceTime {
		builder.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return nil, ErrNoPendingBlocks
	}

	// Issue a block with as many transactions as possible.
	return block.NewBanffStandardBlock(
		timestamp,
		parentID,
		height,
		blockTxs,
	)
}

func packDurangoBlockTxs(
	ctx context.Context,
	parentID ids.ID,
	parentState state.Chain,
	mempool mempool.Mempool[*txs.Tx],
	backend *txexecutor.Backend,
	manager blockexecutor.Manager,
	timestamp time.Time,
	pChainHeight uint64,
	remainingSize int,
) ([]*txs.Tx, error) {
	stateDiff, err := state.NewDiffOn(parentState)
	if err != nil {
		return nil, err
	}

	if _, err := txexecutor.AdvanceTimeTo(backend, stateDiff, timestamp); err != nil {
		return nil, err
	}

	var (
		blockTxs      []*txs.Tx
		inputs        set.Set[ids.ID]
		feeCalculator = state.PickFeeCalculator(backend.Config, stateDiff)
	)
	for {
		tx, exists := mempool.Peek()
		if !exists {
			break
		}
		txSize := len(tx.Bytes())
		if txSize > remainingSize {
			break
		}

		shouldAdd, err := executeTx(
			ctx,
			parentID,
			stateDiff,
			mempool,
			backend,
			manager,
			pChainHeight,
			&inputs,
			feeCalculator,
			tx,
		)
		if err != nil {
			return nil, err
		}
		if !shouldAdd {
			continue
		}

		remainingSize -= txSize
		blockTxs = append(blockTxs, tx)
	}

	return blockTxs, nil
}

func packEtnaBlockTxs(
	ctx context.Context,
	parentID ids.ID,
	parentState state.Chain,
	mempool mempool.Mempool[*txs.Tx],
	backend *txexecutor.Backend,
	manager blockexecutor.Manager,
	timestamp time.Time,
	pChainHeight uint64,
	minCapacity gas.Gas,
) ([]*txs.Tx, error) {
	stateDiff, err := state.NewDiffOn(parentState)
	if err != nil {
		return nil, err
	}

	if _, err := txexecutor.AdvanceTimeTo(backend, stateDiff, timestamp); err != nil {
		return nil, err
	}

	feeState := stateDiff.GetFeeState()
	capacity := max(feeState.Capacity, minCapacity)

	var (
		blockTxs        []*txs.Tx
		inputs          set.Set[ids.ID]
		blockComplexity gas.Dimensions
		feeCalculator   = state.PickFeeCalculator(backend.Config, stateDiff)
	)

	backend.Ctx.Log.Debug("starting to pack block txs",
		zap.Stringer("parentID", parentID),
		zap.Time("blockTimestamp", timestamp),
		zap.Uint64("capacity", uint64(capacity)),
		zap.Int("mempoolLen", mempool.Len()),
	)
	for {
		currentBlockGas, err := blockComplexity.ToGas(backend.Config.DynamicFeeConfig.Weights)
		if err != nil {
			return nil, err
		}

		tx, exists := mempool.Peek()
		if !exists {
			backend.Ctx.Log.Debug("mempool is empty",
				zap.Uint64("capacity", uint64(capacity)),
				zap.Uint64("blockGas", uint64(currentBlockGas)),
				zap.Int("blockLen", len(blockTxs)),
			)
			break
		}

		txComplexity, err := fee.TxComplexity(tx.Unsigned)
		if err != nil {
			return nil, err
		}
		newBlockComplexity, err := blockComplexity.Add(&txComplexity)
		if err != nil {
			return nil, err
		}
		newBlockGas, err := newBlockComplexity.ToGas(backend.Config.DynamicFeeConfig.Weights)
		if err != nil {
			return nil, err
		}
		if newBlockGas > capacity {
			backend.Ctx.Log.Debug("block is full",
				zap.Uint64("nextBlockGas", uint64(newBlockGas)),
				zap.Uint64("capacity", uint64(capacity)),
				zap.Uint64("blockGas", uint64(currentBlockGas)),
				zap.Int("blockLen", len(blockTxs)),
			)
			break
		}

		shouldAdd, err := executeTx(
			ctx,
			parentID,
			stateDiff,
			mempool,
			backend,
			manager,
			pChainHeight,
			&inputs,
			feeCalculator,
			tx,
		)
		if err != nil {
			return nil, err
		}
		if !shouldAdd {
			continue
		}

		blockComplexity = newBlockComplexity
		blockTxs = append(blockTxs, tx)
	}

	return blockTxs, nil
}

func executeTx(
	ctx context.Context,
	parentID ids.ID,
	stateDiff state.Diff,
	mempool mempool.Mempool[*txs.Tx],
	backend *txexecutor.Backend,
	manager blockexecutor.Manager,
	pChainHeight uint64,
	inputs *set.Set[ids.ID],
	feeCalculator fee.Calculator,
	tx *txs.Tx,
) (bool, error) {
	mempool.Remove(tx)

	// Invariant: [tx] has already been syntactically verified.

	txID := tx.ID()
	err := txexecutor.VerifyWarpMessages(
		ctx,
		backend.Ctx.NetworkID,
		backend.Ctx.ValidatorState,
		pChainHeight,
		tx.Unsigned,
	)
	if err != nil {
		backend.Ctx.Log.Debug("transaction failed warp verification",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)

		mempool.MarkDropped(txID, err)
		return false, nil
	}

	txDiff, err := state.NewDiffOn(stateDiff)
	if err != nil {
		return false, err
	}

	txInputs, _, _, err := txexecutor.StandardTx(
		backend,
		feeCalculator,
		tx,
		txDiff,
	)
	if err != nil {
		backend.Ctx.Log.Debug("transaction failed execution",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)

		mempool.MarkDropped(txID, err)
		return false, nil
	}

	if inputs.Overlaps(txInputs) {
		// This log is a warn because the mempool should not have allowed this
		// transaction to be included.
		backend.Ctx.Log.Warn("transaction conflicts with prior transaction",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)

		mempool.MarkDropped(txID, blockexecutor.ErrConflictingBlockTxs)
		return false, nil
	}
	if err := manager.VerifyUniqueInputs(parentID, txInputs); err != nil {
		backend.Ctx.Log.Debug("transaction conflicts with ancestor's import transaction",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)

		mempool.MarkDropped(txID, err)
		return false, nil
	}
	inputs.Union(txInputs)

	backend.Ctx.Log.Debug("successfully executed transaction",
		zap.Stringer("txID", txID),
		zap.Error(err),
	)
	txDiff.AddTx(tx, status.Committed)
	return true, txDiff.Apply(stateDiff)
}

// getNextStakerToReward returns the next staker txID to remove from the staking
// set with a RewardValidatorTx rather than an AdvanceTimeTx. [chainTimestamp]
// is the timestamp of the chain at the time this validator would be getting
// removed and is used to calculate [shouldReward].
// Returns:
// - [txID] of the next staker to reward
// - [shouldReward] if the txID exists and is ready to be rewarded
// - [err] if something bad happened
func getNextStakerToReward(
	chainTimestamp time.Time,
	preferredState state.Chain,
) (ids.ID, bool, error) {
	if !chainTimestamp.Before(mockable.MaxTime) {
		return ids.Empty, false, ErrEndOfTime
	}

	currentStakerIterator, err := preferredState.GetCurrentStakerIterator()
	if err != nil {
		return ids.Empty, false, err
	}
	defer currentStakerIterator.Release()

	for currentStakerIterator.Next() {
		currentStaker := currentStakerIterator.Value()
		priority := currentStaker.Priority
		// If the staker is a permissionless staker (not a permissioned subnet
		// validator), it's the next staker we will want to remove with a
		// RewardValidatorTx rather than an AdvanceTimeTx.
		if priority != txs.SubnetPermissionedValidatorCurrentPriority {
			return currentStaker.TxID, chainTimestamp.Equal(currentStaker.EndTime), nil
		}
	}
	return ids.Empty, false, nil
}

func NewRewardValidatorTx(ctx *snow.Context, txID ids.ID) (*txs.Tx, error) {
	utx := &txs.RewardValidatorTx{TxID: txID}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(ctx)
}
