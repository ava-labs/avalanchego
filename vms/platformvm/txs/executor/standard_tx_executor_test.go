// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// This tests that the math performed during TransformSubnetTx execution can
// never overflow
const _ time.Duration = math.MaxUint32 * time.Second

var errTest = errors.New("non-nil error")

func TestStandardTxExecutorAddValidatorTxEmptyID(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, false /*=postBanff*/, false /*=postCortina*/, false /*=postDurango*/)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	chainTime := env.state.GetTimestamp()
	startTime := defaultValidateStartTime.Add(1 * time.Second)

	tests := []struct {
		banffTime     time.Time
		expectedError error
	}{
		{ // Case: Before banff
			banffTime:     chainTime.Add(1),
			expectedError: errEmptyNodeID,
		},
		{ // Case: At banff
			banffTime:     chainTime,
			expectedError: errEmptyNodeID,
		},
		{ // Case: After banff
			banffTime:     chainTime.Add(-1),
			expectedError: errEmptyNodeID,
		},
	}
	for _, test := range tests {
		// Case: Empty validator node ID after banff
		env.config.BanffTime = test.banffTime

		tx, err := env.txBuilder.NewAddValidatorTx( // create the tx
			env.config.MinValidatorStake,
			uint64(startTime.Unix()),
			uint64(defaultValidateEndTime.Unix()),
			ids.EmptyNodeID,
			ids.GenerateTestShortID(),
			reward.PercentDenominator,
			[]*secp256k1.PrivateKey{preFundedKeys[0]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		stateDiff, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   stateDiff,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, test.expectedError)
	}
}

