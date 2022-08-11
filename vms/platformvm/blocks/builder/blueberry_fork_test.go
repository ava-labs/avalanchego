// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/stretchr/testify/assert"
)

func TestBlueberryFork(t *testing.T) {
	assert := assert.New(t)

	// mock ResetBlockTimer to control timing of block formation
	env := newEnvironment(t, true /*mockResetBlockTimer*/)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	chainTime := env.state.GetTimestamp()
	env.clk.Set(chainTime)

	factory := crypto.FactorySECP256K1R{}
	nodeIDKey, _ := factory.NewPrivateKey()
	rewardAddress := nodeIDKey.PublicKey().Address()

	preBlueberryTimes := []time.Time{
		chainTime.Add(1 * executor.SyncBound),
		chainTime.Add(2 * executor.SyncBound),
	}
	env.config.BlueberryTime = preBlueberryTimes[len(preBlueberryTimes)-1]

	for i, nextValidatorStartTime := range preBlueberryTimes {
		// add a validator with the right start time
		// so that we can then advance chain time to it
		addPendingValidatorTx, err := env.txBuilder.NewAddValidatorTx(
			env.config.MinValidatorStake,
			uint64(nextValidatorStartTime.Unix()),
			uint64(defaultValidateEndTime.Unix()),
			ids.GenerateTestNodeID(),
			rewardAddress,
			reward.PercentDenominator,
			[]*crypto.PrivateKeySECP256K1R{preFundedKeys[i]},
			ids.ShortEmpty,
		)
		assert.NoError(err)
		assert.NoError(env.mempool.Add(addPendingValidatorTx))

		proposalBlk, err := env.Builder.BuildBlock()
		assert.NoError(err)
		assert.NoError(proposalBlk.Verify())
		assert.NoError(proposalBlk.Accept())
		assert.NoError(env.state.Commit())

		options, err := proposalBlk.(snowman.OracleBlock).Options()
		assert.NoError(err)
		commitBlk := options[0]
		assert.NoError(commitBlk.Verify())
		assert.NoError(commitBlk.Accept())
		assert.NoError(env.state.Commit())
		env.Builder.SetPreference(commitBlk.ID())

		// advance chain time
		env.clk.Set(nextValidatorStartTime)
		advanceTimeBlk, err := env.Builder.BuildBlock()
		assert.NoError(err)
		assert.NoError(advanceTimeBlk.Verify())
		assert.NoError(advanceTimeBlk.Accept())
		assert.NoError(env.state.Commit())

		options, err = advanceTimeBlk.(snowman.OracleBlock).Options()
		assert.NoError(err)
		commitBlk = options[0]
		assert.NoError(commitBlk.Verify())
		assert.NoError(commitBlk.Accept())
		assert.NoError(env.state.Commit())
		env.Builder.SetPreference(commitBlk.ID())
	}

	// check Blueberry fork is activated
	assert.True(env.state.GetTimestamp().Equal(env.config.BlueberryTime))

	createChainTx, err := env.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	assert.NoError(err)
	assert.NoError(env.mempool.Add(createChainTx))

	proposalBlk, err := env.Builder.BuildBlock()
	assert.NoError(err)
	assert.NoError(proposalBlk.Verify())
	assert.NoError(proposalBlk.Accept())
	assert.NoError(env.state.Commit())
}
