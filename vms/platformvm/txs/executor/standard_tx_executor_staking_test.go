// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// TestStandardExecutorAddValidatorTxErrors verifies the failure cases of
// [platform.AddValidatorTx] execution.
func TestStandardExecutorAddValidatorTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.Banff)

	var (
		nodeID       = ids.GenerateTestNodeID()
		startTime    = genesistest.DefaultValidatorStartTime.Add(time.Second)
		endTime      = startTime.Add(env.config.MinStakeDuration)
		rewardsOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		}
	)

	// putPendingValidator adds nodeID to the primary network's pending
	// validator set by executing an [platform.AddValidatorTx] funded by a different
	// key than the tx under test.
	putPendingValidator := func(diff *state.Diff) *platform.Tx {
		wallet := newWallet(t, env, walletConfig{
			keys: genesistest.DefaultFundedKeys[1:2],
		})
		tx, err := wallet.IssueAddValidatorTx(
			&platform.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			rewardsOwner,
			reward.PercentDenominator,
		)
		require.NoError(t, err)

		_, _, _, err = StandardTx(
			&env.backend,
			state.PickFeeCalculator(env.config, diff),
			tx,
			diff,
		)
		require.NoError(t, err)
		diff.AddTx(tx, status.Committed)
		return tx
	}

	tests := []struct {
		name        string
		want        error
		updateTx    func(*platform.Tx)
		updateState func(*state.Diff)
	}{
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddValidatorTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "empty_node_id",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddValidatorTx).Validator.NodeID = ids.EmptyNodeID
			},
			want: errEmptyNodeID,
		},
		{
			name: "start_time_before_current_timestamp",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddValidatorTx).Validator.Start = genesistest.DefaultValidatorStartTimeUnix - 1
			},
			want: ErrTimestampNotBeforeStartTime,
		},
		{
			name: "already_in_current_validator_set",
			updateTx: func(tx *platform.Tx) {
				// Start after the validator has been promoted to the current set
				validator := &tx.Unsigned.(*platform.AddValidatorTx).Validator
				validator.Start = uint64(startTime.Add(time.Second).Unix())
				validator.End = uint64(endTime.Add(time.Second).Unix())
			},
			updateState: func(diff *state.Diff) {
				putPendingValidator(diff)

				// Advance chain time to the validator's start time to promote
				// it to the current set
				_, err := AdvanceTimeTo(&env.backend, diff, startTime)
				require.NoError(t, err)
			},
			want: errAlreadyValidator,
		},
		{
			name: "already_in_pending_validator_set",
			updateState: func(diff *state.Diff) {
				putPendingValidator(diff)
			},
			want: errAlreadyValidator,
		},
		{
			name: "insufficient_balance_to_cover_stake",
			updateState: func(diff *state.Diff) {
				// Remove all UTXOs owned by the tx's key
				utxoIDs, err := env.state.UTXOIDs(genesistest.DefaultFundedKeys[0].Address().Bytes(), ids.Empty, math.MaxInt32)
				require.NoError(t, err)

				for _, utxoID := range utxoIDs {
					diff.DeleteUTXO(utxoID)
				}
			},
			want: errFlowCheckFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The tx stakes AVAX, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{
				keys: genesistest.DefaultFundedKeys[:1],
			})
			tx, err := wallet.IssueAddValidatorTx(
				&platform.Validator{
					NodeID: nodeID,
					Start:  uint64(startTime.Unix()),
					End:    uint64(endTime.Unix()),
					Wght:   env.config.MinValidatorStake,
				},
				rewardsOwner,
				reward.PercentDenominator,
			)
			require.NoError(t, err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, got)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			if tt.updateState != nil {
				tt.updateState(diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(t, got, tt.want)
		})
	}
}

// TestStandardExecutorAddValidatorTx verifies the successful execution of an
// [platform.AddValidatorTx].
func TestStandardExecutorAddValidatorTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Banff)
	wallet := newWallet(t, env, walletConfig{})

	var (
		nodeID    = ids.GenerateTestNodeID()
		startTime = genesistest.DefaultValidatorStartTime.Add(time.Second)
		endTime   = startTime.Add(env.config.MinStakeDuration)
	)

	stx, err := wallet.IssueAddValidatorTx(
		&platform.Validator{
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

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, diff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		stx,
		diff,
	)
	require.NoError(err)

	requireBaseTxApplied(t, env, diff, feeCalculator, stx)

	// assert the validator was added to the pending validator set
	gotStaker, err := diff.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.Equal(stx.ID(), gotStaker.TxID)
	require.Equal(env.config.MinValidatorStake, gotStaker.Weight)
	require.Equal(startTime.Unix(), gotStaker.StartTime.Unix())
	require.Equal(endTime.Unix(), gotStaker.EndTime.Unix())
}

