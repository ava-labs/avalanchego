// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"

	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func TestApricotPickingOrder(t *testing.T) {
	require := require.New(t)

	// mock ResetBlockTimer to control timing of block formation
	env := newEnvironment(t, true /*mockResetBlockTimer*/)
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.config.BlueberryTime = mockable.MaxTime // Blueberry not active yet

	chainTime := env.state.GetTimestamp()
	now := chainTime.Add(time.Second)
	env.clk.Set(now)

	nextChainTime := chainTime.Add(env.config.MinStakeDuration).Add(time.Hour)

	// create validator
	validatorStartTime := now.Add(time.Second)
	validatorTx, err := createTestValidatorTx(env, validatorStartTime, nextChainTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
	require.NoError(err)

	onAbortState, err := state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
	require.NoError(err)

	// accept validator as pending
	txExecutor := txexecutor.ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            validatorTx,
	}
	require.NoError(validatorTx.Unsigned.Visit(&txExecutor))
	txExecutor.OnCommitState.AddTx(validatorTx, status.Committed)
	txExecutor.OnCommitState.Apply(env.state)
	require.NoError(env.state.Commit())

	// promote validator to current
	// Reset onCommitState and onAbortState
	txExecutor.OnCommitState, err = state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
	require.NoError(err)
	txExecutor.OnAbortState, err = state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
	require.NoError(err)
	advanceTime, err := env.txBuilder.NewAdvanceTimeTx(validatorStartTime)
	require.NoError(err)
	txExecutor.Tx = advanceTime
	require.NoError(advanceTime.Unsigned.Visit(&txExecutor))
	txExecutor.OnCommitState.Apply(env.state)
	require.NoError(env.state.Commit())

	// move chain time to current validator's
	// end of staking time, so that it may be rewarded
	env.state.SetTimestamp(nextChainTime)
	now = nextChainTime
	env.clk.Set(now)

	// add decisionTx and stakerTxs to mempool
	decisionTxs, err := createTestDecisionTxes(2)
	require.NoError(err)
	for _, dt := range decisionTxs {
		require.NoError(env.mempool.Add(dt))
	}

	// start time is beyond maximal distance from chain time
	starkerTxStartTime := nextChainTime.Add(txexecutor.MaxFutureStartTime).Add(time.Second)
	stakerTx, err := createTestValidatorTx(env, starkerTxStartTime, starkerTxStartTime.Add(time.Hour))
	require.NoError(err)
	require.NoError(env.mempool.Add(stakerTx))

	// test: decisionTxs must be picked first
	blk, err := env.Builder.BuildBlock()
	require.NoError(err)
	stdBlk, ok := blk.(*blockexecutor.Block)
	require.True(ok)
	_, ok = stdBlk.Block.(*blocks.ApricotStandardBlock)
	require.True(ok)
	require.Equal(decisionTxs, stdBlk.Txs())
	require.False(env.mempool.HasDecisionTxs())

	// test: reward validator blocks must follow, one per endingValidator
	blk, err = env.Builder.BuildBlock()
	require.NoError(err)
	rewardBlk, ok := blk.(*blockexecutor.Block)
	require.True(ok)
	_, ok = rewardBlk.Block.(*blocks.ApricotProposalBlock)
	require.True(ok)
	rewardTx, ok := rewardBlk.Txs()[0].Unsigned.(*txs.RewardValidatorTx)
	require.True(ok)
	require.Equal(validatorTx.ID(), rewardTx.TxID)

	// accept reward validator tx so that current validator is removed
	require.NoError(blk.Verify())
	require.NoError(blk.Accept())
	options, err := blk.(snowman.OracleBlock).Options()
	require.NoError(err)
	commitBlk := options[0]
	require.NoError(commitBlk.Verify())
	require.NoError(commitBlk.Accept())
	env.Builder.SetPreference(commitBlk.ID())

	// mempool proposal tx is too far in the future. An advance time tx
	// will be issued first
	now = nextChainTime.Add(txexecutor.MaxFutureStartTime / 2)
	env.clk.Set(now)
	blk, err = env.Builder.BuildBlock()
	require.NoError(err)
	advanceTimeBlk, ok := blk.(*blockexecutor.Block)
	require.True(ok)
	_, ok = advanceTimeBlk.Block.(*blocks.ApricotProposalBlock)
	require.True(ok)
	advanceTimeTx, ok := advanceTimeBlk.Txs()[0].Unsigned.(*txs.AdvanceTimeTx)
	require.True(ok)
	require.True(advanceTimeTx.Timestamp().Equal(now))

	// accept advance time tx so that we can issue mempool proposal tx
	require.NoError(blk.Verify())
	require.NoError(blk.Accept())
	options, err = blk.(snowman.OracleBlock).Options()
	require.NoError(err)
	commitBlk = options[0]
	require.NoError(commitBlk.Verify())
	require.NoError(commitBlk.Accept())
	env.Builder.SetPreference(commitBlk.ID())

	// finally mempool addValidatorTx must be picked
	blk, err = env.Builder.BuildBlock()
	require.NoError(err)
	proposalBlk, ok := blk.(*blockexecutor.Block)
	require.True(ok)
	_, ok = proposalBlk.Block.(*blocks.ApricotProposalBlock)
	require.True(ok)
	require.Equal([]*txs.Tx{stakerTx}, proposalBlk.Txs())
}
