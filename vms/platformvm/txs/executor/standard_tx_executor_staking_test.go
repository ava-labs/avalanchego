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
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestStandardTxExecutorAddValidatorTxEmptyID(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.ApricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	chainTime := env.state.GetTimestamp()
	startTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)

	tests := []struct {
		banffTime time.Time
	}{
		{ // Case: Before banff
			banffTime: chainTime.Add(1),
		},
		{ // Case: At banff
			banffTime: chainTime,
		},
		{ // Case: After banff
			banffTime: chainTime.Add(-1),
		},
	}
	for _, test := range tests {
		// Case: Empty validator node ID after banff
		env.config.UpgradeConfig.BanffTime = test.banffTime

		wallet := newWallet(t, env, walletConfig{})

		tx, err := wallet.IssueAddValidatorTx(
			&platform.Validator{
				NodeID: ids.EmptyNodeID,
				Start:  uint64(startTime.Unix()),
				End:    genesistest.DefaultValidatorEndTimeUnix,
				Wght:   env.config.MinValidatorStake,
			},
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
			reward.PercentDenominator,
		)
		require.NoError(err)

		stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			stateDiff,
		)
		require.ErrorIs(err, errEmptyNodeID)
	}
}

func TestStandardTxExecutorAddDelegator(t *testing.T) {
	dummyHeight := uint64(1)
	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
	nodeID := genesistest.DefaultNodeIDs[0]

	newValidatorID := ids.GenerateTestNodeID()
	newValidatorStartTime := genesistest.DefaultValidatorStartTime.Add(5 * time.Second)
	newValidatorEndTime := genesistest.DefaultValidatorEndTime.Add(-5 * time.Second)

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
				Start:  uint64(newValidatorStartTime.Unix()),
				End:    uint64(newValidatorEndTime.Unix()),
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
			newValidatorStartTime,
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
				Start:  uint64(newValidatorStartTime.Unix()),
				End:    uint64(newValidatorEndTime.Unix()),
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
			newValidatorStartTime,
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
		description          string
		stakeAmount          uint64
		startTime            time.Time
		endTime              time.Time
		nodeID               ids.NodeID
		feeKeys              []*secp256k1.PrivateKey
		setup                func(*environment)
		AP3Time              time.Time
		expectedExecutionErr error
	}

	tests := []test{
		{
			description:          "validator stops validating earlier than delegator",
			stakeAmount:          env.config.MinDelegatorStake,
			startTime:            genesistest.DefaultValidatorStartTime.Add(time.Second),
			endTime:              genesistest.DefaultValidatorEndTime.Add(time.Second),
			nodeID:               nodeID,
			feeKeys:              []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:                nil,
			AP3Time:              genesistest.DefaultValidatorStartTime,
			expectedExecutionErr: errPeriodMismatch,
		},
		{
			description:          "validator not in the current or pending validator sets",
			stakeAmount:          env.config.MinDelegatorStake,
			startTime:            genesistest.DefaultValidatorStartTime.Add(5 * time.Second),
			endTime:              genesistest.DefaultValidatorEndTime.Add(-5 * time.Second),
			nodeID:               newValidatorID,
			feeKeys:              []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:                nil,
			AP3Time:              genesistest.DefaultValidatorStartTime,
			expectedExecutionErr: database.ErrNotFound,
		},
		{
			description:          "delegator starts before validator",
			stakeAmount:          env.config.MinDelegatorStake,
			startTime:            newValidatorStartTime.Add(-1 * time.Second), // start validating subnet before primary network
			endTime:              newValidatorEndTime,
			nodeID:               newValidatorID,
			feeKeys:              []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:                addMinStakeValidator,
			AP3Time:              genesistest.DefaultValidatorStartTime,
			expectedExecutionErr: errPeriodMismatch,
		},
		{
			description:          "delegator stops before validator",
			stakeAmount:          env.config.MinDelegatorStake,
			startTime:            newValidatorStartTime,
			endTime:              newValidatorEndTime.Add(time.Second), // stop validating subnet after stopping validating primary network
			nodeID:               newValidatorID,
			feeKeys:              []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:                addMinStakeValidator,
			AP3Time:              genesistest.DefaultValidatorStartTime,
			expectedExecutionErr: errPeriodMismatch,
		},
		{
			description:          "valid",
			stakeAmount:          env.config.MinDelegatorStake,
			startTime:            newValidatorStartTime, // same start time as for primary network
			endTime:              newValidatorEndTime,   // same end time as for primary network
			nodeID:               newValidatorID,
			feeKeys:              []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:                addMinStakeValidator,
			AP3Time:              genesistest.DefaultValidatorStartTime,
			expectedExecutionErr: nil,
		},
		{
			description:          "starts delegating at current timestamp",
			stakeAmount:          env.config.MinDelegatorStake,                              // weight
			startTime:            currentTimestamp,                                          // start time
			endTime:              genesistest.DefaultValidatorEndTime,                       // end time
			nodeID:               nodeID,                                                    // node ID
			feeKeys:              []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]}, // tx fee payer
			setup:                nil,
			AP3Time:              genesistest.DefaultValidatorStartTime,
			expectedExecutionErr: ErrTimestampNotBeforeStartTime,
		},
		{
			description: "tx fee paying key has no funds",
			stakeAmount: env.config.MinDelegatorStake,                              // weight
			startTime:   genesistest.DefaultValidatorStartTime.Add(time.Second),    // start time
			endTime:     genesistest.DefaultValidatorEndTime,                       // end time
			nodeID:      nodeID,                                                    // node ID
			feeKeys:     []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[1]}, // tx fee payer
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
			AP3Time:              genesistest.DefaultValidatorStartTime,
			expectedExecutionErr: errFlowCheckFailed,
		},
		{
			description:          "over delegation before AP3",
			stakeAmount:          env.config.MinDelegatorStake,
			startTime:            newValidatorStartTime, // same start time as for primary network
			endTime:              newValidatorEndTime,   // same end time as for primary network
			nodeID:               newValidatorID,
			feeKeys:              []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:                addMaxStakeValidator,
			AP3Time:              genesistest.DefaultValidatorEndTime,
			expectedExecutionErr: nil,
		},
		{
			description:          "over delegation after AP3",
			stakeAmount:          env.config.MinDelegatorStake,
			startTime:            newValidatorStartTime, // same start time as for primary network
			endTime:              newValidatorEndTime,   // same end time as for primary network
			nodeID:               newValidatorID,
			feeKeys:              []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
			setup:                addMaxStakeValidator,
			AP3Time:              genesistest.DefaultValidatorStartTime,
			expectedExecutionErr: ErrOverDelegated,
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
					Start:  uint64(tt.startTime.Unix()),
					End:    uint64(tt.endTime.Unix()),
					Wght:   tt.stakeAmount,
				},
				rewardsOwner,
			)
			require.NoError(err)

			if tt.setup != nil {
				tt.setup(env)
			}

			onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			env.config.UpgradeConfig.BanffTime = onAcceptState.GetTimestamp()

			feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				onAcceptState,
			)
			require.ErrorIs(err, tt.expectedExecutionErr)
		})
	}
}

