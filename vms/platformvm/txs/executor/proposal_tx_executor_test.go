// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// addPrimaryNetworkValidator issues an AddValidatorTx for validator and
// executes it as a proposal tx whose commit branch is applied onto diff, so
// the validator ends up in the primary network's pending validator set. The tx
// is funded by key so that callers can avoid UTXO conflicts between txs that
// are staged on diffs but never applied to env.state.
func addPrimaryNetworkValidator(
	t testing.TB,
	env *environment,
	diff *state.Diff,
	key *secp256k1.PrivateKey,
	validator *platform.Validator,
) {
	t.Helper()
	require := require.New(t)

	wallet := newWallet(t, env, walletConfig{
		keys: []*secp256k1.PrivateKey{key},
	})

	tx, err := wallet.IssueAddValidatorTx(
		validator,
		newOwner(),
		reward.PercentDenominator,
	)
	require.NoError(err)

	onCommitState, _, err := executeProposalTx(t, env, diff, tx)
	require.NoError(err)
	require.NoError(onCommitState.Apply(diff))
	diff.AddTx(tx, status.Committed)
}

// executeAddDelegatorProposalTx issues an AddDelegatorTx for validator funded
// by feeKeys and executes it as a proposal tx on commit/abort diffs built on top of diff,
// returning the execution error.
func executeAddDelegatorProposalTx(
	t testing.TB,
	env *environment,
	diff *state.Diff,
	validator *platform.Validator,
	feeKey *secp256k1.PrivateKey,
) error {
	t.Helper()
	require := require.New(t)

	wallet := newWallet(t, env, walletConfig{
		keys: []*secp256k1.PrivateKey{feeKey},
	})
	tx, err := wallet.IssueAddDelegatorTx(
		validator,
		newOwner(),
	)
	require.NoError(err)

	_, _, err = executeProposalTx(t, env, diff, tx)
	return err
}

func TestProposalTxExecuteAddDelegator(t *testing.T) {
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	chainTime := env.state.GetTimestamp()

	startTime := genesistest.DefaultValidatorStartTime.Add(5 * time.Second)
	endTime := genesistest.DefaultValidatorEndTime.Add(-5 * time.Second)

	tests := []struct {
		name            string
		validatorWeight uint64
		AP3Time         time.Time
	}{
		{
			name:            "valid",
			validatorWeight: env.config.MinValidatorStake,
			AP3Time:         chainTime.Add(-time.Second), // AP3 is active
		},
		{
			// Over delegation is not enforced before AP3.
			name:            "over_delegation_before_AP3",
			validatorWeight: env.config.MaxValidatorStake,
			AP3Time:         chainTime.Add(time.Second), // AP3 is not active
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.config.UpgradeConfig.ApricotPhase3Time = tt.AP3Time

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, err)

			nodeID := ids.GenerateTestNodeID()
			addPrimaryNetworkValidator(t, env, diff, genesistest.DefaultFundedKeys[0], &platform.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   tt.validatorWeight,
			})

			err = executeAddDelegatorProposalTx(t, env, diff, &platform.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   env.config.MinDelegatorStake,
			}, genesistest.DefaultFundedKeys[1])
			require.NoError(t, err)
		})
	}
}

