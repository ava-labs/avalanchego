// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

// TODO: possibly better placed in platformvm package?

func TestAddDelegatorTxOverDelegatedRegression(t *testing.T) {
	assert := assert.New(t)

	// mock ResetBlockTimer to control timing of block formation
	env := newEnvironment(t, true /*mockResetBlockTimer*/)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.ctx.Lock.Lock()
	env.sender.SendAppGossipF = func(b []byte) error { return nil }

	validatorStartTime := defaultGenesisTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	key, err := testKeyFactory.NewPrivateKey()
	assert.NoError(err)

	id := key.PublicKey().Address()

	// create valid tx
	addValidatorTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake,
		uint64(validatorStartTime.Unix()),
		uint64(validatorEndTime.Unix()),
		ids.NodeID(id),
		id,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	err = env.BlockBuilder.AddUnverifiedTx(addValidatorTx)
	assert.NoError(err)

	addValidatorBlock, err := env.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(env, assert, addValidatorBlock)

	env.clk.Set(validatorStartTime)

	firstAdvanceTimeBlock, err := env.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(env, assert, firstAdvanceTimeBlock)

	firstDelegatorStartTime := validatorStartTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	firstDelegatorEndTime := firstDelegatorStartTime.Add(env.config.MinStakeDuration)

	// create valid tx
	addFirstDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
		4*env.config.MinValidatorStake, // maximum amount of stake this delegator can provide
		uint64(firstDelegatorStartTime.Unix()),
		uint64(firstDelegatorEndTime.Unix()),
		ids.NodeID(id),
		preFundedKeys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	err = env.BlockBuilder.AddUnverifiedTx(addFirstDelegatorTx)
	assert.NoError(err)

	addFirstDelegatorBlock, err := env.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(env, assert, addFirstDelegatorBlock)

	env.clk.Set(firstDelegatorStartTime)

	secondAdvanceTimeBlock, err := env.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(env, assert, secondAdvanceTimeBlock)

	secondDelegatorStartTime := firstDelegatorEndTime.Add(2 * time.Second)
	secondDelegatorEndTime := secondDelegatorStartTime.Add(env.config.MinStakeDuration)

	env.clk.Set(secondDelegatorStartTime.Add(-10 * txexecutor.SyncBound))

	// create valid tx
	addSecondDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		uint64(secondDelegatorStartTime.Unix()),
		uint64(secondDelegatorEndTime.Unix()),
		ids.NodeID(id),
		preFundedKeys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1], preFundedKeys[3]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	err = env.BlockBuilder.AddUnverifiedTx(addSecondDelegatorTx)
	assert.NoError(err)

	addSecondDelegatorBlock, err := env.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(env, assert, addSecondDelegatorBlock)

	thirdDelegatorStartTime := firstDelegatorEndTime.Add(-time.Second)
	thirdDelegatorEndTime := thirdDelegatorStartTime.Add(env.config.MinStakeDuration)

	// create valid tx
	addThirdDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		uint64(thirdDelegatorStartTime.Unix()),
		uint64(thirdDelegatorEndTime.Unix()),
		ids.NodeID(id),
		preFundedKeys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1], preFundedKeys[4]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	err = env.BlockBuilder.AddUnverifiedTx(addThirdDelegatorTx)
	assert.Error(err, "should have marked the delegator as being over delegated")
}