func TestApricotStandardTxExecutorAddSubnetValidator(t *testing.T) {
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
		startTime := genesistest.DefaultValidatorStartTime.Add(time.Second)

		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: nodeID,
					Start:  uint64(startTime.Unix()),
					End:    genesistest.DefaultValidatorEndTimeUnix + 1,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
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

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		require.NoError(err)
	}

	// Add a validator to pending validator set of primary network
	// Starts validating primary network 10 seconds after genesis
	pendingDSValidatorID := ids.GenerateTestNodeID()
	dsStartTime := genesistest.DefaultValidatorStartTime.Add(10 * time.Second)
	dsEndTime := dsStartTime.Add(5 * defaultMinStakingDuration)

	wallet := newWallet(t, env, walletConfig{})
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

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		require.ErrorIs(err, errNotValidator)
	}

	addValTx := addDSTx.Unsigned.(*platform.AddValidatorTx)
	staker, err := state.NewCurrentStaker(
		addDSTx.ID(),
		addValTx,
		dsStartTime,
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

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
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

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
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

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		require.NoError(err)
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

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
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
		genesistest.DefaultValidatorStartTime,
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
		startTime := genesistest.DefaultValidatorStartTime.Add(time.Second)
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: nodeID,
					Start:  uint64(startTime.Unix()),
					End:    genesistest.DefaultValidatorEndTimeUnix,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		require.ErrorIs(err, ErrDuplicateValidator)
	}

	require.NoError(env.state.DeleteCurrentValidator(staker))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	{
		// Case: Duplicate signatures
		startTime := genesistest.DefaultValidatorStartTime.Add(time.Second)
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: nodeID,
					Start:  uint64(startTime.Unix()),
					End:    uint64(startTime.Add(defaultMinStakingDuration).Unix()) + 1,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		// Duplicate a signature
		addSubnetValidatorTx := tx.Unsigned.(*platform.AddSubnetValidatorTx)
		input := addSubnetValidatorTx.SubnetAuth.(*secp256k1fx.Input)
		input.SigIndices = append(input.SigIndices, input.SigIndices[0])
		// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
		addSubnetValidatorTx.SyntacticallyVerified = false

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		require.ErrorIs(err, secp256k1fx.ErrInputIndicesNotSortedUnique)
	}

	{
		// Case: Too few signatures
		startTime := genesistest.DefaultValidatorStartTime.Add(time.Second)
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: nodeID,
					Start:  uint64(startTime.Unix()),
					End:    uint64(startTime.Add(defaultMinStakingDuration).Unix()),
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

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		require.ErrorIs(err, errUnauthorizedModification)
	}

	{
		// Case: Control Signature from invalid key (keys[3] is not a control key)
		startTime := genesistest.DefaultValidatorStartTime.Add(time.Second)
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: nodeID,
					Start:  uint64(startTime.Unix()),
					End:    uint64(startTime.Add(defaultMinStakingDuration).Unix()),
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

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		require.ErrorIs(err, errUnauthorizedModification)
	}

	{
		// Case: Proposed validator in pending validator set for subnet
		// First, add validator to pending validator set of subnet
		startTime := genesistest.DefaultValidatorStartTime.Add(time.Second)
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&platform.SubnetValidator{
				Validator: platform.Validator{
					NodeID: nodeID,
					Start:  uint64(startTime.Unix()) + 1,
					End:    uint64(startTime.Add(defaultMinStakingDuration).Unix()) + 1,
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
			genesistest.DefaultValidatorStartTime,
			addSubnetValTx.EndTime(),
			addSubnetValTx.Weight(),
			0,
		)
		require.NoError(err)

		require.NoError(env.state.PutCurrentValidator(staker))
		env.state.AddTx(tx, status.Committed)
		env.state.SetHeight(dummyHeight)
		require.NoError(env.state.Commit())

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		require.ErrorIs(err, ErrDuplicateValidator)
	}
}

