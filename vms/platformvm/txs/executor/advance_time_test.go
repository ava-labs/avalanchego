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
	h := newTestHelpersCollection()
	h.ctx.Lock.Lock()
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()

	tx, err := h.txBuilder.NewAdvanceTimeTx(defaultGenesisTime)
	if err != nil {
		t.Fatal(err)
	}

	executor := ProposalTxExecutor{
		Backend:     &h.execBackend,
		ParentState: h.tState,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	if err == nil {
		t.Fatal("should've failed verification because proposed timestamp same as current timestamp")
	}
}

// Ensure semantic verification fails when proposed timestamp is after next validator set change time
func TestAdvanceTimeTxTimestampTooLate(t *testing.T) {
	h := newTestHelpersCollection()
	h.ctx.Lock.Lock()

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	factory := crypto.FactorySECP256K1R{}
	nodeIDKey, _ := factory.NewPrivateKey()
	nodeID := ids.NodeID(nodeIDKey.PublicKey().Address())
	_, err := addPendingValidator(h, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]})
	assert.NoError(t, err)

	{
		tx, err := h.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime.Add(1 * time.Second))
		if err != nil {
			t.Fatal(err)
		}

		executor := ProposalTxExecutor{
			Backend:     &h.execBackend,
			ParentState: h.tState,
			Tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should've failed verification because proposed timestamp is after pending validator start time")
		}
	}

	if err := internalStateShutdown(h); err != nil {
		t.Fatal(err)
	}

	// Case: Timestamp is after next validator end time
	h = newTestHelpersCollection()
	h.ctx.Lock.Lock()
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()

	// fast forward clock to 10 seconds before genesis validators stop validating
	h.clk.Set(defaultValidateEndTime.Add(-10 * time.Second))

	{
		// Proposes advancing timestamp to 1 second after genesis validators stop validating
		tx, err := h.txBuilder.NewAdvanceTimeTx(defaultValidateEndTime.Add(1 * time.Second))
		if err != nil {
			t.Fatal(err)
		}

		executor := ProposalTxExecutor{
			Backend:     &h.execBackend,
			ParentState: h.tState,
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
	h := newTestHelpersCollection()
	h.ctx.Lock.Lock()
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	dummyHeight := uint64(1)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	factory := crypto.FactorySECP256K1R{}
	nodeIDKey, _ := factory.NewPrivateKey()
	nodeID := ids.NodeID(nodeIDKey.PublicKey().Address())
	addPendingValidatorTx, err := addPendingValidator(h, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]})
	assert.NoError(t, err)

	tx, err := h.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime)
	if err != nil {
		t.Fatal(err)
	}

	executor := ProposalTxExecutor{
		Backend:     &h.execBackend,
		ParentState: h.tState,
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
	executor.OnCommit.Apply(h.tState)
	h.tState.SetHeight(dummyHeight)
	assert.NoError(t, h.tState.Commit())
	assert.True(t, h.cfg.Validators.Contains(constants.PrimaryNetworkID, nodeID))
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
	// Staker0:    |--- ??? // Staker0 end time depends on the test
	// Staker1:        |------------------------------------------------------------------------|
	// Staker2:            |------------------------|
	// Staker3:                |------------------------|
	// Staker3sub:                 |----------------|
	// Staker4:                |------------------------|
	// Staker5:            |----------------------------------------|
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
			h := newTestHelpersCollection()
			h.ctx.Lock.Lock()
			defer func() {
				if err := internalStateShutdown(h); err != nil {
					t.Fatal(err)
				}
			}()
			h.cfg.WhitelistedSubnets.Add(testSubnet1.ID())
			dummyHeight := uint64(1)

			for _, staker := range test.stakers {
				_, err := addPendingValidator(
					h,
					staker.startTime,
					staker.endTime,
					staker.nodeID,
					[]*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
				)
				assert.NoError(err)
			}

			for _, staker := range test.subnetStakers {
				tx, err := h.txBuilder.NewAddSubnetValidatorTx(
					10, // Weight
					uint64(staker.startTime.Unix()),
					uint64(staker.endTime.Unix()),
					staker.nodeID,    // validator ID
					testSubnet1.ID(), // Subnet ID
					[]*crypto.PrivateKeySECP256K1R{prefundedKeys[0], prefundedKeys[1]},
					ids.ShortEmpty,
				)
				assert.NoError(err)
				h.tState.AddPendingStaker(tx)
				h.tState.AddTx(tx, status.Committed)
			}
			h.tState.SetHeight(dummyHeight)
			if err := h.tState.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := h.tState.Load(); err != nil {
				t.Fatal(err)
			}

			for _, newTime := range test.advanceTimeTo {
				h.clk.Set(newTime)
				tx, err := h.txBuilder.NewAdvanceTimeTx(newTime)
				if err != nil {
					t.Fatal(err)
				}

				executor := ProposalTxExecutor{
					Backend:     &h.execBackend,
					ParentState: h.tState,
					Tx:          tx,
				}
				err = tx.Unsigned.Visit(&executor)
				if err != nil {
					t.Fatal(err)
				}

				assert.NoError(err)
				executor.OnCommit.Apply(h.tState)
			}

			h.tState.SetHeight(dummyHeight)
			assert.NoError(h.tState.Commit())

			// Check that the validators we expect to be in the current staker set are there
			currentStakers := h.tState.CurrentStakers()
			// Check that the validators we expect to be in the pending staker set are there
			pendingStakers := h.tState.PendingStakers()
			for stakerNodeID, status := range test.expectedStakers {
				switch status {
				case pending:
					_, _, err := pendingStakers.GetValidatorTx(stakerNodeID)
					assert.NoError(err)
					assert.False(h.cfg.Validators.Contains(constants.PrimaryNetworkID, stakerNodeID))
				case current:
					_, err := currentStakers.GetValidator(stakerNodeID)
					assert.NoError(err)
					assert.True(h.cfg.Validators.Contains(constants.PrimaryNetworkID, stakerNodeID))
				}
			}

			for stakerNodeID, status := range test.expectedSubnetStakers {
				switch status {
				case pending:
					assert.False(h.cfg.Validators.Contains(testSubnet1.ID(), stakerNodeID))
				case current:
					assert.True(h.cfg.Validators.Contains(testSubnet1.ID(), stakerNodeID))
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
	h := newTestHelpersCollection()
	h.ctx.Lock.Lock()
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.cfg.WhitelistedSubnets.Add(testSubnet1.ID())
	dummyHeight := uint64(1)
	// Add a subnet validator to the staker set
	subnetValidatorNodeID := prefundedKeys[0].PublicKey().Address()
	// Starts after the corre
	subnetVdr1StartTime := defaultValidateStartTime
	subnetVdr1EndTime := defaultValidateStartTime.Add(defaultMinStakingDuration)
	tx, err := h.txBuilder.NewAddSubnetValidatorTx(
		1,                                  // Weight
		uint64(subnetVdr1StartTime.Unix()), // Start time
		uint64(subnetVdr1EndTime.Unix()),   // end time
		ids.NodeID(subnetValidatorNodeID),  // Node ID
		testSubnet1.ID(),                   // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{prefundedKeys[0], prefundedKeys[1]},
		ids.ShortEmpty,
	)
	if err != nil {
		t.Fatal(err)
	}

	h.tState.AddCurrentStaker(tx, 0)
	h.tState.AddTx(tx, status.Committed)
	h.tState.SetHeight(dummyHeight)
	if err := h.tState.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := h.tState.Load(); err != nil {
		t.Fatal(err)
	}

	// The above validator is now part of the staking set

	// Queue a staker that joins the staker set after the above validator leaves
	subnetVdr2NodeID := prefundedKeys[1].PublicKey().Address()
	tx, err = h.txBuilder.NewAddSubnetValidatorTx(
		1, // Weight
		uint64(subnetVdr1EndTime.Add(time.Second).Unix()),                                // Start time
		uint64(subnetVdr1EndTime.Add(time.Second).Add(defaultMinStakingDuration).Unix()), // end time
		ids.NodeID(subnetVdr2NodeID),                                                     // Node ID
		testSubnet1.ID(),                                                                 // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{prefundedKeys[0], prefundedKeys[1]},               // Keys
		ids.ShortEmpty, // reward address
	)
	if err != nil {
		t.Fatal(err)
	}

	h.tState.AddPendingStaker(tx)
	h.tState.AddTx(tx, status.Committed)
	h.tState.SetHeight(dummyHeight)
	if err := h.tState.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := h.tState.Load(); err != nil {
		t.Fatal(err)
	}

	// The above validator is now in the pending staker set

	// Advance time to the first staker's end time.
	h.clk.Set(subnetVdr1EndTime)
	tx, err = h.txBuilder.NewAdvanceTimeTx(subnetVdr1EndTime)
	if err != nil {
		t.Fatal(err)
	}

	executor := ProposalTxExecutor{
		Backend:     &h.execBackend,
		ParentState: h.tState,
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
	executor.OnCommit.Apply(h.tState)
	h.tState.SetHeight(dummyHeight)
	assert.NoError(t, h.tState.Commit())
	assert.False(t, h.cfg.Validators.Contains(testSubnet1.ID(), ids.NodeID(subnetVdr2NodeID)))
	assert.False(t, h.cfg.Validators.Contains(testSubnet1.ID(), ids.NodeID(subnetValidatorNodeID)))
}

func TestWhitelistedSubnet(t *testing.T) {
	for _, whitelist := range []bool{true, false} {
		t.Run(fmt.Sprintf("whitelisted %t", whitelist), func(ts *testing.T) {
			h := newTestHelpersCollection()
			h.ctx.Lock.Lock()
			defer func() {
				if err := internalStateShutdown(h); err != nil {
					t.Fatal(err)
				}
			}()
			dummyHeight := uint64(1)
			if whitelist {
				h.cfg.WhitelistedSubnets.Add(testSubnet1.ID())
			}
			// Add a subnet validator to the staker set
			subnetValidatorNodeID := prefundedKeys[0].PublicKey().Address()

			subnetVdr1StartTime := defaultGenesisTime.Add(1 * time.Minute)
			subnetVdr1EndTime := defaultGenesisTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute)
			tx, err := h.txBuilder.NewAddSubnetValidatorTx(
				1,                                  // Weight
				uint64(subnetVdr1StartTime.Unix()), // Start time
				uint64(subnetVdr1EndTime.Unix()),   // end time
				ids.NodeID(subnetValidatorNodeID),  // Node ID
				testSubnet1.ID(),                   // Subnet ID
				[]*crypto.PrivateKeySECP256K1R{prefundedKeys[0], prefundedKeys[1]},
				ids.ShortEmpty,
			)
			if err != nil {
				t.Fatal(err)
			}

			h.tState.AddPendingStaker(tx)
			h.tState.AddTx(tx, status.Committed)
			h.tState.SetHeight(dummyHeight)
			if err := h.tState.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := h.tState.Load(); err != nil {
				t.Fatal(err)
			}

			// Advance time to the staker's start time.
			h.clk.Set(subnetVdr1StartTime)
			tx, err = h.txBuilder.NewAdvanceTimeTx(subnetVdr1StartTime)
			if err != nil {
				t.Fatal(err)
			}

			executor := ProposalTxExecutor{
				Backend:     &h.execBackend,
				ParentState: h.tState,
				Tx:          tx,
			}
			err = tx.Unsigned.Visit(&executor)
			if err != nil {
				t.Fatal(err)
			}

			executor.OnCommit.Apply(h.tState)
			h.tState.SetHeight(dummyHeight)
			assert.NoError(t, h.tState.Commit())
			assert.Equal(t, whitelist, h.cfg.Validators.Contains(testSubnet1.ID(), ids.NodeID(subnetValidatorNodeID)))
		})
	}
}

func TestAdvanceTimeTxDelegatorStakerWeight(t *testing.T) {
	h := newTestHelpersCollection()
	h.ctx.Lock.Lock()
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	dummyHeight := uint64(1)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMaxStakingDuration)
	factory := crypto.FactorySECP256K1R{}
	nodeIDKey, _ := factory.NewPrivateKey()
	nodeID := ids.NodeID(nodeIDKey.PublicKey().Address())
	_, err := addPendingValidator(h, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]})
	assert.NoError(t, err)

	tx, err := h.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime)
	assert.NoError(t, err)

	executor := ProposalTxExecutor{
		Backend:     &h.execBackend,
		ParentState: h.tState,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	executor.OnCommit.Apply(h.tState)
	h.tState.SetHeight(dummyHeight)
	assert.NoError(t, h.tState.Commit())

	// Test validator weight before delegation
	primarySet, ok := h.cfg.Validators.GetValidators(constants.PrimaryNetworkID)
	assert.True(t, ok)
	vdrWeight, _ := primarySet.GetWeight(nodeID)
	assert.Equal(t, h.cfg.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(1 * time.Second)

	addDelegatorTx, err := h.txBuilder.NewAddDelegatorTx(
		h.cfg.MinDelegatorStake,
		uint64(pendingDelegatorStartTime.Unix()),
		uint64(pendingDelegatorEndTime.Unix()),
		nodeID,
		prefundedKeys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{prefundedKeys[0], prefundedKeys[1], prefundedKeys[4]},
		ids.ShortEmpty,
	)
	assert.NoError(t, err)
	h.tState.AddPendingStaker(addDelegatorTx)
	h.tState.AddTx(addDelegatorTx, status.Committed)
	h.tState.SetHeight(dummyHeight)
	assert.NoError(t, h.tState.Commit())
	assert.NoError(t, h.tState.Load())

	// Advance Time
	tx, err = h.txBuilder.NewAdvanceTimeTx(pendingDelegatorStartTime)
	assert.NoError(t, err)

	executor = ProposalTxExecutor{
		Backend:     &h.execBackend,
		ParentState: h.tState,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	executor.OnCommit.Apply(h.tState)
	h.tState.SetHeight(dummyHeight)
	assert.NoError(t, h.tState.Commit())

	// Test validator weight after delegation
	vdrWeight, _ = primarySet.GetWeight(nodeID)
	assert.Equal(t, h.cfg.MinDelegatorStake+h.cfg.MinValidatorStake, vdrWeight)
}

func TestAdvanceTimeTxDelegatorStakers(t *testing.T) {
	h := newTestHelpersCollection()
	h.ctx.Lock.Lock()
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	dummyHeight := uint64(1)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	factory := crypto.FactorySECP256K1R{}
	nodeIDKey, _ := factory.NewPrivateKey()
	nodeID := ids.NodeID(nodeIDKey.PublicKey().Address())
	_, err := addPendingValidator(h, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]})
	assert.NoError(t, err)

	tx, err := h.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime)
	assert.NoError(t, err)

	executor := ProposalTxExecutor{
		Backend:     &h.execBackend,
		ParentState: h.tState,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	executor.OnCommit.Apply(h.tState)
	h.tState.SetHeight(dummyHeight)
	assert.NoError(t, h.tState.Commit())

	// Test validator weight before delegation
	primarySet, ok := h.cfg.Validators.GetValidators(constants.PrimaryNetworkID)
	assert.True(t, ok)
	vdrWeight, _ := primarySet.GetWeight(nodeID)
	assert.Equal(t, h.cfg.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(defaultMinStakingDuration)
	addDelegatorTx, err := h.txBuilder.NewAddDelegatorTx(
		h.cfg.MinDelegatorStake,
		uint64(pendingDelegatorStartTime.Unix()),
		uint64(pendingDelegatorEndTime.Unix()),
		nodeID,
		prefundedKeys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{prefundedKeys[0], prefundedKeys[1], prefundedKeys[4]},
		ids.ShortEmpty,
	)
	assert.NoError(t, err)
	h.tState.AddPendingStaker(addDelegatorTx)
	h.tState.AddTx(addDelegatorTx, status.Committed)
	h.tState.SetHeight(dummyHeight)
	assert.NoError(t, h.tState.Commit())
	assert.NoError(t, h.tState.Load())

	// Advance Time
	tx, err = h.txBuilder.NewAdvanceTimeTx(pendingDelegatorStartTime)
	assert.NoError(t, err)

	executor = ProposalTxExecutor{
		Backend:     &h.execBackend,
		ParentState: h.tState,
		Tx:          tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(t, err)

	executor.OnCommit.Apply(h.tState)
	h.tState.SetHeight(dummyHeight)
	assert.NoError(t, h.tState.Commit())

	// Test validator weight after delegation
	vdrWeight, _ = primarySet.GetWeight(nodeID)
	assert.Equal(t, h.cfg.MinDelegatorStake+h.cfg.MinValidatorStake, vdrWeight)
}

// Test method InitiallyPrefersCommit
func TestAdvanceTimeTxInitiallyPrefersCommit(t *testing.T) {
	h := newTestHelpersCollection()
	h.ctx.Lock.Lock()
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.clk.Set(defaultGenesisTime) // VM's clock reads the genesis time

	// Proposed advancing timestamp to 1 second after sync bound
	tx, err := h.txBuilder.NewAdvanceTimeTx(defaultGenesisTime.Add(SyncBound))
	if err != nil {
		t.Fatal(err)
	}

	executor := ProposalTxExecutor{
		Backend:     &h.execBackend,
		ParentState: h.tState,
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
	h := newTestHelpersCollection()
	h.ctx.Lock.Lock()
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()

	tx, err := h.txBuilder.NewAdvanceTimeTx(defaultGenesisTime)
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
	h *testHelpersCollection,
	startTime time.Time,
	endTime time.Time,
	nodeID ids.NodeID,
	keys []*crypto.PrivateKeySECP256K1R,
) (*txs.Tx, error) {
	addPendingValidatorTx, err := h.txBuilder.NewAddValidatorTx(
		h.cfg.MinValidatorStake,
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

	h.tState.AddPendingStaker(addPendingValidatorTx)
	h.tState.AddTx(addPendingValidatorTx, status.Committed)
	dummyHeight := uint64(1)
	h.tState.SetHeight(dummyHeight)
	if err := h.tState.Commit(); err != nil {
		return nil, err
	}
	if err := h.tState.Load(); err != nil {
		return nil, err
	}
	return addPendingValidatorTx, err
}
