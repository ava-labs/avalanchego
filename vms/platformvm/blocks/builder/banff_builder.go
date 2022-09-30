// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
func buildBanffBlock(
	builder *builder,
	parentID ids.ID,
	height uint64,
	timestamp time.Time,
	forceAdvanceTime bool,
	parentState state.Chain,
) (blocks.Block, error) {
	// Try rewarding stakers whose staking period ends at the new chain time.
	// This is done first to prioritize advancing the timestamp as quickly as
	// possible.
	stakerTxID, shouldReward, err := builder.getNextStakerToReward(timestamp, parentState)
	if err != nil {
		return nil, fmt.Errorf("could not find next staker to reward: %w", err)
	}
	if shouldReward {
		rewardValidatorTx, err := builder.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return nil, fmt.Errorf("could not build tx to reward staker: %w", err)
		}

		return blocks.NewBanffProposalBlock(
			timestamp,
			parentID,
			height,
			rewardValidatorTx,
		)
	}

	// Clean out the mempool's transactions with invalid timestamps.
	builder.dropExpiredStakerTxs(timestamp)

	// If there is no reason to build a block, don't.
	if !builder.Mempool.HasTxs() && !forceAdvanceTime {
		builder.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return nil, errNoPendingBlocks
	}

	// Issue a block with as many transactions as possible.
	return blocks.NewBanffStandardBlock(
		timestamp,
		parentID,
		height,
		builder.Mempool.PeekTxs(targetBlockSize),
	)
}
