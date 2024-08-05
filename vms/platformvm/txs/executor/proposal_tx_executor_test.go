// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	walletsigner "github.com/ava-labs/avalanchego/wallet/chain/p/signer"
)

func TestProposalTxExecuteAddDelegator(t *testing.T) {
	dummyHeight := uint64(1)
	rewardAddress := preFundedKeys[0].PublicKey().Address()
	nodeID := genesisNodeIDs[0]

	newValidatorID := ids.GenerateTestNodeID()
	newValidatorStartTime := uint64(defaultValidateStartTime.Add(5 * time.Second).Unix())
	newValidatorEndTime := uint64(defaultValidateEndTime.Add(-5 * time.Second).Unix())

	// [addMinStakeValidator] adds a new validator to the primary network's
	// pending validator set with the minimum staking amount
	addMinStakeValidator := func(env *environment) {
		require := require.New(t)

		builder, signer := env.factory.NewWallet(preFundedKeys[0])
		utx, err := builder.NewAddValidatorTx(
			&txs.Validator{
				NodeID: newValidatorID,
				Start:  newValidatorStartTime,
				End:    newValidatorEndTime,
				Wght:   env.config.MinValidatorStake,
			},
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{rewardAddress},
			},
			reward.PercentDenominator,
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		addValTx := tx.Unsigned.(*txs.AddValidatorTx)
		staker, err := state.NewCurrentStaker(
			tx.ID(),
			addValTx,
			addValTx.StartTime(),
			0,
		)
		require.NoError(err)

		env.state.PutCurrentValidator(staker)
		env.state.AddTx(tx, status.Committed)
		env.state.SetHeight(dummyHeight)
		require.NoError(env.state.Commit())
	}

	// [addMaxStakeValidator] adds a new validator to the primary network's
	// pending validator set with the maximum staking amount
	addMaxStakeValidator := func(env *environment) {
		require := require.New(t)

		builder, signer := env.factory.NewWallet(preFundedKeys[0])
		utx, err := builder.NewAddValidatorTx(
			&txs.Validator{
				NodeID: newValidatorID,
				Start:  newValidatorStartTime,
				End:    newValidatorEndTime,
				Wght:   env.config.MaxValidatorStake,
			},
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{rewardAddress},
			},
			reward.PercentDenominator,
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		addValTx := tx.Unsigned.(*txs.AddValidatorTx)
		staker, err := state.NewCurrentStaker(
			tx.ID(),
			addValTx,
			addValTx.StartTime(),
			0,
		)
		require.NoError(err)

		env.state.PutCurrentValidator(staker)
		env.state.AddTx(tx, status.Committed)
		env.state.SetHeight(dummyHeight)
		require.NoError(env.state.Commit())
	}

	env := newEnvironment(t, apricotPhase5)
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
			startTime:   uint64(defaultValidateStartTime.Unix()) + 1,
			endTime:     uint64(defaultValidateEndTime.Unix()) + 1,
			nodeID:      nodeID,
			feeKeys:     []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:       nil,
			AP3Time:     defaultGenesisTime,
			expectedErr: ErrPeriodMismatch,
		},
		{
			description: "validator not in the current or pending validator sets",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   uint64(defaultValidateStartTime.Add(5 * time.Second).Unix()),
			endTime:     uint64(defaultValidateEndTime.Add(-5 * time.Second).Unix()),
			nodeID:      newValidatorID,
			feeKeys:     []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:       nil,
			AP3Time:     defaultGenesisTime,
			expectedErr: database.ErrNotFound,
		},
		{
			description: "delegator starts before validator",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   newValidatorStartTime - 1, // start validating subnet before primary network
			endTime:     newValidatorEndTime,
			nodeID:      newValidatorID,
			feeKeys:     []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:       addMinStakeValidator,
			AP3Time:     defaultGenesisTime,
			expectedErr: ErrPeriodMismatch,
		},
		{
			description: "delegator stops before validator",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   newValidatorStartTime,
			endTime:     newValidatorEndTime + 1, // stop validating subnet after stopping validating primary network
			nodeID:      newValidatorID,
			feeKeys:     []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:       addMinStakeValidator,
			AP3Time:     defaultGenesisTime,
			expectedErr: ErrPeriodMismatch,
		},
		{
			description: "valid",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   newValidatorStartTime, // same start time as for primary network
			endTime:     newValidatorEndTime,   // same end time as for primary network
			nodeID:      newValidatorID,
			feeKeys:     []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:       addMinStakeValidator,
			AP3Time:     defaultGenesisTime,
			expectedErr: nil,
		},
		{
			description: "starts delegating at current timestamp",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   uint64(currentTimestamp.Unix()),
			endTime:     uint64(defaultValidateEndTime.Unix()),
			nodeID:      nodeID,
			feeKeys:     []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:       nil,
			AP3Time:     defaultGenesisTime,
			expectedErr: ErrTimestampNotBeforeStartTime,
		},
		{
			description: "tx fee paying key has no funds",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   uint64(defaultValidateStartTime.Unix()) + 1,
			endTime:     uint64(defaultValidateEndTime.Unix()),
			nodeID:      nodeID,
			feeKeys:     []*secp256k1.PrivateKey{preFundedKeys[1]},
			setup: func(env *environment) { // Remove all UTXOs owned by keys[1]
				utxoIDs, err := env.state.UTXOIDs(
					preFundedKeys[1].PublicKey().Address().Bytes(),
					ids.Empty,
					math.MaxInt32)
				require.NoError(t, err)

				for _, utxoID := range utxoIDs {
					env.state.DeleteUTXO(utxoID)
				}
				env.state.SetHeight(dummyHeight)
				require.NoError(t, env.state.Commit())
			},
			AP3Time:     defaultGenesisTime,
			expectedErr: ErrFlowCheckFailed,
		},
		{
			description: "over delegation before AP3",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   newValidatorStartTime, // same start time as for primary network
			endTime:     newValidatorEndTime,   // same end time as for primary network
			nodeID:      newValidatorID,
			feeKeys:     []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:       addMaxStakeValidator,
			AP3Time:     defaultValidateEndTime,
			expectedErr: nil,
		},
		{
			description: "over delegation after AP3",
			stakeAmount: env.config.MinDelegatorStake,
			startTime:   newValidatorStartTime, // same start time as for primary network
			endTime:     newValidatorEndTime,   // same end time as for primary network
			nodeID:      newValidatorID,
			feeKeys:     []*secp256k1.PrivateKey{preFundedKeys[0]},
			setup:       addMaxStakeValidator,
			AP3Time:     defaultGenesisTime,
			expectedErr: ErrOverDelegated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, apricotPhase5)
			env.config.UpgradeConfig.ApricotPhase3Time = tt.AP3Time

			builder, signer := env.factory.NewWallet(tt.feeKeys...)
			utx, err := builder.NewAddDelegatorTx(
				&txs.Validator{
					NodeID: tt.nodeID,
					Start:  tt.startTime,
					End:    tt.endTime,
					Wght:   tt.stakeAmount,
				},
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardAddress},
				},
			)
			require.NoError(err)
			tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
			require.NoError(err)

			if tt.setup != nil {
				tt.setup(env)
			}

			onCommitState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			onAbortState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
			executor := ProposalTxExecutor{
				OnCommitState: onCommitState,
				OnAbortState:  onAbortState,
				Backend:       &env.backend,
				FeeCalculator: feeCalculator,
				Tx:            tx,
			}
			err = tx.Unsigned.Visit(&executor)
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestProposalTxExecuteAddSubnetValidator(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, apricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	nodeID := genesisNodeIDs[0]
	{
		// Case: Proposed validator currently validating primary network
		// but stops validating subnet after stops validating primary network
		// (note that keys[0] is a genesis validator)
		builder, signer := env.factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
		utx, err := builder.NewAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(defaultValidateStartTime.Unix()) + 1,
					End:    uint64(defaultValidateEndTime.Unix()) + 1,
					Wght:   defaultWeight,
				},
				Subnet: testSubnet1.ID(),
			},
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrPeriodMismatch)
	}

	{
		// Case: Proposed validator currently validating primary network
		// and proposed subnet validation period is subset of
		// primary network validation period
		// (note that keys[0] is a genesis validator)
		builder, signer := env.factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
		utx, err := builder.NewAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(defaultValidateStartTime.Unix()) + 1,
					End:    uint64(defaultValidateEndTime.Unix()),
					Wght:   defaultWeight,
				},
				Subnet: testSubnet1.ID(),
			},
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		require.NoError(tx.Unsigned.Visit(&executor))
	}

	// Add a validator to pending validator set of primary network
	// Starts validating primary network 10 seconds after genesis
	pendingDSValidatorID := ids.GenerateTestNodeID()
	dsStartTime := defaultValidateStartTime.Add(10 * time.Second)
	dsEndTime := dsStartTime.Add(5 * defaultMinStakingDuration)

	builder, signer := env.factory.NewWallet(preFundedKeys[0])
	utx, err := builder.NewAddValidatorTx(
		&txs.Validator{
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
	addDSTx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
	require.NoError(err)

	{
		// Case: Proposed validator isn't in pending or current validator sets
		builder, signer := env.factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
		utx, err := builder.NewAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: pendingDSValidatorID,
					Start:  uint64(dsStartTime.Unix()), // start validating subnet before primary network
					End:    uint64(dsEndTime.Unix()),
					Wght:   defaultWeight,
				},
				Subnet: testSubnet1.ID(),
			},
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrNotValidator)
	}

	addValTx := addDSTx.Unsigned.(*txs.AddValidatorTx)
	staker, err := state.NewCurrentStaker(
		addDSTx.ID(),
		addValTx,
		addValTx.StartTime(),
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
		builder, signer := env.factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
		utx, err := builder.NewAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: pendingDSValidatorID,
					Start:  uint64(dsStartTime.Unix()) - 1, // start validating subnet before primary network
					End:    uint64(dsEndTime.Unix()),
					Wght:   defaultWeight,
				},
				Subnet: testSubnet1.ID(),
			},
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrPeriodMismatch)
	}

	{
		// Case: Proposed validator is pending validator of primary network
		// but stops validating subnet after primary network
		builder, signer := env.factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
		utx, err := builder.NewAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: pendingDSValidatorID,
					Start:  uint64(dsStartTime.Unix()),
					End:    uint64(dsEndTime.Unix()) + 1, // stop validating subnet after stopping validating primary network
					Wght:   defaultWeight,
				},
				Subnet: testSubnet1.ID(),
			},
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrPeriodMismatch)
	}

	{
		// Case: Proposed validator is pending validator of primary network and
		// period validating subnet is subset of time validating primary network
		builder, signer := env.factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
		utx, err := builder.NewAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: pendingDSValidatorID,
					Start:  uint64(dsStartTime.Unix()), // same start time as for primary network
					End:    uint64(dsEndTime.Unix()),   // same end time as for primary network
					Wght:   defaultWeight,
				},
				Subnet: testSubnet1.ID(),
			},
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		require.NoError(tx.Unsigned.Visit(&executor))
	}

	// Case: Proposed validator start validating at/before current timestamp
	// First, advance the timestamp
	newTimestamp := defaultValidateStartTime.Add(2 * time.Second)
	env.state.SetTimestamp(newTimestamp)

	{
		builder, signer := env.factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
		utx, err := builder.NewAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(newTimestamp.Unix()),
					End:    uint64(newTimestamp.Add(defaultMinStakingDuration).Unix()),
					Wght:   defaultWeight,
				},
				Subnet: testSubnet1.ID(),
			},
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrTimestampNotBeforeStartTime)
	}

	// reset the timestamp
	env.state.SetTimestamp(defaultValidateStartTime)

	// Case: Proposed validator already validating the subnet
	// First, add validator as validator of subnet
	builder, signer = env.factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
	uSubnetTx, err := builder.NewAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(defaultValidateStartTime.Unix()),
				End:    uint64(defaultValidateEndTime.Unix()),
				Wght:   defaultWeight,
			},
			Subnet: testSubnet1.ID(),
		},
	)
	require.NoError(err)
	subnetTx, err := walletsigner.SignUnsigned(context.Background(), signer, uSubnetTx)
	require.NoError(err)

	addSubnetValTx := subnetTx.Unsigned.(*txs.AddSubnetValidatorTx)
	staker, err = state.NewCurrentStaker(
		subnetTx.ID(),
		addSubnetValTx,
		addSubnetValTx.StartTime(),
		0,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(staker)
	env.state.AddTx(subnetTx, status.Committed)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	{
		// Node with ID nodeIDKey.PublicKey().Address() now validating subnet with ID testSubnet1.ID
		builder, signer := env.factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
		utx, err := builder.NewAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(defaultValidateStartTime.Unix()) + 1,
					End:    uint64(defaultValidateEndTime.Unix()),
					Wght:   defaultWeight,
				},
				Subnet: testSubnet1.ID(),
			},
		)
		require.NoError(err)
		duplicateSubnetTx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            duplicateSubnetTx,
		}
		err = duplicateSubnetTx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrDuplicateValidator)
	}

	env.state.DeleteCurrentValidator(staker)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	{
		// Case: Too few signatures
		builder, signer := env.factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
		utx, err := builder.NewAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(defaultValidateStartTime.Unix()) + 1,
					End:    uint64(defaultValidateStartTime.Add(defaultMinStakingDuration).Unix()) + 1,
					Wght:   defaultWeight,
				},
				Subnet: testSubnet1.ID(),
			},
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		// Remove a signature
		addSubnetValidatorTx := tx.Unsigned.(*txs.AddSubnetValidatorTx)
		input := addSubnetValidatorTx.SubnetAuth.(*secp256k1fx.Input)
		input.SigIndices = input.SigIndices[1:]
		// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
		addSubnetValidatorTx.SyntacticallyVerified = false

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, errUnauthorizedSubnetModification)
	}

	{
		// Case: Control Signature from invalid key (keys[3] is not a control key)
		builder, signer := env.factory.NewWallet(testSubnet1ControlKeys[0], preFundedKeys[1])
		utx, err := builder.NewAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(defaultValidateStartTime.Unix()) + 1,
					End:    uint64(defaultValidateStartTime.Add(defaultMinStakingDuration).Unix()) + 1,
					Wght:   defaultWeight,
				},
				Subnet: testSubnet1.ID(),
			},
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		// Replace a valid signature with one from keys[3]
		sig, err := preFundedKeys[3].SignHash(hashing.ComputeHash256(tx.Unsigned.Bytes()))
		require.NoError(err)
		copy(tx.Creds[0].(*secp256k1fx.Credential).Sigs[0][:], sig)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, errUnauthorizedSubnetModification)
	}

	{
		// Case: Proposed validator in pending validator set for subnet
		// First, add validator to pending validator set of subnet
		builder, signer := env.factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
		utx, err := builder.NewAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(defaultValidateStartTime.Unix()) + 1,
					End:    uint64(defaultValidateStartTime.Add(defaultMinStakingDuration).Unix()) + 1,
					Wght:   defaultWeight,
				},
				Subnet: testSubnet1.ID(),
			},
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		addSubnetValTx := subnetTx.Unsigned.(*txs.AddSubnetValidatorTx)
		staker, err = state.NewCurrentStaker(
			subnetTx.ID(),
			addSubnetValTx,
			addSubnetValTx.StartTime(),
			0,
		)
		require.NoError(err)

		env.state.PutCurrentValidator(staker)
		env.state.AddTx(tx, status.Committed)
		env.state.SetHeight(dummyHeight)
		require.NoError(env.state.Commit())

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrDuplicateValidator)
	}
}

