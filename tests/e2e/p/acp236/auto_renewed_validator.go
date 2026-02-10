// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp236

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/e2e/p"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

var _ = DescribeACP236("[Auto-Renewed Validator]", func() {
	var (
		tc      = e2e.NewTestContext()
		require = require.New(tc)
	)

	ginkgo.It("should add an auto-renewed validator and complete a staking cycle", func() {
		const (
			weight                            = 2_000 * units.Avax
			delegatorWeight                   = 1_000 * units.Avax
			delegator2Weight                  = 500 * units.Avax
			delegationShares                  = uint32(reward.PercentDenominator * 0.10) // 10%
			autoCompoundedRewardShares        = uint32(reward.PercentDenominator * 0.40) // 40%
			updatedAutoCompoundedRewardShares = uint32(reward.PercentDenominator * 0.80) // 80%
			stakingPeriod                     = 20 * time.Second
			delegationPeriod                  = stakingPeriod / 2 // delegator stakes for half the validator's period
		)

		var (
			env     = e2e.GetEnv(tc)
			network = env.GetNetwork()
		)

		tc.By("adding an ephemeral node")
		node := e2e.AddEphemeralNode(tc, network, tmpnet.NewEphemeralNode(tmpnet.FlagsMap{}))

		tc.By("waiting until node is healthy")
		e2e.WaitForHealthy(tc, node)

		tc.By("retrieving node id and proof of possession")
		nodeURI := node.GetAccessibleURI()
		infoClient := info.NewClient(nodeURI)
		nodeID, nodePOP, err := infoClient.GetNodeID(tc.DefaultContext())
		require.NoError(err)

		tc.By("creating reward keys and keychain")
		var (
			validationRewardKey  = e2e.NewPrivateKey(tc)
			delegationRewardKey  = e2e.NewPrivateKey(tc)
			delegatorRewardKey   = e2e.NewPrivateKey(tc)
			delegatorFundingKey  = e2e.NewPrivateKey(tc)
			delegator2RewardKey  = e2e.NewPrivateKey(tc)
			delegator2FundingKey = e2e.NewPrivateKey(tc)

			rewardKeys = []*secp256k1.PrivateKey{
				validationRewardKey,
				delegationRewardKey,
				delegatorRewardKey,
				delegator2RewardKey,
			}

			keychain      = env.NewKeychain()
			walletNodeURI = tmpnet.NodeURI{NodeID: nodeID, URI: nodeURI}
			pWallet       = e2e.NewWallet(tc, keychain, walletNodeURI).P()
			pContext      = pWallet.Builder().Context()
			pvmClient     = platformvm.NewClient(nodeURI)
			adminClient   = admin.NewClient(nodeURI)
			rewardConfig  = p.GetRewardConfig(tc, adminClient)
			calculator    = reward.NewCalculator(rewardConfig)
			stakingHelper = stakingHelper{tc: tc, require: require, pvmClient: pvmClient}
		)

		configOwner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keychain.Keys[0].Address()},
		}
		validationRewardsOwner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{validationRewardKey.Address()},
		}
		delegationRewardsOwner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{delegationRewardKey.Address()},
		}

		tc.By("retrieving supply before adding the validator")
		supplyAtValidatorStart, _, err := pvmClient.GetCurrentSupply(tc.DefaultContext(), constants.PrimaryNetworkID)
		require.NoError(err)

		var validatorTxID ids.ID
		tc.By("adding the node as an auto-renewed validator", func() {
			tx, err := pWallet.IssueAddAutoRenewedValidatorTx(
				nodeID,
				weight,
				nodePOP,
				pContext.AVAXAssetID,
				validationRewardsOwner,
				delegationRewardsOwner,
				configOwner,
				delegationShares,
				autoCompoundedRewardShares,
				stakingPeriod,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
			validatorTxID = tx.ID()
		})

		tc.By("funding delegator wallet", func() {
			_, err = pWallet.IssueBaseTx(
				[]*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: pContext.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: delegatorWeight + units.Avax, // stake + fees
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{delegatorFundingKey.Address()},
							},
						},
					},
				},
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("funding delegator2 wallet", func() {
			pWallet = e2e.NewWallet(tc, keychain, walletNodeURI).P()
			_, err = pWallet.IssueBaseTx(
				[]*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: pContext.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: delegator2Weight + units.Avax,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{delegator2FundingKey.Address()},
							},
						},
					},
				},
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("retrieving supply before adding the delegator")
		supplyAtDelegatorStart, _, err := pvmClient.GetCurrentSupply(tc.DefaultContext(), constants.PrimaryNetworkID)
		require.NoError(err)

		tc.By("adding a delegator for half the staking period", func() {
			delegatorKeychain := secp256k1fx.NewKeychain(delegatorFundingKey)
			delegatorPWallet := e2e.NewWallet(tc, delegatorKeychain, walletNodeURI).P()

			delegationEndTime := time.Now().Add(delegationPeriod)

			_, err := delegatorPWallet.IssueAddPermissionlessDelegatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID,
						End:    uint64(delegationEndTime.Unix()),
						Wght:   delegatorWeight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				pContext.AVAXAssetID,
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{delegatorRewardKey.Address()},
				},
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("retrieving supply for second cycle")
		supplyAtCycle2Start, _, err := pvmClient.GetCurrentSupply(tc.DefaultContext(), constants.PrimaryNetworkID)
		require.NoError(err)

		var actualDelegatorPeriod time.Duration
		tc.By("verifying the validator is in the current validator set and retrieving delegation period", func() {
			tc.Eventually(func() bool {
				validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
				require.NoError(err)

				return len(validators) == 1 && len(validators[0].Delegators) == 1
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "node failed to start validating before timeout")

			// Get the actual delegation period from the chain (validator uses specified stakingPeriod)
			validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
			require.NoError(err)
			require.Len(validators, 1)
			delegator := validators[0].Delegators[0]
			actualDelegatorPeriod = time.Duration(delegator.EndTime-delegator.StartTime) * time.Second
		})

		tc.By("waiting for the first staking cycle to complete", func() {
			stakingHelper.waitForStakingCycleEnd(nodeID)
		})

		var expectedValidationReward1, expectedDelegateeReward1, expectedDelegatorReward1,
			restakingValidationRewards1, restakingDelegateeRewards1 uint64
		tc.By("checking reward balances after first cycle completion", func() {
			rewardBalances := make(map[ids.ShortID]uint64, len(rewardKeys))
			for _, rewardKey := range rewardKeys {
				var (
					keychain = secp256k1fx.NewKeychain(rewardKey)
					pWallet  = e2e.NewWallet(tc, keychain, walletNodeURI).P()
					pBuilder = pWallet.Builder()
				)

				balances, err := pBuilder.GetBalance()
				require.NoError(err)
				rewardBalances[rewardKey.Address()] = balances[pContext.AVAXAssetID]
			}

			// Calculate expected rewards using the reward calculator
			potentialValidationReward := calculator.Calculate(stakingPeriod, weight, supplyAtValidatorStart)
			potentialDelegationReward := calculator.Calculate(actualDelegatorPeriod, delegatorWeight, supplyAtDelegatorStart)

			expectedDelegateeReward1, expectedDelegatorReward1 = reward.Split(potentialDelegationReward, delegationShares)
			restakingValidationRewards1, expectedValidationReward1 = reward.Split(potentialValidationReward, autoCompoundedRewardShares)
			restakingDelegateeRewards1, expectedDelegateeReward1 = reward.Split(expectedDelegateeReward1, autoCompoundedRewardShares)

			require.Equal(
				map[ids.ShortID]uint64{
					validationRewardKey.Address(): expectedValidationReward1,
					delegationRewardKey.Address(): expectedDelegateeReward1,
					delegatorRewardKey.Address():  expectedDelegatorReward1,
					delegator2RewardKey.Address(): 0,
				},
				rewardBalances,
			)
		})

		tc.By("checking validator's weight and accrued rewards", func() {
			validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
			require.NoError(err)
			require.Len(validators, 1)

			expectedWeight := weight + restakingValidationRewards1 + restakingDelegateeRewards1
			require.Equal(expectedWeight, validators[0].Weight)
		})

		tc.By("updating auto compounded reward shares to 80%", func() {
			pWallet = e2e.NewWalletWithConfig(tc, keychain, walletNodeURI, primary.WalletConfig{AutoRenewedValidatorTxIDs: []ids.ID{validatorTxID}}).P()

			_, err := pWallet.IssueSetAutoRenewedValidatorConfigTx(
				validatorTxID,
				updatedAutoCompoundedRewardShares,
				stakingPeriod,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("retrieving supply before adding delegator2")
		supplyAtDelegator2Start, _, err := pvmClient.GetCurrentSupply(tc.DefaultContext(), constants.PrimaryNetworkID)
		require.NoError(err)

		tc.By("adding delegator2 with endtime = validator endtime", func() {
			delegator2Keychain := secp256k1fx.NewKeychain(delegator2FundingKey)
			delegator2PWallet := e2e.NewWallet(tc, delegator2Keychain, walletNodeURI).P()

			validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
			require.NoError(err)
			require.Len(validators, 1)
			validatorEndTime := validators[0].EndTime

			_, err = delegator2PWallet.IssueAddPermissionlessDelegatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID,
						End:    validatorEndTime,
						Wght:   delegator2Weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				pContext.AVAXAssetID,
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{delegator2RewardKey.Address()},
				},
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		var actualDelegator2Period time.Duration
		tc.By("verifying delegator2 is active and retrieving delegation period", func() {
			tc.Eventually(func() bool {
				validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
				require.NoError(err)
				return len(validators) == 1 && len(validators[0].Delegators) == 1
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "delegator2 not active")

			validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
			require.NoError(err)
			require.Len(validators, 1)
			delegator2 := validators[0].Delegators[0]
			actualDelegator2Period = time.Duration(delegator2.EndTime-delegator2.StartTime) * time.Second
		})

		tc.By("retrieving supply for third cycle")
		supplyAtCycle3Start, _, err := pvmClient.GetCurrentSupply(tc.DefaultContext(), constants.PrimaryNetworkID)
		require.NoError(err)

		tc.By("waiting for the second staking cycle to complete", func() {
			stakingHelper.waitForStakingCycleEnd(nodeID)
		})

		var expectedValidationReward2, expectedDelegateeReward2, expectedDelegator2Reward,
			restakingValidationRewards2, restakingDelegateeRewards2 uint64
		tc.By("checking reward balances after second cycle completion", func() {
			rewardBalances := make(map[ids.ShortID]uint64, len(rewardKeys))
			for _, rewardKey := range rewardKeys {
				var (
					keychain = secp256k1fx.NewKeychain(rewardKey)
					pWallet  = e2e.NewWallet(tc, keychain, walletNodeURI).P()
					pBuilder = pWallet.Builder()
				)

				balances, err := pBuilder.GetBalance()
				require.NoError(err)
				rewardBalances[rewardKey.Address()] = balances[pContext.AVAXAssetID]
			}

			// Calculate expected rewards for second cycle with 80% auto-compounded (20% withdrawn)
			cycle2ValidatorWeight := weight + restakingValidationRewards1 + restakingDelegateeRewards1
			potentialValidationReward2 := calculator.Calculate(stakingPeriod, cycle2ValidatorWeight, supplyAtCycle2Start)
			restakingValidationRewards2, expectedValidationReward2 = reward.Split(potentialValidationReward2, updatedAutoCompoundedRewardShares)

			// Calculate delegator2 rewards
			potentialDelegationReward2 := calculator.Calculate(actualDelegator2Period, delegator2Weight, supplyAtDelegator2Start)
			expectedDelegateeReward2, expectedDelegator2Reward = reward.Split(potentialDelegationReward2, delegationShares)
			restakingDelegateeRewards2, expectedDelegateeReward2 = reward.Split(expectedDelegateeReward2, updatedAutoCompoundedRewardShares)

			require.Equal(
				map[ids.ShortID]uint64{
					validationRewardKey.Address(): expectedValidationReward1 + expectedValidationReward2,
					delegationRewardKey.Address(): expectedDelegateeReward1 + expectedDelegateeReward2,
					delegatorRewardKey.Address():  expectedDelegatorReward1,
					delegator2RewardKey.Address(): expectedDelegator2Reward,
				},
				rewardBalances,
			)
		})

		tc.By("checking auto-renewed validator's weight and accrued rewards", func() {
			validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
			require.NoError(err)
			require.Len(validators, 1)

			expectedWeight := weight + restakingValidationRewards1 + restakingDelegateeRewards1 + restakingValidationRewards2 + restakingDelegateeRewards2
			require.Equal(expectedWeight, validators[0].Weight)
		})

		tc.By("setting period to 0 to request graceful exit", func() {
			// Refresh wallet to get updated UTXOs after reward distribution
			pWallet = e2e.NewWalletWithConfig(tc, keychain, walletNodeURI, primary.WalletConfig{AutoRenewedValidatorTxIDs: []ids.ID{validatorTxID}}).P()

			_, err := pWallet.IssueSetAutoRenewedValidatorConfigTx(
				validatorTxID,
				updatedAutoCompoundedRewardShares,
				0,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("retrieving wallet balance before exiting")
		fundedKeyBalancesBeforeExit, err := pWallet.Builder().GetBalance()
		require.NoError(err)

		tc.By("waiting for the third staking cycle to complete", func() {
			stakingHelper.waitForStakingCycleEnd(nodeID)
		})

		tc.By("verifying the validator has exited the validator set", func() {
			tc.Eventually(func() bool {
				validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
				require.NoError(err)
				return len(validators) == 0
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "validator should have exited after period=0")
		})

		tc.By("checking final reward balances and stake returned", func() {
			// Calculate expected rewards for third cycle (all rewards withdrawn since exiting)
			cycle3ValidatorWeight := weight + restakingValidationRewards1 + restakingDelegateeRewards1 + restakingValidationRewards2 + restakingDelegateeRewards2
			validatorPotentialReward3 := calculator.Calculate(stakingPeriod, cycle3ValidatorWeight, supplyAtCycle3Start)

			// Check validation reward key balance includes all withdrawn + accrued validation rewards
			validationKeychain := secp256k1fx.NewKeychain(validationRewardKey)
			validationPWallet := e2e.NewWallet(tc, validationKeychain, walletNodeURI).P()
			validationBalances, err := validationPWallet.Builder().GetBalance()
			require.NoError(err)

			expectedTotalValidationReward := expectedValidationReward1 + expectedValidationReward2 +
				restakingValidationRewards1 + restakingValidationRewards2 +
				validatorPotentialReward3
			require.Equal(expectedTotalValidationReward, validationBalances[pContext.AVAXAssetID])

			// Check delegation reward key balance includes all withdrawn + accrued delegatee rewards
			delegationKeychain := secp256k1fx.NewKeychain(delegationRewardKey)
			delegationPWallet := e2e.NewWallet(tc, delegationKeychain, walletNodeURI).P()
			delegationBalances, err := delegationPWallet.Builder().GetBalance()
			require.NoError(err)

			expectedTotalDelegateeReward := expectedDelegateeReward1 + expectedDelegateeReward2 +
				restakingDelegateeRewards1 + restakingDelegateeRewards2
			require.Equal(expectedTotalDelegateeReward, delegationBalances[pContext.AVAXAssetID])

			// Check that stake was returned to the config owner (funded key)
			pWallet = e2e.NewWallet(tc, keychain, walletNodeURI).P()
			fundedKeyBalances, err := pWallet.Builder().GetBalance()
			require.NoError(err)

			// The funded key should have received the original stake back.
			require.Equal(fundedKeyBalancesBeforeExit[pContext.AVAXAssetID]+weight, fundedKeyBalances[pContext.AVAXAssetID])
		})

		tc.By("stopping node to free up resources for a bootstrap check", func() {
			require.NoError(node.Stop(tc.DefaultContext()))
		})

		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})
