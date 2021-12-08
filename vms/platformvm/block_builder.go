// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

const (
	// syncBound is the synchrony bound used for safe decision making
	syncBound = 10 * time.Second

	// BatchSize is the number of decision transactions to place into a block
	BatchSize = 30
)

var (
	errEndOfTime         = errors.New("program time is suspiciously far in the future. Either this codebase was way more successful than expected, or a critical error has occurred")
	errNoPendingBlocks   = errors.New("no pending blocks")
	errMempoolReentrancy = errors.New("mempool reentrancy")
)

// blockBuilder implements a simple blockBuilder to convert txs into valid blocks
type blockBuilder struct {
	Mempool

	// TODO: factor out VM into separable interfaces
	vm *VM

	// This timer goes off when it is time for the next validator to add/leave
	// the validator set. When it goes off ResetTimer() is called, potentially
	// triggering creation of a new block.
	timer *timer.Timer

	// Transactions that have not been put into blocks yet
	dropIncoming bool
}

// Initialize this builder.
func (m *blockBuilder) Initialize(vm *VM, registerer prometheus.Registerer) error {
	m.vm = vm

	m.vm.ctx.Log.Verbo("initializing platformVM mempool")
	mempool, err := NewMempool(
		"mempool",
		registerer,
	)
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
func (m *blockBuilder) AddUnverifiedTx(tx *Tx) error {
	// Initialize the transaction
	if err := tx.Sign(Codec, nil); err != nil {
		return err
	}

	txID := tx.ID()
	if m.Has(txID) {
		// If the transaction is already in the mempool - then it looks the same
		// as if it was successfully added
		return nil
	}

	// Get the preferred block (which we want to build off)
	preferred, err := m.vm.Preferred()
	if err != nil {
		return fmt.Errorf("couldn't get preferred block: %w", err)
	}

	preferredDecision, ok := preferred.(decision)
	if !ok {
		// The preferred block should always be a decision block
		return errInvalidBlockType
	}

	preferredState := preferredDecision.onAccept()
	if err := tx.UnsignedTx.SemanticVerify(m.vm, preferredState, tx); err != nil {
		m.MarkDropped(txID)
		return err
	}

	if err := m.AddVerifiedTx(tx); err != nil {
		return err
	}
	return m.vm.GossipTx(tx)
}

// AddVerifiedTx attempts to add a transaction to the mempool
func (m *blockBuilder) AddVerifiedTx(tx *Tx) error {
	if m.dropIncoming {
		return errMempoolReentrancy
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

	m.vm.ctx.Log.Debug("in BuildBlock")

	// Get the preferred block (which we want to build off)
	preferred, err := m.vm.Preferred()
	if err != nil {
		return nil, fmt.Errorf("couldn't get preferred block: %w", err)
	}

	preferredDecision, ok := preferred.(decision)
	if !ok {
		// The preferred block should always be a decision block
		return nil, errInvalidBlockType
	}

	preferredID := preferred.ID()
	nextHeight := preferred.Height() + 1

	// The state if the preferred block were to be accepted
	preferredState := preferredDecision.onAccept()
	currentChainTimestamp := preferredState.GetTimestamp()
	if !currentChainTimestamp.Before(mockable.MaxTime) {
		return nil, errEndOfTime
	}

	// If there are pending decision txs, build a block with a batch of them
	if m.HasDecisionTxs() {
		txs := m.PopDecisionTxs(BatchSize)
		return m.vm.newStandardBlock(preferredID, nextHeight, txs)
	}

	currentStakers := preferredState.CurrentStakerChainState()

	// If the chain time would be the time for the next primary network staker
	// to leave, then we create a block that removes the staker and proposes
	// they receive a staker reward
	tx, _, err := currentStakers.GetNextStaker()
	if err != nil {
		return nil, err
	}
	staker, ok := tx.UnsignedTx.(TimedTx)
	if !ok {
		return nil, fmt.Errorf("expected staker tx to be TimedTx but got %T", tx.UnsignedTx)
	}
	nextValidatorEndtime := staker.EndTime()
	if currentChainTimestamp.Equal(nextValidatorEndtime) {
		rewardValidatorTx, err := m.vm.newRewardValidatorTx(tx.ID())
		if err != nil {
			return nil, err
		}
		return m.vm.newProposalBlock(preferredID, nextHeight, *rewardValidatorTx)
	}

	// If local time is >= time of the next staker set change,
	// propose moving the chain time forward
	nextStakerChangeTime, err := m.vm.nextStakerChangeTime(preferredState)
	if err != nil {
		return nil, err
	}

	localTime := m.vm.clock.Time()
	if !localTime.Before(nextStakerChangeTime) {
		// local time is at or after the time for the next staker to start/stop
		advanceTimeTx, err := m.vm.newAdvanceTimeTx(nextStakerChangeTime)
		if err != nil {
			return nil, err
		}
		return m.vm.newProposalBlock(preferredID, nextHeight, *advanceTimeTx)
	}

	// Propose adding a new validator but only if their start time is in the
	// future relative to local time (plus Delta)
	syncTime := localTime.Add(syncBound)
	for m.HasProposalTx() {
		tx := m.PopProposalTx()
		txID := tx.ID()
		utx := tx.UnsignedTx.(TimedTx)
		startTime := utx.StartTime()
		if startTime.Before(syncTime) {
			errMsg := fmt.Sprintf(
				"synchrony bound (%s) is later than staker start time (%s)",
				syncTime,
				startTime,
			)
			m.vm.droppedTxCache.Put(txID, errMsg) // cache tx as dropped
			m.vm.ctx.Log.Debug("dropping tx %s: %s", txID, errMsg)
			continue
		}

		maxLocalStartTime := localTime.Add(maxFutureStartTime)
		// If the start time is too far in the future relative to local time
		// drop the transaction and continue
		if startTime.After(maxLocalStartTime) {
			continue
		}

		// If the chain timestamp is too far in the past to issue this
		// transaction but according to local time, it's ready to be issued,
		// then attempt to advance the timestamp, so it can be issued.
		maxChainStartTime := currentChainTimestamp.Add(maxFutureStartTime)
		if startTime.After(maxChainStartTime) {
			m.AddProposalTx(tx)

			advanceTimeTx, err := m.vm.newAdvanceTimeTx(localTime)
			if err != nil {
				return nil, err
			}
			return m.vm.newProposalBlock(preferredID, nextHeight, *advanceTimeTx)
		}
		return m.vm.newProposalBlock(preferredID, nextHeight, *tx)
	}

	m.vm.ctx.Log.Debug("BuildBlock returning error (no blocks)")
	return nil, errNoPendingBlocks
}

// ResetTimer Check if there is a block ready to be added to consensus. If so, notify the
// consensus engine.
func (m *blockBuilder) ResetTimer() {
	// If there is a pending transaction trigger building of a block with that
	// transaction
	if m.HasDecisionTxs() {
		m.vm.NotifyBlockReady()
		return
	}

	// Get the preferred block (which we want to build off)
	preferred, err := m.vm.Preferred()
	if err != nil {
		m.vm.ctx.Log.Error("error fetching the preferred block: %s", err)
		return
	}

	preferredDecision, ok := preferred.(decision)
	if !ok {
		m.vm.ctx.Log.Error("the preferred block %q should be a decision block", preferred.ID())
		return
	}

	// The state if the preferred block were to be accepted
	preferredState := preferredDecision.onAccept()

	// The chain time if the preferred block were to be accepted
	timestamp := preferredState.GetTimestamp()
	if timestamp.Equal(mockable.MaxTime) {
		m.vm.ctx.Log.Error("program time is suspiciously far in the future. Either this codebase was way more successful than expected, or a critical error has occurred")
		return
	}

	// If local time is >= time of the next change in the validator set,
	// propose moving forward the chain timestamp
	nextStakerChangeTime, err := m.vm.nextStakerChangeTime(preferredState)
	if err != nil {
		m.vm.ctx.Log.Error("couldn't get next staker change time: %s", err)
		return
	}
	if timestamp.Equal(nextStakerChangeTime) {
		m.vm.NotifyBlockReady() // Should issue a proposal to reward a validator
		return
	}

	localTime := m.vm.clock.Time()
	if !localTime.Before(nextStakerChangeTime) { // time is at or after the time for the next validator to join/leave
		m.vm.NotifyBlockReady() // Should issue a proposal to advance timestamp
		return
	}

	syncTime := localTime.Add(syncBound)
	for m.HasProposalTx() {
		tx := m.PopProposalTx()
		startTime := tx.UnsignedTx.(TimedTx).StartTime()
		if !syncTime.After(startTime) {
			m.AddProposalTx(tx)
			m.vm.NotifyBlockReady() // Should issue a ProposeAddValidator
			return
		}
		// If the tx doesn't meet the synchrony bound, drop it
		txID := tx.ID()
		errMsg := fmt.Sprintf(
			"synchrony bound (%s) is later than staker start time (%s)",
			syncTime,
			startTime,
		)
		m.vm.droppedTxCache.Put( // cache tx as dropped
			txID,
			errMsg,
		)
		m.vm.ctx.Log.Debug("dropping tx %s: %s", txID, errMsg)
	}

	waitTime := nextStakerChangeTime.Sub(localTime)
	m.vm.ctx.Log.Debug("next scheduled event is at %s (%s in the future)", nextStakerChangeTime, waitTime)

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
