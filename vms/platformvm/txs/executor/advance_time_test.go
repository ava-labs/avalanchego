// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func newAdvanceTimeTx(t testing.TB, timestamp time.Time) (*txs.Tx, error) {
	utx := &txs.AdvanceTimeTx{Time: uint64(timestamp.Unix())}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(snowtest.Context(t, snowtest.PChainID))
}

// Ensure semantic verification updates the current and pending staker set
// for the primary network
func TestAdvanceTimeTxUpdatePrimaryNetworkStakers(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()
	dummyHeight := uint64(1)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	addPendingValidatorTx := addPendingValidator(
		t,
		env,
		pendingValidatorStartTime,
		pendingValidatorEndTime,
		nodeID,
		[]*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
	)

	tx, err := newAdvanceTimeTx(t, pendingValidatorStartTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	))

	validatorStaker, err := onCommitState.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.Equal(addPendingValidatorTx.ID(), validatorStaker.TxID)
	require.Equal(uint64(1370), validatorStaker.PotentialReward) // See rewards tests to explain why 1370

	_, err = onCommitState.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = onAbortState.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	validatorStaker, err = onAbortState.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.Equal(addPendingValidatorTx.ID(), validatorStaker.TxID)

	// Test VM validators
	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())
	_, ok := env.config.Validators.GetValidator(constants.PrimaryNetworkID, nodeID)
	require.True(ok)
}

// Ensure semantic verification fails when proposed timestamp is before the
// current timestamp
func TestAdvanceTimeTxTimestampTooEarly(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)

	tx, err := newAdvanceTimeTx(t, env.state.GetTimestamp().Add(-time.Second))
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
	err = ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	)
	require.ErrorIs(err, ErrChildBlockEarlierThanParent)
}

