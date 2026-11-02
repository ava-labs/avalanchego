// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"math"
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

func TestProposalTxExecuteAddDelegator(t *testing.T) {
	dummyHeight := uint64(1)
	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
	nodeID := genesistest.DefaultNodeIDs[0]

	newValidatorID := ids.GenerateTestNodeID()
	newValidatorStartTime := uint64(genesistest.DefaultValidatorStartTime.Add(5 * time.Second).Unix())
	newValidatorEndTime := uint64(genesistest.DefaultValidatorEndTime.Add(-5 * time.Second).Unix())

	// [addMinStakeValidator] adds a new validator to the primary network's
	// pending validator set with the minimum staking amount
	addMinStakeValidator := func(env *environment) {
		require := require.New(t)

		wallet := newWallet(t, env, walletConfig{
			keys: genesistest.DefaultFundedKeys[:1],
		})

		tx, err := wallet.IssueAddValidatorTx(
			&platform.Validator{
				NodeID: newValidatorID,
				Start:  newValidatorStartTime,
				End:    newValidatorEndTime,
				Wght:   env.config.MinValidatorStake,
			},
			rewardsOwner,
			reward.PercentDenominator,
		)
		require.NoError(err)

		addValTx := tx.Unsigned.(*platform.AddValidatorTx)
		staker, err := state.NewCurrentStaker(
			tx.ID(),
			addValTx,
			addValTx.StartTime(),
			addValTx.EndTime(),
			addValTx.Weight(),
			0,
		)
		require.NoError(err)

		require.NoError(env.state.PutCurrentValidator(staker))
		env.state.AddTx(tx, status.Committed)
		env.state.SetHeight(dummyHeight)
		require.NoError(env.state.Commit())
	}

	// [addMaxStakeValidator] adds a new validator to the primary network's
	// pending validator set with the maximum staking amount
	addMaxStakeValidator := func(env *environment) {
		require := require.New(t)

		wallet := newWallet(t, env, walletConfig{
			keys: genesistest.DefaultFundedKeys[:1],
		})

		tx, err := wallet.IssueAddValidatorTx(
			&platform.Validator{
				NodeID: newValidatorID,
				Start:  newValidatorStartTime,
				End:    newValidatorEndTime,
				Wght:   env.config.MaxValidatorStake,
			},
			rewardsOwner,
			reward.PercentDenominator,
		)
		require.NoError(err)

		addValTx := tx.Unsigned.(*platform.AddValidatorTx)
		staker, err := state.NewCurrentStaker(
			tx.ID(),
			addValTx,
			addValTx.StartTime(),
			addValTx.EndTime(),
			addValTx.Weight(),
			0,
		)
		require.NoError(err)

		require.NoError(env.state.PutCurrentValidator(staker))
		env.state.AddTx(tx, status.Committed)
		env.state.SetHeight(dummyHeight)
		require.NoError(env.state.Commit())
	}

	env := newEnvironment(t, upgradetest.ApricotPhase5)
	currentTimestamp := env.state.GetTimestamp()

	type test struct {
		description string
		stakeAmount uint64
		startTime   uint64
		endTime     uint64
		nodeID      ids.NodeID
		feeKeys     []*secp256k1.PrivateKey
		setup       func(*environment)
		AP3Time     time.Time
		expectedErr error
	}

	tests := []test{
		{
			description: "validator stops validating earlier than delegator",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   genesistest.DefaultValidatorStartTimeUnix + 1,
			endTime:     genesistest.DefaultValidatorEndTimeUnix + 1,
			nodeID:      nodeID,
			feeKeys:     []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:       nil,
			AP3Time:     genesistest.DefaultValidatorStartTime,
			expectedErr: errPeriodMismatch,
		},
		{
			description: "validator not in the current or pending validator sets",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   uint64(genesistest.DefaultValidatorStartTime.Add(5 * time.Second).Unix()),
			endTime:     uint64(genesistest.DefaultValidatorEndTime.Add(-5 * time.Second).Unix()),
			nodeID:      newValidatorID,
			feeKeys:     []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:       nil,
			AP3Time:     genesistest.DefaultValidatorStartTime,
			expectedErr: database.ErrNotFound,
		},
		{
			description: "delegator starts before validator",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   newValidatorStartTime - 1, // start validating subnet before primary network
			endTime:     newValidatorEndTime,
			nodeID:      newValidatorID,
			feeKeys:     []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:       addMinStakeValidator,
			AP3Time:     genesistest.DefaultValidatorStartTime,
			expectedErr: errPeriodMismatch,
		},
		{
			description: "delegator stops before validator",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   newValidatorStartTime,
			endTime:     newValidatorEndTime + 1, // stop validating subnet after stopping validating primary network
			nodeID:      newValidatorID,
			feeKeys:     []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:       addMinStakeValidator,
			AP3Time:     genesistest.DefaultValidatorStartTime,
			expectedErr: errPeriodMismatch,
		},
		{
			description: "valid",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   newValidatorStartTime, // same start time as for primary network
			endTime:     newValidatorEndTime,   // same end time as for primary network
			nodeID:      newValidatorID,
			feeKeys:     []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:       addMinStakeValidator,
			AP3Time:     genesistest.DefaultValidatorStartTime,
			expectedErr: nil,
		},
		{
			description: "starts delegating at current timestamp",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   uint64(currentTimestamp.Unix()),
			endTime:     genesistest.DefaultValidatorEndTimeUnix,
			nodeID:      nodeID,
			feeKeys:     []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:       nil,
			AP3Time:     genesistest.DefaultValidatorStartTime,
			expectedErr: ErrTimestampNotBeforeStartTime,
		},
		{
			description: "tx fee paying key has no funds",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   genesistest.DefaultValidatorStartTimeUnix + 1,
			endTime:     genesistest.DefaultValidatorEndTimeUnix,
			nodeID:      nodeID,
			feeKeys:     []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[1]},
			setup: func(env *environment) { // Remove all UTXOs owned by keys[1]
				utxoIDs, err := env.state.UTXOIDs(
					genesistest.DefaultFundedKeys[1].Address().Bytes(),
					ids.Empty,
					math.MaxInt32)
				require.NoError(t, err)

				for _, utxoID := range utxoIDs {
					env.state.DeleteUTXO(utxoID)
				}
				env.state.SetHeight(dummyHeight)
				require.NoError(t, env.state.Commit())
			},
			AP3Time:     genesistest.DefaultValidatorStartTime,
			expectedErr: errFlowCheckFailed,
		},
		{
			description: "over delegation before AP3",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   newValidatorStartTime, // same start time as for primary network
			endTime:     newValidatorEndTime,   // same end time as for primary network
			nodeID:      newValidatorID,
			feeKeys:     []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:       addMaxStakeValidator,
			AP3Time:     genesistest.DefaultValidatorEndTime,
			expectedErr: nil,
		},
		{
			description: "over delegation after AP3",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   newValidatorStartTime, // same start time as for primary network
			endTime:     newValidatorEndTime,   // same end time as for primary network
			nodeID:      newValidatorID,
			feeKeys:     []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:       addMaxStakeValidator,
			AP3Time:     genesistest.DefaultValidatorStartTime,
			expectedErr: ErrOverDelegated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, upgradetest.ApricotPhase5)
			env.config.UpgradeConfig.ApricotPhase3Time = tt.AP3Time

			wallet := newWallet(t, env, walletConfig{
				keys: tt.feeKeys,
			})

			tx, err := wallet.IssueAddDelegatorTx(
				&platform.Validator{
					NodeID: tt.nodeID,
					Start:  tt.startTime,
					End:    tt.endTime,
					Wght:   tt.stakeAmount,
				},
				rewardsOwner,
			)
			require.NoError(err)

			if tt.setup != nil {
				tt.setup(env)
			}

			onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
			err = ProposalTx(
				&env.backend,
				feeCalculator,
				tx,
				onCommitState,
				onAbortState,
			)
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestProposalTxExecuteAddSubnetValidator(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	nodeID := genesistest.DefaultNodeIDs[0]
	subnetID := testSubnet1.ID()
	{
		// Case: Proposed validator currently validating primary network
		// but stops validating subnet after stops validating primary network
		// (note that keys[0] is a genesis validator)
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: nodeID,
					Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
					End:    genesistest.DefaultValidatorEndTimeUnix + 1,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, errPeriodMismatch)
	}

	{
		// Case: Proposed validator currently validating primary network
		// and proposed subnet validation period is subset of
		// primary network validation period
		// (note that keys[0] is a genesis validator)
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
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
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		require.NoError(ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		))
	}

	// Add a validator to pending validator set of primary network
	// Starts validating primary network 10 seconds after genesis
	pendingDSValidatorID := ids.GenerateTestNodeID()
	dsStartTime := genesistest.DefaultValidatorStartTime.Add(10 * time.Second)
	dsEndTime := dsStartTime.Add(5 * defaultMinStakingDuration)

	wallet := newWallet(t, env, walletConfig{
		keys: genesistest.DefaultFundedKeys[:1],
	})
	addDSTx, err := wallet.IssueAddValidatorTx(
		&platform.Validator{
			NodeID: pendingDSValidatorID,
			Start:  uint64(dsStartTime.Unix()),
			End:    uint64(dsEndTime.Unix()),
			Wght:   env.config.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)

	{
		// Case: Proposed validator isn't in pending or current validator sets
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: pendingDSValidatorID,
					Start:  uint64(dsStartTime.Unix()), // start validating subnet before primary network
					End:    uint64(dsEndTime.Unix()),
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, errNotValidator)
	}

	addValTx := addDSTx.Unsigned.(*platform.AddValidatorTx)
	staker, err := state.NewCurrentStaker(
		addDSTx.ID(),
		addValTx,
		addValTx.StartTime(),
		addValTx.EndTime(),
		addValTx.Weight(),
		0,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(staker))
	env.state.AddTx(addDSTx, status.Committed)
	dummyHeight := uint64(1)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Node with ID key.Address() now a pending validator for primary network

	{
		// Case: Proposed validator is pending validator of primary network
		// but starts validating subnet before primary network
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: pendingDSValidatorID,
					Start:  uint64(dsStartTime.Unix()) - 1, // start validating subnet before primary network
					End:    uint64(dsEndTime.Unix()),
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, errPeriodMismatch)
	}

	{
		// Case: Proposed validator is pending validator of primary network
		// but stops validating subnet after primary network
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: pendingDSValidatorID,
					Start:  uint64(dsStartTime.Unix()),
					End:    uint64(dsEndTime.Unix()) + 1, // stop validating subnet after stopping validating primary network
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, errPeriodMismatch)
	}

	{
		// Case: Proposed validator is pending validator of primary network and
		// period validating subnet is subset of time validating primary network
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: pendingDSValidatorID,
					Start:  uint64(dsStartTime.Unix()), // same start time as for primary network
					End:    uint64(dsEndTime.Unix()),   // same end time as for primary network
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		require.NoError(ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		))
	}

	// Case: Proposed validator start validating at/before current timestamp
	// First, advance the timestamp
	newTimestamp := genesistest.DefaultValidatorStartTime.Add(2 * time.Second)
	env.state.SetTimestamp(newTimestamp)

	{
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: nodeID,
					Start:  uint64(newTimestamp.Unix()),
					End:    uint64(newTimestamp.Add(defaultMinStakingDuration).Unix()),
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, ErrTimestampNotBeforeStartTime)
	}

	// reset the timestamp
	env.state.SetTimestamp(genesistest.DefaultValidatorStartTime)

	// Case: Proposed validator already validating the subnet
	// First, add validator as validator of subnet
	wallet = newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})
	subnetTx, err := wallet.IssueAddSubnetValidatorTx(
		&platform.SubnetValidator{
			Validator: platform.Validator{
				NodeID: nodeID,
				Start:  genesistest.DefaultValidatorStartTimeUnix,
				End:    genesistest.DefaultValidatorEndTimeUnix,
				Wght:   genesistest.DefaultValidatorWeight,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	addSubnetValTx := subnetTx.Unsigned.(*platform.AddSubnetValidatorTx)
	staker, err = state.NewCurrentStaker(
		subnetTx.ID(),
		addSubnetValTx,
		addSubnetValTx.StartTime(),
		addSubnetValTx.EndTime(),
		addSubnetValTx.Weight(),
		0,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(staker))
	env.state.AddTx(subnetTx, status.Committed)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	{
		// Node with ID nodeIDKey.Address() now validating subnet with ID testSubnet1.ID
		wallet = newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		duplicateSubnetTx, err := wallet.IssueAddSubnetValidatorTx(
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
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			duplicateSubnetTx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, ErrDuplicateValidator)
	}

	require.NoError(env.state.DeleteCurrentValidator(staker))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	{
		// Case: Too few signatures
		wallet = newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: nodeID,
					Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
					End:    uint64(genesistest.DefaultValidatorStartTime.Add(defaultMinStakingDuration).Unix()) + 1,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		// Remove a signature
		addSubnetValidatorTx := tx.Unsigned.(*platform.AddSubnetValidatorTx)
		input := addSubnetValidatorTx.SubnetAuth.(*secp256k1fx.Input)
		input.SigIndices = input.SigIndices[1:]
		// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
		addSubnetValidatorTx.SyntacticallyVerified = false

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, errUnauthorizedModification)
	}

	{
		// Case: Control Signature from invalid key (keys[3] is not a control key)
		wallet = newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: nodeID,
					Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
					End:    uint64(genesistest.DefaultValidatorStartTime.Add(defaultMinStakingDuration).Unix()) + 1,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		// Replace a valid signature with one from keys[3]
		sig, err := genesistest.DefaultFundedKeys[3].SignHash(hashing.ComputeHash256(tx.Unsigned.Bytes()))
		require.NoError(err)
		copy(tx.Creds[0].(*secp256k1fx.Credential).Sigs[0][:], sig)

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, errUnauthorizedModification)
	}

	{
		// Case: Proposed validator in pending validator set for subnet
		// First, add validator to pending validator set of subnet
		wallet = newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: nodeID,
					Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
					End:    uint64(genesistest.DefaultValidatorStartTime.Add(defaultMinStakingDuration).Unix()) + 1,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		addSubnetValTx := subnetTx.Unsigned.(*platform.AddSubnetValidatorTx)
		staker, err = state.NewCurrentStaker(
			subnetTx.ID(),
			addSubnetValTx,
			addSubnetValTx.StartTime(),
			addSubnetValTx.EndTime(),
			addSubnetValTx.Weight(),
			0,
		)
		require.NoError(err)

		require.NoError(env.state.PutCurrentValidator(staker))
		env.state.AddTx(tx, status.Committed)
		env.state.SetHeight(dummyHeight)
		require.NoError(env.state.Commit())

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, ErrDuplicateValidator)
	}
}

