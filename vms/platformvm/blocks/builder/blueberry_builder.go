// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func buildBlueberryBlock(
	builder *builder,
	parentID ids.ID,
	height uint64,
	parentState state.Chain,
) (blocks.Block, error) {
	// timestamp is zeroed for Apricot blocks
	// it is set to the proposed chainTime for Blueberry ones
	parentTimestamp := parentState.GetTimestamp()

	// try including as many standard txs as possible. No need to advance chain time
	if builder.Mempool.HasDecisionTxs() {
		return blocks.NewBlueberryStandardBlock(
			parentTimestamp,
			parentID,
			height,
			builder.Mempool.PeekDecisionTxs(targetBlockSize),
		)
	}

	// try rewarding stakers whose staking period ends at current chain time.
	stakerTxID, shouldReward, err := builder.getNextStakerToReward(parentState)
	if err != nil {
		return nil, fmt.Errorf("could not find next staker to reward: %w", err)
	}
	if shouldReward {
		rewardValidatorTx, err := builder.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return nil, fmt.Errorf("could not build tx to reward staker: %w", err)
		}

		return blocks.NewBlueberryProposalBlock(
			parentTimestamp,
			parentID,
			height,
			rewardValidatorTx,
		)
	}

	// try advancing chain time. It may result in empty blocks
	nextChainTime, shouldAdvanceTime, err := builder.getNextChainTime(parentState)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve next chain time: %w", err)
	}
	if shouldAdvanceTime {
		// empty standard block are allowed to move chain time head
		return blocks.NewBlueberryStandardBlock(
			nextChainTime,
			parentID,
			height,
			nil,
		)
	}

	// clean out the mempool's transactions with invalid timestamps.
	builder.dropExpiredProposalTxs()

	// try including a mempool proposal tx is available.
	if !builder.Mempool.HasProposalTx() {
		builder.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return nil, errNoPendingBlocks
	}

	tx := builder.Mempool.PeekProposalTx()

	// if the chain timestamp is too far in the past to issue this transaction
	// but according to local time, it's ready to be issued, then attempt to
	// advance the timestamp, so it can be issued.
	startTime := tx.Unsigned.(txs.StakerTx).StartTime()
	maxChainStartTime := parentTimestamp.Add(executor.MaxFutureStartTime)

	newTimestamp := parentTimestamp
	if startTime.After(maxChainStartTime) {
		// setting blkTime to now to propose moving chain time ahead
		newTimestamp = builder.txExecutorBackend.Clk.Time()
	}

	return blocks.NewBlueberryProposalBlock(
		newTimestamp,
		parentID,
		height,
		tx,
	)
}