func TestProposalTxExecuteAddDelegatorErrors(t *testing.T) {
	genesisNodeID := genesistest.DefaultNodeIDs[0]
	// Every AddDelegatorTx is funded by delegatorKey. The validators staged
	// on diff are funded by other keys so that the UTXOs the wallet reads
	// from env.state are never already consumed on diff.
	delegatorKey := genesistest.DefaultFundedKeys[0]

	env := newEnvironment(t, upgradetest.ApricotPhase5)
	// Activate AP3 so that over delegation is enforced.
	env.config.UpgradeConfig.ApricotPhase3Time = env.state.GetTimestamp().Add(-time.Second)

	// All cases are executed on top of diff, which stages a min and a max stake
	// validator and is never applied to env.state. Both validators' staking
	// period is strictly inside the genesis validators' period.
	var (
		minStakeNodeID = ids.GenerateTestNodeID()
		maxStakeNodeID = ids.GenerateTestNodeID()
		startTime      = uint64(genesistest.DefaultValidatorStartTime.Add(5 * time.Second).Unix())
		endTime        = uint64(genesistest.DefaultValidatorEndTime.Add(-5 * time.Second).Unix())
	)
	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)

	addPrimaryNetworkValidator(t, env, diff, genesistest.DefaultFundedKeys[1], &platform.Validator{
		NodeID: minStakeNodeID,
		Start:  startTime,
		End:    endTime,
		Wght:   env.config.MinValidatorStake,
	})
	addPrimaryNetworkValidator(t, env, diff, genesistest.DefaultFundedKeys[2], &platform.Validator{
		NodeID: maxStakeNodeID,
		Start:  startTime,
		End:    endTime,
		Wght:   env.config.MaxValidatorStake,
	})

	require.NoError(t, diff.Apply(env.state))

	tests := []struct {
		name        string
		startTime   uint64
		endTime     uint64
		nodeID      ids.NodeID
		updateState func(*testing.T, *state.Diff)
		want        error
	}{
		{
			name:      "validator_stops_validating_earlier_than_delegator",
			startTime: genesistest.DefaultValidatorStartTimeUnix + 1,
			endTime:   genesistest.DefaultValidatorEndTimeUnix + 1,
			nodeID:    genesisNodeID,
			want:      errPeriodMismatch,
		},
		{
			name:      "validator_not_in_the_current_or_pending_validator_sets",
			startTime: startTime,
			endTime:   endTime,
			nodeID:    ids.GenerateTestNodeID(),
			want:      database.ErrNotFound,
		},
		{
			name:      "delegator_starts_before_validator",
			startTime: startTime - 1,
			endTime:   endTime,
			nodeID:    minStakeNodeID,
			want:      errPeriodMismatch,
		},
		{
			name:      "delegator_stops_after_validator",
			startTime: startTime,
			endTime:   endTime + 1,
			nodeID:    minStakeNodeID,
			want:      errPeriodMismatch,
		},
		{
			name:      "starts_delegating_at_current_timestamp",
			startTime: genesistest.DefaultValidatorStartTimeUnix,
			endTime:   genesistest.DefaultValidatorEndTimeUnix,
			nodeID:    genesisNodeID,
			want:      ErrTimestampNotBeforeStartTime,
		},
		{
			name:      "tx_fee_paying_key_has_no_funds",
			startTime: genesistest.DefaultValidatorStartTimeUnix + 1,
			endTime:   genesistest.DefaultValidatorEndTimeUnix,
			nodeID:    genesisNodeID,
			updateState: func(t *testing.T, diff *state.Diff) {
				deleteUTXOsOwnedBy(t, env, diff, delegatorKey)
			},
			want: errFlowCheckFailed,
		},
		{
			name:      "over_delegation_after_AP3",
			startTime: startTime,
			endTime:   endTime,
			nodeID:    maxStakeNodeID,
			want:      ErrOverDelegated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, err)

			if tt.updateState != nil {
				tt.updateState(t, diff)
			}

			err = executeAddDelegatorProposalTx(t, env, diff, &platform.Validator{
				NodeID: tt.nodeID,
				Start:  tt.startTime,
				End:    tt.endTime,
				Wght:   env.config.MinDelegatorStake,
			}, delegatorKey)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestProposalTxExecuteAddSubnetValidator(t *testing.T) {
	env := newEnvironment(t, upgradetest.ApricotPhase5)

	// diff stages a non-genesis primary network validator whose staking period
	// is strictly inside the genesis validators' period. It is never applied to
	// env.state.
	stagedNodeID := ids.GenerateTestNodeID()
	stagedStartTime := uint64(genesistest.DefaultValidatorStartTime.Add(5 * time.Second).Unix())
	stagedEndTime := uint64(genesistest.DefaultValidatorEndTime.Add(-5 * time.Second).Unix())
	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)
	addPrimaryNetworkValidator(t, env, diff, genesistest.DefaultFundedKeys[0], &platform.Validator{
		NodeID: stagedNodeID,
		Start:  stagedStartTime,
		End:    stagedEndTime,
		Wght:   env.config.MinValidatorStake,
	})

	tests := []struct {
		name      string
		nodeID    ids.NodeID
		startTime uint64
		endTime   uint64
	}{
		{
			name:      "genesis_validator_subnet_period_subset_of_primary_network_period",
			nodeID:    genesistest.DefaultNodeIDs[0],
			startTime: genesistest.DefaultValidatorStartTimeUnix + 1,
			endTime:   genesistest.DefaultValidatorEndTimeUnix,
		},
		{
			name:      "staged_validator_subnet_period_equal_to_primary_network_period",
			nodeID:    stagedNodeID,
			startTime: stagedStartTime,
			endTime:   stagedEndTime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueAddSubnetValidatorTx(&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: tt.nodeID,
					Start:  tt.startTime,
					End:    tt.endTime,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: testSubnet1.ID(),
			})
			require.NoError(err)

			_, _, err = executeProposalTx(t, env, diff, tx)
			require.NoError(err)
		})
	}
}

