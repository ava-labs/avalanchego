// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestShiftRewardValidatorOnRewardTxCommit(t *testing.T) {
	require := require.New(t)
	env := newEnvironmentNoValidator(latestFork)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	// Add a continuous validator
	onParentState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	var (
		nodeID            = ids.GenerateTestNodeID()
		validatorDuration = defaultMinStakingDuration
		dummyStartTime    = time.Unix(0, 0)

		validatorAuthKey = preFundedKeys[4]
		addr             = validatorAuthKey.PublicKey().Address()
	)

	blsSK, err := bls.NewSecretKey()
	require.NoError(err)
	blsPOP := signer.NewProofOfPossession(blsSK)

	utxosHandler := utxo.NewHandler(env.ctx, env.clk, env.fx)
	ins, unstakedOuts, stakedOuts, signers, err := utxosHandler.Spend(
		env.state,
		preFundedKeys,
		env.config.MinValidatorStake, // stakeAmount
		env.config.AddPrimaryNetworkValidatorFee,
		addr, // changeAddr
	)
	require.NoError(err)

	// Create the tx
	continuousValidatorTx := &txs.AddContinuousValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    env.ctx.NetworkID,
			BlockchainID: env.ctx.ChainID,
			Ins:          ins,
			Outs:         unstakedOuts,
		}},
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  uint64(dummyStartTime.Unix()),
			End:    uint64(dummyStartTime.Add(validatorDuration).Unix()),
			Wght:   env.config.MinValidatorStake,
		},
		Signer: blsPOP,
		ValidatorAuthKey: &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		},
		StakeOuts: stakedOuts,
		ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		ValidatorRewardRestakeShares: 0,
		DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		DelegationShares: 20_000,
	}
	addContinuousValTx, err := txs.NewSigned(continuousValidatorTx, txs.Codec, signers)
	require.NoError(err)
	require.NoError(addContinuousValTx.SyntacticVerify(env.ctx))

	addValExecutor := StandardTxExecutor{
		State:   onParentState,
		Backend: &env.backend,
		Tx:      addContinuousValTx,
	}
	require.NoError(addContinuousValTx.Unsigned.Visit(&addValExecutor))
	onParentState.AddTx(addContinuousValTx, status.Committed)
	require.NoError(onParentState.Apply(env.state))
	require.NoError(env.state.Commit())

	// shift validator ahead a few times
	for i := 1; i < 10; i++ {
		continuousValidator, err := env.state.GetCurrentValidator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
		require.NoError(err)

		// advance time
		chainTime := env.state.GetTimestamp()
		nextChainTime := chainTime.Add(validatorDuration)
		env.state.SetTimestamp(nextChainTime)

		// create and execute reward tx
		tx, err := env.txBuilder.NewRewardValidatorTx(continuousValidator.TxID)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		txExecutor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			Tx:            tx,
		}
		require.NoError(tx.Unsigned.Visit(&txExecutor))
		require.NoError(txExecutor.OnCommitState.Apply(env.state))
		require.NoError(env.state.Commit())

		onCommitStakerIterator, err := env.state.GetCurrentStakerIterator()
		require.NoError(err)

		// check that post continuousStakingFork, staker is shifted ahead by its staking period
		var shiftedValidator *state.Staker
		for onCommitStakerIterator.Next() {
			nextStaker := onCommitStakerIterator.Value()
			if nextStaker.TxID == continuousValidator.TxID {
				shiftedValidator = nextStaker
				break
			}
		}
		onCommitStakerIterator.Release()
		require.NotNil(shiftedValidator)
		require.Equal(continuousValidator.StakingPeriod, shiftedValidator.StakingPeriod)
		require.Equal(continuousValidator.StartTime.Add(continuousValidator.StakingPeriod), shiftedValidator.StartTime)
		require.Equal(continuousValidator.NextTime.Add(continuousValidator.StakingPeriod), shiftedValidator.NextTime)
		require.Equal(continuousValidator.EndTime.Add(continuousValidator.StakingPeriod), shiftedValidator.EndTime)
	}

	continuousValidator, err := env.state.GetCurrentValidator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
	require.NoError(err)

	// stop the validator
	stopValidatorTx, err := env.txBuilder.NewStopStakerTx(
		addContinuousValTx.ID(),
		[]*secp256k1.PrivateKey{validatorAuthKey},
		addr,
	)
	require.NoError(err)

	diff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor := StandardTxExecutor{
		State:   diff,
		Backend: &env.backend,
		Tx:      stopValidatorTx,
	}
	require.NoError(stopValidatorTx.Unsigned.Visit(&txExecutor))
	require.NoError(txExecutor.State.Apply(env.state))
	require.NoError(env.state.Commit())

	// check that validator is stopped
	stakersIt, err := env.state.GetCurrentStakerIterator()
	require.NoError(err)

	var stoppedValidator *state.Staker
	for stakersIt.Next() {
		nextStaker := stakersIt.Value()
		if nextStaker.TxID == continuousValidator.TxID {
			stoppedValidator = nextStaker
			break
		}
	}
	stakersIt.Release()
	require.NotNil(stoppedValidator)
	require.Equal(continuousValidator.StakingPeriod, stoppedValidator.StakingPeriod)
	require.Equal(continuousValidator.StartTime, stoppedValidator.StartTime)
	require.Equal(continuousValidator.NextTime, stoppedValidator.NextTime)
	require.Equal(stoppedValidator.NextTime, stoppedValidator.EndTime)
}

