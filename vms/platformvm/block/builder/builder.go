// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	txbuilder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

// targetBlockSize is maximum number of transaction bytes to place into a
// StandardBlock
const targetBlockSize = 128 * units.KiB

var (
	_ Builder = (*builder)(nil)

	ErrEndOfTime       = errors.New("program time is suspiciously far in the future")
	ErrNoPendingBlocks = errors.New("no pending blocks")
)

type Builder interface {
	mempool.Mempool

	// ResetBlockTimer schedules a timer to notify the consensus engine once a
	// block needs to be built to process a staker change.
	ResetBlockTimer()

	// BuildBlock can be called to attempt to create a new block
	BuildBlock(context.Context) (snowman.Block, error)

	// Shutdown cleanly shuts Builder down
	Shutdown()
}

// builder implements a simple builder to convert txs into valid blocks
type builder struct {
	mempool.Mempool

	txBuilder         txbuilder.Builder
	txExecutorBackend *txexecutor.Backend
	blkManager        blockexecutor.Manager

	// This timer goes off when it is time for the next staker to add/leave
	// the staking set. When it goes off, [maybeIssueEmptyBlock()] is called,
	// potentially triggering creation of a new block.
	timer                    *timer.Timer
	nextStakerChangeTimeLock sync.RWMutex
	nextStakerChangeTime     time.Time
}

func New(
	mempool mempool.Mempool,
	txBuilder txbuilder.Builder,
	txExecutorBackend *txexecutor.Backend,
	blkManager blockexecutor.Manager,
) Builder {
	builder := &builder{
		Mempool:           mempool,
		txBuilder:         txBuilder,
		txExecutorBackend: txExecutorBackend,
		blkManager:        blkManager,
	}

	builder.timer = timer.NewTimer(builder.maybeIssueEmptyBlock)

	go txExecutorBackend.Ctx.Log.RecoverAndPanic(builder.timer.Dispatch)
	return builder
}

