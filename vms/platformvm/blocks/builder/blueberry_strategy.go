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

var _ buildingStrategy = &blueberryStrategy{}

type blueberryStrategy struct {
	*blockBuilder

	// inputs
	parentBlkID ids.ID
	parentState state.Chain
	height      uint64

	// outputs to build blocks
	txes    []*txs.Tx
	blkTime time.Time
}

func (b *blueberryStrategy) selectBlockContent() error {
	// blkTimestamp is zeroed for Apricot blocks
	// it is tentatively set to chainTime for Blueberry ones
	blkTime := b.parentState.GetTimestamp()

	// try including as many standard txs as possible. No need to advance chain time
	if b.HasDecisionTxs() {
		txs := b.PopDecisionTxs(TargetBlockSize)
		b.txes = txs
		b.blkTime = blkTime
		return nil
	}

	// try rewarding stakers whose staking period ends at current chain time.
	stakerTx, shouldReward, err := b.getStakerToReward(b.parentState)
	if err != nil {
		return fmt.Errorf("could not find next staker to reward %s", err)
	}
	if shouldReward {
		rewardValidatorTx, err := b.txBuilder.NewRewardValidatorTx(stakerTx.ID())
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

	b.txes, b.blkTime, err = b.trySelectMempoolProposalTx()
	return err
}

func (b *blueberryStrategy) trySelectMempoolProposalTx() ([]*txs.Tx, time.Time, error) {
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
	maxChainStartTime := b.parentState.GetTimestamp().Add(executor.MaxFutureStartTime)

	if startTime.After(maxChainStartTime) {
		now := b.txExecutorBackend.Clk.Time()
		return []*txs.Tx{tx}, now, nil
	}
	return []*txs.Tx{tx}, maxChainStartTime, nil
}

func (b *blueberryStrategy) build() (snowman.Block, error) {
	blkVersion := uint16(stateless.BlueberryVersion)
	if err := b.selectBlockContent(); err != nil {
		return nil, err
	}

	if len(b.txes) == 0 {
		// empty standard block are allowed to move chain time head
		return stateful.NewStandardBlock(
			blkVersion,
			uint64(b.blkTime.Unix()),
			b.blkVerifier,
			b.txExecutorBackend,
			b.parentBlkID,
			b.height,
			nil,
		)
	}

	switch b.txes[0].Unsigned.(type) {
	case txs.StakerTx,
		*txs.RewardValidatorTx,
		*txs.AdvanceTimeTx:
		return stateful.NewProposalBlock(
			blkVersion,
			uint64(b.blkTime.Unix()),
			b.blkVerifier,
			b.txExecutorBackend,
			b.parentBlkID,
			b.height,
			b.txes[0],
		)

	case *txs.CreateChainTx,
		*txs.CreateSubnetTx,
		*txs.ImportTx,
		*txs.ExportTx:
		return stateful.NewStandardBlock(
			blkVersion,
			uint64(b.blkTime.Unix()),
			b.blkVerifier,
			b.txExecutorBackend,
			b.parentBlkID,
			b.height,
			b.txes,
		)

	default:
		return nil, fmt.Errorf("unhandled tx type, could not include into a block")
	}
}
