// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// nextTx returns the next transactions to be included in a block, along with
// its timestamp.
func (b *blockBuilder) nextPostForkTxs(prefBlkState state.Chain) ([]*txs.Tx, time.Time, error) {
	// Before selecting txs to be included in block, we need to clean mempool
	// from transactions with invalid timestamps.
	b.dropTooEarlyMempoolProposalTxs()

	// check if timing is right for a standard block and issue it if so
	nextStdBlkTime, couldBuildStdBlk, err := b.nextStandardPostForkBlkTimestamp(prefBlkState)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("could not evaluate what block should be built %s", err)
	}
	if couldBuildStdBlk && b.HasDecisionTxs() {
		txs := b.PopDecisionTxs(TargetBlockSize)
		return txs, nextStdBlkTime, nil
	}

	// try rewarding a staker
	stakerTx, shouldReward, err := b.getStakerToReward(prefBlkState)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("could not find next staker to reward %s", err)
	}
	if shouldReward {
		rewardValidatorTx, err := b.txBuilder.NewRewardValidatorTx(stakerTx.ID())
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("could not build tx to reward staker %s", err)
		}

		nextPropBlkTime := stakerTx.Unsigned.(txs.StakerTx).EndTime()
		return []*txs.Tx{rewardValidatorTx}, nextPropBlkTime, nil
	}

	// Try advance chain time or issue a mempool proposal tx;
	// whatever is earlier.
	nextChainTime, shouldAdvanceTime, err := b.getNextChainTime(prefBlkState)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("could not retrieve next chain time %s", err)
	}

	if shouldAdvanceTime && nextChainTime.Before(b.EarliestProposalTxTime()) {
		// whether or not there is a proposalTx in mempool,
		// chain time should be advanced first.
		// Just issue an empty standard block to advance time.
		return []*txs.Tx{}, nextChainTime, nil
	}

	if b.HasProposalTx() {
		// should tx start time happen to match with nextChainTime,
		// we'll issue here a block with the right time,
		// carriying the proposalTx too
		tx := b.PopProposalTx()
		startTime := tx.Unsigned.(txs.StakerTx).StartTime()
		return []*txs.Tx{tx}, startTime, nil
	}

	b.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
	return nil, time.Time{}, errNoPendingBlocks
}

// nextStandardPostForkBlkTimestamp attempts to calculate timestamp of next Standard Block
// to be issued. Returns the calculated time, true/false if a standard block could be issued,
// finally error.
func (b *blockBuilder) nextStandardPostForkBlkTimestamp(preBlkState state.Chain) (time.Time, bool, error) {
	minNextTimestamp := preBlkState.GetTimestamp()

	maxNextTimeStamp, err := preBlkState.GetNextStakerChangeTime()
	if err != nil {
		return time.Time{}, false, err
	}
	if mt := b.Mempool.EarliestProposalTxTime(); mt.Before(maxNextTimeStamp) {
		maxNextTimeStamp = mt
	}

	nextBlkTime := b.txExecutorBackend.Clk.Time()
	if !nextBlkTime.After(minNextTimestamp) {
		nextBlkTime = minNextTimestamp.Add(time.Second)
	}
	if !nextBlkTime.Before(maxNextTimeStamp) {
		// a proposal block must be issued before this standard block.
		return time.Time{}, false, nil
	}

	return nextBlkTime, true, nil
}
