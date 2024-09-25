// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx/fxmock"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txstest"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo/utxomock"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// This tests that the math performed during TransformSubnetTx execution can
// never overflow
const _ time.Duration = math.MaxUint32 * time.Second

var errTest = errors.New("non-nil error")

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
			&txs.Validator{
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

		stateDiff, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         stateDiff,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
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
			&txs.Validator{
				NodeID: newValidatorID,
				Start:  uint64(newValidatorStartTime.Unix()),
				End:    uint64(newValidatorEndTime.Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			rewardsOwner,
			reward.PercentDenominator,
		)
		require.NoError(err)

		addValTx := tx.Unsigned.(*txs.AddValidatorTx)
		staker, err := state.NewCurrentStaker(
			tx.ID(),
			addValTx,
			newValidatorStartTime,
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
			&txs.Validator{
				NodeID: newValidatorID,
				Start:  uint64(newValidatorStartTime.Unix()),
				End:    uint64(newValidatorEndTime.Unix()),
				Wght:   env.config.MaxValidatorStake,
			},
			rewardsOwner,
			reward.PercentDenominator,
		)
		require.NoError(err)

		addValTx := tx.Unsigned.(*txs.AddValidatorTx)
		staker, err := state.NewCurrentStaker(
			tx.ID(),
			addValTx,
			newValidatorStartTime,
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
			expectedExecutionErr: ErrPeriodMismatch,
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
			expectedExecutionErr: ErrPeriodMismatch,
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
			expectedExecutionErr: ErrPeriodMismatch,
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
			expectedExecutionErr: ErrFlowCheckFailed,
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
				&txs.Validator{
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

			onAcceptState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			env.config.UpgradeConfig.BanffTime = onAcceptState.GetTimestamp()

			feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
			executor := StandardTxExecutor{
				Backend:       &env.backend,
				State:         onAcceptState,
				FeeCalculator: feeCalculator,
				Tx:            tx,
			}
			err = tx.Unsigned.Visit(&executor)
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
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(startTime.Unix()),
					End:    genesistest.DefaultValidatorEndTimeUnix + 1,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
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
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
					End:    genesistest.DefaultValidatorEndTimeUnix,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		require.NoError(tx.Unsigned.Visit(&executor))
	}

	// Add a validator to pending validator set of primary network
	// Starts validating primary network 10 seconds after genesis
	pendingDSValidatorID := ids.GenerateTestNodeID()
	dsStartTime := genesistest.DefaultValidatorStartTime.Add(10 * time.Second)
	dsEndTime := dsStartTime.Add(5 * defaultMinStakingDuration)

	wallet := newWallet(t, env, walletConfig{})
	addDSTx, err := wallet.IssueAddValidatorTx(
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

	{
		// Case: Proposed validator isn't in pending or current validator sets
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: pendingDSValidatorID,
					Start:  uint64(dsStartTime.Unix()), // start validating subnet before primary network
					End:    uint64(dsEndTime.Unix()),
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
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
		dsStartTime,
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
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: pendingDSValidatorID,
					Start:  uint64(dsStartTime.Unix()) - 1, // start validating subnet before primary network
					End:    uint64(dsEndTime.Unix()),
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrPeriodMismatch)
	}

	{
		// Case: Proposed validator is pending validator of primary network
		// but stops validating subnet after primary network
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: pendingDSValidatorID,
					Start:  uint64(dsStartTime.Unix()),
					End:    uint64(dsEndTime.Unix()) + 1, // stop validating subnet after stopping validating primary network
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrPeriodMismatch)
	}

	{
		// Case: Proposed validator is pending validator of primary network and
		// period validating subnet is subset of time validating primary network
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: pendingDSValidatorID,
					Start:  uint64(dsStartTime.Unix()), // same start time as for primary network
					End:    uint64(dsEndTime.Unix()),   // same end time as for primary network
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		require.NoError(tx.Unsigned.Visit(&executor))
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
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(newTimestamp.Unix()),
					End:    uint64(newTimestamp.Add(defaultMinStakingDuration).Unix()),
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
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
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  genesistest.DefaultValidatorStartTimeUnix,
				End:    genesistest.DefaultValidatorEndTimeUnix,
				Wght:   genesistest.DefaultValidatorWeight,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	addSubnetValTx := subnetTx.Unsigned.(*txs.AddSubnetValidatorTx)
	staker, err = state.NewCurrentStaker(
		subnetTx.ID(),
		addSubnetValTx,
		genesistest.DefaultValidatorStartTime,
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
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(startTime.Unix()),
					End:    genesistest.DefaultValidatorEndTimeUnix,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrDuplicateValidator)
	}

	env.state.DeleteCurrentValidator(staker)
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	{
		// Case: Duplicate signatures
		startTime := genesistest.DefaultValidatorStartTime.Add(time.Second)
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
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
		addSubnetValidatorTx := tx.Unsigned.(*txs.AddSubnetValidatorTx)
		input := addSubnetValidatorTx.SubnetAuth.(*secp256k1fx.Input)
		input.SigIndices = append(input.SigIndices, input.SigIndices[0])
		// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
		addSubnetValidatorTx.SyntacticallyVerified = false

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, secp256k1fx.ErrInputIndicesNotSortedUnique)
	}

	{
		// Case: Too few signatures
		startTime := genesistest.DefaultValidatorStartTime.Add(time.Second)
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
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
		addSubnetValidatorTx := tx.Unsigned.(*txs.AddSubnetValidatorTx)
		input := addSubnetValidatorTx.SubnetAuth.(*secp256k1fx.Input)
		input.SigIndices = input.SigIndices[1:]
		// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
		addSubnetValidatorTx.SyntacticallyVerified = false

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, errUnauthorizedSubnetModification)
	}

	{
		// Case: Control Signature from invalid key (keys[3] is not a control key)
		startTime := genesistest.DefaultValidatorStartTime.Add(time.Second)
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
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

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, errUnauthorizedSubnetModification)
	}

	{
		// Case: Proposed validator in pending validator set for subnet
		// First, add validator to pending validator set of subnet
		startTime := genesistest.DefaultValidatorStartTime.Add(time.Second)
		wallet := newWallet(t, env, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})
		tx, err := wallet.IssueAddSubnetValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(startTime.Unix()) + 1,
					End:    uint64(startTime.Add(defaultMinStakingDuration).Unix()) + 1,
					Wght:   genesistest.DefaultValidatorWeight,
				},
				Subnet: subnetID,
			},
		)
		require.NoError(err)

		addSubnetValTx := subnetTx.Unsigned.(*txs.AddSubnetValidatorTx)
		staker, err = state.NewCurrentStaker(
			subnetTx.ID(),
			addSubnetValTx,
			genesistest.DefaultValidatorStartTime,
			0,
		)
		require.NoError(err)

		require.NoError(env.state.PutCurrentValidator(staker))
		env.state.AddTx(tx, status.Committed)
		env.state.SetHeight(dummyHeight)
		require.NoError(env.state.Commit())

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
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
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  genesistest.DefaultValidatorStartTimeUnix + 1,
				End:    genesistest.DefaultValidatorEndTimeUnix,
				Wght:   genesistest.DefaultValidatorWeight,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	onAcceptState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAcceptState.SetSubnetManager(subnetID, ids.GenerateTestID(), []byte{'a', 'd', 'd', 'r', 'e', 's', 's'})

	executor := StandardTxExecutor{
		Backend: &env.backend,
		State:   onAcceptState,
		Tx:      tx,
	}
	err = tx.Unsigned.Visit(&executor)
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
			&txs.Validator{
				NodeID: nodeID,
				Start:  genesistest.DefaultValidatorStartTimeUnix - 1,
				End:    genesistest.DefaultValidatorEndTimeUnix,
				Wght:   env.config.MinValidatorStake,
			},
			rewardsOwner,
			reward.PercentDenominator,
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrTimestampNotBeforeStartTime)
	}

	{
		// Case: Validator in current validator set of primary network
		wallet := newWallet(t, env, walletConfig{})

		startTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
		tx, err := wallet.IssueAddValidatorTx(
			&txs.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(startTime.Add(defaultMinStakingDuration).Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			rewardsOwner,
			reward.PercentDenominator,
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

		require.NoError(onAcceptState.PutCurrentValidator(staker))
		onAcceptState.AddTx(tx, status.Committed)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrAlreadyValidator)
	}

	{
		// Case: Validator in pending validator set of primary network
		wallet := newWallet(t, env, walletConfig{})

		startTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
		tx, err := wallet.IssueAddValidatorTx(
			&txs.Validator{
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
			tx.Unsigned.(*txs.AddValidatorTx),
		)
		require.NoError(err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		require.NoError(onAcceptState.PutPendingValidator(staker))
		onAcceptState.AddTx(tx, status.Committed)

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			State:         onAcceptState,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrAlreadyValidator)
	}

	{
		// Case: Validator doesn't have enough tokens to cover stake amount
		wallet := newWallet(t, env, walletConfig{
			keys: genesistest.DefaultFundedKeys[:1],
		})

		startTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
		tx, err := wallet.IssueAddValidatorTx(
			&txs.Validator{
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

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		for _, utxoID := range utxoIDs {
			onAcceptState.DeleteUTXO(utxoID)
		}

		feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
		executor := StandardTxExecutor{
			Backend:       &env.backend,
			FeeCalculator: feeCalculator,
			State:         onAcceptState,
			Tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(err, ErrFlowCheckFailed)
	}
}

// Verifies that [AddValidatorTx] and [AddDelegatorTx] are disabled post-Durango
func TestDurangoDisabledTransactions(t *testing.T) {
	type test struct {
		name        string
		buildTx     func(t *testing.T, env *environment) *txs.Tx
		expectedErr error
	}

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	tests := []test{
		{
			name: "AddValidatorTx",
			buildTx: func(t *testing.T, env *environment) *txs.Tx {
				var (
					nodeID    = ids.GenerateTestNodeID()
					chainTime = env.state.GetTimestamp()
					endTime   = chainTime.Add(defaultMaxStakingDuration)
				)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddValidatorTx(
					&txs.Validator{
						NodeID: nodeID,
						Start:  0,
						End:    uint64(endTime.Unix()),
						Wght:   defaultMinValidatorStake,
					},
					rewardsOwner,
					reward.PercentDenominator,
				)
				require.NoError(t, err)

				return tx
			},
			expectedErr: ErrAddValidatorTxPostDurango,
		},
		{
			name: "AddDelegatorTx",
			buildTx: func(t *testing.T, env *environment) *txs.Tx {
				require := require.New(t)

				var primaryValidator *state.Staker
				it, err := env.state.GetCurrentStakerIterator()
				require.NoError(err)
				for it.Next() {
					staker := it.Value()
					if staker.Priority != txs.PrimaryNetworkValidatorCurrentPriority {
						continue
					}
					primaryValidator = staker
					break
				}
				it.Release()

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddDelegatorTx(
					&txs.Validator{
						NodeID: primaryValidator.NodeID,
						Start:  0,
						End:    uint64(primaryValidator.EndTime.Unix()),
						Wght:   defaultMinValidatorStake,
					},
					rewardsOwner,
				)
				require.NoError(err)

				return tx
			},
			expectedErr: ErrAddDelegatorTxPostDurango,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			env := newEnvironment(t, upgradetest.Durango)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
			require.NoError(err)

			tx := tt.buildTx(t, env)

			feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
			err = tx.Unsigned.Visit(&StandardTxExecutor{
				Backend:       &env.backend,
				State:         onAcceptState,
				FeeCalculator: feeCalculator,
				Tx:            tx,
			})
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

// Verifies that the Memo field is required to be empty post-Durango
func TestDurangoMemoField(t *testing.T) {
	type test struct {
		name      string
		setupTest func(t *testing.T, env *environment, memoField []byte) (*txs.Tx, state.Diff)
	}

	owners := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	tests := []test{
		{
			name: "AddSubnetValidatorTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				require := require.New(t)

				var primaryValidator *state.Staker
				it, err := env.state.GetCurrentStakerIterator()
				require.NoError(err)
				for it.Next() {
					staker := it.Value()
					if staker.Priority != txs.PrimaryNetworkValidatorCurrentPriority {
						continue
					}
					primaryValidator = staker
					break
				}
				it.Release()

				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})
				tx, err := wallet.IssueAddSubnetValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: primaryValidator.NodeID,
							Start:  0,
							End:    uint64(primaryValidator.EndTime.Unix()),
							Wght:   defaultMinValidatorStake,
						},
						Subnet: subnetID,
					},
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "CreateChainTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				require := require.New(t)

				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})

				tx, err := wallet.IssueCreateChainTx(
					subnetID,
					[]byte{},
					ids.GenerateTestID(),
					[]ids.ID{},
					"aaa",
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "CreateSubnetTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				require := require.New(t)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueCreateSubnetTx(
					owners,
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "ImportTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				require := require.New(t)

				var (
					sourceChain  = env.ctx.XChainID
					sourceKey    = genesistest.DefaultFundedKeys[1]
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
					rand.NewSource(0),
				)
				env.msm.SharedMemory = sharedMemory

				wallet := newWallet(t, env, walletConfig{
					chainIDs: []ids.ID{sourceChain},
				})

				tx, err := wallet.IssueImportTx(
					sourceChain,
					owners,
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "ExportTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				require := require.New(t)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueExportTx(
					env.ctx.XChainID,
					[]*avax.TransferableOutput{{
						Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          units.Avax,
							OutputOwners: *owners,
						},
					}},
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "RemoveSubnetValidatorTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				require := require.New(t)

				var primaryValidator *state.Staker
				it, err := env.state.GetCurrentStakerIterator()
				require.NoError(err)
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

				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})
				subnetValTx, err := wallet.IssueAddSubnetValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: primaryValidator.NodeID,
							Start:  0,
							End:    uint64(endTime.Unix()),
							Wght:   genesistest.DefaultValidatorWeight,
						},
						Subnet: subnetID,
					},
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(err)

				feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
				require.NoError(subnetValTx.Unsigned.Visit(&StandardTxExecutor{
					Backend:       &env.backend,
					State:         onAcceptState,
					FeeCalculator: feeCalculator,
					Tx:            subnetValTx,
				}))

				tx, err := wallet.IssueRemoveSubnetValidatorTx(
					primaryValidator.NodeID,
					subnetID,
					common.WithMemo(memoField),
				)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "TransformSubnetTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				require := require.New(t)

				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})

				tx, err := wallet.IssueTransformSubnetTx(
					subnetID,                  // subnetID
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
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "AddPermissionlessValidatorTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				require := require.New(t)
				var (
					nodeID    = ids.GenerateTestNodeID()
					chainTime = env.state.GetTimestamp()
					endTime   = chainTime.Add(defaultMaxStakingDuration)
				)
				sk, err := bls.NewSecretKey()
				require.NoError(err)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddPermissionlessValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: nodeID,
							Start:  0,
							End:    uint64(endTime.Unix()),
							Wght:   env.config.MinValidatorStake,
						},
						Subnet: constants.PrimaryNetworkID,
					},
					signer.NewProofOfPossession(sk),
					env.ctx.AVAXAssetID,
					owners,
					owners,
					reward.PercentDenominator,
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "AddPermissionlessDelegatorTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				require := require.New(t)

				var primaryValidator *state.Staker
				it, err := env.state.GetCurrentStakerIterator()
				require.NoError(err)
				for it.Next() {
					staker := it.Value()
					if staker.Priority != txs.PrimaryNetworkValidatorCurrentPriority {
						continue
					}
					primaryValidator = staker
					break
				}
				it.Release()

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddPermissionlessDelegatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: primaryValidator.NodeID,
							Start:  0,
							End:    uint64(primaryValidator.EndTime.Unix()),
							Wght:   defaultMinValidatorStake,
						},
						Subnet: constants.PrimaryNetworkID,
					},
					env.ctx.AVAXAssetID,
					owners,
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "TransferSubnetOwnershipTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				require := require.New(t)

				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})

				tx, err := wallet.IssueTransferSubnetOwnershipTx(
					subnetID,
					owners,
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
		{
			name: "BaseTx",
			setupTest: func(t *testing.T, env *environment, memoField []byte) (*txs.Tx, state.Diff) {
				require := require.New(t)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueBaseTx(
					[]*avax.TransferableOutput{
						{
							Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
								OutputOwners: secp256k1fx.OutputOwners{
									Threshold: 1,
									Addrs:     []ids.ShortID{ids.ShortEmpty},
								},
							},
						},
					},
					common.WithMemo(memoField),
				)
				require.NoError(err)

				onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
				require.NoError(err)
				return tx, onAcceptState
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			env := newEnvironment(t, upgradetest.Durango)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			feeCalculator := state.PickFeeCalculator(env.config, env.state)

			// Populated memo field should error
			tx, onAcceptState := tt.setupTest(t, env, []byte{'m', 'e', 'm', 'o'})
			err := tx.Unsigned.Visit(&StandardTxExecutor{
				Backend:       &env.backend,
				State:         onAcceptState,
				FeeCalculator: feeCalculator,
				Tx:            tx,
			})
			require.ErrorIs(err, avax.ErrMemoTooLarge)

			// Empty memo field should not error
			tx, onAcceptState = tt.setupTest(t, env, []byte{})
			require.NoError(tx.Unsigned.Visit(&StandardTxExecutor{
				Backend:       &env.backend,
				State:         onAcceptState,
				FeeCalculator: feeCalculator,
				Tx:            tx,
			}))
		})
	}
}

