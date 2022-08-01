// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var _ buildingStrategy = &blueberryStrategy{}

type blueberryStrategy struct {
	*blockBuilder

	// inputs
	// All must be set before [build] is called.
	parentBlkID ids.ID
	parentState state.Chain
	height      uint64

	// outputs
	// All set in [selectBlockContent].
	txes    []*txs.Tx
	blkTime time.Time
}

func (b *blueberryStrategy) hasContent() (bool, error) {
	if err := b.selectBlockContent(); err != nil {
		return false, err
	}

	if len(b.txes) == 0 {
		// blueberry allows empty blocks with non-zero timestamp
		// to move ahead chain time
		return !b.blkTime.IsZero(), nil
	}

	return true, nil
}

// Note: selectBlockContent will only peek into mempool and must not
// remove any transactions. It's up to the caller to cleanup the mempool
// if it must
func (b *blueberryStrategy) selectBlockContent() error {
	// blkTimestamp is zeroed for Apricot blocks
	// it is tentatively set to chainTime for Blueberry ones
	blkTime := b.parentState.GetTimestamp()

	// try including as many standard txs as possible. No need to advance chain time
	if b.Mempool.HasDecisionTxs() {
		b.txes = b.Mempool.PeekDecisionTxs(TargetBlockSize)
		b.blkTime = blkTime
		return nil
	}

	// try rewarding stakers whose staking period ends at current chain time.
	stakerTxID, shouldReward, err := b.blockBuilder.getNextStakerToReward(b.parentState)
	if err != nil {
		return fmt.Errorf("could not find next staker to reward %s", err)
	}
	if shouldReward {
		rewardValidatorTx, err := b.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return fmt.Errorf("could not build tx to reward staker %s", err)
		}

		b.txes = []*txs.Tx{rewardValidatorTx}
		b.blkTime = blkTime
		return nil
	}

	// try advancing chain time. It may result in empty blocks
	nextChainTime, shouldAdvanceTime, err := b.getNextChainTime(b.parentState)
	if err != nil {
		return fmt.Errorf("could not retrieve next chain time %s", err)
	}
	if shouldAdvanceTime {
		b.txes = []*txs.Tx{}
		b.blkTime = nextChainTime
		return nil
	}

	// clean out the mempool's transactions with invalid timestamps.
	b.dropExpiredProposalTxs()

	// try including a mempool proposal tx is available.
	if !b.Mempool.HasProposalTx() {
		b.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return errNoPendingBlocks
	}
	tx := b.Mempool.PeekProposalTx()

	// if the chain timestamp is too far in the past to issue this transaction
	// but according to local time, it's ready to be issued, then attempt to
	// advance the timestamp, so it can be issued.
	startTime := tx.Unsigned.(txs.StakerTx).StartTime()
	maxChainStartTime := b.parentState.GetTimestamp().Add(executor.MaxFutureStartTime)

	b.txes = []*txs.Tx{tx}
	if startTime.After(maxChainStartTime) {
		now := b.txExecutorBackend.Clk.Time()
		b.blkTime = now
	} else {
		b.blkTime = blkTime // do not change chain time
	}
	return err
}

func (b *blueberryStrategy) build() (snowman.Block, error) {
	blkVersion := version.BlueberryBlockVersion
	if err := b.selectBlockContent(); err != nil {
		return nil, err
	}

	// remove selected txs from mempool
	b.Mempool.Remove(b.txes)

	if len(b.txes) == 0 {
		// empty standard block are allowed to move chain time head
		statelessBlk, err := stateless.NewStandardBlock(
			blkVersion,
			uint64(b.blkTime.Unix()),
			b.parentBlkID,
			b.height,
			nil,
		)
		if err != nil {
			return nil, err
		}
		return b.blkManager.NewBlock(statelessBlk), nil
	}

	tx := b.txes[0]
	switch tx.Unsigned.(type) {
	case txs.StakerTx,
		*txs.RewardValidatorTx,
		*txs.AdvanceTimeTx:
		statelessBlk, err := stateless.NewProposalBlock(
			blkVersion,
			uint64(b.blkTime.Unix()),
			b.parentBlkID,
			b.height,
			tx,
		)
		if err != nil {
			return nil, err
		}
		return b.blkManager.NewBlock(statelessBlk), nil

	case *txs.CreateChainTx,
		*txs.CreateSubnetTx,
		*txs.ImportTx,
		*txs.ExportTx:
		statelessBlk, err := stateless.NewStandardBlock(
			blkVersion,
			uint64(b.blkTime.Unix()),
			b.parentBlkID,
			b.height,
			b.txes,
		)
		if err != nil {
			return nil, err
		}
		return b.blkManager.NewBlock(statelessBlk), nil

	default:
		return nil, fmt.Errorf("unhandled tx type %T, could not include into a block", tx.Unsigned)
	}
}
