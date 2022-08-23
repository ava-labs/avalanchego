// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

// [timestamp] is min(max(now, parent timestamp), next staker change time)
func buildBlueberryBlock(
	builder *builder,
	parentID ids.ID,
	height uint64,
	timestamp time.Time,
	forceAdvanceTime bool,
	parentState state.Chain,
) (blocks.Block, error) {
	// try including as many standard txs as possible
	if builder.Mempool.HasDecisionTxs() {
		return blocks.NewBlueberryStandardBlock(
			timestamp,
			parentID,
			height,
			builder.Mempool.PeekDecisionTxs(targetBlockSize),
		)
	}

	// try rewarding stakers whose staking period ends at the new chain time.
	stakerTxID, shouldReward, err := builder.getNextStakerToReward(timestamp, parentState)
	if err != nil {
		return nil, fmt.Errorf("could not find next staker to reward: %w", err)
	}
	if shouldReward {
		rewardValidatorTx, err := builder.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return nil, fmt.Errorf("could not build tx to reward staker: %w", err)
		}

		return blocks.NewBlueberryProposalBlock(
			timestamp,
			parentID,
			height,
			rewardValidatorTx,
		)
	}

	// clean out the mempool's transactions with invalid timestamps.
	builder.dropExpiredProposalTxs(timestamp)

	// try including a mempool proposal tx.
	if builder.Mempool.HasProposalTx() {
		tx := builder.Mempool.PeekProposalTx()
		return blocks.NewBlueberryProposalBlock(
			timestamp,
			parentID,
			height,
			tx,
		)
	}

	if forceAdvanceTime {
		// We should issue an empty block to advance the chain time because the
		// staker set should change.
		return blocks.NewBlueberryStandardBlock(
			timestamp,
			parentID,
			height,
			nil,
		)
	}

	builder.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
	return nil, errNoPendingBlocks
}
