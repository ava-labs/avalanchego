// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestRewardsChecksRewardValidator(t *testing.T) {
	properties := gopter.NewProperties(nil)

	// to reproduce a given scenario do something like this:
	// parameters := gopter.DefaultTestParametersWithSeed(1685887576153675816)
	// properties := gopter.NewProperties(parameters)

	var (
		nodeID            = ids.GenerateTestNodeID()
		validatorDuration = defaultMinStakingDuration
		dummyStartTime    = time.Unix(0, 0)

		validatorAuthKey = preFundedKeys[4]
		AuthOwner        = validatorAuthKey.PublicKey().Address()

		stakeKey   = preFundedKeys[3] // stake
		stakeOwner = stakeKey.PublicKey().Address()

		stopKey   = preFundedKeys[2]
		stopOwner = stopKey.PublicKey().Address()
	)

	var testKeyfactory secp256k1.Factory
	rewardsKey, err := testKeyfactory.NewPrivateKey()
	require.NoError(t, err)

	rewardOwner := rewardsKey.PublicKey().Address() // rewards
	rewardOwners := set.NewSet[ids.ShortID](1)
	rewardOwners.Add(rewardOwner)

	stakeOwners := set.NewSet[ids.ShortID](1)
	stakeOwners.Add(stakeOwner)

	blsSK, err := bls.NewSecretKey()
	require.NoError(t, err)
	blsPOP := signer.NewProofOfPossession(blsSK)

	properties.Property("validator is rewarded or not in commits and aborts", prop.ForAll(
		func(choices []bool) string {
			env := newEnvironmentNoValidator(latestFork)
			defer func() {
				_ = shutdownEnvironment(env)
			}()

			var (
				validatorWeight = env.config.MinValidatorStake
				validatorFee    = env.config.AddPrimaryNetworkValidatorFee
			)

			preCreationStakeBalance, err := avax.GetBalance(env.state, stakeOwners)
			if err != nil {
				return err.Error()
			}

			utxosHandler := utxo.NewHandler(env.ctx, env.clk, env.fx)
			ins, unstakedOuts, stakedOuts, signers, err := utxosHandler.Spend(
				env.state,
				[]*secp256k1.PrivateKey{stakeKey},
				validatorWeight,
				validatorFee,
				stakeOwner, // changeAddr
			)
			if err != nil {
				return err.Error()
			}

			// Create the continuous validator
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
					Wght:   validatorWeight,
				},
				Signer: blsPOP,
				ValidatorAuthKey: &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{AuthOwner},
				},
				StakeOuts: stakedOuts,
				ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
					Addrs:     rewardOwners.List(),
					Threshold: 1,
				},
				ValidatorRewardRestakeShares: 0,
				DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
					Addrs:     rewardOwners.List(),
					Threshold: 1,
				},
				DelegationShares: 20_000,
			}
			addContinuousValTx, err := txs.NewSigned(continuousValidatorTx, txs.Codec, signers)
			if err != nil {
				return err.Error()
			}
			if err := addContinuousValTx.SyntacticVerify(env.ctx); err != nil {
				return err.Error()
			}

			onParentState, err := state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			addValExecutor := StandardTxExecutor{
				State:   onParentState,
				Backend: &env.backend,
				Tx:      addContinuousValTx,
			}
			if err := addContinuousValTx.Unsigned.Visit(&addValExecutor); err != nil {
				return err.Error()
			}

			onParentState.AddTx(addContinuousValTx, status.Committed)
			if err := onParentState.Apply(env.state); err != nil {
				return err.Error()
			}
			if err := env.state.Commit(); err != nil {
				return err.Error()
			}

			postCreationStakeBalance, err := avax.GetBalance(env.state, stakeOwners)
			if err != nil {
				return err.Error()
			}
			if postCreationStakeBalance != preCreationStakeBalance-validatorWeight-validatorFee {
				return "unexpected postCreationStakeBalance"
			}

			// shift validator ahead a few times
			for _, pickCommit := range choices {
				continuousValidator, err := env.state.GetCurrentValidator(
					continuousValidatorTx.SubnetID(),
					continuousValidatorTx.NodeID(),
				)
				if err != nil {
					return err.Error()
				}

				preShiftStakeBalance, err := avax.GetBalance(env.state, stakeOwners)
				if err != nil {
					return err.Error()
				}

				preShiftRewardBalance, err := avax.GetBalance(env.state, rewardOwners)
				if err != nil {
					return err.Error()
				}

				// advance time
				chainTime := env.state.GetTimestamp()
				nextChainTime := chainTime.Add(validatorDuration)
				env.state.SetTimestamp(nextChainTime)

				// create and execute reward tx
				tx, err := env.txBuilder.NewRewardValidatorTx(continuousValidator.TxID)
				if err != nil {
					return err.Error()
				}

				onCommitState, err := state.NewDiff(lastAcceptedID, env)
				if err != nil {
					return err.Error()
				}

				onAbortState, err := state.NewDiff(lastAcceptedID, env)
				if err != nil {
					return err.Error()
				}

				txExecutor := ProposalTxExecutor{
					OnCommitState: onCommitState,
					OnAbortState:  onAbortState,
					Backend:       &env.backend,
					Tx:            tx,
				}
				if err := tx.Unsigned.Visit(&txExecutor); err != nil {
					return err.Error()
				}

				if pickCommit {
					if err := txExecutor.OnCommitState.Apply(env.state); err != nil {
						return err.Error()
					}
				} else {
					if err := txExecutor.OnAbortState.Apply(env.state); err != nil {
						return err.Error()
					}
				}
				if err := env.state.Commit(); err != nil {
					return err.Error()
				}

				// check stake is NOT given back (balance on addresses) while shifting
				postShiftStakeBalance, err := avax.GetBalance(env.state, stakeOwners)
				if err != nil {
					return err.Error()
				}
				if postShiftStakeBalance != preShiftStakeBalance {
					return "unexpected preShiftStakeBalance"
				}

				// check reward is fully (for now) given back while shifting
				postShiftRewardBalance, err := avax.GetBalance(env.state, rewardOwners)
				if err != nil {
					return err.Error()
				}

				if pickCommit {
					if postShiftRewardBalance != preShiftRewardBalance+continuousValidator.PotentialReward {
						return "unexpected preShiftRewardBalance on commit"
					}
				} else {
					if postShiftRewardBalance != preShiftRewardBalance {
						return "unexpected preShiftRewardBalance on abort"
					}
				}
			}

			// stop the validator
			preStopStakeBalance, err := avax.GetBalance(env.state, stakeOwners)
			if err != nil {
				return err.Error()
			}

			preStopRewardBalance, err := avax.GetBalance(env.state, rewardOwners)
			if err != nil {
				return err.Error()
			}

			stopValidatorTx, err := env.txBuilder.NewStopStakerTx(
				addContinuousValTx.ID(),
				[]*secp256k1.PrivateKey{validatorAuthKey},
				stopOwner,
			)
			if err != nil {
				return err.Error()
			}

			diff, err := state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			txExecutor := StandardTxExecutor{
				State:   diff,
				Backend: &env.backend,
				Tx:      stopValidatorTx,
			}
			if err := stopValidatorTx.Unsigned.Visit(&txExecutor); err != nil {
				return err.Error()
			}
			if err := txExecutor.State.Apply(env.state); err != nil {
				return err.Error()
			}
			if err := env.state.Commit(); err != nil {
				return err.Error()
			}

			stoppedValidator, err := env.state.GetCurrentValidator(
				continuousValidatorTx.SubnetID(),
				continuousValidatorTx.NodeID(),
			)
			if err != nil {
				return err.Error()
			}

			// advance time to drop the validator
			chainTime := env.state.GetTimestamp()
			nextChainTime := chainTime.Add(validatorDuration)
			env.state.SetTimestamp(nextChainTime)

			// create and execute reward tx
			tx, err := env.txBuilder.NewRewardValidatorTx(stoppedValidator.TxID)
			if err != nil {
				return err.Error()
			}

			onCommitState, err := state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			onAbortState, err := state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			rewardTxExecutor := ProposalTxExecutor{
				OnCommitState: onCommitState,
				OnAbortState:  onAbortState,
				Backend:       &env.backend,
				Tx:            tx,
			}
			if err := tx.Unsigned.Visit(&rewardTxExecutor); err != nil {
				return err.Error()
			}
			if err := rewardTxExecutor.OnCommitState.Apply(env.state); err != nil {
				return err.Error()
			}
			if err := env.state.Commit(); err != nil {
				return err.Error()
			}

			// check that stake is returned
			postStopStakeBalance, err := avax.GetBalance(env.state, stakeOwners)
			if err != nil {
				return err.Error()
			}
			if postStopStakeBalance != preStopStakeBalance+env.config.MinValidatorStake {
				return "unexpected postStopStakeBalance"
			}

			// check that last reward is paid
			postStopRewardBalance, err := avax.GetBalance(env.state, rewardOwners)
			if err != nil {
				return err.Error()
			}
			if postStopRewardBalance != preStopRewardBalance+stoppedValidator.PotentialReward {
				return "unexpected postStopRewardBalance"
			}
			return ""
		},
		gen.SliceOfN(10, gen.Bool()),
	))

	properties.TestingRun(t)
}