func TestStandardTxExecutorAddDelegator(t *testing.T) {
	dummyHeight := uint64(1)
	rewardAddress := preFundedKeys[0].PublicKey().Address()
	nodeID := genesisNodeIDs[0]

	newValidatorID := ids.GenerateTestNodeID()
	newValidatorStartTime := defaultValidateStartTime.Add(5 * time.Second)
	newValidatorEndTime := defaultValidateEndTime.Add(-5 * time.Second)

	// [addMinStakeValidator] adds a new validator to the primary network's
	// pending validator set with the minimum staking amount
	addMinStakeValidator := func(target *environment) {
		tx, err := target.txBuilder.NewAddValidatorTx(
			target.config.MinValidatorStake,      // stake amount
			uint64(newValidatorStartTime.Unix()), // start time
			uint64(newValidatorEndTime.Unix()),   // end time
			newValidatorID,                       // node ID
			rewardAddress,                        // Reward Address
			reward.PercentDenominator,            // Shares
			[]*secp256k1.PrivateKey{preFundedKeys[0]},
			ids.ShortEmpty,
			nil,
		)
		require.NoError(t, err)

		addValTx := tx.Unsigned.(*txs.AddValidatorTx)
		staker, err := state.NewCurrentStaker(
			tx.ID(),
			addValTx,
			newValidatorStartTime,
			0,
		)
		require.NoError(t, err)

		target.state.PutCurrentValidator(staker)
		target.state.AddTx(tx, status.Committed)
		target.state.SetHeight(dummyHeight)
		require.NoError(t, target.state.Commit())
	}

	// [addMaxStakeValidator] adds a new validator to the primary network's
	// pending validator set with the maximum staking amount
	addMaxStakeValidator := func(target *environment) {
		tx, err := target.txBuilder.NewAddValidatorTx(
			target.config.MaxValidatorStake,      // stake amount
			uint64(newValidatorStartTime.Unix()), // start time
			uint64(newValidatorEndTime.Unix()),   // end time
			newValidatorID,                       // node ID
			rewardAddress,                        // Reward Address
			reward.PercentDenominator,            // Shared
			[]*secp256k1.PrivateKey{preFundedKeys[0]},
			ids.ShortEmpty,
			nil,
		)
		require.NoError(t, err)

		addValTx := tx.Unsigned.(*txs.AddValidatorTx)
		staker, err := state.NewCurrentStaker(
			tx.ID(),
			addValTx,
			newValidatorStartTime,
			0,
		)
		require.NoError(t, err)

		target.state.PutCurrentValidator(staker)
		target.state.AddTx(tx, status.Committed)
		target.state.SetHeight(dummyHeight)
		require.NoError(t, target.state.Commit())
	}

	dummyH := newEnvironment(t, false /*=postBanff*/, false /*=postCortina*/, false /*=postDurango*/)
	currentTimestamp := dummyH.state.GetTimestamp()

	type test struct {
		description          string
		stakeAmount          uint64
		startTime            time.Time
		endTime              time.Time
		nodeID               ids.NodeID
		rewardAddress        ids.ShortID
		feeKeys              []*secp256k1.PrivateKey
		setup                func(*environment)
		AP3Time              time.Time
		expectedExecutionErr error
	}

	tests := []test{
		{
			description:          "validator stops validating earlier than delegator",
			stakeAmount:          dummyH.config.MinDelegatorStake,
			startTime:            defaultValidateStartTime.Add(time.Second),
			endTime:              defaultValidateEndTime.Add(time.Second),
			nodeID:               nodeID,
			rewardAddress:        rewardAddress,
			feeKeys:              []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:                nil,
			AP3Time:              defaultGenesisTime,
			expectedExecutionErr: ErrPeriodMismatch,
		},
		{
			description:          fmt.Sprintf("delegator should not be added more than (%s) in the future", MaxFutureStartTime),
			stakeAmount:          dummyH.config.MinDelegatorStake,
			startTime:            currentTimestamp.Add(MaxFutureStartTime + time.Second),
			endTime:              currentTimestamp.Add(MaxFutureStartTime + defaultMinStakingDuration + time.Second),
			nodeID:               nodeID,
			rewardAddress:        rewardAddress,
			feeKeys:              []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:                nil,
			AP3Time:              defaultGenesisTime,
			expectedExecutionErr: ErrFutureStakeTime,
		},
		{
			description:          "validator not in the current or pending validator sets",
			stakeAmount:          dummyH.config.MinDelegatorStake,
			startTime:            defaultValidateStartTime.Add(5 * time.Second),
			endTime:              defaultValidateEndTime.Add(-5 * time.Second),
			nodeID:               newValidatorID,
			rewardAddress:        rewardAddress,
			feeKeys:              []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:                nil,
			AP3Time:              defaultGenesisTime,
			expectedExecutionErr: database.ErrNotFound,
		},
		{
			description:          "delegator starts before validator",
			stakeAmount:          dummyH.config.MinDelegatorStake,
			startTime:            newValidatorStartTime.Add(-1 * time.Second), // start validating subnet before primary network
			endTime:              newValidatorEndTime,
			nodeID:               newValidatorID,
			rewardAddress:        rewardAddress,
			feeKeys:              []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:                addMinStakeValidator,
			AP3Time:              defaultGenesisTime,
			expectedExecutionErr: ErrPeriodMismatch,
		},
		{
			description:          "delegator stops before validator",
			stakeAmount:          dummyH.config.MinDelegatorStake,
			startTime:            newValidatorStartTime,
			endTime:              newValidatorEndTime.Add(time.Second), // stop validating subnet after stopping validating primary network
			nodeID:               newValidatorID,
			rewardAddress:        rewardAddress,
			feeKeys:              []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:                addMinStakeValidator,
			AP3Time:              defaultGenesisTime,
			expectedExecutionErr: ErrPeriodMismatch,
		},
		{
			description:          "valid",
			stakeAmount:          dummyH.config.MinDelegatorStake,
			startTime:            newValidatorStartTime, // same start time as for primary network
			endTime:              newValidatorEndTime,   // same end time as for primary network
			nodeID:               newValidatorID,
			rewardAddress:        rewardAddress,
			feeKeys:              []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:                addMinStakeValidator,
			AP3Time:              defaultGenesisTime,
			expectedExecutionErr: nil,
		},
		{
			description:          "starts delegating at current timestamp",
			stakeAmount:          dummyH.config.MinDelegatorStake,           // weight
			startTime:            currentTimestamp,                          // start time
			endTime:              defaultValidateEndTime,                    // end time
			nodeID:               nodeID,                                    // node ID
			rewardAddress:        rewardAddress,                             // Reward Address
			feeKeys:              []*secp256k1.PrivateKey{preFundedKeys[0]}, // tx fee payer
			setup:                nil,
			AP3Time:              defaultGenesisTime,
			expectedExecutionErr: ErrTimestampNotBeforeStartTime,
		},
		{
			description:   "tx fee paying key has no funds",
			stakeAmount:   dummyH.config.MinDelegatorStake,           // weight
			startTime:     defaultValidateStartTime.Add(time.Second), // start time
			endTime:       defaultValidateEndTime,                    // end time
			nodeID:        nodeID,                                    // node ID
			rewardAddress: rewardAddress,                             // Reward Address
			feeKeys:       []*secp256k1.PrivateKey{preFundedKeys[1]}, // tx fee payer
			setup: func(target *environment) { // Remove all UTXOs owned by keys[1]
				utxoIDs, err := target.state.UTXOIDs(
					preFundedKeys[1].PublicKey().Address().Bytes(),
					ids.Empty,
					math.MaxInt32)
				require.NoError(t, err)

				for _, utxoID := range utxoIDs {
					target.state.DeleteUTXO(utxoID)
				}
				target.state.SetHeight(dummyHeight)
				require.NoError(t, target.state.Commit())
			},
			AP3Time:              defaultGenesisTime,
			expectedExecutionErr: ErrFlowCheckFailed,
		},
		{
			description:          "over delegation before AP3",
			stakeAmount:          dummyH.config.MinDelegatorStake,
			startTime:            newValidatorStartTime, // same start time as for primary network
			endTime:              newValidatorEndTime,   // same end time as for primary network
			nodeID:               newValidatorID,
			rewardAddress:        rewardAddress,
			feeKeys:              []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:                addMaxStakeValidator,
			AP3Time:              defaultValidateEndTime,
			expectedExecutionErr: nil,
		},
		{
			description:          "over delegation after AP3",
			stakeAmount:          dummyH.config.MinDelegatorStake,
			startTime:            newValidatorStartTime, // same start time as for primary network
			endTime:              newValidatorEndTime,   // same end time as for primary network
			nodeID:               newValidatorID,
			rewardAddress:        rewardAddress,
			feeKeys:              []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:                addMaxStakeValidator,
			AP3Time:              defaultGenesisTime,
			expectedExecutionErr: ErrOverDelegated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			require := require.New(t)
			freshTH := newEnvironment(t, false /*=postBanff*/, false /*=postCortina*/, false /*=postDurango*/)
			freshTH.config.ApricotPhase3Time = tt.AP3Time

			tx, err := freshTH.txBuilder.NewAddDelegatorTx(
				tt.stakeAmount,
				uint64(tt.startTime.Unix()),
				uint64(tt.endTime.Unix()),
				tt.nodeID,
				tt.rewardAddress,
				tt.feeKeys,
				ids.ShortEmpty,
				nil,
			)
			require.NoError(err)

			if tt.setup != nil {
				tt.setup(freshTH)
			}

			onAcceptState, err := state.NewDiff(lastAcceptedID, freshTH)
			require.NoError(err)

			freshTH.config.BanffTime = onAcceptState.GetTimestamp()

			executor := StandardTxExecutor{
				Backend: &freshTH.backend,
				State:   onAcceptState,
				Tx:      tx,
			}
			err = tx.Unsigned.Visit(&executor)
			require.ErrorIs(err, tt.expectedExecutionErr)
		})
	}
}

