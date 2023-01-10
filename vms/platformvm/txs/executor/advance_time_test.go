// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Ensure semantic verification updates the current and pending staker set
// for the primary network
func TestAdvanceTimeTxUpdatePrimaryNetworkStakers(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	addPendingValidatorTx, err := addPendingValidator(env, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{preFundedKeys[0]})
	require.NoError(err)

	tx, err := env.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&executor))

	validatorStaker, err := executor.OnCommitState.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.Equal(addPendingValidatorTx.ID(), validatorStaker.TxID)
	require.EqualValues(1370, validatorStaker.PotentialReward) // See rewards tests to explain why 1370

	_, err = executor.OnCommitState.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = executor.OnAbortState.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	validatorStaker, err = executor.OnAbortState.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.Equal(addPendingValidatorTx.ID(), validatorStaker.TxID)

	// Test VM validators
	executor.OnCommitState.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())
	require.True(validators.Contains(env.config.Validators, constants.PrimaryNetworkID, nodeID))
}

// Ensure semantic verification fails when proposed timestamp is at or before current timestamp
func TestAdvanceTimeTxTimestampTooEarly(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	tx, err := env.txBuilder.NewAdvanceTimeTx(defaultGenesisTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&executor)
	require.Error(err, "should've failed verification because proposed timestamp same as current timestamp")
}

