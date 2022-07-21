// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"

	p_tx_builder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
)

var (
	_ mempool.BlockTimer = &blockBuilder{}
	_ BlockBuilder       = &blockBuilder{}

	errEndOfTime       = errors.New("program time is suspiciously far in the future")
	errNoPendingBlocks = errors.New("no pending blocks")
)

// TargetBlockSize is maximum number of transaction bytes to place into a
// StandardBlock
const TargetBlockSize = 128 * units.KiB

type BlockBuilder interface {
	mempool.Mempool
	mempool.BlockTimer
	Network

	StartTimer()
	SetPreference(blockID ids.ID) error
	Preferred() (snowman.Block, error)
	AddUnverifiedTx(tx *txs.Tx) error
	BuildBlock() (snowman.Block, error)
	Shutdown()
}

// blockBuilder implements a simple blockBuilder to convert txs into valid blocks
type blockBuilder struct {
	mempool.Mempool
	Network

	txBuilder         p_tx_builder.Builder
	txExecutorBackend executor.Backend
	blkManager        stateful.Manager

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
	txExecutorBackend executor.Backend,
	blkManager stateful.Manager,
	toEngine chan<- common.Message,
	appSender common.AppSender,
) BlockBuilder {
	blkBuilder := &blockBuilder{
		Mempool:           mempool,
		txBuilder:         txBuilder,
		txExecutorBackend: txExecutorBackend,
		blkManager:        blkManager,
		toEngine:          toEngine,
	}

	blkBuilder.timer = timer.NewTimer(
		func() {
			txExecutorBackend.Ctx.Lock.Lock()
			defer txExecutorBackend.Ctx.Lock.Unlock()

			blkBuilder.resetTimer()
		},
	)

	blkBuilder.Network = NewNetwork(
		txExecutorBackend.Ctx,
		blkBuilder,
		txExecutorBackend.Config.ApricotPhase4Time,
		appSender,
	)
	return blkBuilder
}

func (b *blockBuilder) StartTimer() { b.timer.Dispatch() }

func (b *blockBuilder) SetPreference(blockID ids.ID) error {
	if blockID == b.preferredBlockID {
		// If the preference didn't change, then this is a noop
		return nil
	}
	b.preferredBlockID = blockID
	b.resetTimer()
	return nil
}

func (b *blockBuilder) Preferred() (snowman.Block, error) {
	return b.blkManager.GetBlock(b.preferredBlockID)
}

func (b *blockBuilder) ResetBlockTimer() { b.resetTimer() }

// AddUnverifiedTx verifies a transaction and attempts to add it to the mempool
func (b *blockBuilder) AddUnverifiedTx(tx *txs.Tx) error {
	txID := tx.ID()
	if b.Mempool.Has(txID) {
		// If the transaction is already in the mempool - then it looks the same
		// as if it was successfully added
		return nil
	}

	verifier := executor.MempoolTxVerifier{
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
	var (
		ctx           = b.txExecutorBackend.Ctx
		stateVersions = b.txExecutorBackend.StateVersions
	)

	b.Mempool.DisableAdding()
	defer func() {
		b.Mempool.EnableAdding()
		b.resetTimer()
	}()

	ctx.Log.Debug("starting to attempt to build a block")

	// Get the block to build on top of and retrieve the new block's context.
	preferred, err := b.Preferred()
	if err != nil {
		return nil, err
	}
	preferredID := preferred.ID()
	nextHeight := preferred.Height() + 1

	preferredState, ok := stateVersions.GetState(preferredID)
	if !ok {
		return nil, fmt.Errorf("could not retrieve state for block %s, which should be a decision block", preferredID)
	}

	// Try building a standard block.
	if b.Mempool.HasDecisionTxs() {
		txs := b.Mempool.PopDecisionTxs(TargetBlockSize)
		statelessBlk, err := stateless.NewStandardBlock(preferredID, nextHeight, txs)
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
		statelessBlk, err := stateless.NewProposalBlock(
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
		statelessBlk, err := stateless.NewProposalBlock(
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
	maxChainStartTime := preferredState.GetTimestamp().Add(executor.MaxFutureStartTime)
	if startTime.After(maxChainStartTime) {
		b.Mempool.AddProposalTx(tx)

		advanceTimeTx, err := b.txBuilder.NewAdvanceTimeTx(b.txExecutorBackend.Clk.Time())
		if err != nil {
			return nil, err
		}
		statelessBlk, err := stateless.NewProposalBlock(preferredID, nextHeight, advanceTimeTx)
		if err != nil {
			return nil, err
		}
		return b.blkManager.NewBlock(statelessBlk), nil
	}

	statelessBlk, err := stateless.NewProposalBlock(preferredID, nextHeight, tx)
	if err != nil {
		return nil, err
	}
	return b.blkManager.NewBlock(statelessBlk), nil
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
func (b *blockBuilder) resetTimer() {
	// If there is a pending transaction trigger building of a block with that transaction
	if b.Mempool.HasDecisionTxs() {
		b.notifyBlockReady()
		return
	}

	var (
		ctx           = b.txExecutorBackend.Ctx
		stateVersions = b.txExecutorBackend.StateVersions
	)

	preferredState, ok := stateVersions.GetState(b.preferredBlockID)
	if !ok {
		ctx.Log.Error("could not retrieve state for block %s. Preferred block must be a decision block", b.preferredBlockID)
		return
	}

	_, shouldReward, err := b.getNextStakerToReward(preferredState)
	if err != nil {
		ctx.Log.Error("failed to fetch next staker to reward with %s", err)
		return
	}
	if shouldReward {
		b.notifyBlockReady()
		return
	}

	_, shouldAdvanceTime, err := b.getNextChainTime(preferredState)
	if err != nil {
		ctx.Log.Error("failed to fetch next chain time with %s", err)
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
	nextStakerChangeTime, err := executor.GetNextStakerChangeTime(preferredState)
	if err != nil {
		ctx.Log.Error("couldn't get next staker change time: %s", err)
		return
	}
	waitTime := nextStakerChangeTime.Sub(now)
	ctx.Log.Debug("next scheduled event is at %s (%s in the future)", nextStakerChangeTime, waitTime)

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
	nextStakerChangeTime, err := executor.GetNextStakerChangeTime(preferredState)
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
func (b *blockBuilder) dropTooEarlyMempoolProposalTxs() bool {
	ctx := b.txExecutorBackend.Ctx
	now := b.txExecutorBackend.Clk.Time()
	syncTime := now.Add(executor.SyncBound)
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
		ctx.Log.Debug("dropping tx %s: %s", txID, errMsg)
	}
	return false
}

// notifyBlockReady tells the consensus engine that a new block is ready to be
// created
func (b *blockBuilder) notifyBlockReady() {
	select {
	case b.toEngine <- common.PendingTxs:
	default:
		b.txExecutorBackend.Ctx.Log.Debug("dropping message to consensus engine")
	}
}
