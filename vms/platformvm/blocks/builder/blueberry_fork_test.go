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

	h := newTestHelpersCollection(t)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()

	chainTime := h.fullState.GetTimestamp()
	h.clk.Set(chainTime)

	factory := crypto.FactorySECP256K1R{}
	nodeIDKey, _ := factory.NewPrivateKey()
	rewardAddress := nodeIDKey.PublicKey().Address()

	preBlueberryTimes := []time.Time{
		chainTime.Add(1 * executor.SyncBound),
		chainTime.Add(2 * executor.SyncBound),
	}
	h.cfg.BlueberryTime = preBlueberryTimes[len(preBlueberryTimes)-1]

	for i, nextValidatorStartTime := range preBlueberryTimes {
		// add a validator with the right start time
		// so that we can then advance chain time to it
		addPendingValidatorTx, err := h.txBuilder.NewAddValidatorTx(
			h.cfg.MinValidatorStake,
			uint64(nextValidatorStartTime.Unix()),
			uint64(defaultValidateEndTime.Unix()),
			ids.GenerateTestNodeID(),
			rewardAddress,
			reward.PercentDenominator,
			[]*crypto.PrivateKeySECP256K1R{preFundedKeys[i]},
			ids.ShortEmpty,
		)
		assert.NoError(err)
		h.mpool.AddProposalTx(addPendingValidatorTx)

		proposalBlk, err := h.BlockBuilder.BuildBlock()
		assert.NoError(err)
		assert.NoError(proposalBlk.Verify())
		assert.NoError(proposalBlk.Accept())
		assert.NoError(h.fullState.Commit())

		options, err := proposalBlk.(snowman.OracleBlock).Options()
		assert.NoError(err)
		commitBlk := options[0]
		assert.NoError(commitBlk.Verify())
		assert.NoError(commitBlk.Accept())
		assert.NoError(h.fullState.Commit())
		assert.NoError(h.BlockBuilder.SetPreference(commitBlk.ID()))

		// advance chain time
		h.clk.Set(nextValidatorStartTime)
		advanceTimeBlk, err := h.BlockBuilder.BuildBlock()
		assert.NoError(err)
		assert.NoError(advanceTimeBlk.Verify())
		assert.NoError(advanceTimeBlk.Accept())
		assert.NoError(h.fullState.Commit())

		options, err = advanceTimeBlk.(snowman.OracleBlock).Options()
		assert.NoError(err)
		commitBlk = options[0]
		assert.NoError(commitBlk.Verify())
		assert.NoError(commitBlk.Accept())
		assert.NoError(h.fullState.Commit())
		assert.NoError(h.BlockBuilder.SetPreference(commitBlk.ID()))
	}

	// check Blueberry fork is activated
	assert.True(h.fullState.GetTimestamp().Equal(h.cfg.BlueberryTime))

	createChainTx, err := h.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	assert.NoError(err)
	h.mpool.AddDecisionTx(createChainTx)

	proposalBlk, err := h.BlockBuilder.BuildBlock()
	assert.NoError(err)
	assert.NoError(proposalBlk.Verify())
	assert.NoError(proposalBlk.Accept())
	assert.NoError(h.fullState.Commit())

	// check post-Blueberry block moves ahead chain time
	assert.True(h.fullState.GetTimestamp().After(h.cfg.BlueberryTime))
}
