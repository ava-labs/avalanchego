// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

// nextTx returns the next transactions to be included in a block, along with
// its timestamp.
func (b *blockBuilder) nextBlueberryTxs(chainTipState state.Chain) ([]*txs.Tx, time.Time, error) {
	currentChainTime := chainTipState.GetTimestamp()
	// try including as many standard txs as possible. No need to advance chain time
	if b.HasDecisionTxs() {
		txs := b.PopDecisionTxs(TargetBlockSize)
		return txs, currentChainTime, nil
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

		return []*txs.Tx{rewardValidatorTx}, currentChainTime, nil
	}

	// try advancing chain time. Note that is this case an empty block will be issued.
	nextChainTime, shouldAdvanceTime, err := b.getNextChainTime(chainTipState)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("could not retrieve next chain time %s", err)
	}
	if shouldAdvanceTime {
		return []*txs.Tx{}, nextChainTime, nil
	}

	// clean out the mempool's transactions with invalid timestamps.
	b.dropTooEarlyMempoolProposalTxs()

	// try including a mempool proposal tx is available.
	if !b.HasProposalTx() {
		b.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return nil, currentChainTime, errNoPendingBlocks
	}

	tx := b.PopProposalTx()
	startTime := tx.Unsigned.(txs.StakerTx).StartTime()

	// if the chain timestamp is too far in the past to issue this transaction
	// but according to local time, it's ready to be issued, then attempt to
	// advance the timestamp, so it can be issued.
	maxChainStartTime := chainTipState.GetTimestamp().Add(executor.MaxFutureStartTime)
	if startTime.After(maxChainStartTime) {
		now := b.txExecutorBackend.Clk.Time()
		return []*txs.Tx{tx}, now, nil
	}
	return []*txs.Tx{tx}, maxChainStartTime, nil
}

func (b *blockBuilder) buildBlueberryBlock(
	blkTime time.Time,
	parentBlkID ids.ID,
	height uint64,
	txes []*txs.Tx,
) (snowman.Block, error) {
	blkVersion := uint16(stateless.BlueberryVersion)
	if len(txes) == 0 {
		// empty standard block, just to move chain time head
		return stateful.NewStandardBlock(
			blkVersion,
			uint64(blkTime.Unix()),
			b.blkVerifier,
			b.txExecutorBackend,
			parentBlkID,
			height,
			nil,
		)
	}

	switch txes[0].Unsigned.(type) {
	case txs.StakerTx,
		*txs.RewardValidatorTx,
		*txs.AdvanceTimeTx:
		return stateful.NewProposalBlock(
			blkVersion,
			uint64(blkTime.Unix()),
			b.blkVerifier,
			b.txExecutorBackend,
			parentBlkID,
			height,
			txes[0],
		)

	case *txs.CreateChainTx,
		*txs.CreateSubnetTx,
		*txs.ImportTx,
		*txs.ExportTx:
		return stateful.NewStandardBlock(
			blkVersion,
			uint64(blkTime.Unix()),
			b.blkVerifier,
			b.txExecutorBackend,
			parentBlkID,
			height,
			txes,
		)

	default:
		return nil, fmt.Errorf("unhandled tx type, could not include into a block")
	}
}
