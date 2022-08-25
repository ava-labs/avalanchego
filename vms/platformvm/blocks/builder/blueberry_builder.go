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
	// clean out the mempool's transactions with invalid timestamps.
	builder.dropExpiredStakerTxs(timestamp)

	// try including as many mempool txs as possible
	// Note that, upon Blueberry activation, all mempool transactions
	// will be included into a standard block
	if builder.Mempool.HasTxs() {
		return blocks.NewBlueberryStandardBlock(
			timestamp,
			parentID,
			height,
			builder.Mempool.PeekTxs(targetBlockSize),
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
