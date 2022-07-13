// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/stretchr/testify/assert"
)

func TestAddDelegatorTxOverDelegatedRegression(t *testing.T) {
	assert := assert.New(t)

	h := newTestHelpersCollection(t, false /*mockResetBlockTimer*/)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.ctx.Lock.Lock()
	h.sender.SendAppGossipF = func(b []byte) error { return nil }

	validatorStartTime := defaultGenesisTime.Add(executor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	key, err := testKeyFactory.NewPrivateKey()
	assert.NoError(err)

	id := key.PublicKey().Address()

	// create valid tx
	addValidatorTx, err := h.txBuilder.NewAddValidatorTx(
		h.cfg.MinValidatorStake,
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
	err = h.BlockBuilder.AddUnverifiedTx(addValidatorTx)
	assert.NoError(err)

	addValidatorBlock, err := h.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, addValidatorBlock)

	h.clk.Set(validatorStartTime)

	firstAdvanceTimeBlock, err := h.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, firstAdvanceTimeBlock)

	firstDelegatorStartTime := validatorStartTime.Add(executor.SyncBound).Add(1 * time.Second)
	firstDelegatorEndTime := firstDelegatorStartTime.Add(h.cfg.MinStakeDuration)

	// create valid tx
	addFirstDelegatorTx, err := h.txBuilder.NewAddDelegatorTx(
		4*h.cfg.MinValidatorStake, // maximum amount of stake this delegator can provide
		uint64(firstDelegatorStartTime.Unix()),
		uint64(firstDelegatorEndTime.Unix()),
		ids.NodeID(id),
		preFundedKeys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	err = h.BlockBuilder.AddUnverifiedTx(addFirstDelegatorTx)
	assert.NoError(err)

	addFirstDelegatorBlock, err := h.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, addFirstDelegatorBlock)

	h.clk.Set(firstDelegatorStartTime)

	secondAdvanceTimeBlock, err := h.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, secondAdvanceTimeBlock)

	secondDelegatorStartTime := firstDelegatorEndTime.Add(2 * time.Second)
	secondDelegatorEndTime := secondDelegatorStartTime.Add(h.cfg.MinStakeDuration)

	h.clk.Set(secondDelegatorStartTime.Add(-10 * executor.SyncBound))

	// create valid tx
	addSecondDelegatorTx, err := h.txBuilder.NewAddDelegatorTx(
		h.cfg.MinDelegatorStake,
		uint64(secondDelegatorStartTime.Unix()),
		uint64(secondDelegatorEndTime.Unix()),
		ids.NodeID(id),
		preFundedKeys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1], preFundedKeys[3]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	err = h.BlockBuilder.AddUnverifiedTx(addSecondDelegatorTx)
	assert.NoError(err)

	addSecondDelegatorBlock, err := h.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, addSecondDelegatorBlock)

	thirdDelegatorStartTime := firstDelegatorEndTime.Add(-time.Second)
	thirdDelegatorEndTime := thirdDelegatorStartTime.Add(h.cfg.MinStakeDuration)

	// create valid tx
	addThirdDelegatorTx, err := h.txBuilder.NewAddDelegatorTx(
		h.cfg.MinDelegatorStake,
		uint64(thirdDelegatorStartTime.Unix()),
		uint64(thirdDelegatorEndTime.Unix()),
		ids.NodeID(id),
		preFundedKeys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1], preFundedKeys[4]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	err = h.BlockBuilder.AddUnverifiedTx(addThirdDelegatorTx)
	assert.Error(err, "should have marked the delegator as being over delegated")
}

func TestAddDelegatorTxHeapCorruption(t *testing.T) {
	validatorStartTime := defaultGenesisTime.Add(executor.SyncBound).Add(1 * time.Second)
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

			h := newTestHelpersCollection(t, false /*mockResetBlockTimer*/)
			h.cfg.ApricotPhase3Time = test.ap3Time
			defer func() {
				if err := internalStateShutdown(h); err != nil {
					t.Fatal(err)
				}
			}()
			h.ctx.Lock.Lock()
			h.cfg.ApricotPhase3Time = test.ap3Time
			h.sender.SendAppGossipF = func(b []byte) error { return nil }

			key, err := testKeyFactory.NewPrivateKey()
			assert.NoError(err)

			id := key.PublicKey().Address()
			changeAddr := preFundedKeys[0].PublicKey().Address()

			// create valid tx
			addValidatorTx, err := h.txBuilder.NewAddValidatorTx(
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
			err = h.BlockBuilder.AddUnverifiedTx(addValidatorTx)
			assert.NoError(err)

			// trigger block creation for the validator tx
			addValidatorBlock, err := h.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, addValidatorBlock)

			// create valid tx
			addFirstDelegatorTx, err := h.txBuilder.NewAddDelegatorTx(
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
			err = h.BlockBuilder.AddUnverifiedTx(addFirstDelegatorTx)
			assert.NoError(err)

			// trigger block creation for the first add delegator tx
			addFirstDelegatorBlock, err := h.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, addFirstDelegatorBlock)

			// create valid tx
			addSecondDelegatorTx, err := h.txBuilder.NewAddDelegatorTx(
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
			err = h.BlockBuilder.AddUnverifiedTx(addSecondDelegatorTx)
			assert.NoError(err)

			// trigger block creation for the second add delegator tx
			addSecondDelegatorBlock, err := h.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, addSecondDelegatorBlock)

			// create valid tx
			addThirdDelegatorTx, err := h.txBuilder.NewAddDelegatorTx(
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
			err = h.BlockBuilder.AddUnverifiedTx(addThirdDelegatorTx)
			assert.NoError(err)

			// trigger block creation for the third add delegator tx
			addThirdDelegatorBlock, err := h.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, addThirdDelegatorBlock)

			// create valid tx
			addFourthDelegatorTx, err := h.txBuilder.NewAddDelegatorTx(
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
			err = h.BlockBuilder.AddUnverifiedTx(addFourthDelegatorTx)
			assert.NoError(err)

			// trigger block creation for the fourth add delegator tx
			addFourthDelegatorBlock, err := h.BuildBlock()

			if test.shouldFail {
				assert.Error(err, "should have failed to allow new delegator")
				return
			}

			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, addFourthDelegatorBlock)
		})
	}
}

func verifyAndAcceptProposalCommitment(assert *assert.Assertions, blk snowman.Block) {
	// Verify the proposed block
	err := blk.Verify()
	assert.NoError(err)

	// Assert preferences are correct
	proposalBlk := blk.(snowman.OracleBlock)
	options, err := proposalBlk.Options()
	assert.NoError(err)

	// verify the preferences
	commit, ok := options[0].(*stateful.Block)
	assert.True(ok, "expected commit block to be preferred")
	_, ok = options[0].(*stateful.Block).Block.(*stateless.CommitBlock)
	assert.True(ok, "expected commit block to be preferred")

	abort, ok := options[1].(*stateful.Block)
	assert.True(ok, "expected abort block to be issued")
	_, ok = options[1].(*stateful.Block).Block.(*stateless.AbortBlock)
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
}