func TestAddDelegatorTxHeapCorruption(t *testing.T) {
	validatorStartTime := defaultGenesisTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)
	validatorStake := defaultMaxValidatorStake / 5

	delegator1StartTime := validatorStartTime
	delegator1EndTime := delegator1StartTime.Add(3 * defaultMinStakingDuration)
	delegator1Stake := defaultMinValidatorStake

	delegator2StartTime := validatorStartTime.Add(1 * defaultMinStakingDuration)
	delegator2EndTime := delegator1StartTime.Add(6 * defaultMinStakingDuration)
	delegator2Stake := defaultMinValidatorStake

	delegator3StartTime := validatorStartTime.Add(2 * defaultMinStakingDuration)
	delegator3EndTime := delegator1StartTime.Add(4 * defaultMinStakingDuration)
	delegator3Stake := defaultMaxValidatorStake - validatorStake - 2*defaultMinValidatorStake

	delegator4StartTime := validatorStartTime.Add(5 * defaultMinStakingDuration)
	delegator4EndTime := delegator1StartTime.Add(7 * defaultMinStakingDuration)
	delegator4Stake := defaultMaxValidatorStake - validatorStake - defaultMinValidatorStake

	tests := []struct {
		name       string
		ap3Time    time.Time
		shouldFail bool
	}{
		{
			name:       "pre-upgrade is no longer restrictive",
			ap3Time:    validatorEndTime,
			shouldFail: false,
		},
		{
			name:       "post-upgrade calculate max stake correctly",
			ap3Time:    defaultGenesisTime,
			shouldFail: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert := assert.New(t)

			// mock ResetBlockTimer to control timing of block formation
			env := newEnvironment(t, true /*mockResetBlockTimer*/)
			env.config.ApricotPhase3Time = test.ap3Time
			defer func() {
				if err := shutdownEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()
			env.ctx.Lock.Lock()
			env.config.ApricotPhase3Time = test.ap3Time
			env.sender.SendAppGossipF = func(b []byte) error { return nil }

			key, err := testKeyFactory.NewPrivateKey()
			assert.NoError(err)

			id := key.PublicKey().Address()
			changeAddr := preFundedKeys[0].PublicKey().Address()

			// create valid tx
			addValidatorTx, err := env.txBuilder.NewAddValidatorTx(
				validatorStake,
				uint64(validatorStartTime.Unix()),
				uint64(validatorEndTime.Unix()),
				ids.NodeID(id),
				id,
				reward.PercentDenominator,
				[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the add validator tx
			err = env.BlockBuilder.AddUnverifiedTx(addValidatorTx)
			assert.NoError(err)

			// trigger block creation for the validator tx
			addValidatorBlock, err := env.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(env, assert, addValidatorBlock)

			// create valid tx
			addFirstDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
				delegator1Stake,
				uint64(delegator1StartTime.Unix()),
				uint64(delegator1EndTime.Unix()),
				ids.NodeID(id),
				preFundedKeys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the first add delegator tx
			err = env.BlockBuilder.AddUnverifiedTx(addFirstDelegatorTx)
			assert.NoError(err)

			// trigger block creation for the first add delegator tx
			addFirstDelegatorBlock, err := env.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(env, assert, addFirstDelegatorBlock)

			// create valid tx
			addSecondDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
				delegator2Stake,
				uint64(delegator2StartTime.Unix()),
				uint64(delegator2EndTime.Unix()),
				ids.NodeID(id),
				preFundedKeys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the second add delegator tx
			err = env.BlockBuilder.AddUnverifiedTx(addSecondDelegatorTx)
			assert.NoError(err)

			// trigger block creation for the second add delegator tx
			addSecondDelegatorBlock, err := env.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(env, assert, addSecondDelegatorBlock)

			// create valid tx
			addThirdDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
				delegator3Stake,
				uint64(delegator3StartTime.Unix()),
				uint64(delegator3EndTime.Unix()),
				ids.NodeID(id),
				preFundedKeys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the third add delegator tx
			err = env.BlockBuilder.AddUnverifiedTx(addThirdDelegatorTx)
			assert.NoError(err)

			// trigger block creation for the third add delegator tx
			addThirdDelegatorBlock, err := env.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(env, assert, addThirdDelegatorBlock)

			// create valid tx
			addFourthDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
				delegator4Stake,
				uint64(delegator4StartTime.Unix()),
				uint64(delegator4EndTime.Unix()),
				ids.NodeID(id),
				preFundedKeys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the fourth add delegator tx
			err = env.BlockBuilder.AddUnverifiedTx(addFourthDelegatorTx)
			assert.NoError(err)

			// trigger block creation for the fourth add delegator tx
			addFourthDelegatorBlock, err := env.BuildBlock()

			if test.shouldFail {
				assert.Error(err, "should have failed to allow new delegator")
				return
			}

			assert.NoError(err)

			verifyAndAcceptProposalCommitment(env, assert, addFourthDelegatorBlock)
		})
	}
}

func verifyAndAcceptProposalCommitment(
	env *environment,
	assert *assert.Assertions,
	blk snowman.Block,
) {
	// Verify the proposed block
	err := blk.Verify()
	assert.NoError(err)

	// Assert preferences are correct
	proposalBlk := blk.(snowman.OracleBlock)
	options, err := proposalBlk.Options()
	assert.NoError(err)

	// verify the preferences
	commit, ok := options[0].(*blockexecutor.Block)
	assert.True(ok, "expected commit block to be preferred")
	_, ok = options[0].(*blockexecutor.Block).Block.(*blocks.ApricotCommitBlock)
	assert.True(ok, "expected commit block to be preferred")

	abort, ok := options[1].(*blockexecutor.Block)
	assert.True(ok, "expected abort block to be issued")
	_, ok = options[1].(*blockexecutor.Block).Block.(*blocks.ApricotAbortBlock)
	assert.True(ok, "expected abort block to be issued")

	err = commit.Verify()
	assert.NoError(err)

	err = abort.Verify()
	assert.NoError(err)

	// Accept the proposal block and the commit block
	err = proposalBlk.Accept()
	assert.NoError(err)

	err = commit.Accept()
	assert.NoError(err)

	err = abort.Reject()
	assert.NoError(err)

	assert.NoError(env.BlockBuilder.SetPreference(commit.ID()))
}
