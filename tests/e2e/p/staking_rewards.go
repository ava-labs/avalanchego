// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"math"
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
			upgrades, err := alphaInfoClient.Upgrades(tc.DefaultContext())
			require.NoError(err)

			var (
				adminClient  = admin.NewClient(nodeURI.URI)
				rewardConfig = GetRewardConfig(tc, adminClient)
				calculator   = reward.NewPrimaryNetworkCalculator(rewardConfig, *upgrades)
			)

			// ACP-285 selects the primary-network rate from each staker's start time.
			var (
				expectedValidationReward = calculator.Calculate(
					time.Unix(int64(alphaValidator.StartTime), 0),
					actualAlphaValidationPeriod,
					weight,
					supplyAtAlphaNodeStart,
				)

				potentialDelegationReward = calculator.Calculate(
					time.Unix(int64(gammaDelegator.StartTime), 0),
					actualGammaDelegationPeriod,
					weight,
					supplyAtGammaDelegatorStart,
				)
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

	ginkgo.It("should require 90 percent uptime for primary validators after Helicon", func() {
		env := e2e.GetEnv(tc)
		network := env.GetNetwork()
		upgradeConfig, err := info.NewClient(env.GetRandomNodeURI().URI).Upgrades(tc.DefaultContext())
		require.NoError(err)
		if !upgradeConfig.IsHeliconActivated(time.Now()) {
			ginkgo.Skip("Helicon is not active")
		}

		tc.By("adding an ephemeral node to use as the short-lived validator")
		testNode := e2e.AddEphemeralNode(tc, network, tmpnet.NewEphemeralNode(tmpnet.FlagsMap{}))
		e2e.WaitForHealthy(tc, testNode)

		tc.By("retrieving the validator node ID and proof of possession")
		testNodeURI := testNode.GetAccessibleURI()
		testInfoClient := info.NewClient(testNodeURI)
		testNodeID, testPOP, err := testInfoClient.GetNodeID(tc.DefaultContext())
		require.NoError(err)

		tc.By("creating the staking wallet and reward key")
		nodeURI := env.GetRandomNodeURI()
		rewardKey := e2e.NewPrivateKey(tc)
		keychain := env.NewKeychain()
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)
		pWallet := baseWallet.P()
		pContext := pWallet.Builder().Context()

		tc.By("adding the ephemeral node as a primary validator", func() {
			const (
				targetValidationPeriod = 3 * time.Minute
				delegationPercent      = 0.10
				delegationShare        = reward.PercentDenominator * delegationPercent
				weight                 = 2_000 * units.Avax
			)

			e2e.OutputWalletBalances(tc, baseWallet)

			endTime := time.Now().Add(targetValidationPeriod)
			_, err := pWallet.IssueAddPermissionlessValidatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: testNodeID,
						End:    uint64(endTime.Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				testPOP,
				pContext.AVAXAssetID,
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardKey.Address()},
				},
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardKey.Address()},
				},
				delegationShare,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("waiting for the ephemeral node to enter the current validator set")
		pvmClient := platformvm.NewClient(nodeURI.URI)
		testValidator := waitForCurrentValidator(tc, pvmClient, testNodeID)
		actualStartTime := time.Unix(int64(testValidator.StartTime), 0)
		actualEndTime := time.Unix(int64(testValidator.EndTime), 0)
		actualValidationPeriod := actualEndTime.Sub(actualStartTime)
		require.Positive(actualValidationPeriod)

		tc.By("taking the validator offline long enough to finish between 80 and 90 percent uptime", func() {
			const targetOfflinePercent = .12
			offlineDuration := time.Duration(targetOfflinePercent * float64(actualValidationPeriod))
			require.Positive(offlineDuration)

			require.NoError(testNode.Stop(tc.DefaultContext()))
			time.Sleep(offlineDuration)
			require.NoError(testNode.Restart(tc.DefaultContext()))
			e2e.WaitForHealthy(tc, testNode)
			testInfoClient = info.NewClient(testNode.GetAccessibleURI())
		})

		tc.By("checking that the network does not consider 80 to 90 percent uptime rewarding", func() {
			currentValidators, err := pvmClient.GetCurrentValidators(
				tc.DefaultContext(),
				constants.PrimaryNetworkID,
				nil,
			)
			require.NoError(err)

			validatorWeights := make(map[ids.NodeID]uint64, len(currentValidators))
			var totalWeight uint64
			for _, validator := range currentValidators {
				weight := validator.Weight
				if validator.DelegatorWeight != nil {
					weight += *validator.DelegatorWeight
				}
				validatorWeights[validator.NodeID] = weight
				totalWeight += weight
			}
			testWeight, ok := validatorWeights[testNodeID]
			require.True(ok)
			wantRewardingStakePercentage := 100 * float64(testWeight) / float64(totalWeight)

			var gotRewardingStakePercentage float64
			tc.Eventually(func() bool {
				nodeUptime, err := testInfoClient.Uptime(tc.DefaultContext())
				if err != nil {
					return false
				}
				gotRewardingStakePercentage = float64(nodeUptime.RewardingStakePercentage)
				return math.Abs(gotRewardingStakePercentage-wantRewardingStakePercentage) < 1e-6
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "network did not apply the ACP-267 uptime requirement")

			require.InDelta(wantRewardingStakePercentage, gotRewardingStakePercentage, 1e-6)
		})

		tc.By("waiting for the validator period to end", func() {
			time.Sleep(time.Until(actualEndTime))
			tc.Eventually(func() bool {
				_, ok := getCurrentValidator(tc, pvmClient, testNodeID)
				return !ok
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "validator remained in the current validator set after its end time")
		})

		tc.By("checking that the reward address received no P-Chain reward", func() {
			rewardKeychain := secp256k1fx.NewKeychain(rewardKey)
			rewardWallet := e2e.NewWallet(tc, rewardKeychain, nodeURI)
			balances, err := rewardWallet.P().Builder().GetBalance()
			require.NoError(err)
			require.Zero(balances[pContext.AVAXAssetID])
		})
	})

	ginkgo.It("should reward a fully online primary validator after Helicon", func() {
		env := e2e.GetEnv(tc)
		network := env.GetNetwork()
		upgradeConfig, err := info.NewClient(env.GetRandomNodeURI().URI).Upgrades(tc.DefaultContext())
		require.NoError(err)
		if !upgradeConfig.IsHeliconActivated(time.Now()) {
			ginkgo.Skip("Helicon is not active")
		}

		tc.By("adding an ephemeral node to use as the validator")
		testNode := e2e.AddEphemeralNode(tc, network, tmpnet.NewEphemeralNode(tmpnet.FlagsMap{}))
		e2e.WaitForHealthy(tc, testNode)

		tc.By("retrieving the validator node ID and proof of possession")
		testInfoClient := info.NewClient(testNode.GetAccessibleURI())
		testNodeID, testPOP, err := testInfoClient.GetNodeID(tc.DefaultContext())
		require.NoError(err)

		tc.By("creating the staking wallet and reward key")
		nodeURI := env.GetRandomNodeURI()
		rewardKey := e2e.NewPrivateKey(tc)
		keychain := env.NewKeychain()
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)
		pWallet := baseWallet.P()
		pContext := pWallet.Builder().Context()

		tc.By("adding the ephemeral node as a primary validator", func() {
			const (
				targetValidationPeriod = 3 * time.Minute
				delegationPercent      = 0.10
				delegationShare        = reward.PercentDenominator * delegationPercent
				weight                 = 2_000 * units.Avax
			)

			e2e.OutputWalletBalances(tc, baseWallet)

			endTime := time.Now().Add(targetValidationPeriod)
			_, err := pWallet.IssueAddPermissionlessValidatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: testNodeID,
						End:    uint64(endTime.Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				testPOP,
				pContext.AVAXAssetID,
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardKey.Address()},
				},
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardKey.Address()},
				},
				delegationShare,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("waiting for the ephemeral node to enter the current validator set")
		pvmClient := platformvm.NewClient(nodeURI.URI)
		testValidator := waitForCurrentValidator(tc, pvmClient, testNodeID)
		actualEndTime := time.Unix(int64(testValidator.EndTime), 0)

		currentValidators, err := pvmClient.GetCurrentValidators(
			tc.DefaultContext(),
			constants.PrimaryNetworkID,
			nil,
		)
		require.NoError(err)

		var (
			testWeight  uint64
			totalWeight uint64
		)
		for _, validator := range currentValidators {
			weight := validator.Weight
			if validator.DelegatorWeight != nil {
				weight += *validator.DelegatorWeight
			}
			totalWeight += weight
			if validator.NodeID == testNodeID {
				testWeight = weight
			}
		}
		require.Positive(testWeight)
		wantLocalStakePercentage := 100 * float64(testWeight) / float64(totalWeight)

		tc.By("checking that the network considers a fully online validator rewarding", func() {
			var gotRewardingStakePercentage float64
			tc.Eventually(func() bool {
				nodeUptime, err := testInfoClient.Uptime(tc.DefaultContext())
				if err != nil {
					return false
				}
				gotRewardingStakePercentage = float64(nodeUptime.RewardingStakePercentage)
				weightedAveragePercentage := float64(nodeUptime.WeightedAveragePercentage)
				return weightedAveragePercentage >= 90 && gotRewardingStakePercentage > wantLocalStakePercentage
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "network did not count 90 percent uptime as rewarding")

			require.Greater(gotRewardingStakePercentage, wantLocalStakePercentage)
		})

		tc.By("waiting for the validator period to end", func() {
			time.Sleep(time.Until(actualEndTime))
			tc.Eventually(func() bool {
				_, ok := getCurrentValidator(tc, pvmClient, testNodeID)
				return !ok
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "validator remained in the current validator set after its end time")
		})

		tc.By("checking that the reward address received a P-Chain reward", func() {
			rewardKeychain := secp256k1fx.NewKeychain(rewardKey)
			rewardWallet := e2e.NewWallet(tc, rewardKeychain, nodeURI)
			balances, err := rewardWallet.P().Builder().GetBalance()
			require.NoError(err)
			require.Positive(balances[pContext.AVAXAssetID])
		})
	})
})

// TODO(marun) Enable GetConfig to return *node.Config directly. Currently, due
// to a circular dependency issue, a map-based equivalent is used for which
// manual unmarshaling is required.
func GetRewardConfig(tc tests.TestContext, client *admin.Client) reward.Config {
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

func waitForCurrentValidator(
	tc tests.TestContext,
	client *platformvm.Client,
	nodeID ids.NodeID,
) platformvm.ClientPermissionlessValidator {
	var currentValidator platformvm.ClientPermissionlessValidator
	tc.Eventually(func() bool {
		var ok bool
		currentValidator, ok = getCurrentValidator(tc, client, nodeID)
		return ok
	}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "validator did not enter the current validator set")
	return currentValidator
}

func getCurrentValidator(
	tc tests.TestContext,
	client *platformvm.Client,
	nodeID ids.NodeID,
) (platformvm.ClientPermissionlessValidator, bool) {
	validators, err := client.GetCurrentValidators(
		tc.DefaultContext(),
		constants.PrimaryNetworkID,
		[]ids.NodeID{nodeID},
	)
	if err != nil || len(validators) != 1 {
		return platformvm.ClientPermissionlessValidator{}, false
	}
	return validators[0], true
}