func TestProposalTxExecuteAddSubnetValidatorErrors(t *testing.T) {
	genesisNodeID := genesistest.DefaultNodeIDs[0]

	env := newEnvironment(t, upgradetest.ApricotPhase5)
	subnetID := testSubnet1.ID()

	// All cases are executed on top of diff, which stages a non-genesis
	// primary network validator whose staking period is strictly inside the
	// genesis validators' period. It is never applied to env.state.
	stagedNodeID := ids.GenerateTestNodeID()
	stagedStartTime := uint64(genesistest.DefaultValidatorStartTime.Add(5 * time.Second).Unix())
	stagedEndTime := uint64(genesistest.DefaultValidatorEndTime.Add(-5 * time.Second).Unix())

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)

	addPrimaryNetworkValidator(t, env, diff, genesistest.DefaultFundedKeys[0], &platform.Validator{
		NodeID: stagedNodeID,
		Start:  stagedStartTime,
		End:    stagedEndTime,
		Wght:   env.config.MinValidatorStake,
	})

	tests := []struct {
		name        string
		nodeID      ids.NodeID
		startTime   uint64
		endTime     uint64
		updateState func(*testing.T, *state.Diff)
		updateTx    func(*platform.Tx)
		want        error
	}{
		{
			name:      "genesis_validator_stops_validating_subnet_after_primary_network",
			nodeID:    genesisNodeID,
			startTime: genesistest.DefaultValidatorStartTimeUnix + 1,
			endTime:   genesistest.DefaultValidatorEndTimeUnix + 1,
			want:      errPeriodMismatch,
		},
		{
			name:      "validator_not_in_the_current_or_pending_validator_sets",
			nodeID:    ids.GenerateTestNodeID(),
			startTime: stagedStartTime,
			endTime:   stagedEndTime,
			want:      errNotValidator,
		},
		{
			name:      "staged_validator_starts_validating_subnet_before_primary_network",
			nodeID:    stagedNodeID,
			startTime: stagedStartTime - 1,
			endTime:   stagedEndTime,
			want:      errPeriodMismatch,
		},
		{
			name:      "staged_validator_stops_validating_subnet_after_primary_network",
			nodeID:    stagedNodeID,
			startTime: stagedStartTime,
			endTime:   stagedEndTime + 1,
			want:      errPeriodMismatch,
		},
		{
			name:      "starts_validating_at_current_timestamp",
			nodeID:    genesisNodeID,
			startTime: genesistest.DefaultValidatorStartTimeUnix + 2,
			endTime:   uint64(genesistest.DefaultValidatorStartTime.Add(2 * time.Second).Add(env.config.MinStakeDuration).Unix()),
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetTimestamp(genesistest.DefaultValidatorStartTime.Add(2 * time.Second))
			},
			want: ErrTimestampNotBeforeStartTime,
		},
		{
			name:      "already_validating_subnet",
			nodeID:    genesisNodeID,
			startTime: genesistest.DefaultValidatorStartTimeUnix + 2,
			endTime:   genesistest.DefaultValidatorEndTimeUnix,
			updateState: func(t *testing.T, diff *state.Diff) {
				// Add the validator to the subnet's pending set, then advance
				// the chain time to its start time so it is moved into the
				// subnet's current set.
				subnetValidatorStartTime := genesistest.DefaultValidatorStartTime.Add(time.Second)
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddSubnetValidatorTx(&platform.SubnetValidator{
					Validator: platform.Validator{
						NodeID: genesisNodeID,
						Start:  uint64(subnetValidatorStartTime.Unix()),
						End:    genesistest.DefaultValidatorEndTimeUnix,
						Wght:   genesistest.DefaultValidatorWeight,
					},
					Subnet: subnetID,
				})
				require.NoError(t, err)

				onCommitState, _, err := executeProposalTx(t, env, diff, tx)
				require.NoError(t, err)
				require.NoError(t, onCommitState.Apply(diff))
				diff.AddTx(tx, status.Committed)

				advanceTimeTx := newAdvanceTimeTx(t, subnetValidatorStartTime)
				onCommitState, _, err = executeProposalTx(t, env, diff, advanceTimeTx)
				require.NoError(t, err)
				require.NoError(t, onCommitState.Apply(diff))
				diff.AddTx(advanceTimeTx, status.Committed)

				_, err = diff.GetCurrentValidator(subnetID, genesisNodeID)
				require.NoError(t, err)
			},
			want: ErrDuplicateValidator,
		},
		{
			name:      "too_few_subnet_auth_signatures",
			nodeID:    genesisNodeID,
			startTime: genesistest.DefaultValidatorStartTimeUnix + 1,
			endTime:   uint64(genesistest.DefaultValidatorStartTime.Add(env.config.MinStakeDuration).Unix()) + 1,
			updateTx: func(tx *platform.Tx) {
				addSubnetValidatorTx := tx.Unsigned.(*platform.AddSubnetValidatorTx)
				input := addSubnetValidatorTx.SubnetAuth.(*secp256k1fx.Input)
				input.SigIndices = input.SigIndices[1:]
				// The tx was syntactically verified when it was built. Force
				// it to be re-verified.
				addSubnetValidatorTx.SyntacticallyVerified = false
			},
			want: errUnauthorizedModification,
		},
		{
			name:      "subnet_auth_signature_from_non_control_key",
			nodeID:    genesisNodeID,
			startTime: genesistest.DefaultValidatorStartTimeUnix + 1,
			endTime:   uint64(genesistest.DefaultValidatorStartTime.Add(env.config.MinStakeDuration).Unix()) + 1,
			updateTx: func(tx *platform.Tx) {
				// Replace a valid signature with one from a key that is not a
				// control key of the subnet.
				sig, err := genesistest.DefaultFundedKeys[3].SignHash(hashing.ComputeHash256(tx.Unsigned.Bytes()))
				require.NoError(t, err)
				copy(tx.Creds[0].(*secp256k1fx.Credential).Sigs[0][:], sig)
			},
			want: errUnauthorizedModification,
		},
		{
			name:      "already_pending_validator_of_subnet",
			nodeID:    genesisNodeID,
			startTime: genesistest.DefaultValidatorStartTimeUnix + 1,
			endTime:   uint64(genesistest.DefaultValidatorStartTime.Add(env.config.MinStakeDuration).Unix()) + 1,
			updateState: func(t *testing.T, diff *state.Diff) {
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddSubnetValidatorTx(&platform.SubnetValidator{
					Validator: platform.Validator{
						NodeID: genesisNodeID,
						Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
						End:    genesistest.DefaultValidatorEndTimeUnix,
						Wght:   genesistest.DefaultValidatorWeight,
					},
					Subnet: subnetID,
				})
				require.NoError(t, err)

				onCommitState, _, err := executeProposalTx(t, env, diff, tx)
				require.NoError(t, err)
				require.NoError(t, onCommitState.Apply(diff))
				diff.AddTx(tx, status.Committed)
			},
			want: ErrDuplicateValidator,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			diff, err := state.NewDiffOn(diff, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			if tt.updateState != nil {
				tt.updateState(t, diff)
			}

			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueAddSubnetValidatorTx(&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: tt.nodeID,
					Start:  tt.startTime,
					End:    tt.endTime,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			})
			require.NoError(err)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			_, _, err = executeProposalTx(t, env, diff, tx)
			require.ErrorIs(err, tt.want)
		})
	}
}