// TestStandardExecutorAddDelegatorTxErrors verifies the failure cases of
// [platform.AddDelegatorTx] execution.
func TestStandardExecutorAddDelegatorTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	// AddDelegatorTx is executed as a standard tx post-Banff, and the
	// over-delegation cap depends on ApricotPhase3 being active.
	env.config.UpgradeConfig.ApricotPhase3Time = genesistest.DefaultValidatorStartTime
	env.config.UpgradeConfig.BanffTime = env.state.GetTimestamp()

	var (
		currentTimestamp = env.state.GetTimestamp()
		rewardsOwner     = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		}

		genesisNodeID      = genesistest.DefaultNodeIDs[0]
		validatorID        = ids.GenerateTestNodeID()
		validatorStartTime = genesistest.DefaultValidatorStartTime.Add(5 * time.Second)
		validatorEndTime   = genesistest.DefaultValidatorEndTime.Add(-5 * time.Second)
	)

	// putCurrentValidator adds validatorID to the primary network's current
	// validator set with the given stake.
	putCurrentValidator := func(diff *state.Diff, weight uint64) {
		wallet := newWallet(t, env, walletConfig{
			keys: genesistest.DefaultFundedKeys[:1],
		})
		tx, err := wallet.IssueAddValidatorTx(
			&platform.Validator{
				NodeID: validatorID,
				Start:  uint64(validatorStartTime.Unix()),
				End:    uint64(validatorEndTime.Unix()),
				Wght:   weight,
			},
			rewardsOwner,
			reward.PercentDenominator,
		)
		require.NoError(t, err)

		addValidatorTx := tx.Unsigned.(*platform.AddValidatorTx)
		staker, err := state.NewCurrentStaker(
			tx.ID(),
			addValidatorTx,
			validatorStartTime,
			addValidatorTx.EndTime(),
			addValidatorTx.Weight(),
			0,
		)
		require.NoError(t, err)

		require.NoError(t, diff.PutCurrentValidator(staker))
		diff.AddTx(tx, status.Committed)
	}

	tests := []struct {
		name        string
		want        error
		updateTx    func(*platform.Tx)
		updateState func(*state.Diff)
	}{
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddDelegatorTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "validator_stops_before_delegator",
			updateTx: func(tx *platform.Tx) {
				validator := &tx.Unsigned.(*platform.AddDelegatorTx).Validator
				validator.NodeID = genesisNodeID
				validator.Start = genesistest.DefaultValidatorStartTimeUnix + 1
				validator.End = genesistest.DefaultValidatorEndTimeUnix + 1
			},
			want: errPeriodMismatch,
		},
		{
			name: "validator_not_in_current_or_pending_validator_sets",
			want: database.ErrNotFound,
		},
		{
			name: "delegator_starts_before_validator",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddDelegatorTx).Validator.Start = uint64(validatorStartTime.Add(-time.Second).Unix())
			},
			updateState: func(diff *state.Diff) {
				putCurrentValidator(diff, env.config.MinValidatorStake)
			},
			want: errPeriodMismatch,
		},
		{
			name: "delegator_stops_after_validator",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddDelegatorTx).Validator.End = uint64(validatorEndTime.Add(time.Second).Unix())
			},
			updateState: func(diff *state.Diff) {
				putCurrentValidator(diff, env.config.MinValidatorStake)
			},
			want: errPeriodMismatch,
		},
		{
			name: "starts_delegating_at_current_timestamp",
			updateTx: func(tx *platform.Tx) {
				validator := &tx.Unsigned.(*platform.AddDelegatorTx).Validator
				validator.NodeID = genesisNodeID
				validator.Start = uint64(currentTimestamp.Unix())
				validator.End = genesistest.DefaultValidatorEndTimeUnix
			},
			want: ErrTimestampNotBeforeStartTime,
		},
		{
			name: "fee_paying_key_has_no_funds",
			updateTx: func(tx *platform.Tx) {
				validator := &tx.Unsigned.(*platform.AddDelegatorTx).Validator
				validator.NodeID = genesisNodeID
				validator.Start = genesistest.DefaultValidatorStartTimeUnix + 1
				validator.End = genesistest.DefaultValidatorEndTimeUnix
			},
			updateState: func(diff *state.Diff) {
				// Remove all UTXOs owned by the tx's key
				utxoIDs, err := env.state.UTXOIDs(genesistest.DefaultFundedKeys[0].Address().Bytes(), ids.Empty, math.MaxInt32)
				require.NoError(t, err)

				for _, utxoID := range utxoIDs {
					diff.DeleteUTXO(utxoID)
				}
			},
			want: errFlowCheckFailed,
		},
		{
			name: "over_delegation",
			updateState: func(diff *state.Diff) {
				putCurrentValidator(diff, env.config.MaxValidatorStake)
			},
			want: ErrOverDelegated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The tx stakes AVAX, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{
				keys: genesistest.DefaultFundedKeys[:1],
			})
			tx, err := wallet.IssueAddDelegatorTx(
				&platform.Validator{
					NodeID: validatorID,
					Start:  uint64(validatorStartTime.Unix()),
					End:    uint64(validatorEndTime.Unix()),
					Wght:   env.config.MinDelegatorStake,
				},
				rewardsOwner,
			)
			require.NoError(t, err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, got)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			if tt.updateState != nil {
				tt.updateState(diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(t, got, tt.want)
		})
	}
}