func TestShiftRewardValidatorOnRewardTxAbort(t *testing.T) {
	require := require.New(t)
	env := newEnvironmentNoValidator(latestFork)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	// Add a continuous validator
	onParentState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	var (
		nodeID            = ids.GenerateTestNodeID()
		validatorDuration = defaultMinStakingDuration
		dummyStartTime    = time.Unix(0, 0)

		validatorAuthKey = preFundedKeys[4]
		addr             = validatorAuthKey.PublicKey().Address()
	)

	blsSK, err := bls.NewSecretKey()
	require.NoError(err)
	blsPOP := signer.NewProofOfPossession(blsSK)

	utxosHandler := utxo.NewHandler(env.ctx, env.clk, env.fx)
	ins, unstakedOuts, stakedOuts, signers, err := utxosHandler.Spend(
		env.state,
		preFundedKeys,
		env.config.MinValidatorStake, // stakeAmount
		env.config.AddPrimaryNetworkValidatorFee,
		addr, // changeAddr
	)
	require.NoError(err)

	// Create the tx
	continuousValidatorTx := &txs.AddContinuousValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    env.ctx.NetworkID,
			BlockchainID: env.ctx.ChainID,
			Ins:          ins,
			Outs:         unstakedOuts,
		}},
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  uint64(dummyStartTime.Unix()),
			End:    uint64(dummyStartTime.Add(validatorDuration).Unix()),
			Wght:   env.config.MinValidatorStake,
		},
		Signer: blsPOP,
		ValidatorAuthKey: &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		},
		StakeOuts: stakedOuts,
		ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		ValidatorRewardRestakeShares: 0,
		DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		DelegationShares: 20_000,
	}
	addContinuousValTx, err := txs.NewSigned(continuousValidatorTx, txs.Codec, signers)
	require.NoError(err)
	require.NoError(addContinuousValTx.SyntacticVerify(env.ctx))

	addValExecutor := StandardTxExecutor{
		State:   onParentState,
		Backend: &env.backend,
		Tx:      addContinuousValTx,
	}
	require.NoError(addContinuousValTx.Unsigned.Visit(&addValExecutor))
	onParentState.AddTx(addContinuousValTx, status.Committed)
	require.NoError(onParentState.Apply(env.state))
	require.NoError(env.state.Commit())

	// shift validator ahead a few times
	for i := 1; i < 10; i++ {
		continuousValidator, err := env.state.GetCurrentValidator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
		require.NoError(err)

		// advance time
		chainTime := env.state.GetTimestamp()
		nextChainTime := chainTime.Add(validatorDuration)
		env.state.SetTimestamp(nextChainTime)

		// create and execute reward tx
		tx, err := env.txBuilder.NewRewardValidatorTx(continuousValidator.TxID)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		txExecutor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			Tx:            tx,
		}
		require.NoError(tx.Unsigned.Visit(&txExecutor))
		require.NoError(txExecutor.OnAbortState.Apply(env.state))
		require.NoError(env.state.Commit())

		onAbortStakerIterator, err := env.state.GetCurrentStakerIterator()
		require.NoError(err)

		// check that post continuousStakingFork, validator is shifted ahead by its staking period
		var shiftedValidator *state.Staker
		for onAbortStakerIterator.Next() {
			nextStaker := onAbortStakerIterator.Value()
			if nextStaker.TxID == continuousValidator.TxID {
				shiftedValidator = nextStaker
				break
			}
		}
		onAbortStakerIterator.Release()
		require.NotNil(shiftedValidator)
		require.Equal(continuousValidator.StakingPeriod, shiftedValidator.StakingPeriod)
		require.Equal(continuousValidator.StartTime.Add(continuousValidator.StakingPeriod), shiftedValidator.StartTime)
		require.Equal(continuousValidator.NextTime.Add(continuousValidator.StakingPeriod), shiftedValidator.NextTime)
		require.Equal(continuousValidator.EndTime.Add(continuousValidator.StakingPeriod), shiftedValidator.EndTime)
	}

	continuousValidator, err := env.state.GetCurrentValidator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
	require.NoError(err)

	// stop the validator
	stopValidatorTx, err := env.txBuilder.NewStopStakerTx(
		addContinuousValTx.ID(),
		[]*secp256k1.PrivateKey{validatorAuthKey},
		addr,
	)
	require.NoError(err)

	diff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor := StandardTxExecutor{
		State:   diff,
		Backend: &env.backend,
		Tx:      stopValidatorTx,
	}
	require.NoError(stopValidatorTx.Unsigned.Visit(&txExecutor))
	require.NoError(txExecutor.State.Apply(env.state))
	require.NoError(env.state.Commit())

	// check that validator is stopped
	stakersIt, err := env.state.GetCurrentStakerIterator()
	require.NoError(err)

	var stoppedValidator *state.Staker
	for stakersIt.Next() {
		nextStaker := stakersIt.Value()
		if nextStaker.TxID == continuousValidator.TxID {
			stoppedValidator = nextStaker
			break
		}
	}
	stakersIt.Release()
	require.NotNil(stoppedValidator)
	require.Equal(continuousValidator.StakingPeriod, stoppedValidator.StakingPeriod)
	require.Equal(continuousValidator.StartTime, stoppedValidator.StartTime)
	require.Equal(continuousValidator.NextTime, stoppedValidator.NextTime)
	require.Equal(stoppedValidator.NextTime, stoppedValidator.EndTime)
}

