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

	// Get the preferred block (which we want to build off)
	preferred, err := b.Preferred()
	if err != nil {
		return fmt.Errorf("couldn't get preferred block: %w", err)
	}

	preferredState := b.blkManager.OnAccept(preferred.ID())

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
	ctx := b.txExecutorBackend.Ctx
	b.Mempool.DisableAdding()
	defer func() {
		b.Mempool.EnableAdding()
		b.resetTimer()
	}()

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
func (b *blockBuilder) resetTimer() {
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
	ctx := b.txExecutorBackend.Ctx
	now := b.txExecutorBackend.Clk.Time()
	preferred, err := b.Preferred()
	if err != nil {
		return
	}
	preferredState := b.blkManager.OnAccept(preferred.ID())
	nextStakerChangeTime, err := preferredState.GetNextStakerChangeTime()
	if err != nil {
		ctx.Log.Error("couldn't get next staker change time: %s", err)
		return
	}
	waitTime := nextStakerChangeTime.Sub(now)
	ctx.Log.Debug("next scheduled event is at %s (%s in the future)", nextStakerChangeTime, waitTime)
	b.timer.SetTimeoutIn(waitTime)
}

// getStakerToReward return the staker txID to remove from the primary network
// staking set, if one exists.
func (b *blockBuilder) getStakerToReward(preferredState state.Chain) (*txs.Tx, bool, error) {
	currentChainTimestamp := preferredState.GetTimestamp()
	if !currentChainTimestamp.Before(mockable.MaxTime) {
		return nil, false, errEndOfTime
	}

	currentStakers := preferredState.CurrentStakers()
	tx, _, err := currentStakers.GetNextStaker()
	if err != nil {
		return nil, false, err
	}

	staker, ok := tx.Unsigned.(txs.StakerTx)
	if !ok {
		return nil, false, fmt.Errorf("expected staker tx to be StakerTx but got %T", tx.Unsigned)
	}
	return tx, currentChainTimestamp.Equal(staker.EndTime()), nil
}

// getNextChainTime returns the timestamp for the next chain time and if the
// local time is >= time of the next staker set change.
func (b *blockBuilder) getNextChainTime(preferredState state.Chain) (time.Time, bool, error) {
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
func (b *blockBuilder) dropTooEarlyMempoolProposalTxs() {
	ctx := b.txExecutorBackend.Ctx
	now := b.txExecutorBackend.Clk.Time()
	syncTime := now.Add(executor.SyncBound)
	for b.Mempool.HasProposalTx() {
		tx := b.Mempool.PeekProposalTx()
		startTime := tx.Unsigned.(txs.StakerTx).StartTime()
		if !startTime.Before(syncTime) {
			return
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