// executeAddValidatorProposalTx issues an AddValidatorTx for validator funded
// by feeKey and executes it as a proposal tx on commit/abort diffs built on top of diff,
// returning the execution error.
func executeAddValidatorProposalTx(
	t testing.TB,
	env *environment,
	diff *state.Diff,
	validator *platform.Validator,
	feeKey *secp256k1.PrivateKey,
) error {
	t.Helper()
	require := require.New(t)

	wallet := newWallet(t, env, walletConfig{
		keys: []*secp256k1.PrivateKey{feeKey},
	})
	tx, err := wallet.IssueAddValidatorTx(
		validator,
		newOwner(),
		reward.PercentDenominator,
	)
	require.NoError(err)

	_, _, err = executeProposalTx(t, env, diff, tx)
	return err
}

func TestProposalTxExecuteAddValidatorErrors(t *testing.T) {
	// Every AddValidatorTx under test is funded by feeKey. Validators staged on
	// diff by a case's updateState are funded by other keys so that the UTXOs the
	// wallet reads from env.state are never already consumed on diff.
	feeKey := genesistest.DefaultFundedKeys[0]
	pendingNodeID := ids.GenerateTestNodeID()

	env := newEnvironment(t, upgradetest.ApricotPhase5)

	tests := []struct {
		name        string
		nodeID      ids.NodeID
		startTime   uint64
		endTime     uint64
		updateState func(*testing.T, *state.Diff)
		want        error
	}{
		{
			name:      "starts_validating_at_current_timestamp",
			nodeID:    ids.GenerateTestNodeID(),
			startTime: genesistest.DefaultValidatorStartTimeUnix,
			endTime:   genesistest.DefaultValidatorEndTimeUnix,
			want:      ErrTimestampNotBeforeStartTime,
		},
		{
			name:      "already_validating_primary_network",
			nodeID:    genesistest.DefaultNodeIDs[0],
			startTime: genesistest.DefaultValidatorStartTimeUnix + 1,
			endTime:   genesistest.DefaultValidatorEndTimeUnix,
			want:      errAlreadyValidator,
		},
		{
			name:      "already_pending_validator_of_primary_network",
			nodeID:    pendingNodeID,
			startTime: genesistest.DefaultValidatorStartTimeUnix + 1,
			endTime:   genesistest.DefaultValidatorEndTimeUnix,
			updateState: func(t *testing.T, diff *state.Diff) {
				addPrimaryNetworkValidator(t, env, diff, genesistest.DefaultFundedKeys[1], &platform.Validator{
					NodeID: pendingNodeID,
					Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
					End:    genesistest.DefaultValidatorEndTimeUnix,
					Wght:   env.config.MinValidatorStake,
				})
			},
			want: errAlreadyValidator,
		},
		{
			name:      "tx_fee_paying_key_has_no_funds",
			nodeID:    ids.GenerateTestNodeID(),
			startTime: genesistest.DefaultValidatorStartTimeUnix + 1,
			endTime:   genesistest.DefaultValidatorEndTimeUnix,
			updateState: func(t *testing.T, diff *state.Diff) {
				deleteUTXOsOwnedBy(t, env, diff, feeKey)
			},
			want: errFlowCheckFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, err)

			if tt.updateState != nil {
				tt.updateState(t, diff)
			}

			err = executeAddValidatorProposalTx(
				t,
				env,
				diff,
				&platform.Validator{
					NodeID: tt.nodeID,
					Start:  tt.startTime,
					End:    tt.endTime,
					Wght:   env.config.MinValidatorStake,
				},
				feeKey,
			)
			require.ErrorIs(t, err, tt.want)
		})
	}
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
	pendingValidatorEndTime := pendingValidatorStartTime.Add(env.config.MinStakeDuration)
	nodeID := ids.GenerateTestNodeID()
	addPendingValidatorTx := addPendingValidator(
		t,
		env,
		pendingValidatorStartTime,
		pendingValidatorEndTime,
		nodeID,
		[]*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
	)

	tx := newAdvanceTimeTx(t, pendingValidatorStartTime)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	onCommitState, onAbortState, err := executeProposalTx(t, env, diff, tx)
	require.NoError(err)

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

