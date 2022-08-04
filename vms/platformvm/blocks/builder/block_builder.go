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
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	p_tx_builder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

// targetBlockSize is maximum number of transaction bytes to place into a
// StandardBlock
const targetBlockSize = 128 * units.KiB

var (
	_ BlockBuilder = &blockBuilder{}

	errEndOfTime       = errors.New("program time is suspiciously far in the future")
	errNoPendingBlocks = errors.New("no pending blocks")
)

type BlockBuilder interface {
	mempool.Mempool
	mempool.BlockTimer
	Network

	// StartTimer starts a timer to periodically check whether a block can be built
	StartTimer()

	// set preferred block on top of which we'll build next
	SetPreference(blockID ids.ID) error

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

// blockBuilder implements a simple blockBuilder to convert txs into valid blocks
type blockBuilder struct {
	mempool.Mempool
	Network

	txBuilder         p_tx_builder.Builder
	txExecutorBackend txexecutor.Backend
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

// Initialize this builder.
func NewBlockBuilder(
	mempool mempool.Mempool,
	txBuilder p_tx_builder.Builder,
	txExecutorBackend txexecutor.Backend,
	blkManager blockexecutor.Manager,
	toEngine chan<- common.Message,
	appSender common.AppSender,
) BlockBuilder {
	builder := &blockBuilder{
		Mempool:           mempool,
		txBuilder:         txBuilder,
		txExecutorBackend: txExecutorBackend,
		blkManager:        blkManager,
		toEngine:          toEngine,
	}

	builder.timer = timer.NewTimer(
		func() {
			txExecutorBackend.Ctx.Lock.Lock()
			defer txExecutorBackend.Ctx.Lock.Unlock()

			builder.ResetBlockTimer()
		})

	builder.Network = NewNetwork(
		txExecutorBackend.Ctx,
		builder,
		appSender,
	)

	go txExecutorBackend.Ctx.Log.RecoverAndPanic(builder.timer.Dispatch)
	return builder
}

func (b *blockBuilder) StartTimer() { b.timer.Dispatch() }

func (b *blockBuilder) SetPreference(blockID ids.ID) error {
	if blockID == b.preferredBlockID {
		// If the preference didn't change, then this is a noop
		return nil
	}
	b.preferredBlockID = blockID
	b.ResetBlockTimer()
	return nil
}

func (b *blockBuilder) Preferred() (snowman.Block, error) {
	return b.blkManager.GetBlock(b.preferredBlockID)
}

// AddUnverifiedTx verifies a transaction and attempts to add it to the mempool
func (b *blockBuilder) AddUnverifiedTx(tx *txs.Tx) error {
	txID := tx.ID()
	if b.Mempool.Has(txID) {
		// If the transaction is already in the mempool - then it looks the same
		// as if it was successfully added
		return nil
	}

	verifier := txexecutor.MempoolTxVerifier{
		Backend:  &b.txExecutorBackend,
		ParentID: b.preferredBlockID, // We want to build off of the preferred block
		Tx:       tx,
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
func (b *blockBuilder) BuildBlock() (snowman.Block, error) {
	b.Mempool.DisableAdding()
	defer func() {
		b.Mempool.EnableAdding()
		b.ResetBlockTimer()
	}()

	ctx := b.txExecutorBackend.Ctx
	ctx.Log.Debug("starting to attempt to build a block")
	blkBuildingStrategy, err := b.getBuildingStrategy()
	if err != nil {
		return nil, err
	}

	return blkBuildingStrategy.build()
}

func (b *blockBuilder) Shutdown() {
	if b.timer == nil {
		return
	}

	// There is a potential deadlock if the timer is about to execute a timeout.
	// So, the lock must be released before stopping the timer.
	ctx := b.txExecutorBackend.Ctx
	ctx.Lock.Unlock()
	b.timer.Stop()
	ctx.Lock.Lock()
}

// resetTimer Check if there is a block ready to be added to consensus. If so, notify the
// consensus engine.
func (b *blockBuilder) ResetBlockTimer() {
	blkBuildStrategy, err := b.getBuildingStrategy()
	if err != nil {
		return
	}

	// check if there are txs to be included in the block
	// or if chain time can be moved ahead via block timestamp
	hasContent, err := blkBuildStrategy.hasContent()
	if err != nil {
		return
	}
	if hasContent {
		b.notifyBlockReady()
		return
	}

	// Wake up when it's time to add/remove the next validator
	var (
		ctx           = b.txExecutorBackend.Ctx
		now           = b.txExecutorBackend.Clk.Time()
		stateVersions = b.txExecutorBackend.StateVersions
	)

	preferredState, ok := stateVersions.GetState(b.preferredBlockID)
	if !ok {
		// The preferred block should always be a decision block
		ctx.Log.Error("couldn't get preferred block state",
			zap.Stringer("blkID", b.preferredBlockID),
		)
		return
	}
	nextStakerChangeTime, err := txexecutor.GetNextStakerChangeTime(preferredState)
	if err != nil {
		ctx.Log.Error("couldn't get next staker change time",
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
	nextStakerChangeTime, err := txexecutor.GetNextStakerChangeTime(preferredState)
	if err != nil {
		return time.Time{}, false, err
	}

	now := b.txExecutorBackend.Clk.Time()
	return nextStakerChangeTime, !now.Before(nextStakerChangeTime), nil
}

// dropExpiredProposalTxs drops add validator transactions in the mempool
// whose start time is not sufficiently far in the future
// (i.e. within local time plus [MaxFutureStartFrom]).
// Guarantees that [PeekProposalTx] will return a valid tx after calling.
// May not remove all expired txs since txs aren't necessarily popped
// ordered by start time.
func (b *blockBuilder) dropExpiredProposalTxs() {
	var (
		ctx      = b.txExecutorBackend.Ctx
		now      = b.txExecutorBackend.Clk.Time()
		syncTime = now.Add(txexecutor.SyncBound)
	)
	for b.Mempool.HasProposalTx() {
		tx := b.Mempool.PeekProposalTx()
		startTime := tx.Unsigned.(txs.StakerTx).StartTime()
		if !startTime.Before(syncTime) {
			// The next proposal tx in the mempool starts
			// sufficiently far in the future.
			return
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
}

// notifyBlockReady tells the consensus engine that a new block is ready to be
// created
func (b *blockBuilder) notifyBlockReady() {
	select {
	case b.toEngine <- common.PendingTxs:
	default:
		ctx := b.txExecutorBackend.Ctx
		ctx.Log.Debug("dropping message to consensus engine")
	}
}
