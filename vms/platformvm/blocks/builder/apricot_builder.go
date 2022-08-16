// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"

	transactions "github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ forkBuilder = &apricotBuilder{}

// Build blocks for the Apricot network fork.
type apricotBuilder struct {
	*builder

	// inputs
	// All must be set before [build] is called.
	parentBlkID ids.ID
	parentState state.Chain
	// Build a block with this height.
	nextHeight uint64
}

// Returns the transactions we should include in the block.
// Only updates the mempool to remove expired proposal txs.
// It's up to the caller to cleanup the mempool if necessary.
func (a *apricotBuilder) selectTxs() ([]*transactions.Tx, error) {
	// try including as many standard txs as possible. No need to advance chain time
	if a.Mempool.HasDecisionTxs() {
		return a.Mempool.PeekDecisionTxs(targetBlockSize), nil
	}

	// try rewarding stakers whose staking period ends at current chain time.
	stakerTxID, shouldReward, err := a.builder.getNextStakerToReward(a.parentState)
	if err != nil {
		return nil, fmt.Errorf("could not find next staker to reward: %w", err)
	}
	if shouldReward {
		rewardValidatorTx, err := a.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return nil, fmt.Errorf("could not build tx to reward staker: %w", err)
		}

		return []*transactions.Tx{rewardValidatorTx}, nil
	}

	// try advancing chain time
	nextChainTime, shouldAdvanceTime, err := a.builder.getNextChainTime(a.parentState)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve next chain time: %w", err)
	}
	if shouldAdvanceTime {
		advanceTimeTx, err := a.txBuilder.NewAdvanceTimeTx(nextChainTime)
		if err != nil {
			return nil, fmt.Errorf("could not build tx to reward staker: %w", err)
		}
		return []*transactions.Tx{advanceTimeTx}, nil
	}

	tx, err := a.nextProposalTx()
	if err != nil {
		return nil, err
	}
	return []*transactions.Tx{tx}, nil
}

// Try to get/make a proposal tx to put into a block.
// Returns an error if there's no suitable proposal tx.
func (a *apricotBuilder) nextProposalTx() (*transactions.Tx, error) {
	// clean out transactions with an invalid timestamp.
	a.dropExpiredProposalTxs()

	// Check the mempool
	if !a.Mempool.HasProposalTx() {
		a.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return nil, errNoPendingBlocks
	}
	tx := a.Mempool.PeekProposalTx()
	startTime := tx.Unsigned.(transactions.StakerTx).StartTime()

	// Check whether this staker starts within at most [MaxFutureStartTime].
	// If it does, issue the staking tx.
	// If it doesn't, issue an advance time tx.
	maxChainStartTime := a.parentState.GetTimestamp().Add(executor.MaxFutureStartTime)
	if !startTime.After(maxChainStartTime) {
		return tx, nil
	}

	// The chain timestamp is too far in the past. Advance it.
	now := a.txExecutorBackend.Clk.Time()
	advanceTimeTx, err := a.txBuilder.NewAdvanceTimeTx(now)
	if err != nil {
		return nil, fmt.Errorf("could not build tx to advance time: %w", err)
	}
	return advanceTimeTx, nil
}

func (a *apricotBuilder) buildBlock() (snowman.Block, []*transactions.Tx, error) {
	txs, err := a.selectTxs()
	if err != nil {
		return nil, nil, err
	}

	var (
		tx           = txs[0]
		statelessBlk blocks.Block
	)
	switch tx.Unsigned.(type) {
	case transactions.StakerTx,
		*transactions.RewardValidatorTx,
		*transactions.AdvanceTimeTx:
		// Note that if [tx] is one of the above types, it's the only
		// tx in this block because Apricot proposal blocks have 1 tx.
		statelessBlk, err = blocks.NewApricotProposalBlock(
			a.parentBlkID,
			a.nextHeight,
			tx,
		)

	case *transactions.CreateChainTx,
		*transactions.CreateSubnetTx,
		*transactions.ImportTx,
		*transactions.ExportTx:
		// Note that if [tx] is one of the above types, all of
		// the txs in [a.txs] must be "standard" transactions.
		statelessBlk, err = blocks.NewApricotStandardBlock(
			a.parentBlkID,
			a.nextHeight,
			txs,
		)

	default:
		return nil, nil, fmt.Errorf("unhandled tx type %T, could not include into a block", tx.Unsigned)
	}

	if err != nil {
		return nil, nil, err
	}
	return a.blkManager.NewBlock(statelessBlk), txs, nil
}