func TestAdvanceTimeTxErrors(t *testing.T) {
	pendingValidatorStartTime := genesistest.DefaultValidatorStartTime.Add(time.Second)
	banffTime := genesistest.DefaultValidatorStartTime.Add(SyncBound)

	tests := []struct {
		name      string
		fork      upgradetest.Fork
		updateEnv func(*testing.T, *environment)
		timestamp time.Time
		want      error
	}{
		{
			name:      "timestamp_before_chain_time",
			fork:      upgradetest.ApricotPhase5,
			timestamp: genesistest.DefaultValidatorStartTime.Add(-time.Second),
			want:      ErrChildBlockEarlierThanParent,
		},
		{
			name: "timestamp_after_next_validator_start_time",
			fork: upgradetest.ApricotPhase5,
			updateEnv: func(t *testing.T, env *environment) {
				addPendingValidator(
					t,
					env,
					pendingValidatorStartTime,
					pendingValidatorStartTime.Add(env.config.MinStakeDuration),
					ids.GenerateTestNodeID(),
					[]*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
				)
			},
			timestamp: pendingValidatorStartTime.Add(time.Second),
			want:      ErrChildBlockAfterStakerChangeTime,
		},
		{
			name: "timestamp_after_next_validator_end_time",
			fork: upgradetest.ApricotPhase5,
			updateEnv: func(_ *testing.T, env *environment) {
				// Fast forward the clock to when the genesis validators stop
				// validating
				env.clk.Set(genesistest.DefaultValidatorEndTime)
			},
			timestamp: genesistest.DefaultValidatorEndTime.Add(time.Second),
			want:      ErrChildBlockAfterStakerChangeTime,
		},
		{
			name: "issued_after_banff",
			fork: upgradetest.Durango,
			updateEnv: func(_ *testing.T, env *environment) {
				// The VM's clock reads the genesis time
				env.clk.Set(genesistest.DefaultValidatorStartTime)
				env.config.UpgradeConfig.BanffTime = banffTime
				env.config.UpgradeConfig.CortinaTime = banffTime
				env.config.UpgradeConfig.DurangoTime = banffTime
			},
			timestamp: banffTime,
			want:      ErrAdvanceTimeTxIssuedAfterBanff,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newEnvironment(t, tt.fork)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			if tt.updateEnv != nil {
				tt.updateEnv(t, env)
			}

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, err)
			_, _, err = executeProposalTx(t, env, diff, newAdvanceTimeTx(t, tt.timestamp))
			require.ErrorIs(t, err, tt.want)
		})
	}
}

