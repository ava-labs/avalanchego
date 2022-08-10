// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

// TargetBlockSize is maximum number of transaction bytes to place into a
// StandardBlock
const TargetBlockSize = 128 * units.KiB

var (
	_ mempool.BlockTimer = &blockBuilder{}

	errEndOfTime       = errors.New("program time is suspiciously far in the future")
	errNoPendingBlocks = errors.New("no pending blocks")
)

// blockBuilder implements a simple blockBuilder to convert txs into valid blocks
type blockBuilder struct {
	mempool.Mempool

	// TODO: factor out VM into separable interfaces
	vm *VM

	// channel to send messages to the consensus engine
	toEngine chan<- common.Message

	// This timer goes off when it is time for the next validator to add/leave
	// the validator set. When it goes off ResetTimer() is called, potentially
	// triggering creation of a new block.
	timer *timer.Timer
}

// Initialize this builder.
func (b *blockBuilder) Initialize(
	mempool mempool.Mempool,
	vm *VM,
	toEngine chan<- common.Message,
) {
	b.Mempool = mempool
	b.vm = vm
	b.toEngine = toEngine

	b.timer = timer.NewTimer(func() {
		b.vm.ctx.Lock.Lock()
		defer b.vm.ctx.Lock.Unlock()

		b.ResetBlockTimer()
	})
	go b.vm.ctx.Log.RecoverAndPanic(b.timer.Dispatch)
}

// AddUnverifiedTx verifies a transaction and attempts to add it to the mempool
func (b *blockBuilder) AddUnverifiedTx(tx *txs.Tx) error {
	txID := tx.ID()
	if b.Has(txID) {
		// If the transaction is already in the mempool - then it looks the same
		// as if it was successfully added
		return nil
	}

	verifier := executor.MempoolTxVerifier{
		Backend:       b.vm.txExecutorBackend,
		ParentID:      b.vm.preferred, // We want to build off of the preferred block
		StateVersions: b.vm.manager,
		Tx:            tx,
	}
	if err := tx.Unsigned.Visit(&verifier); err != nil {
		b.MarkDropped(txID, err.Error())
		return err
	}

	if err := b.Mempool.Add(tx); err != nil {
		return err
	}
	return b.vm.GossipTx(tx)
}

