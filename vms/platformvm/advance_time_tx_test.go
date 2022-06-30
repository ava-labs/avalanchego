// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Ensure semantic verification fails when proposed timestamp is at or before current timestamp
func TestAdvanceTimeTxTimestampTooEarly(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	tx, err := vm.txBuilder.NewAdvanceTimeTx(defaultGenesisTime)
	if err != nil {
		t.Fatal(err)
	}

	executor := proposalTxExecutor{
		vm:          vm,
		parentState: vm.internalState,
		tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	if err == nil {
		t.Fatal("should've failed verification because proposed timestamp same as current timestamp")
	}
}

// Ensure semantic verification fails when proposed timestamp is after next validator set change time
func TestAdvanceTimeTxTimestampTooLate(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeIDKey, _ := testKeyfactory.NewPrivateKey()
	nodeID := ids.NodeID(nodeIDKey.PublicKey().Address())
	_, err := addPendingValidator(vm, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{keys[0]})
	assert.NoError(t, err)

	{
		tx, err := vm.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime.Add(1 * time.Second))
		if err != nil {
			t.Fatal(err)
		}

		executor := proposalTxExecutor{
			vm:          vm,
			parentState: vm.internalState,
			tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should've failed verification because proposed timestamp is after pending validator start time")
		}
	}
	if err := vm.Shutdown(); err != nil {
		t.Fatal(err)
	}
	vm.ctx.Lock.Unlock()

	// Case: Timestamp is after next validator end time
	vm, _, _, _ = defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// fast forward clock to 10 seconds before genesis validators stop validating
	vm.clock.Set(defaultValidateEndTime.Add(-10 * time.Second))

	{
		// Proposes advancing timestamp to 1 second after genesis validators stop validating
		tx, err := vm.txBuilder.NewAdvanceTimeTx(defaultValidateEndTime.Add(1 * time.Second))
		if err != nil {
			t.Fatal(err)
		}

		executor := proposalTxExecutor{
			vm:          vm,
			parentState: vm.internalState,
			tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should've failed verification because proposed timestamp is after pending validator start time")
		}
	}
}