// BuildBlock builds a block to be added to consensus.
// This method removes the transactions from the returned
// blocks from the mempool.
func (b *builder) BuildBlock(context.Context) (snowman.Block, error) {
	b.Mempool.DisableAdding()
	defer func() {
		b.Mempool.EnableAdding()
		b.Mempool.RequestBuildBlock(false /*=emptyBlockPermitted*/)
	}()

	ctx := b.txExecutorBackend.Ctx
	ctx.Log.Debug("starting to attempt to build a block")

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

	timestamp, timeWasCapped, nextStakerChangeTime, err := txexecutor.NextBlockTime(preferredState, b.txExecutorBackend.Clk)
	if err != nil {
		return nil, fmt.Errorf("could not calculate next staker change time: %w", err)
	}

	waitTime := nextStakerChangeTime.Sub(timestamp)
	ctx.Log.Debug("setting next scheduled event",
		zap.Time("nextEventTime", nextStakerChangeTime),
		zap.Duration("timeUntil", waitTime),
	)

	b.nextStakerChangeTimeLock.Lock()
	ctx.Log.Debug("updating nextStakerChangeTime in BuildBlock",
		zap.Time("old", b.nextStakerChangeTime),
		zap.Time("new", nextStakerChangeTime),
	)
	b.nextStakerChangeTime = nextStakerChangeTime
	b.nextStakerChangeTimeLock.Unlock()
	b.timer.SetTimeoutIn(waitTime)

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

func (b *builder) Shutdown() {
	// There is a potential deadlock if the timer is about to execute a timeout.
	// So, the lock must be released before stopping the timer.
	ctx := b.txExecutorBackend.Ctx
	ctx.Lock.Unlock()
	b.timer.Stop()
	ctx.Lock.Lock()
}

func (b *builder) ResetBlockTimer() {
	ctx := b.txExecutorBackend.Ctx

	if !b.txExecutorBackend.Bootstrapped.Get() {
		ctx.Log.Verbo("skipping block timer reset",
			zap.String("reason", "not bootstrapped"),
		)
		return
	}

	preferredID := b.blkManager.Preferred()
	preferredState, ok := b.blkManager.GetState(preferredID)
	if !ok {
		// The preferred block should always be a decision block
		ctx.Log.Error("couldn't get preferred block state",
			zap.Stringer("preferredID", preferredID),
			zap.Stringer("lastAcceptedID", b.blkManager.LastAccepted()),
		)
		return
	}

	nextStakerChangeTime, err := txexecutor.GetNextStakerChangeTime(preferredState)
	if err != nil {
		ctx.Log.Error("couldn't get next staker change time",
			zap.Stringer("preferredID", preferredID),
			zap.Stringer("lastAcceptedID", b.blkManager.LastAccepted()),
			zap.Error(err),
		)
		return
	}

	now := b.txExecutorBackend.Clk.Time()
	waitTime := nextStakerChangeTime.Sub(now)
	ctx.Log.Debug("setting next scheduled event",
		zap.Time("nextEventTime", nextStakerChangeTime),
		zap.Duration("timeUntil", waitTime),
	)

	// Wake up when it's time to add/remove the next validator
	b.nextStakerChangeTimeLock.Lock()
	ctx.Log.Debug("updating nextStakerChangeTime in ResetBlockTimer",
		zap.Time("old", b.nextStakerChangeTime),
		zap.Time("new", nextStakerChangeTime),
	)
	b.nextStakerChangeTime = nextStakerChangeTime
	b.nextStakerChangeTimeLock.Unlock()
	b.timer.SetTimeoutIn(waitTime)
}

func (b *builder) maybeIssueEmptyBlock() {
	ctx := b.txExecutorBackend.Ctx

	// Grabbing the lock here enforces that this function is not called mid-way
	// through modifying of the state.
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	b.nextStakerChangeTimeLock.RLock()
	defer b.nextStakerChangeTimeLock.RUnlock()

	now := b.txExecutorBackend.Clk.Time()
	if b.nextStakerChangeTime.After(now) {
		// [nextStakerChangeTime] is in the future, no need to advance time.
		waitTime := b.nextStakerChangeTime.Sub(now)
		ctx.Log.Debug("setting next scheduled event",
			zap.Time("nextEventTime", b.nextStakerChangeTime),
			zap.Duration("timeUntil", waitTime),
		)
		b.timer.SetTimeoutIn(waitTime)
		return
	}

	ctx.Log.Debug("issuing empty block to advance time",
		zap.Time("now", now),
		zap.Time("nextStakerChangeTime", b.nextStakerChangeTime),
	)

	// Block needs to be issued to advance time.
	b.Mempool.RequestBuildBlock(true /*=emptyBlockPermitted*/)
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
	// Try rewarding stakers whose staking period ends at the new chain time.
	// This is done first to prioritize advancing the timestamp as quickly as
	// possible.
	stakerTxID, shouldReward, err := getNextStakerToReward(timestamp, parentState)
	if err != nil {
		return nil, fmt.Errorf("could not find next staker to reward: %w", err)
	}
	if shouldReward {
		rewardValidatorTx, err := builder.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return nil, fmt.Errorf("could not build tx to reward staker: %w", err)
		}

		return block.NewBanffProposalBlock(
			timestamp,
			parentID,
			height,
			rewardValidatorTx,
		)
	}

	preferredID := builder.blkManager.Preferred()
	stateDiff, err := state.NewDiff(preferredID, builder.blkManager)
	if err != nil {
		return nil, err
	}

	changes, err := txexecutor.AdvanceTimeTo(builder.txExecutorBackend, stateDiff, timestamp)
	if err != nil {
		return nil, err
	}
	changes.Apply(stateDiff)
	stateDiff.SetTimestamp(timestamp)

	var (
		blockTxs      []*txs.Tx
		inputs        set.Set[ids.ID]
		remainingSize = targetBlockSize
	)

	for {
		tx := builder.Mempool.Peek(remainingSize)
		if tx == nil {
			break
		}
		builder.Mempool.Remove([]*txs.Tx{tx})

		// Invariant: [tx] has already been syntactically verified.

		txDiff, err := wrapState(stateDiff)
		if err != nil {
			return nil, err
		}

		executor := &txexecutor.StandardTxExecutor{
			Backend: builder.txExecutorBackend,
			State:   txDiff,
			Tx:      tx,
		}

		err = tx.Unsigned.Visit(executor)
		if err != nil {
			txID := tx.ID()
			builder.Mempool.MarkDropped(txID, err)
			continue
		}

		if inputs.Overlaps(executor.Inputs) {
			txID := tx.ID()
			builder.Mempool.MarkDropped(txID, blockexecutor.ErrConflictingBlockTxs)
			continue
		}
		err = builder.blkManager.VerifyUniqueInputs(preferredID, inputs)
		if err != nil {
			txID := tx.ID()
			builder.Mempool.MarkDropped(txID, err)
			continue
		}
		inputs.Union(executor.Inputs)

		txDiff.AddTx(tx, status.Committed)
		err = txDiff.Apply(stateDiff)
		if err != nil {
			txID := tx.ID()
			builder.Mempool.MarkDropped(txID, err)
			continue
		}

		remainingSize -= len(tx.Bytes())
		blockTxs = append(blockTxs, tx)
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

type stateGetter struct {
	state state.Chain
}

func (s stateGetter) GetState(ids.ID) (state.Chain, bool) {
	return s.state, true
}

func wrapState(parentState state.Chain) (state.Diff, error) {
	return state.NewDiff(ids.Empty, stateGetter{
		state: parentState,
	})
}
