// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

const (
	// TargetTxSize is the maximum number of bytes a transaction can use to be
	// allowed into the mempool.
	TargetTxSize = 64 * units.KiB

	// TargetBlockSize is maximum number of transaction bytes to place into a
	// StandardBlock
	TargetBlockSize = 128 * units.KiB
)

var (
	errEndOfTime         = errors.New("program time is suspiciously far in the future")
	errNoPendingBlocks   = errors.New("no pending blocks")
	errMempoolReentrancy = errors.New("mempool reentrancy")
	errTxTooBig          = errors.New("tx too big")
)

// blockBuilder implements a simple blockBuilder to convert txs into valid blocks
type blockBuilder struct {
	Mempool

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
func (m *blockBuilder) Initialize(
	vm *VM,
	toEngine chan<- common.Message,
	registerer prometheus.Registerer,
) error {
	m.vm = vm
	m.toEngine = toEngine

	m.vm.ctx.Log.Verbo("initializing platformVM mempool")
	mempool, err := NewMempool("mempool", registerer)
	if err != nil {
		return err
	}
	m.Mempool = mempool

	m.timer = timer.NewTimer(func() {
		m.vm.ctx.Lock.Lock()
		defer m.vm.ctx.Lock.Unlock()

		m.ResetTimer()
	})
	go m.vm.ctx.Log.RecoverAndPanic(m.timer.Dispatch)
	return nil
}

// AddUnverifiedTx verifies a transaction and attempts to add it to the mempool
func (m *blockBuilder) AddUnverifiedTx(tx *txs.Tx) error {
	txID := tx.ID()
	if m.Has(txID) {
		// If the transaction is already in the mempool - then it looks the same
		// as if it was successfully added
		return nil
	}

	verifier := executor.MempoolTxVerifier{
		Backend:  &m.vm.txExecutorBackend,
		ParentID: m.vm.preferred, // We want to build off of the preferred block
		Tx:       tx,
	}
	if err := tx.Unsigned.Visit(&verifier); err != nil {
		m.MarkDropped(txID, err.Error())
		return err
	}

	if err := m.AddVerifiedTx(tx); err != nil {
		return err
	}
	return m.vm.GossipTx(tx)
}

// AddVerifiedTx attempts to add a transaction to the mempool
func (m *blockBuilder) AddVerifiedTx(tx *txs.Tx) error {
	if m.dropIncoming {
		return errMempoolReentrancy
	}

	txBytes := tx.Bytes()
	if len(txBytes) > TargetTxSize {
		return errTxTooBig
	}

	if err := m.Add(tx); err != nil {
		return err
	}
	m.ResetTimer()
	return nil
}

// BuildBlock builds a block to be added to consensus
func (m *blockBuilder) BuildBlock() (snowman.Block, error) {
	m.dropIncoming = true
	defer func() {
		m.dropIncoming = false
		m.ResetTimer()
	}()

	m.vm.ctx.Log.Debug("starting to attempt to build a block")

	// Get the block to build on top of and retrieve the new block's context.
	preferred, err := m.vm.Preferred()
	if err != nil {
		return nil, fmt.Errorf("couldn't get preferred block: %w", err)
	}
	preferredID := preferred.ID()
	nextHeight := preferred.Height() + 1

	preferredState, ok := m.vm.stateVersions.GetState(preferredID)
	if !ok {
		// The preferred block should always be a decision block
		return nil, errInvalidBlockType
	}

	// Try building a standard block.
	if m.HasDecisionTxs() {
		txs := m.PopDecisionTxs(TargetBlockSize)
		return m.vm.newStandardBlock(preferredID, nextHeight, txs)
	}

	// Try building a proposal block that rewards a staker.
	stakerTxID, shouldReward, err := m.getNextStakerToReward(preferredState)
	if err != nil {
		return nil, err
	}
	if shouldReward {
		rewardValidatorTx, err := m.vm.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return nil, err
		}
		return m.vm.newProposalBlock(preferredID, nextHeight, rewardValidatorTx)
	}

	// Try building a proposal block that advances the chain timestamp.
	nextChainTime, shouldAdvanceTime, err := m.getNextChainTime(preferredState)
	if err != nil {
		return nil, err
	}
	if shouldAdvanceTime {
		advanceTimeTx, err := m.vm.txBuilder.NewAdvanceTimeTx(nextChainTime)
		if err != nil {
			return nil, err
		}
		return m.vm.newProposalBlock(preferredID, nextHeight, advanceTimeTx)
	}

	// Clean out the mempool's transactions with invalid timestamps.
	if hasProposalTxs := m.dropTooEarlyMempoolProposalTxs(); !hasProposalTxs {
		m.vm.ctx.Log.Debug("no pending blocks to build")
		return nil, errNoPendingBlocks
	}

	// Get the proposal transaction that should be issued.
	tx := m.PopProposalTx()
	startTime := tx.Unsigned.(txs.StakerTx).StartTime()

	// If the chain timestamp is too far in the past to issue this transaction
	// but according to local time, it's ready to be issued, then attempt to
	// advance the timestamp, so it can be issued.
	maxChainStartTime := preferredState.GetTimestamp().Add(executor.MaxFutureStartTime)
	if startTime.After(maxChainStartTime) {
		m.AddProposalTx(tx)

		advanceTimeTx, err := m.vm.txBuilder.NewAdvanceTimeTx(m.vm.clock.Time())
		if err != nil {
			return nil, err
		}
		return m.vm.newProposalBlock(preferredID, nextHeight, advanceTimeTx)
	}

	return m.vm.newProposalBlock(preferredID, nextHeight, tx)
}