// Ensure semantic verification updates the current and pending staker set
// for the primary network
func TestAdvanceTimeTxUpdatePrimaryNetworkStakers(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeIDKey, _ := testKeyfactory.NewPrivateKey()
	nodeID := ids.NodeID(nodeIDKey.PublicKey().Address())
	addPendingValidatorTx, err := addPendingValidator(vm, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{keys[0]})
	assert.NoError(t, err)

	tx, err := vm.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime)
	if err != nil {
		t.Fatal(err)
	}

	executor := proposalTxExecutor{
		vm:          vm,
		parentState: vm.internalState,
		tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	if err != nil {
		t.Fatal(err)
	}

	onCommitCurrentStakers := executor.onCommit.CurrentStakers()
	validator, err := onCommitCurrentStakers.GetValidator(nodeID)
	if err != nil {
		t.Fatal(err)
	}

	_, vdrTxID := validator.AddValidatorTx()
	if vdrTxID != addPendingValidatorTx.ID() {
		t.Fatalf("Added the wrong tx to the validator set")
	}

	onCommitPendingStakers := executor.onCommit.PendingStakers()
	if _, _, err := onCommitPendingStakers.GetValidatorTx(nodeID); err == nil {
		t.Fatalf("Should have removed the validator from the pending validator set")
	}

	_, reward, err := onCommitCurrentStakers.GetNextStaker()
	if err != nil {
		t.Fatal(err)
	}
	if reward != 1370 { // See rewards tests
		t.Fatalf("Expected reward of %d but was %d", 1370, reward)
	}

	onAbortCurrentStakers := executor.onAbort.CurrentStakers()
	if _, err := onAbortCurrentStakers.GetValidator(nodeID); err == nil {
		t.Fatalf("Shouldn't have added the validator to the validator set")
	}

	onAbortPendingStakers := executor.onAbort.PendingStakers()
	_, retrievedTxID, err := onAbortPendingStakers.GetValidatorTx(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if retrievedTxID != addPendingValidatorTx.ID() {
		t.Fatalf("Added the wrong tx to the pending validator set")
	}

	// Test VM validators
	executor.onCommit.Apply(vm.internalState)
	assert.NoError(t, vm.internalState.Commit())
	assert.True(t, vm.Validators.Contains(constants.PrimaryNetworkID, nodeID))
}

// Ensure semantic verification updates the current and pending staker sets correctly.
// Namely, it should add pending stakers whose start time is at or before the timestamp.
// It will not remove primary network stakers; that happens in rewardTxs.
func TestAdvanceTimeTxUpdateStakers(t *testing.T) {
	type stakerStatus uint
	const (
		pending stakerStatus = iota
		current
	)

	type staker struct {
		nodeID             ids.NodeID
		startTime, endTime time.Time
	}
	type test struct {
		description           string
		stakers               []staker
		subnetStakers         []staker
		advanceTimeTo         []time.Time
		expectedStakers       map[ids.NodeID]stakerStatus
		expectedSubnetStakers map[ids.NodeID]stakerStatus
	}

	// Chronological order: staker1 start, staker2 start, staker3 start and staker 4 start,
	//  staker3 and staker4 end, staker2 end and staker5 start, staker1 end
	staker1 := staker{
		nodeID:    ids.GenerateTestNodeID(),
		startTime: defaultGenesisTime.Add(1 * time.Minute),
		endTime:   defaultGenesisTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute),
	}
	staker2 := staker{
		nodeID:    ids.GenerateTestNodeID(),
		startTime: staker1.startTime.Add(1 * time.Minute),
		endTime:   staker1.startTime.Add(1 * time.Minute).Add(defaultMinStakingDuration),
	}
	staker3 := staker{
		nodeID:    ids.GenerateTestNodeID(),
		startTime: staker2.startTime.Add(1 * time.Minute),
		endTime:   staker2.endTime.Add(1 * time.Minute),
	}
	staker3Sub := staker{
		nodeID:    staker3.nodeID,
		startTime: staker3.startTime.Add(1 * time.Minute),
		endTime:   staker3.endTime.Add(-1 * time.Minute),
	}
	staker4 := staker{
		nodeID:    ids.GenerateTestNodeID(),
		startTime: staker3.startTime,
		endTime:   staker3.endTime,
	}
	staker5 := staker{
		nodeID:    ids.GenerateTestNodeID(),
		startTime: staker2.endTime,
		endTime:   staker2.endTime.Add(defaultMinStakingDuration),
	}

	tests := []test{
		{
			description:   "advance time to before staker1 start with subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime.Add(-1 * time.Second)},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: pending, staker2.nodeID: pending, staker3.nodeID: pending, staker4.nodeID: pending, staker5.nodeID: pending,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: pending, staker2.nodeID: pending, staker3.nodeID: pending, staker4.nodeID: pending, staker5.nodeID: pending,
			},
		},
		{
			description:   "advance time to staker 1 start with subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1},
			advanceTimeTo: []time.Time{staker1.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker2.nodeID: pending, staker3.nodeID: pending, staker4.nodeID: pending, staker5.nodeID: pending,
				staker1.nodeID: current,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker2.nodeID: pending, staker3.nodeID: pending, staker4.nodeID: pending, staker5.nodeID: pending,
				staker1.nodeID: current,
			},
		},
		{
			description:   "advance time to the staker2 start",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker3.nodeID: pending, staker4.nodeID: pending, staker5.nodeID: pending,
				staker1.nodeID: current, staker2.nodeID: current,
			},
		},
		{
			description:   "staker3 should validate only primary network",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3Sub, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker5.nodeID: pending,
				staker1.nodeID: current, staker2.nodeID: current, staker3.nodeID: current, staker4.nodeID: current,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker5.nodeID: pending, staker3Sub.nodeID: pending,
				staker1.nodeID: current, staker2.nodeID: current, staker4.nodeID: current,
			},
		},
		{
			description:   "advance time to staker3 start with subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3Sub, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime, staker3Sub.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker5.nodeID: pending,
				staker1.nodeID: current, staker2.nodeID: current, staker3.nodeID: current, staker4.nodeID: current,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker5.nodeID: pending,
				staker1.nodeID: current, staker2.nodeID: current, staker3.nodeID: current, staker4.nodeID: current,
			},
		},
		{
			description:   "advance time to staker5 end",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime, staker5.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current, staker2.nodeID: current, staker3.nodeID: current, staker4.nodeID: current, staker5.nodeID: current,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(ts *testing.T) {
			assert := assert.New(ts)
			vm, _, _, _ := defaultVM()
			vm.ctx.Lock.Lock()
			defer func() {
				if err := vm.Shutdown(); err != nil {
					t.Fatal(err)
				}
				vm.ctx.Lock.Unlock()
			}()
			vm.WhitelistedSubnets.Add(testSubnet1.ID())

			for _, staker := range test.stakers {
				_, err := addPendingValidator(vm, staker.startTime, staker.endTime, staker.nodeID, []*crypto.PrivateKeySECP256K1R{keys[0]})
				assert.NoError(err)
			}

			for _, staker := range test.subnetStakers {
				tx, err := vm.txBuilder.NewAddSubnetValidatorTx(
					10, // Weight
					uint64(staker.startTime.Unix()),
					uint64(staker.endTime.Unix()),
					staker.nodeID,    // validator ID
					testSubnet1.ID(), // Subnet ID
					[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]}, // Keys
					ids.ShortEmpty, // reward address
				)
				assert.NoError(err)
				vm.internalState.AddPendingStaker(tx)
				vm.internalState.AddTx(tx, status.Committed)
			}
			if err := vm.internalState.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := vm.internalState.Load(); err != nil {
				t.Fatal(err)
			}

			for _, newTime := range test.advanceTimeTo {
				vm.clock.Set(newTime)
				tx, err := vm.txBuilder.NewAdvanceTimeTx(newTime)
				if err != nil {
					t.Fatal(err)
				}

				executor := proposalTxExecutor{
					vm:          vm,
					parentState: vm.internalState,
					tx:          tx,
				}
				err = tx.Unsigned.Visit(&executor)
				if err != nil {
					t.Fatal(err)
				}

				assert.NoError(err)
				executor.onCommit.Apply(vm.internalState)
			}

			assert.NoError(vm.internalState.Commit())

			// Check that the validators we expect to be in the current staker set are there
			currentStakers := vm.internalState.CurrentStakers()
			// Check that the validators we expect to be in the pending staker set are there
			pendingStakers := vm.internalState.PendingStakers()
			for stakerNodeID, status := range test.expectedStakers {
				switch status {
				case pending:
					_, _, err := pendingStakers.GetValidatorTx(stakerNodeID)
					assert.NoError(err)
					assert.False(vm.Validators.Contains(constants.PrimaryNetworkID, stakerNodeID))
				case current:
					_, err := currentStakers.GetValidator(stakerNodeID)
					assert.NoError(err)
					assert.True(vm.Validators.Contains(constants.PrimaryNetworkID, stakerNodeID))
				}
			}

			for stakerNodeID, status := range test.expectedSubnetStakers {
				switch status {
				case pending:
					assert.False(vm.Validators.Contains(testSubnet1.ID(), stakerNodeID))
				case current:
					assert.True(vm.Validators.Contains(testSubnet1.ID(), stakerNodeID))
				}
			}
		})
	}
}