func TestEtnaStandardTxExecutorAddSubnetValidator(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Etna)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	nodeID := genesistest.DefaultNodeIDs[0]
	subnetID := testSubnet1.ID()

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

	onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	onAcceptState.SetSubnetToL1Conversion(
		subnetID,
		state.SubnetToL1Conversion{
			ConversionID: ids.GenerateTestID(),
			ChainID:      ids.GenerateTestID(),
			Addr:         []byte("address"),
		},
	)

	feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		onAcceptState,
	)
	require.ErrorIs(err, errIsImmutable)
}

func TestBanffStandardTxExecutorAddValidator(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Banff)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	nodeID := ids.GenerateTestNodeID()
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
				Start:  genesistest.DefaultValidatorStartTimeUnix - 1,
				End:    genesistest.DefaultValidatorEndTimeUnix,
				Wght:   env.config.MinValidatorStake,
			},
			rewardsOwner,
			reward.PercentDenominator,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		require.ErrorIs(err, ErrTimestampNotBeforeStartTime)
	}

	{
		// Case: Validator in current validator set of primary network
		wallet := newWallet(t, env, walletConfig{})

		startTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
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
			startTime,
			addValTx.EndTime(),
			addValTx.Weight(),
			0,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		require.NoError(onAcceptState.PutCurrentValidator(staker))
		onAcceptState.AddTx(tx, status.Committed)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		require.ErrorIs(err, errAlreadyValidator)
	}

	{
		// Case: Validator in pending validator set of primary network
		wallet := newWallet(t, env, walletConfig{})

		startTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
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

		staker, err := state.NewPendingStaker(
			tx.ID(),
			tx.Unsigned.(*platform.AddValidatorTx),
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		require.NoError(onAcceptState.PutPendingValidator(staker))
		onAcceptState.AddTx(tx, status.Committed)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		require.ErrorIs(err, errAlreadyValidator)
	}

	{
		// Case: Validator doesn't have enough tokens to cover stake amount
		wallet := newWallet(t, env, walletConfig{
			keys: genesistest.DefaultFundedKeys[:1],
		})

		startTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
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

		// Remove all UTXOs owned by preFundedKeys[0]
		utxoIDs, err := env.state.UTXOIDs(genesistest.DefaultFundedKeys[0].Address().Bytes(), ids.Empty, math.MaxInt32)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(err)

		for _, utxoID := range utxoIDs {
			onAcceptState.DeleteUTXO(utxoID)
		}

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		_, _, _, err = StandardTx(
			&env.backend,
			feeCalculator,
			tx,
			onAcceptState,
		)
		require.ErrorIs(err, errFlowCheckFailed)
	}
}

