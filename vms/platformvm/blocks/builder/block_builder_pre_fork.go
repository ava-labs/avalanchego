// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func (b *blockBuilder) nextPreForkTxs(preBlkState state.Mutable) ([]*txs.Tx, error) {
	// Try return standard txs. Note that
	// PreFork standard blocks to not advance chain time
	if b.HasDecisionTxs() {
		txs := b.PopDecisionTxs(TargetBlockSize)
		return txs, nil
	}

	// try rewarding a staker
	stakerTx, shouldReward, err := b.getStakerToReward(preBlkState)
	if err != nil {
		return nil, fmt.Errorf("could not find next staker to reward %s", err)
	}
	if shouldReward {
		rewardValidatorTx, err := b.txBuilder.NewRewardValidatorTx(stakerTx.ID())
		if err != nil {
			return nil, fmt.Errorf("could not build tx to reward staker %s", err)
		}

		return []*txs.Tx{rewardValidatorTx}, nil
	}

	// Try building a proposal block that advances the chain timestamp.
	nextChainTime, shouldAdvanceTime, err := b.getNextChainTime(preBlkState)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve next chain time %s", err)
	}
	if shouldAdvanceTime {
		advanceTimeTx, err := b.txBuilder.NewAdvanceTimeTx(nextChainTime)
		if err != nil {
			return nil, fmt.Errorf("could not build tx to reward staker %s", err)
		}
		return []*txs.Tx{advanceTimeTx}, nil
	}

	// Clean out the mempool's transactions with invalid timestamps.
	b.dropTooEarlyMempoolProposalTxs()

	if !b.HasProposalTx() {
		b.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return nil, errNoPendingBlocks
	}

	// Get the proposal transaction that should be issued.
	tx := b.PopProposalTx()
	startTime := tx.Unsigned.(txs.StakerTx).StartTime()

	// If the chain timestamp is too far in the past to issue this transaction
	// but according to local time, it's ready to be issued, then attempt to
	// advance the timestamp, so it can be issued.
	maxChainStartTime := preBlkState.GetTimestamp().Add(executor.MaxFutureStartTime)
	if startTime.After(maxChainStartTime) {
		b.AddProposalTx(tx)

		now := b.txExecutorBackend.Clk.Time()
		advanceTimeTx, err := b.txBuilder.NewAdvanceTimeTx(now)
		if err != nil {
			return nil, fmt.Errorf("could not build tx to advance time %s", err)
		}
		return []*txs.Tx{advanceTimeTx}, nil
	}
	return []*txs.Tx{tx}, nil
}