// TestStandardExecutorAddDelegatorTx verifies the successful execution of an
// [platform.AddDelegatorTx].
func TestStandardExecutorAddDelegatorTx(t *testing.T) {
	tests := []struct {
		name            string
		ap3Time         time.Time
		validatorWeight func(*environment) uint64
	}{
		{
			name:    "delegation_period_matches_validator_period",
			ap3Time: genesistest.DefaultValidatorStartTime,
			validatorWeight: func(env *environment) uint64 {
				return env.config.MinValidatorStake
			},
		},
		{
			// Pre-ApricotPhase3, the delegation cap isn't bounded by the max
			// validator stake, so delegating to a validator staking the max
			// isn't over-delegation.
			name:    "over_delegation_before_apricot_phase_3",
			ap3Time: genesistest.DefaultValidatorEndTime,
			validatorWeight: func(env *environment) uint64 {
				return env.config.MaxValidatorStake
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			env := newEnvironment(t, upgradetest.ApricotPhase5)
			env.config.UpgradeConfig.ApricotPhase3Time = tt.ap3Time
			env.config.UpgradeConfig.BanffTime = env.state.GetTimestamp()

			var (
				rewardsOwner = &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				}
				validatorID        = ids.GenerateTestNodeID()
				validatorStartTime = genesistest.DefaultValidatorStartTime.Add(5 * time.Second)
				validatorEndTime   = genesistest.DefaultValidatorEndTime.Add(-5 * time.Second)
			)

			// Fund the validator and the delegator from different keys so the
			// delegator's wallet UTXO view stays consistent with env.state.
			validatorWallet := newWallet(t, env, walletConfig{
				keys: genesistest.DefaultFundedKeys[1:2],
			})
			addValidatorTx, err := validatorWallet.IssueAddValidatorTx(
				&platform.Validator{
					NodeID: validatorID,
					Start:  uint64(validatorStartTime.Unix()),
					End:    uint64(validatorEndTime.Unix()),
					Wght:   tt.validatorWeight(env),
				},
				rewardsOwner,
				reward.PercentDenominator,
			)
			require.NoError(err)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			// Add the validator to the current validator set
			unsignedAddValidatorTx := addValidatorTx.Unsigned.(*platform.AddValidatorTx)
			validator, err := state.NewCurrentStaker(
				addValidatorTx.ID(),
				unsignedAddValidatorTx,
				validatorStartTime,
				unsignedAddValidatorTx.EndTime(),
				unsignedAddValidatorTx.Weight(),
				0,
			)
			require.NoError(err)
			require.NoError(diff.PutCurrentValidator(validator))
			diff.AddTx(addValidatorTx, status.Committed)

			wallet := newWallet(t, env, walletConfig{
				keys: genesistest.DefaultFundedKeys[:1],
			})
			stx, err := wallet.IssueAddDelegatorTx(
				&platform.Validator{
					NodeID: validatorID,
					Start:  uint64(validatorStartTime.Unix()),
					End:    uint64(validatorEndTime.Unix()),
					Wght:   env.config.MinDelegatorStake,
				},
				rewardsOwner,
			)
			require.NoError(err)

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				stx,
				diff,
			)
			require.NoError(err)

			requireBaseTxApplied(t, env, diff, feeCalculator, stx)

			// assert the delegator was added to the pending delegator set
			delegatorIt, err := diff.GetPendingDelegatorIterator(constants.PrimaryNetworkID, validatorID)
			require.NoError(err)

			gotDelegators := iterator.ToSlice(delegatorIt)
			require.Len(gotDelegators, 1)
			require.Equal(stx.ID(), gotDelegators[0].TxID)
			require.Equal(env.config.MinDelegatorStake, gotDelegators[0].Weight)
		})
	}
}