// Regression test for https://github.com/ava-labs/avalanchego/pull/584
// that ensures it fixes a bug where subnet validators are not removed
// when timestamp is advanced and there is a pending staker whose start time
// is after the new timestamp
func TestAdvanceTimeTxRemoveSubnetValidator(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	vm.WhitelistedSubnets.Add(testSubnet1.ID())
	// Add a subnet validator to the staker set
	subnetValidatorNodeID := keys[0].PublicKey().Address()
	// Starts after the corre
	subnetVdr1StartTime := defaultValidateStartTime
	subnetVdr1EndTime := defaultValidateStartTime.Add(defaultMinStakingDuration)
	tx, err := vm.txBuilder.NewAddSubnetValidatorTx(
		1,                                  // Weight
		uint64(subnetVdr1StartTime.Unix()), // Start time
		uint64(subnetVdr1EndTime.Unix()),   // end time
		ids.NodeID(subnetValidatorNodeID),  // Node ID
		testSubnet1.ID(),                   // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]}, // Keys
		ids.ShortEmpty, // reward address
	)
	if err != nil {
		t.Fatal(err)
	}

	vm.internalState.AddCurrentStaker(tx, 0)
	vm.internalState.AddTx(tx, status.Committed)
	if err := vm.internalState.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := vm.internalState.Load(); err != nil {
		t.Fatal(err)
	}

	// The above validator is now part of the staking set

	// Queue a staker that joins the staker set after the above validator leaves
	subnetVdr2NodeID := keys[1].PublicKey().Address()
	tx, err = vm.txBuilder.NewAddSubnetValidatorTx(
		1, // Weight
		uint64(subnetVdr1EndTime.Add(time.Second).Unix()),                                // Start time
		uint64(subnetVdr1EndTime.Add(time.Second).Add(defaultMinStakingDuration).Unix()), // end time
		ids.NodeID(subnetVdr2NodeID),                                                     // Node ID
		testSubnet1.ID(),                                                                 // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},                                 // Keys
		ids.ShortEmpty, // reward address
	)
	if err != nil {
		t.Fatal(err)
	}

	vm.internalState.AddPendingStaker(tx)
	vm.internalState.AddTx(tx, status.Committed)
	if err := vm.internalState.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := vm.internalState.Load(); err != nil {
		t.Fatal(err)
	}

	// The above validator is now in the pending staker set

	// Advance time to the first staker's end time.
	vm.clock.Set(subnetVdr1EndTime)
	tx, err = vm.txBuilder.NewAdvanceTimeTx(subnetVdr1EndTime)
	if err != nil {
		t.Fatal(err)
	}

	executor := proposalTxExecutor{
		vm:          vm,
		parentState: vm.internalState,
		tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	if err != nil {
		t.Fatal(err)
	}

	currentStakers := executor.onCommit.CurrentStakers()
	vdr, err := currentStakers.GetValidator(ids.NodeID(subnetValidatorNodeID))
	if err != nil {
		t.Fatal(err)
	}
	_, exists := vdr.SubnetValidators()[testSubnet1.ID()]

	// The first staker should now be removed. Verify that is the case.
	if exists {
		t.Fatal("should have been removed from validator set")
	}
	// Check VM Validators are removed successfully
	executor.onCommit.Apply(vm.internalState)
	assert.NoError(t, vm.internalState.Commit())
	assert.False(t, vm.Validators.Contains(testSubnet1.ID(), ids.NodeID(subnetVdr2NodeID)))
	assert.False(t, vm.Validators.Contains(testSubnet1.ID(), ids.NodeID(subnetValidatorNodeID)))
}

func TestWhitelistedSubnet(t *testing.T) {
	for _, whitelist := range []bool{true, false} {
		t.Run(fmt.Sprintf("whitelisted %t", whitelist), func(ts *testing.T) {
			vm, _, _, _ := defaultVM()
			vm.ctx.Lock.Lock()
			defer func() {
				if err := vm.Shutdown(); err != nil {
					t.Fatal(err)
				}
				vm.ctx.Lock.Unlock()
			}()

			if whitelist {
				vm.WhitelistedSubnets.Add(testSubnet1.ID())
			}
			// Add a subnet validator to the staker set
			subnetValidatorNodeID := keys[0].PublicKey().Address()

			subnetVdr1StartTime := defaultGenesisTime.Add(1 * time.Minute)
			subnetVdr1EndTime := defaultGenesisTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute)
			tx, err := vm.txBuilder.NewAddSubnetValidatorTx(
				1,                                  // Weight
				uint64(subnetVdr1StartTime.Unix()), // Start time
				uint64(subnetVdr1EndTime.Unix()),   // end time
				ids.NodeID(subnetValidatorNodeID),  // Node ID
				testSubnet1.ID(),                   // Subnet ID
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]}, // Keys
				ids.ShortEmpty, // reward address
			)
			if err != nil {
				t.Fatal(err)
			}

			vm.internalState.AddPendingStaker(tx)
			vm.internalState.AddTx(tx, status.Committed)
			if err := vm.internalState.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := vm.internalState.Load(); err != nil {
				t.Fatal(err)
			}

			// Advance time to the staker's start time.
			vm.clock.Set(subnetVdr1StartTime)
			tx, err = vm.txBuilder.NewAdvanceTimeTx(subnetVdr1StartTime)
			if err != nil {
				t.Fatal(err)
			}

			executor := proposalTxExecutor{
				vm:          vm,
				parentState: vm.internalState,
				tx:          tx,
			}
			err = tx.Unsigned.Visit(&executor)
			if err != nil {
				t.Fatal(err)
			}

			executor.onCommit.Apply(vm.internalState)
			assert.NoError(t, vm.internalState.Commit())
			assert.Equal(t, whitelist, vm.Validators.Contains(testSubnet1.ID(), ids.NodeID(subnetValidatorNodeID)))
		})
	}
}