func TestShiftRewardDelegatorOnRewardTxCommit(t *testing.T) {
	require := require.New(t)
	env := newEnvironmentNoValidator(latestFork)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	var (
		nodeID            = ids.GenerateTestNodeID()
		validatorDuration = defaultMaxStakingDuration
		delegatorDuration = defaultMinStakingDuration
		dummyStartTime    = time.Unix(0, 0)

		stakerAuthKey = preFundedKeys[4]
		addr          = stakerAuthKey.PublicKey().Address()
	)

	blsSK, err := bls.NewSecretKey()
	require.NoError(err)
	blsPOP := signer.NewProofOfPossession(blsSK)

	// Add a continuous validator
	onParentState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	utxosHandler := utxo.NewHandler(env.ctx, env.clk, env.fx)
	ins, unstakedOuts, stakedOuts, signers, err := utxosHandler.Spend(
		env.state,
		preFundedKeys,
		env.config.MinValidatorStake, // stakeAmount
		env.config.AddPrimaryNetworkValidatorFee,
		addr, // changeAddr
	)
	require.NoError(err)

	continuousValidatorTx := &txs.AddContinuousValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    env.ctx.NetworkID,
			BlockchainID: env.ctx.ChainID,
			Ins:          ins,
			Outs:         unstakedOuts,
		}},
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  uint64(dummyStartTime.Unix()),
			End:    uint64(dummyStartTime.Add(validatorDuration).Unix()),
			Wght:   env.config.MinValidatorStake,
		},
		Signer: blsPOP,
		ValidatorAuthKey: &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		},
		StakeOuts: stakedOuts,
		ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		ValidatorRewardRestakeShares: 0,
		DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		DelegationShares: 20_000,
	}
	addContinuousValTx, err := txs.NewSigned(continuousValidatorTx, txs.Codec, signers)
	require.NoError(err)
	require.NoError(addContinuousValTx.SyntacticVerify(env.ctx))

	addValExecutor := StandardTxExecutor{
		State:   onParentState,
		Backend: &env.backend,
		Tx:      addContinuousValTx,
	}
	require.NoError(addContinuousValTx.Unsigned.Visit(&addValExecutor))
	onParentState.AddTx(addContinuousValTx, status.Committed)
	require.NoError(onParentState.Apply(env.state))
	require.NoError(env.state.Commit())

	// Create the delegator tx
	onParentState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	ins, unstakedOuts, stakedOuts, signers, err = utxosHandler.Spend(
		env.state,
		preFundedKeys,
		env.config.MinDelegatorStake, // stakeAmount
		env.config.AddPrimaryNetworkDelegatorFee,
		addr, // changeAddr
	)
	require.NoError(err)

	continuousDelegatorTx := &txs.AddContinuousDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    env.ctx.NetworkID,
			BlockchainID: env.ctx.ChainID,
			Ins:          ins,
			Outs:         unstakedOuts,
		}},
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  uint64(dummyStartTime.Unix()),
			End:    uint64(dummyStartTime.Add(delegatorDuration).Unix()),
			Wght:   env.config.MinDelegatorStake,
		},
		DelegatorAuthKey: &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		},
		StakeOuts: stakedOuts,
		DelegationRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		DelegatorRewardRestakeShares: 0,
	}
	addContinuousDelTx, err := txs.NewSigned(continuousDelegatorTx, txs.Codec, signers)
	require.NoError(err)
	require.NoError(addContinuousDelTx.SyntacticVerify(env.ctx))

	addDelExecutor := StandardTxExecutor{
		State:   onParentState,
		Backend: &env.backend,
		Tx:      addContinuousDelTx,
	}
	require.NoError(addContinuousDelTx.Unsigned.Visit(&addDelExecutor))
	onParentState.AddTx(addContinuousDelTx, status.Committed)
	require.NoError(onParentState.Apply(env.state))
	require.NoError(env.state.Commit())

	// shift validator ahead a few times
	for i := 1; i < int(validatorDuration/delegatorDuration); i++ {
		delIt, err := env.state.GetCurrentDelegatorIterator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
		require.NoError(err)
		require.True(delIt.Next())
		continuousDelegator := delIt.Value()

		// advance time
		chainTime := env.state.GetTimestamp()
		nextChainTime := chainTime.Add(delegatorDuration)
		env.state.SetTimestamp(nextChainTime)

		// create and execute reward tx
		tx, err := env.txBuilder.NewRewardValidatorTx(continuousDelegator.TxID)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		txExecutor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			Tx:            tx,
		}
		require.NoError(tx.Unsigned.Visit(&txExecutor))
		require.NoError(txExecutor.OnCommitState.Apply(env.state))
		require.NoError(env.state.Commit())

		onCommitStakerIterator, err := env.state.GetCurrentStakerIterator()
		require.NoError(err)

		// check that post continuousStakingFork, staker is shifted ahead by its staking period
		var shiftedDelegator *state.Staker
		for onCommitStakerIterator.Next() {
			nextStaker := onCommitStakerIterator.Value()
			if nextStaker.TxID == continuousDelegator.TxID {
				shiftedDelegator = nextStaker
				break
			}
		}
		onCommitStakerIterator.Release()
		require.NotNil(shiftedDelegator)
		require.Equal(continuousDelegator.StakingPeriod, shiftedDelegator.StakingPeriod)
		require.Equal(continuousDelegator.StartTime.Add(continuousDelegator.StakingPeriod), shiftedDelegator.StartTime)
		require.Equal(continuousDelegator.NextTime.Add(continuousDelegator.StakingPeriod), shiftedDelegator.NextTime)
		require.Equal(continuousDelegator.EndTime.Add(continuousDelegator.StakingPeriod), shiftedDelegator.EndTime)
	}

	delIt, err := env.state.GetCurrentDelegatorIterator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
	require.NoError(err)
	require.True(delIt.Next())
	continuousDelegator := delIt.Value()

	// stop the delegator
	stopDelegatorTx, err := env.txBuilder.NewStopStakerTx(
		addContinuousDelTx.ID(),
		[]*secp256k1.PrivateKey{stakerAuthKey},
		addr,
	)
	require.NoError(err)

	diff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor := StandardTxExecutor{
		State:   diff,
		Backend: &env.backend,
		Tx:      stopDelegatorTx,
	}
	require.NoError(stopDelegatorTx.Unsigned.Visit(&txExecutor))
	require.NoError(txExecutor.State.Apply(env.state))
	require.NoError(env.state.Commit())

	// check that staker is stopped
	stakersIt, err := env.state.GetCurrentStakerIterator()
	require.NoError(err)

	var stoppedDelegator *state.Staker
	for stakersIt.Next() {
		nextStaker := stakersIt.Value()
		if nextStaker.TxID == continuousDelegator.TxID {
			stoppedDelegator = nextStaker
			break
		}
	}
	stakersIt.Release()
	require.NotNil(stoppedDelegator)
	require.Equal(continuousDelegator.StakingPeriod, stoppedDelegator.StakingPeriod)
	require.Equal(continuousDelegator.StartTime, stoppedDelegator.StartTime)
	require.Equal(continuousDelegator.NextTime, stoppedDelegator.NextTime)
	require.Equal(stoppedDelegator.NextTime, stoppedDelegator.EndTime)
}

