// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"

	transactions "github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ forkBuilder = &blueberryStrategy{}

type blueberryStrategy struct {
	*builder

	// inputs
	// All must be set before [build] is called.
	parentBlkID ids.ID
	parentState state.Chain
	height      uint64
}

// Returns:
// 1. The transactions we should include in the block
//    Note that this may be nil -- empty blocks can advance the timestamp.
// 2. The timestamp for the block
// Only updates the mempool to remove expired proposal txs.
// It's up to the caller to cleanup the mempool if necessary.
func (b *blueberryStrategy) selectTxs() ([]*transactions.Tx, time.Time, error) {
	// timestamp is zeroed for Apricot blocks
	// it is set to the proposed chainTime for Blueberry ones
	parentTimestamp := b.parentState.GetTimestamp()

	// try including as many standard txs as possible. No need to advance chain time
	if b.Mempool.HasDecisionTxs() {
		return b.Mempool.PeekDecisionTxs(targetBlockSize), parentTimestamp, nil
	}

	// try rewarding stakers whose staking period ends at current chain time.
	stakerTxID, shouldReward, err := b.builder.getNextStakerToReward(b.parentState)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("could not find next staker to reward: %w", err)
	}
	if shouldReward {
		rewardValidatorTx, err := b.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("could not build tx to reward staker: %w", err)
		}

		return []*transactions.Tx{rewardValidatorTx}, parentTimestamp, nil
	}

	// try advancing chain time. It may result in empty blocks
	nextChainTime, shouldAdvanceTime, err := b.getNextChainTime(b.parentState)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("could not retrieve next chain time: %w", err)
	}
	if shouldAdvanceTime {
		return nil, nextChainTime, nil
	}

	// clean out the mempool's transactions with invalid timestamps.
	b.dropExpiredProposalTxs()

	// try including a mempool proposal tx is available.
	if !b.Mempool.HasProposalTx() {
		b.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return nil, time.Time{}, errNoPendingBlocks
	}
	tx := b.Mempool.PeekProposalTx()

	// if the chain timestamp is too far in the past to issue this transaction
	// but according to local time, it's ready to be issued, then attempt to
	// advance the timestamp, so it can be issued.
	startTime := tx.Unsigned.(transactions.StakerTx).StartTime()
	maxChainStartTime := b.parentState.GetTimestamp().Add(executor.MaxFutureStartTime)

	newTimestamp := parentTimestamp
	if startTime.After(maxChainStartTime) {
		// setting blkTime to now to propose moving chain time ahead
		newTimestamp = b.txExecutorBackend.Clk.Time()
	}

	return []*transactions.Tx{tx}, newTimestamp, nil
}

func (b *blueberryStrategy) buildBlock() (snowman.Block, []*transactions.Tx, error) {
	txs, timestamp, err := b.selectTxs()
	if err != nil {
		return nil, nil, err
	}

	if len(txs) == 0 {
		// empty standard block are allowed to move chain time head
		statelessBlk, err := blocks.NewBlueberryStandardBlock(
			timestamp,
			b.parentBlkID,
			b.height,
			nil,
		)
		if err != nil {
			return nil, nil, err
		}
		return b.blkManager.NewBlock(statelessBlk), txs, nil
	}

	var (
		tx           = txs[0]
		statelessBlk blocks.Block
	)
	switch tx.Unsigned.(type) {
	case transactions.StakerTx,
		*transactions.RewardValidatorTx,
		*transactions.AdvanceTimeTx:
		statelessBlk, err = blocks.NewBlueberryProposalBlock(
			timestamp,
			b.parentBlkID,
			b.height,
			tx,
		)

	case *transactions.CreateChainTx,
		*transactions.CreateSubnetTx,
		*transactions.ImportTx,
		*transactions.ExportTx:
		statelessBlk, err = blocks.NewBlueberryStandardBlock(
			timestamp,
			b.parentBlkID,
			b.height,
			txs,
		)

	default:
		return nil, nil, fmt.Errorf("unhandled tx type %T, could not include into a block", tx.Unsigned)
	}

	if err != nil {
		return nil, nil, err
	}
	return b.blkManager.NewBlock(statelessBlk), txs, nil
}