func TestAdvanceTimeTxDelegatorStakerWeight(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMaxStakingDuration)
	nodeIDKey, _ := testKeyfactory.NewPrivateKey()
	nodeID := ids.NodeID(nodeIDKey.PublicKey().Address())
	_, err := addPendingValidator(vm, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{keys[0]})
	assert.NoError(t, err)

	tx, err := vm.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime)
	assert.NoError(t, err)

	executor := proposalTxExecutor{
		vm:          vm,
		parentState: vm.internalState,
		tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	executor.onCommit.Apply(vm.internalState)
	assert.NoError(t, vm.internalState.Commit())

	// Test validator weight before delegation
	primarySet, ok := vm.Validators.GetValidators(constants.PrimaryNetworkID)
	assert.True(t, ok)
	vdrWeight, _ := primarySet.GetWeight(nodeID)
	assert.Equal(t, vm.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(1 * time.Second)
	addDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		vm.MinDelegatorStake,
		uint64(pendingDelegatorStartTime.Unix()),
		uint64(pendingDelegatorEndTime.Unix()),
		nodeID,
		keys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1], keys[4]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(t, err)
	vm.internalState.AddPendingStaker(addDelegatorTx)
	vm.internalState.AddTx(addDelegatorTx, status.Committed)
	assert.NoError(t, vm.internalState.Commit())
	assert.NoError(t, vm.internalState.Load())

	// Advance Time
	tx, err = vm.txBuilder.NewAdvanceTimeTx(pendingDelegatorStartTime)
	assert.NoError(t, err)

	executor = proposalTxExecutor{
		vm:          vm,
		parentState: vm.internalState,
		tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	executor.onCommit.Apply(vm.internalState)
	assert.NoError(t, vm.internalState.Commit())

	// Test validator weight after delegation
	vdrWeight, _ = primarySet.GetWeight(nodeID)
	assert.Equal(t, vm.MinDelegatorStake+vm.MinValidatorStake, vdrWeight)
}

func TestAdvanceTimeTxDelegatorStakers(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeIDKey, _ := testKeyfactory.NewPrivateKey()
	nodeID := ids.NodeID(nodeIDKey.PublicKey().Address())
	_, err := addPendingValidator(vm, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{keys[0]})
	assert.NoError(t, err)

	tx, err := vm.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime)
	assert.NoError(t, err)

	executor := proposalTxExecutor{
		vm:          vm,
		parentState: vm.internalState,
		tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	executor.onCommit.Apply(vm.internalState)
	assert.NoError(t, vm.internalState.Commit())

	// Test validator weight before delegation
	primarySet, ok := vm.Validators.GetValidators(constants.PrimaryNetworkID)
	assert.True(t, ok)
	vdrWeight, _ := primarySet.GetWeight(nodeID)
	assert.Equal(t, vm.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(defaultMinStakingDuration)
	addDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		vm.MinDelegatorStake,
		uint64(pendingDelegatorStartTime.Unix()),
		uint64(pendingDelegatorEndTime.Unix()),
		nodeID,
		keys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1], keys[4]},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(t, err)
	vm.internalState.AddPendingStaker(addDelegatorTx)
	vm.internalState.AddTx(addDelegatorTx, status.Committed)
	assert.NoError(t, vm.internalState.Commit())
	assert.NoError(t, vm.internalState.Load())

	// Advance Time
	tx, err = vm.txBuilder.NewAdvanceTimeTx(pendingDelegatorStartTime)
	assert.NoError(t, err)

	executor = proposalTxExecutor{
		vm:          vm,
		parentState: vm.internalState,
		tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	executor.onCommit.Apply(vm.internalState)
	assert.NoError(t, vm.internalState.Commit())

	// Test validator weight after delegation
	vdrWeight, _ = primarySet.GetWeight(nodeID)
	assert.Equal(t, vm.MinDelegatorStake+vm.MinValidatorStake, vdrWeight)
}

// Test method InitiallyPrefersCommit
func TestAdvanceTimeTxInitiallyPrefersCommit(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	vm.clock.Set(defaultGenesisTime) // VM's clock reads the genesis time

	// Proposed advancing timestamp to 1 second after sync bound
	tx, err := vm.txBuilder.NewAdvanceTimeTx(defaultGenesisTime.Add(syncBound))
	if err != nil {
		t.Fatal(err)
	}

	executor := proposalTxExecutor{
		vm:          vm,
		parentState: vm.internalState,
		tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	if !executor.prefersCommit {
		t.Fatal("should prefer to commit this tx because its proposed timestamp it's within sync bound")
	}
}

// Ensure marshaling/unmarshaling works
func TestAdvanceTimeTxUnmarshal(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	tx, err := vm.txBuilder.NewAdvanceTimeTx(defaultGenesisTime)
	if err != nil {
		t.Fatal(err)
	}

	bytes, err := Codec.Marshal(txs.Version, tx)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshaledTx txs.Tx
	if _, err := Codec.Unmarshal(bytes, &unmarshaledTx); err != nil {
		t.Fatal(err)
	} else if tx.Unsigned.(*txs.AdvanceTimeTx).Time != unmarshaledTx.Unsigned.(*txs.AdvanceTimeTx).Time {
		t.Fatal("should have same timestamp")
	}
}

func addPendingValidator(
	vm *VM,
	startTime, endTime time.Time,
	nodeID ids.NodeID,
	keys []*crypto.PrivateKeySECP256K1R,
) (*txs.Tx, error) {
	addPendingValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		ids.ShortID(nodeID),
		reward.PercentDenominator,
		keys,
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		return nil, err
	}

	vm.internalState.AddPendingStaker(addPendingValidatorTx)
	vm.internalState.AddTx(addPendingValidatorTx, status.Committed)
	if err := vm.internalState.Commit(); err != nil {
		return nil, err
	}
	if err := vm.internalState.Load(); err != nil {
		return nil, err
	}
	return addPendingValidatorTx, err
}
