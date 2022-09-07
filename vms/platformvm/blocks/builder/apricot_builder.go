// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func buildApricotBlock(
	builder *builder,
	parentID ids.ID,
	height uint64,
	timestamp time.Time,
	shouldAdvanceTime bool,
	parentState state.Chain,
) (blocks.Block, error) {
	// try including as many standard txs as possible. No need to advance chain time
	if builder.Mempool.HasApricotDecisionTxs() {
		return blocks.NewApricotStandardBlock(
			parentID,
			height,
			builder.Mempool.PeekApricotDecisionTxs(targetBlockSize),
		)
	}

	// try rewarding stakers whose staking period ends at current chain time.
	stakerTxID, shouldReward, err := builder.getNextStakerToReward(parentState.GetTimestamp(), parentState)
	if err != nil {
		return nil, fmt.Errorf("could not find next staker to reward: %w", err)
	}
	if shouldReward {
		rewardValidatorTx, err := builder.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return nil, fmt.Errorf("could not build tx to reward staker: %w", err)
		}

		return blocks.NewApricotProposalBlock(
			parentID,
			height,
			rewardValidatorTx,
		)
	}

	// try advancing chain time
	if shouldAdvanceTime {
		advanceTimeTx, err := builder.txBuilder.NewAdvanceTimeTx(timestamp)
		if err != nil {
			return nil, fmt.Errorf("could not build tx to reward staker: %w", err)
		}

		return blocks.NewApricotProposalBlock(
			parentID,
			height,
			advanceTimeTx,
		)
	}

	// Clean out transactions with an invalid timestamp.
	builder.dropExpiredStakerTxs(timestamp)

	// Check the mempool
	if !builder.Mempool.HasStakerTx() {
		builder.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return nil, errNoPendingBlocks
	}

	tx, err := nextApricotProposalTx(builder, timestamp, parentState)
	if err != nil {
		builder.txExecutorBackend.Ctx.Log.Error(
			"failed to get the next proposal tx",
			zap.Error(err),
		)
		return nil, err
	}

	return blocks.NewApricotProposalBlock(
		parentID,
		height,
		tx,
	)
}

// Try to get/make a proposal tx to put into a block.
// Any returned error is unexpected.
func nextApricotProposalTx(builder *builder, timestamp time.Time, parentState state.Chain) (*txs.Tx, error) {
	tx := builder.Mempool.PeekStakerTx()
	startTime := tx.Unsigned.(txs.Staker).StartTime()

	// Check whether this staker starts within at most [MaxFutureStartTime].
	// If it does, issue the staking tx.
	// If it doesn't, issue an advance time tx.
	maxChainStartTime := parentState.GetTimestamp().Add(executor.MaxFutureStartTime)
	if !startTime.After(maxChainStartTime) {
		return tx, nil
	}

	// The chain timestamp is too far in the past. Advance it.
	advanceTimeTx, err := builder.txBuilder.NewAdvanceTimeTx(timestamp)
	if err != nil {
		return nil, fmt.Errorf("could not build tx to advance time: %w", err)
	}
	return advanceTimeTx, nil
}
