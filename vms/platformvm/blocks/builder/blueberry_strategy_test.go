// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/stretchr/testify/assert"
)

func TestBlueberryPickingOrder(t *testing.T) {
	assert := assert.New(t)

	// mock ResetBlockTimer to control timing of block formation
	env := newEnvironment(t, true /*mockResetBlockTimer*/)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.config.BlueberryTime = time.Time{}

	chainTime := env.state.GetTimestamp()
	now := chainTime.Add(time.Second)
	env.clk.Set(now)

	nextChainTime := chainTime.Add(env.config.MinStakeDuration).Add(time.Hour)

	// create validator
	validatorStartTime := now.Add(time.Second)
	validatorTx, err := createTestValidatorTx(env, validatorStartTime, nextChainTime)
	assert.NoError(err)

	// accept validator as pending
	txExecutor := executor.ProposalTxExecutor{
		Backend:          &env.backend,
		ReferenceBlockID: env.state.GetLastAccepted(),
		Tx:               validatorTx,
	}
	assert.NoError(validatorTx.Unsigned.Visit(&txExecutor))
	txExecutor.OnCommit.AddTx(validatorTx, status.Committed)
	txExecutor.OnCommit.Apply(env.state)
	assert.NoError(env.state.Commit())

	// promote validator to current
	advanceTime, err := env.txBuilder.NewAdvanceTimeTx(validatorStartTime)
	assert.NoError(err)
	txExecutor.Tx = advanceTime
	assert.NoError(advanceTime.Unsigned.Visit(&txExecutor))
	txExecutor.OnCommit.Apply(env.state)
	assert.NoError(env.state.Commit())

	// move chain time to current validator's
	// end of staking time, so that it may be rewarded
	env.state.SetTimestamp(nextChainTime)
	now = nextChainTime
	env.clk.Set(now)

	// add decisionTx and stakerTxs to mempool
	decisionTxs, err := createTestDecisionTxes(2)
	assert.NoError(err)
	for _, dt := range decisionTxs {
		assert.NoError(env.mempool.Add(dt))
	}

	// start time is beyond maximal distance from chain time
	starkerTxStartTime := nextChainTime.Add(executor.MaxFutureStartTime).Add(time.Second)
	stakerTx, err := createTestValidatorTx(env, starkerTxStartTime, starkerTxStartTime.Add(time.Hour))
	assert.NoError(err)
	assert.NoError(env.mempool.Add(stakerTx))

	// test: decisionTxs must be picked first
	blk, err := env.BlockBuilder.BuildBlock()
	assert.NoError(err)
	assert.True(blk.Timestamp().Equal(nextChainTime))
	stdBlk, ok := blk.(*stateful.Block)
	assert.True(ok)
	_, ok = stdBlk.Block.(*stateless.BlueberryStandardBlock)
	assert.True(ok)
	assert.True(len(decisionTxs) == len(stdBlk.BlockTxs()))
	for i, tx := range stdBlk.BlockTxs() {
		assert.Equal(decisionTxs[i].ID(), tx.ID())
	}

	assert.False(env.mempool.HasDecisionTxs())

	// test: reward validator blocks must follow, one per endingValidator
	blk, err = env.BlockBuilder.BuildBlock()
	assert.NoError(err)
	assert.True(blk.Timestamp().Equal(nextChainTime))
	rewardBlk, ok := blk.(*stateful.OracleBlock)
	assert.True(ok)
	_, ok = rewardBlk.Block.Block.(*stateless.BlueberryProposalBlock)
	assert.True(ok)
	rewardTx, ok := rewardBlk.BlockTxs()[0].Unsigned.(*txs.RewardValidatorTx)
	assert.True(ok)
	assert.Equal(validatorTx.ID(), rewardTx.TxID)

	// accept reward validator tx so that current validator is removed
	assert.NoError(blk.Verify())
	assert.NoError(blk.Accept())
	options, err := rewardBlk.Options()
	assert.NoError(err)
	commitBlk := options[0]
	assert.NoError(commitBlk.Verify())
	assert.NoError(commitBlk.Accept())
	assert.NoError(env.BlockBuilder.SetPreference(commitBlk.ID()))

	// mempool proposal tx is too far in the future. A
	// proposal block including mempool proposalTx
	// will be issued to advance time and
	now = nextChainTime.Add(executor.MaxFutureStartTime / 2)
	env.clk.Set(now)
	blk, err = env.BlockBuilder.BuildBlock()
	assert.NoError(err)
	assert.True(blk.Timestamp().Equal(now))
	proposalBlk, ok := blk.(*stateful.OracleBlock)
	assert.True(ok)
	_, ok = proposalBlk.Block.Block.(*stateless.BlueberryProposalBlock)
	assert.True(ok)
	assert.Equal(stakerTx.ID(), proposalBlk.BlockTxs()[0].ID())

	// Finally an empty standard block can be issued to advance time
	// if no mempool txs are available
	now, err = executor.GetNextStakerChangeTime(env.state)
	assert.NoError(err)
	env.clk.Set(now)

	// finally mempool addValidatorTx must be picked
	blk, err = env.BlockBuilder.BuildBlock()
	assert.NoError(err)
	assert.True(blk.Timestamp().Equal(now))
	emptyStdBlk, ok := blk.(*stateful.Block)
	assert.True(ok)
	_, ok = emptyStdBlk.Block.(*stateless.BlueberryStandardBlock)
	assert.True(ok)
	assert.True(len(emptyStdBlk.BlockTxs()) == 0)
}