func TestProposalTxExecuteAddValidator(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, apricotPhase5)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	nodeID := ids.GenerateTestNodeID()
	chainTime := env.state.GetTimestamp()

	{
		// Case: Validator's start time too early
		builder, signer := env.factory.NewWallet(preFundedKeys[0])
		utx, err := builder.NewAddValidatorTx(
			&txs.Validator{
				NodeID: nodeID,
				Start:  uint64(chainTime.Unix()),
				End:    uint64(defaultValidateEndTime.Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
			reward.PercentDenominator,
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrTimestampNotBeforeStartTime)
	}

	{
		nodeID := genesisNodeIDs[0]

		// Case: Validator already validating primary network
		builder, signer := env.factory.NewWallet(preFundedKeys[0])
		utx, err := builder.NewAddValidatorTx(
			&txs.Validator{
				NodeID: nodeID,
				Start:  uint64(defaultValidateStartTime.Unix()) + 1,
				End:    uint64(defaultValidateEndTime.Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
			reward.PercentDenominator,
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrAlreadyValidator)
	}

	{
		// Case: Validator in pending validator set of primary network
		startTime := defaultValidateStartTime.Add(1 * time.Second)
		builder, signer := env.factory.NewWallet(preFundedKeys[0])
		utx, err := builder.NewAddValidatorTx(
			&txs.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(startTime.Add(defaultMinStakingDuration).Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
			reward.PercentDenominator,
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		addValTx := tx.Unsigned.(*txs.AddValidatorTx)
		staker, err := state.NewCurrentStaker(
			tx.ID(),
			addValTx,
			addValTx.StartTime(),
			0,
		)
		require.NoError(err)

		env.state.PutPendingValidator(staker)
		env.state.AddTx(tx, status.Committed)
		dummyHeight := uint64(1)
		env.state.SetHeight(dummyHeight)
		require.NoError(env.state.Commit())

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrAlreadyValidator)
	}

	{
		// Case: Validator doesn't have enough tokens to cover stake amount
		builder, signer := env.factory.NewWallet(preFundedKeys[0])
		utx, err := builder.NewAddValidatorTx(
			&txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				Start:  uint64(defaultValidateStartTime.Unix()) + 1,
				End:    uint64(defaultValidateEndTime.Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
			reward.PercentDenominator,
		)
		require.NoError(err)
		tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
		require.NoError(err)

		// Remove all UTXOs owned by preFundedKeys[0]
		utxoIDs, err := env.state.UTXOIDs(preFundedKeys[0].PublicKey().Address().Bytes(), ids.Empty, math.MaxInt32)
		require.NoError(err)

		for _, utxoID := range utxoIDs {
			env.state.DeleteUTXO(utxoID)
		}

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
		executor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrFlowCheckFailed)
	}
}
