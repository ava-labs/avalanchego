// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

func TestAddDelegatorTxSyntacticVerify(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	nodeID := keys[0].PublicKey().Address()
	rewardAddress := nodeID

	// Case : tx is nil
	var unsignedTx *UnsignedAddDelegatorTx
	if err := unsignedTx.SyntacticVerify(vm.ctx); err == nil {
		t.Fatal("should have errored because tx is nil")
	}

	// Case: Wrong network ID
	tx, err := vm.newAddDelegatorTx(
		vm.MinDelegatorStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		rewardAddress,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.UnsignedTx.(*UnsignedAddDelegatorTx).NetworkID++
	// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
	tx.UnsignedTx.(*UnsignedAddDelegatorTx).syntacticallyVerified = false
	if err := tx.UnsignedTx.(*UnsignedAddDelegatorTx).SyntacticVerify(vm.ctx); err == nil {
		t.Fatal("should have errored because the wrong network ID was used")
	}

	// Case: Valid
	if tx, err = vm.newAddDelegatorTx(
		vm.MinDelegatorStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		rewardAddress,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	); err != nil {
		t.Fatal(err)
	} else if err := tx.UnsignedTx.(*UnsignedAddDelegatorTx).SyntacticVerify(vm.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAddDelegatorTxExecute(t *testing.T) {
	nodeID := keys[0].PublicKey().Address()
	rewardAddress := nodeID

	factory := crypto.FactorySECP256K1R{}
	keyIntf, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	newValidatorKey := keyIntf.(*crypto.PrivateKeySECP256K1R)
	newValidatorID := newValidatorKey.PublicKey().Address()
	newValidatorStartTime := uint64(defaultValidateStartTime.Add(5 * time.Second).Unix())
	newValidatorEndTime := uint64(defaultValidateEndTime.Add(-5 * time.Second).Unix())

	// [addMinStakeValidator] adds a new validator to the primary network's
	// pending validator set with the minimum staking amount
	addMinStakeValidator := func(vm *VM) {
		tx, err := vm.newAddValidatorTx(
			vm.MinValidatorStake,                    // stake amount
			newValidatorStartTime,                   // start time
			newValidatorEndTime,                     // end time
			newValidatorID,                          // node ID
			rewardAddress,                           // Reward Address
			reward.PercentDenominator,               // subnet
			[]*crypto.PrivateKeySECP256K1R{keys[0]}, // key
			ids.ShortEmpty,                          // change addr
		)
		if err != nil {
			t.Fatal(err)
		}

		vm.internalState.AddCurrentStaker(tx, 0)
		vm.internalState.AddTx(tx, status.Committed)
		if err := vm.internalState.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := vm.internalState.(*internalStateImpl).loadCurrentValidators(); err != nil {
			t.Fatal(err)
		}
	}

	// [addMaxStakeValidator] adds a new validator to the primary network's
	// pending validator set with the maximum staking amount
	addMaxStakeValidator := func(vm *VM) {
		tx, err := vm.newAddValidatorTx(
			vm.MaxValidatorStake,                    // stake amount
			newValidatorStartTime,                   // start time
			newValidatorEndTime,                     // end time
			newValidatorID,                          // node ID
			rewardAddress,                           // Reward Address
			reward.PercentDenominator,               // subnet
			[]*crypto.PrivateKeySECP256K1R{keys[0]}, // key
			ids.ShortEmpty,                          // change addr
		)
		if err != nil {
			t.Fatal(err)
		}

		vm.internalState.AddCurrentStaker(tx, 0)
		vm.internalState.AddTx(tx, status.Committed)
		if err := vm.internalState.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := vm.internalState.(*internalStateImpl).loadCurrentValidators(); err != nil {
			t.Fatal(err)
		}
	}

	freshVM, _, _ := defaultVM()
	currentTimestamp := freshVM.internalState.GetTimestamp()

	type test struct {
		stakeAmount   uint64
		startTime     uint64
		endTime       uint64
		nodeID        ids.ShortID
		rewardAddress ids.ShortID
		feeKeys       []*crypto.PrivateKeySECP256K1R
		setup         func(vm *VM)
		AP3Time       time.Time
		shouldErr     bool
		description   string
	}

	tests := []test{
		{
			stakeAmount:   freshVM.MinDelegatorStake,
			startTime:     uint64(defaultValidateStartTime.Unix()),
			endTime:       uint64(defaultValidateEndTime.Unix()) + 1,
			nodeID:        nodeID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{keys[0]},
			setup:         nil,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "validator stops validating primary network earlier than subnet",
		},
		{
			stakeAmount:   freshVM.MinDelegatorStake,
			startTime:     uint64(currentTimestamp.Add(maxFutureStartTime + time.Second).Unix()),
			endTime:       uint64(currentTimestamp.Add(maxFutureStartTime * 2).Unix()),
			nodeID:        nodeID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{keys[0]},
			setup:         nil,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   fmt.Sprintf("validator should not be added more than (%s) in the future", maxFutureStartTime),
		},
		{
			stakeAmount:   freshVM.MinDelegatorStake,
			startTime:     uint64(defaultValidateStartTime.Unix()),
			endTime:       uint64(defaultValidateEndTime.Unix()) + 1,
			nodeID:        nodeID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{keys[0]},
			setup:         nil,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "end time is after the primary network end time",
		},
		{
			stakeAmount:   freshVM.MinDelegatorStake,
			startTime:     uint64(defaultValidateStartTime.Add(5 * time.Second).Unix()),
			endTime:       uint64(defaultValidateEndTime.Add(-5 * time.Second).Unix()),
			nodeID:        newValidatorID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{keys[0]},
			setup:         nil,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "validator not in the current or pending validator sets of the subnet",
		},
		{
			stakeAmount:   freshVM.MinDelegatorStake,
			startTime:     newValidatorStartTime - 1, // start validating subnet before primary network
			endTime:       newValidatorEndTime,
			nodeID:        newValidatorID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{keys[0]},
			setup:         addMinStakeValidator,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "validator starts validating subnet before primary network",
		},
		{
			stakeAmount:   freshVM.MinDelegatorStake,
			startTime:     newValidatorStartTime,
			endTime:       newValidatorEndTime + 1, // stop validating subnet after stopping validating primary network
			nodeID:        newValidatorID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{keys[0]},
			setup:         addMinStakeValidator,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "validator stops validating primary network before subnet",
		},
		{
			stakeAmount:   freshVM.MinDelegatorStake,
			startTime:     newValidatorStartTime, // same start time as for primary network
			endTime:       newValidatorEndTime,   // same end time as for primary network
			nodeID:        newValidatorID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{keys[0]},
			setup:         addMinStakeValidator,
			AP3Time:       defaultGenesisTime,
			shouldErr:     false,
			description:   "valid",
		},
		{
			stakeAmount:   freshVM.MinDelegatorStake, // weight
			startTime:     uint64(currentTimestamp.Unix()),
			endTime:       uint64(defaultValidateEndTime.Unix()),
			nodeID:        nodeID,                                  // node ID
			rewardAddress: rewardAddress,                           // Reward Address
			feeKeys:       []*crypto.PrivateKeySECP256K1R{keys[0]}, // tx fee payer
			setup:         nil,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "starts validating at current timestamp",
		},
		{
			stakeAmount:   freshVM.MinDelegatorStake,               // weight
			startTime:     uint64(defaultValidateStartTime.Unix()), // start time
			endTime:       uint64(defaultValidateEndTime.Unix()),   // end time
			nodeID:        nodeID,                                  // node ID
			rewardAddress: rewardAddress,                           // Reward Address
			feeKeys:       []*crypto.PrivateKeySECP256K1R{keys[1]}, // tx fee payer
			setup: func(vm *VM) { // Remove all UTXOs owned by keys[1]
				utxoIDs, err := vm.internalState.UTXOIDs(keys[1].PublicKey().Address().Bytes(), ids.Empty, math.MaxInt32)
				if err != nil {
					t.Fatal(err)
				}
				for _, utxoID := range utxoIDs {
					vm.internalState.DeleteUTXO(utxoID)
				}
				if err := vm.internalState.Commit(); err != nil {
					t.Fatal(err)
				}
			},
			AP3Time:     defaultGenesisTime,
			shouldErr:   true,
			description: "tx fee paying key has no funds",
		},
		{
			stakeAmount:   freshVM.MinDelegatorStake,
			startTime:     newValidatorStartTime, // same start time as for primary network
			endTime:       newValidatorEndTime,   // same end time as for primary network
			nodeID:        newValidatorID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{keys[0]},
			setup:         addMaxStakeValidator,
			AP3Time:       defaultValidateEndTime,
			shouldErr:     false,
			description:   "over delegation before AP3",
		},
		{
			stakeAmount:   freshVM.MinDelegatorStake,
			startTime:     newValidatorStartTime, // same start time as for primary network
			endTime:       newValidatorEndTime,   // same end time as for primary network
			nodeID:        newValidatorID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{keys[0]},
			setup:         addMaxStakeValidator,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "over delegation after AP3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			vm, _, _ := defaultVM()
			vm.ApricotPhase3Time = tt.AP3Time

			vm.ctx.Lock.Lock()
			defer func() {
				if err := vm.Shutdown(); err != nil {
					t.Fatal(err)
				}
				vm.ctx.Lock.Unlock()
			}()

			tx, err := vm.newAddDelegatorTx(
				tt.stakeAmount,
				tt.startTime,
				tt.endTime,
				tt.nodeID,
				tt.rewardAddress,
				tt.feeKeys,
				ids.ShortEmpty, // change addr
			)
			if err != nil {
				t.Fatalf("couldn't build tx: %s", err)
			}
			if tt.setup != nil {
				tt.setup(vm)
			}
			if _, _, err := tx.UnsignedTx.(UnsignedProposalTx).Execute(vm, vm.internalState, tx); err != nil && !tt.shouldErr {
				t.Fatalf("shouldn't have errored but got %s", err)
			} else if err == nil && tt.shouldErr {
				t.Fatalf("expected test to error but got none")
			}
		})
	}
}

func TestAddDelegatorTxOverDelegatedRegression(t *testing.T) {
	assert := assert.New(t)

	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	validatorStartTime := defaultGenesisTime.Add(syncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	key, err := vm.factory.NewPrivateKey()
	assert.NoError(err)

	id := key.PublicKey().Address()

	// create valid tx
	addValidatorTx, err := vm.newAddValidatorTx(
		vm.MinValidatorStake,
		uint64(validatorStartTime.Unix()),
		uint64(validatorEndTime.Unix()),
		id,
		id,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	err = vm.blockBuilder.AddUnverifiedTx(addValidatorTx)
	assert.NoError(err)

	addValidatorBlock, err := vm.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, addValidatorBlock)

	vm.clock.Set(validatorStartTime)

	firstAdvanceTimeBlock, err := vm.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, firstAdvanceTimeBlock)

	firstDelegatorStartTime := validatorStartTime.Add(syncBound).Add(1 * time.Second)
	firstDelegatorEndTime := firstDelegatorStartTime.Add(vm.MinStakeDuration)

	// create valid tx
	addFirstDelegatorTx, err := vm.newAddDelegatorTx(
		4*vm.MinValidatorStake, // maximum amount of stake this delegator can provide
		uint64(firstDelegatorStartTime.Unix()),
		uint64(firstDelegatorEndTime.Unix()),
		id,
		keys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	err = vm.blockBuilder.AddUnverifiedTx(addFirstDelegatorTx)
	assert.NoError(err)

	addFirstDelegatorBlock, err := vm.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, addFirstDelegatorBlock)

	vm.clock.Set(firstDelegatorStartTime)

	secondAdvanceTimeBlock, err := vm.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, secondAdvanceTimeBlock)

	secondDelegatorStartTime := firstDelegatorEndTime.Add(2 * time.Second)
	secondDelegatorEndTime := secondDelegatorStartTime.Add(vm.MinStakeDuration)

	vm.clock.Set(secondDelegatorStartTime.Add(-10 * syncBound))

	// create valid tx
	addSecondDelegatorTx, err := vm.newAddDelegatorTx(
		vm.MinDelegatorStake,
		uint64(secondDelegatorStartTime.Unix()),
		uint64(secondDelegatorEndTime.Unix()),
		id,
		keys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1], keys[3]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	err = vm.blockBuilder.AddUnverifiedTx(addSecondDelegatorTx)
	assert.NoError(err)

	addSecondDelegatorBlock, err := vm.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, addSecondDelegatorBlock)

	thirdDelegatorStartTime := firstDelegatorEndTime.Add(-time.Second)
	thirdDelegatorEndTime := thirdDelegatorStartTime.Add(vm.MinStakeDuration)

	// create valid tx
	addThirdDelegatorTx, err := vm.newAddDelegatorTx(
		vm.MinDelegatorStake,
		uint64(thirdDelegatorStartTime.Unix()),
		uint64(thirdDelegatorEndTime.Unix()),
		id,
		keys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1], keys[4]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	err = vm.blockBuilder.AddUnverifiedTx(addThirdDelegatorTx)
	assert.Error(err, "should have marked the delegator as being over delegated")
}

