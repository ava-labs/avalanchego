// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

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
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	txbuilder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

// targetBlockSize is maximum number of transaction bytes to place into a
// StandardBlock
const targetBlockSize = 128 * units.KiB

var (
	_ Builder = &builder{}

	errEndOfTime       = errors.New("program time is suspiciously far in the future")
	errNoPendingBlocks = errors.New("no pending blocks")
)

type Builder interface {
	mempool.Mempool
	mempool.BlockTimer
	Network

	// set preferred block on top of which we'll build next
	SetPreference(blockID ids.ID)

	// get preferred block on top of which we'll build next
	Preferred() (snowman.Block, error)

	// AddUnverifiedTx verifier the tx before adding it to mempool
	AddUnverifiedTx(tx *txs.Tx) error

	// BuildBlock is called on timer clock to attempt to create
	// next block
	BuildBlock() (snowman.Block, error)

	// Shutdown cleanly shuts BlockBuilder down
	Shutdown()
}

// builder implements a simple builder to convert txs into valid blocks
type builder struct {
	mempool.Mempool
	Network

	txBuilder         txbuilder.Builder
	txExecutorBackend *txexecutor.Backend
	blkManager        blockexecutor.Manager

	// ID of the preferred block to build on top of
	preferredBlockID ids.ID

	// channel to send messages to the consensus engine
	toEngine chan<- common.Message

	// This timer goes off when it is time for the next validator to add/leave
	// the validator set. When it goes off ResetTimer() is called, potentially
	// triggering creation of a new block.
	timer *timer.Timer
}

func New(
	mempool mempool.Mempool,
	txBuilder txbuilder.Builder,
	txExecutorBackend *txexecutor.Backend,
	blkManager blockexecutor.Manager,
	toEngine chan<- common.Message,
	appSender common.AppSender,
) Builder {
	builder := &builder{
		Mempool:           mempool,
		txBuilder:         txBuilder,
		txExecutorBackend: txExecutorBackend,
		blkManager:        blkManager,
		toEngine:          toEngine,
	}

	builder.timer = timer.NewTimer(builder.setNextBuildBlockTime)

	builder.Network = NewNetwork(
		txExecutorBackend.Ctx,
		builder,
		appSender,
	)

	go txExecutorBackend.Ctx.Log.RecoverAndPanic(builder.timer.Dispatch)
	return builder
}

func (b *builder) SetPreference(blockID ids.ID) {
	if blockID == b.preferredBlockID {
		// If the preference didn't change, then this is a noop
		return
	}
	b.preferredBlockID = blockID
	b.ResetBlockTimer()
}

func (b *builder) Preferred() (snowman.Block, error) {
	return b.blkManager.GetBlock(b.preferredBlockID)
}

// AddUnverifiedTx verifies a transaction and attempts to add it to the mempool
func (b *builder) AddUnverifiedTx(tx *txs.Tx) error {
	txID := tx.ID()
	if b.Mempool.Has(txID) {
		// If the transaction is already in the mempool - then it looks the same
		// as if it was successfully added
		return nil
	}

	verifier := txexecutor.MempoolTxVerifier{
		Backend:       b.txExecutorBackend,
		ParentID:      b.preferredBlockID, // We want to build off of the preferred block
		StateVersions: b.blkManager,
		Tx:            tx,
	}
	if err := tx.Unsigned.Visit(&verifier); err != nil {
		b.MarkDropped(txID, err.Error())
		return err
	}

	if err := b.Mempool.Add(tx); err != nil {
		return err
	}
	return b.GossipTx(tx)
}