func TestProposalTxExecuteAddValidator(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	nodeID := ids.GenerateTestNodeID()
	chainTime := env.state.GetTimestamp()
	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	{
		// Case: Validator's start time too early
		wallet := newWallet(t, env, walletConfig{})
		tx, err := wallet.IssueAddValidatorTx(
			&platform.Validator{
				NodeID: nodeID,
				Start:  uint64(chainTime.Unix()),
				End:    genesistest.DefaultValidatorEndTimeUnix,
				Wght:   env.config.MinValidatorStake,
			},
			rewardsOwner,
			reward.PercentDenominator,
		)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, ErrTimestampNotBeforeStartTime)
	}

	{
		nodeID := genesistest.DefaultNodeIDs[0]

		// Case: Validator already validating primary network
		wallet := newWallet(t, env, walletConfig{})
		tx, err := wallet.IssueAddValidatorTx(
			&platform.Validator{
				NodeID: nodeID,
				Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
				End:    genesistest.DefaultValidatorEndTimeUnix,
				Wght:   env.config.MinValidatorStake,
			},
			rewardsOwner,
			reward.PercentDenominator,
		)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, errAlreadyValidator)
	}

	{
		// Case: Validator in pending validator set of primary network
		startTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
		wallet := newWallet(t, env, walletConfig{})
		tx, err := wallet.IssueAddValidatorTx(
			&platform.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(startTime.Add(defaultMinStakingDuration).Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			rewardsOwner,
			reward.PercentDenominator,
		)
		require.NoError(err)

		addValTx := tx.Unsigned.(*platform.AddValidatorTx)
		staker, err := state.NewCurrentStaker(
			tx.ID(),
			addValTx,
			addValTx.StartTime(),
			addValTx.EndTime(),
			addValTx.Weight(),
			0,
		)
		require.NoError(err)

		require.NoError(env.state.PutPendingValidator(staker))
		env.state.AddTx(tx, status.Committed)
		dummyHeight := uint64(1)
		env.state.SetHeight(dummyHeight)
		require.NoError(env.state.Commit())

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, errAlreadyValidator)
	}

	{
		// Case: Validator doesn't have enough tokens to cover stake amount
		wallet := newWallet(t, env, walletConfig{
			keys: genesistest.DefaultFundedKeys[:1],
		})
		tx, err := wallet.IssueAddValidatorTx(
			&platform.Validator{
				NodeID: ids.GenerateTestNodeID(),
				Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
				End:    genesistest.DefaultValidatorEndTimeUnix,
				Wght:   env.config.MinValidatorStake,
			},
			rewardsOwner,
			reward.PercentDenominator,
		)
		require.NoError(err)

		// Remove all UTXOs owned by preFundedKeys[0]
		utxoIDs, err := env.state.UTXOIDs(genesistest.DefaultFundedKeys[0].Address().Bytes(), ids.Empty, math.MaxInt32)
		require.NoError(err)

		for _, utxoID := range utxoIDs {
			env.state.DeleteUTXO(utxoID)
		}

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		err = ProposalTx(
			&env.backend,
			feeCalculator,
			tx,
			onCommitState,
			onAbortState,
		)
		require.ErrorIs(err, errFlowCheckFailed)
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

	onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

	onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

		onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

				require.NoError(env.state.PutPendingValidator(staker))
				env.state.AddTx(tx, status.Committed)
			}
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			for _, newTime := range test.advanceTimeTo {
				env.clk.Set(newTime)
				tx, err := newAdvanceTimeTx(t, newTime)
				require.NoError(err)

				onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(err)

				onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

	require.NoError(env.state.PutCurrentValidator(staker))
	env.state.AddTx(tx, status.Committed)
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
				End:    uint64(subnetVdr1EndTime.Add(time.Second).Add(defaultMinStakingDuration).Unix()),
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

	require.NoError(env.state.PutPendingValidator(staker))
	env.state.AddTx(tx, status.Committed)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// The above validator is now in the pending staker set

	// Advance time to the first staker's end time.
	env.clk.Set(subnetVdr1EndTime)
	tx, err = newAdvanceTimeTx(t, subnetVdr1EndTime)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

			require.NoError(env.state.PutPendingValidator(staker))
			env.state.AddTx(tx, status.Committed)
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			// Advance time to the staker's start time.
			env.clk.Set(subnetVdr1StartTime)
			tx, err = newAdvanceTimeTx(t, subnetVdr1StartTime)
			require.NoError(err)

			onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

	onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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
		&platform.Validator{
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
		addDelegatorTx.Unsigned.(*platform.AddDelegatorTx),
	)
	require.NoError(err)

	env.state.PutPendingDelegator(staker)
	env.state.AddTx(addDelegatorTx, status.Committed)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Advance Time
	tx, err = newAdvanceTimeTx(t, pendingDelegatorStartTime)
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

	onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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
		&platform.Validator{
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
		addDelegatorTx.Unsigned.(*platform.AddDelegatorTx),
	)
	require.NoError(err)

	env.state.PutPendingDelegator(staker)
	env.state.AddTx(addDelegatorTx, status.Committed)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Advance Time
	tx, err = newAdvanceTimeTx(t, pendingDelegatorStartTime)
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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

	onCommitState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
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
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)

	staker, err := state.NewPendingStaker(
		addPendingValidatorTx.ID(),
		addPendingValidatorTx.Unsigned.(*platform.AddValidatorTx),
	)
	require.NoError(err)

	require.NoError(env.state.PutPendingValidator(staker))
	env.state.AddTx(addPendingValidatorTx, status.Committed)
	dummyHeight := uint64(1)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())
	return addPendingValidatorTx
}
