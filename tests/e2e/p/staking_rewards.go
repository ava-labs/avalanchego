// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/mitchellh/mapstructure"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/spf13/cast"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/ephnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	delegationPeriod = 15 * time.Second
	validationPeriod = 30 * time.Second
)

var _ = ginkgo.Describe("[Staking Rewards]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("should ensure that validator node uptime determines whether a staking reward is issued", func() {
		network := e2e.Env.GetNetwork()

		ginkgo.By("checking that the network has a compatible minimum stake duration", func() {
			minStakeDuration := cast.ToDuration(network.GetConfig().DefaultFlags[config.MinStakeDurationKey])
			require.Equal(ephnet.DefaultMinStakeDuration, minStakeDuration)
		})

		ginkgo.By("adding alpha node, whose uptime should result in a staking reward")
		alphaNode := e2e.AddEphemeralNode(network, ephnet.FlagsMap{})
		ginkgo.By("adding beta node, whose uptime should not result in a staking reward")
		betaNode := e2e.AddEphemeralNode(network, ephnet.FlagsMap{})

		// Wait to check health until both nodes have started to minimize the duration
		// required for both nodes to report healthy.
		ginkgo.By("waiting until alpha node is healthy")
		e2e.WaitForHealthy(alphaNode)
		ginkgo.By("waiting until beta node is healthy")
		e2e.WaitForHealthy(betaNode)

		ginkgo.By("generating reward keys")

		alphaValidationRewardKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		alphaDelegationRewardKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)

		betaValidationRewardKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		betaDelegationRewardKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)

		gammaDelegationRewardKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)

		deltaDelegationRewardKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)

		rewardKeys := []*secp256k1.PrivateKey{
			alphaValidationRewardKey,
			alphaDelegationRewardKey,
			betaValidationRewardKey,
			betaDelegationRewardKey,
			gammaDelegationRewardKey,
			deltaDelegationRewardKey,
		}

		ginkgo.By("creating keychain and P-Chain wallet")
		keychain := secp256k1fx.NewKeychain(rewardKeys...)
		fundedKey := e2e.Env.AllocateFundedKey()
		keychain.Add(fundedKey)
		nodeURI := e2e.Env.GetRandomNodeURI()
		baseWallet := e2e.NewWallet(keychain, nodeURI)
		pWallet := baseWallet.P()

		ginkgo.By("retrieving alpha node id and pop")
		alphaInfoClient := info.NewClient(alphaNode.GetProcessContext().URI)
		alphaNodeID, alphaPOP, err := alphaInfoClient.GetNodeID(e2e.DefaultContext())
		require.NoError(err)

		ginkgo.By("retrieving beta node id and pop")
		betaInfoClient := info.NewClient(betaNode.GetProcessContext().URI)
		betaNodeID, betaPOP, err := betaInfoClient.GetNodeID(e2e.DefaultContext())
		require.NoError(err)

		const (
			delegationPercent = 0.10 // 10%
			delegationShare   = reward.PercentDenominator * delegationPercent
			weight            = 2_000 * units.Avax
		)

		alphaValidatorStartTime := time.Now().Add(e2e.DefaultValidatorStartTimeDiff)
		alphaValidatorEndTime := alphaValidatorStartTime.Add(validationPeriod)
		tests.Outf("alpha node validation period starting at: %v\n", alphaValidatorStartTime)

		ginkgo.By("adding alpha node as a validator", func() {
			_, err := pWallet.IssueAddPermissionlessValidatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: alphaNodeID,
						Start:  uint64(alphaValidatorStartTime.Unix()),
						End:    uint64(alphaValidatorEndTime.Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				alphaPOP,
				pWallet.AVAXAssetID(),
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{alphaValidationRewardKey.Address()},
				},
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{alphaDelegationRewardKey.Address()},
				},
				delegationShare,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		betaValidatorStartTime := time.Now().Add(e2e.DefaultValidatorStartTimeDiff)
		betaValidatorEndTime := betaValidatorStartTime.Add(validationPeriod)
		tests.Outf("beta node validation period starting at: %v\n", betaValidatorStartTime)

		ginkgo.By("adding beta node as a validator", func() {
			_, err := pWallet.IssueAddPermissionlessValidatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: betaNodeID,
						Start:  uint64(betaValidatorStartTime.Unix()),
						End:    uint64(betaValidatorEndTime.Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				betaPOP,
				pWallet.AVAXAssetID(),
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{betaValidationRewardKey.Address()},
				},
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{betaDelegationRewardKey.Address()},
				},
				delegationShare,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		gammaDelegatorStartTime := time.Now().Add(e2e.DefaultValidatorStartTimeDiff)
		tests.Outf("gamma delegation period starting at: %v\n", gammaDelegatorStartTime)

		ginkgo.By("adding gamma as delegator to the alpha node", func() {
			_, err := pWallet.IssueAddPermissionlessDelegatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: alphaNodeID,
						Start:  uint64(gammaDelegatorStartTime.Unix()),
						End:    uint64(gammaDelegatorStartTime.Add(delegationPeriod).Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				pWallet.AVAXAssetID(),
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{gammaDelegationRewardKey.Address()},
				},
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		deltaDelegatorStartTime := time.Now().Add(e2e.DefaultValidatorStartTimeDiff)
		tests.Outf("delta delegation period starting at: %v\n", deltaDelegatorStartTime)

		ginkgo.By("adding delta as delegator to the beta node", func() {
			_, err := pWallet.IssueAddPermissionlessDelegatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: betaNodeID,
						Start:  uint64(deltaDelegatorStartTime.Unix()),
						End:    uint64(deltaDelegatorStartTime.Add(delegationPeriod).Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				pWallet.AVAXAssetID(),
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{deltaDelegationRewardKey.Address()},
				},
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("stopping beta node to prevent it and its delegator from receiving a validation reward")
		require.NoError(betaNode.Stop())

		ginkgo.By("waiting until all validation periods are over")
		// The beta validator was the last added and so has the latest end time. The
		// delegation periods are shorter than the validation periods.
		time.Sleep(time.Until(betaValidatorEndTime))

		pvmClient := platformvm.NewClient(alphaNode.GetProcessContext().URI)

		ginkgo.By("waiting until the alpha and beta nodes are no longer validators")
		e2e.Eventually(func() bool {
			validators, err := pvmClient.GetCurrentValidators(e2e.DefaultContext(), constants.PrimaryNetworkID, nil)
			require.NoError(err)
			for _, validator := range validators {
				if validator.NodeID == alphaNodeID || validator.NodeID == betaNodeID {
					return false
				}
			}
			return true
		}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "nodes failed to stop validating before timeout ")

		ginkgo.By("retrieving reward configuration for the network")
		// TODO(marun) Enable GetConfig to return *node.Config
		// directly. Currently, due to a circular dependency issue, a
		// map-based equivalent is used for which manual unmarshaling
		// is required.
		adminClient := admin.NewClient(e2e.Env.GetRandomNodeURI().URI)
		rawNodeConfigMap, err := adminClient.GetConfig(e2e.DefaultContext())
		require.NoError(err)
		nodeConfigMap, ok := rawNodeConfigMap.(map[string]interface{})
		require.True(ok)
		stakingConfigMap, ok := nodeConfigMap["stakingConfig"].(map[string]interface{})
		require.True(ok)
		rawRewardConfig := stakingConfigMap["rewardConfig"]
		rewardConfig := reward.Config{}
		require.NoError(mapstructure.Decode(rawRewardConfig, &rewardConfig))

		ginkgo.By("retrieving reward address balances")
		rewardBalances := make(map[ids.ShortID]uint64, len(rewardKeys))
		for _, rewardKey := range rewardKeys {
			keychain := secp256k1fx.NewKeychain(rewardKey)
			baseWallet := e2e.NewWallet(keychain, nodeURI)
			pWallet := baseWallet.P()
			balances, err := pWallet.Builder().GetBalance()
			require.NoError(err)
			rewardBalances[rewardKey.Address()] = balances[pWallet.AVAXAssetID()]
		}
		require.Len(rewardBalances, len(rewardKeys))

		ginkgo.By("determining expected validation and delegation rewards")
		currentSupply, _, err := pvmClient.GetCurrentSupply(e2e.DefaultContext(), constants.PrimaryNetworkID)
		require.NoError(err)
		calculator := reward.NewCalculator(rewardConfig)
		expectedValidationReward := calculator.Calculate(validationPeriod, weight, currentSupply)
		potentialDelegationReward := calculator.Calculate(delegationPeriod, weight, currentSupply)
		expectedDelegationFee, expectedDelegatorReward := reward.Split(potentialDelegationReward, delegationShare)

		ginkgo.By("checking expected rewards against actual rewards")
		expectedRewardBalances := map[ids.ShortID]uint64{
			alphaValidationRewardKey.Address(): expectedValidationReward,
			alphaDelegationRewardKey.Address(): expectedDelegationFee,
			betaValidationRewardKey.Address():  0, // Validator didn't meet uptime requirement
			betaDelegationRewardKey.Address():  0, // Validator didn't meet uptime requirement
			gammaDelegationRewardKey.Address(): expectedDelegatorReward,
			deltaDelegationRewardKey.Address(): 0, // Validator didn't meet uptime requirement
		}
		for address := range expectedRewardBalances {
			require.Equal(expectedRewardBalances[address], rewardBalances[address])
		}

		ginkgo.By("stopping alpha to free up resources for a bootstrap check")
		require.NoError(alphaNode.Stop())

		e2e.CheckBootstrapIsPossible(network)
	})
})