// Ensure semantic verification fails when proposed timestamp is after next
// validator set change time
func TestAdvanceTimeTxTimestampTooLate(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	addPendingValidator(t, env, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]})

	{
		tx, err := newAdvanceTimeTx(t, pendingValidatorStartTime.Add(1*time.Second))
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, ErrChildBlockAfterStakerChangeTime)
	}

	// Case: Timestamp is after next validator end time
	env = newEnvironment(t, upgradetest.ApricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	// fast forward clock to when genesis validators stop validating
	env.clk.Set(genesistest.DefaultValidatorEndTime)

	{
		// Proposes advancing timestamp to 1 second after genesis validators stop validating
		tx, err := newAdvanceTimeTx(t, genesistest.DefaultValidatorEndTime.Add(1*time.Second))
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, ErrChildBlockAfterStakerChangeTime)
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
		startTime: genesistest.DefaultValidatorStartTime.Add(1 * time.Minute),
		endTime:   genesistest.DefaultValidatorStartTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute),
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
			env := newEnvironment(t, upgradetest.ApricotPhase5)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			dummyHeight := uint64(1)

			subnetID := testSubnet1.ID()
			env.config.TrackedSubnets.Add(subnetID)

			for _, staker := range test.stakers {
				addPendingValidator(
					t,
					env,
					staker.startTime,
					staker.endTime,
					staker.nodeID,
					[]*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
				)
			}

			for _, staker := range test.subnetStakers {
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})

				tx, err := wallet.IssueAddSubnetValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: staker.nodeID,
							Start:  uint64(staker.startTime.Unix()),
							End:    uint64(staker.endTime.Unix()),
							Wght:   10,
						},
						Subnet: subnetID,
					},
				)
				require.NoError(err)

				staker, err := state.NewPendingStaker(
					tx.ID(),
					tx.Unsigned.(*txs.AddSubnetValidatorTx),
				)
				require.NoError(err)

				require.NoError(env.state.PutPendingValidator(staker))
				env.state.AddTx(tx, status.Committed)
			}
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			for _, newTime := range test.advanceTimeTo {
				env.clk.Set(newTime)
				tx, err := newAdvanceTimeTx(t, newTime)
				require.NoError(err)

				onCommitState, err := state.NewDiff(lastAcceptedID, env)
				require.NoError(err)

				onAbortState, err := state.NewDiff(lastAcceptedID, env)
				require.NoError(err)

				feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
				require.NoError(ProposalTx(
					&env.backend,
					feeCalculator,
					tx,
					onCommitState,
					onAbortState,
				))

				require.NoError(onCommitState.Apply(env.state))
			}
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			for stakerNodeID, status := range test.expectedStakers {
				switch status {
				case pending:
					_, err := env.state.GetPendingValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.NoError(err)
					_, ok := env.config.Validators.GetValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.False(ok)
				case current:
					_, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.NoError(err)
					_, ok := env.config.Validators.GetValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.True(ok)
				}
			}

			for stakerNodeID, status := range test.expectedSubnetStakers {
				switch status {
				case pending:
					_, ok := env.config.Validators.GetValidator(subnetID, stakerNodeID)
					require.False(ok)
				case current:
					_, ok := env.config.Validators.GetValidator(subnetID, stakerNodeID)
					require.True(ok)
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
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	env.config.TrackedSubnets.Add(subnetID)

	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	dummyHeight := uint64(1)
	// Add a subnet validator to the staker set
	subnetValidatorNodeID := genesistest.DefaultNodeIDs[0]
	subnetVdr1EndTime := genesistest.DefaultValidatorStartTime.Add(defaultMinStakingDuration)

	tx, err := wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: subnetValidatorNodeID,
				Start:  genesistest.DefaultValidatorStartTimeUnix,
				End:    uint64(subnetVdr1EndTime.Unix()),
				Wght:   1,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	addSubnetValTx := tx.Unsigned.(*txs.AddSubnetValidatorTx)
	staker, err := state.NewCurrentStaker(
		tx.ID(),
		addSubnetValTx,
		addSubnetValTx.StartTime(),
		0,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(staker))
	env.state.AddTx(tx, status.Committed)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// The above validator is now part of the staking set

	// Queue a staker that joins the staker set after the above validator leaves
	subnetVdr2NodeID := genesistest.DefaultNodeIDs[1]
	tx, err = wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: subnetVdr2NodeID,
				Start:  uint64(subnetVdr1EndTime.Add(time.Second).Unix()),
				End:    uint64(subnetVdr1EndTime.Add(time.Second).Add(defaultMinStakingDuration).Unix()),
				Wght:   1,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	staker, err = state.NewPendingStaker(
		tx.ID(),
		tx.Unsigned.(*txs.AddSubnetValidatorTx),
	)
	require.NoError(err)

	require.NoError(env.state.PutPendingValidator(staker))
	env.state.AddTx(tx, status.Committed)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// The above validator is now in the pending staker set

	// Advance time to the first staker's end time.
	env.clk.Set(subnetVdr1EndTime)
	tx, err = newAdvanceTimeTx(t, subnetVdr1EndTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	))

	_, err = onCommitState.GetCurrentValidator(subnetID, subnetValidatorNodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// Check VM Validators are removed successfully
	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())
	_, ok := env.config.Validators.GetValidator(subnetID, subnetVdr2NodeID)
	require.False(ok)
	_, ok = env.config.Validators.GetValidator(subnetID, subnetValidatorNodeID)
	require.False(ok)
}

func TestTrackedSubnet(t *testing.T) {
	for _, tracked := range []bool{true, false} {
		t.Run(fmt.Sprintf("tracked %t", tracked), func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, upgradetest.ApricotPhase5)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()
			dummyHeight := uint64(1)

			subnetID := testSubnet1.ID()
			if tracked {
				env.config.TrackedSubnets.Add(subnetID)
			}

			wallet := newWallet(t, env, walletConfig{
				subnetIDs: []ids.ID{subnetID},
			})

			// Add a subnet validator to the staker set
			subnetValidatorNodeID := genesistest.DefaultNodeIDs[0]

			subnetVdr1StartTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Minute)
			subnetVdr1EndTime := genesistest.DefaultValidatorStartTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute)
			tx, err := wallet.IssueAddSubnetValidatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: subnetValidatorNodeID,
						Start:  uint64(subnetVdr1StartTime.Unix()),
						End:    uint64(subnetVdr1EndTime.Unix()),
						Wght:   1,
					},
					Subnet: subnetID,
				},
			)
			require.NoError(err)

			staker, err := state.NewPendingStaker(
				tx.ID(),
				tx.Unsigned.(*txs.AddSubnetValidatorTx),
			)
			require.NoError(err)

			require.NoError(env.state.PutPendingValidator(staker))
			env.state.AddTx(tx, status.Committed)
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			// Advance time to the staker's start time.
			env.clk.Set(subnetVdr1StartTime)
			tx, err = newAdvanceTimeTx(t, subnetVdr1StartTime)
			require.NoError(err)

			onCommitState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			onAbortState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
			require.NoError(ProposalTx(
				&env.backend,
				feeCalculator,
				tx,
				onCommitState,
				onAbortState,
			))

			require.NoError(onCommitState.Apply(env.state))

			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())
			_, ok := env.config.Validators.GetValidator(subnetID, subnetValidatorNodeID)
			require.True(ok)
		})
	}
}