// BuildBlock builds a block to be added to consensus
func (b *blockBuilder) BuildBlock() (snowman.Block, error) {
	b.Mempool.DisableAdding()
	defer func() {
		b.Mempool.EnableAdding()
		b.ResetBlockTimer()
	}()

	b.vm.ctx.Log.Debug("starting to attempt to build a block")

	// Get the block to build on top of and retrieve the new block's context.
	preferred, err := b.vm.Preferred()
	if err != nil {
		return nil, fmt.Errorf("couldn't get preferred block: %w", err)
	}
	preferredID := preferred.ID()
	nextHeight := preferred.Height() + 1

	preferredState, ok := b.vm.manager.GetState(preferredID)
	if !ok {
		return nil, fmt.Errorf("could not retrieve state for block %s", preferredID)
	}

	// Try building a standard block.
	if b.HasDecisionTxs() {
		txs := b.PopDecisionTxs(TargetBlockSize)
		statelessBlk, err := blocks.NewStandardBlock(preferredID, nextHeight, txs)
		if err != nil {
			return nil, err
		}
		return b.vm.manager.NewBlock(statelessBlk), nil
	}

	// Try building a proposal block that rewards a staker.
	stakerTxID, shouldReward, err := b.getNextStakerToReward(preferredState)
	if err != nil {
		return nil, err
	}
	if shouldReward {
		rewardValidatorTx, err := b.vm.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return nil, err
		}
		statelessBlk, err := blocks.NewProposalBlock(preferredID, nextHeight, rewardValidatorTx)
		if err != nil {
			return nil, err
		}
		return b.vm.manager.NewBlock(statelessBlk), nil
	}

	// Try building a proposal block that advances the chain timestamp.
	nextChainTime, shouldAdvanceTime, err := b.getNextChainTime(preferredState)
	if err != nil {
		return nil, err
	}
	if shouldAdvanceTime {
		advanceTimeTx, err := b.vm.txBuilder.NewAdvanceTimeTx(nextChainTime)
		if err != nil {
			return nil, err
		}
		statelessBlk, err := blocks.NewProposalBlock(preferredID, nextHeight, advanceTimeTx)
		if err != nil {
			return nil, err
		}
		return b.vm.manager.NewBlock(statelessBlk), nil
	}

	// Clean out the mempool's transactions with invalid timestamps.
	if hasProposalTxs := b.dropTooEarlyMempoolProposalTxs(); !hasProposalTxs {
		b.vm.ctx.Log.Debug("no pending blocks to build")
		return nil, errNoPendingBlocks
	}

	// Get the proposal transaction that should be issued.
	tx := b.PopProposalTx()
	startTime := tx.Unsigned.(txs.StakerTx).StartTime()

	// If the chain timestamp is too far in the past to issue this transaction
	// but according to local time, it's ready to be issued, then attempt to
	// advance the timestamp, so it can be issued.
	maxChainStartTime := preferredState.GetTimestamp().Add(executor.MaxFutureStartTime)
	if startTime.After(maxChainStartTime) {
		b.AddProposalTx(tx)

		advanceTimeTx, err := b.vm.txBuilder.NewAdvanceTimeTx(b.vm.clock.Time())
		if err != nil {
			return nil, err
		}
		statelessBlk, err := blocks.NewProposalBlock(preferredID, nextHeight, advanceTimeTx)
		if err != nil {
			return nil, err
		}
		return b.vm.manager.NewBlock(statelessBlk), nil
	}

	statelessBlk, err := blocks.NewProposalBlock(preferredID, nextHeight, tx)
	if err != nil {
		return nil, err
	}
	return b.vm.manager.NewBlock(statelessBlk), nil
}

func (b *blockBuilder) ResetBlockTimer() {
	// If there is a pending transaction trigger building of a block with that transaction
	if b.HasDecisionTxs() {
		b.notifyBlockReady()
		return
	}

	preferredState, ok := b.vm.manager.GetState(b.vm.preferred)
	if !ok {
		b.vm.ctx.Log.Error("couldn't get preferred block state",
			zap.Stringer("blkID", b.vm.preferred),
		)
		return
	}

	_, shouldReward, err := b.getNextStakerToReward(preferredState)
	if err != nil {
		b.vm.ctx.Log.Error("failed to fetch next staker to reward",
			zap.Error(err),
		)
		return
	}
	if shouldReward {
		b.notifyBlockReady()
		return
	}

	_, shouldAdvanceTime, err := b.getNextChainTime(preferredState)
	if err != nil {
		b.vm.ctx.Log.Error("failed to fetch next chain time",
			zap.Error(err),
		)
		return
	}
	if shouldAdvanceTime {
		// time is at or after the time for the next validator to join/leave
		b.notifyBlockReady() // Should issue a proposal to advance timestamp
		return
	}

	if hasProposalTxs := b.dropTooEarlyMempoolProposalTxs(); hasProposalTxs {
		b.notifyBlockReady() // Should issue a ProposeAddValidator
		return
	}

	now := b.vm.clock.Time()
	nextStakerChangeTime, err := executor.GetNextStakerChangeTime(preferredState)
	if err != nil {
		b.vm.ctx.Log.Error("couldn't get next staker change time",
			zap.Error(err),
		)
		return
	}
	waitTime := nextStakerChangeTime.Sub(now)
	b.vm.ctx.Log.Debug("setting next scheduled event",
		zap.Time("nextEventTime", nextStakerChangeTime),
		zap.Duration("timeUntil", waitTime),
	)

	// Wake up when it's time to add/remove the next validator
	b.timer.SetTimeoutIn(waitTime)
}