// Verifies that [TransformSubnetTx] is disabled post-Etna
func TestEtnaDisabledTransactions(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Etna)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	onAcceptState, err := state.NewDiff(env.state.GetLastAccepted(), env)
	require.NoError(err)

	tx := &txs.Tx{
		Unsigned: &txs.TransformSubnetTx{},
	}

	err = tx.Unsigned.Visit(&StandardTxExecutor{
		Backend: &env.backend,
		State:   onAcceptState,
		Tx:      tx,
	})
	require.ErrorIs(err, errTransformSubnetTxPostEtna)
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
	fx             *fxmock.Fx
	flowChecker    *utxomock.Verifier
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
	mockFx := fxmock.NewFx(ctrl)
	mockFlowChecker := utxomock.NewVerifier(ctrl)
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
				subnetOwner := fxmock.NewOwner(ctrl)
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(subnetOwner, nil).Times(1)
				env.fx.EXPECT().VerifyPermission(env.unsignedTx, env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(nil).Times(1)
				env.flowChecker.EXPECT().VerifySpend(
					env.unsignedTx, env.state, env.unsignedTx.Ins, env.unsignedTx.Outs, env.tx.Creds[:len(env.tx.Creds)-1], gomock.Any(),
				).Return(nil).Times(1)
				env.state.EXPECT().DeleteCurrentValidator(env.staker)
				env.state.EXPECT().DeleteUTXO(gomock.Any()).Times(len(env.unsignedTx.Ins))
				env.state.EXPECT().AddUTXO(gomock.Any()).Times(len(env.unsignedTx.Outs))

				cfg := &config.Config{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
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
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()

				cfg := &config.Config{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
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
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(nil, database.ErrNotFound)
				env.state.EXPECT().GetPendingValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(nil, database.ErrNotFound)

				cfg := &config.Config{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
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
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(&staker, nil).Times(1)

				cfg := &config.Config{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
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
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(env.staker, nil)

				cfg := &config.Config{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
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
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(env.staker, nil)
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(nil, database.ErrNotFound)

				cfg := &config.Config{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
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
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(env.staker, nil)
				subnetOwner := fxmock.NewOwner(ctrl)
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(subnetOwner, nil)
				env.fx.EXPECT().VerifyPermission(gomock.Any(), env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(errTest)

				cfg := &config.Config{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
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
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()
				env.state.EXPECT().GetCurrentValidator(env.unsignedTx.Subnet, env.unsignedTx.NodeID).Return(env.staker, nil)
				subnetOwner := fxmock.NewOwner(ctrl)
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(subnetOwner, nil)
				env.fx.EXPECT().VerifyPermission(gomock.Any(), env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(nil)
				env.flowChecker.EXPECT().VerifySpend(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(errTest)

				cfg := &config.Config{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			expectedErr: ErrFlowCheckFailed,
		},
		{
			name: "attempted to remove subnet validator after subnet manager is set",
			newExecutor: func(ctrl *gomock.Controller) (*txs.RemoveSubnetValidatorTx, *StandardTxExecutor) {
				env := newValidRemoveSubnetValidatorTxVerifyEnv(t, ctrl)
				env.state.EXPECT().GetSubnetManager(env.unsignedTx.Subnet).Return(ids.GenerateTestID(), []byte{'a', 'd', 'd', 'r', 'e', 's', 's'}, nil).AnyTimes()
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()

				cfg := &config.Config{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Etna, env.latestForkTime),
				}
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
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
			expectedErr: ErrRemoveValidatorManagedSubnet,
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
	fx             *fxmock.Fx
	flowChecker    *utxomock.Verifier
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
	mockFx := fxmock.NewFx(ctrl)
	mockFlowChecker := utxomock.NewVerifier(ctrl)
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
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()

				cfg := &config.Config{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
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
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()

				cfg := &config.Config{
					UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
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
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()

				cfg := &config.Config{
					UpgradeConfig:    upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
					MaxStakeDuration: math.MaxInt64,
				}

				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
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
				subnetOwner := fxmock.NewOwner(ctrl)
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(subnetOwner, nil)
				env.state.EXPECT().GetSubnetManager(env.unsignedTx.Subnet).Return(ids.Empty, nil, database.ErrNotFound).Times(1)
				env.state.EXPECT().GetSubnetTransformation(env.unsignedTx.Subnet).Return(nil, database.ErrNotFound).Times(1)
				env.fx.EXPECT().VerifyPermission(gomock.Any(), env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(nil)
				env.flowChecker.EXPECT().VerifySpend(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(ErrFlowCheckFailed)

				cfg := &config.Config{
					UpgradeConfig:    upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
					MaxStakeDuration: math.MaxInt64,
				}

				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			err: ErrFlowCheckFailed,
		},
		{
			name: "invalid if subnet manager is set",
			newExecutor: func(ctrl *gomock.Controller) (*txs.TransformSubnetTx, *StandardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)

				// Set dependency expectations.
				subnetOwner := fxmock.NewOwner(ctrl)
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(subnetOwner, nil).Times(1)
				env.state.EXPECT().GetSubnetManager(env.unsignedTx.Subnet).Return(ids.GenerateTestID(), make([]byte, 20), nil)
				env.state.EXPECT().GetSubnetTransformation(env.unsignedTx.Subnet).Return(nil, database.ErrNotFound).Times(1)
				env.fx.EXPECT().VerifyPermission(env.unsignedTx, env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(nil).Times(1)

				cfg := &config.Config{
					UpgradeConfig:    upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
					MaxStakeDuration: math.MaxInt64,
				}
				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
				}
				e.Bootstrapped.Set(true)
				return env.unsignedTx, e
			},
			err: errIsImmutable,
		},
		{
			name: "valid tx",
			newExecutor: func(ctrl *gomock.Controller) (*txs.TransformSubnetTx, *StandardTxExecutor) {
				env := newValidTransformSubnetTxVerifyEnv(t, ctrl)

				// Set dependency expectations.
				subnetOwner := fxmock.NewOwner(ctrl)
				env.state.EXPECT().GetTimestamp().Return(env.latestForkTime).AnyTimes()
				env.state.EXPECT().GetSubnetOwner(env.unsignedTx.Subnet).Return(subnetOwner, nil).Times(1)
				env.state.EXPECT().GetSubnetManager(env.unsignedTx.Subnet).Return(ids.Empty, nil, database.ErrNotFound).Times(1)
				env.state.EXPECT().GetSubnetTransformation(env.unsignedTx.Subnet).Return(nil, database.ErrNotFound).Times(1)
				env.fx.EXPECT().VerifyPermission(env.unsignedTx, env.unsignedTx.SubnetAuth, env.tx.Creds[len(env.tx.Creds)-1], subnetOwner).Return(nil).Times(1)
				env.flowChecker.EXPECT().VerifySpend(
					env.unsignedTx, env.state, env.unsignedTx.Ins, env.unsignedTx.Outs, env.tx.Creds[:len(env.tx.Creds)-1], gomock.Any(),
				).Return(nil).Times(1)
				env.state.EXPECT().AddSubnetTransformation(env.tx)
				env.state.EXPECT().SetCurrentSupply(env.unsignedTx.Subnet, env.unsignedTx.InitialSupply)
				env.state.EXPECT().DeleteUTXO(gomock.Any()).Times(len(env.unsignedTx.Ins))
				env.state.EXPECT().AddUTXO(gomock.Any()).Times(len(env.unsignedTx.Outs))

				cfg := &config.Config{
					UpgradeConfig:    upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, env.latestForkTime),
					MaxStakeDuration: math.MaxInt64,
				}

				feeCalculator := state.PickFeeCalculator(cfg, env.state)
				e := &StandardTxExecutor{
					Backend: &Backend{
						Config:       cfg,
						Bootstrapped: &utils.Atomic[bool]{},
						Fx:           env.fx,
						FlowChecker:  env.flowChecker,
						Ctx:          &snow.Context{},
					},
					FeeCalculator: feeCalculator,
					Tx:            env.tx,
					State:         env.state,
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

func TestStandardExecutorConvertSubnetTx(t *testing.T) {
	var (
		fx = &secp256k1fx.Fx{}
		vm = &secp256k1fx.TestVM{
			Log: logging.NoLog{},
		}
	)
	require.NoError(t, fx.InitializeVM(vm))

	var (
		ctx           = snowtest.Context(t, constants.PlatformChainID)
		defaultConfig = &config.Config{
			DynamicFeeConfig: genesis.LocalParams.DynamicFeeConfig,
			UpgradeConfig:    upgradetest.GetConfig(upgradetest.Latest),
		}
		baseState = statetest.New(t, statetest.Config{
			Upgrades: defaultConfig.UpgradeConfig,
		})
		wallet = txstest.NewWallet(
			t,
			ctx,
			defaultConfig,
			baseState,
			secp256k1fx.NewKeychain(genesistest.DefaultFundedKeys...),
			nil, // subnetIDs
			nil, // chainIDs
		)
		flowChecker = utxo.NewVerifier(
			ctx,
			&vm.Clk,
			fx,
		)
	)

	// Create the subnet
	createSubnetTx, err := wallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{},
	)
	require.NoError(t, err)

	diff, err := state.NewDiffOn(baseState)
	require.NoError(t, err)

	require.NoError(t, createSubnetTx.Unsigned.Visit(&StandardTxExecutor{
		Backend: &Backend{
			Config:       defaultConfig,
			Bootstrapped: utils.NewAtomic(true),
			Fx:           fx,
			FlowChecker:  flowChecker,
			Ctx:          ctx,
		},
		FeeCalculator: state.PickFeeCalculator(defaultConfig, baseState),
		Tx:            createSubnetTx,
		State:         diff,
	}))
	require.NoError(t, diff.Apply(baseState))
	require.NoError(t, baseState.Commit())

	subnetID := createSubnetTx.ID()
	tests := []struct {
		name           string
		builderOptions []common.Option
		updateExecutor func(executor *StandardTxExecutor)
		expectedErr    error
	}{
		{
			name: "invalid prior to E-Upgrade",
			updateExecutor: func(e *StandardTxExecutor) {
				e.Backend.Config = &config.Config{
					UpgradeConfig: upgradetest.GetConfig(upgradetest.Durango),
				}
			},
			expectedErr: errEtnaUpgradeNotActive,
		},
		{
			name: "tx fails syntactic verification",
			updateExecutor: func(e *StandardTxExecutor) {
				e.Backend.Ctx = snowtest.Context(t, ids.GenerateTestID())
			},
			expectedErr: avax.ErrWrongChainID,
		},
		{
			name: "invalid memo length",
			builderOptions: []common.Option{
				common.WithMemo([]byte("memo!")),
			},
			expectedErr: avax.ErrMemoTooLarge,
		},
		{
			name: "fail subnet authorization",
			updateExecutor: func(e *StandardTxExecutor) {
				e.State.SetSubnetOwner(subnetID, &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
				})
			},
			expectedErr: errUnauthorizedSubnetModification,
		},
		{
			name: "invalid if subnet is transformed",
			updateExecutor: func(e *StandardTxExecutor) {
				e.State.AddSubnetTransformation(&txs.Tx{Unsigned: &txs.TransformSubnetTx{
					Subnet: subnetID,
				}})
			},
			expectedErr: errIsImmutable,
		},
		{
			name: "invalid if subnet is converted",
			updateExecutor: func(e *StandardTxExecutor) {
				e.State.SetSubnetManager(subnetID, ids.GenerateTestID(), nil)
			},
			expectedErr: errIsImmutable,
		},
		{
			name: "invalid fee calculation",
			updateExecutor: func(e *StandardTxExecutor) {
				e.FeeCalculator = fee.NewStaticCalculator(e.Config.StaticFeeConfig)
			},
			expectedErr: fee.ErrUnsupportedTx,
		},
		{
			name: "insufficient fee",
			updateExecutor: func(e *StandardTxExecutor) {
				e.FeeCalculator = fee.NewDynamicCalculator(
					e.Config.DynamicFeeConfig.Weights,
					100*genesis.LocalParams.DynamicFeeConfig.MinPrice,
				)
			},
			expectedErr: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name: "valid tx",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Create the ConvertSubnetTx
			var (
				wallet = txstest.NewWallet(
					t,
					ctx,
					defaultConfig,
					baseState,
					secp256k1fx.NewKeychain(genesistest.DefaultFundedKeys...),
					[]ids.ID{subnetID},
					nil, // chainIDs
				)
				chainID = ids.GenerateTestID()
				address = utils.RandomBytes(32)
			)
			convertSubnetTx, err := wallet.IssueConvertSubnetTx(
				subnetID,
				chainID,
				address,
				test.builderOptions...,
			)
			require.NoError(err)

			diff, err := state.NewDiffOn(baseState)
			require.NoError(err)

			executor := &StandardTxExecutor{
				Backend: &Backend{
					Config:       defaultConfig,
					Bootstrapped: utils.NewAtomic(true),
					Fx:           fx,
					FlowChecker:  flowChecker,
					Ctx:          ctx,
				},
				FeeCalculator: state.PickFeeCalculator(defaultConfig, baseState),
				Tx:            convertSubnetTx,
				State:         diff,
			}
			if test.updateExecutor != nil {
				test.updateExecutor(executor)
			}

			err = convertSubnetTx.Unsigned.Visit(executor)
			require.ErrorIs(err, test.expectedErr)
			if err != nil {
				return
			}

			for utxoID := range convertSubnetTx.InputIDs() {
				_, err := diff.GetUTXO(utxoID)
				require.ErrorIs(err, database.ErrNotFound)
			}

			for _, expectedUTXO := range convertSubnetTx.UTXOs() {
				utxoID := expectedUTXO.InputID()
				utxo, err := diff.GetUTXO(utxoID)
				require.NoError(err)
				require.Equal(expectedUTXO, utxo)
			}

			stateChainID, stateAddress, err := diff.GetSubnetManager(subnetID)
			require.NoError(err)
			require.Equal(chainID, stateChainID)
			require.Equal(address, stateAddress)
		})
	}
}
