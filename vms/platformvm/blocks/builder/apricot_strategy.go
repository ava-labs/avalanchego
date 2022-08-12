// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"

	transactions "github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ buildingStrategy = &apricotStrategy{}

type apricotStrategy struct {
	*builder

	// inputs
	// All must be set before [build] is called.
	parentBlkID ids.ID
	parentState state.Chain
	// Build a block with this height.
	nextHeight uint64

	// outputs
	// Set in [selectBlockContent].
	txs []*transactions.Tx
}

func (a *apricotStrategy) hasContent() (bool, error) {
	if err := a.selectBlockContent(); err != nil {
		return false, err
	}

	return len(a.txs) != 0, nil
}

// Note: selectBlockContent will only peek into mempool and must not remove
// any transactions. It's up to the caller to cleanup the mempool if necessary.
// If this method returns nil, [a.txs] won't be empty.
func (a *apricotStrategy) selectBlockContent() error {
	// try including as many standard txs as possible. No need to advance chain time
	if a.Mempool.HasDecisionTxs() {
		a.txs = a.Mempool.PeekDecisionTxs(targetBlockSize)
		return nil
	}

	// try rewarding stakers whose staking period ends at current chain time.
	stakerTxID, shouldReward, err := a.builder.getNextStakerToReward(a.parentState)
	if err != nil {
		return fmt.Errorf("could not find next staker to reward %s", err)
	}
	if shouldReward {
		rewardValidatorTx, err := a.txBuilder.NewRewardValidatorTx(stakerTxID)
		if err != nil {
			return fmt.Errorf("could not build tx to reward staker %s", err)
		}

		a.txs = []*transactions.Tx{rewardValidatorTx}
		return nil
	}

	// try advancing chain time
	nextChainTime, shouldAdvanceTime, err := a.builder.getNextChainTime(a.parentState)
	if err != nil {
		return fmt.Errorf("could not retrieve next chain time %s", err)
	}
	if shouldAdvanceTime {
		advanceTimeTx, err := a.txBuilder.NewAdvanceTimeTx(nextChainTime)
		if err != nil {
			return fmt.Errorf("could not build tx to reward staker %s", err)
		}
		a.txs = []*transactions.Tx{advanceTimeTx}
		return nil
	}

	tx, err := a.nextProposalTx()
	if err != nil {
		return err
	}
	a.txs = []*transactions.Tx{tx}
	return nil
}

// Try to get/make a proposal tx to put into a block.
// Returns an error if there's no suitable proposal tx.
// Doesn't modify [a.Mempool].
func (a *apricotStrategy) nextProposalTx() (*transactions.Tx, error) {
	// clean out transactions with an invalid timestamp.
	a.dropExpiredProposalTxs()

	// Check the mempool
	if !a.Mempool.HasProposalTx() {
		a.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return nil, errNoPendingBlocks
	}
	tx := a.Mempool.PeekProposalTx()
	startTime := tx.Unsigned.(transactions.StakerTx).StartTime()

	// Check whether this staker starts within at most [MaxFutureStartTime].
	// If it does, issue the staking tx.
	// If it doesn't, issue an advance time tx.
	maxChainStartTime := a.parentState.GetTimestamp().Add(executor.MaxFutureStartTime)
	if !startTime.After(maxChainStartTime) {
		return tx, nil
	}

	// The chain timestamp is too far in the past. Advance it.
	now := a.txExecutorBackend.Clk.Time()
	advanceTimeTx, err := a.txBuilder.NewAdvanceTimeTx(now)
	if err != nil {
		return nil, fmt.Errorf("could not build tx to advance time %s", err)
	}
	return advanceTimeTx, nil
}

func (a *apricotStrategy) buildBlock() (snowman.Block, error) {
	if err := a.selectBlockContent(); err != nil {
		return nil, err
	}

	var (
		tx           = a.txs[0]
		statelessBlk blocks.Block
		err          error
	)
	switch tx.Unsigned.(type) {
	case transactions.StakerTx,
		*transactions.RewardValidatorTx,
		*transactions.AdvanceTimeTx:
		// Note that if [tx] is one of the above types, it's the only
		// tx in this block because Apricot proposal blocks have 1 tx.
		statelessBlk, err = blocks.NewApricotProposalBlock(
			a.parentBlkID,
			a.nextHeight,
			tx,
		)

	case *transactions.CreateChainTx,
		*transactions.CreateSubnetTx,
		*transactions.ImportTx,
		*transactions.ExportTx:
		// Note that if [tx] is one of the above types, all of
		// the txs in [a.txs] must be "standard" transactions.
		statelessBlk, err = blocks.NewApricotStandardBlock(
			a.parentBlkID,
			a.nextHeight,
			a.txs,
		)

	default:
		return nil, fmt.Errorf("unhandled tx type %T, could not include into a block", tx.Unsigned)
	}

	if err != nil {
		return nil, err
	}
	// remove selected txs from mempool only when we are sure
	// a valid block containing it has been generated
	a.Mempool.Remove(a.txs)
	return a.blkManager.NewBlock(statelessBlk), nil
}