// TestStandardExecutorAddSubnetValidatorTxErrors verifies the failure cases
// of [platform.AddSubnetValidatorTx] execution.
func TestStandardExecutorAddSubnetValidatorTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.ApricotPhase5)

	var (
		nodeID   = genesistest.DefaultNodeIDs[0]
		subnetID = testSubnet1.ID()

		// A primary network validator that is only in the pending set
		pendingValidatorID        = ids.GenerateTestNodeID()
		pendingValidatorStartTime = genesistest.DefaultValidatorStartTime.Add(10 * time.Second)
		pendingValidatorEndTime   = pendingValidatorStartTime.Add(5 * env.config.MinStakeDuration)

		newTimestamp = genesistest.DefaultValidatorStartTime.Add(2 * time.Second)
	)

	// putPendingValidator adds pendingValidatorID to the primary network's
	// pending validator set.
	putPendingValidator := func(diff *state.Diff) {
		wallet := newWallet(t, env, walletConfig{})
		tx, err := wallet.IssueAddValidatorTx(
			&platform.Validator{
				NodeID: pendingValidatorID,
				Start:  uint64(pendingValidatorStartTime.Unix()),
				End:    uint64(pendingValidatorEndTime.Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
			reward.PercentDenominator,
		)
		require.NoError(t, err)

		_, _, _, err = StandardTx(
			&env.backend,
			state.PickFeeCalculator(env.config, diff),
			tx,
			diff,
		)
		require.NoError(t, err)
		diff.AddTx(tx, status.Committed)
	}

	// putSubnetValidator adds nodeID to the subnet's pending validator set.
	putSubnetValidator := func(diff *state.Diff) {
		wallet := newWallet(t, env, walletConfig{})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: nodeID,
					Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
					End:    genesistest.DefaultValidatorEndTimeUnix,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(t, err)

		_, _, _, err = StandardTx(
			&env.backend,
			state.PickFeeCalculator(env.config, diff),
			tx,
			diff,
		)
		require.NoError(t, err)
		diff.AddTx(tx, status.Committed)
	}

	tests := []struct {
		name        string
		want        error
		updateTx    func(*platform.Tx)
		updateState func(*state.Diff)
	}{
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddSubnetValidatorTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "subnet_validator_stops_after_primary_network",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddSubnetValidatorTx).Validator.End = genesistest.DefaultValidatorEndTimeUnix + 1
			},
			want: errPeriodMismatch,
		},
		{
			name: "node_not_in_pending_or_current_primary_network_validator_sets",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddSubnetValidatorTx).Validator.NodeID = ids.GenerateTestNodeID()
			},
			want: errNotValidator,
		},
		{
			name: "subnet_validator_starts_before_pending_primary_network_validator",
			updateTx: func(tx *platform.Tx) {
				validator := &tx.Unsigned.(*platform.AddSubnetValidatorTx).Validator
				validator.NodeID = pendingValidatorID
				validator.Start = uint64(pendingValidatorStartTime.Unix()) - 1
				validator.End = uint64(pendingValidatorEndTime.Unix())
			},
			updateState: putPendingValidator,
			want:        errPeriodMismatch,
		},
		{
			name: "subnet_validator_stops_after_pending_primary_network_validator",
			updateTx: func(tx *platform.Tx) {
				validator := &tx.Unsigned.(*platform.AddSubnetValidatorTx).Validator
				validator.NodeID = pendingValidatorID
				validator.Start = uint64(pendingValidatorStartTime.Unix())
				validator.End = uint64(pendingValidatorEndTime.Unix()) + 1
			},
			updateState: putPendingValidator,
			want:        errPeriodMismatch,
		},
		{
			name: "subnet_validator_starts_at_current_timestamp",
			updateTx: func(tx *platform.Tx) {
				validator := &tx.Unsigned.(*platform.AddSubnetValidatorTx).Validator
				validator.Start = uint64(newTimestamp.Unix())
				validator.End = uint64(newTimestamp.Add(env.config.MinStakeDuration).Unix())
			},
			updateState: func(diff *state.Diff) {
				diff.SetTimestamp(newTimestamp)
			},
			want: ErrTimestampNotBeforeStartTime,
		},
		{
			name: "duplicate_subnet_auth_signature_indices",
			updateTx: func(tx *platform.Tx) {
				input := tx.Unsigned.(*platform.AddSubnetValidatorTx).SubnetAuth.(*secp256k1fx.Input)
				input.SigIndices = append(input.SigIndices, input.SigIndices[0])
			},
			want: secp256k1fx.ErrInputIndicesNotSortedUnique,
		},
		{
			name: "too_few_subnet_auth_signatures",
			updateTx: func(tx *platform.Tx) {
				input := tx.Unsigned.(*platform.AddSubnetValidatorTx).SubnetAuth.(*secp256k1fx.Input)
				input.SigIndices = input.SigIndices[1:]
			},
			want: errUnauthorizedModification,
		},
		{
			name: "subnet_auth_signature_from_non_control_key",
			updateTx: func(tx *platform.Tx) {
				sig, err := genesistest.DefaultFundedKeys[3].SignHash(hashing.ComputeHash256(tx.Unsigned.Bytes()))
				require.NoError(t, err)
				copy(tx.Creds[0].(*secp256k1fx.Credential).Sigs[0][:], sig)
			},
			want: errUnauthorizedModification,
		},
		{
			name: "node_already_a_subnet_validator",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddSubnetValidatorTx).Validator.Start = genesistest.DefaultValidatorStartTimeUnix + 2
			},
			updateState: putSubnetValidator,
			want:        ErrDuplicateValidator,
		},
		{
			name: "invalid_after_subnet_conversion",
			updateState: func(diff *state.Diff) {
				diff.SetSubnetToL1Conversion(subnetID, state.SubnetToL1Conversion{
					ConversionID: ids.GenerateTestID(),
					ChainID:      ids.GenerateTestID(),
					Addr:         []byte("address"),
				})
			},
			want: errIsImmutable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueAddSubnetValidatorTx(
				&platform.SubnetValidator{
					Validator: platform.Validator{
						NodeID: nodeID,
						Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
						End:    genesistest.DefaultValidatorEndTimeUnix,
						Wght:   genesistest.DefaultValidatorWeight,
					},
					Subnet: subnetID,
				},
			)
			require.NoError(t, err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, got)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			if tt.updateState != nil {
				tt.updateState(diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(t, got, tt.want)
		})
	}
}

