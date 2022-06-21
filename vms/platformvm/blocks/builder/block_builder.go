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
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"

	p_block "github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful"
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
	Preferred() (p_block.Block, error)
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
	blkVerifier       p_block.Verifier

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
	blkVerifier p_block.Verifier,
	toEngine chan<- common.Message,
	appSender common.AppSender,
) BlockBuilder {
	blkBuilder := &blockBuilder{
		Mempool:           mempool,
		txBuilder:         txBuilder,
		txExecutorBackend: txExecutorBackend,
		blkVerifier:       blkVerifier,
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
		txExecutorBackend.Cfg.ApricotPhase4Time,
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

func (b *blockBuilder) Preferred() (p_block.Block, error) {
	return b.blkVerifier.GetStatefulBlock(b.preferredBlockID)
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

	// Get the preferred block (which we want to build off)
	preferred, err := b.Preferred()
	if err != nil {
		return fmt.Errorf("couldn't get preferred block: %w", err)
	}

	preferredDecision, ok := preferred.(p_block.Decision)
	if !ok {
		// The preferred block should always be a decision block
		return fmt.Errorf("expected Decision block but got %T", preferred)
	}
	preferredState := preferredDecision.OnAccept()
	verifier := executor.MempoolTxVerifier{
		Backend:     &b.txExecutorBackend,
		ParentState: preferredState,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&verifier)
	if err != nil {
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
		b.resetTimer()
	}()

	b.txExecutorBackend.Ctx.Log.Debug("starting to attempt to build a block")

	// Get the block to build on top of and retrieve the new block's context.
	preferred, err := b.Preferred()
	if err != nil {
		return nil, err
	}
	preferredID := preferred.ID()
	nextHeight := preferred.Height() + 1
	preferredDecision, ok := preferred.(p_block.Decision)
	if !ok {
		// The preferred block should always be a decision block
		return nil, fmt.Errorf("expected Decision block but got %T", preferred)
	}
	preferredState := preferredDecision.OnAccept()

	// Try building a standard block.
	if b.Mempool.HasDecisionTxs() {
		txs := b.Mempool.PopDecisionTxs(TargetBlockSize)
		return p_block.NewStandardBlock(
			b.blkVerifier,
			b.txExecutorBackend,
			preferredID,
			nextHeight,
			txs,
		)
	}

	// Try building a proposal block that rewards a staker.
	stakerTxID, shouldReward, err := b.getStakerToReward(preferredState)
	if err != nil {
		return nil, err
	}
	if shouldReward {
		rewardValidatorTx, err := b.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return nil, err
		}
		return p_block.NewProposalBlock(
			b.blkVerifier,
			b.txExecutorBackend,
			preferredID,
			nextHeight,
			rewardValidatorTx,
		)
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
		return p_block.NewProposalBlock(
			b.blkVerifier,
			b.txExecutorBackend,
			preferredID,
			nextHeight,
			advanceTimeTx,
		)
	}

	// Clean out the mempool's transactions with invalid timestamps.
	if hasProposalTxs := b.dropTooEarlyMempoolProposalTxs(); !hasProposalTxs {
		b.txExecutorBackend.Ctx.Log.Debug("no pending blocks to build")
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
		return p_block.NewProposalBlock(
			b.blkVerifier,
			b.txExecutorBackend,
			preferredID,
			nextHeight,
			advanceTimeTx,
		)
	}

	return p_block.NewProposalBlock(
		b.blkVerifier,
		b.txExecutorBackend,
		preferredID,
		nextHeight,
		tx,
	)
}

func (b *blockBuilder) Shutdown() {
	if b.timer == nil {
		return
	}

	// There is a potential deadlock if the timer is about to execute a timeout.
	// So, the lock must be released before stopping the timer.
	b.txExecutorBackend.Ctx.Lock.Unlock()
	b.timer.Stop()
	b.txExecutorBackend.Ctx.Lock.Lock()
}

// resetTimer Check if there is a block ready to be added to consensus. If so, notify the
// consensus engine.
func (b *blockBuilder) resetTimer() {
	// If there is a pending transaction trigger building of a block with that transaction
	if b.Mempool.HasDecisionTxs() {
		b.notifyBlockReady()
		return
	}

	preferred, err := b.Preferred()
	if err != nil {
		return
	}
	preferredDecision, ok := preferred.(p_block.Decision)
	if !ok {
		// The preferred block should always be a decision block
		b.txExecutorBackend.Ctx.Log.Error("the preferred block %q should be a decision block but was %T", preferred.ID(), preferred)
		return
	}
	preferredState := preferredDecision.OnAccept()

	_, shouldReward, err := b.getStakerToReward(preferredState)
	if err != nil {
		b.txExecutorBackend.Ctx.Log.Error("failed to fetch next staker to reward with %s", err)
		return
	}
	if shouldReward {
		b.notifyBlockReady()
		return
	}

	_, shouldAdvanceTime, err := b.getNextChainTime(preferredState)
	if err != nil {
		b.txExecutorBackend.Ctx.Log.Error("failed to fetch next chain time with %s", err)
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
	nextStakerChangeTime, err := preferredState.GetNextStakerChangeTime()
	if err != nil {
		b.txExecutorBackend.Ctx.Log.Error("couldn't get next staker change time: %s", err)
		return
	}
	waitTime := nextStakerChangeTime.Sub(now)
	b.txExecutorBackend.Ctx.Log.Debug("next scheduled event is at %s (%s in the future)", nextStakerChangeTime, waitTime)

	// Wake up when it's time to add/remove the next validator
	b.timer.SetTimeoutIn(waitTime)
}

// getStakerToReward return the staker txID to remove from the primary network
// staking set, if one exists.
func (b *blockBuilder) getStakerToReward(preferredState state.Mutable) (ids.ID, bool, error) {
	currentChainTimestamp := preferredState.GetTimestamp()
	if !currentChainTimestamp.Before(mockable.MaxTime) {
		return ids.Empty, false, errEndOfTime
	}

	currentStakers := preferredState.CurrentStakerChainState()
	tx, _, err := currentStakers.GetNextStaker()
	if err != nil {
		return ids.Empty, false, err
	}

	staker, ok := tx.Unsigned.(txs.StakerTx)
	if !ok {
		return ids.Empty, false, fmt.Errorf("expected staker tx to be TimedTx but got %T", tx.Unsigned)
	}
	return tx.ID(), currentChainTimestamp.Equal(staker.EndTime()), nil
}

// getNextChainTime returns the timestamp for the next chain time and if the
// local time is >= time of the next staker set change.
func (b *blockBuilder) getNextChainTime(preferredState state.Mutable) (time.Time, bool, error) {
	nextStakerChangeTime, err := preferredState.GetNextStakerChangeTime()
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
		b.txExecutorBackend.Ctx.Log.Debug("dropping tx %s: %s", txID, errMsg)
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
