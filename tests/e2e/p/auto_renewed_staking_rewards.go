// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

var _ = e2e.DescribePChain("[Auto-Renewed Validators] [Staking Rewards]", func() {
	tc := e2e.NewTestContext()

	ginkgo.It("should add an auto-renewed validator and complete a staking cycle", func() {
		const (
			// Validator's data
			validatorWeight                 = 2_000 * units.Avax
			delegationShares                = uint32(reward.PercentDenominator * 0.10) // 10%
			autoCompoundRewardShares        = uint32(reward.PercentDenominator * 0.40) // 40%
			updatedAutoCompoundRewardShares = uint32(reward.PercentDenominator * 0.80) // 80%
			stakingPeriod                   = 20 * time.Second
			updatedStakingPeriod            = 15 * time.Second
			delegationPeriod                = stakingPeriod / 2 // delegator stakes for half the validator's period

			// Delegators' weights
			delegator1Weight = 1_000 * units.Avax
			delegator2Weight = 500 * units.Avax

			gasAmount = 10 * units.Avax
		)

		env := e2e.GetEnv(tc)

		requireHeliconActivated(tc, info.NewClient(env.GetRandomNodeURI().URI))

		f := newAutoRenewedValidatorFixture(tc, env, validatorWeight+gasAmount)

		pvmClient := platformvm.NewClient(f.randomWalletNodeURI.URI)
		upgrades, err := info.NewClient(f.randomWalletNodeURI.URI).Upgrades(tc.DefaultContext())
		require.NoError(tc, err)
		rewardsCalculator := reward.NewPrimaryNetworkCalculator(
			GetRewardConfig(f.tc, admin.NewClient(f.randomWalletNodeURI.URI)),
			*upgrades,
		)

		var (
			delegator1RewardKey  = e2e.NewPrivateKey(tc)
			delegator1FundingKey = e2e.NewPrivateKey(tc)
			delegator2RewardKey  = e2e.NewPrivateKey(tc)
			delegator2FundingKey = e2e.NewPrivateKey(tc)
		)

		var (
			validatorTxID                       ids.ID
			validatorFirstCyclePotentialRewards uint64
		)
		tc.By("adding the node as an auto-renewed validator and checking the supply mint", func() {
			validatorTxID, validatorFirstCyclePotentialRewards, _ = f.addValidatorAndCheckSupplyMint(
				validatorWeight,
				delegationShares,
				autoCompoundRewardShares,
				stakingPeriod,
			)
		})

		var chainTimeAtValidatorAdd time.Time
		tc.By("retrieving chain time after adding the validator", func() {
			var err error
			chainTimeAtValidatorAdd, err = pvmClient.GetTimestamp(tc.DefaultContext())
			require.NoError(tc, err)
		})

		tc.By("funding delegator wallets", func() {
			f.fundKey(delegator1FundingKey, delegator1Weight+units.Avax)
			f.fundKey(delegator2FundingKey, delegator2Weight+units.Avax)
		})

		var supplyBeforeDelegator1 uint64
		tc.By("adding delegator1 for half the staking period", func() {
			delegationEndTime := uint64(time.Now().Add(delegationPeriod).Unix())
			supplyBeforeDelegator1 = f.addDelegator(
				delegator1FundingKey,
				delegator1RewardKey,
				delegator1Weight,
				delegationEndTime,
			)
		})

		var (
			supplyBeforeSecondCycle    uint64
			delegator1PotentialRewards uint64
		)
		tc.By("verifying delegator1 is active and checking the supply mint", func() {
			supplyBeforeSecondCycle = currentSupply(tc, pvmClient)
			stakeStartTime, delegator1StakingDuration := waitForOneActiveDelegator(tc, pvmClient, f.validatorNode.NodeID)
			delegator1PotentialRewards = rewardsCalculator.Calculate(stakeStartTime, delegator1StakingDuration, delegator1Weight, supplyBeforeDelegator1)
			require.Equal(tc, supplyBeforeDelegator1+delegator1PotentialRewards, supplyBeforeSecondCycle)
		})

		tc.By("updating period to 15s", func() {
			f.setValidatorConfig(validatorTxID, autoCompoundRewardShares, updatedStakingPeriod)
		})

		tc.By("checking API fields reflect updated staking period", func() {
			require.Equal(tc, &platformvm.ClientAutoRenewedConfig{
				ValidatorAuthority: &platformvm.ClientOwner{
					Locktime:  0,
					Threshold: 1,
					Addresses: []ids.ShortID{f.validatorFundingKey.Address()},
				},
				NextPeriod:               uint64(updatedStakingPeriod.Seconds()),
				AutoCompoundRewardShares: autoCompoundRewardShares,
			}, currentValidator(tc, pvmClient, f.validatorNode.NodeID).AutoRenewedConfig)
		})

		tc.By("waiting for the first staking cycle to complete", func() {
			waitForAutoRenewedCycleEnd(tc, pvmClient, f.validatorNode.NodeID)
		})

		var chainTimeAtCycle1End time.Time
		tc.By("retrieving chain time after first cycle", func() {
			var err error
			chainTimeAtCycle1End, err = pvmClient.GetTimestamp(tc.DefaultContext())
			require.NoError(tc, err)
		})

		tc.By("checking the first cycle duration matches the staking period", func() {
			cycle1Duration := chainTimeAtCycle1End.Sub(chainTimeAtValidatorAdd)
			require.Equal(tc, stakingPeriod, cycle1Duration)
		})

		var (
			restakingValidationRewards1, restakingDelegateeRewards1 uint64
			withdrawnValidationRewards1, withdrawnDelegateeRewards1 uint64
		)
		tc.By("calculating first cycle reward splits", func() {
			delegateeReward1, delegator1Reward := reward.Split(delegator1PotentialRewards, delegationShares)
			restakingValidationRewards1, withdrawnValidationRewards1 = reward.Split(validatorFirstCyclePotentialRewards, autoCompoundRewardShares)
			restakingDelegateeRewards1, withdrawnDelegateeRewards1 = reward.Split(delegateeReward1, autoCompoundRewardShares)

			// delegator1 is the only delegator whose delegation ends mid-cycle, so
			// assert its payout here even though the eligibility spec covers
			// delegator rewards for cycle-boundary delegations.
			require.Equal(tc, delegator1Reward, balanceOf(tc, f.randomWalletNodeURI, delegator1RewardKey))
		})

		var validatorSecondCyclePotentialRewards uint64
		tc.By("checking supply was increased by the second cycle's potential reward on renewal", func() {
			expectedValidatorWeight := validatorWeight + restakingValidationRewards1 + restakingDelegateeRewards1
			validator := currentValidator(tc, pvmClient, f.validatorNode.NodeID)
			stakeStartTime := time.Unix(int64(validator.StartTime), 0)
			validatorSecondCyclePotentialRewards = rewardsCalculator.Calculate(
				stakeStartTime,
				updatedStakingPeriod,
				expectedValidatorWeight,
				supplyBeforeSecondCycle,
			)
			require.Equal(tc, supplyBeforeSecondCycle+validatorSecondCyclePotentialRewards, currentSupply(tc, pvmClient))
		})

		tc.By("updating auto compounded reward shares to 80%", func() {
			f.setValidatorConfig(validatorTxID, updatedAutoCompoundRewardShares, updatedStakingPeriod)
		})

		tc.By("checking API fields reflect updated auto compound shares", func() {
			require.Equal(tc, &platformvm.ClientAutoRenewedConfig{
				ValidatorAuthority: &platformvm.ClientOwner{
					Locktime:  0,
					Threshold: 1,
					Addresses: []ids.ShortID{f.validatorFundingKey.Address()},
				},
				NextPeriod:               uint64(updatedStakingPeriod.Seconds()),
				AutoCompoundRewardShares: updatedAutoCompoundRewardShares,
			}, currentValidator(tc, pvmClient, f.validatorNode.NodeID).AutoRenewedConfig)
		})

		var supplyBeforeDelegator2 uint64
		tc.By("adding delegator2 with endtime = validator endtime", func() {
			supplyBeforeDelegator2 = f.addDelegator(
				delegator2FundingKey,
				delegator2RewardKey,
				delegator2Weight,
				currentValidator(tc, pvmClient, f.validatorNode.NodeID).EndTime,
			)
		})

		var (
			delegator2PotentialRewards uint64
			supplyBeforeThirdCycle     uint64
		)
		tc.By("verifying delegator2 is active and checking the supply mint", func() {
			supplyBeforeThirdCycle = currentSupply(tc, pvmClient)
			stakeStartTime, delegator2StakingDuration := waitForOneActiveDelegator(tc, pvmClient, f.validatorNode.NodeID)
			delegator2PotentialRewards = rewardsCalculator.Calculate(stakeStartTime, delegator2StakingDuration, delegator2Weight, supplyBeforeDelegator2)
			require.Equal(tc, supplyBeforeDelegator2+delegator2PotentialRewards, supplyBeforeThirdCycle)
		})

		tc.By("waiting for the second staking cycle to complete", func() {
			waitForAutoRenewedCycleEnd(tc, pvmClient, f.validatorNode.NodeID)
		})

		var chainTimeAtCycle2End time.Time
		tc.By("retrieving chain time after second cycle", func() {
			var err error
			chainTimeAtCycle2End, err = pvmClient.GetTimestamp(tc.DefaultContext())
			require.NoError(tc, err)
		})

		tc.By("checking the second cycle duration matches the updated staking period", func() {
			cycle2Duration := chainTimeAtCycle2End.Sub(chainTimeAtCycle1End)
			require.Equal(tc, updatedStakingPeriod, cycle2Duration)
		})

		var (
			restakingValidationRewards2, restakingDelegateeRewards2 uint64
			withdrawnValidationRewards2, withdrawnDelegateeRewards2 uint64
		)
		tc.By("checking reward balances after updated auto-compound shares", func() {
			// Second cycle rewards are split with 80% auto-compounded (20% withdrawn)
			restakingValidationRewards2, withdrawnValidationRewards2 = reward.Split(validatorSecondCyclePotentialRewards, updatedAutoCompoundRewardShares)
			delegateeReward2, _ := reward.Split(delegator2PotentialRewards, delegationShares)
			restakingDelegateeRewards2, withdrawnDelegateeRewards2 = reward.Split(delegateeReward2, updatedAutoCompoundRewardShares)

			require.Equal(tc, withdrawnValidationRewards1+withdrawnValidationRewards2, balanceOf(tc, f.randomWalletNodeURI, f.validationRewardKey))
			require.Equal(tc, withdrawnDelegateeRewards1+withdrawnDelegateeRewards2, balanceOf(tc, f.randomWalletNodeURI, f.delegationRewardKey))
		})

		tc.By("checking auto-renewed validator's weight and accrued rewards", func() {
			expectedValidatorWeight := validatorWeight + restakingValidationRewards1 + restakingDelegateeRewards1 + restakingValidationRewards2 + restakingDelegateeRewards2
			require.Equal(tc, expectedValidatorWeight, currentValidator(tc, pvmClient, f.validatorNode.NodeID).Weight)
		})

		var validatorThirdCyclePotentialRewards uint64
		tc.By("checking supply was increased by the third cycle's potential reward on renewal", func() {
			expectedValidatorWeight := validatorWeight + restakingValidationRewards1 + restakingDelegateeRewards1 + restakingValidationRewards2 + restakingDelegateeRewards2
			validator := currentValidator(tc, pvmClient, f.validatorNode.NodeID)
			stakeStartTime := time.Unix(int64(validator.StartTime), 0)
			validatorThirdCyclePotentialRewards = rewardsCalculator.Calculate(
				stakeStartTime,
				updatedStakingPeriod,
				expectedValidatorWeight,
				supplyBeforeThirdCycle,
			)
			require.Equal(tc, supplyBeforeThirdCycle+validatorThirdCyclePotentialRewards, currentSupply(tc, pvmClient))
		})

		tc.By("setting period to 0 to request graceful exit", func() {
			f.setValidatorConfig(validatorTxID, updatedAutoCompoundRewardShares, 0)
		})

		tc.By("retrieving wallet balance before exiting")
		fundedKeyBalanceBeforeExit := balanceOf(tc, f.randomWalletNodeURI, f.validatorFundingKey)

		tc.By("waiting for the third staking cycle to complete", func() {
			waitForAutoRenewedCycleEnd(tc, pvmClient, f.validatorNode.NodeID)
		})

		tc.By("verifying the validator has exited the validator set", func() {
			requireValidatorRemoved(tc, pvmClient, f.validatorNode.NodeID, "validator should have exited after period=0")
		})

		tc.By("checking supply is unchanged by the graceful exit", func() {
			// The third cycle's potential reward was already minted on renewal and
			// is fully paid out on exit, so the exit itself mints and burns nothing.
			require.Equal(tc, supplyBeforeThirdCycle+validatorThirdCyclePotentialRewards, currentSupply(tc, pvmClient))
		})

		tc.By("checking final reward balances and stake returned", func() {
			// Check validation reward key balance includes all withdrawn + accrued
			// validation rewards and delegation reward key balance includes all
			// withdrawn + accrued delegatee rewards.
			expectedTotalValidationReward := withdrawnValidationRewards1 + withdrawnValidationRewards2 +
				restakingValidationRewards1 + restakingValidationRewards2 +
				validatorThirdCyclePotentialRewards
			expectedTotalDelegateeReward := withdrawnDelegateeRewards1 + withdrawnDelegateeRewards2 +
				restakingDelegateeRewards1 + restakingDelegateeRewards2

			require.Equal(tc, expectedTotalValidationReward, balanceOf(tc, f.randomWalletNodeURI, f.validationRewardKey))
			require.Equal(tc, expectedTotalDelegateeReward, balanceOf(tc, f.randomWalletNodeURI, f.delegationRewardKey))

			// The funded key should have received the original stake back.
			require.Equal(tc, fundedKeyBalanceBeforeExit+validatorWeight, balanceOf(tc, f.randomWalletNodeURI, f.validatorFundingKey))
		})

		var (
			validatorReAddPotentialRewards uint64
			supplyBeforeReAdd              uint64
		)
		tc.By("re-adding the same node as an auto-renewed validator after exit", func() {
			validatorTxID, validatorReAddPotentialRewards, supplyBeforeReAdd = f.addValidatorAndCheckSupplyMint(
				validatorWeight,
				delegationShares,
				autoCompoundRewardShares,
				updatedStakingPeriod,
			)
		})

		tc.By("verifying the re-added validator is in the current set", func() {
			currentValidator(tc, pvmClient, f.validatorNode.NodeID)
		})

		tc.By("waiting for the re-added validator's staking cycle to complete", func() {
			waitForAutoRenewedCycleEnd(tc, pvmClient, f.validatorNode.NodeID)
		})

		tc.By("checking supply was increased by the re-added validator's renewal", func() {
			// The re-added validator has no delegators, so its renewed weight only
			// grows by the restaked share of its own validation reward.
			restakingReAddRewards, _ := reward.Split(validatorReAddPotentialRewards, autoCompoundRewardShares)
			validator := currentValidator(tc, pvmClient, f.validatorNode.NodeID)
			stakeStartTime := time.Unix(int64(validator.StartTime), 0)
			renewalPotentialReward := rewardsCalculator.Calculate(
				stakeStartTime,
				updatedStakingPeriod,
				validatorWeight+restakingReAddRewards,
				supplyBeforeReAdd+validatorReAddPotentialRewards,
			)

			require.Equal(tc, supplyBeforeReAdd+validatorReAddPotentialRewards+renewalPotentialReward, currentSupply(tc, pvmClient))
		})

		// Gracefully exit the re-added validator so the spec leaves no validator
		// behind that would keep minting on renewals and eventually burn its
		// potential reward, polluting the supply observed by later specs.
		tc.By("requesting graceful exit of the re-added validator", func() {
			f.setValidatorConfig(validatorTxID, autoCompoundRewardShares, 0)
		})

		tc.By("waiting for the re-added validator to exit", func() {
			requireValidatorRemoved(tc, pvmClient, f.validatorNode.NodeID, "re-added validator should have exited after period=0")
		})

		tc.By("stopping node to free up resources for a bootstrap check", func() {
			require.NoError(tc, f.validatorNode.Stop(tc.DefaultContext()))
		})

		_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
	})
})