func TestAdvanceTimeTxDelegatorStakerWeight(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()
	dummyHeight := uint64(1)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMaxStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	addPendingValidator(
		t,
		env,
		pendingValidatorStartTime,
		pendingValidatorEndTime,
		nodeID,
		[]*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
	)

	tx, err := newAdvanceTimeTx(t, pendingValidatorStartTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	))

	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	wallet := newWallet(t, env, walletConfig{})

	// Test validator weight before delegation
	vdrWeight := env.config.Validators.GetWeight(constants.PrimaryNetworkID, nodeID)
	require.Equal(env.config.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(1 * time.Second)

	addDelegatorTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(pendingDelegatorStartTime.Unix()),
			End:    uint64(pendingDelegatorEndTime.Unix()),
			Wght:   env.config.MinDelegatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
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
	tx, err = newAdvanceTimeTx(t, pendingDelegatorStartTime)
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	))

	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Test validator weight after delegation
	vdrWeight = env.config.Validators.GetWeight(constants.PrimaryNetworkID, nodeID)
	require.Equal(env.config.MinDelegatorStake+env.config.MinValidatorStake, vdrWeight)
}

func TestAdvanceTimeTxDelegatorStakers(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()
	dummyHeight := uint64(1)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	addPendingValidator(t, env, pendingValidatorStartTime, pendingValidatorEndTime, nodeID, []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]})

	tx, err := newAdvanceTimeTx(t, pendingValidatorStartTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	))

	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	wallet := newWallet(t, env, walletConfig{})

	// Test validator weight before delegation
	vdrWeight := env.config.Validators.GetWeight(constants.PrimaryNetworkID, nodeID)
	require.Equal(env.config.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(defaultMinStakingDuration)
	addDelegatorTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(pendingDelegatorStartTime.Unix()),
			End:    uint64(pendingDelegatorEndTime.Unix()),
			Wght:   env.config.MinDelegatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
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
	tx, err = newAdvanceTimeTx(t, pendingDelegatorStartTime)
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	require.NoError(ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	))

	require.NoError(onCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Test validator weight after delegation
	vdrWeight = env.config.Validators.GetWeight(constants.PrimaryNetworkID, nodeID)
	require.Equal(env.config.MinDelegatorStake+env.config.MinValidatorStake, vdrWeight)
}

func TestAdvanceTimeTxAfterBanff(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Durango)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()
	env.clk.Set(genesistest.DefaultValidatorStartTime) // VM's clock reads the genesis time
	upgradeTime := env.clk.Time().Add(SyncBound)
	env.config.UpgradeConfig.BanffTime = upgradeTime
	env.config.UpgradeConfig.CortinaTime = upgradeTime
	env.config.UpgradeConfig.DurangoTime = upgradeTime

	// Proposed advancing timestamp to the banff timestamp
	tx, err := newAdvanceTimeTx(t, upgradeTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
	err = ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	)
	require.ErrorIs(err, ErrAdvanceTimeTxIssuedAfterBanff)
}

// Ensure marshaling/unmarshaling works
func TestAdvanceTimeTxUnmarshal(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	chainTime := env.state.GetTimestamp()
	tx, err := newAdvanceTimeTx(t, chainTime.Add(time.Second))
	require.NoError(err)

	bytes, err := txs.Codec.Marshal(txs.CodecVersion, tx)
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
	t testing.TB,
	env *environment,
	startTime time.Time,
	endTime time.Time,
	nodeID ids.NodeID,
	keys []*secp256k1.PrivateKey,
) *txs.Tx {
	require := require.New(t)

	wallet := newWallet(t, env, walletConfig{
		keys: keys,
	})
	addPendingValidatorTx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(startTime.Unix()),
			End:    uint64(endTime.Unix()),
			Wght:   env.config.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)

	staker, err := state.NewPendingStaker(
		addPendingValidatorTx.ID(),
		addPendingValidatorTx.Unsigned.(*txs.AddValidatorTx),
	)
	require.NoError(err)

	require.NoError(env.state.PutPendingValidator(staker))
	env.state.AddTx(addPendingValidatorTx, status.Committed)
	dummyHeight := uint64(1)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())
	return addPendingValidatorTx
}