func TestStandardExecutorRemoveSubnetValidatorTx(t *testing.T) {
	var (
		env    = newEnvironment(t, upgradetest.Etna)
		wallet = newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{testSubnet1.ID()},
		})
		nodeID = genesistest.DefaultNodeIDs[0]
	)

	testSubnetID := ids.GenerateTestID()

	type test struct {
		name            string
		updateTx        func(*platform.Tx)
		updateStaker    func(*state.Staker)
		updateState     func(*state.Diff)
		stateAssertions func(*testing.T, *state.Diff, *platform.Tx)
		expectedErr     error
	}

	tests := []test{
		{
			name: "valid tx",
			updateState: func(diff *state.Diff) {
				// This isn't actually called, but is added here as a regression
				// test to ensure that converted subnets can still remove
				// permissioned validators.
				diff.SetSubnetToL1Conversion(testSubnet1.ID(), state.SubnetToL1Conversion{
					ConversionID: ids.GenerateTestID(),
					ChainID:      ids.GenerateTestID(),
					Addr:         []byte("address"),
				})
			},
			stateAssertions: func(t *testing.T, diff *state.Diff, sTx *platform.Tx) {
				tx := sTx.Unsigned.(*platform.RemoveSubnetValidatorTx)
				// assert that the validator was removed from the current validator set
				_, err := diff.GetCurrentValidator(tx.Subnet, tx.NodeID)
				require.ErrorIs(t, err, database.ErrNotFound)

				// assert the utxos inputs were deleted
				for _, utxo := range tx.Ins {
					_, err := diff.GetUTXO(utxo.InputID())
					require.ErrorIs(t, err, database.ErrNotFound)
				}

				for _, expectedOut := range sTx.UTXOs() {
					utxo, err := diff.GetUTXO(expectedOut.InputID())
					require.NoError(t, err)

					require.Equal(t, expectedOut.Out, utxo.Out)
				}
			},
			expectedErr: nil,
		},
		{
			name: "tx fails syntactic verification",
			updateTx: func(tx *platform.Tx) {
				// Setting the subnet to the primary network ID makes the tx
				// syntactically invalid
				tx.Unsigned.(*platform.RemoveSubnetValidatorTx).Subnet = constants.PrimaryNetworkID
			},
			expectedErr: platform.ErrRemovePrimaryNetworkValidator,
		},
		{
			name: "node isn't a validator of the subnet",
			updateStaker: func(staker *state.Staker) {
				staker.NodeID = ids.GenerateTestNodeID()
			},
			expectedErr: errNotValidator,
		},
		{
			name: "validator is permissionless",
			updateStaker: func(staker *state.Staker) {
				staker.Priority = platform.SubnetPermissionlessValidatorCurrentPriority
			},
			expectedErr: errRemovePermissionlessValidator,
		},
		{
			name: "can't find subnet",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.RemoveSubnetValidatorTx).Subnet = testSubnetID
			},
			updateStaker: func(staker *state.Staker) {
				staker.SubnetID = testSubnetID
			},
			expectedErr: database.ErrNotFound,
		},
		{
			name: "tx has no credentials",
			updateTx: func(tx *platform.Tx) {
				tx.Creds = nil
			},
			expectedErr: errWrongNumberOfCredentials,
		},
		{
			name: "no permission to remove validator",
			updateTx: func(tx *platform.Tx) {
				// Remove a signature from the subnet auth credential
				cred := tx.Creds[len(tx.Creds)-1].(*secp256k1fx.Credential)
				cred.Sigs = cred.Sigs[1:]
			},
			expectedErr: errUnauthorizedModification,
		},
		{
			name: "flow checker failed",
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
			expectedErr: errFlowCheckFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			tx, err := wallet.IssueRemoveSubnetValidatorTx(
				nodeID,
				testSubnet1.ID(),
			)
			require.NoError(err)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(err)

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
			require.NoError(diff.PutCurrentValidator(staker))

			if tt.updateState != nil {
				tt.updateState(diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, env.state)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)
			require.ErrorIs(err, tt.expectedErr)

			if tt.stateAssertions != nil {
				tt.stateAssertions(t, diff, tx)
			}
		})
	}
}

// TestHeliconMinStakeDurationValidator verifies that the Helicon upgrade lowers
// the primary network validator minimum staking duration.
func TestHeliconMinStakeDurationValidator(t *testing.T) {
	type test struct {
		name        string
		fork        upgradetest.Fork
		expectedErr error
	}
	tests := []test{
		{
			name:        "pre-Helicon 12h stake rejected",
			fork:        upgradetest.Granite,
			expectedErr: errStakeTooShort,
		},
		{
			name:        "post-Helicon 12h stake accepted",
			fork:        upgradetest.Helicon,
			expectedErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, tt.fork)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			chainTime := env.state.GetTimestamp()
			nodeID := ids.GenerateTestNodeID()
			sk, err := localsigner.New()
			require.NoError(err)
			pop, err := signer.NewProofOfPossession(sk)
			require.NoError(err)

			rewardsOwner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			}
			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueAddPermissionlessValidatorTx(
				&platform.SubnetValidator{
					Validator: platform.Validator{
						NodeID: nodeID,
						End:    uint64(chainTime.Add(defaultHeliconMinStakingDuration).Unix()),
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

			onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
			_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}
