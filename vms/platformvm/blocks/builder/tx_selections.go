// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

// nextTx returns the next transactions to be included in a block, along with
// its timestamp.
func (b *blockBuilder) nextTxs(
	chainTipState state.Chain,
	blkVersion uint16,
) ([]*txs.Tx, time.Time, error) {
	// blkTimestamp is zeroed for Apricot blocks
	// it is tentatively set to chainTime for Blueberry ones
	blkTime := time.Time{}
	if blkVersion != stateless.ApricotVersion {
		blkTime = chainTipState.GetTimestamp()
	}

	// try including as many standard txs as possible. No need to advance chain time
	if b.HasDecisionTxs() {
		txs := b.PopDecisionTxs(TargetBlockSize)
		return txs, blkTime, nil
	}

	// try rewarding stakers whose staking period ends at current chain time.
	stakerTx, shouldReward, err := b.getStakerToReward(chainTipState)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("could not find next staker to reward %s", err)
	}
	if shouldReward {
		rewardValidatorTx, err := b.txBuilder.NewRewardValidatorTx(stakerTx.ID())
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("could not build tx to reward staker %s", err)
		}

		return []*txs.Tx{rewardValidatorTx}, blkTime, nil
	}

	// try advancing chain time. It may result in empty blocks
	txes, blkTime, shouldAdvance, err := b.tryAdvanceToNextChainTime(chainTipState, blkVersion)
	if shouldAdvance {
		return txes, blkTime, err
	}

	return b.trySelectMempoolProposalTx(chainTipState, blkVersion)
}

// Note: trailing boolean signals whether block content is found
// or an error occurred. In both cases tx selection is done
func (b *blockBuilder) tryAdvanceToNextChainTime(
	chainTipState state.Chain,
	blkVersion uint16,
) ([]*txs.Tx, time.Time, bool, error) {
	nextChainTime, shouldAdvanceTime, err := b.getNextChainTime(chainTipState)
	if err != nil {
		return nil, time.Time{}, true, fmt.Errorf("could not retrieve next chain time %s", err)
	}

	if shouldAdvanceTime {
		if blkVersion == stateless.ApricotVersion {
			advanceTimeTx, err := b.txBuilder.NewAdvanceTimeTx(nextChainTime)
			if err != nil {
				return nil, time.Time{}, true, fmt.Errorf("could not build tx to reward staker %s", err)
			}
			return []*txs.Tx{advanceTimeTx}, time.Time{}, true, nil
		}

		return []*txs.Tx{}, nextChainTime, true, nil
	}

	return nil, time.Time{}, false, nil
}

func (b *blockBuilder) trySelectMempoolProposalTx(
	chainTipState state.Chain,
	blkVersion uint16,
) ([]*txs.Tx, time.Time, error) {
	// clean out the mempool's transactions with invalid timestamps.
	b.dropTooEarlyMempoolProposalTxs()

	// try including a mempool proposal tx is available.
	if !b.HasProposalTx() {
		b.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return nil, time.Time{}, errNoPendingBlocks
	}

	tx := b.PopProposalTx()
	startTime := tx.Unsigned.(txs.StakerTx).StartTime()

	// if the chain timestamp is too far in the past to issue this transaction
	// but according to local time, it's ready to be issued, then attempt to
	// advance the timestamp, so it can be issued.
	maxChainStartTime := chainTipState.GetTimestamp().Add(executor.MaxFutureStartTime)

	if blkVersion == stateless.ApricotVersion {
		if !startTime.After(maxChainStartTime) {
			return []*txs.Tx{tx}, time.Time{}, nil
		}

		b.AddProposalTx(tx)
		now := b.txExecutorBackend.Clk.Time()
		advanceTimeTx, err := b.txBuilder.NewAdvanceTimeTx(now)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("could not build tx to advance time %s", err)
		}
		return []*txs.Tx{advanceTimeTx}, time.Time{}, nil
	}

	// Blueberry blocks
	if startTime.After(maxChainStartTime) {
		now := b.txExecutorBackend.Clk.Time()
		return []*txs.Tx{tx}, now, nil
	}
	return []*txs.Tx{tx}, maxChainStartTime, nil
}