// TestStandardExecutorAddSubnetValidatorTx verifies the successful execution
// of an [platform.AddSubnetValidatorTx].
func TestStandardExecutorAddSubnetValidatorTx(t *testing.T) {
	env := newEnvironment(t, upgradetest.ApricotPhase5)

	var (
		subnetID = testSubnet1.ID()

		// A primary network validator that is only in the pending set
		pendingValidatorID        = ids.GenerateTestNodeID()
		pendingValidatorStartTime = genesistest.DefaultValidatorStartTime.Add(10 * time.Second)
		pendingValidatorEndTime   = pendingValidatorStartTime.Add(5 * env.config.MinStakeDuration)
	)

	tests := []struct {
		name        string
		validator   platform.Validator
		updateState func(*state.Diff)
	}{
		{
			name: "validation_period_within_current_primary_network_validator_period",
			validator: platform.Validator{
				NodeID: genesistest.DefaultNodeIDs[0],
				Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
				End:    genesistest.DefaultValidatorEndTimeUnix,
				Wght:   genesistest.DefaultValidatorWeight,
			},
		},
		{
			name: "validation_period_matches_pending_primary_network_validator_period",
			validator: platform.Validator{
				NodeID: pendingValidatorID,
				Start:  uint64(pendingValidatorStartTime.Unix()),
				End:    uint64(pendingValidatorEndTime.Unix()),
				Wght:   genesistest.DefaultValidatorWeight,
			},
			updateState: func(diff *state.Diff) {
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddValidatorTx(
					&platform.Validator{
						NodeID: pendingValidatorID,
						Start:  uint64(pendingValidatorStartTime.Unix()),
						End:    uint64(pendingValidatorEndTime.Unix()),
						Wght:   env.config.MinValidatorStake,
					},
					&secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
					reward.PercentDenominator,
				)
				require.NoError(t, err)

				_, _, _, err = StandardTx(
					&env.backend,
					state.PickFeeCalculator(env.config, diff),
					tx,
					diff,
				)
				require.NoError(t, err)
				diff.AddTx(tx, status.Committed)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			wallet := newWallet(t, env, walletConfig{})
			stx, err := wallet.IssueAddSubnetValidatorTx(
				&platform.SubnetValidator{
					Validator: tt.validator,
					Subnet:    subnetID,
				},
			)
			require.NoError(err)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			if tt.updateState != nil {
				tt.updateState(diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				stx,
				diff,
			)
			require.NoError(err)

			requireBaseTxApplied(t, env, diff, feeCalculator, stx)

			// assert the validator was added to the subnet's pending validator
			// set
			gotStaker, err := diff.GetPendingValidator(subnetID, tt.validator.NodeID)
			require.NoError(err)
			require.Equal(stx.ID(), gotStaker.TxID)
			require.Equal(tt.validator.Wght, gotStaker.Weight)
			require.Equal(tt.validator.Start, uint64(gotStaker.StartTime.Unix()))
			require.Equal(tt.validator.End, uint64(gotStaker.EndTime.Unix()))
		})
	}
}

// TestStandardExecutorAddPermissionlessValidatorTxErrors verifies the failure
// cases of [platform.AddPermissionlessValidatorTx] execution.
func TestStandardExecutorAddPermissionlessValidatorTxErrors(t *testing.T) {
	// Helicon lowers the primary network's minimum staking duration, so run
	// against the fork right before it.
	env := newEnvironment(t, upgradetest.Granite)
	wallet := newWallet(t, env, walletConfig{})

	sk, err := localsigner.New()
	require.NoError(t, err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	tests := []struct {
		name     string
		want     error
		updateTx func(*platform.Tx)
	}{
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddPermissionlessValidatorTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "stake_duration_shorter_than_pre_helicon_minimum",
			updateTx: func(tx *platform.Tx) {
				validator := &tx.Unsigned.(*platform.AddPermissionlessValidatorTx).Validator
				validator.End = uint64(env.state.GetTimestamp().Add(env.config.HeliconMinStakeDuration).Unix())
			},
			want: errStakeTooShort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := wallet.IssueAddPermissionlessValidatorTx(
				&platform.SubnetValidator{
					Validator: platform.Validator{
						NodeID: ids.GenerateTestNodeID(),
						End:    uint64(env.state.GetTimestamp().Add(env.config.MinStakeDuration).Unix()),
						Wght:   env.config.MinValidatorStake,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				pop,
				env.ctx.AVAXAssetID,
				rewardsOwner,
				rewardsOwner,
				reward.PercentDenominator,
			)
			require.NoError(t, err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, got)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(t, got, tt.want)
		})
	}
}

// TestStandardExecutorAddPermissionlessValidatorTx verifies the successful
// execution of an [platform.AddPermissionlessValidatorTx].
func TestStandardExecutorAddPermissionlessValidatorTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Helicon)
	wallet := newWallet(t, env, walletConfig{})

	sk, err := localsigner.New()
	require.NoError(err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	var (
		nodeID       = ids.GenerateTestNodeID()
		endTime      = env.state.GetTimestamp().Add(env.config.HeliconMinStakeDuration)
		rewardsOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		}
	)

	// Stake for the Helicon minimum staking duration
	stx, err := wallet.IssueAddPermissionlessValidatorTx(
		&platform.SubnetValidator{
			Validator: platform.Validator{
				NodeID: nodeID,
				End:    uint64(endTime.Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop,
		env.ctx.AVAXAssetID,
		rewardsOwner,
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, diff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		stx,
		diff,
	)
	require.NoError(err)

	requireBaseTxApplied(t, env, diff, feeCalculator, stx)

	// assert the validator was added to the current validator set
	gotStaker, err := diff.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.Equal(stx.ID(), gotStaker.TxID)
	require.Equal(env.config.MinValidatorStake, gotStaker.Weight)
	require.Equal(endTime.Unix(), gotStaker.EndTime.Unix())
}

// TestStandardExecutorAddPermissionlessDelegatorTxErrors verifies the failure
// cases of [platform.AddPermissionlessDelegatorTx] execution.
func TestStandardExecutorAddPermissionlessDelegatorTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)

	var (
		nodeID       = genesistest.DefaultNodeIDs[0]
		endTime      = env.state.GetTimestamp().Add(env.config.MinStakeDuration)
		rewardsOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		}
	)

	// setWeight updates the delegator's weight and its stake output
	// consistently, as the two must match syntactically.
	setWeight := func(tx *platform.Tx, weight uint64) {
		unsignedTx := tx.Unsigned.(*platform.AddPermissionlessDelegatorTx)
		unsignedTx.Validator.Wght = weight
		unsignedTx.StakeOuts[0].Out.(*secp256k1fx.TransferOutput).Amt = weight
	}

	tests := []struct {
		name     string
		want     error
		updateTx func(*platform.Tx)
	}{
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddPermissionlessDelegatorTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "weight_too_small",
			updateTx: func(tx *platform.Tx) {
				setWeight(tx, env.config.MinDelegatorStake-1)
			},
			want: errWeightTooSmall,
		},
		{
			name: "stake_too_short",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddPermissionlessDelegatorTx).Validator.End = uint64(env.state.GetTimestamp().Add(time.Second).Unix())
			},
			want: errStakeTooShort,
		},
		{
			name: "stake_too_long",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddPermissionlessDelegatorTx).Validator.End = uint64(env.state.GetTimestamp().Add(env.config.MaxStakeDuration + time.Second).Unix())
			},
			want: ErrStakeTooLong,
		},
		{
			name: "wrong_staked_asset_id",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddPermissionlessDelegatorTx).StakeOuts[0].Asset.ID = ids.GenerateTestID()
			},
			want: errWrongStakedAssetID,
		},
		{
			name: "validator_not_found",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddPermissionlessDelegatorTx).Validator.NodeID = ids.GenerateTestNodeID()
			},
			want: database.ErrNotFound,
		},
		{
			name: "delegator_stops_after_validator",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.AddPermissionlessDelegatorTx).Validator.End = genesistest.DefaultValidatorEndTimeUnix + 1
			},
			want: errPeriodMismatch,
		},
		{
			name: "over_delegation",
			updateTx: func(tx *platform.Tx) {
				setWeight(tx, env.config.MaxValidatorStake)
			},
			want: ErrOverDelegated,
		},
		{
			name: "flow_checker_failed",
			updateTx: func(tx *platform.Tx) {
				// Produce more AVAX than the tx consumes
				unsignedTx := tx.Unsigned.(*platform.AddPermissionlessDelegatorTx)
				unsignedTx.Outs = append(unsignedTx.Outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64 / 2,
					},
				})
			},
			want: errFlowCheckFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The tx stakes AVAX, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueAddPermissionlessDelegatorTx(
				&platform.SubnetValidator{
					Validator: platform.Validator{
						NodeID: nodeID,
						End:    uint64(endTime.Unix()),
						Wght:   env.config.MinDelegatorStake,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				env.ctx.AVAXAssetID,
				rewardsOwner,
			)
			require.NoError(t, err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, got)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(t, got, tt.want)
		})
	}
}