// BuildBlock builds a block to be added to consensus
func (b *builder) BuildBlock() (snowman.Block, error) {
	b.Mempool.DisableAdding()
	defer func() {
		b.Mempool.EnableAdding()
		b.ResetBlockTimer()
	}()

	ctx := b.txExecutorBackend.Ctx
	ctx.Log.Debug("starting to attempt to build a block")

	// Get the block to build on top of and retrieve the new block's context.
	preferred, err := b.Preferred()
	if err != nil {
		return nil, err
	}
	preferredID := preferred.ID()
	nextHeight := preferred.Height() + 1

	preferredState, ok := b.blkManager.GetState(preferredID)
	if !ok {
		return nil, fmt.Errorf("could not retrieve state for block %s", preferredID)
	}

	// Try building a standard block.
	if b.Mempool.HasDecisionTxs() {
		txs := b.Mempool.PopDecisionTxs(targetBlockSize)
		statelessBlk, err := blocks.NewStandardBlock(preferredID, nextHeight, txs)
		if err != nil {
			return nil, err
		}
		return b.blkManager.NewBlock(statelessBlk), nil
	}

	// Try building a proposal block that rewards a staker.
	stakerTxID, shouldReward, err := b.getNextStakerToReward(preferredState)
	if err != nil {
		return nil, err
	}
	if shouldReward {
		rewardValidatorTx, err := b.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return nil, err
		}
		statelessBlk, err := blocks.NewProposalBlock(
			preferredID,
			nextHeight,
			rewardValidatorTx,
		)
		if err != nil {
			return nil, err
		}
		return b.blkManager.NewBlock(statelessBlk), nil
	}

	// Try building a proposal block that advances the chain timestamp.
	nextChainTime, shouldAdvanceTime, err := b.getNextChainTime(preferredState)
	if err != nil {
		return nil, err
	}
	if shouldAdvanceTime {
		advanceTimeTx, err := b.txBuilder.NewAdvanceTimeTx(nextChainTime)
		if err != nil {
			return nil, err
		}
		statelessBlk, err := blocks.NewProposalBlock(
			preferredID,
			nextHeight,
			advanceTimeTx,
		)
		if err != nil {
			return nil, err
		}
		return b.blkManager.NewBlock(statelessBlk), nil
	}

	// Clean out the mempool's transactions with invalid timestamps.
	if hasProposalTxs := b.dropTooEarlyMempoolProposalTxs(); !hasProposalTxs {
		ctx.Log.Debug("no pending blocks to build")
		return nil, errNoPendingBlocks
	}

	// Get the proposal transaction that should be issued.
	tx := b.Mempool.PopProposalTx()
	startTime := tx.Unsigned.(txs.StakerTx).StartTime()

	// If the chain timestamp is too far in the past to issue this transaction
	// but according to local time, it's ready to be issued, then attempt to
	// advance the timestamp, so it can be issued.
	maxChainStartTime := preferredState.GetTimestamp().Add(txexecutor.MaxFutureStartTime)
	if startTime.After(maxChainStartTime) {
		b.Mempool.AddProposalTx(tx)

		advanceTimeTx, err := b.txBuilder.NewAdvanceTimeTx(b.txExecutorBackend.Clk.Time())
		if err != nil {
			return nil, err
		}
		statelessBlk, err := blocks.NewProposalBlock(preferredID, nextHeight, advanceTimeTx)
		if err != nil {
			return nil, err
		}
		return b.blkManager.NewBlock(statelessBlk), nil
	}

	statelessBlk, err := blocks.NewProposalBlock(preferredID, nextHeight, tx)
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
	// Next time the context lock is released, we can attempt to reset the block
	// timer.
	b.timer.SetTimeoutIn(0)
}

