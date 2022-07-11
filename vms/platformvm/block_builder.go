// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
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

	// Transactions that have not been put into blocks yet
	dropIncoming bool
}

// Initialize this builder.
func (b *blockBuilder) Initialize(
	mempool mempool.Mempool,
	vm *VM,
	toEngine chan<- common.Message,
	registerer prometheus.Registerer,
) error {
	b.vm = vm
	b.toEngine = toEngine
	b.Mempool = mempool

	b.timer = timer.NewTimer(func() {
		b.vm.ctx.Lock.Lock()
		defer b.vm.ctx.Lock.Unlock()

		b.resetTimer()
	})
	go b.vm.ctx.Log.RecoverAndPanic(b.timer.Dispatch)
	return nil
}

func (b *blockBuilder) ResetBlockTimer() { b.resetTimer() }

// AddUnverifiedTx verifies a transaction and attempts to add it to the mempool
func (b *blockBuilder) AddUnverifiedTx(tx *txs.Tx) error {
	txID := tx.ID()
	if b.Has(txID) {
		// If the transaction is already in the mempool - then it looks the same
		// as if it was successfully added
		return nil
	}

	preferred, err := b.vm.Preferred()
	if err != nil {
		return fmt.Errorf("couldn't get preferred block: %w", err)
	}

	/*
		preferredDecision, ok := preferred.(stateful.Decision)
		if !ok {
			// The preferred block should always be a decision block
			return fmt.Errorf("expected Decision block but got %T", preferred)
		}
		preferredState := preferredDecision.OnAccept()
	*/
	preferredState := b.vm.manager.OnAccept(preferred.ID())

	verifier := executor.MempoolTxVerifier{
		Backend:     &b.vm.txExecutorBackend,
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
	return b.vm.GossipTx(tx)
}

// BuildBlock builds a block to be added to consensus
func (b *blockBuilder) BuildBlock() (snowman.Block, error) {
	b.dropIncoming = true
	defer func() {
		b.dropIncoming = false
		b.resetTimer()
	}()

	b.vm.ctx.Log.Debug("starting to attempt to build a block")

	// Get the block to build on top of and retrieve the new block's context.
	preferred, err := b.vm.Preferred()
	if err != nil {
		return nil, fmt.Errorf("couldn't get preferred block: %w", err)
	}
	preferredID := preferred.ID()
	nextHeight := preferred.Height() + 1

	/* TODO remove
	preferredDecision, ok := preferred.(stateful.Decision)
	if !ok {
		// The preferred block should always be a decision block
		return nil, fmt.Errorf("expected Decision block but got %T", preferred)
	}
	preferredState := preferredDecision.OnAccept()
	*/
	preferredState := b.vm.manager.OnAccept(preferredID)

	// Try building a standard block.
	if b.HasDecisionTxs() {
		// return stateful.NewStandardBlock(
		// 	b.vm.manager,
		// 	b.vm.ctx,
		// 	preferredID,
		// 	nextHeight,
		// 	txs,
		// )
		txs := b.PopDecisionTxs(TargetBlockSize)
		statelessBlk, err := stateless.NewStandardBlock(preferredID, nextHeight, txs)
		if err != nil {
			return nil, err
		}
		return b.vm.manager.NewBlock(statelessBlk), nil
	}

	// Try building a proposal block that rewards a staker.
	stakerTxID, shouldReward, err := b.getStakerToReward(preferredState)
	if err != nil {
		return nil, err
	}
	if shouldReward {
		rewardValidatorTx, err := b.vm.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return nil, err
		}
		// return stateful.NewProposalBlock(
		// 	b.vm.manager,
		// 	b.vm.ctx,
		// 	preferredID,
		// 	nextHeight,
		// 	rewardValidatorTx,
		// )
		statelessBlk, err := stateless.NewProposalBlock(preferredID, nextHeight, rewardValidatorTx)
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
		// return stateful.NewProposalBlock(
		// 	b.vm.manager,
		// 	b.vm.ctx,
		// 	preferredID,
		// 	nextHeight,
		// 	advanceTimeTx,
		// )
		statelessBlk, err := stateless.NewProposalBlock(preferredID, nextHeight, advanceTimeTx)
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
		// return stateful.NewProposalBlock(
		// 	b.vm.manager,
		// 	b.vm.ctx,
		// 	preferredID,
		// 	nextHeight,
		// 	advanceTimeTx,
		// )
		statelessBlk, err := stateless.NewProposalBlock(preferredID, nextHeight, advanceTimeTx)
		if err != nil {
			return nil, err
		}
		return b.vm.manager.NewBlock(statelessBlk), nil
	}

	// return stateful.NewProposalBlock(
	// 	b.vm.manager,
	// 	b.vm.ctx,
	// 	preferredID,
	// 	nextHeight,
	// 	tx,
	// )
	statelessBlk, err := stateless.NewProposalBlock(preferredID, nextHeight, tx)
	if err != nil {
		return nil, err
	}
	return b.vm.manager.NewBlock(statelessBlk), nil
}

// ResetTimer Check if there is a block ready to be added to consensus. If so, notify the
// consensus engine.
func (b *blockBuilder) resetTimer() {
	// If there is a pending transaction trigger building of a block with that transaction
	if b.HasDecisionTxs() {
		b.notifyBlockReady()
		return
	}

	preferred, err := b.vm.Preferred()
	if err != nil {
		return
	}

	/* TODO remove
	preferredDecision, ok := preferred.(stateful.Decision)
	if !ok {
		// The preferred block should always be a decision block
		b.vm.ctx.Log.Error("the preferred block %q should be a decision block but was %T", preferred.ID(), preferred)
		return
	}
	preferredState := preferredDecision.OnAccept()
	*/
	preferredState := b.vm.manager.OnAccept(preferred.ID())

	_, shouldReward, err := b.getStakerToReward(preferredState)
	if err != nil {
		b.vm.ctx.Log.Error("failed to fetch next staker to reward with %s", err)
		return
	}
	if shouldReward {
		b.notifyBlockReady()
		return
	}

	_, shouldAdvanceTime, err := b.getNextChainTime(preferredState)
	if err != nil {
		b.vm.ctx.Log.Error("failed to fetch next chain time with %s", err)
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
	nextStakerChangeTime, err := preferredState.GetNextStakerChangeTime()
	if err != nil {
		b.vm.ctx.Log.Error("couldn't get next staker change time: %s", err)
		return
	}
	waitTime := nextStakerChangeTime.Sub(now)
	b.vm.ctx.Log.Debug("next scheduled event is at %s (%s in the future)", nextStakerChangeTime, waitTime)

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

// getStakerToReward return the staker txID to remove from the primary network
// staking set, if one exists.
func (b *blockBuilder) getStakerToReward(preferredState state.Chain) (ids.ID, bool, error) {
	currentChainTimestamp := preferredState.GetTimestamp()
	if !currentChainTimestamp.Before(mockable.MaxTime) {
		return ids.Empty, false, errEndOfTime
	}

	currentStakers := preferredState.CurrentStakers()
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
func (b *blockBuilder) getNextChainTime(preferredState state.Chain) (time.Time, bool, error) {
	nextStakerChangeTime, err := preferredState.GetNextStakerChangeTime()
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
		b.vm.ctx.Log.Debug("dropping tx %s: %s", txID, errMsg)
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