// TestStandardExecutorAddPermissionlessDelegatorTx verifies the successful
// execution of an [platform.AddPermissionlessDelegatorTx].
func TestStandardExecutorAddPermissionlessDelegatorTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	wallet := newWallet(t, env, walletConfig{})

	var (
		nodeID  = genesistest.DefaultNodeIDs[0]
		endTime = env.state.GetTimestamp().Add(env.config.MinStakeDuration)
	)

	stx, err := wallet.IssueAddPermissionlessDelegatorTx(
		&platform.SubnetValidator{
			Validator: platform.Validator{
				NodeID: nodeID,
				End:    uint64(endTime.Unix()),
				Wght:   env.config.MinDelegatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		env.ctx.AVAXAssetID,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, diff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		stx,
		diff,
	)
	require.NoError(err)

	requireBaseTxApplied(t, env, diff, feeCalculator, stx)

	// assert the delegator was added to the current delegator set
	delegatorIt, err := diff.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)

	gotDelegators := iterator.ToSlice(delegatorIt)
	require.Len(gotDelegators, 1)
	require.Equal(stx.ID(), gotDelegators[0].TxID)
	require.Equal(env.config.MinDelegatorStake, gotDelegators[0].Weight)
	require.Equal(endTime.Unix(), gotDelegators[0].EndTime.Unix())
}