func TestShiftChecksRewardValidator(t *testing.T) {
	properties := gopter.NewProperties(nil)

	// to reproduce a given scenario do something like this:
	// parameters := gopter.DefaultTestParametersWithSeed(1685887576153675816)
	// properties := gopter.NewProperties(parameters)

	var (
		nodeID            = ids.GenerateTestNodeID()
		validatorDuration = defaultMinStakingDuration
		dummyStartTime    = time.Unix(0, 0)

		validatorAuthKey = preFundedKeys[4]
		addr             = validatorAuthKey.PublicKey().Address()
	)

	blsSK, err := bls.NewSecretKey()
	require.NoError(t, err)
	blsPOP := signer.NewProofOfPossession(blsSK)

	properties.Property("validator is shift in both commits and aborts", prop.ForAll(
		func(choices []bool) string {
			env := newEnvironmentNoValidator(latestFork)
			defer func() {
				_ = shutdownEnvironment(env)
			}()

			// Add a continuous validator
			onParentState, err := state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			utxosHandler := utxo.NewHandler(env.ctx, env.clk, env.fx)
			ins, unstakedOuts, stakedOuts, signers, err := utxosHandler.Spend(
				env.state,
				preFundedKeys,
				env.config.MinValidatorStake, // stakeAmount
				env.config.AddPrimaryNetworkValidatorFee,
				addr, // changeAddr
			)
			if err != nil {
				return err.Error()
			}

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
			if err != nil {
				return err.Error()
			}
			if err := addContinuousValTx.SyntacticVerify(env.ctx); err != nil {
				return err.Error()
			}

			addValExecutor := StandardTxExecutor{
				State:   onParentState,
				Backend: &env.backend,
				Tx:      addContinuousValTx,
			}
			if err := addContinuousValTx.Unsigned.Visit(&addValExecutor); err != nil {
				return err.Error()
			}

			onParentState.AddTx(addContinuousValTx, status.Committed)
			if err := onParentState.Apply(env.state); err != nil {
				return err.Error()
			}
			if err := env.state.Commit(); err != nil {
				return err.Error()
			}

			// shift validator ahead a few times
			for _, pickCommit := range choices {
				continuousValidator, err := env.state.GetCurrentValidator(
					continuousValidatorTx.SubnetID(),
					continuousValidatorTx.NodeID(),
				)
				if err != nil {
					return err.Error()
				}

				// advance time
				chainTime := env.state.GetTimestamp()
				nextChainTime := chainTime.Add(validatorDuration)
				env.state.SetTimestamp(nextChainTime)

				// create and execute reward tx
				tx, err := env.txBuilder.NewRewardValidatorTx(continuousValidator.TxID)
				if err != nil {
					return err.Error()
				}

				onCommitState, err := state.NewDiff(lastAcceptedID, env)
				if err != nil {
					return err.Error()
				}

				onAbortState, err := state.NewDiff(lastAcceptedID, env)
				if err != nil {
					return err.Error()
				}

				txExecutor := ProposalTxExecutor{
					OnCommitState: onCommitState,
					OnAbortState:  onAbortState,
					Backend:       &env.backend,
					Tx:            tx,
				}
				if err := tx.Unsigned.Visit(&txExecutor); err != nil {
					return err.Error()
				}

				if pickCommit {
					err = txExecutor.OnCommitState.Apply(env.state)
				} else {
					err = txExecutor.OnAbortState.Apply(env.state)
				}
				if err != nil {
					return err.Error()
				}
				if err := env.state.Commit(); err != nil {
					return err.Error()
				}

				// check that post continuousStakingFork, staker is shifted ahead by its staking period
				shiftedValidator, err := env.state.GetCurrentValidator(
					continuousValidatorTx.SubnetID(),
					continuousValidatorTx.NodeID(),
				)
				if err != nil {
					return err.Error()
				}

				if continuousValidator.StakingPeriod != shiftedValidator.StakingPeriod {
					return "unexpected staking period for shifted validator"
				}
				if !continuousValidator.StartTime.Add(continuousValidator.StakingPeriod).Equal(shiftedValidator.StartTime) {
					return "unexpected shifted validator start time"
				}
				if !continuousValidator.NextTime.Add(continuousValidator.StakingPeriod).Equal(shiftedValidator.NextTime) {
					return "unexpected shifted validator next time"
				}
				if !shiftedValidator.EndTime.Equal(mockable.MaxTime) {
					return "unexpected shifted validator end time"
				}
			}

			continuousValidator, err := env.state.GetCurrentValidator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
			if err != nil {
				return err.Error()
			}

			// stop the validator
			stopValidatorTx, err := env.txBuilder.NewStopStakerTx(
				addContinuousValTx.ID(),
				[]*secp256k1.PrivateKey{validatorAuthKey},
				addr,
			)
			if err != nil {
				return err.Error()
			}

			diff, err := state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			txExecutor := StandardTxExecutor{
				State:   diff,
				Backend: &env.backend,
				Tx:      stopValidatorTx,
			}
			if err := stopValidatorTx.Unsigned.Visit(&txExecutor); err != nil {
				return err.Error()
			}
			if err := txExecutor.State.Apply(env.state); err != nil {
				return err.Error()
			}
			if err := env.state.Commit(); err != nil {
				return err.Error()
			}

			// check that validator is stopped
			stoppedValidator, err := env.state.GetCurrentValidator(
				continuousValidatorTx.SubnetID(),
				continuousValidatorTx.NodeID(),
			)
			if err != nil {
				return err.Error()
			}
			if continuousValidator.StakingPeriod != stoppedValidator.StakingPeriod {
				return "unexpected staking period for stopped validator"
			}
			if !continuousValidator.StartTime.Equal(stoppedValidator.StartTime) {
				return "unexpected stopped validator start time"
			}
			if !continuousValidator.NextTime.Equal(stoppedValidator.NextTime) {
				return "unexpected stopped validator next time"
			}
			if !stoppedValidator.NextTime.Equal(stoppedValidator.EndTime) {
				return "unexpected stopped validator end time"
			}

			return ""
		},
		gen.SliceOfN(10, gen.Bool()),
	))

	properties.TestingRun(t)
}