// getNextStakerToReward returns the next staker txID to remove from the staking
// set with a RewardValidatorTx rather than an AdvanceTimeTx.
// Returns:
// - [txID] of the next staker to reward
// - [shouldReward] if the txID exists and is ready to be rewarded
// - [err] if something bad happened
func (b *builder) getNextStakerToReward(preferredState state.Chain) (ids.ID, bool, error) {
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
func (b *builder) getNextChainTime(preferredState state.Chain) (time.Time, bool, error) {
	nextStakerChangeTime, err := txexecutor.GetNextStakerChangeTime(preferredState)
	if err != nil {
		return time.Time{}, false, err
	}

	now := b.txExecutorBackend.Clk.Time()
	return nextStakerChangeTime, !now.Before(nextStakerChangeTime), nil
}

// dropTooEarlyMempoolProposalTxs drops mempool's validators whose start time is
// too close in the future i.e. within local time plus Delta.
// dropTooEarlyMempoolProposalTxs makes sure that mempool's top proposal tx has
// a valid starting time but does not necessarily remove all txs since
// popped txs are not necessarily ordered by start time.
// Returns true/false if mempool is non-empty/empty following cleanup.
func (b *builder) dropTooEarlyMempoolProposalTxs() bool {
	ctx := b.txExecutorBackend.Ctx
	now := b.txExecutorBackend.Clk.Time()
	syncTime := now.Add(txexecutor.SyncBound)
	for b.Mempool.HasProposalTx() {
		tx := b.Mempool.PopProposalTx()
		startTime := tx.Unsigned.(txs.StakerTx).StartTime()
		if !startTime.Before(syncTime) {
			b.Mempool.AddProposalTx(tx)
			return true
		}

		txID := tx.ID()
		errMsg := fmt.Sprintf(
			"synchrony bound (%s) is later than staker start time (%s)",
			syncTime,
			startTime,
		)

		b.Mempool.MarkDropped(txID, errMsg) // cache tx as dropped
		ctx.Log.Debug("dropping tx",
			zap.String("reason", errMsg),
			zap.Stringer("txID", txID),
		)
	}
	return false
}

func (b *builder) setNextBuildBlockTime() {
	ctx := b.txExecutorBackend.Ctx

	// Grabbing the lock here enforces that this function is not called mid-way
	// through modifying of the state.
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	if !b.txExecutorBackend.Bootstrapped.GetValue() {
		ctx.Log.Verbo("skipping block timer reset",
			zap.String("reason", "not bootstrapped"),
		)
		return
	}

	// If there is a pending transaction trigger building of a block with that transaction
	if b.Mempool.HasDecisionTxs() {
		b.notifyBlockReady()
		return
	}

	preferredState, ok := b.blkManager.GetState(b.preferredBlockID)
	if !ok {
		// The preferred block should always be a decision block
		ctx.Log.Error("couldn't get preferred block state",
			zap.Stringer("preferredID", b.preferredBlockID),
			zap.Stringer("lastAcceptedID", b.blkManager.LastAccepted()),
		)
		return
	}

	_, shouldReward, err := b.getNextStakerToReward(preferredState)
	if err != nil {
		ctx.Log.Error("failed to fetch next staker to reward",
			zap.Stringer("preferredID", b.preferredBlockID),
			zap.Stringer("lastAcceptedID", b.blkManager.LastAccepted()),
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
		ctx.Log.Error("failed to fetch next chain time",
			zap.Stringer("preferredID", b.preferredBlockID),
			zap.Stringer("lastAcceptedID", b.blkManager.LastAccepted()),
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

	now := b.txExecutorBackend.Clk.Time()
	nextStakerChangeTime, err := txexecutor.GetNextStakerChangeTime(preferredState)
	if err != nil {
		ctx.Log.Error("couldn't get next staker change time",
			zap.Stringer("preferredID", b.preferredBlockID),
			zap.Stringer("lastAcceptedID", b.blkManager.LastAccepted()),
			zap.Error(err),
		)
		return
	}
	waitTime := nextStakerChangeTime.Sub(now)
	ctx.Log.Debug("setting next scheduled event",
		zap.Time("nextEventTime", nextStakerChangeTime),
		zap.Duration("timeUntil", waitTime),
	)

	// Wake up when it's time to add/remove the next validator
	b.timer.SetTimeoutIn(waitTime)
}

// notifyBlockReady tells the consensus engine that a new block is ready to be
// created
func (b *builder) notifyBlockReady() {
	select {
	case b.toEngine <- common.PendingTxs:
	default:
		b.txExecutorBackend.Ctx.Log.Debug("dropping message to consensus engine")
	}
}