// TestStandardExecutorRemoveSubnetValidatorTxErrors verifies the failure cases
// of [platform.RemoveSubnetValidatorTx] execution.
func TestStandardExecutorRemoveSubnetValidatorTxErrors(t *testing.T) {
	var (
		env    = newEnvironment(t, upgradetest.Durango)
		wallet = newWallet(t, env, walletConfig{})
		nodeID = genesistest.DefaultNodeIDs[0]
	)

	testSubnetID := ids.GenerateTestID()

	tests := []struct {
		name         string
		want         error
		updateTx     func(*platform.Tx)
		updateStaker func(*state.Staker)
	}{
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.RemoveSubnetValidatorTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "node_is_not_a_validator_of_the_subnet",
			updateStaker: func(staker *state.Staker) {
				staker.NodeID = ids.GenerateTestNodeID()
			},
			want: errNotValidator,
		},
		{
			name: "validator_is_permissionless",
			updateStaker: func(staker *state.Staker) {
				staker.Priority = platform.SubnetPermissionlessValidatorCurrentPriority
			},
			want: errRemovePermissionlessValidator,
		},
		{
			name: "cannot_find_subnet",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.RemoveSubnetValidatorTx).Subnet = testSubnetID
			},
			updateStaker: func(staker *state.Staker) {
				staker.SubnetID = testSubnetID
			},
			want: database.ErrNotFound,
		},
		{
			name: "tx_has_no_credentials",
			updateTx: func(tx *platform.Tx) {
				tx.Creds = nil
			},
			want: errWrongNumberOfCredentials,
		},
		{
			name: "no_permission_to_remove_validator",
			updateTx: func(tx *platform.Tx) {
				// Remove a signature from the subnet auth credential
				cred := tx.Creds[len(tx.Creds)-1].(*secp256k1fx.Credential)
				cred.Sigs = cred.Sigs[1:]
			},
			want: errUnauthorizedModification,
		},
		{
			name: "flow_checker_failed",
			updateTx: func(tx *platform.Tx) {
				// Produce more AVAX than the tx consumes
				unsignedTx := tx.Unsigned.(*platform.RemoveSubnetValidatorTx)
				unsignedTx.Outs = append(unsignedTx.Outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64 / 2,
					},
				})
			},
			want: errFlowCheckFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := wallet.IssueRemoveSubnetValidatorTx(
				nodeID,
				testSubnet1.ID(),
			)
			require.NoError(t, err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, got)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			staker := &state.Staker{
				TxID:     ids.GenerateTestID(),
				NodeID:   nodeID,
				SubnetID: testSubnet1.ID(),
				Priority: platform.SubnetPermissionedValidatorCurrentPriority,
			}
			if tt.updateStaker != nil {
				tt.updateStaker(staker)
			}
			require.NoError(t, diff.PutCurrentValidator(staker))

			feeCalculator := state.PickFeeCalculator(env.config, env.state)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(t, got, tt.want)
		})
	}
}