// ResetTimer Check if there is a block ready to be added to consensus. If so, notify the
// consensus engine.
func (m *blockBuilder) ResetTimer() {
	// If there is a pending transaction trigger building of a block with that transaction
	if m.HasDecisionTxs() {
		m.notifyBlockReady()
		return
	}

	preferredState, ok := m.vm.stateVersions.GetState(m.vm.preferred)
	if !ok {
		// The preferred block should always be a decision block
		m.vm.ctx.Log.Error("missing preferred block state",
			zap.Stringer("blkID", m.vm.preferred),
		)
		return
	}

	_, shouldReward, err := m.getNextStakerToReward(preferredState)
	if err != nil {
		m.vm.ctx.Log.Error("failed to fetch next staker to reward",
			zap.Error(err),
		)
		return
	}
	if shouldReward {
		m.notifyBlockReady()
		return
	}

	_, shouldAdvanceTime, err := m.getNextChainTime(preferredState)
	if err != nil {
		m.vm.ctx.Log.Error("failed to fetch next chain time",
			zap.Error(err),
		)
		return
	}
	if shouldAdvanceTime {
		// time is at or after the time for the next validator to join/leave
		m.notifyBlockReady() // Should issue a proposal to advance timestamp
		return
	}

	if hasProposalTxs := m.dropTooEarlyMempoolProposalTxs(); hasProposalTxs {
		m.notifyBlockReady() // Should issue a ProposeAddValidator
		return
	}

	now := m.vm.clock.Time()
	nextStakerChangeTime, err := executor.GetNextStakerChangeTime(preferredState)
	if err != nil {
		m.vm.ctx.Log.Error("couldn't get next staker change time",
			zap.Error(err),
		)
		return
	}
	waitTime := nextStakerChangeTime.Sub(now)
	m.vm.ctx.Log.Debug("setting next scheduled event",
		zap.Time("nextEventTime", nextStakerChangeTime),
		zap.Duration("timeUntil", waitTime),
	)

	// Wake up when it's time to add/remove the next validator
	m.timer.SetTimeoutIn(waitTime)
}

// Shutdown this mempool
func (m *blockBuilder) Shutdown() {
	if m.timer == nil {
		return
	}

	// There is a potential deadlock if the timer is about to execute a timeout.
	// So, the lock must be released before stopping the timer.
	m.vm.ctx.Lock.Unlock()
	m.timer.Stop()
	m.vm.ctx.Lock.Lock()
}

// getNextStakerToReward returns the next staker txID to remove from the staking
// set with a RewardValidatorTx rather than an AdvanceTimeTx.
// Returns:
// - [txID] of the next staker to reward
// - [shouldReward] if the txID exists and is ready to be rewarded
// - [err] if something bad happened
func (m *blockBuilder) getNextStakerToReward(preferredState state.Chain) (ids.ID, bool, error) {
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
func (m *blockBuilder) getNextChainTime(preferredState state.Chain) (time.Time, bool, error) {
	nextStakerChangeTime, err := executor.GetNextStakerChangeTime(preferredState)
	if err != nil {
		return time.Time{}, false, err
	}

	now := m.vm.clock.Time()
	return nextStakerChangeTime, !now.Before(nextStakerChangeTime), nil
}

// dropTooEarlyMempoolProposalTxs drops mempool's validators whose start time is
// too close in the future i.e. within local time plus Delta.
// dropTooEarlyMempoolProposalTxs makes sure that mempool's top proposal tx has
// a valid starting time but does not necessarily remove all txs since
// popped txs are not necessarily ordered by start time.
// Returns true/false if mempool is non-empty/empty following cleanup.
func (m *blockBuilder) dropTooEarlyMempoolProposalTxs() bool {
	now := m.vm.clock.Time()
	syncTime := now.Add(executor.SyncBound)
	for m.HasProposalTx() {
		tx := m.PopProposalTx()
		startTime := tx.Unsigned.(txs.StakerTx).StartTime()
		if !startTime.Before(syncTime) {
			m.AddProposalTx(tx)
			return true
		}

		txID := tx.ID()
		errMsg := fmt.Sprintf(
			"synchrony bound (%s) is later than staker start time (%s)",
			syncTime,
			startTime,
		)

		m.vm.blockBuilder.MarkDropped(txID, errMsg) // cache tx as dropped
		m.vm.ctx.Log.Debug("dropping tx",
			zap.String("reason", errMsg),
			zap.Stringer("txID", txID),
		)
	}
	return false
}

// notifyBlockReady tells the consensus engine that a new block is ready to be
// created
func (m *blockBuilder) notifyBlockReady() {
	select {
	case m.toEngine <- common.PendingTxs:
	default:
		m.vm.ctx.Log.Debug("dropping message to consensus engine")
	}
}
