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
)

var _ = DescribeACP236("[Reward Eligibility]", ginkgo.Label("local"), func() {
	var (
		tc      = e2e.NewTestContext()
		require = require.New(tc)
	)

	ginkgo.It("should reward eligible stakers and not reward ineligible ones", func() {
		const (
			weight            = 2_000 * units.Avax
			delegator1Weight  = 1_000 * units.Avax
			delegator2Weight  = 500 * units.Avax
			delegationShares  = uint32(reward.PercentDenominator * 0.10) // 10%
			autoRestakeShares = uint32(reward.PercentDenominator * 0.40) // 40%
			stakingPeriod     = 20 * time.Second
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

		tc.By("creating keys and wallets")
		var (
			validationRewardKey  = e2e.NewPrivateKey(tc)
			delegationRewardKey  = e2e.NewPrivateKey(tc)
			delegator1RewardKey  = e2e.NewPrivateKey(tc)
			delegator1FundingKey = e2e.NewPrivateKey(tc)
			delegator2RewardKey  = e2e.NewPrivateKey(tc)
			delegator2FundingKey = e2e.NewPrivateKey(tc)

			rewardKeys = []*secp256k1.PrivateKey{
				validationRewardKey,
				delegationRewardKey,
				delegator1RewardKey,
				delegator2RewardKey,
			}

			keychain      = env.NewKeychain()
			walletNodeURI = network.GetNodeURIs()[0] // use another node, because we will stop it later to fail uptime check
			pWallet       = e2e.NewWallet(tc, keychain, walletNodeURI).P()
			pContext      = pWallet.Builder().Context()
			pvmClient     = platformvm.NewClient(walletNodeURI.URI)
			adminClient   = admin.NewClient(walletNodeURI.URI)
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

		tc.By("adding the node as a continuous validator", func() {
			_, err := pWallet.IssueAddContinuousValidatorTx(
				nodeID,
				weight,
				nodePOP,
				pContext.AVAXAssetID,
				validationRewardsOwner,
				delegationRewardsOwner,
				configOwner,
				delegationShares,
				autoRestakeShares,
				stakingPeriod,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("funding delegator1 wallet", func() {
			_, err = pWallet.IssueBaseTx(
				[]*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: pContext.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: delegator1Weight + units.Avax,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{delegator1FundingKey.Address()},
							},
						},
					},
				},
				tc.WithDefaultContext(),
			)
		})
		require.NoError(err)

		tc.By("retrieving supply before adding delegator1")
		supplyAtDelegator1Start, _, err := pvmClient.GetCurrentSupply(tc.DefaultContext(), constants.PrimaryNetworkID)
		require.NoError(err)

		tc.By("adding delegator1 for the first staking cycle with same end time as validator", func() {
			delegator1Keychain := secp256k1fx.NewKeychain(delegator1FundingKey)
			delegator1PWallet := e2e.NewWallet(tc, delegator1Keychain, walletNodeURI).P()

			// Get validator's end time so delegator1 delegates for the entire cycle
			validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
			require.NoError(err)
			require.Len(validators, 1)
			validatorEndTime := validators[0].EndTime

			_, err = delegator1PWallet.IssueAddPermissionlessDelegatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID,
						End:    validatorEndTime,
						Wght:   delegator1Weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				pContext.AVAXAssetID,
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{delegator1RewardKey.Address()},
				},
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		var actualDelegator1Period time.Duration
		tc.By("verifying the validator and delegator1 are active", func() {
			tc.Eventually(func() bool {
				validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
				require.NoError(err)
				return len(validators) == 1 && len(validators[0].Delegators) == 1
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "validator and delegator1 not active")

			validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
			require.NoError(err)
			require.Len(validators, 1)
			delegator := validators[0].Delegators[0]
			actualDelegator1Period = time.Duration(delegator.EndTime-delegator.StartTime) * time.Second
		})

		tc.By("waiting for the first staking cycle to complete", func() {
			stakingHelper.waitForStakingCycleEnd(nodeID)
		})

		tc.By("stopping the validator node to fail uptime check in the second cycle", func() {
			require.NoError(node.Stop(tc.DefaultContext()))
		})

		var expectedValidationReward1, expectedDelegateeReward1, expectedDelegator1Reward, restakedValidationRewards1, restakedDelegateeRewards1 uint64
		tc.By("checking reward balances after first cycle", func() {
			rewardBalances := make(map[ids.ShortID]uint64, len(rewardKeys))
			for _, rewardKey := range rewardKeys {
				rewardKeychain := secp256k1fx.NewKeychain(rewardKey)
				rewardPWallet := e2e.NewWallet(tc, rewardKeychain, walletNodeURI).P()
				balances, err := rewardPWallet.Builder().GetBalance()
				require.NoError(err)
				rewardBalances[rewardKey.Address()] = balances[pContext.AVAXAssetID]
			}

			// Calculate expected rewards
			potentialValidationReward := calculator.Calculate(stakingPeriod, weight, supplyAtValidatorStart)
			potentialDelegationReward := calculator.Calculate(actualDelegator1Period, delegator1Weight, supplyAtDelegator1Start)

			expectedDelegateeReward1, expectedDelegator1Reward = reward.Split(potentialDelegationReward, delegationShares)
			restakedValidationRewards1, expectedValidationReward1 = reward.Split(potentialValidationReward, autoRestakeShares)
			restakedDelegateeRewards1, expectedDelegateeReward1 = reward.Split(expectedDelegateeReward1, autoRestakeShares)

			require.Equal(
				map[ids.ShortID]uint64{
					validationRewardKey.Address(): expectedValidationReward1,
					delegationRewardKey.Address(): expectedDelegateeReward1,
					delegator1RewardKey.Address(): expectedDelegator1Reward,
					delegator2RewardKey.Address(): 0, // delegator2 not active yet
				},
				rewardBalances,
			)
		})

		tc.By("checking validator weight increased by restaked rewards", func() {
			validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
			require.NoError(err)
			require.Len(validators, 1)

			expectedWeight := weight + restakedValidationRewards1 + restakedDelegateeRewards1
			require.Equal(expectedWeight, validators[0].Weight)
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

		tc.By("adding delegator2 for the second staking cycle with same end time as validator", func() {
			delegator2Keychain := secp256k1fx.NewKeychain(delegator2FundingKey)
			delegator2PWallet := e2e.NewWallet(tc, delegator2Keychain, walletNodeURI).P()

			// Get validator's end time so delegator2 delegates for the entire remaining period
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

		tc.By("verifying delegator2 is active", func() {
			tc.Eventually(func() bool {
				validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
				require.NoError(err)
				return len(validators) == 1 && len(validators[0].Delegators) == 1
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "delegator2 not active")
		})

		tc.By("retrieving wallet balance before validator exits")
		fundedKeyBalancesBeforeExit, err := pWallet.Builder().GetBalance()
		require.NoError(err)

		tc.By("waiting for the second staking cycle to complete", func() {
			stakingHelper.waitForStakingCycleEnd(nodeID)
		})

		tc.By("verifying the validator is no longer in the current set due to uptime failure", func() {
			tc.Eventually(func() bool {
				validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
				require.NoError(err)
				return len(validators) == 0
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "validator should have been removed due to uptime failure")
		})

		tc.By("checking reward balances after second cycle", func() {
			rewardBalances := make(map[ids.ShortID]uint64, len(rewardKeys))
			for _, rewardKey := range rewardKeys {
				rewardKeychain := secp256k1fx.NewKeychain(rewardKey)
				rewardPWallet := e2e.NewWallet(tc, rewardKeychain, walletNodeURI).P()
				balances, err := rewardPWallet.Builder().GetBalance()
				require.NoError(err)
				rewardBalances[rewardKey.Address()] = balances[pContext.AVAXAssetID]
			}

			require.Equal(
				map[ids.ShortID]uint64{
					validationRewardKey.Address(): expectedValidationReward1 + restakedValidationRewards1,
					delegationRewardKey.Address(): expectedDelegateeReward1 + restakedDelegateeRewards1,
					delegator1RewardKey.Address(): expectedDelegator1Reward,
					delegator2RewardKey.Address(): 0,
				},
				rewardBalances,
			)
		})

		tc.By("checking stake was returned", func() {
			// Refresh wallet to get updated UTXOs after validator exit
			pWallet = e2e.NewWallet(tc, keychain, walletNodeURI).P()
			fundedKeyBalances, err := pWallet.Builder().GetBalance()
			require.NoError(err)

			require.Equal(fundedKeyBalancesBeforeExit[pContext.AVAXAssetID]+weight, fundedKeyBalances[pContext.AVAXAssetID])
		})

		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})