// Ensure semantic verification fails when proposed timestamp is after next validator set change time
func TestAdvanceTimeTxTimestampTooLate(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	env.ctx.Lock.Lock()

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	_, err := addPendingValidator(env, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{preFundedKeys[0]})
	require.NoError(err)

	{
		tx, err := env.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime.Add(1 * time.Second))
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.Error(err, "should've failed verification because proposed timestamp is after pending validator start time")
	}

	err = shutdownEnvironment(env)
	require.NoError(err)

	// Case: Timestamp is after next validator end time
	env = newEnvironment( /*postBanff*/ false)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	// fast forward clock to 10 seconds before genesis validators stop validating
	env.clk.Set(defaultValidateEndTime.Add(-10 * time.Second))

	{
		// Proposes advancing timestamp to 1 second after genesis validators stop validating
		tx, err := env.txBuilder.NewAdvanceTimeTx(defaultValidateEndTime.Add(1 * time.Second))
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.Error(err, "should've failed verification because proposed timestamp is after pending validator start time")
	}
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
		t.Run(test.description, func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment( /*postBanff*/ false)
			env.ctx.Lock.Lock()
			defer func() {
				require.NoError(shutdownEnvironment(env))
			}()

			dummyHeight := uint64(1)

			subnetID := testSubnet1.ID()
			env.config.TrackedSubnets.Add(subnetID)
			env.config.Validators.Add(subnetID, validators.NewSet())

			for _, staker := range test.stakers {
				_, err := addPendingValidator(
					env,
					staker.startTime,
					staker.endTime,
					staker.nodeID,
					[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
				)
				require.NoError(err)
			}

			for _, staker := range test.subnetStakers {
				tx, err := env.txBuilder.NewAddSubnetValidatorTx(
					10, // Weight
					uint64(staker.startTime.Unix()),
					uint64(staker.endTime.Unix()),
					staker.nodeID, // validator ID
					subnetID,      // Subnet ID
					[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
					ids.ShortEmpty,
				)
				require.NoError(err)

				staker, err := state.NewPendingStaker(
					tx.ID(),
					tx.Unsigned.(*txs.AddSubnetValidatorTx),
				)
				require.NoError(err)

				env.state.PutPendingValidator(staker)
				env.state.AddTx(tx, status.Committed)
			}
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			for _, newTime := range test.advanceTimeTo {
				env.clk.Set(newTime)
				tx, err := env.txBuilder.NewAdvanceTimeTx(newTime)
				require.NoError(err)

				onCommitState, err := state.NewDiff(lastAcceptedID, env)
				require.NoError(err)

				onAbortState, err := state.NewDiff(lastAcceptedID, env)
				require.NoError(err)

				executor := ProposalTxExecutor{
					OnCommitState: onCommitState,
					OnAbortState:  onAbortState,
					Backend:       &env.backend,
					Tx:            tx,
				}
				require.NoError(tx.Unsigned.Visit(&executor))

				executor.OnCommitState.Apply(env.state)
			}
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			for stakerNodeID, status := range test.expectedStakers {
				switch status {
				case pending:
					_, err := env.state.GetPendingValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.NoError(err)
					require.False(validators.Contains(env.config.Validators, constants.PrimaryNetworkID, stakerNodeID))
				case current:
					_, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.NoError(err)
					require.True(validators.Contains(env.config.Validators, constants.PrimaryNetworkID, stakerNodeID))
				}
			}

			for stakerNodeID, status := range test.expectedSubnetStakers {
				switch status {
				case pending:
					require.False(validators.Contains(env.config.Validators, subnetID, stakerNodeID))
				case current:
					require.True(validators.Contains(env.config.Validators, subnetID, stakerNodeID))
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
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	subnetID := testSubnet1.ID()
	env.config.TrackedSubnets.Add(subnetID)
	env.config.Validators.Add(subnetID, validators.NewSet())

	dummyHeight := uint64(1)
	// Add a subnet validator to the staker set
	subnetValidatorNodeID := ids.NodeID(preFundedKeys[0].PublicKey().Address())
	// Starts after the corre
	subnetVdr1StartTime := defaultValidateStartTime
	subnetVdr1EndTime := defaultValidateStartTime.Add(defaultMinStakingDuration)
	tx, err := env.txBuilder.NewAddSubnetValidatorTx(
		1,                                  // Weight
		uint64(subnetVdr1StartTime.Unix()), // Start time
		uint64(subnetVdr1EndTime.Unix()),   // end time
		subnetValidatorNodeID,              // Node ID
		subnetID,                           // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	staker, err := state.NewCurrentStaker(
		tx.ID(),
		tx.Unsigned.(*txs.AddSubnetValidatorTx),
		0,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(staker)
	env.state.AddTx(tx, status.Committed)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// The above validator is now part of the staking set

	// Queue a staker that joins the staker set after the above validator leaves
	subnetVdr2NodeID := ids.NodeID(preFundedKeys[1].PublicKey().Address())
	tx, err = env.txBuilder.NewAddSubnetValidatorTx(
		1, // Weight
		uint64(subnetVdr1EndTime.Add(time.Second).Unix()),                                // Start time
		uint64(subnetVdr1EndTime.Add(time.Second).Add(defaultMinStakingDuration).Unix()), // end time
		subnetVdr2NodeID, // Node ID
		subnetID,         // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]}, // Keys
		ids.ShortEmpty, // reward address
	)
	require.NoError(err)

	staker, err = state.NewPendingStaker(
		tx.ID(),
		tx.Unsigned.(*txs.AddSubnetValidatorTx),
	)
	require.NoError(err)

	env.state.PutPendingValidator(staker)
	env.state.AddTx(tx, status.Committed)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// The above validator is now in the pending staker set

	// Advance time to the first staker's end time.
	env.clk.Set(subnetVdr1EndTime)
	tx, err = env.txBuilder.NewAdvanceTimeTx(subnetVdr1EndTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&executor))

	_, err = executor.OnCommitState.GetCurrentValidator(subnetID, subnetValidatorNodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// Check VM Validators are removed successfully
	executor.OnCommitState.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())
	require.False(validators.Contains(env.config.Validators, subnetID, subnetVdr2NodeID))
	require.False(validators.Contains(env.config.Validators, subnetID, subnetValidatorNodeID))
}

func TestTrackedSubnet(t *testing.T) {
	for _, tracked := range []bool{true, false} {
		t.Run(fmt.Sprintf("tracked %t", tracked), func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment( /*postBanff*/ false)
			env.ctx.Lock.Lock()
			defer func() {
				require.NoError(shutdownEnvironment(env))
			}()
			dummyHeight := uint64(1)

			subnetID := testSubnet1.ID()
			if tracked {
				env.config.TrackedSubnets.Add(subnetID)
				env.config.Validators.Add(subnetID, validators.NewSet())
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
				subnetID,                           // Subnet ID
				[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
				ids.ShortEmpty,
			)
			require.NoError(err)

			staker, err := state.NewPendingStaker(
				tx.ID(),
				tx.Unsigned.(*txs.AddSubnetValidatorTx),
			)
			require.NoError(err)

			env.state.PutPendingValidator(staker)
			env.state.AddTx(tx, status.Committed)
			env.state.SetHeight(dummyHeight)
			err = env.state.Commit()
			require.NoError(err)

			// Advance time to the staker's start time.
			env.clk.Set(subnetVdr1StartTime)
			tx, err = env.txBuilder.NewAdvanceTimeTx(subnetVdr1StartTime)
			require.NoError(err)

			onCommitState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			onAbortState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			executor := ProposalTxExecutor{
				OnCommitState: onCommitState,
				OnAbortState:  onAbortState,
				Backend:       &env.backend,
				Tx:            tx,
			}
			err = tx.Unsigned.Visit(&executor)
			require.NoError(err)

			executor.OnCommitState.Apply(env.state)
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())
			require.Equal(tracked, validators.Contains(env.config.Validators, subnetID, ids.NodeID(subnetValidatorNodeID)))
		})
	}
}

func TestAdvanceTimeTxDelegatorStakerWeight(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMaxStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	_, err := addPendingValidator(
		env,
		pendingValidatorStartTime,
		pendingValidatorEndTime,
		nodeID,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
	)
	require.NoError(err)

	tx, err := env.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&executor)
	require.NoError(err)

	executor.OnCommitState.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Test validator weight before delegation
	primarySet, ok := env.config.Validators.Get(constants.PrimaryNetworkID)
	require.True(ok)
	vdrWeight := primarySet.GetWeight(nodeID)
	require.Equal(env.config.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(1 * time.Second)

	addDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		uint64(pendingDelegatorStartTime.Unix()),
		uint64(pendingDelegatorEndTime.Unix()),
		nodeID,
		preFundedKeys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{
			preFundedKeys[0],
			preFundedKeys[1],
			preFundedKeys[4],
		},
		ids.ShortEmpty,
	)
	require.NoError(err)

	staker, err := state.NewPendingStaker(
		addDelegatorTx.ID(),
		addDelegatorTx.Unsigned.(*txs.AddDelegatorTx),
	)
	require.NoError(err)

	env.state.PutPendingDelegator(staker)
	env.state.AddTx(addDelegatorTx, status.Committed)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Advance Time
	tx, err = env.txBuilder.NewAdvanceTimeTx(pendingDelegatorStartTime)
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor = ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&executor)
	require.NoError(err)

	executor.OnCommitState.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Test validator weight after delegation
	vdrWeight = primarySet.GetWeight(nodeID)
	require.Equal(env.config.MinDelegatorStake+env.config.MinValidatorStake, vdrWeight)
}

func TestAdvanceTimeTxDelegatorStakers(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	_, err := addPendingValidator(env, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*crypto.PrivateKeySECP256K1R{preFundedKeys[0]})
	require.NoError(err)

	tx, err := env.txBuilder.NewAdvanceTimeTx(pendingValidatorStartTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&executor)
	require.NoError(err)

	executor.OnCommitState.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Test validator weight before delegation
	primarySet, ok := env.config.Validators.Get(constants.PrimaryNetworkID)
	require.True(ok)
	vdrWeight := primarySet.GetWeight(nodeID)
	require.Equal(env.config.MinValidatorStake, vdrWeight)

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
	require.NoError(err)

	staker, err := state.NewPendingStaker(
		addDelegatorTx.ID(),
		addDelegatorTx.Unsigned.(*txs.AddDelegatorTx),
	)
	require.NoError(err)

	env.state.PutPendingDelegator(staker)
	env.state.AddTx(addDelegatorTx, status.Committed)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Advance Time
	tx, err = env.txBuilder.NewAdvanceTimeTx(pendingDelegatorStartTime)
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor = ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&executor)
	require.NoError(err)

	executor.OnCommitState.Apply(env.state)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Test validator weight after delegation
	vdrWeight = primarySet.GetWeight(nodeID)
	require.Equal(env.config.MinDelegatorStake+env.config.MinValidatorStake, vdrWeight)
}

