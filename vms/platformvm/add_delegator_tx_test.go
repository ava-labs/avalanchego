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
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestAddDelegatorTxSyntacticVerify(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	rewardAddress := keys[0].PublicKey().Address()
	nodeID := ids.NodeID(rewardAddress)

	// Case : tx is nil
	var unsignedTx *txs.AddDelegatorTx
	stx := txs.Tx{
		Unsigned: unsignedTx,
	}
	if err := stx.SyntacticVerify(vm.ctx); err == nil {
		t.Fatal("should have errored because tx is nil")
	}

	// Case: Wrong network ID
	tx, err := vm.txBuilder.NewAddDelegatorTx(
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

	addDelegatorTx := tx.Unsigned.(*txs.AddDelegatorTx)
	addDelegatorTx.NetworkID++
	// This tx was syntactically verified when it was created... pretend it
	// wasn't so we don't use cache
	addDelegatorTx.SyntacticallyVerified = false
	if err := tx.SyntacticVerify(vm.ctx); err == nil {
		t.Fatal("should have erred because the wrong network ID was used")
	}

	// Case: Valid
	if tx, err = vm.txBuilder.NewAddDelegatorTx(
		vm.MinDelegatorStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		rewardAddress,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	); err != nil {
		t.Fatal(err)
	} else if err := tx.SyntacticVerify(vm.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAddDelegatorTxExecute(t *testing.T) {
	rewardAddress := keys[0].PublicKey().Address()
	nodeID := ids.NodeID(rewardAddress)

	factory := crypto.FactorySECP256K1R{}
	keyIntf, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	newValidatorKey := keyIntf.(*crypto.PrivateKeySECP256K1R)
	newValidatorID := ids.NodeID(newValidatorKey.PublicKey().Address())
	newValidatorStartTime := uint64(defaultValidateStartTime.Add(5 * time.Second).Unix())
	newValidatorEndTime := uint64(defaultValidateEndTime.Add(-5 * time.Second).Unix())

	// [addMinStakeValidator] adds a new validator to the primary network's
	// pending validator set with the minimum staking amount
	addMinStakeValidator := func(vm *VM) {
		tx, err := vm.txBuilder.NewAddValidatorTx(
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

		staker := state.NewPrimaryNetworkStaker(tx.ID(), &tx.Unsigned.(*txs.AddValidatorTx).Validator)
		staker.PotentialReward = 0
		staker.NextTime = staker.EndTime
		staker.Priority = state.PrimaryNetworkValidatorCurrentPriority

		vm.internalState.PutCurrentValidator(staker)
		vm.internalState.AddTx(tx, status.Committed)
		if err := vm.internalState.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := vm.internalState.Load(); err != nil {
			t.Fatal(err)
		}
	}

	// [addMaxStakeValidator] adds a new validator to the primary network's
	// pending validator set with the maximum staking amount
	addMaxStakeValidator := func(vm *VM) {
		tx, err := vm.txBuilder.NewAddValidatorTx(
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

		staker := state.NewPrimaryNetworkStaker(tx.ID(), &tx.Unsigned.(*txs.AddValidatorTx).Validator)
		staker.PotentialReward = 0
		staker.NextTime = staker.EndTime
		staker.Priority = state.PrimaryNetworkValidatorCurrentPriority

		vm.internalState.PutCurrentValidator(staker)
		vm.internalState.AddTx(tx, status.Committed)
		if err := vm.internalState.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := vm.internalState.Load(); err != nil {
			t.Fatal(err)
		}
	}

	freshVM, _, _, _ := defaultVM()
	currentTimestamp := freshVM.internalState.GetTimestamp()

	type test struct {
		stakeAmount   uint64
		startTime     uint64
		endTime       uint64
		nodeID        ids.NodeID
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
			vm, _, _, _ := defaultVM()
			vm.ApricotPhase3Time = tt.AP3Time

			vm.ctx.Lock.Lock()
			defer func() {
				if err := vm.Shutdown(); err != nil {
					t.Fatal(err)
				}
				vm.ctx.Lock.Unlock()
			}()

			tx, err := vm.txBuilder.NewAddDelegatorTx(
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

			executor := proposalTxExecutor{
				vm:            vm,
				parentID:      vm.preferred,
				stateVersions: vm.stateVersions,
				tx:            tx,
			}
			err = tx.Unsigned.Visit(&executor)
			if err != nil && !tt.shouldErr {
				t.Fatalf("shouldn't have errored but got %s", err)
			} else if err == nil && tt.shouldErr {
				t.Fatalf("expected test to error but got none")
			}
		})
	}
}

func TestAddDelegatorTxOverDelegatedRegression(t *testing.T) {
	assert := assert.New(t)
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		assert.NoError(vm.Shutdown())
		vm.ctx.Lock.Unlock()
	}()

	validatorStartTime := defaultGenesisTime.Add(syncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	key, err := testKeyfactory.NewPrivateKey()
	assert.NoError(err)

	id := key.PublicKey().Address()

	// create valid tx
	addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(validatorStartTime.Unix()),
		uint64(validatorEndTime.Unix()),
		ids.NodeID(id),
		id,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	assert.NoError(vm.blockBuilder.AddUnverifiedTx(addValidatorTx))

	addValidatorBlock, err := vm.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, vm, addValidatorBlock)

	vm.clock.Set(validatorStartTime)

	firstAdvanceTimeBlock, err := vm.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, vm, firstAdvanceTimeBlock)

	firstDelegatorStartTime := validatorStartTime.Add(syncBound).Add(1 * time.Second)
	firstDelegatorEndTime := firstDelegatorStartTime.Add(vm.MinStakeDuration)

	// create valid tx
	addFirstDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		4*vm.MinValidatorStake, // maximum amount of stake this delegator can provide
		uint64(firstDelegatorStartTime.Unix()),
		uint64(firstDelegatorEndTime.Unix()),
		ids.NodeID(id),
		keys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	assert.NoError(vm.blockBuilder.AddUnverifiedTx(addFirstDelegatorTx))

	addFirstDelegatorBlock, err := vm.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, vm, addFirstDelegatorBlock)

	vm.clock.Set(firstDelegatorStartTime)

	secondAdvanceTimeBlock, err := vm.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, vm, secondAdvanceTimeBlock)

	secondDelegatorStartTime := firstDelegatorEndTime.Add(2 * time.Second)
	secondDelegatorEndTime := secondDelegatorStartTime.Add(vm.MinStakeDuration)

	vm.clock.Set(secondDelegatorStartTime.Add(-10 * syncBound))

	// create valid tx
	addSecondDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		vm.MinDelegatorStake,
		uint64(secondDelegatorStartTime.Unix()),
		uint64(secondDelegatorEndTime.Unix()),
		ids.NodeID(id),
		keys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1], keys[3]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	// trigger block creation
	assert.NoError(vm.blockBuilder.AddUnverifiedTx(addSecondDelegatorTx))

	addSecondDelegatorBlock, err := vm.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, vm, addSecondDelegatorBlock)

	thirdDelegatorStartTime := firstDelegatorEndTime.Add(-time.Second)
	thirdDelegatorEndTime := thirdDelegatorStartTime.Add(vm.MinStakeDuration)

	// create valid tx
	addThirdDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		vm.MinDelegatorStake,
		uint64(thirdDelegatorStartTime.Unix()),
		uint64(thirdDelegatorEndTime.Unix()),
		ids.NodeID(id),
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
			vm, _, _, _ := defaultVM()
			vm.ApricotPhase3Time = test.ap3Time

			vm.ctx.Lock.Lock()
			defer func() {
				assert.NoError(vm.Shutdown())
				vm.ctx.Lock.Unlock()
			}()

			key, err := testKeyfactory.NewPrivateKey()
			assert.NoError(err)

			id := key.PublicKey().Address()
			changeAddr := keys[0].PublicKey().Address()

			// create valid tx
			addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
				validatorStake,
				uint64(validatorStartTime.Unix()),
				uint64(validatorEndTime.Unix()),
				ids.NodeID(id),
				id,
				reward.PercentDenominator,
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the add validator tx
			assert.NoError(vm.blockBuilder.AddUnverifiedTx(addValidatorTx))

			// trigger block creation for the validator tx
			addValidatorBlock, err := vm.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, vm, addValidatorBlock)

			// create valid tx
			addFirstDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
				delegator1Stake,
				uint64(delegator1StartTime.Unix()),
				uint64(delegator1EndTime.Unix()),
				ids.NodeID(id),
				keys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the first add delegator tx
			assert.NoError(vm.blockBuilder.AddUnverifiedTx(addFirstDelegatorTx))

			// trigger block creation for the first add delegator tx
			addFirstDelegatorBlock, err := vm.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, vm, addFirstDelegatorBlock)

			// create valid tx
			addSecondDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
				delegator2Stake,
				uint64(delegator2StartTime.Unix()),
				uint64(delegator2EndTime.Unix()),
				ids.NodeID(id),
				keys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the second add delegator tx
			assert.NoError(vm.blockBuilder.AddUnverifiedTx(addSecondDelegatorTx))

			// trigger block creation for the second add delegator tx
			addSecondDelegatorBlock, err := vm.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, vm, addSecondDelegatorBlock)

			// create valid tx
			addThirdDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
				delegator3Stake,
				uint64(delegator3StartTime.Unix()),
				uint64(delegator3EndTime.Unix()),
				ids.NodeID(id),
				keys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the third add delegator tx
			assert.NoError(vm.blockBuilder.AddUnverifiedTx(addThirdDelegatorTx))

			// trigger block creation for the third add delegator tx
			addThirdDelegatorBlock, err := vm.BuildBlock()
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, vm, addThirdDelegatorBlock)

			// create valid tx
			addFourthDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
				delegator4Stake,
				uint64(delegator4StartTime.Unix()),
				uint64(delegator4EndTime.Unix()),
				ids.NodeID(id),
				keys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			assert.NoError(err)

			// issue the fourth add delegator tx
			assert.NoError(vm.blockBuilder.AddUnverifiedTx(addFourthDelegatorTx))

			// trigger block creation for the fourth add delegator tx
			addFourthDelegatorBlock, err := vm.BuildBlock()

			if test.shouldFail {
				assert.Error(err, "should have failed to allow new delegator")
				return
			}
			assert.NoError(err)

			verifyAndAcceptProposalCommitment(assert, vm, addFourthDelegatorBlock)
		})
	}
}