// Ensure semantic verification updates the current and pending staker sets correctly.
// Namely, it should add pending stakers whose start time is at or before the timestamp.
// It will not remove primary network stakers; that happens in rewardplatform.
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
		name              string
		stakers           []staker
		subnetStakers     []staker
		advanceTimeTo     []time.Time
		wantStakers       map[ids.NodeID]stakerStatus
		wantSubnetStakers map[ids.NodeID]stakerStatus
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
			name:          "advance_time_to_before_staker1_start_with_subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime.Add(-1 * time.Second)},
			wantStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: pending,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
			wantSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: pending,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
		},
		{
			name:          "advance_time_to_staker1_start_with_subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1},
			advanceTimeTo: []time.Time{staker1.startTime},
			wantStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
			wantSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
		},
		{
			name:          "advance_time_to_the_staker2_start",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime},
			wantStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
		},
		{
			name:          "staker3_should_validate_only_primary_network",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3Sub, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime},
			wantStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: pending,
			},
			wantSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID:    current,
				staker2.nodeID:    current,
				staker3Sub.nodeID: pending,
				staker4.nodeID:    current,
				staker5.nodeID:    pending,
			},
		},
		{
			name:          "advance_time_to_staker3_start_with_subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3Sub, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime, staker3Sub.startTime},
			wantStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: pending,
			},
			wantSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: pending,
			},
		},
		{
			name:          "advance_time_to_staker5_end",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime, staker5.startTime},
			wantStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: current,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)
			for _, staker := range test.subnetStakers {
				wallet := newWallet(t, env, walletConfig{})

				tx, err := wallet.IssueAddSubnetValidatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
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
					tx.Unsigned.(*platform.AddSubnetValidatorTx),
				)
				require.NoError(err)

				require.NoError(diff.PutPendingValidator(staker))
				diff.AddTx(tx, status.Committed)
			}
			require.NoError(diff.Apply(env.state))
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			for _, newTime := range test.advanceTimeTo {
				env.clk.Set(newTime)
				tx := newAdvanceTimeTx(t, newTime)

				diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)
				onCommitState, _, err := executeProposalTx(t, env, diff, tx)
				require.NoError(err)

				require.NoError(onCommitState.Apply(env.state))
			}
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			for stakerNodeID, status := range test.wantStakers {
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

			for stakerNodeID, status := range test.wantSubnetStakers {
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

	wallet := newWallet(t, env, walletConfig{})

	dummyHeight := uint64(1)
	// Add a subnet validator to the staker set
	subnetValidatorNodeID := genesistest.DefaultNodeIDs[0]
	subnetVdr1EndTime := genesistest.DefaultValidatorStartTime.Add(env.config.MinStakeDuration)

	tx, err := wallet.IssueAddSubnetValidatorTx(
		&platform.SubnetValidator{
			Validator: platform.Validator{
				NodeID: subnetValidatorNodeID,
				Start:  genesistest.DefaultValidatorStartTimeUnix,
				End:    uint64(subnetVdr1EndTime.Unix()),
				Wght:   1,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	addSubnetValTx := tx.Unsigned.(*platform.AddSubnetValidatorTx)
	staker, err := state.NewCurrentStaker(
		tx.ID(),
		addSubnetValTx,
		addSubnetValTx.StartTime(),
		addSubnetValTx.EndTime(),
		addSubnetValTx.Weight(),
		0,
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	require.NoError(diff.PutCurrentValidator(staker))
	diff.AddTx(tx, status.Committed)
	require.NoError(diff.Apply(env.state))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// The above validator is now part of the staking set

	// Queue a staker that joins the staker set after the above validator leaves
	subnetVdr2NodeID := genesistest.DefaultNodeIDs[1]
	tx, err = wallet.IssueAddSubnetValidatorTx(
		&platform.SubnetValidator{
			Validator: platform.Validator{
				NodeID: subnetVdr2NodeID,
				Start:  uint64(subnetVdr1EndTime.Add(time.Second).Unix()),
				End:    uint64(subnetVdr1EndTime.Add(time.Second).Add(env.config.MinStakeDuration).Unix()),
				Wght:   1,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	staker, err = state.NewPendingStaker(
		tx.ID(),
		tx.Unsigned.(*platform.AddSubnetValidatorTx),
	)
	require.NoError(err)

	diff, err = state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	require.NoError(diff.PutPendingValidator(staker))
	diff.AddTx(tx, status.Committed)
	require.NoError(diff.Apply(env.state))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// The above validator is now in the pending staker set

	// Advance time to the first staker's end time.
	env.clk.Set(subnetVdr1EndTime)
	tx = newAdvanceTimeTx(t, subnetVdr1EndTime)

	diff, err = state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	onCommitState, _, err := executeProposalTx(t, env, diff, tx)
	require.NoError(err)

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
		t.Run(fmt.Sprintf("tracked_%t", tracked), func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, upgradetest.ApricotPhase5)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()
			dummyHeight := uint64(1)

			subnetID := testSubnet1.ID()
			if tracked {
				env.config.TrackedSubnets.Add(subnetID)
			}

			wallet := newWallet(t, env, walletConfig{})

			// Add a subnet validator to the staker set
			subnetValidatorNodeID := genesistest.DefaultNodeIDs[0]

			subnetVdr1StartTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Minute)
			subnetVdr1EndTime := genesistest.DefaultValidatorStartTime.Add(10 * env.config.MinStakeDuration).Add(1 * time.Minute)
			tx, err := wallet.IssueAddSubnetValidatorTx(
				&platform.SubnetValidator{
					Validator: platform.Validator{
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
				tx.Unsigned.(*platform.AddSubnetValidatorTx),
			)
			require.NoError(err)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)
			require.NoError(diff.PutPendingValidator(staker))
			diff.AddTx(tx, status.Committed)
			require.NoError(diff.Apply(env.state))
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			// Advance time to the staker's start time.
			env.clk.Set(subnetVdr1StartTime)
			tx = newAdvanceTimeTx(t, subnetVdr1StartTime)

			diff, err = state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)
			onCommitState, _, err := executeProposalTx(t, env, diff, tx)
			require.NoError(err)

			require.NoError(onCommitState.Apply(env.state))

			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())
			_, ok := env.config.Validators.GetValidator(subnetID, subnetValidatorNodeID)
			require.True(ok)
		})
	}
}

// Ensure advancing time to a pending delegator's start time adds its stake to
// its validator's weight.
func TestAdvanceTimeTxDelegatorStakerWeight(t *testing.T) {
	tests := []struct {
		name              string
		validatorDuration time.Duration
		delegatorDuration time.Duration
	}{
		{
			name:              "short_delegation_to_long_validator",
			validatorDuration: defaultMaxStakingDuration,
			delegatorDuration: time.Second,
		},
		{
			name:              "min_stake_duration_delegation",
			validatorDuration: defaultMinStakingDuration,
			delegatorDuration: defaultMinStakingDuration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, upgradetest.ApricotPhase5)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()
			dummyHeight := uint64(1)

			// Add a pending validator and advance time to its start time
			validatorStartTime := genesistest.DefaultValidatorStartTime.Add(time.Second)
			nodeID := ids.GenerateTestNodeID()
			addPendingValidator(
				t,
				env,
				validatorStartTime,
				validatorStartTime.Add(tt.validatorDuration),
				nodeID,
				[]*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)
			onCommitState, _, err := executeProposalTx(t, env, diff, newAdvanceTimeTx(t, validatorStartTime))
			require.NoError(err)
			require.NoError(onCommitState.Apply(env.state))
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			require.Equal(env.config.MinValidatorStake, env.config.Validators.GetWeight(constants.PrimaryNetworkID, nodeID))

			// Add a pending delegator
			delegatorStartTime := validatorStartTime.Add(time.Second)
			wallet := newWallet(t, env, walletConfig{})
			addDelegatorTx, err := wallet.IssueAddDelegatorTx(
				&platform.Validator{
					NodeID: nodeID,
					Start:  uint64(delegatorStartTime.Unix()),
					End:    uint64(delegatorStartTime.Add(tt.delegatorDuration).Unix()),
					Wght:   env.config.MinDelegatorStake,
				},
				newOwner(),
			)
			require.NoError(err)

			staker, err := state.NewPendingStaker(
				addDelegatorTx.ID(),
				addDelegatorTx.Unsigned.(*platform.AddDelegatorTx),
			)
			require.NoError(err)

			diff, err = state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)
			diff.PutPendingDelegator(staker)
			diff.AddTx(addDelegatorTx, status.Committed)
			require.NoError(diff.Apply(env.state))
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			// Advance time to the delegator's start time
			diff, err = state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)
			onCommitState, _, err = executeProposalTx(t, env, diff, newAdvanceTimeTx(t, delegatorStartTime))
			require.NoError(err)
			require.NoError(onCommitState.Apply(env.state))
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			require.Equal(env.config.MinDelegatorStake+env.config.MinValidatorStake, env.config.Validators.GetWeight(constants.PrimaryNetworkID, nodeID))
		})
	}
}

