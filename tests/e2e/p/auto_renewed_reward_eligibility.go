// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

var _ = e2e.DescribePChain("[Auto-Renewed Validators] [Reward Eligibility]", func() {
	var (
		tc      = e2e.NewTestContext()
		require = require.New(tc)
	)

	ginkgo.It("should reward eligible stakers and not reward ineligible ones", func() {
		const (
			// Validator's data
			validatorWeight          = 2_000 * units.Avax
			delegationShares         = uint32(reward.PercentDenominator * 0.10) // 10%
			autoCompoundRewardShares = uint32(reward.PercentDenominator * 0.40) // 40%
			stakingPeriod            = 20 * time.Second

			// Delegators' weights
			delegator1Weight = 1_000 * units.Avax
			delegator2Weight = 500 * units.Avax

			gasAmount = 1 * units.Avax
		)

		env := e2e.GetEnv(tc)

		requireHeliconActivated(tc, require, info.NewClient(env.GetRandomNodeURI().URI))

		f := newAutoRenewedValidatorFixture(
			tc,
			require,
			env,
			validatorWeight+gasAmount, // funding amount
		)

		pvmClient := platformvm.NewClient(f.randomWalletNodeURI.URI)
		rewardsCalculator := reward.NewCalculator(GetRewardConfig(f.tc, admin.NewClient(f.randomWalletNodeURI.URI)))

		var (
			delegator1RewardKey  = e2e.NewPrivateKey(tc)
			delegator1FundingKey = e2e.NewPrivateKey(tc)
			delegator2RewardKey  = e2e.NewPrivateKey(tc)
			delegator2FundingKey = e2e.NewPrivateKey(tc)
		)

		tc.By("adding the node as an auto renewed validator and checking the supply mint")
		_, validatorFirstCyclePotentialRewards, _ := f.addValidatorAndCheckSupplyMint(
			validatorWeight,
			delegationShares,
			autoCompoundRewardShares,
			stakingPeriod,
		)

		var supplyBeforeDelegator1 uint64
		tc.By("funding and adding delegator1 for the first staking cycle with same end time as validator", func() {
			f.fundKey(delegator1FundingKey, delegator1Weight+units.Avax)
			supplyBeforeDelegator1 = f.addDelegator(
				delegator1FundingKey,
				delegator1RewardKey,
				delegator1Weight,
				currentValidator(tc, require, pvmClient, f.validatorNode.NodeID).EndTime,
			)
		})

		var (
			supplyBeforeSecondCycle    uint64
			delegator1PotentialRewards uint64
		)
		tc.By("verifying delegator1 is active and checking the supply mint", func() {
			supplyBeforeSecondCycle = currentSupply(tc, require, pvmClient)
			delegator1StakingDuration := waitForOneActiveDelegator(tc, require, pvmClient, f.validatorNode.NodeID)
			delegator1PotentialRewards = rewardsCalculator.Calculate(delegator1StakingDuration, delegator1Weight, supplyBeforeDelegator1)
			require.Equal(supplyBeforeDelegator1+delegator1PotentialRewards, supplyBeforeSecondCycle)
		})

		tc.By("retrieving delegator1 wallet balance before delegation ends")
		delegator1BalanceBeforeExit := balanceOf(tc, require, f.randomWalletNodeURI, delegator1FundingKey)

		tc.By("waiting for the first staking cycle to complete", func() {
			waitForAutoRenewedCycleEnd(tc, require, pvmClient, f.validatorNode.NodeID)
		})

		tc.By("stopping the validator node to fail uptime check in the second cycle", func() {
			require.NoError(f.validatorNode.Stop(tc.DefaultContext()))
		})

		var (
			restakingValidationRewards, restakingDelegateeRewards uint64
			withdrawnValidationRewards, withdrawnDelegateeRewards uint64
			delegator1Reward                                      uint64
		)

		tc.By("checking reward balances and validator's weight after first cycle", func() {
			var delegateeReward uint64

			delegateeReward, delegator1Reward = reward.Split(delegator1PotentialRewards, delegationShares)
			restakingValidationRewards, withdrawnValidationRewards = reward.Split(validatorFirstCyclePotentialRewards, autoCompoundRewardShares)
			restakingDelegateeRewards, withdrawnDelegateeRewards = reward.Split(delegateeReward, autoCompoundRewardShares)

			require.Equal(withdrawnValidationRewards, balanceOf(tc, require, f.randomWalletNodeURI, f.validationRewardKey))
			require.Equal(withdrawnDelegateeRewards, balanceOf(tc, require, f.randomWalletNodeURI, f.delegationRewardKey))

			expectedDelegator1Balance := delegator1Reward
			require.Equal(expectedDelegator1Balance, balanceOf(tc, require, f.randomWalletNodeURI, delegator1RewardKey))
			require.Zero(balanceOf(tc, require, f.randomWalletNodeURI, delegator2RewardKey)) // delegator2 not active yet

			expectedValidatorWeight := validatorWeight + restakingValidationRewards + restakingDelegateeRewards
			require.Equal(expectedValidatorWeight, currentValidator(tc, require, pvmClient, f.validatorNode.NodeID).Weight)
		})

		tc.By("checking delegator1 stake was returned", func() {
			require.Equal(delegator1BalanceBeforeExit+delegator1Weight, balanceOf(tc, require, f.randomWalletNodeURI, delegator1FundingKey))
		})

		var validatorSecondCyclePotentialRewards uint64
		tc.By("checking supply was increased by the second cycle's potential reward on renewal", func() {
			expectedValidatorWeight := validatorWeight + restakingValidationRewards + restakingDelegateeRewards

			validatorSecondCyclePotentialRewards = rewardsCalculator.Calculate(
				stakingPeriod,
				expectedValidatorWeight,
				supplyBeforeSecondCycle,
			)
			require.Equal(supplyBeforeSecondCycle+validatorSecondCyclePotentialRewards, currentSupply(tc, require, pvmClient))
		})

		var supplyBeforeDelegator2 uint64
		tc.By("funding and adding delegator2 for the second staking cycle with same end time as validator", func() {
			f.fundKey(delegator2FundingKey, delegator2Weight+units.Avax)
			supplyBeforeDelegator2 = f.addDelegator(
				delegator2FundingKey,
				delegator2RewardKey,
				delegator2Weight,
				currentValidator(tc, require, pvmClient, f.validatorNode.NodeID).EndTime,
			)
		})

		var (
			delegator2PotentialRewards uint64
			supplyAfterDelegator2      uint64
		)
		tc.By("verifying delegator2 is active and checking the supply mint", func() {
			supplyAfterDelegator2 = currentSupply(tc, require, pvmClient)
			actualDelegator2Period := waitForOneActiveDelegator(tc, require, pvmClient, f.validatorNode.NodeID)
			delegator2PotentialRewards = rewardsCalculator.Calculate(actualDelegator2Period, delegator2Weight, supplyBeforeDelegator2)
			require.Equal(supplyBeforeDelegator2+delegator2PotentialRewards, supplyAfterDelegator2)
		})

		tc.By("retrieving delegator2 wallet balance before delegation ends")
		delegator2BalanceBeforeExit := balanceOf(tc, require, f.randomWalletNodeURI, delegator2FundingKey)

		tc.By("retrieving wallet balance before validator exits")
		fundedKeyBalanceBeforeExit := balanceOf(tc, require, f.randomWalletNodeURI, f.validatorFundingKey)

		tc.By("waiting for the second staking cycle to complete", func() {
			waitForAutoRenewedCycleEnd(tc, require, pvmClient, f.validatorNode.NodeID)
		})

		tc.By("verifying the validator is no longer in the current set due to uptime failure", func() {
			requireValidatorRemoved(tc, require, pvmClient, f.validatorNode.NodeID, "validator should have been removed due to uptime failure")
		})

		tc.By("checking unearned potential rewards were burned on the failed cycle", func() {
			// Neither the validator nor delegator2 earned their second-cycle
			// potential rewards, so both optimistic supply mints are reverted.
			expectedSupply := supplyAfterDelegator2 - validatorSecondCyclePotentialRewards - delegator2PotentialRewards
			require.Equal(expectedSupply, currentSupply(tc, require, pvmClient))
		})

		tc.By("checking reward balances after second cycle", func() {
			require.Equal(restakingValidationRewards+withdrawnValidationRewards, balanceOf(tc, require, f.randomWalletNodeURI, f.validationRewardKey))
			require.Equal(restakingDelegateeRewards+withdrawnDelegateeRewards, balanceOf(tc, require, f.randomWalletNodeURI, f.delegationRewardKey))
			require.Equal(delegator1Reward, balanceOf(tc, require, f.randomWalletNodeURI, delegator1RewardKey))
			require.Zero(balanceOf(tc, require, f.randomWalletNodeURI, delegator2RewardKey))
		})

		tc.By("checking delegator2 stake was returned", func() {
			require.Equal(delegator2BalanceBeforeExit+delegator2Weight, balanceOf(tc, require, f.randomWalletNodeURI, delegator2FundingKey))
		})

		tc.By("checking stake was returned", func() {
			require.Equal(fundedKeyBalanceBeforeExit+validatorWeight, balanceOf(tc, require, f.randomWalletNodeURI, f.validatorFundingKey))
		})

		_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
	})
})