// TestStandardExecutorRemoveSubnetValidatorTx verifies the successful
// execution of a [platform.RemoveSubnetValidatorTx], including on a subnet
// converted to an L1.
func TestStandardExecutorRemoveSubnetValidatorTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Etna)
	wallet := newWallet(t, env, walletConfig{})
	nodeID := genesistest.DefaultNodeIDs[0]

	stx, err := wallet.IssueRemoveSubnetValidatorTx(
		nodeID,
		testSubnet1.ID(),
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(err)

	staker := &state.Staker{
		TxID:     ids.GenerateTestID(),
		NodeID:   nodeID,
		SubnetID: testSubnet1.ID(),
		Priority: platform.SubnetPermissionedValidatorCurrentPriority,
	}
	require.NoError(diff.PutCurrentValidator(staker))

	// Regression test: converted subnets can still remove permissioned
	// validators.
	diff.SetSubnetToL1Conversion(testSubnet1.ID(), state.SubnetToL1Conversion{
		ConversionID: ids.GenerateTestID(),
		ChainID:      ids.GenerateTestID(),
		Addr:         []byte("address"),
	})

	feeCalculator := state.PickFeeCalculator(env.config, env.state)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		stx,
		diff,
	)
	require.NoError(err)

	tx := stx.Unsigned.(*platform.RemoveSubnetValidatorTx)

	// assert that the validator was removed from the current validator set
	_, err = diff.GetCurrentValidator(tx.Subnet, tx.NodeID)
	require.ErrorIs(err, database.ErrNotFound)

	requireBaseTxApplied(t, env, diff, feeCalculator, stx)
}
