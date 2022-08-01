// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var _ buildingStrategy = &apricotStrategy{}

type apricotStrategy struct {
	*blockBuilder

	// inputs
	// All must be set before [build] is called.
	parentBlkID ids.ID
	parentState state.Chain
	// Build a block with this height.
	nextHeight uint64

	// outputs
	// Set in [selectBlockContent].
	txes []*txs.Tx
}

func (a *apricotStrategy) hasContent() (bool, error) {
	if err := a.selectBlockContent(); err != nil {
		return false, err
	}

	return len(a.txes) != 0, nil
}

// Note: selectBlockContent will only peek into mempool and must not
// remove any transactions. It's up to the caller to cleanup the mempool
// if it must
func (a *apricotStrategy) selectBlockContent() error {
	// try including as many standard txs as possible. No need to advance chain time
	if a.Mempool.HasDecisionTxs() {
		a.txes = a.Mempool.PeekDecisionTxs(TargetBlockSize)
		return nil
	}

	// try rewarding stakers whose staking period ends at current chain time.
	stakerTxID, shouldReward, err := a.blockBuilder.getNextStakerToReward(a.parentState)
	if err != nil {
		return fmt.Errorf("could not find next staker to reward %s", err)
	}
	if shouldReward {
		rewardValidatorTx, err := a.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return fmt.Errorf("could not build tx to reward staker %s", err)
		}

		a.txes = []*txs.Tx{rewardValidatorTx}
		return nil
	}

	// try advancing chain time
	nextChainTime, shouldAdvanceTime, err := a.blockBuilder.getNextChainTime(a.parentState)
	if err != nil {
		return fmt.Errorf("could not retrieve next chain time %s", err)
	}
	if shouldAdvanceTime {
		advanceTimeTx, err := a.txBuilder.NewAdvanceTimeTx(nextChainTime)
		if err != nil {
			return fmt.Errorf("could not build tx to reward staker %s", err)
		}
		a.txes = []*txs.Tx{advanceTimeTx}
		return nil
	}

	a.txes, err = a.nextProposalTx()
	return err
}

// Try to get/make a proposal tx to put into a block.
// Returns an error if there's no suitable proposal tx.
// Doesn't modify [a.Mempool].
// TODO can this return one tx?
func (a *apricotStrategy) nextProposalTx() ([]*txs.Tx, error) {
	// clean out transactions with an invalid timestamp.
	a.dropExpiredProposalTxs()

	// Check the mempool
	if !a.Mempool.HasProposalTx() {
		a.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return nil, errNoPendingBlocks
	}
	tx := a.Mempool.PeekProposalTx()
	startTime := tx.Unsigned.(txs.StakerTx).StartTime()

	// Check whether this staker starts within at most [MaxFutureStartTime].
	// If it does, issue the staking tx.
	// If it doesn't, issue an advance time tx.
	maxChainStartTime := a.parentState.GetTimestamp().Add(executor.MaxFutureStartTime)
	if !startTime.After(maxChainStartTime) {
		return []*txs.Tx{tx}, nil
	}

	// The chain timestamp is too far in the past. Advance it.
	now := a.txExecutorBackend.Clk.Time()
	advanceTimeTx, err := a.txBuilder.NewAdvanceTimeTx(now)
	if err != nil {
		return nil, fmt.Errorf("could not build tx to advance time %s", err)
	}
	return []*txs.Tx{advanceTimeTx}, nil
}

func (a *apricotStrategy) build() (snowman.Block, error) {
	blkVersion := uint16(stateless.ApricotVersion)
	if err := a.selectBlockContent(); err != nil {
		return nil, err
	}

	// remove selected txs from mempool
	a.Mempool.Remove(a.txes)
	tx := a.txes[0]
	switch tx.Unsigned.(type) {
	case txs.StakerTx,
		*txs.RewardValidatorTx,
		*txs.AdvanceTimeTx:
		statelessBlk, err := stateless.NewProposalBlock(
			blkVersion,
			uint64(0),
			a.parentBlkID,
			a.nextHeight,
			tx,
		)
		if err != nil {
			return nil, err
		}
		return a.blkManager.NewBlock(statelessBlk), nil

	case *txs.CreateChainTx,
		*txs.CreateSubnetTx,
		*txs.ImportTx,
		*txs.ExportTx:
		statelessBlk, err := stateless.NewStandardBlock(
			blkVersion,
			uint64(0),
			a.parentBlkID,
			a.nextHeight,
			a.txes,
		)
		if err != nil {
			return nil, err
		}
		return a.blkManager.NewBlock(statelessBlk), nil

	default:
		return nil, fmt.Errorf("unhandled tx type %T, could not include into a block", tx.Unsigned)
	}
}