func TestAddDelegatorTxHeapCorruption(t *testing.T) {
	validatorStartTime := defaultGenesisTime.Add(syncBound).Add(1 * time.Second)
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

			vm, _, _ := defaultVM()
			vm.ApricotPhase3Time = test.ap3Time

			vm.ctx.Lock.Lock()
			defer func() {
				err := vm.Shutdown()
				assert.NoError(err)

				vm.ctx.Lock.Unlock()
			}()

			key, err := vm.factory.NewPrivateKey()
			assert.NoError(err)

			id := key.PublicKey().Address()
			changeAddr := keys[0].PublicKey().Address()

			// create valid tx
			addValidatorTx, err := vm.newAddValidatorTx(
				validatorStake,
				uint64(validatorStartTime.Unix()),
				uint64(validatorEndTime.Unix()),
				id,
				id,
				reward.PercentDenominator,
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the add validator tx
			err = vm.blockBuilder.AddUnverifiedTx(addValidatorTx)
			assert.NoError(err)

			// trigger block creation for the validator tx
			addValidatorBlock, err := vm.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, addValidatorBlock)

			// create valid tx
			addFirstDelegatorTx, err := vm.newAddDelegatorTx(
				delegator1Stake,
				uint64(delegator1StartTime.Unix()),
				uint64(delegator1EndTime.Unix()),
				id,
				keys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the first add delegator tx
			err = vm.blockBuilder.AddUnverifiedTx(addFirstDelegatorTx)
			assert.NoError(err)

			// trigger block creation for the first add delegator tx
			addFirstDelegatorBlock, err := vm.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, addFirstDelegatorBlock)

			// create valid tx
			addSecondDelegatorTx, err := vm.newAddDelegatorTx(
				delegator2Stake,
				uint64(delegator2StartTime.Unix()),
				uint64(delegator2EndTime.Unix()),
				id,
				keys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the second add delegator tx
			err = vm.blockBuilder.AddUnverifiedTx(addSecondDelegatorTx)
			assert.NoError(err)

			// trigger block creation for the second add delegator tx
			addSecondDelegatorBlock, err := vm.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, addSecondDelegatorBlock)

			// create valid tx
			addThirdDelegatorTx, err := vm.newAddDelegatorTx(
				delegator3Stake,
				uint64(delegator3StartTime.Unix()),
				uint64(delegator3EndTime.Unix()),
				id,
				keys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the third add delegator tx
			err = vm.blockBuilder.AddUnverifiedTx(addThirdDelegatorTx)
			assert.NoError(err)

			// trigger block creation for the third add delegator tx
			addThirdDelegatorBlock, err := vm.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, addThirdDelegatorBlock)

			// create valid tx
			addFourthDelegatorTx, err := vm.newAddDelegatorTx(
				delegator4Stake,
				uint64(delegator4StartTime.Unix()),
				uint64(delegator4EndTime.Unix()),
				id,
				keys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the fourth add delegator tx
			err = vm.blockBuilder.AddUnverifiedTx(addFourthDelegatorTx)
			assert.NoError(err)

			// trigger block creation for the fourth add delegator tx
			addFourthDelegatorBlock, err := vm.BuildBlock()

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
	proposalBlk := blk.(*ProposalBlock)
	options, err := proposalBlk.Options()
	assert.NoError(err)

	// verify the preferences
	commit, ok := options[0].(*CommitBlock)
	assert.True(ok, "expected commit block to be preferred")

	abort, ok := options[1].(*AbortBlock)
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