func TestApricotStandardTxExecutorAddSubnetValidator(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, false /*=postBanff*/, false /*=postCortina*/, false /*=postDurango*/)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	nodeID := genesisNodeIDs[0]

	{
		// Case: Proposed validator currently validating primary network
		// but stops validating subnet after stops validating primary network
		// (note that keys[0] is a genesis validator)
		startTime := defaultValidateStartTime.Add(time.Second)
		tx, err := env.txBuilder.NewAddSubnetValidatorTx(
			defaultWeight,
			uint64(startTime.Unix()),
			uint64(defaultValidateEndTime.Unix())+1,
			nodeID,
			testSubnet1.ID(),
			[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrPeriodMismatch)
	}

	{
		// Case: Proposed validator currently validating primary network
		// and proposed subnet validation period is subset of
		// primary network validation period
		// (note that keys[0] is a genesis validator)
		tx, err := env.txBuilder.NewAddSubnetValidatorTx(
			defaultWeight,
			uint64(defaultValidateStartTime.Unix()+1),
			uint64(defaultValidateEndTime.Unix()),
			nodeID,
			testSubnet1.ID(),
			[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		require.NoError(tx.Unsigned.Visit(&executor))
	}

	// Add a validator to pending validator set of primary network
	// Starts validating primary network 10 seconds after genesis
	pendingDSValidatorID := ids.GenerateTestNodeID()
	dsStartTime := defaultGenesisTime.Add(10 * time.Second)
	dsEndTime := dsStartTime.Add(5 * defaultMinStakingDuration)

	addDSTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake, // stake amount
		uint64(dsStartTime.Unix()),   // start time
		uint64(dsEndTime.Unix()),     // end time
		pendingDSValidatorID,         // node ID
		ids.GenerateTestShortID(),    // reward address
		reward.PercentDenominator,    // shares
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		ids.ShortEmpty,
		nil,
	)
	require.NoError(err)

	{
		// Case: Proposed validator isn't in pending or current validator sets
		tx, err := env.txBuilder.NewAddSubnetValidatorTx(
			defaultWeight,
			uint64(dsStartTime.Unix()), // start validating subnet before primary network
			uint64(dsEndTime.Unix()),
			pendingDSValidatorID,
			testSubnet1.ID(),
			[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrNotValidator)
	}

	addValTx := addDSTx.Unsigned.(*txs.AddValidatorTx)
	staker, err := state.NewCurrentStaker(
		addDSTx.ID(),
		addValTx,
		dsStartTime,
		0,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(staker)
	env.state.AddTx(addDSTx, status.Committed)
	dummyHeight := uint64(1)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Node with ID key.PublicKey().Address() now a pending validator for primary network

	{
		// Case: Proposed validator is pending validator of primary network
		// but starts validating subnet before primary network
		tx, err := env.txBuilder.NewAddSubnetValidatorTx(
			defaultWeight,
			uint64(dsStartTime.Unix())-1, // start validating subnet before primary network
			uint64(dsEndTime.Unix()),
			pendingDSValidatorID,
			testSubnet1.ID(),
			[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrPeriodMismatch)
	}

	{
		// Case: Proposed validator is pending validator of primary network
		// but stops validating subnet after primary network
		tx, err := env.txBuilder.NewAddSubnetValidatorTx(
			defaultWeight,
			uint64(dsStartTime.Unix()),
			uint64(dsEndTime.Unix())+1, // stop validating subnet after stopping validating primary network
			pendingDSValidatorID,
			testSubnet1.ID(),
			[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrPeriodMismatch)
	}

	{
		// Case: Proposed validator is pending validator of primary network and
		// period validating subnet is subset of time validating primary network
		tx, err := env.txBuilder.NewAddSubnetValidatorTx(
			defaultWeight,
			uint64(dsStartTime.Unix()), // same start time as for primary network
			uint64(dsEndTime.Unix()),   // same end time as for primary network
			pendingDSValidatorID,
			testSubnet1.ID(),
			[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)
		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		require.NoError(tx.Unsigned.Visit(&executor))
	}

	// Case: Proposed validator start validating at/before current timestamp
	// First, advance the timestamp
	newTimestamp := defaultGenesisTime.Add(2 * time.Second)
	env.state.SetTimestamp(newTimestamp)

	{
		tx, err := env.txBuilder.NewAddSubnetValidatorTx(
			defaultWeight,               // weight
			uint64(newTimestamp.Unix()), // start time
			uint64(newTimestamp.Add(defaultMinStakingDuration).Unix()), // end time
			nodeID,           // node ID
			testSubnet1.ID(), // subnet ID
			[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrTimestampNotBeforeStartTime)
	}

	// reset the timestamp
	env.state.SetTimestamp(defaultGenesisTime)

	// Case: Proposed validator already validating the subnet
	// First, add validator as validator of subnet
	subnetTx, err := env.txBuilder.NewAddSubnetValidatorTx(
		defaultWeight,                           // weight
		uint64(defaultValidateStartTime.Unix()), // start time
		uint64(defaultValidateEndTime.Unix()),   // end time
		nodeID,                                  // node ID
		testSubnet1.ID(),                        // subnet ID
		[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty,
		nil,
	)
	require.NoError(err)

	addSubnetValTx := subnetTx.Unsigned.(*txs.AddSubnetValidatorTx)
	staker, err = state.NewCurrentStaker(
		subnetTx.ID(),
		addSubnetValTx,
		defaultValidateStartTime,
		0,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(staker)
	env.state.AddTx(subnetTx, status.Committed)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	{
		// Node with ID nodeIDKey.PublicKey().Address() now validating subnet with ID testSubnet1.ID
		startTime := defaultValidateStartTime.Add(time.Second)
		duplicateSubnetTx, err := env.txBuilder.NewAddSubnetValidatorTx(
			defaultWeight,                         // weight
			uint64(startTime.Unix()),              // start time
			uint64(defaultValidateEndTime.Unix()), // end time
			nodeID,                                // node ID
			testSubnet1.ID(),                      // subnet ID
			[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      duplicateSubnetTx,
		}
		err = duplicateSubnetTx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrDuplicateValidator)
	}

	env.state.DeleteCurrentValidator(staker)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	{
		// Case: Duplicate signatures
		startTime := defaultValidateStartTime.Add(time.Second)
		tx, err := env.txBuilder.NewAddSubnetValidatorTx(
			defaultWeight,            // weight
			uint64(startTime.Unix()), // start time
			uint64(startTime.Add(defaultMinStakingDuration).Unix())+1, // end time
			nodeID,           // node ID
			testSubnet1.ID(), // subnet ID
			[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1], testSubnet1ControlKeys[2]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		// Duplicate a signature
		addSubnetValidatorTx := tx.Unsigned.(*txs.AddSubnetValidatorTx)
		input := addSubnetValidatorTx.SubnetAuth.(*secp256k1fx.Input)
		input.SigIndices = append(input.SigIndices, input.SigIndices[0])
		// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
		addSubnetValidatorTx.SyntacticallyVerified = false

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, secp256k1fx.ErrInputIndicesNotSortedUnique)
	}

	{
		// Case: Too few signatures
		startTime := defaultValidateStartTime.Add(time.Second)
		tx, err := env.txBuilder.NewAddSubnetValidatorTx(
			defaultWeight,            // weight
			uint64(startTime.Unix()), // start time
			uint64(startTime.Add(defaultMinStakingDuration).Unix()), // end time
			nodeID,           // node ID
			testSubnet1.ID(), // subnet ID
			[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[2]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		// Remove a signature
		addSubnetValidatorTx := tx.Unsigned.(*txs.AddSubnetValidatorTx)
		input := addSubnetValidatorTx.SubnetAuth.(*secp256k1fx.Input)
		input.SigIndices = input.SigIndices[1:]
		// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
		addSubnetValidatorTx.SyntacticallyVerified = false

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, errUnauthorizedSubnetModification)
	}

	{
		// Case: Control Signature from invalid key (keys[3] is not a control key)
		startTime := defaultValidateStartTime.Add(time.Second)
		tx, err := env.txBuilder.NewAddSubnetValidatorTx(
			defaultWeight,            // weight
			uint64(startTime.Unix()), // start time
			uint64(startTime.Add(defaultMinStakingDuration).Unix()), // end time
			nodeID,           // node ID
			testSubnet1.ID(), // subnet ID
			[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], preFundedKeys[1]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		// Replace a valid signature with one from keys[3]
		sig, err := preFundedKeys[3].SignHash(hashing.ComputeHash256(tx.Unsigned.Bytes()))
		require.NoError(err)
		copy(tx.Creds[1].(*secp256k1fx.Credential).Sigs[0][:], sig)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, errUnauthorizedSubnetModification)
	}

	{
		// Case: Proposed validator in pending validator set for subnet
		// First, add validator to pending validator set of subnet
		startTime := defaultValidateStartTime.Add(time.Second)
		tx, err := env.txBuilder.NewAddSubnetValidatorTx(
			defaultWeight,              // weight
			uint64(startTime.Unix())+1, // start time
			uint64(startTime.Add(defaultMinStakingDuration).Unix())+1, // end time
			nodeID,           // node ID
			testSubnet1.ID(), // subnet ID
			[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		addSubnetValTx := subnetTx.Unsigned.(*txs.AddSubnetValidatorTx)
		staker, err = state.NewCurrentStaker(
			subnetTx.ID(),
			addSubnetValTx,
			defaultValidateStartTime,
			0,
		)
		require.NoError(err)

		env.state.PutCurrentValidator(staker)
		env.state.AddTx(tx, status.Committed)
		env.state.SetHeight(dummyHeight)
		require.NoError(env.state.Commit())

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrDuplicateValidator)
	}
}

func TestBanffStandardTxExecutorAddValidator(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, true /*=postBanff*/, false /*=postCortina*/, false /*=postDurango*/)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	nodeID := ids.GenerateTestNodeID()

	{
		// Case: Validator's start time too early
		tx, err := env.txBuilder.NewAddValidatorTx(
			env.config.MinValidatorStake,
			uint64(defaultValidateStartTime.Unix())-1,
			uint64(defaultValidateEndTime.Unix()),
			nodeID,
			ids.ShortEmpty,
			reward.PercentDenominator,
			[]*secp256k1.PrivateKey{preFundedKeys[0]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrTimestampNotBeforeStartTime)
	}

	{
		// Case: Validator's start time too far in the future
		tx, err := env.txBuilder.NewAddValidatorTx(
			env.config.MinValidatorStake,
			uint64(defaultValidateStartTime.Add(MaxFutureStartTime).Unix()+1),
			uint64(defaultValidateStartTime.Add(MaxFutureStartTime).Add(defaultMinStakingDuration).Unix()+1),
			nodeID,
			ids.ShortEmpty,
			reward.PercentDenominator,
			[]*secp256k1.PrivateKey{preFundedKeys[0]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrFutureStakeTime)
	}

	{
		// Case: Validator in current validator set of primary network
		startTime := defaultValidateStartTime.Add(1 * time.Second)
		tx, err := env.txBuilder.NewAddValidatorTx(
			env.config.MinValidatorStake,                            // stake amount
			uint64(startTime.Unix()),                                // start time
			uint64(startTime.Add(defaultMinStakingDuration).Unix()), // end time
			nodeID,
			ids.ShortEmpty,
			reward.PercentDenominator, // shares
			[]*secp256k1.PrivateKey{preFundedKeys[0]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		addValTx := tx.Unsigned.(*txs.AddValidatorTx)
		staker, err := state.NewCurrentStaker(
			tx.ID(),
			addValTx,
			startTime,
			0,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAcceptState.PutCurrentValidator(staker)
		onAcceptState.AddTx(tx, status.Committed)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrAlreadyValidator)
	}

	{
		// Case: Validator in pending validator set of primary network
		startTime := defaultValidateStartTime.Add(1 * time.Second)
		tx, err := env.txBuilder.NewAddValidatorTx(
			env.config.MinValidatorStake,                            // stake amount
			uint64(startTime.Unix()),                                // start time
			uint64(startTime.Add(defaultMinStakingDuration).Unix()), // end time
			nodeID,
			ids.ShortEmpty,
			reward.PercentDenominator, // shares
			[]*secp256k1.PrivateKey{preFundedKeys[0]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		staker, err := state.NewPendingStaker(
			tx.ID(),
			tx.Unsigned.(*txs.AddValidatorTx),
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAcceptState.PutPendingValidator(staker)
		onAcceptState.AddTx(tx, status.Committed)

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrAlreadyValidator)
	}

	{
		// Case: Validator doesn't have enough tokens to cover stake amount
		startTime := defaultValidateStartTime.Add(1 * time.Second)
		tx, err := env.txBuilder.NewAddValidatorTx( // create the tx
			env.config.MinValidatorStake,
			uint64(startTime.Unix()),
			uint64(startTime.Add(defaultMinStakingDuration).Unix()),
			nodeID,
			ids.ShortEmpty,
			reward.PercentDenominator,
			[]*secp256k1.PrivateKey{preFundedKeys[0]},
			ids.ShortEmpty, // change addr
			nil,
		)
		require.NoError(err)

		// Remove all UTXOs owned by preFundedKeys[0]
		utxoIDs, err := env.state.UTXOIDs(preFundedKeys[0].PublicKey().Address().Bytes(), ids.Empty, math.MaxInt32)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		for _, utxoID := range utxoIDs {
			onAcceptState.DeleteUTXO(utxoID)
		}

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrFlowCheckFailed)
	}
}

// Verifies that [AddValidatorTx] and [AddDelegatorTx] are disabled post-Durango
func TestDurangoDisabledTransactions(t *testing.T) {
	type test struct {
		name        string
		buildTx     func(*environment) *txs.Tx
		expectedErr error
	}

	tests := []test{
		{
			name: "AddValidatorTx",
			buildTx: func(env *environment) *txs.Tx {
				var (
					nodeID    = ids.GenerateTestNodeID()
					chainTime = env.state.GetTimestamp()
					endTime   = chainTime.Add(defaultMaxStakingDuration)
				)

				tx, err := env.txBuilder.NewAddValidatorTx(
					defaultMinValidatorStake,
					0, // startTime
					uint64(endTime.Unix()),
					nodeID,
					ids.ShortEmpty,            // reward address,
					reward.PercentDenominator, // shares
					preFundedKeys,
					ids.ShortEmpty, // change address
					nil,            // memo
				)
				require.NoError(t, err)

				return tx
			},
			expectedErr: ErrAddValidatorTxPostDurango,
		},
		{
			name: "AddDelegatorTx",
			buildTx: func(env *environment) *txs.Tx {
				var primaryValidator *state.Staker
				it, err := env.state.GetCurrentStakerIterator()
				require.NoError(t, err)
				for it.Next() {
					staker := it.Value()
					if staker.Priority != txs.PrimaryNetworkValidatorCurrentPriority {
						continue
					}
					primaryValidator = staker
					break
				}
				it.Release()

				tx, err := env.txBuilder.NewAddDelegatorTx(
					defaultMinValidatorStake,
					0, // startTime
					uint64(primaryValidator.EndTime.Unix()),
					primaryValidator.NodeID,
					ids.ShortEmpty, // reward address,
					preFundedKeys,
					ids.ShortEmpty, // change address
					nil,            // memo
				)
				require.NoError(t, err)

				return tx
			},
			expectedErr: ErrAddDelegatorTxPostDurango,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			env := newEnvironment(t, true /*=postBanff*/, true /*=postCortina*/, true /*=postDurango*/)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
			require.NoError(err)

			tx := tt.buildTx(env)

			err = tx.Unsigned.Visit(&StandardTxExecutor{
				Backend: &env.backend,
				State:   onAcceptState,
				Tx:      tx,
			})
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

// Verifies that the Memo field is required to be empty post-Durango
func TestDurangoMemoField(t *testing.T) {
	type test struct {
		name      string
		setupTest func(env *environment, memoField []byte) (*txs.Tx, state.Diff)
	}

	tests := []test{
		{
			name: "AddSubnetValidatorTx",
			setupTest: func(env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				var primaryValidator *state.Staker
				it, err := env.state.GetCurrentStakerIterator()
				require.NoError(t, err)
				for it.Next() {
					staker := it.Value()
					if staker.Priority != txs.PrimaryNetworkValidatorCurrentPriority {
						continue
					}
					primaryValidator = staker
					break
				}
				it.Release()

				tx, err := env.txBuilder.NewAddSubnetValidatorTx(
					defaultMinValidatorStake,
					0, // startTime
					uint64(primaryValidator.EndTime.Unix()),
					primaryValidator.NodeID,
					testSubnet1.TxID,
					preFundedKeys,
					ids.ShortEmpty,
					memoField,
				)
				require.NoError(t, err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(t, err)
				return tx, onAcceptState
			},
		},
		{
			name: "CreateChainTx",
			setupTest: func(env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				tx, err := env.txBuilder.NewCreateChainTx(
					testSubnet1.TxID,
					[]byte{},             // genesisData
					ids.GenerateTestID(), // vmID
					[]ids.ID{},           // fxIDs
					"aaa",                // chain name
					preFundedKeys,
					ids.ShortEmpty,
					memoField,
				)
				require.NoError(t, err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(t, err)

				return tx, onAcceptState
			},
		},
		{
			name: "CreateSubnetTx",
			setupTest: func(env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				tx, err := env.txBuilder.NewCreateSubnetTx(
					1,
					[]ids.ShortID{ids.GenerateTestShortID()},
					preFundedKeys,
					ids.ShortEmpty,
					memoField,
				)
				require.NoError(t, err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(t, err)

				return tx, onAcceptState
			},
		},
		{
			name: "ImportTx",
			setupTest: func(env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				// Skip shared memory checks
				env.backend.Bootstrapped.Set(false)

				var (
					sourceChain  = env.ctx.XChainID
					sourceKey    = preFundedKeys[1]
					sourceAmount = 10 * units.Avax
				)

				sharedMemory := fundedSharedMemory(
					t,
					env,
					sourceKey,
					sourceChain,
					map[ids.ID]uint64{
						env.ctx.AVAXAssetID: sourceAmount,
					},
				)
				env.msm.SharedMemory = sharedMemory

				tx, err := env.txBuilder.NewImportTx(
					sourceChain,
					sourceKey.PublicKey().Address(),
					preFundedKeys,
					ids.ShortEmpty, // change address
					memoField,
				)
				require.NoError(t, err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(t, err)

				return tx, onAcceptState
			},
		},
		{
			name: "ExportTx",
			setupTest: func(env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				tx, err := env.txBuilder.NewExportTx(
					units.Avax,                // amount
					env.ctx.XChainID,          // destination chain
					ids.GenerateTestShortID(), // destination address
					preFundedKeys,
					ids.ShortEmpty, // change address
					memoField,
				)
				require.NoError(t, err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(t, err)

				return tx, onAcceptState
			},
		},
		{
			name: "RemoveSubnetValidatorTx",
			setupTest: func(env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				var primaryValidator *state.Staker
				it, err := env.state.GetCurrentStakerIterator()
				require.NoError(t, err)
				for it.Next() {
					staker := it.Value()
					if staker.Priority != txs.PrimaryNetworkValidatorCurrentPriority {
						continue
					}
					primaryValidator = staker
					break
				}
				it.Release()

				endTime := primaryValidator.EndTime
				subnetValTx, err := env.txBuilder.NewAddSubnetValidatorTx(
					defaultWeight,
					0,
					uint64(endTime.Unix()),
					primaryValidator.NodeID,
					testSubnet1.ID(),
					[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
					ids.ShortEmpty,
					nil,
				)
				require.NoError(t, err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(t, err)

				require.NoError(t, subnetValTx.Unsigned.Visit(&StandardTxExecutor{
					Backend: &env.backend,
					State:   onAcceptState,
					Tx:      subnetValTx,
				}))

				tx, err := env.txBuilder.NewRemoveSubnetValidatorTx(
					primaryValidator.NodeID,
					testSubnet1.ID(),
					preFundedKeys,
					ids.ShortEmpty,
					memoField,
				)
				require.NoError(t, err)

				return tx, onAcceptState
			},
		},
		{
			name: "TransformSubnetTx",
			setupTest: func(env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				tx, err := env.txBuilder.NewTransformSubnetTx(
					testSubnet1.TxID,          // subnetID
					ids.GenerateTestID(),      // assetID
					10,                        // initial supply
					10,                        // max supply
					0,                         // min consumption rate
					reward.PercentDenominator, // max consumption rate
					2,                         // min validator stake
					10,                        // max validator stake
					time.Minute,               // min stake duration
					time.Hour,                 // max stake duration
					1,                         // min delegation fees
					10,                        // min delegator stake
					1,                         // max validator weight factor
					80,                        // uptime requirement
					preFundedKeys,
					ids.ShortEmpty, // change address
					memoField,
				)
				require.NoError(t, err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(t, err)

				return tx, onAcceptState
			},
		},
		{
			name: "AddPermissionlessValidatorTx",
			setupTest: func(env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				var (
					nodeID    = ids.GenerateTestNodeID()
					chainTime = env.state.GetTimestamp()
					endTime   = chainTime.Add(defaultMaxStakingDuration)
				)
				sk, err := bls.NewSecretKey()
				require.NoError(t, err)

				tx, err := env.txBuilder.NewAddPermissionlessValidatorTx(
					env.config.MinValidatorStake,
					0, // start Time
					uint64(endTime.Unix()),
					nodeID,
					signer.NewProofOfPossession(sk),
					ids.ShortEmpty,            // reward address
					reward.PercentDenominator, // shares
					preFundedKeys,
					ids.ShortEmpty, // change address
					memoField,
				)
				require.NoError(t, err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(t, err)

				return tx, onAcceptState
			},
		},
		{
			name: "AddPermissionlessDelegatorTx",
			setupTest: func(env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				var primaryValidator *state.Staker
				it, err := env.state.GetCurrentStakerIterator()
				require.NoError(t, err)
				for it.Next() {
					staker := it.Value()
					if staker.Priority != txs.PrimaryNetworkValidatorCurrentPriority {
						continue
					}
					primaryValidator = staker
					break
				}
				it.Release()

				tx, err := env.txBuilder.NewAddPermissionlessDelegatorTx(
					defaultMinValidatorStake,
					0, // start Time
					uint64(primaryValidator.EndTime.Unix()),
					primaryValidator.NodeID,
					ids.ShortEmpty, // reward address
					preFundedKeys,
					ids.ShortEmpty, // change address
					memoField,
				)
				require.NoError(t, err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(t, err)

				return tx, onAcceptState
			},
		},
		{
			name: "TransferSubnetOwnershipTx",
			setupTest: func(env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				tx, err := env.txBuilder.NewTransferSubnetOwnershipTx(
					testSubnet1.TxID,
					1,
					[]ids.ShortID{ids.ShortEmpty},
					preFundedKeys,
					ids.ShortEmpty, // change address
					memoField,
				)
				require.NoError(t, err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(t, err)

				return tx, onAcceptState
			},
		},
		{
			name: "BaseTx",
			setupTest: func(env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				tx, err := env.txBuilder.NewBaseTx(
					1,
					secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.ShortEmpty},
					},
					preFundedKeys,
					ids.ShortEmpty,
					memoField,
				)
				require.NoError(t, err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(t, err)

				return tx, onAcceptState
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			env := newEnvironment(t, true /*=postBanff*/, true /*=postCortina*/, true /*=postDurango*/)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			// Populated memo field should error
			tx, onAcceptState := tt.setupTest(env, []byte{'m', 'e', 'm', 'o'})
			err := tx.Unsigned.Visit(&StandardTxExecutor{
				Backend: &env.backend,
				State:   onAcceptState,
				Tx:      tx,
			})
			require.ErrorIs(err, avax.ErrMemoTooLarge)

			// Empty memo field should not error
			tx, onAcceptState = tt.setupTest(env, []byte{})
			require.NoError(tx.Unsigned.Visit(&StandardTxExecutor{
				Backend: &env.backend,
				State:   onAcceptState,
				Tx:      tx,
			}))
		})
	}
}

// Returns a RemoveSubnetValidatorTx that passes syntactic verification.
// Memo field is empty as required post Durango activation
func newRemoveSubnetValidatorTx(t *testing.T) (*txs.RemoveSubnetValidatorTx, *txs.Tx) {
	t.Helper()

	creds := []verify.Verifiable{
		&secp256k1fx.Credential{
			Sigs: make([][65]byte, 1),
		},
		&secp256k1fx.Credential{
			Sigs: make([][65]byte, 1),
		},
	}
	unsignedTx := &txs.RemoveSubnetValidatorTx{
		BaseTx: txs.BaseTx{
			BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{
						TxID: ids.GenerateTestID(),
					},
					Asset: avax.Asset{
						ID: ids.GenerateTestID(),
					},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{0, 1},
						},
					},
				}},
				Outs: []*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: ids.GenerateTestID(),
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: 1,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
							},
						},
					},
				},
			},
		},
		Subnet: ids.GenerateTestID(),
		NodeID: ids.GenerateTestNodeID(),
		SubnetAuth: &secp256k1fx.Credential{
			Sigs: make([][65]byte, 1),
		},
	}
	tx := &txs.Tx{
		Unsigned: unsignedTx,
		Creds:    creds,
	}
	require.NoError(t, tx.Initialize(txs.Codec))
	return unsignedTx, tx
}

// mock implementations that can be used in tests
// for verifying RemoveSubnetValidatorTx.
type removeSubnetValidatorTxVerifyEnv struct {
	latestForkTime time.Time
	fx             *fx.MockFx
	flowChecker    *utxo.MockVerifier
	unsignedTx     *txs.RemoveSubnetValidatorTx
	tx             *txs.Tx
	state          *state.MockDiff
	staker         *state.Staker
}

// Returns mock implementations that can be used in tests
// for verifying RemoveSubnetValidatorTx.
func newValidRemoveSubnetValidatorTxVerifyEnv(t *testing.T, ctrl *gomock.Controller) removeSubnetValidatorTxVerifyEnv {
	t.Helper()

	now := time.Now()
	mockFx := fx.NewMockFx(ctrl)
	mockFlowChecker := utxo.NewMockVerifier(ctrl)
	unsignedTx, tx := newRemoveSubnetValidatorTx(t)
	mockState := state.NewMockDiff(ctrl)
	return removeSubnetValidatorTxVerifyEnv{
		latestForkTime: now,
		fx:             mockFx,
		flowChecker:    mockFlowChecker,
		unsignedTx:     unsignedTx,
		tx:             tx,
		state:          mockState,
		staker: &state.Staker{
			TxID:     ids.GenerateTestID(),
			NodeID:   ids.GenerateTestNodeID(),
			Priority: txs.SubnetPermissionedValidatorCurrentPriority,
		},
	}
}

func TestStandardExecutorRemoveSubnetValidatorTx(t *testing.T) {
	type test struct {
		name        string
		newExecutor func(*gomock.Controller) (*txs.RemoveSubnetValidatorTx, *StandardTxExecutor)
		expectedErr error
	}

	tests := []test{
		{
			name: "valid tx",
			newExecutor: func(ctrl *gomock.Controller) (*txs.RemoveSubnetValidatorTx, *StandardTxExecutor) {
				env := newValidRemoveSubnetValidatorTxVerifyEnv(t, ctrl)

				// Set dependency expectations.
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(env.staker, nil).Times(1)
				subnetOwner := fx.NewMockOwner(ctrl)
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(subnetOwner, nil).Times(1)
				env.fx.EXPECT().VerifyPermission(env.unsignedTx, env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(nil).Times(1)
				env.flowChecker.EXPECT().VerifySpend(
					env.unsignedTx, env.state, env.unsignedTx.Ins, env.unsignedTx.Outs, env.tx.Creds[:len(env.tx.Creds)-1], gomock.Any(),
				).Return(nil).Times(1)
				env.state.EXPECT().DeleteCurrentValidator(env.staker)
				env.state.EXPECT().DeleteUTXO(gomock.Any()).Times(len(env.unsignedTx.Ins))
				env.state.EXPECT().AddUTXO(gomock.Any()).Times(len(env.unsignedTx.Outs))
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:   env.latestForkTime,
							CortinaTime: env.latestForkTime,
							DurangoTime: env.latestForkTime,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			expectedErr: nil,
		},
		{
			name: "tx fails syntactic verification",
			newExecutor: func(ctrl *gomock.Controller) (*txs.RemoveSubnetValidatorTx, *StandardTxExecutor) {
				env := newValidRemoveSubnetValidatorTxVerifyEnv(t, ctrl)
				// Setting the subnet ID to the Primary Network ID makes the tx fail syntactic verification
				env.tx.Unsigned.(*txs.RemoveSubnetValidatorTx).Subnet = constants.PrimaryNetworkID
				env.state = state.NewMockDiff(ctrl)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:   env.latestForkTime,
							CortinaTime: env.latestForkTime,
							DurangoTime: env.latestForkTime,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			expectedErr: txs.ErrRemovePrimaryNetworkValidator,
		},
		{
			name: "node isn't a validator of the subnet",
			newExecutor: func(ctrl *gomock.Controller) (*txs.RemoveSubnetValidatorTx, *StandardTxExecutor) {
				env := newValidRemoveSubnetValidatorTxVerifyEnv(t, ctrl)
				env.state = state.NewMockDiff(ctrl)
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime)
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(nil, database.ErrNotFound)
				env.state.EXPECT().GetPendingValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(nil, database.ErrNotFound)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:   env.latestForkTime,
							CortinaTime: env.latestForkTime,
							DurangoTime: env.latestForkTime,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			expectedErr: ErrNotValidator,
		},
		{
			name: "validator is permissionless",
			newExecutor: func(ctrl *gomock.Controller) (*txs.RemoveSubnetValidatorTx, *StandardTxExecutor) {
				env := newValidRemoveSubnetValidatorTxVerifyEnv(t, ctrl)

				staker := *env.staker
				staker.Priority = txs.SubnetPermissionlessValidatorCurrentPriority

				// Set dependency expectations.
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime)
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(&staker, nil).Times(1)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:   env.latestForkTime,
							CortinaTime: env.latestForkTime,
							DurangoTime: env.latestForkTime,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			expectedErr: ErrRemovePermissionlessValidator,
		},
		{
			name: "tx has no credentials",
			newExecutor: func(ctrl *gomock.Controller) (*txs.RemoveSubnetValidatorTx, *StandardTxExecutor) {
				env := newValidRemoveSubnetValidatorTxVerifyEnv(t, ctrl)
				// Remove credentials
				env.tx.Creds = nil
				env.state = state.NewMockDiff(ctrl)
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime)
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(env.staker, nil)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:   env.latestForkTime,
							CortinaTime: env.latestForkTime,
							DurangoTime: env.latestForkTime,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			expectedErr: errWrongNumberOfCredentials,
		},
		{
			name: "can't find subnet",
			newExecutor: func(ctrl *gomock.Controller) (*txs.RemoveSubnetValidatorTx, *StandardTxExecutor) {
				env := newValidRemoveSubnetValidatorTxVerifyEnv(t, ctrl)
				env.state = state.NewMockDiff(ctrl)
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime)
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(env.staker, nil)
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(nil, database.ErrNotFound)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:   env.latestForkTime,
							CortinaTime: env.latestForkTime,
							DurangoTime: env.latestForkTime,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			expectedErr: database.ErrNotFound,
		},
		{
			name: "no permission to remove validator",
			newExecutor: func(ctrl *gomock.Controller) (*txs.RemoveSubnetValidatorTx, *StandardTxExecutor) {
				env := newValidRemoveSubnetValidatorTxVerifyEnv(t, ctrl)
				env.state = state.NewMockDiff(ctrl)
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime)
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(env.staker, nil)
				subnetOwner := fx.NewMockOwner(ctrl)
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(subnetOwner, nil)
				env.fx.EXPECT().VerifyPermission(gomock.Any(), env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(errTest)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:   env.latestForkTime,
							CortinaTime: env.latestForkTime,
							DurangoTime: env.latestForkTime,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			expectedErr: errUnauthorizedSubnetModification,
		},
		{
			name: "flow checker failed",
			newExecutor: func(ctrl *gomock.Controller) (*txs.RemoveSubnetValidatorTx, *StandardTxExecutor) {
				env := newValidRemoveSubnetValidatorTxVerifyEnv(t, ctrl)
				env.state = state.NewMockDiff(ctrl)
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime)
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(env.staker, nil)
				subnetOwner := fx.NewMockOwner(ctrl)
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(subnetOwner, nil)
				env.fx.EXPECT().VerifyPermission(gomock.Any(), env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(nil)
				env.flowChecker.EXPECT().VerifySpend(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(errTest)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:   env.latestForkTime,
							CortinaTime: env.latestForkTime,
							DurangoTime: env.latestForkTime,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			expectedErr: ErrFlowCheckFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			unsignedTx, executor := tt.newExecutor(ctrl)
			err := executor.RemoveSubnetValidatorTx(unsignedTx)
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

// Returns a TransformSubnetTx that passes syntactic verification.
// Memo field is empty as required post Durango activation
func newTransformSubnetTx(t *testing.T) (*txs.TransformSubnetTx, *txs.Tx) {
	t.Helper()

	creds := []verify.Verifiable{
		&secp256k1fx.Credential{
			Sigs: make([][65]byte, 1),
		},
		&secp256k1fx.Credential{
			Sigs: make([][65]byte, 1),
		},
	}
	unsignedTx := &txs.TransformSubnetTx{
		BaseTx: txs.BaseTx{
			BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{
						TxID: ids.GenerateTestID(),
					},
					Asset: avax.Asset{
						ID: ids.GenerateTestID(),
					},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{0, 1},
						},
					},
				}},
				Outs: []*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: ids.GenerateTestID(),
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: 1,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
							},
						},
					},
				},
			},
		},
		Subnet:                   ids.GenerateTestID(),
		AssetID:                  ids.GenerateTestID(),
		InitialSupply:            10,
		MaximumSupply:            10,
		MinConsumptionRate:       0,
		MaxConsumptionRate:       reward.PercentDenominator,
		MinValidatorStake:        2,
		MaxValidatorStake:        10,
		MinStakeDuration:         1,
		MaxStakeDuration:         2,
		MinDelegationFee:         reward.PercentDenominator,
		MinDelegatorStake:        1,
		MaxValidatorWeightFactor: 1,
		UptimeRequirement:        reward.PercentDenominator,
		SubnetAuth: &secp256k1fx.Credential{
			Sigs: make([][65]byte, 1),
		},
	}
	tx := &txs.Tx{
		Unsigned: unsignedTx,
		Creds:    creds,
	}
	require.NoError(t, tx.Initialize(txs.Codec))
	return unsignedTx, tx
}

// mock implementations that can be used in tests
// for verifying TransformSubnetTx.
type transformSubnetTxVerifyEnv struct {
	latestForkTime time.Time
	fx             *fx.MockFx
	flowChecker    *utxo.MockVerifier
	unsignedTx     *txs.TransformSubnetTx
	tx             *txs.Tx
	state          *state.MockDiff
	staker         *state.Staker
}

// Returns mock implementations that can be used in tests
// for verifying TransformSubnetTx.
func newValidTransformSubnetTxVerifyEnv(t *testing.T, ctrl *gomock.Controller) transformSubnetTxVerifyEnv {
	t.Helper()

	now := time.Now()
	mockFx := fx.NewMockFx(ctrl)
	mockFlowChecker := utxo.NewMockVerifier(ctrl)
	unsignedTx, tx := newTransformSubnetTx(t)
	mockState := state.NewMockDiff(ctrl)
	return transformSubnetTxVerifyEnv{
		latestForkTime: now,
		fx:             mockFx,
		flowChecker:    mockFlowChecker,
		unsignedTx:     unsignedTx,
		tx:             tx,
		state:          mockState,
		staker: &state.Staker{
			TxID:   ids.GenerateTestID(),
			NodeID: ids.GenerateTestNodeID(),
		},
	}
}

func TestStandardExecutorTransformSubnetTx(t *testing.T) {
	type test struct {
		name        string
		newExecutor func(*gomock.Controller) (*txs.TransformSubnetTx, *StandardTxExecutor)
		err         error
	}

	tests := []test{
		{
			name: "tx fails syntactic verification",
			newExecutor: func(ctrl *gomock.Controller) (*txs.TransformSubnetTx, *StandardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)
				// Setting the tx to nil makes the tx fail syntactic verification
				env.tx.Unsigned = (*txs.TransformSubnetTx)(nil)
				env.state = state.NewMockDiff(ctrl)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:   env.latestForkTime,
							CortinaTime: env.latestForkTime,
							DurangoTime: env.latestForkTime,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			err: txs.ErrNilTx,
		},
		{
			name: "max stake duration too large",
			newExecutor: func(ctrl *gomock.Controller) (*txs.TransformSubnetTx, *StandardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)
				env.unsignedTx.MaxStakeDuration = math.MaxUint32
				env.state = state.NewMockDiff(ctrl)
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:   env.latestForkTime,
							CortinaTime: env.latestForkTime,
							DurangoTime: env.latestForkTime,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			err: errMaxStakeDurationTooLarge,
		},
		{
			name: "fail subnet authorization",
			newExecutor: func(ctrl *gomock.Controller) (*txs.TransformSubnetTx, *StandardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)
				// Remove credentials
				env.tx.Creds = nil
				env.state = state.NewMockDiff(ctrl)
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:        env.latestForkTime,
							CortinaTime:      env.latestForkTime,
							DurangoTime:      env.latestForkTime,
							MaxStakeDuration: math.MaxInt64,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			err: errWrongNumberOfCredentials,
		},
		{
			name: "flow checker failed",
			newExecutor: func(ctrl *gomock.Controller) (*txs.TransformSubnetTx, *StandardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)
				env.state = state.NewMockDiff(ctrl)
				subnetOwner := fx.NewMockOwner(ctrl)
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime)
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(subnetOwner, nil)
				env.state.EXPECT().GetSubnetTransformation(env.unsignedTx.Subnet).Return(nil, database.ErrNotFound).Times(1)
				env.fx.EXPECT().VerifyPermission(gomock.Any(), env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(nil)
				env.flowChecker.EXPECT().VerifySpend(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(ErrFlowCheckFailed)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:        env.latestForkTime,
							CortinaTime:      env.latestForkTime,
							DurangoTime:      env.latestForkTime,
							MaxStakeDuration: math.MaxInt64,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			err: ErrFlowCheckFailed,
		},
		{
			name: "valid tx",
			newExecutor: func(ctrl *gomock.Controller) (*txs.TransformSubnetTx, *StandardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)

				// Set dependency expectations.
				subnetOwner := fx.NewMockOwner(ctrl)
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime)
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(subnetOwner, nil).Times(1)
				env.state.EXPECT().GetSubnetTransformation(env.unsignedTx.Subnet).Return(nil, database.ErrNotFound).Times(1)
				env.fx.EXPECT().VerifyPermission(env.unsignedTx, env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(nil).Times(1)
				env.flowChecker.EXPECT().VerifySpend(
					env.unsignedTx, env.state, env.unsignedTx.Ins, env.unsignedTx.Outs, env.tx.Creds[:len(env.tx.Creds)-1], gomock.Any(),
				).Return(nil).Times(1)
				env.state.EXPECT().AddSubnetTransformation(env.tx)
				env.state.EXPECT().SetCurrentSupply(env.unsignedTx.Subnet, env.unsignedTx.InitialSupply)
				env.state.EXPECT().DeleteUTXO(gomock.Any()).Times(len(env.unsignedTx.Ins))
				env.state.EXPECT().AddUTXO(gomock.Any()).Times(len(env.unsignedTx.Outs))
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config: &config.Config{
							BanffTime:        env.latestForkTime,
							CortinaTime:      env.latestForkTime,
							DurangoTime:      env.latestForkTime,
							MaxStakeDuration: math.MaxInt64,
						},
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					Tx:    env.tx,
					State: env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			unsignedTx, executor := tt.newExecutor(ctrl)
			err := executor.TransformSubnetTx(unsignedTx)
			require.ErrorIs(t, err, tt.err)
		})
	}
}