func TestRewardsChecksRewardDelegator(t *testing.T) {
	properties := gopter.NewProperties(nil)

	// to reproduce a given scenario do something like this:
	// parameters := gopter.DefaultTestParametersWithSeed(1685887576153675816)
	// properties := gopter.NewProperties(parameters)

	var (
		nodeID             = ids.GenerateTestNodeID()
		validationDuration = defaultMaxStakingDuration
		delegationDuration = defaultMinStakingDuration
		dummyStartTime     = time.Unix(0, 0)

		delegatorAuthKey = preFundedKeys[4]
		AuthOwner        = delegatorAuthKey.PublicKey().Address()

		stakeKey   = preFundedKeys[3] // stake
		stakeOwner = stakeKey.PublicKey().Address()

		stopKey   = preFundedKeys[2]
		stopOwner = stopKey.PublicKey().Address()

		validatorKey   = preFundedKeys[1]
		validatorOwner = validatorKey.PublicKey().Address()
	)

	var testKeyfactory secp256k1.Factory
	rewardsKey, err := testKeyfactory.NewPrivateKey()
	require.NoError(t, err)

	rewardOwner := rewardsKey.PublicKey().Address() // rewards
	rewardOwners := set.NewSet[ids.ShortID](1)
	rewardOwners.Add(rewardOwner)

	stakeOwners := set.NewSet[ids.ShortID](1)
	stakeOwners.Add(stakeOwner)

	blsSK, err := bls.NewSecretKey()
	require.NoError(t, err)
	blsPOP := signer.NewProofOfPossession(blsSK)

	properties.Property("delegator is rewarded or not in commits and aborts", prop.ForAll(
		func(choices []bool) string {
			env := newEnvironmentNoValidator(latestFork)
			defer func() {
				_ = shutdownEnvironment(env)
			}()

			// Add a continuous validator
			onParentState, err := state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			utxosHandler := utxo.NewHandler(env.ctx, env.clk, env.fx)
			ins, unstakedOuts, stakedOuts, signers, err := utxosHandler.Spend(
				env.state,
				[]*secp256k1.PrivateKey{validatorKey},
				env.config.MinValidatorStake,
				env.config.AddPrimaryNetworkValidatorFee,
				validatorOwner,
			)
			if err != nil {
				return err.Error()
			}

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
					End:    uint64(dummyStartTime.Add(validationDuration).Unix()),
					Wght:   env.config.MinValidatorStake,
				},
				Signer: blsPOP,
				ValidatorAuthKey: &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{validatorOwner},
				},
				StakeOuts: stakedOuts,
				ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
					Addrs:     []ids.ShortID{validatorOwner},
					Threshold: 1,
				},
				ValidatorRewardRestakeShares: 0,
				DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
					Addrs:     []ids.ShortID{validatorOwner},
					Threshold: 1,
				},
				DelegationShares: 20_000,
			}
			addContinuousValTx, err := txs.NewSigned(continuousValidatorTx, txs.Codec, signers)
			if err != nil {
				return err.Error()
			}
			if err := addContinuousValTx.SyntacticVerify(env.ctx); err != nil {
				return err.Error()
			}

			addValExecutor := StandardTxExecutor{
				State:   onParentState,
				Backend: &env.backend,
				Tx:      addContinuousValTx,
			}
			if err := addContinuousValTx.Unsigned.Visit(&addValExecutor); err != nil {
				return err.Error()
			}
			onParentState.AddTx(addContinuousValTx, status.Committed)
			if err := onParentState.Apply(env.state); err != nil {
				return err.Error()
			}
			if err := env.state.Commit(); err != nil {
				return err.Error()
			}

			// Create the delegator tx
			var (
				delegatorWeight = env.config.MinDelegatorStake
				delegatorFee    = env.config.AddPrimaryNetworkDelegatorFee
			)

			preCreationStakeBalance, err := avax.GetBalance(env.state, stakeOwners)
			if err != nil {
				return err.Error()
			}

			utxosHandler = utxo.NewHandler(env.ctx, env.clk, env.fx)
			ins, unstakedOuts, stakedOuts, signers, err = utxosHandler.Spend(
				env.state,
				[]*secp256k1.PrivateKey{stakeKey},
				delegatorWeight,
				delegatorFee,
				stakeOwner, // changeAddr
			)
			if err != nil {
				return err.Error()
			}

			// Create the continuous validator
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
					End:    uint64(dummyStartTime.Add(delegationDuration).Unix()),
					Wght:   delegatorWeight,
				},
				DelegatorAuthKey: &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{AuthOwner},
				},
				StakeOuts: stakedOuts,
				DelegationRewardsOwner: &secp256k1fx.OutputOwners{
					Addrs:     []ids.ShortID{rewardOwner},
					Threshold: 1,
				},
				DelegatorRewardRestakeShares: 0,
			}
			addContinuousDelTx, err := txs.NewSigned(continuousDelegatorTx, txs.Codec, signers)
			if err != nil {
				return err.Error()
			}
			if err := addContinuousDelTx.SyntacticVerify(env.ctx); err != nil {
				return err.Error()
			}

			onParentState, err = state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			addDelExecutor := StandardTxExecutor{
				State:   onParentState,
				Backend: &env.backend,
				Tx:      addContinuousDelTx,
			}
			if err := addContinuousDelTx.Unsigned.Visit(&addDelExecutor); err != nil {
				return err.Error()
			}

			onParentState.AddTx(addContinuousDelTx, status.Committed)
			if err := onParentState.Apply(env.state); err != nil {
				return err.Error()
			}
			if err := env.state.Commit(); err != nil {
				return err.Error()
			}

			postCreationStakeBalance, err := avax.GetBalance(env.state, stakeOwners)
			if err != nil {
				return err.Error()
			}
			if postCreationStakeBalance != preCreationStakeBalance-delegatorWeight-delegatorFee {
				return "unexpected postCreationStakeBalance"
			}

			// shift validator ahead a few times
			for _, pickCommit := range choices {
				delIt, err := env.state.GetCurrentDelegatorIterator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
				if err != nil {
					return err.Error()
				}
				if !delIt.Next() {
					return "missing continuous delegator pre shifting"
				}
				continuousDelegator := delIt.Value()
				delIt.Release()

				preShiftStakeBalance, err := avax.GetBalance(env.state, stakeOwners)
				if err != nil {
					return err.Error()
				}

				preShiftRewardBalance, err := avax.GetBalance(env.state, rewardOwners)
				if err != nil {
					return err.Error()
				}

				// advance time
				chainTime := env.state.GetTimestamp()
				nextChainTime := chainTime.Add(delegationDuration)
				env.state.SetTimestamp(nextChainTime)

				// create and execute reward tx
				tx, err := env.txBuilder.NewRewardValidatorTx(continuousDelegator.TxID)
				if err != nil {
					return err.Error()
				}

				onCommitState, err := state.NewDiff(lastAcceptedID, env)
				if err != nil {
					return err.Error()
				}

				onAbortState, err := state.NewDiff(lastAcceptedID, env)
				if err != nil {
					return err.Error()
				}

				txExecutor := ProposalTxExecutor{
					OnCommitState: onCommitState,
					OnAbortState:  onAbortState,
					Backend:       &env.backend,
					Tx:            tx,
				}
				if err := tx.Unsigned.Visit(&txExecutor); err != nil {
					return err.Error()
				}

				if pickCommit {
					if err := txExecutor.OnCommitState.Apply(env.state); err != nil {
						return err.Error()
					}
				} else {
					if err := txExecutor.OnAbortState.Apply(env.state); err != nil {
						return err.Error()
					}
				}
				if err := env.state.Commit(); err != nil {
					return err.Error()
				}

				// check stake is NOT given back (balance on addresses) while shifting
				postShiftStakeBalance, err := avax.GetBalance(env.state, stakeOwners)
				if err != nil {
					return err.Error()
				}
				if postShiftStakeBalance != preShiftStakeBalance {
					return "unexpected preShiftStakeBalance"
				}

				// check reward is fully (for now) given back while shifting
				postShiftRewardBalance, err := avax.GetBalance(env.state, rewardOwners)
				if err != nil {
					return err.Error()
				}

				delegatorReward := calculateDelegatorReward(continuousDelegator, continuousValidatorTx.Shares())
				if pickCommit {
					if postShiftRewardBalance != preShiftRewardBalance+delegatorReward {
						return "unexpected preShiftRewardBalance on commit"
					}
				} else {
					if postShiftRewardBalance != preShiftRewardBalance {
						return "unexpected preShiftRewardBalance on abort"
					}
				}
			}

			// stop the validator
			preStopStakeBalance, err := avax.GetBalance(env.state, stakeOwners)
			if err != nil {
				return err.Error()
			}

			preStopRewardBalance, err := avax.GetBalance(env.state, rewardOwners)
			if err != nil {
				return err.Error()
			}

			stopDelegatorTx, err := env.txBuilder.NewStopStakerTx(
				addContinuousDelTx.ID(),
				[]*secp256k1.PrivateKey{delegatorAuthKey},
				stopOwner,
			)
			if err != nil {
				return err.Error()
			}

			diff, err := state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			txExecutor := StandardTxExecutor{
				State:   diff,
				Backend: &env.backend,
				Tx:      stopDelegatorTx,
			}
			if err := stopDelegatorTx.Unsigned.Visit(&txExecutor); err != nil {
				return err.Error()
			}
			if err := txExecutor.State.Apply(env.state); err != nil {
				return err.Error()
			}
			if err := env.state.Commit(); err != nil {
				return err.Error()
			}

			delIt, err := env.state.GetCurrentDelegatorIterator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
			if err != nil {
				return err.Error()
			}
			if !delIt.Next() {
				return "missing continuous delegator post stop"
			}
			stoppedDelegator := delIt.Value()
			delIt.Release()

			// advance time to drop the validator
			chainTime := env.state.GetTimestamp()
			nextChainTime := chainTime.Add(delegationDuration)
			env.state.SetTimestamp(nextChainTime)

			// create and execute reward tx
			tx, err := env.txBuilder.NewRewardValidatorTx(stoppedDelegator.TxID)
			if err != nil {
				return err.Error()
			}

			onCommitState, err := state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			onAbortState, err := state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			rewardTxExecutor := ProposalTxExecutor{
				OnCommitState: onCommitState,
				OnAbortState:  onAbortState,
				Backend:       &env.backend,
				Tx:            tx,
			}
			if err := tx.Unsigned.Visit(&rewardTxExecutor); err != nil {
				return err.Error()
			}
			if err := rewardTxExecutor.OnCommitState.Apply(env.state); err != nil {
				return err.Error()
			}
			if err := env.state.Commit(); err != nil {
				return err.Error()
			}

			// check that stake is returned
			postStopStakeBalance, err := avax.GetBalance(env.state, stakeOwners)
			if err != nil {
				return err.Error()
			}
			if postStopStakeBalance != preStopStakeBalance+env.config.MinDelegatorStake {
				return "unexpected postStopStakeBalance"
			}

			// check that last reward is paid
			postStopRewardBalance, err := avax.GetBalance(env.state, rewardOwners)
			if err != nil {
				return err.Error()
			}

			delegatorReward := calculateDelegatorReward(stoppedDelegator, continuousValidatorTx.Shares())
			if postStopRewardBalance != preStopRewardBalance+delegatorReward {
				return "unexpected postStopRewardBalance"
			}
			return ""
		},
		gen.SliceOfN(1, gen.Bool()),
	))

	properties.TestingRun(t)
}

