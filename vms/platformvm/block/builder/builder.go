// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"

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
	mempool.Mempool

	// StartBlockTimer starts to issue block creation requests to advance the
	// chain timestamp.
	StartBlockTimer()

	// ResetBlockTimer forces the block timer to recalculate when it should
	// advance the chain timestamp.
	ResetBlockTimer()

	// ShutdownBlockTimer stops block creation requests to advance the chain
	// timestamp.
	//
	// Invariant: Assumes the context lock is held when calling.
	ShutdownBlockTimer()

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
	mempool.Mempool

	txExecutorBackend *txexecutor.Backend
	blkManager        blockexecutor.Manager

	// resetTimer is used to signal that the block builder timer should update
	// when it will trigger building of a block.
	resetTimer chan struct{}
	closed     chan struct{}
	closeOnce  sync.Once
}

func New(
	mempool mempool.Mempool,
	txExecutorBackend *txexecutor.Backend,
	blkManager blockexecutor.Manager,
) Builder {
	return &builder{
		Mempool:           mempool,
		txExecutorBackend: txExecutorBackend,
		blkManager:        blkManager,
		resetTimer:        make(chan struct{}, 1),
		closed:            make(chan struct{}),
	}
}

func (b *builder) StartBlockTimer() {
	go func() {
		timer := time.NewTimer(0)
		defer timer.Stop()

		for {
			// Invariant: The [timer] is not stopped.
			select {
			case <-timer.C:
			case <-b.resetTimer:
				if !timer.Stop() {
					<-timer.C
				}
			case <-b.closed:
				return
			}

			// Note: Because the context lock is not held here, it is possible
			// that [ShutdownBlockTimer] is called concurrently with this
			// execution.
			for {
				duration, err := b.durationToSleep()
				if err != nil {
					b.txExecutorBackend.Ctx.Log.Error("block builder encountered a fatal error",
						zap.Error(err),
					)
					return
				}

				if duration > 0 {
					timer.Reset(duration)
					break
				}

				// Block needs to be issued to advance time.
				b.Mempool.RequestBuildBlock(true /*=emptyBlockPermitted*/)

				// Invariant: ResetBlockTimer is guaranteed to be called after
				// [durationToSleep] returns a value <= 0. This is because we
				// are guaranteed to attempt to build block. After building a
				// valid block, the chain will have its preference updated which
				// may change the duration to sleep and trigger a timer reset.
				select {
				case <-b.resetTimer:
				case <-b.closed:
					return
				}
			}
		}
	}()
}

func (b *builder) durationToSleep() (time.Duration, error) {
	// Grabbing the lock here enforces that this function is not called mid-way
	// through modifying of the state.
	b.txExecutorBackend.Ctx.Lock.Lock()
	defer b.txExecutorBackend.Ctx.Lock.Unlock()

	// If [ShutdownBlockTimer] was called, we want to exit the block timer
	// goroutine. We check this with the context lock held because
	// [ShutdownBlockTimer] is expected to only be called with the context lock
	// held.
	select {
	case <-b.closed:
		return 0, nil
	default:
	}

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

func (b *builder) ResetBlockTimer() {
	// Ensure that the timer will be reset at least once.
	select {
	case b.resetTimer <- struct{}{}:
	default:
	}
}

func (b *builder) ShutdownBlockTimer() {
	b.closeOnce.Do(func() {
		close(b.closed)
	})
}

// BuildBlock builds a block to be added to consensus.
// This method removes the transactions from the returned
// blocks from the mempool.
func (b *builder) BuildBlock(context.Context) (snowman.Block, error) {
	// If there are still transactions in the mempool, then we need to
	// re-trigger block building.
	defer b.Mempool.RequestBuildBlock(false /*=emptyBlockPermitted*/)

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
		b,
		preferredID,
		nextHeight,
		timestamp,
		timeWasCapped,
		preferredState,
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

	if !b.txExecutorBackend.Config.UpgradeConfig.IsEtnaActivated(timestamp) {
		return packDurangoBlockTxs(
			preferredID,
			preferredState,
			b.Mempool,
			b.txExecutorBackend,
			b.blkManager,
			timestamp,
			math.MaxInt,
		)
	}
	return packEtnaBlockTxs(
		preferredID,
		preferredState,
		b.Mempool,
		b.txExecutorBackend,
		b.blkManager,
		timestamp,
		math.MaxUint64,
	)
}

// [timestamp] is min(max(now, parent timestamp), next staker change time)
func buildBlock(
	builder *builder,
	parentID ids.ID,
	height uint64,
	timestamp time.Time,
	forceAdvanceTime bool,
	parentState state.Chain,
) (block.Block, error) {
	var (
		blockTxs []*txs.Tx
		err      error
	)
	if builder.txExecutorBackend.Config.UpgradeConfig.IsEtnaActivated(timestamp) {
		blockTxs, err = packEtnaBlockTxs(
			parentID,
			parentState,
			builder.Mempool,
			builder.txExecutorBackend,
			builder.blkManager,
			timestamp,
			0, // minCapacity is 0 as we want to honor the capacity in state.
		)
	} else {
		blockTxs, err = packDurangoBlockTxs(
			parentID,
			parentState,
			builder.Mempool,
			builder.txExecutorBackend,
			builder.blkManager,
			timestamp,
			targetBlockSize,
		)
	}
	if err != nil {
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
	parentID ids.ID,
	parentState state.Chain,
	mempool mempool.Mempool,
	backend *txexecutor.Backend,
	manager blockexecutor.Manager,
	timestamp time.Time,
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
			parentID,
			stateDiff,
			mempool,
			backend,
			manager,
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
	parentID ids.ID,
	parentState state.Chain,
	mempool mempool.Mempool,
	backend *txexecutor.Backend,
	manager blockexecutor.Manager,
	timestamp time.Time,
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
	for {
		tx, exists := mempool.Peek()
		if !exists {
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
			break
		}

		shouldAdd, err := executeTx(
			parentID,
			stateDiff,
			mempool,
			backend,
			manager,
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
	parentID ids.ID,
	stateDiff state.Diff,
	mempool mempool.Mempool,
	backend *txexecutor.Backend,
	manager blockexecutor.Manager,
	inputs *set.Set[ids.ID],
	feeCalculator fee.Calculator,
	tx *txs.Tx,
) (bool, error) {
	mempool.Remove(tx)

	// Invariant: [tx] has already been syntactically verified.

	txDiff, err := state.NewDiffOn(stateDiff)
	if err != nil {
		return false, err
	}

	executor := &txexecutor.StandardTxExecutor{
		Backend:       backend,
		State:         txDiff,
		FeeCalculator: feeCalculator,
		Tx:            tx,
	}

	err = tx.Unsigned.Visit(executor)
	if err != nil {
		txID := tx.ID()
		mempool.MarkDropped(txID, err)
		return false, nil
	}

	if inputs.Overlaps(executor.Inputs) {
		txID := tx.ID()
		mempool.MarkDropped(txID, blockexecutor.ErrConflictingBlockTxs)
		return false, nil
	}
	err = manager.VerifyUniqueInputs(parentID, executor.Inputs)
	if err != nil {
		txID := tx.ID()
		mempool.MarkDropped(txID, err)
		return false, nil
	}
	inputs.Union(executor.Inputs)

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
