// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ = e2e.DescribePChain("[Continuous Staking]", func() {
	var (
		tc      = e2e.NewTestContext()
		require = require.New(tc)
	)

	ginkgo.It("should handle continuous staking lifecycle with configuration changes and graceful stop", func() {
		const (
			weight                   = 2_000 * units.Avax
			initialAutoRestakeShares = 500_000 // 50%
			updatedAutoRestakeShares = 750_000 // 75%
			initialPeriod            = 10 * time.Second
			updatedPeriod            = 12 * time.Second
			delegationShares         = 0 // No delegators in this test
		)

		// Get environment and network
		env := e2e.GetEnv(tc)
		network := env.GetNetwork()

		tc.By("checking if Helicon upgrade is activated")
		nodeURI := env.GetRandomNodeURI()
		infoClient := info.NewClient(nodeURI.URI)
		upgrades, err := infoClient.Upgrades(tc.DefaultContext())
		require.NoError(err)

		if !upgrades.IsHeliconActivated(time.Now()) {
			ginkgo.Skip("skipping test because Helicon upgrade isn't active (run with --activate-latest)")
		}

		tc.By("adding ephemeral node to network")
		node := e2e.AddEphemeralNode(tc, network, tmpnet.NewEphemeralNode(tmpnet.FlagsMap{}))
		e2e.WaitForHealthy(tc, node)

		// Get node ID and proof of possession
		ephemeralNodeURI := node.GetAccessibleURI()
		ephemeralInfoClient := info.NewClient(ephemeralNodeURI)
		nodeID, pop, err := ephemeralInfoClient.GetNodeID(tc.DefaultContext())
		require.NoError(err)

		tc.By("creating keys and wallets")
		// Create keys
		validationRewardKey := e2e.NewPrivateKey(tc)
		configOwnerKey := e2e.NewPrivateKey(tc)

		// Create wallet with config owner key for authorization
		keychain := env.NewKeychain()
		keychain.Add(configOwnerKey)

		baseWallet := e2e.NewWallet(tc, keychain, tmpnet.NodeURI{
			NodeID: nodeID,
			URI:    ephemeralNodeURI,
		})
		pWallet := baseWallet.P()
		pBuilder := pWallet.Builder()
		pContext := pBuilder.Context()
		pvmClient := platformvm.NewClient(ephemeralNodeURI)

		// Get initial balance
		initialBalances, err := pBuilder.GetBalance()
		require.NoError(err)
		initialBalance := initialBalances[pContext.AVAXAssetID]

		tc.Log().Info("initial setup complete",
			zap.Stringer("nodeID", nodeID),
			zap.Uint64("initialBalance", initialBalance),
		)

		tc.By("adding continuous validator with 50% auto-restake and 10-second period")
		addContinuousTx, err := pWallet.IssueAddContinuousValidatorTx(
			nodeID,
			weight,
			pop,
			pContext.AVAXAssetID,
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{validationRewardKey.Address()},
			},
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{validationRewardKey.Address()},
			},
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{configOwnerKey.Address()},
			},
			delegationShares,
			initialAutoRestakeShares,
			initialPeriod,
			tc.WithDefaultContext(),
		)
		require.NoError(err)

		firstCycleStartTime := time.Now()
		tc.Log().Info("continuous validator added",
			zap.Stringer("txID", addContinuousTx.ID()),
			zap.Time("cycleStartTime", firstCycleStartTime),
		)

		tc.By("waiting for first cycle to complete")
		firstCycleEndTime := firstCycleStartTime.Add(initialPeriod)
		time.Sleep(time.Until(firstCycleEndTime))
		time.Sleep(2 * time.Second) // Buffer for block processing

		tc.By("verifying validator restarted and rewards were partially restaked")
		validators, err := pvmClient.GetCurrentValidators(
			tc.DefaultContext(),
			constants.PrimaryNetworkID,
			[]ids.NodeID{nodeID},
		)
		require.NoError(err)
		require.Len(validators, 1, "validator should still be active")

		validator1 := validators[0]
		require.Greater(validator1.Weight, weight, "weight should have increased from auto-restaked rewards")

		// Check reward owner balance
		rewardKeychain := secp256k1fx.NewKeychain(validationRewardKey)
		rewardWallet := e2e.NewWallet(tc, rewardKeychain, tmpnet.NodeURI{
			NodeID: nodeID,
			URI:    ephemeralNodeURI,
		})
		rewardBalances, err := rewardWallet.P().Builder().GetBalance()
		require.NoError(err)
		require.Greater(rewardBalances[pContext.AVAXAssetID], uint64(0), "reward owner should have received withdrawn rewards")

		tc.Log().Info("first cycle completed",
			zap.Uint64("initialWeight", weight),
			zap.Uint64("newWeight", validator1.Weight),
			zap.Uint64("rewardBalance", rewardBalances[pContext.AVAXAssetID]),
		)

		tc.By("updating configuration to 75% auto-restake and 12-second period")
		setConfigTx, err := pWallet.IssueSetAutoRestakeConfigTx(
			addContinuousTx.ID(),
			updatedAutoRestakeShares,
			updatedPeriod,
			tc.WithDefaultContext(),
		)
		require.NoError(err)

		tc.Log().Info("configuration update issued",
			zap.Stringer("txID", setConfigTx.ID()),
		)

		tc.By("waiting for current cycle to complete with old config")
		secondCycleEndTime := firstCycleEndTime.Add(initialPeriod)
		time.Sleep(time.Until(secondCycleEndTime))
		time.Sleep(2 * time.Second)

		tc.By("waiting for new cycle to complete with updated config")
		thirdCycleEndTime := secondCycleEndTime.Add(updatedPeriod)
		time.Sleep(time.Until(thirdCycleEndTime))
		time.Sleep(2 * time.Second)

		tc.By("verifying validator continued with updated configuration")
		validators2, err := pvmClient.GetCurrentValidators(
			tc.DefaultContext(),
			constants.PrimaryNetworkID,
			[]ids.NodeID{nodeID},
		)
		require.NoError(err)
		require.Len(validators2, 1, "validator should still be active")

		validator2 := validators2[0]
		require.Greater(validator2.Weight, validator1.Weight, "weight should have increased further")

		// Verify new period is in effect
		actualPeriod := time.Duration(validator2.EndTime-validator2.StartTime) * time.Second
		require.InDelta(updatedPeriod.Seconds(), actualPeriod.Seconds(), 2.0, "period should reflect update")

		// Check reward balance increased
		rewardBalances2, err := rewardWallet.P().Builder().GetBalance()
		require.NoError(err)
		require.Greater(rewardBalances2[pContext.AVAXAssetID], rewardBalances[pContext.AVAXAssetID], "reward owner should have received more rewards")

		tc.Log().Info("second cycle completed with updated config",
			zap.Uint64("previousWeight", validator1.Weight),
			zap.Uint64("newWeight", validator2.Weight),
			zap.Uint64("previousRewardBalance", rewardBalances[pContext.AVAXAssetID]),
			zap.Uint64("newRewardBalance", rewardBalances2[pContext.AVAXAssetID]),
		)

		tc.By("setting period to 0 to stop continuous staking")
		stopTx, err := pWallet.IssueSetAutoRestakeConfigTx(
			addContinuousTx.ID(),
			0, // AutoRestakeShares doesn't matter when stopping
			0, // Period = 0 signals stop
			tc.WithDefaultContext(),
		)
		require.NoError(err)

		tc.Log().Info("stop signal issued",
			zap.Stringer("txID", stopTx.ID()),
		)

		tc.By("waiting for final cycle to complete")
		finalCycleEndTime := thirdCycleEndTime.Add(updatedPeriod)
		time.Sleep(time.Until(finalCycleEndTime))
		time.Sleep(2 * time.Second)

		tc.By("verifying validator has stopped")
		tc.Eventually(func() bool {
			validators, err := pvmClient.GetCurrentValidators(
				tc.DefaultContext(),
				constants.PrimaryNetworkID,
				[]ids.NodeID{nodeID},
			)
			require.NoError(err)
			return len(validators) == 0
		}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "validator failed to stop before timeout")

		tc.By("verifying all stake and rewards were returned")
		finalRewardBalances, err := rewardWallet.P().Builder().GetBalance()
		require.NoError(err)

		// Balance should have increased significantly from:
		// 1. Final cycle rewards
		// 2. All previously auto-restaked amounts
		// 3. Original stake
		require.Greater(
			finalRewardBalances[pContext.AVAXAssetID],
			rewardBalances2[pContext.AVAXAssetID]+weight,
			"final balance should include stake plus all rewards",
		)

		tc.Log().Info("validator stopped and funds returned",
			zap.Uint64("finalRewardBalance", finalRewardBalances[pContext.AVAXAssetID]),
			zap.Uint64("totalReceived", finalRewardBalances[pContext.AVAXAssetID]),
		)

		tc.By("stopping node to free resources")
		require.NoError(node.Stop(tc.DefaultContext()))

		tc.By("verifying network can still bootstrap")
		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})