func TestShiftChecksRewardDelegator(t *testing.T) {
	properties := gopter.NewProperties(nil)

	// to reproduce a given scenario do something like this:
	// parameters := gopter.DefaultTestParametersWithSeed(1685887576153675816)
	// properties := gopter.NewProperties(parameters)

	var (
		nodeID            = ids.GenerateTestNodeID()
		validatorDuration = defaultMaxStakingDuration
		delegatorDuration = defaultMinStakingDuration
		dummyStartTime    = time.Unix(0, 0)

		stakerAuthKey = preFundedKeys[4]
		addr          = stakerAuthKey.PublicKey().Address()
	)

	blsSK, err := bls.NewSecretKey()
	require.NoError(t, err)
	blsPOP := signer.NewProofOfPossession(blsSK)

	properties.Property("delegator is shift in both commits and aborts", prop.ForAll(
		func(choices []bool) string {
			env := newEnvironmentNoValidator(latestFork)
			defer func() {
				_ = shutdownEnvironment(env)
			}()

			// Add a continuous validator
			onParentState, err := state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			utxosHandler := utxo.NewHandler(env.ctx, env.clk, env.fx)
			ins, unstakedOuts, stakedOuts, signers, err := utxosHandler.Spend(
				env.state,
				preFundedKeys,
				env.config.MinValidatorStake, // stakeAmount
				env.config.AddPrimaryNetworkValidatorFee,
				addr, // changeAddr
			)
			if err != nil {
				return err.Error()
			}

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
			if err != nil {
				return err.Error()
			}
			if err := addContinuousValTx.SyntacticVerify(env.ctx); err != nil {
				return err.Error()
			}

			addValExecutor := StandardTxExecutor{
				State:   onParentState,
				Backend: &env.backend,
				Tx:      addContinuousValTx,
			}
			if err := addContinuousValTx.Unsigned.Visit(&addValExecutor); err != nil {
				return err.Error()
			}
			onParentState.AddTx(addContinuousValTx, status.Committed)
			if err := onParentState.Apply(env.state); err != nil {
				return err.Error()
			}
			if err := env.state.Commit(); err != nil {
				return err.Error()
			}

			// Create the delegator tx
			onParentState, err = state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			ins, unstakedOuts, stakedOuts, signers, err = utxosHandler.Spend(
				env.state,
				preFundedKeys,
				env.config.MinDelegatorStake, // stakeAmount
				env.config.AddPrimaryNetworkDelegatorFee,
				addr, // changeAddr
			)
			if err != nil {
				return err.Error()
			}

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
			if err != nil {
				return err.Error()
			}
			if err := addContinuousDelTx.SyntacticVerify(env.ctx); err != nil {
				return err.Error()
			}

			addDelExecutor := StandardTxExecutor{
				State:   onParentState,
				Backend: &env.backend,
				Tx:      addContinuousDelTx,
			}
			if err := addContinuousDelTx.Unsigned.Visit(&addDelExecutor); err != nil {
				return err.Error()
			}
			onParentState.AddTx(addContinuousDelTx, status.Committed)
			if err := onParentState.Apply(env.state); err != nil {
				return err.Error()
			}
			if err := env.state.Commit(); err != nil {
				return err.Error()
			}

			// shift validator ahead a few times
			for _, pickCommit := range choices {
				delIt, err := env.state.GetCurrentDelegatorIterator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
				if err != nil {
					return err.Error()
				}
				if !delIt.Next() {
					return "missing delegator"
				}
				continuousDelegator := delIt.Value()
				delIt.Release()

				// advance time
				chainTime := env.state.GetTimestamp()
				nextChainTime := chainTime.Add(delegatorDuration)
				env.state.SetTimestamp(nextChainTime)

				// create and execute reward tx
				tx, err := env.txBuilder.NewRewardValidatorTx(continuousDelegator.TxID)
				if err != nil {
					return err.Error()
				}

				onCommitState, err := state.NewDiff(lastAcceptedID, env)
				if err != nil {
					return err.Error()
				}

				onAbortState, err := state.NewDiff(lastAcceptedID, env)
				if err != nil {
					return err.Error()
				}

				txExecutor := ProposalTxExecutor{
					OnCommitState: onCommitState,
					OnAbortState:  onAbortState,
					Backend:       &env.backend,
					Tx:            tx,
				}
				if err := tx.Unsigned.Visit(&txExecutor); err != nil {
					return err.Error()
				}
				if pickCommit {
					err = txExecutor.OnCommitState.Apply(env.state)
				} else {
					err = txExecutor.OnAbortState.Apply(env.state)
				}
				if err != nil {
					return err.Error()
				}
				if err := env.state.Commit(); err != nil {
					return err.Error()
				}

				stakersIt, err := env.state.GetCurrentStakerIterator()
				if err != nil {
					return err.Error()
				}

				// check that post continuousStakingFork, staker is shifted ahead by its staking period
				var shiftedDelegator *state.Staker
				for stakersIt.Next() {
					nextStaker := stakersIt.Value()
					if nextStaker.TxID == continuousDelegator.TxID {
						shiftedDelegator = nextStaker
						break
					}
				}
				stakersIt.Release()
				if shiftedDelegator == nil {
					return "nil shifted delegator"
				}
				if continuousDelegator.StakingPeriod != shiftedDelegator.StakingPeriod {
					return "unexpected staking period for shifted delegator"
				}
				if !continuousDelegator.StartTime.Add(continuousDelegator.StakingPeriod).Equal(shiftedDelegator.StartTime) {
					return "unexpected shifted delegator start time"
				}
				if !continuousDelegator.NextTime.Add(continuousDelegator.StakingPeriod).Equal(shiftedDelegator.NextTime) {
					return "unexpected shifted delegator next time"
				}
				if !shiftedDelegator.EndTime.Equal(mockable.MaxTime) {
					return "unexpected shifted delegator end time"
				}
			}

			delIt, err := env.state.GetCurrentDelegatorIterator(continuousValidatorTx.SubnetID(), continuousValidatorTx.NodeID())
			if err != nil {
				return err.Error()
			}
			if !delIt.Next() {
				return "missing delegator"
			}
			continuousDelegator := delIt.Value()
			delIt.Release()

			// stop the delegator
			stopDelegatorTx, err := env.txBuilder.NewStopStakerTx(
				addContinuousDelTx.ID(),
				[]*secp256k1.PrivateKey{stakerAuthKey},
				addr,
			)
			if err != nil {
				return err.Error()
			}

			diff, err := state.NewDiff(lastAcceptedID, env)
			if err != nil {
				return err.Error()
			}

			txExecutor := StandardTxExecutor{
				State:   diff,
				Backend: &env.backend,
				Tx:      stopDelegatorTx,
			}
			if err := stopDelegatorTx.Unsigned.Visit(&txExecutor); err != nil {
				return err.Error()
			}
			if err := txExecutor.State.Apply(env.state); err != nil {
				return err.Error()
			}
			if err := env.state.Commit(); err != nil {
				return err.Error()
			}

			// check that staker is stopped
			stakersIt, err := env.state.GetCurrentStakerIterator()
			if err != nil {
				return err.Error()
			}

			var stoppedDelegator *state.Staker
			for stakersIt.Next() {
				nextStaker := stakersIt.Value()
				if nextStaker.TxID == continuousDelegator.TxID {
					stoppedDelegator = nextStaker
					break
				}
			}
			stakersIt.Release()
			if stoppedDelegator == nil {
				return "nil shifted delegator"
			}
			if continuousDelegator.StakingPeriod != stoppedDelegator.StakingPeriod {
				return "unexpected staking period for stopped delegator"
			}
			if !continuousDelegator.StartTime.Equal(stoppedDelegator.StartTime) {
				return "unexpected stopped delegator start time"
			}
			if !continuousDelegator.NextTime.Equal(stoppedDelegator.NextTime) {
				return "unexpected stopped delegator next time"
			}
			if !stoppedDelegator.NextTime.Equal(stoppedDelegator.EndTime) {
				return "unexpected stopped delegator end time"
			}
			return ""
		},
		gen.SliceOfN(int(validatorDuration/delegatorDuration)-1, gen.Bool()),
	))

	properties.TestingRun(t)
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
