// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
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
	_ Builder = (*builder)(nil)

	errEndOfTime       = errors.New("program time is suspiciously far in the future")
	errNoPendingBlocks = errors.New("no pending blocks")
	errChainNotSynced  = errors.New("chain not synced")
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
	BuildBlock(context.Context) (snowman.Block, error)

	// Shutdown cleanly shuts Builder down
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
	if !b.txExecutorBackend.Bootstrapped.Get() {
		return errChainNotSynced
	}

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
		b.MarkDropped(txID, err)
		return err
	}

	if err := b.Mempool.Add(tx); err != nil {
		return err
	}
	return b.GossipTx(tx)
}

// BuildBlock builds a block to be added to consensus.
// This method removes the transactions from the returned
// blocks from the mempool.
func (b *builder) BuildBlock(context.Context) (snowman.Block, error) {
	b.Mempool.DisableAdding()
	defer func() {
		b.Mempool.EnableAdding()
		b.ResetBlockTimer()
	}()

	ctx := b.txExecutorBackend.Ctx
	ctx.Log.Debug("starting to attempt to build a block")

	statelessBlk, err := b.buildBlock()
	if err != nil {
		return nil, err
	}

	// Remove selected txs from mempool now that we are returning the block to
	// the consensus engine.
	txs := statelessBlk.Txs()
	b.Mempool.Remove(txs)
	return b.blkManager.NewBlock(statelessBlk), nil
}

// Returns the block we want to build and issue.
// Only modifies state to remove expired proposal txs.
func (b *builder) buildBlock() (blocks.Block, error) {
	// Get the block to build on top of and retrieve the new block's context.
	preferred, err := b.Preferred()
	if err != nil {
		return nil, err
	}
	preferredID := preferred.ID()
	nextHeight := preferred.Height() + 1
	preferredState, ok := b.blkManager.GetState(preferredID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", state.ErrMissingParentState, preferredID)
	}

	timestamp := b.txExecutorBackend.Clk.Time()
	if parentTime := preferred.Timestamp(); parentTime.After(timestamp) {
		timestamp = parentTime
	}
	// [timestamp] = max(now, parentTime)

	nextStakerChangeTime, err := txexecutor.GetNextStakerChangeTime(preferredState)
	if err != nil {
		return nil, fmt.Errorf("could not calculate next staker change time: %w", err)
	}

	// timeWasCapped means that [timestamp] was reduced to
	// [nextStakerChangeTime]. It is used as a flag for [buildApricotBlock] to
	// be willing to issue an advanceTimeTx. It is also used as a flag for
	// [buildBanffBlock] to force the issuance of an empty block to advance
	// the time forward; if there are no available transactions.
	timeWasCapped := !timestamp.Before(nextStakerChangeTime)
	if timeWasCapped {
		timestamp = nextStakerChangeTime
	}
	// [timestamp] = min(max(now, parentTime), nextStakerChangeTime)

	return buildBlock(
		b,
		preferredID,
		nextHeight,
		timestamp,
		timeWasCapped,
		preferredState,
	)
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

// dropExpiredStakerTxs drops add validator/delegator transactions in the
// mempool whose start time is not sufficiently far in the future
// (i.e. within local time plus [MaxFutureStartFrom]).
func (b *builder) dropExpiredStakerTxs(timestamp time.Time) {
	minStartTime := timestamp.Add(txexecutor.SyncBound)
	for b.Mempool.HasStakerTx() {
		tx := b.Mempool.PeekStakerTx()
		startTime := tx.Unsigned.(txs.Staker).StartTime()
		if !startTime.Before(minStartTime) {
			// The next proposal tx in the mempool starts sufficiently far in
			// the future.
			return
		}

		txID := tx.ID()
		err := fmt.Errorf(
			"synchrony bound (%s) is later than staker start time (%s)",
			minStartTime,
			startTime,
		)

		b.Mempool.Remove([]*txs.Tx{tx})
		b.Mempool.MarkDropped(txID, err) // cache tx as dropped
		b.txExecutorBackend.Ctx.Log.Debug("dropping tx",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)
	}
}

func (b *builder) setNextBuildBlockTime() {
	ctx := b.txExecutorBackend.Ctx

	// Grabbing the lock here enforces that this function is not called mid-way
	// through modifying of the state.
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	if !b.txExecutorBackend.Bootstrapped.Get() {
		ctx.Log.Verbo("skipping block timer reset",
			zap.String("reason", "not bootstrapped"),
		)
		return
	}

	if _, err := b.buildBlock(); err == nil {
		// We can build a block now
		b.notifyBlockReady()
		return
	}

	// Wake up when it's time to add/remove the next validator/delegator
	preferredState, ok := b.blkManager.GetState(b.preferredBlockID)
	if !ok {
		// The preferred block should always be a decision block
		ctx.Log.Error("couldn't get preferred block state",
			zap.Stringer("preferredID", b.preferredBlockID),
			zap.Stringer("lastAcceptedID", b.blkManager.LastAccepted()),
		)
		return
	}

	nextStakerChangeTime, err := txexecutor.GetNextStakerChangeTime(preferredState)
	if err != nil {
		ctx.Log.Error("couldn't get next staker change time",
			zap.Stringer("preferredID", b.preferredBlockID),
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

// [timestamp] is min(max(now, parent timestamp), next staker change time)
func buildBlock(
	builder *builder,
	parentID ids.ID,
	height uint64,
	timestamp time.Time,
	forceAdvanceTime bool,
	parentState state.Chain,
) (blocks.Block, error) {
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

		return blocks.NewBanffProposalBlock(
			timestamp,
			parentID,
			height,
			rewardValidatorTx,
		)
	}

	// Clean out the mempool's transactions with invalid timestamps.
	builder.dropExpiredStakerTxs(timestamp)

	// If there is no reason to build a block, don't.
	if !builder.Mempool.HasTxs() && !forceAdvanceTime {
		builder.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return nil, errNoPendingBlocks
	}

	// Issue a block with as many transactions as possible.
	return blocks.NewBanffStandardBlock(
		timestamp,
		parentID,
		height,
		builder.Mempool.PeekTxs(targetBlockSize),
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
		// If the staker is a permissionless staker (not a permissioned subnet
		// validator), it's the next staker we will want to remove with a
		// RewardValidatorTx rather than an AdvanceTimeTx.
		if priority != txs.SubnetPermissionedValidatorCurrentPriority {
			return currentStaker.TxID, chainTimestamp.Equal(currentStaker.EndTime), nil
		}
	}
	return ids.Empty, false, nil
}
