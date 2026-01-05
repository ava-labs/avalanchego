// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ = ginkgo.Describe("[Staking Rewards]", func() {
	var (
		tc      = e2e.NewTestContext()
		require = require.New(tc)
	)

	ginkgo.It("should ensure that validator node uptime determines whether a staking reward is issued", func() {
		const (
			targetDelegationPeriod = 15 * time.Second
			targetValidationPeriod = 30 * time.Second

			delegationPercent = 0.10 // 10%
			delegationShare   = reward.PercentDenominator * delegationPercent
			weight            = 2_000 * units.Avax
		)

		var (
			env     = e2e.GetEnv(tc)
			network = env.GetNetwork()
		)

		tc.By("checking that the network has a compatible minimum stake duration", func() {
			require.Equal(tmpnet.DefaultMinStakeDuration, network.DefaultFlags[config.MinStakeDurationKey])
		})

		tc.By("adding alpha node, whose uptime should result in a staking reward")
		alphaNode := e2e.AddEphemeralNode(tc, network, tmpnet.NewEphemeralNode(tmpnet.FlagsMap{}))
		tc.By("adding beta node, whose uptime should not result in a staking reward")
		betaNode := e2e.AddEphemeralNode(tc, network, tmpnet.NewEphemeralNode(tmpnet.FlagsMap{}))

		// Wait to check health until both nodes have started to minimize the duration
		// required for both nodes to report healthy.
		tc.By("waiting until alpha node is healthy")
		e2e.WaitForHealthy(tc, alphaNode)
		tc.By("waiting until beta node is healthy")
		e2e.WaitForHealthy(tc, betaNode)

		tc.By("retrieving alpha node id and pop")
		alphaNodeURI := alphaNode.GetAccessibleURI()
		alphaInfoClient := info.NewClient(alphaNodeURI)
		alphaNodeID, alphaPOP, err := alphaInfoClient.GetNodeID(tc.DefaultContext())
		require.NoError(err)

		tc.By("retrieving beta node id and pop")
		betaNodeURI := betaNode.GetAccessibleURI()
		betaInfoClient := info.NewClient(betaNodeURI)
		betaNodeID, betaPOP, err := betaInfoClient.GetNodeID(tc.DefaultContext())
		require.NoError(err)

		tc.By("creating keychain and P-Chain wallet")
		var (
			alphaValidationRewardKey = e2e.NewPrivateKey(tc)
			alphaDelegationRewardKey = e2e.NewPrivateKey(tc)
			betaValidationRewardKey  = e2e.NewPrivateKey(tc)
			betaDelegationRewardKey  = e2e.NewPrivateKey(tc)
			gammaDelegationRewardKey = e2e.NewPrivateKey(tc)
			deltaDelegationRewardKey = e2e.NewPrivateKey(tc)

			rewardKeys = []*secp256k1.PrivateKey{
				alphaValidationRewardKey,
				alphaDelegationRewardKey,
				betaValidationRewardKey,
				betaDelegationRewardKey,
				gammaDelegationRewardKey,
				deltaDelegationRewardKey,
			}

			keychain = env.NewKeychain()
			nodeURI  = tmpnet.NodeURI{
				NodeID: alphaNodeID,
				URI:    alphaNodeURI,
			}
			baseWallet = e2e.NewWallet(tc, keychain, nodeURI)
			pWallet    = baseWallet.P()
			pBuilder   = pWallet.Builder()
			pContext   = pBuilder.Context()

			pvmClient = platformvm.NewClient(alphaNodeURI)
		)

		tc.By("retrieving supply before adding alpha node as a validator")
		supplyAtAlphaNodeStart, _, err := pvmClient.GetCurrentSupply(tc.DefaultContext(), constants.PrimaryNetworkID)
		require.NoError(err)

		tc.By("adding alpha node as a validator", func() {
			e2e.OutputWalletBalances(tc, baseWallet)

			endTime := time.Now().Add(targetValidationPeriod)
			tc.Log().Info("determined alpha node validation end time",
				zap.Time("time", endTime),
			)

			_, err := pWallet.IssueAddPermissionlessValidatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: alphaNodeID,
						End:    uint64(endTime.Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				alphaPOP,
				pContext.AVAXAssetID,
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{alphaValidationRewardKey.Address()},
				},
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{alphaDelegationRewardKey.Address()},
				},
				delegationShare,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		betaValidatorEndTime := time.Now().Add(targetValidationPeriod)
		tc.Log().Info("determined beta node validation end time",
			zap.Time("time", betaValidatorEndTime),
		)

		tc.By("adding beta node as a validator", func() {
			e2e.OutputWalletBalances(tc, baseWallet)

			_, err := pWallet.IssueAddPermissionlessValidatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: betaNodeID,
						End:    uint64(betaValidatorEndTime.Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				betaPOP,
				pContext.AVAXAssetID,
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{betaValidationRewardKey.Address()},
				},
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{betaDelegationRewardKey.Address()},
				},
				delegationShare,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("retrieving supply before adding gamma as a delegator")
		supplyAtGammaDelegatorStart, _, err := pvmClient.GetCurrentSupply(tc.DefaultContext(), constants.PrimaryNetworkID)
		require.NoError(err)

		tc.By("adding gamma as a delegator to the alpha node", func() {
			e2e.OutputWalletBalances(tc, baseWallet)

			endTime := time.Now().Add(targetDelegationPeriod)
			tc.Log().Info("determined gamma delegator validation end time",
				zap.Time("time", endTime),
			)

			_, err := pWallet.IssueAddPermissionlessDelegatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: alphaNodeID,
						End:    uint64(endTime.Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				pContext.AVAXAssetID,
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{gammaDelegationRewardKey.Address()},
				},
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("adding delta as delegator to the beta node", func() {
			e2e.OutputWalletBalances(tc, baseWallet)

			endTime := time.Now().Add(targetDelegationPeriod)
			tc.Log().Info("determined delta delegator delegation period end time",
				zap.Time("time", endTime),
			)

			_, err := pWallet.IssueAddPermissionlessDelegatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: betaNodeID,
						End:    uint64(endTime.Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				pContext.AVAXAssetID,
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{deltaDelegationRewardKey.Address()},
				},
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("stopping beta node to prevent it and its delegator from receiving a validation reward", func() {
			require.NoError(betaNode.Stop(tc.DefaultContext()))
		})

		tc.By("retrieving staking periods from the chain")
		currentValidators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PlatformChainID, []ids.NodeID{alphaNodeID})
		require.NoError(err)
		require.Len(currentValidators, 1)
		alphaValidator := currentValidators[0]
		require.Len(alphaValidator.Delegators, 1)
		gammaDelegator := alphaValidator.Delegators[0]
		actualAlphaValidationPeriod := time.Duration(alphaValidator.EndTime-alphaValidator.StartTime) * time.Second
		actualGammaDelegationPeriod := time.Duration(gammaDelegator.EndTime-gammaDelegator.StartTime) * time.Second

		tc.By("waiting until the alpha and beta nodes are no longer validators", func() {
			// The beta validator was the last added and so has the latest end
			// time. The delegation periods are shorter than the validation
			// periods.
			time.Sleep(time.Until(betaValidatorEndTime))

			// To avoid racy behavior, we confirm that the nodes are no longer
			// validators before advancing.
			tc.Eventually(func() bool {
				validators, err := pvmClient.GetCurrentValidators(tc.DefaultContext(), constants.PrimaryNetworkID, nil)
				require.NoError(err)
				for _, validator := range validators {
					if validator.NodeID == alphaNodeID || validator.NodeID == betaNodeID {
						return false
					}
				}
				return true
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "nodes failed to stop validating before timeout")
		})

		tc.By("checking expected rewards against actual rewards", func() {
			var (
				adminClient  = admin.NewClient(nodeURI.URI)
				rewardConfig = getRewardConfig(tc, adminClient)
				calculator   = reward.NewCalculator(rewardConfig)

				expectedValidationReward = calculator.Calculate(actualAlphaValidationPeriod, weight, supplyAtAlphaNodeStart)

				potentialDelegationReward                      = calculator.Calculate(actualGammaDelegationPeriod, weight, supplyAtGammaDelegatorStart)
				expectedDelegationFee, expectedDelegatorReward = reward.Split(potentialDelegationReward, delegationShare)
			)

			rewardBalances := make(map[ids.ShortID]uint64, len(rewardKeys))
			for _, rewardKey := range rewardKeys {
				var (
					keychain   = secp256k1fx.NewKeychain(rewardKey)
					baseWallet = e2e.NewWallet(tc, keychain, nodeURI)
					pWallet    = baseWallet.P()
					pBuilder   = pWallet.Builder()
				)

				balances, err := pBuilder.GetBalance()
				require.NoError(err)
				rewardBalances[rewardKey.Address()] = balances[pContext.AVAXAssetID]
			}

			require.Equal(
				map[ids.ShortID]uint64{
					alphaValidationRewardKey.Address(): expectedValidationReward,
					alphaDelegationRewardKey.Address(): expectedDelegationFee,
					betaValidationRewardKey.Address():  0, // Validator didn't meet uptime requirement
					betaDelegationRewardKey.Address():  0, // Validator didn't meet uptime requirement
					gammaDelegationRewardKey.Address(): expectedDelegatorReward,
					deltaDelegationRewardKey.Address(): 0, // Validator didn't meet uptime requirement
				},
				rewardBalances,
			)
		})

		tc.By("stopping alpha to free up resources for a bootstrap check", func() {
			require.NoError(alphaNode.Stop(tc.DefaultContext()))
		})

		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})

// TODO(marun) Enable GetConfig to return *node.Config directly. Currently, due
// to a circular dependency issue, a map-based equivalent is used for which
// manual unmarshaling is required.
func getRewardConfig(tc tests.TestContext, client *admin.Client) reward.Config {
	require := require.New(tc)

	rawNodeConfigMap, err := client.GetConfig(tc.DefaultContext())
	require.NoError(err)
	nodeConfigMap, ok := rawNodeConfigMap.(map[string]interface{})
	require.True(ok)
	stakingConfigMap, ok := nodeConfigMap["stakingConfig"].(map[string]interface{})
	require.True(ok)

	var rewardConfig reward.Config
	require.NoError(mapstructure.Decode(
		stakingConfigMap["rewardConfig"],
		&rewardConfig,
	))
	return rewardConfig
}