func TestShiftRewardDelegatorOnRewardTxAbort(t *testing.T) {
	require := require.New(t)
	env := newEnvironmentNoValidator(latestFork)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	var (
		nodeID            = ids.GenerateTestNodeID()
		validatorDuration = defaultMaxStakingDuration
		delegatorDuration = defaultMinStakingDuration
		dummyStartTime    = time.Unix(0, 0)

		stakerAuthKey = preFundedKeys[4]
		addr          = stakerAuthKey.PublicKey().Address()
	)

	blsSK, err := bls.NewSecretKey()
	require.NoError(err)
	blsPOP := signer.NewProofOfPossession(blsSK)

	// Add a continuous validator
	onParentState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	utxosHandler := utxo.NewHandler(env.ctx, env.clk, env.fx)
	ins, unstakedOuts, stakedOuts, signers, err := utxosHandler.Spend(
		env.state,
		preFundedKeys,
		env.config.MinValidatorStake, // stakeAmount
		env.config.AddPrimaryNetworkValidatorFee,
		addr, // changeAddr
	)
	require.NoError(err)

	continuousValidatorTx := &txs.AddContinuousValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    env.ctx.NetworkID,
			BlockchainID: env.ctx.ChainID,
			Ins:          ins,
			Outs:         unstakedOuts,
		}},
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  uint64(dummyStartTime.Unix()),
			End:    uint64(dummyStartTime.Add(validatorDuration).Unix()),
			Wght:   env.config.MinValidatorStake,
		},
		Signer: blsPOP,
		ValidatorAuthKey: &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		},
		StakeOuts: stakedOuts,
		ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		ValidatorRewardRestakeShares: 0,
		DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		DelegationShares: 20_000,
	}
	addContinuousValTx, err := txs.NewSigned(continuousValidatorTx, txs.Codec, signers)
	require.NoError(err)
	require.NoError(addContinuousValTx.SyntacticVerify(env.ctx))

	addValExecutor := StandardTxExecutor{
		State:   onParentState,
		Backend: &env.backend,
		Tx:      addContinuousValTx,
	}
	require.NoError(addContinuousValTx.Unsigned.Visit(&addValExecutor))
	onParentState.AddTx(addContinuousValTx, status.Committed)
	require.NoError(onParentState.Apply(env.state))
	require.NoError(env.state.Commit())

	// Create the delegator tx
	onParentState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	ins, unstakedOuts, stakedOuts, signers, err = utxosHandler.Spend(
		env.state,
		preFundedKeys,
		env.config.MinDelegatorStake, // stakeAmount
		env.config.AddPrimaryNetworkDelegatorFee,
		addr, // changeAddr
	)
	require.NoError(err)

	continuousDelegatorTx := &txs.AddContinuousDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    env.ctx.NetworkID,
			BlockchainID: env.ctx.ChainID,
			Ins:          ins,
			Outs:         unstakedOuts,
		}},
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  uint64(dummyStartTime.Unix()),
			End:    uint64(dummyStartTime.Add(delegatorDuration).Unix()),
			Wght:   env.config.MinDelegatorStake,
		},
		DelegatorAuthKey: &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		},
		StakeOuts: stakedOuts,
		DelegationRewardsOwner: &secp256k1fx.OutputOwners{
			Addrs:     []ids.ShortID{addr},
			Threshold: 1,
		},
		DelegatorRewardRestakeShares: 0,
	}
	addContinuousDelTx, err := txs.NewSigned(continuousDelegatorTx, txs.Codec, signers)
	require.NoError(err)
	require.NoError(addContinuousDelTx.SyntacticVerify(env.ctx))

	addDelExecutor := StandardTxExecutor{
		State:   onParentState,
		Backend: &env.backend,
		Tx:      addContinuousDelTx,
	}
	require.NoError(addContinuousDelTx.Unsigned.Visit(&addDelExecutor))
	onParentState.AddTx(addContinuousDelTx, status.Committed)
	require.NoError(onParentState.Apply(env.state))
	require.NoError(env.state.Commit())

	// shift validator ahead a few times
	for i := 1; i < int(validatorDuration/delegatorDuration); i++ {
		delIt, err := env.state.GetCurrentDelegatorIterator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
		require.NoError(err)
		require.True(delIt.Next())
		continuousDelegator := delIt.Value()

		// advance time
		chainTime := env.state.GetTimestamp()
		nextChainTime := chainTime.Add(delegatorDuration)
		env.state.SetTimestamp(nextChainTime)

		// create and execute reward tx
		tx, err := env.txBuilder.NewRewardValidatorTx(continuousDelegator.TxID)
		require.NoError(err)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(err)

		txExecutor := ProposalTxExecutor{
			OnCommitState: onCommitState,
			OnAbortState:  onAbortState,
			Backend:       &env.backend,
			Tx:            tx,
		}
		require.NoError(tx.Unsigned.Visit(&txExecutor))
		require.NoError(txExecutor.OnAbortState.Apply(env.state))
		require.NoError(env.state.Commit())

		onAbortStakerIterator, err := env.state.GetCurrentStakerIterator()
		require.NoError(err)

		// check that post continuousStakingFork, staker is shifted ahead by its staking period
		var shiftedDelegator *state.Staker
		for onAbortStakerIterator.Next() {
			nextStaker := onAbortStakerIterator.Value()
			if nextStaker.TxID == continuousDelegator.TxID {
				shiftedDelegator = nextStaker
				break
			}
		}
		onAbortStakerIterator.Release()
		require.NotNil(shiftedDelegator)
		require.Equal(continuousDelegator.StakingPeriod, shiftedDelegator.StakingPeriod)
		require.Equal(continuousDelegator.StartTime.Add(continuousDelegator.StakingPeriod), shiftedDelegator.StartTime)
		require.Equal(continuousDelegator.NextTime.Add(continuousDelegator.StakingPeriod), shiftedDelegator.NextTime)
		require.Equal(continuousDelegator.EndTime.Add(continuousDelegator.StakingPeriod), shiftedDelegator.EndTime)
	}

	delIt, err := env.state.GetCurrentDelegatorIterator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
	require.NoError(err)
	require.True(delIt.Next())
	continuousDelegator := delIt.Value()

	// stop the delegator
	stopDelegatorTx, err := env.txBuilder.NewStopStakerTx(
		addContinuousDelTx.ID(),
		[]*secp256k1.PrivateKey{stakerAuthKey},
		addr,
	)
	require.NoError(err)

	diff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor := StandardTxExecutor{
		State:   diff,
		Backend: &env.backend,
		Tx:      stopDelegatorTx,
	}
	require.NoError(stopDelegatorTx.Unsigned.Visit(&txExecutor))
	require.NoError(txExecutor.State.Apply(env.state))
	require.NoError(env.state.Commit())

	// check that staker is stopped
	stakersIt, err := env.state.GetCurrentStakerIterator()
	require.NoError(err)

	var stoppedDelegator *state.Staker
	for stakersIt.Next() {
		nextStaker := stakersIt.Value()
		if nextStaker.TxID == continuousDelegator.TxID {
			stoppedDelegator = nextStaker
			break
		}
	}
	stakersIt.Release()
	require.NotNil(stoppedDelegator)
	require.Equal(continuousDelegator.StakingPeriod, stoppedDelegator.StakingPeriod)
	require.Equal(continuousDelegator.StartTime, stoppedDelegator.StartTime)
	require.Equal(continuousDelegator.NextTime, stoppedDelegator.NextTime)
	require.Equal(stoppedDelegator.NextTime, stoppedDelegator.EndTime)
}

