// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

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
	env := newEnvironment()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	tx, err := env.txBuilder.NewAdvanceTimeTx(defaultGenesisTime)
	if err != nil {
		t.Fatal(err)
	}

	executor := ProposalTxExecutor{
		Backend:     &env.backend,
		ParentState: env.state,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	if err == nil {
		t.Fatal("should've failed verification because proposed timestamp same as current timestamp")
	}
}

// Ensure semantic verification fails when proposed timestamp is after next validator set change time
func TestAdvanceTimeTxTimestampTooLate(t *testing.T) {
	env := newEnvironment()

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	_, err := addPendingValidator(env, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{preFundedKeys[0]})
	assert.NoError(t, err)

	{
		tx, err := env.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime.Add(1 * time.Second))
		if err != nil {
			t.Fatal(err)
		}

		executor := ProposalTxExecutor{
			Backend:     &env.backend,
			ParentState: env.state,
			Tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should've failed verification because proposed timestamp is after pending validator start time")
		}
	}

	if err := shutdownEnvironment(env); err != nil {
		t.Fatal(err)
	}

	// Case: Timestamp is after next validator end time
	env = newEnvironment()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	// fast forward clock to 10 seconds before genesis validators stop validating
	env.clk.Set(defaultValidateEndTime.Add(-10 * time.Second))

	{
		// Proposes advancing timestamp to 1 second after genesis validators stop validating
		tx, err := env.txBuilder.NewAdvanceTimeTx(defaultValidateEndTime.Add(1 * time.Second))
		if err != nil {
			t.Fatal(err)
		}

		executor := ProposalTxExecutor{
			Backend:     &env.backend,
			ParentState: env.state,
			Tx:          tx,
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
	env := newEnvironment()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	dummyHeight := uint64(1)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	addPendingValidatorTx, err := addPendingValidator(env, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{preFundedKeys[0]})
	assert.NoError(t, err)

	tx, err := env.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime)
	if err != nil {
		t.Fatal(err)
	}

	executor := ProposalTxExecutor{
		Backend:     &env.backend,
		ParentState: env.state,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	if err != nil {
		t.Fatal(err)
	}

	onCommitCurrentStakers := executor.OnCommit.CurrentStakers()
	validator, err := onCommitCurrentStakers.GetValidator(nodeID)
	if err != nil {
		t.Fatal(err)
	}

	_, vdrTxID := validator.AddValidatorTx()
	if vdrTxID != addPendingValidatorTx.ID() {
		t.Fatalf("Added the wrong tx to the validator set")
	}

	onCommitPendingStakers := executor.OnCommit.PendingStakers()
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

	onAbortCurrentStakers := executor.OnAbort.CurrentStakers()
	if _, err := onAbortCurrentStakers.GetValidator(nodeID); err == nil {
		t.Fatalf("Shouldn't have added the validator to the validator set")
	}

	onAbortPendingStakers := executor.OnAbort.PendingStakers()
	_, retrievedTxID, err := onAbortPendingStakers.GetValidatorTx(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if retrievedTxID != addPendingValidatorTx.ID() {
		t.Fatalf("Added the wrong tx to the pending validator set")
	}

	// Test VM validators
	executor.OnCommit.Apply(env.state)
	assert.NoError(t, env.state.Write(dummyHeight))
	assert.True(t, env.config.Validators.Contains(constants.PrimaryNetworkID, nodeID))
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

	// Chronological order (not in scale):
	// Staker1:    |----------------------------------------------------------|
	// Staker2:        |------------------------|
	// Staker3:            |------------------------|
	// Staker3sub:             |----------------|
	// Staker4:            |------------------------|
	// Staker5:                                 |--------------------|
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
				staker1.nodeID: pending,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: pending,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
		},
		{
			description:   "advance time to staker 1 start with subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1},
			advanceTimeTo: []time.Time{staker1.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
		},
		{
			description:   "advance time to the staker2 start",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
		},
		{
			description:   "staker3 should validate only primary network",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3Sub, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: pending,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID:    current,
				staker2.nodeID:    current,
				staker3Sub.nodeID: pending,
				staker4.nodeID:    current,
				staker5.nodeID:    pending,
			},
		},
		{
			description:   "advance time to staker3 start with subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3Sub, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime, staker3Sub.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: pending,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: pending,
			},
		},
		{
			description:   "advance time to staker5 end",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime, staker5.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: current,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(ts *testing.T) {
			assert := assert.New(ts)
			env := newEnvironment()
			defer func() {
				if err := shutdownEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()
			env.config.WhitelistedSubnets.Add(testSubnet1.ID())
			dummyHeight := uint64(1)

			for _, staker := range test.stakers {
				_, err := addPendingValidator(
					env,
					staker.startTime,
					staker.endTime,
					staker.nodeID,
					[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
				)
				assert.NoError(err)
			}

			for _, staker := range test.subnetStakers {
				tx, err := env.txBuilder.NewAddSubnetValidatorTx(
					10, // Weight
					uint64(staker.startTime.Unix()),
					uint64(staker.endTime.Unix()),
					staker.nodeID,    // validator ID
					testSubnet1.ID(), // Subnet ID
					[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
					ids.ShortEmpty,
				)
				assert.NoError(err)
				env.state.AddPendingStaker(tx)
				env.state.AddTx(tx, status.Committed)
			}
			if err := env.state.Write(dummyHeight); err != nil {
				t.Fatal(err)
			}
			if err := env.state.Load(); err != nil {
				t.Fatal(err)
			}

			for _, newTime := range test.advanceTimeTo {
				env.clk.Set(newTime)
				tx, err := env.txBuilder.NewAdvanceTimeTx(newTime)
				if err != nil {
					t.Fatal(err)
				}

				executor := ProposalTxExecutor{
					Backend:     &env.backend,
					ParentState: env.state,
					Tx:          tx,
				}
				err = tx.Unsigned.Visit(&executor)
				if err != nil {
					t.Fatal(err)
				}

				assert.NoError(err)
				executor.OnCommit.Apply(env.state)
			}

			assert.NoError(env.state.Write(dummyHeight))

			// Check that the validators we expect to be in the current staker set are there
			currentStakers := env.state.CurrentStakers()
			// Check that the validators we expect to be in the pending staker set are there
			pendingStakers := env.state.PendingStakers()
			for stakerNodeID, status := range test.expectedStakers {
				switch status {
				case pending:
					_, _, err := pendingStakers.GetValidatorTx(stakerNodeID)
					assert.NoError(err)
					assert.False(env.config.Validators.Contains(constants.PrimaryNetworkID, stakerNodeID))
				case current:
					_, err := currentStakers.GetValidator(stakerNodeID)
					assert.NoError(err)
					assert.True(env.config.Validators.Contains(constants.PrimaryNetworkID, stakerNodeID))
				}
			}

			for stakerNodeID, status := range test.expectedSubnetStakers {
				switch status {
				case pending:
					assert.False(env.config.Validators.Contains(testSubnet1.ID(), stakerNodeID))
				case current:
					assert.True(env.config.Validators.Contains(testSubnet1.ID(), stakerNodeID))
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
	env := newEnvironment()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.config.WhitelistedSubnets.Add(testSubnet1.ID())
	dummyHeight := uint64(1)
	// Add a subnet validator to the staker set
	subnetValidatorNodeID := preFundedKeys[0].PublicKey().Address()
	// Starts after the corre
	subnetVdr1StartTime := defaultValidateStartTime
	subnetVdr1EndTime := defaultValidateStartTime.Add(defaultMinStakingDuration)
	tx, err := env.txBuilder.NewAddSubnetValidatorTx(
		1,                                  // Weight
		uint64(subnetVdr1StartTime.Unix()), // Start time
		uint64(subnetVdr1EndTime.Unix()),   // end time
		ids.NodeID(subnetValidatorNodeID),  // Node ID
		testSubnet1.ID(),                   // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	if err != nil {
		t.Fatal(err)
	}

	env.state.AddCurrentStaker(tx, 0)
	env.state.AddTx(tx, status.Committed)
	if err := env.state.Write(dummyHeight); err != nil {
		t.Fatal(err)
	}
	if err := env.state.Load(); err != nil {
		t.Fatal(err)
	}

	// The above validator is now part of the staking set

	// Queue a staker that joins the staker set after the above validator leaves
	subnetVdr2NodeID := preFundedKeys[1].PublicKey().Address()
	tx, err = env.txBuilder.NewAddSubnetValidatorTx(
		1, // Weight
		uint64(subnetVdr1EndTime.Add(time.Second).Unix()),                                // Start time
		uint64(subnetVdr1EndTime.Add(time.Second).Add(defaultMinStakingDuration).Unix()), // end time
		ids.NodeID(subnetVdr2NodeID),                                                     // Node ID
		testSubnet1.ID(),                                                                 // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},               // Keys
		ids.ShortEmpty, // reward address
	)
	if err != nil {
		t.Fatal(err)
	}

	env.state.AddPendingStaker(tx)
	env.state.AddTx(tx, status.Committed)
	if err := env.state.Write(dummyHeight); err != nil {
		t.Fatal(err)
	}
	if err := env.state.Load(); err != nil {
		t.Fatal(err)
	}

	// The above validator is now in the pending staker set

	// Advance time to the first staker's end time.
	env.clk.Set(subnetVdr1EndTime)
	tx, err = env.txBuilder.NewAdvanceTimeTx(subnetVdr1EndTime)
	if err != nil {
		t.Fatal(err)
	}

	executor := ProposalTxExecutor{
		Backend:     &env.backend,
		ParentState: env.state,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	if err != nil {
		t.Fatal(err)
	}

	currentStakers := executor.OnCommit.CurrentStakers()
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
	executor.OnCommit.Apply(env.state)
	assert.NoError(t, env.state.Write(dummyHeight))
	assert.False(t, env.config.Validators.Contains(testSubnet1.ID(), ids.NodeID(subnetVdr2NodeID)))
	assert.False(t, env.config.Validators.Contains(testSubnet1.ID(), ids.NodeID(subnetValidatorNodeID)))
}

func TestWhitelistedSubnet(t *testing.T) {
	for _, whitelist := range []bool{true, false} {
		t.Run(fmt.Sprintf("whitelisted %t", whitelist), func(ts *testing.T) {
			env := newEnvironment()
			defer func() {
				if err := shutdownEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()
			dummyHeight := uint64(1)
			if whitelist {
				env.config.WhitelistedSubnets.Add(testSubnet1.ID())
			}
			// Add a subnet validator to the staker set
			subnetValidatorNodeID := preFundedKeys[0].PublicKey().Address()

			subnetVdr1StartTime := defaultGenesisTime.Add(1 * time.Minute)
			subnetVdr1EndTime := defaultGenesisTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute)
			tx, err := env.txBuilder.NewAddSubnetValidatorTx(
				1,                                  // Weight
				uint64(subnetVdr1StartTime.Unix()), // Start time
				uint64(subnetVdr1EndTime.Unix()),   // end time
				ids.NodeID(subnetValidatorNodeID),  // Node ID
				testSubnet1.ID(),                   // Subnet ID
				[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
				ids.ShortEmpty,
			)
			if err != nil {
				t.Fatal(err)
			}

			env.state.AddPendingStaker(tx)
			env.state.AddTx(tx, status.Committed)
			if err := env.state.Write(dummyHeight); err != nil {
				t.Fatal(err)
			}
			if err := env.state.Load(); err != nil {
				t.Fatal(err)
			}

			// Advance time to the staker's start time.
			env.clk.Set(subnetVdr1StartTime)
			tx, err = env.txBuilder.NewAdvanceTimeTx(subnetVdr1StartTime)
			if err != nil {
				t.Fatal(err)
			}

			executor := ProposalTxExecutor{
				Backend:     &env.backend,
				ParentState: env.state,
				Tx:          tx,
			}
			err = tx.Unsigned.Visit(&executor)
			if err != nil {
				t.Fatal(err)
			}

			executor.OnCommit.Apply(env.state)
			assert.NoError(t, env.state.Write(dummyHeight))
			assert.Equal(t, whitelist, env.config.Validators.Contains(testSubnet1.ID(), ids.NodeID(subnetValidatorNodeID)))
		})
	}
}

func TestAdvanceTimeTxDelegatorStakerWeight(t *testing.T) {
	env := newEnvironment()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	dummyHeight := uint64(1)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMaxStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	_, err := addPendingValidator(env, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{preFundedKeys[0]})
	assert.NoError(t, err)

	tx, err := env.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime)
	assert.NoError(t, err)

	executor := ProposalTxExecutor{
		Backend:     &env.backend,
		ParentState: env.state,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	executor.OnCommit.Apply(env.state)
	assert.NoError(t, env.state.Write(dummyHeight))

	// Test validator weight before delegation
	primarySet, ok := env.config.Validators.GetValidators(constants.PrimaryNetworkID)
	assert.True(t, ok)
	vdrWeight, _ := primarySet.GetWeight(nodeID)
	assert.Equal(t, env.config.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(1 * time.Second)

	addDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		uint64(pendingDelegatorStartTime.Unix()),
		uint64(pendingDelegatorEndTime.Unix()),
		nodeID,
		preFundedKeys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1], preFundedKeys[4]},
		ids.ShortEmpty,
	)
	assert.NoError(t, err)
	env.state.AddPendingStaker(addDelegatorTx)
	env.state.AddTx(addDelegatorTx, status.Committed)
	assert.NoError(t, env.state.Write(dummyHeight))
	assert.NoError(t, env.state.Load())

	// Advance Time
	tx, err = env.txBuilder.NewAdvanceTimeTx(pendingDelegatorStartTime)
	assert.NoError(t, err)

	executor = ProposalTxExecutor{
		Backend:     &env.backend,
		ParentState: env.state,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	executor.OnCommit.Apply(env.state)
	assert.NoError(t, env.state.Write(dummyHeight))

	// Test validator weight after delegation
	vdrWeight, _ = primarySet.GetWeight(nodeID)
	assert.Equal(t, env.config.MinDelegatorStake+env.config.MinValidatorStake, vdrWeight)
}

func TestAdvanceTimeTxDelegatorStakers(t *testing.T) {
	env := newEnvironment()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	dummyHeight := uint64(1)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	_, err := addPendingValidator(env, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{preFundedKeys[0]})
	assert.NoError(t, err)

	tx, err := env.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime)
	assert.NoError(t, err)

	executor := ProposalTxExecutor{
		Backend:     &env.backend,
		ParentState: env.state,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	executor.OnCommit.Apply(env.state)
	assert.NoError(t, env.state.Write(dummyHeight))

	// Test validator weight before delegation
	primarySet, ok := env.config.Validators.GetValidators(constants.PrimaryNetworkID)
	assert.True(t, ok)
	vdrWeight, _ := primarySet.GetWeight(nodeID)
	assert.Equal(t, env.config.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(defaultMinStakingDuration)
	addDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		uint64(pendingDelegatorStartTime.Unix()),
		uint64(pendingDelegatorEndTime.Unix()),
		nodeID,
		preFundedKeys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1], preFundedKeys[4]},
		ids.ShortEmpty,
	)
	assert.NoError(t, err)
	env.state.AddPendingStaker(addDelegatorTx)
	env.state.AddTx(addDelegatorTx, status.Committed)
	assert.NoError(t, env.state.Write(dummyHeight))
	assert.NoError(t, env.state.Load())

	// Advance Time
	tx, err = env.txBuilder.NewAdvanceTimeTx(pendingDelegatorStartTime)
	assert.NoError(t, err)

	executor = ProposalTxExecutor{
		Backend:     &env.backend,
		ParentState: env.state,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	executor.OnCommit.Apply(env.state)
	assert.NoError(t, env.state.Write(dummyHeight))

	// Test validator weight after delegation
	vdrWeight, _ = primarySet.GetWeight(nodeID)
	assert.Equal(t, env.config.MinDelegatorStake+env.config.MinValidatorStake, vdrWeight)
}

// Test method InitiallyPrefersCommit
func TestAdvanceTimeTxInitiallyPrefersCommit(t *testing.T) {
	env := newEnvironment()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.clk.Set(defaultGenesisTime) // VM's clock reads the genesis time

	// Proposed advancing timestamp to 1 second after sync bound
	tx, err := env.txBuilder.NewAdvanceTimeTx(defaultGenesisTime.Add(SyncBound))
	if err != nil {
		t.Fatal(err)
	}

	executor := ProposalTxExecutor{
		Backend:     &env.backend,
		ParentState: env.state,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	if !executor.PrefersCommit {
		t.Fatal("should prefer to commit this tx because its proposed timestamp it's within sync bound")
	}
}

// Ensure marshaling/unmarshaling works
func TestAdvanceTimeTxUnmarshal(t *testing.T) {
	env := newEnvironment()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	tx, err := env.txBuilder.NewAdvanceTimeTx(defaultGenesisTime)
	if err != nil {
		t.Fatal(err)
	}

	bytes, err := txs.Codec.Marshal(txs.Version, tx)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshaledTx txs.Tx
	if _, err := txs.Codec.Unmarshal(bytes, &unmarshaledTx); err != nil {
		t.Fatal(err)
	} else if tx.Unsigned.(*txs.AdvanceTimeTx).Time != unmarshaledTx.Unsigned.(*txs.AdvanceTimeTx).Time {
		t.Fatal("should have same timestamp")
	}
}

func addPendingValidator(
	h *environment,
	startTime time.Time,
	endTime time.Time,
	nodeID ids.NodeID,
	keys []*crypto.PrivateKeySECP256K1R,
) (*txs.Tx, error) {
	addPendingValidatorTx, err := h.txBuilder.NewAddValidatorTx(
		h.config.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		ids.ShortID(nodeID),
		reward.PercentDenominator,
		keys,
		ids.ShortEmpty,
	)
	if err != nil {
		return nil, err
	}

	h.state.AddPendingStaker(addPendingValidatorTx)
	h.state.AddTx(addPendingValidatorTx, status.Committed)
	dummyHeight := uint64(1)
	if err := h.state.Write(dummyHeight); err != nil {
		return nil, err
	}
	if err := h.state.Load(); err != nil {
		return nil, err
	}
	return addPendingValidatorTx, err
}