// Ensure marshaling/unmarshaling works
func TestAdvanceTimeTxUnmarshal(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	chainTime := env.state.GetTimestamp()
	tx := newAdvanceTimeTx(t, chainTime.Add(time.Second))

	bytes, err := platform.Codec.Marshal(platform.CodecVersion, tx)
	require.NoError(err)

	var unmarshaledTx platform.Tx
	_, err = platform.Codec.Unmarshal(bytes, &unmarshaledTx)
	require.NoError(err)

	require.Equal(
		tx.Unsigned.(*platform.AdvanceTimeTx).Time,
		unmarshaledTx.Unsigned.(*platform.AdvanceTimeTx).Time,
	)
}

func addPendingValidator(
	t testing.TB,
	env *environment,
	startTime time.Time,
	endTime time.Time,
	nodeID ids.NodeID,
	keys []*secp256k1.PrivateKey,
) *platform.Tx {
	require := require.New(t)

	wallet := newWallet(t, env, walletConfig{
		keys: keys,
	})
	addPendingValidatorTx, err := wallet.IssueAddValidatorTx(
		&platform.Validator{
			NodeID: nodeID,
			Start:  uint64(startTime.Unix()),
			End:    uint64(endTime.Unix()),
			Wght:   env.config.MinValidatorStake,
		},
		newOwner(),
		reward.PercentDenominator,
	)
	require.NoError(err)

	staker, err := state.NewPendingStaker(
		addPendingValidatorTx.ID(),
		addPendingValidatorTx.Unsigned.(*platform.AddValidatorTx),
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	require.NoError(diff.PutPendingValidator(staker))
	diff.AddTx(addPendingValidatorTx, status.Committed)
	require.NoError(diff.Apply(env.state))
	dummyHeight := uint64(1)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())
	return addPendingValidatorTx
}