// Test method InitiallyPrefersCommit
func TestAdvanceTimeTxInitiallyPrefersCommit(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	env.clk.Set(defaultGenesisTime) // VM's clock reads the genesis time

	// Proposed advancing timestamp to 1 second after sync bound
	tx, err := env.txBuilder.NewAdvanceTimeTx(defaultGenesisTime.Add(SyncBound))
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&executor)
	require.NoError(err)

	require.True(executor.PrefersCommit, "should prefer to commit this tx because its proposed timestamp it's within sync bound")
}

func TestAdvanceTimeTxAfterBanff(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	env.clk.Set(defaultGenesisTime) // VM's clock reads the genesis time
	env.config.BanffTime = defaultGenesisTime.Add(SyncBound)

	// Proposed advancing timestamp to the banff timestamp
	tx, err := env.txBuilder.NewAdvanceTimeTx(defaultGenesisTime.Add(SyncBound))
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&executor)
	require.ErrorIs(err, errAdvanceTimeTxIssuedAfterBanff)
}

// Ensure marshaling/unmarshaling works
func TestAdvanceTimeTxUnmarshal(t *testing.T) {
	require := require.New(t)
	env := newEnvironment( /*postBanff*/ false)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	tx, err := env.txBuilder.NewAdvanceTimeTx(defaultGenesisTime)
	require.NoError(err)

	bytes, err := txs.Codec.Marshal(txs.Version, tx)
	require.NoError(err)

	var unmarshaledTx txs.Tx
	_, err = txs.Codec.Unmarshal(bytes, &unmarshaledTx)
	require.NoError(err)

	require.Equal(
		tx.Unsigned.(*txs.AdvanceTimeTx).Time,
		unmarshaledTx.Unsigned.(*txs.AdvanceTimeTx).Time,
	)
}

func addPendingValidator(
	env *environment,
	startTime time.Time,
	endTime time.Time,
	nodeID ids.NodeID,
	keys []*crypto.PrivateKeySECP256K1R,
) (*txs.Tx, error) {
	addPendingValidatorTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake,
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

	staker, err := state.NewPendingStaker(
		addPendingValidatorTx.ID(),
		addPendingValidatorTx.Unsigned.(*txs.AddValidatorTx),
	)
	if err != nil {
		return nil, err
	}

	env.state.PutPendingValidator(staker)
	env.state.AddTx(addPendingValidatorTx, status.Committed)
	dummyHeight := uint64(1)
	env.state.SetHeight(dummyHeight)
	if err := env.state.Commit(); err != nil {
		return nil, err
	}
	return addPendingValidatorTx, nil
}