func TestCortinaForkRewardValidatorTxExecuteOnCommit(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(cortinaFork)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	currentStakerIterator, err := env.state.GetCurrentStakerIterator()
	require.NoError(err)
	require.True(currentStakerIterator.Next())

	stakerToRemove := currentStakerIterator.Value()
	currentStakerIterator.Release()

	stakerToRemoveTxIntf, _, err := env.state.GetTx(stakerToRemove.TxID)
	require.NoError(err)
	stakerToRemoveTx := stakerToRemoveTxIntf.Unsigned.(*txs.AddValidatorTx)

	// Case 1: Chain timestamp is wrong
	tx, err := env.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&txExecutor)
	require.ErrorIs(err, ErrRemoveStakerTooEarly)

	// Advance chain timestamp to time that next validator leaves
	env.state.SetTimestamp(stakerToRemove.EndTime)

	// Case 2: Wrong validator
	tx, err = env.txBuilder.NewRewardValidatorTx(ids.GenerateTestID())
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor = ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&txExecutor)
	require.ErrorIs(err, ErrRemoveWrongStaker)

	// Case 3: Happy path
	tx, err = env.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor = ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&txExecutor))

	onCommitStakerIterator, err := txExecutor.OnCommitState.GetCurrentStakerIterator()
	require.NoError(err)

	stakerRemoved := true
	for onCommitStakerIterator.Next() {
		nextStaker := onCommitStakerIterator.Value()
		if nextStaker.TxID == stakerToRemove.TxID {
			stakerRemoved = false
			break
		}
	}
	onCommitStakerIterator.Release()
	require.True(stakerRemoved)

	// check that stake/reward is given back
	stakeOwners := stakerToRemoveTx.StakeOuts[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()

	// Get old balances
	oldBalance, err := avax.GetBalance(env.state, stakeOwners)
	require.NoError(err)

	require.NoError(txExecutor.OnCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	onCommitBalance, err := avax.GetBalance(env.state, stakeOwners)
	require.NoError(err)
	require.Equal(oldBalance+stakerToRemove.Weight+27697, onCommitBalance)
}

func TestCortinaStakingForkRewardValidatorTxExecuteOnAbort(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(latestFork)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	currentStakerIterator, err := env.state.GetCurrentStakerIterator()
	require.NoError(err)
	require.True(currentStakerIterator.Next())

	stakerToRemove := currentStakerIterator.Value()
	currentStakerIterator.Release()

	stakerToRemoveTxIntf, _, err := env.state.GetTx(stakerToRemove.TxID)
	require.NoError(err)
	stakerToRemoveTx := stakerToRemoveTxIntf.Unsigned.(*txs.AddValidatorTx)

	// Case 1: Chain timestamp is wrong
	tx, err := env.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&txExecutor)
	require.ErrorIs(err, ErrRemoveStakerTooEarly)

	// Advance chain timestamp to time that next validator leaves
	env.state.SetTimestamp(stakerToRemove.EndTime)

	// Case 2: Wrong validator
	tx, err = env.txBuilder.NewRewardValidatorTx(ids.GenerateTestID())
	require.NoError(err)

	txExecutor = ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	err = tx.Unsigned.Visit(&txExecutor)
	require.ErrorIs(err, ErrRemoveWrongStaker)

	// Case 3: Happy path
	tx, err = env.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	require.NoError(err)

	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor = ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&txExecutor))

	onAbortStakerIterator, err := txExecutor.OnAbortState.GetCurrentStakerIterator()
	require.NoError(err)

	stakerRemoved := true
	for onAbortStakerIterator.Next() {
		nextStaker := onAbortStakerIterator.Value()
		if nextStaker.TxID == stakerToRemove.TxID {
			stakerRemoved = false
			break
		}
	}
	onAbortStakerIterator.Release()
	require.True(stakerRemoved)

	// check that stake/reward isn't given back
	stakeOwners := stakerToRemoveTx.StakeOuts[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()

	// Get old balances
	oldBalance, err := avax.GetBalance(env.state, stakeOwners)
	require.NoError(err)

	require.NoError(txExecutor.OnAbortState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	onAbortBalance, err := avax.GetBalance(env.state, stakeOwners)
	require.NoError(err)
	require.Equal(oldBalance+stakerToRemove.Weight, onAbortBalance)
}

func TestCortinaForkRewardDelegatorTxExecuteOnCommit(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(banffFork)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := uint64(defaultValidateStartTime.Unix()) + 1
	vdrEndTime := uint64(defaultValidateStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestNodeID()

	vdrTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake, // stakeAmt
		vdrStartTime,
		vdrEndTime,
		vdrNodeID,        // node ID
		vdrRewardAddress, // reward address
		reward.PercentDenominator/4,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime

	delTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		delStartTime,
		delEndTime,
		vdrNodeID,
		delRewardAddress,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		ids.ShortEmpty, // Change address
	)
	require.NoError(err)

	addValTx := vdrTx.Unsigned.(*txs.AddValidatorTx)
	vdrStaker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		addValTx,
		addValTx.StartTime(),
		addValTx.EndTime(),
		0,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		addDelTx,
		addDelTx.StartTime(),
		addDelTx.EndTime(),
		1000000,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(vdrStaker)
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// test validator stake
	vdrSet, ok := env.config.Validators.Get(constants.PrimaryNetworkID)
	require.True(ok)

	stake := vdrSet.GetWeight(vdrNodeID)
	require.Equal(env.config.MinValidatorStake+env.config.MinDelegatorStake, stake)

	tx, err := env.txBuilder.NewRewardValidatorTx(delTx.ID())
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&txExecutor))

	vdrDestSet := set.Set[ids.ShortID]{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := set.Set[ids.ShortID]{}
	delDestSet.Add(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)

	require.NoError(txExecutor.OnCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Since the tx was committed, the delegator and the delegatee should be rewarded.
	// The delegator reward should be higher since the delegatee's share is 25%.
	commitVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	vdrReward, err := math.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	require.NotZero(vdrReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := math.Sub(commitDelBalance, oldDelBalance)
	require.NoError(err)
	require.NotZero(delReward, "expected delegator balance to increase because of reward")

	require.Less(vdrReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	require.Equal(expectedReward, delReward+vdrReward, "expected total reward to be %d but is %d", expectedReward, delReward+vdrReward)

	require.Equal(env.config.MinValidatorStake, vdrSet.GetWeight(vdrNodeID))
}

func TestRewardDelegatorTxExecuteOnCommitPostDelegateeDeferral(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(latestFork)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := uint64(defaultValidateStartTime.Unix()) + 1
	vdrEndTime := uint64(defaultValidateStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestNodeID()

	vdrTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake,
		vdrStartTime,
		vdrEndTime,
		vdrNodeID,
		vdrRewardAddress,
		reward.PercentDenominator/4,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		ids.ShortEmpty, /*=changeAddr*/
	)
	require.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime

	delTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		delStartTime,
		delEndTime,
		vdrNodeID,
		delRewardAddress,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		ids.ShortEmpty, /*=changeAddr*/
	)
	require.NoError(err)

	addValTx := vdrTx.Unsigned.(*txs.AddValidatorTx)
	vdrRewardAmt := uint64(2000000)
	vdrStaker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		addValTx,
		time.Unix(int64(vdrStartTime), 0),
		time.Unix(int64(vdrEndTime), 0),
		vdrRewardAmt,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delRewardAmt := uint64(1000000)
	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		addDelTx,
		time.Unix(int64(delStartTime), 0),
		time.Unix(int64(delEndTime), 0),
		delRewardAmt,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(vdrStaker)
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(vdrEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	vdrDestSet := set.Set[ids.ShortID]{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := set.Set[ids.ShortID]{}
	delDestSet.Add(delRewardAddress)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)

	// test validator stake
	vdrSet, ok := env.config.Validators.Get(constants.PrimaryNetworkID)
	require.True(ok)

	stake := vdrSet.GetWeight(vdrNodeID)
	require.Equal(env.config.MinValidatorStake+env.config.MinDelegatorStake, stake)

	tx, err := env.txBuilder.NewRewardValidatorTx(delTx.ID())
	require.NoError(err)

	// Create Delegator Diff
	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&txExecutor))

	// The delegator should be rewarded if the ProposalTx is committed. Since the
	// delegatee's share is 25%, we expect the delegator to receive 75% of the reward.
	// Since this is post [CortinaTime], the delegatee should not be rewarded until a
	// RewardValidatorTx is issued for the delegatee.
	numDelStakeUTXOs := uint32(len(delTx.Unsigned.InputIDs()))
	delRewardUTXOID := &avax.UTXOID{
		TxID:        delTx.ID(),
		OutputIndex: numDelStakeUTXOs + 1,
	}

	utxo, err := onCommitState.GetUTXO(delRewardUTXOID.InputID())
	require.NoError(err)
	require.IsType(&secp256k1fx.TransferOutput{}, utxo.Out)
	castUTXO := utxo.Out.(*secp256k1fx.TransferOutput)
	require.Equal(delRewardAmt*3/4, castUTXO.Amt, "expected delegator balance to increase by 3/4 of reward amount")
	require.True(delDestSet.Equals(castUTXO.AddressesSet()), "expected reward UTXO to be issued to delDestSet")

	preCortinaVdrRewardUTXOID := &avax.UTXOID{
		TxID:        delTx.ID(),
		OutputIndex: numDelStakeUTXOs + 2,
	}
	_, err = onCommitState.GetUTXO(preCortinaVdrRewardUTXOID.InputID())
	require.ErrorIs(err, database.ErrNotFound)

	// Commit Delegator Diff
	require.NoError(txExecutor.OnCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	tx, err = env.txBuilder.NewRewardValidatorTx(vdrStaker.TxID)
	require.NoError(err)

	// Create Validator Diff
	onCommitState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err = state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor = ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&txExecutor))

	require.NotEqual(vdrStaker.TxID, delStaker.TxID)

	numVdrStakeUTXOs := uint32(len(delTx.Unsigned.InputIDs()))

	// check for validator reward here
	vdrRewardUTXOID := &avax.UTXOID{
		TxID:        vdrTx.ID(),
		OutputIndex: numVdrStakeUTXOs + 1,
	}

	utxo, err = onCommitState.GetUTXO(vdrRewardUTXOID.InputID())
	require.NoError(err)
	require.IsType(&secp256k1fx.TransferOutput{}, utxo.Out)
	castUTXO = utxo.Out.(*secp256k1fx.TransferOutput)
	require.Equal(vdrRewardAmt, castUTXO.Amt, "expected validator to be rewarded")
	require.True(vdrDestSet.Equals(castUTXO.AddressesSet()), "expected reward UTXO to be issued to vdrDestSet")

	// check for validator's batched delegator rewards here
	onCommitVdrDelRewardUTXOID := &avax.UTXOID{
		TxID:        vdrTx.ID(),
		OutputIndex: numVdrStakeUTXOs + 2,
	}

	utxo, err = onCommitState.GetUTXO(onCommitVdrDelRewardUTXOID.InputID())
	require.NoError(err)
	require.IsType(&secp256k1fx.TransferOutput{}, utxo.Out)
	castUTXO = utxo.Out.(*secp256k1fx.TransferOutput)
	require.Equal(delRewardAmt/4, castUTXO.Amt, "expected validator to be rewarded with accrued delegator rewards")
	require.True(vdrDestSet.Equals(castUTXO.AddressesSet()), "expected reward UTXO to be issued to vdrDestSet")

	// aborted validator tx should still distribute accrued delegator rewards
	onAbortVdrDelRewardUTXOID := &avax.UTXOID{
		TxID:        vdrTx.ID(),
		OutputIndex: numVdrStakeUTXOs + 1,
	}

	utxo, err = onAbortState.GetUTXO(onAbortVdrDelRewardUTXOID.InputID())
	require.NoError(err)
	require.IsType(&secp256k1fx.TransferOutput{}, utxo.Out)
	castUTXO = utxo.Out.(*secp256k1fx.TransferOutput)
	require.Equal(delRewardAmt/4, castUTXO.Amt, "expected validator to be rewarded with accrued delegator rewards")
	require.True(vdrDestSet.Equals(castUTXO.AddressesSet()), "expected reward UTXO to be issued to vdrDestSet")

	_, err = onCommitState.GetUTXO(preCortinaVdrRewardUTXOID.InputID())
	require.ErrorIs(err, database.ErrNotFound)

	// Commit Validator Diff
	require.NoError(txExecutor.OnCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Since the tx was committed, the delegator and the delegatee should be rewarded.
	// The delegator reward should be higher since the delegatee's share is 25%.
	commitVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	vdrReward, err := math.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	delegateeReward, err := math.Sub(vdrReward, 2000000)
	require.NoError(err)
	require.NotZero(delegateeReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := math.Sub(commitDelBalance, oldDelBalance)
	require.NoError(err)
	require.NotZero(delReward, "expected delegator balance to increase because of reward")

	require.Less(delegateeReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	require.Equal(delRewardAmt, delReward+delegateeReward, "expected total reward to be %d but is %d", delRewardAmt, delReward+vdrReward)
}

func TestCortinaForkRewardDelegatorTxAndValidatorTxExecuteOnCommit(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(latestFork)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := uint64(defaultValidateStartTime.Unix()) + 1
	vdrEndTime := uint64(defaultValidateStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestNodeID()

	vdrTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake, // stakeAmt
		vdrStartTime,
		vdrEndTime,
		vdrNodeID,        // node ID
		vdrRewardAddress, // reward address
		reward.PercentDenominator/4,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime

	delTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		delStartTime,
		delEndTime,
		vdrNodeID,
		delRewardAddress,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		ids.ShortEmpty, // Change address
	)
	require.NoError(err)

	addValTx := vdrTx.Unsigned.(*txs.AddValidatorTx)
	vdrRewardAmt := uint64(2000000)
	vdrStaker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		addValTx,
		addValTx.StartTime(),
		addValTx.EndTime(),
		vdrRewardAmt,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delRewardAmt := uint64(1000000)
	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		addDelTx,
		time.Unix(int64(delStartTime), 0),
		time.Unix(int64(delEndTime), 0),
		delRewardAmt,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(vdrStaker)
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(vdrEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	vdrDestSet := set.Set[ids.ShortID]{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := set.Set[ids.ShortID]{}
	delDestSet.Add(delRewardAddress)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)

	tx, err := env.txBuilder.NewRewardValidatorTx(delTx.ID())
	require.NoError(err)

	// Create Delegator Diffs
	delOnCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	delOnAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor := ProposalTxExecutor{
		OnCommitState: delOnCommitState,
		OnAbortState:  delOnAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&txExecutor))

	// Create Validator Diffs
	testID := ids.GenerateTestID()
	env.SetState(testID, delOnCommitState)

	vdrOnCommitState, err := state.NewDiff(testID, env)
	require.NoError(err)

	vdrOnAbortState, err := state.NewDiff(testID, env)
	require.NoError(err)

	tx, err = env.txBuilder.NewRewardValidatorTx(vdrTx.ID())
	require.NoError(err)

	txExecutor = ProposalTxExecutor{
		OnCommitState: vdrOnCommitState,
		OnAbortState:  vdrOnAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&txExecutor))

	// aborted validator tx should still distribute accrued delegator rewards
	numVdrStakeUTXOs := uint32(len(delTx.Unsigned.InputIDs()))
	onAbortVdrDelRewardUTXOID := &avax.UTXOID{
		TxID:        vdrTx.ID(),
		OutputIndex: numVdrStakeUTXOs + 1,
	}

	utxo, err := vdrOnAbortState.GetUTXO(onAbortVdrDelRewardUTXOID.InputID())
	require.NoError(err)
	require.IsType(&secp256k1fx.TransferOutput{}, utxo.Out)
	castUTXO := utxo.Out.(*secp256k1fx.TransferOutput)
	require.Equal(delRewardAmt/4, castUTXO.Amt, "expected validator to be rewarded with accrued delegator rewards")
	require.True(vdrDestSet.Equals(castUTXO.AddressesSet()), "expected reward UTXO to be issued to vdrDestSet")

	// Commit Delegator Diff
	require.NoError(delOnCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Commit Validator Diff
	require.NoError(vdrOnCommitState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Since the tx was committed, the delegator and the delegatee should be rewarded.
	// The delegator reward should be higher since the delegatee's share is 25%.
	commitVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	vdrReward, err := math.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	delegateeReward, err := math.Sub(vdrReward, vdrRewardAmt)
	require.NoError(err)
	require.NotZero(delegateeReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := math.Sub(commitDelBalance, oldDelBalance)
	require.NoError(err)
	require.NotZero(delReward, "expected delegator balance to increase because of reward")

	require.Less(delegateeReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	require.Equal(delRewardAmt, delReward+delegateeReward, "expected total reward to be %d but is %d", delRewardAmt, delReward+vdrReward)
}

func TestCortinaForkRewardDelegatorTxExecuteOnAbort(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(latestFork)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	initialSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := uint64(defaultValidateStartTime.Unix()) + 1
	vdrEndTime := uint64(defaultValidateStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestNodeID()

	vdrTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake, // stakeAmt
		vdrStartTime,
		vdrEndTime,
		vdrNodeID,        // node ID
		vdrRewardAddress, // reward address
		reward.PercentDenominator/4,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime
	delTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		delStartTime,
		delEndTime,
		vdrNodeID,
		delRewardAddress,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	addValTx := vdrTx.Unsigned.(*txs.AddValidatorTx)
	vdrStaker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		addValTx,
		addValTx.StartTime(),
		addValTx.EndTime(),
		0,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		addDelTx,
		addDelTx.StartTime(),
		addDelTx.EndTime(),
		1000000,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(vdrStaker)
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	tx, err := env.txBuilder.NewRewardValidatorTx(delTx.ID())
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&txExecutor))

	vdrDestSet := set.Set[ids.ShortID]{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := set.Set[ids.ShortID]{}
	delDestSet.Add(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)

	require.NoError(txExecutor.OnAbortState.Apply(env.state))

	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// If tx is aborted, delegator and delegatee shouldn't get reward
	newVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	vdrReward, err := math.Sub(newVdrBalance, oldVdrBalance)
	require.NoError(err)
	require.Zero(vdrReward, "expected delegatee balance not to increase")

	newDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := math.Sub(newDelBalance, oldDelBalance)
	require.NoError(err)
	require.Zero(delReward, "expected delegator balance not to increase")

	newSupply, err := env.state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(initialSupply-expectedReward, newSupply, "should have removed un-rewarded tokens from the potential supply")
}

func TestBanffForkRewardDelegatorTxExecuteOnCommit(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(banffFork)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	dummyHeight := uint64(1)

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := uint64(defaultValidateStartTime.Unix()) + 1
	vdrEndTime := uint64(defaultValidateStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestNodeID()

	vdrTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake, // stakeAmt
		vdrStartTime,
		vdrEndTime,
		vdrNodeID,        // node ID
		vdrRewardAddress, // reward address
		reward.PercentDenominator/4,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime

	delTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		delStartTime,
		delEndTime,
		vdrNodeID,
		delRewardAddress,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		ids.ShortEmpty, // Change address
	)
	require.NoError(err)

	addValTx := vdrTx.Unsigned.(*txs.AddValidatorTx)
	vdrStaker, err := state.NewCurrentStaker(
		vdrTx.ID(),
		addValTx,
		addValTx.StartTime(),
		addValTx.EndTime(),
		0,
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	delStaker, err := state.NewCurrentStaker(
		delTx.ID(),
		addDelTx,
		addDelTx.StartTime(),
		addDelTx.EndTime(),
		1000000,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(vdrStaker)
	env.state.AddTx(vdrTx, status.Committed)
	env.state.PutCurrentDelegator(delStaker)
	env.state.AddTx(delTx, status.Committed)
	env.state.SetTimestamp(time.Unix(int64(delEndTime), 0))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// test validator stake
	vdrSet, ok := env.config.Validators.Get(constants.PrimaryNetworkID)
	require.True(ok)

	stake := vdrSet.GetWeight(vdrNodeID)
	require.Equal(env.config.MinValidatorStake+env.config.MinDelegatorStake, stake)

	tx, err := env.txBuilder.NewRewardValidatorTx(delTx.ID())
	require.NoError(err)

	onCommitState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	onAbortState, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	txExecutor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       &env.backend,
		Tx:            tx,
	}
	require.NoError(tx.Unsigned.Visit(&txExecutor))

	vdrDestSet := set.Set[ids.ShortID]{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := set.Set[ids.ShortID]{}
	delDestSet.Add(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	oldDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)

	require.NoError(txExecutor.OnCommitState.Apply(env.state))
	env.state.SetHeight(dummyHeight)
	require.NoError(env.state.Commit())

	// Since the tx was committed, the delegator and the delegatee should be rewarded.
	// The delegator reward should be higher since the delegatee's share is 25%.
	commitVdrBalance, err := avax.GetBalance(env.state, vdrDestSet)
	require.NoError(err)
	vdrReward, err := math.Sub(commitVdrBalance, oldVdrBalance)
	require.NoError(err)
	require.NotZero(vdrReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(env.state, delDestSet)
	require.NoError(err)
	delReward, err := math.Sub(commitDelBalance, oldDelBalance)
	require.NoError(err)
	require.NotZero(delReward, "expected delegator balance to increase because of reward")

	require.Less(vdrReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	require.Equal(expectedReward, delReward+vdrReward, "expected total reward to be %d but is %d", expectedReward, delReward+vdrReward)

	require.Equal(env.config.MinValidatorStake, vdrSet.GetWeight(vdrNodeID))
}