// Shutdown this mempool
func (b *blockBuilder) Shutdown() {
	if b.timer == nil {
		return
	}

	// There is a potential deadlock if the timer is about to execute a timeout.
	// So, the lock must be released before stopping the timer.
	b.vm.ctx.Lock.Unlock()
	b.timer.Stop()
	b.vm.ctx.Lock.Lock()
}

// getNextStakerToReward returns the next staker txID to remove from the staking
// set with a RewardValidatorTx rather than an AdvanceTimeTx.
// Returns:
// - [txID] of the next staker to reward
// - [shouldReward] if the txID exists and is ready to be rewarded
// - [err] if something bad happened
func (b *blockBuilder) getNextStakerToReward(preferredState state.Chain) (ids.ID, bool, error) {
	currentChainTimestamp := preferredState.GetTimestamp()
	if !currentChainTimestamp.Before(mockable.MaxTime) {
		return ids.Empty, false, errEndOfTime
	}

	currentStakerIterator, err := preferredState.GetCurrentStakerIterator()
	if err != nil {
		return ids.Empty, false, err
	}
	defer currentStakerIterator.Release()

	for currentStakerIterator.Next() {
		currentStaker := currentStakerIterator.Value()
		priority := currentStaker.Priority
		// If the staker is a primary network staker (not a subnet validator),
		// it's the next staker we will want to remove with a RewardValidatorTx
		// rather than an AdvanceTimeTx.
		if priority == state.PrimaryNetworkDelegatorCurrentPriority ||
			priority == state.PrimaryNetworkValidatorCurrentPriority {
			return currentStaker.TxID, currentChainTimestamp.Equal(currentStaker.EndTime), nil
		}
	}
	return ids.Empty, false, nil
}

// getNextChainTime returns the timestamp for the next chain time and if the
// local time is >= time of the next staker set change.
func (b *blockBuilder) getNextChainTime(preferredState state.Chain) (time.Time, bool, error) {
	nextStakerChangeTime, err := executor.GetNextStakerChangeTime(preferredState)
	if err != nil {
		return time.Time{}, false, err
	}

	now := b.vm.clock.Time()
	return nextStakerChangeTime, !now.Before(nextStakerChangeTime), nil
}

// dropTooEarlyMempoolProposalTxs drops mempool's validators whose start time is
// too close in the future i.e. within local time plus Delta.
// dropTooEarlyMempoolProposalTxs makes sure that mempool's top proposal tx has
// a valid starting time but does not necessarily remove all txs since
// popped txs are not necessarily ordered by start time.
// Returns true/false if mempool is non-empty/empty following cleanup.
func (b *blockBuilder) dropTooEarlyMempoolProposalTxs() bool {
	now := b.vm.clock.Time()
	syncTime := now.Add(executor.SyncBound)
	for b.HasProposalTx() {
		tx := b.PopProposalTx()
		startTime := tx.Unsigned.(txs.StakerTx).StartTime()
		if !startTime.Before(syncTime) {
			b.AddProposalTx(tx)
			return true
		}

		txID := tx.ID()
		errMsg := fmt.Sprintf(
			"synchrony bound (%s) is later than staker start time (%s)",
			syncTime,
			startTime,
		)

		b.vm.blockBuilder.MarkDropped(txID, errMsg) // cache tx as dropped
		b.vm.ctx.Log.Debug("dropping tx",
			zap.String("reason", errMsg),
			zap.Stringer("txID", txID),
		)
	}
	return false
}

// notifyBlockReady tells the consensus engine that a new block is ready to be
// created
func (b *blockBuilder) notifyBlockReady() {
	select {
	case b.toEngine <- common.PendingTxs:
	default:
		b.vm.ctx.Log.Debug("dropping message to consensus engine")
	}
}
