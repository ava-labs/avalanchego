package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var _ buildingStrategy = &apricotStrategy{}

type apricotStrategy struct {
	*blockBuilder

	// inputs
	parentBlkID ids.ID
	parentState state.Chain
	height      uint64

	// outputs to build blocks
	txes []*txs.Tx
}

func (a *apricotStrategy) selectBlockContent() error {
	// try including as many standard txs as possible. No need to advance chain time
	if a.HasDecisionTxs() {
		txs := a.PopDecisionTxs(TargetBlockSize)
		a.txes = txs
		return nil
	}

	// try rewarding stakers whose staking period ends at current chain time.
	stakerTx, shouldReward, err := a.getStakerToReward(a.parentState)
	if err != nil {
		return fmt.Errorf("could not find next staker to reward %s", err)
	}
	if shouldReward {
		rewardValidatorTx, err := a.txBuilder.NewRewardValidatorTx(stakerTx.ID())
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

	a.txes, err = a.trySelectMempoolProposalTx()
	return err
}

func (a *apricotStrategy) trySelectMempoolProposalTx() ([]*txs.Tx, error) {
	// clean out the mempool's transactions with invalid timestamps.
	a.dropTooEarlyMempoolProposalTxs()

	// try including a mempool proposal tx is available.
	if !a.HasProposalTx() {
		a.txExecutorBackend.Ctx.Log.Debug("no pending txs to issue into a block")
		return []*txs.Tx{}, errNoPendingBlocks
	}

	tx := a.PopProposalTx()
	startTime := tx.Unsigned.(txs.StakerTx).StartTime()

	// if the chain timestamp is too far in the past to issue this transaction
	// but according to local time, it's ready to be issued, then attempt to
	// advance the timestamp, so it can be issued.
	maxChainStartTime := a.parentState.GetTimestamp().Add(executor.MaxFutureStartTime)
	if !startTime.After(maxChainStartTime) {
		return []*txs.Tx{tx}, nil
	}

	a.AddProposalTx(tx)
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

	switch a.txes[0].Unsigned.(type) {
	case txs.StakerTx,
		*txs.RewardValidatorTx,
		*txs.AdvanceTimeTx:
		return stateful.NewProposalBlock(
			blkVersion,
			uint64(0),
			a.blkVerifier,
			a.txExecutorBackend,
			a.parentBlkID,
			a.height,
			a.txes[0],
		)
	case *txs.CreateChainTx,
		*txs.CreateSubnetTx,
		*txs.ImportTx,
		*txs.ExportTx:
		return stateful.NewStandardBlock(
			blkVersion,
			uint64(0),
			a.blkVerifier,
			a.txExecutorBackend,
			a.parentBlkID,
			a.height,
			a.txes,
		)
	default:
		return nil, fmt.Errorf("unhandled tx type, could not include into a block")
	}
}