func TestAddDelegatorTxAddBeforeRemove(t *testing.T) {
	assert := assert.New(t)
	validatorStartTime := defaultGenesisTime.Add(syncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)
	validatorStake := defaultMaxValidatorStake / 5

	delegator1StartTime := validatorStartTime
	delegator1EndTime := delegator1StartTime.Add(3 * defaultMinStakingDuration)
	delegator1Stake := defaultMaxValidatorStake - validatorStake

	delegator2StartTime := delegator1EndTime
	delegator2EndTime := delegator2StartTime.Add(3 * defaultMinStakingDuration)
	delegator2Stake := defaultMaxValidatorStake - validatorStake

	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		assert.NoError(vm.Shutdown())
		vm.ctx.Lock.Unlock()
	}()

	nodeID := ids.GenerateTestNodeID()
	rewardAddress := ids.GenerateTestShortID()
	changeAddr := keys[0].PublicKey().Address()

	// create valid tx
	addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
		validatorStake,
		uint64(validatorStartTime.Unix()),
		uint64(validatorEndTime.Unix()),
		nodeID,
		rewardAddress,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
		changeAddr,
	)
	assert.NoError(err)

	// issue the add validator tx
	assert.NoError(vm.blockBuilder.AddUnverifiedTx(addValidatorTx))

	// trigger block creation for the validator tx
	addValidatorBlock, err := vm.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, vm, addValidatorBlock)

	// create valid tx
	addFirstDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		delegator1Stake,
		uint64(delegator1StartTime.Unix()),
		uint64(delegator1EndTime.Unix()),
		nodeID,
		rewardAddress,
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
		changeAddr,
	)
	assert.NoError(err)

	// issue the first add delegator tx
	assert.NoError(vm.blockBuilder.AddUnverifiedTx(addFirstDelegatorTx))

	// trigger block creation for the first add delegator tx
	addFirstDelegatorBlock, err := vm.BuildBlock()
	assert.NoError(err)

	verifyAndAcceptProposalCommitment(assert, vm, addFirstDelegatorBlock)

	// create valid tx
	addSecondDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		delegator2Stake,
		uint64(delegator2StartTime.Unix()),
		uint64(delegator2EndTime.Unix()),
		nodeID,
		rewardAddress,
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
		changeAddr,
	)
	assert.NoError(err)

	// attempt to issue the second add delegator tx
	assert.ErrorIs(vm.blockBuilder.AddUnverifiedTx(addSecondDelegatorTx), errOverDelegated)
}

func verifyAndAcceptProposalCommitment(assert *assert.Assertions, vm *VM, blk snowman.Block) {
	// Verify the proposed block
	assert.NoError(blk.Verify())

	// Assert preferences are correct
	proposalBlk := blk.(*ProposalBlock)
	options, err := proposalBlk.Options()
	assert.NoError(err)

	// verify the preferences
	commit := options[0].(*CommitBlock)
	abort := options[1].(*AbortBlock)

	assert.NoError(commit.Verify())
	assert.NoError(abort.Verify())

	// Accept the proposal block and the commit block
	assert.NoError(proposalBlk.Accept())
	assert.NoError(commit.Accept())
	assert.NoError(abort.Reject())
	assert.NoError(vm.SetPreference(vm.lastAcceptedID))
}
