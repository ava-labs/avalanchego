// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"

	pwallet "github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
)

// autoRenewedValidatorFixture bundles the state shared by the auto-renewed validator specs.
type autoRenewedValidatorFixture struct {
	tc *e2e.GinkgoTestContext

	validatorNode       *tmpnet.Node
	randomWalletNodeURI tmpnet.NodeURI

	// fundingPWallet is backed by a pre-funded key and is the source of all
	// funding transfers.
	fundingPWallet pwallet.Wallet

	// validatorFundingKey pays for the validator's stake, its tx fees
	// and receives the stake back when the validator exits.
	validatorFundingKey *secp256k1.PrivateKey

	// validationRewardKey receives the withdrawn share of the validation
	// rewards, plus any accrued (compounded) rewards when the validator
	// exits.
	validationRewardKey *secp256k1.PrivateKey

	// delegationRewardKey receives the withdrawn share of the delegation fee
	// (delegatee) rewards, plus any accrued (compounded) rewards when the
	// validator exits.
	delegationRewardKey *secp256k1.PrivateKey
}

// newAutoRenewedValidatorFixture adds an ephemeral node to the network,
// creates the validator's keys and owners, and funds the validator wallet
// with fundingAmount from a pre-funded key. The wallet and clients target a
// pre-existing network node so that specs remain free to stop the ephemeral
// node mid-test.
func newAutoRenewedValidatorFixture(
	tc *e2e.GinkgoTestContext,
	env *e2e.TestEnvironment,
	fundingAmount uint64,
) *autoRenewedValidatorFixture {
	tc.By("adding an ephemeral node")
	node := e2e.AddEphemeralNode(tc, env.GetNetwork(), tmpnet.NewEphemeralNode(tmpnet.FlagsMap{}))

	tc.By("waiting until node is healthy")
	e2e.WaitForHealthy(tc, node)

	tc.By("creating keys and wallets")
	walletNodeURI := env.GetRandomNodeURI()

	var (
		validationRewardKey = e2e.NewPrivateKey(tc)
		delegationRewardKey = e2e.NewPrivateKey(tc)
		validatorFundingKey = e2e.NewPrivateKey(tc)

		fundingPWallet = e2e.NewWallet(tc, env.NewKeychain(), walletNodeURI).P()
	)

	f := &autoRenewedValidatorFixture{
		tc:                  tc,
		validatorNode:       node,
		randomWalletNodeURI: walletNodeURI,
		fundingPWallet:      fundingPWallet,

		validatorFundingKey: validatorFundingKey,
		validationRewardKey: validationRewardKey,
		delegationRewardKey: delegationRewardKey,
	}

	tc.By("funding validator wallet", func() {
		f.fundKey(validatorFundingKey, fundingAmount)
	})

	return f
}

// addValidatorAndCheckSupplyMint issues an AddAutoRenewedValidatorTx for the fixture's validatorNode and
// asserts that the supply was increased by the validator's potential reward.
// It returns the tx id, the potential reward, and the supply captured before
// issuance.
func (f *autoRenewedValidatorFixture) addValidatorAndCheckSupplyMint(
	weight uint64,
	delegationShares uint32,
	autoCompoundRewardShares uint32,
	period time.Duration,
) (txID ids.ID, potentialReward uint64, supply uint64) {
	pvmClient := platformvm.NewClient(f.randomWalletNodeURI.URI)
	calculator := reward.NewCalculator(GetRewardConfig(f.tc, admin.NewClient(f.randomWalletNodeURI.URI)))

	supplyBefore := currentSupply(f.tc, pvmClient)

	// A fresh wallet reflects the current UTXO set, including any stake
	// returned by a previous exit of the fixture's validator.
	wallet := e2e.NewWallet(f.tc, secp256k1fx.NewKeychain(f.validatorFundingKey), f.randomWalletNodeURI).P()

	nodeID, nodePOP, err := info.NewClient(f.validatorNode.GetAccessibleURI()).GetNodeID(f.tc.DefaultContext())
	require.NoError(f.tc, err)

	validationRewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{f.validationRewardKey.Address()},
	}
	delegationRewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{f.delegationRewardKey.Address()},
	}
	configOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{f.validatorFundingKey.Address()},
	}

	tx, err := wallet.IssueAddAutoRenewedValidatorTx(
		nodeID,
		weight,
		nodePOP,
		validationRewardsOwner,
		delegationRewardsOwner,
		configOwner,
		delegationShares,
		autoCompoundRewardShares,
		period,
		f.tc.WithDefaultContext(),
	)
	require.NoError(f.tc, err)

	potentialReward = calculator.Calculate(period, weight, supplyBefore)
	require.Equal(f.tc, supplyBefore+potentialReward, currentSupply(f.tc, pvmClient))

	return tx.ID(), potentialReward, supplyBefore
}

// setValidatorConfig issues a SetAutoRenewedValidatorConfigTx for txID using a
// wallet authorized for the validator's config owner.
func (f *autoRenewedValidatorFixture) setValidatorConfig(
	txID ids.ID,
	autoCompoundRewardShares uint32,
	period time.Duration,
) {
	wallet := e2e.NewWalletWithConfig(
		f.tc,
		secp256k1fx.NewKeychain(f.validatorFundingKey),
		f.randomWalletNodeURI,
		primary.WalletConfig{AutoRenewedValidatorTxIDs: []ids.ID{txID}},
	).P()

	_, err := wallet.IssueSetAutoRenewedValidatorConfigTx(
		txID,
		autoCompoundRewardShares,
		period,
		f.tc.WithDefaultContext(),
	)
	require.NoError(f.tc, err)
}

// fundKey transfers amount from the pre-funded wallet to key.
func (f *autoRenewedValidatorFixture) fundKey(key *secp256k1.PrivateKey, amount uint64) {
	_, err := f.fundingPWallet.IssueBaseTx(
		[]*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: f.fundingPWallet.Builder().Context().AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{key.Address()},
					},
				},
			},
		},
		f.tc.WithDefaultContext(),
	)
	require.NoError(f.tc, err)
}

// addDelegator delegates weight to the fixture's validator until endTime,
// funded by fundingKey and rewarding rewardKey. It returns the supply captured
// before issuance for use as a reward calculator input.
func (f *autoRenewedValidatorFixture) addDelegator(
	fundingKey, rewardKey *secp256k1.PrivateKey,
	weight, endTime uint64,
) uint64 {
	supplyBefore := currentSupply(f.tc, platformvm.NewClient(f.randomWalletNodeURI.URI))

	wallet := e2e.NewWallet(f.tc, secp256k1fx.NewKeychain(fundingKey), f.randomWalletNodeURI).P()

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{rewardKey.Address()},
	}
	_, err := wallet.IssueAddPermissionlessDelegatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: f.validatorNode.NodeID,
				End:    endTime,
				Wght:   weight,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		wallet.Builder().Context().AVAXAssetID,
		rewardsOwner,
		f.tc.WithDefaultContext(),
	)
	require.NoError(f.tc, err)

	return supplyBefore
}

// currentValidator returns the current API representation of the primary
// network validator with nodeID, requiring that it exists.
func currentValidator(
	tc *e2e.GinkgoTestContext,
	pvmClient *platformvm.Client,
	nodeID ids.NodeID,
) platformvm.ClientPermissionlessValidator {
	validators, err := pvmClient.GetCurrentValidators(
		tc.DefaultContext(),
		constants.PrimaryNetworkID,
		[]ids.NodeID{nodeID},
	)
	require.NoError(tc, err)
	require.Len(tc, validators, 1)
	return validators[0]
}

// waitForOneActiveDelegator waits until the validator with nodeID has exactly one
// active delegator and returns the delegator's actual staking period.
func waitForOneActiveDelegator(
	tc *e2e.GinkgoTestContext,
	pvmClient *platformvm.Client,
	nodeID ids.NodeID,
) time.Duration {
	tc.Eventually(func() bool {
		validators, err := pvmClient.GetCurrentValidators(
			tc.DefaultContext(),
			constants.PrimaryNetworkID,
			[]ids.NodeID{nodeID},
		)
		require.NoError(tc, err)
		return len(validators) == 1 && len(validators[0].Delegators) == 1
	}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "no active delegators")

	validator := currentValidator(tc, pvmClient, nodeID)
	require.Len(tc, validator.Delegators, 1)
	delegator := validator.Delegators[0]
	return time.Duration(delegator.EndTime-delegator.StartTime) * time.Second
}

// requireValidatorRemoved waits until the validator with nodeID is no longer
// in the current validator set.
func requireValidatorRemoved(tc *e2e.GinkgoTestContext, pvmClient *platformvm.Client, nodeID ids.NodeID, msg string) {
	tc.Eventually(func() bool {
		validators, err := pvmClient.GetCurrentValidators(
			tc.DefaultContext(),
			constants.PrimaryNetworkID,
			[]ids.NodeID{nodeID},
		)
		require.NoError(tc, err)
		return len(validators) == 0
	}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, msg)
}

func waitForAutoRenewedCycleEnd(tc *e2e.GinkgoTestContext, pvmClient *platformvm.Client, nodeID ids.NodeID) {
	validator := currentValidator(tc, pvmClient, nodeID)
	initialStartTime := validator.StartTime

	tc.Eventually(func() bool {
		validators, err := pvmClient.GetCurrentValidators(
			tc.DefaultContext(),
			constants.PrimaryNetworkID,
			[]ids.NodeID{nodeID},
		)
		require.NoError(tc, err)
		if len(validators) == 0 {
			return true
		}
		require.Len(tc, validators, 1)

		// A renewal re-adds the validator with its start time set to the
		// previous cycle's end time, so a start time change marks a new cycle start.
		return validators[0].StartTime != initialStartTime
	}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "node failed to finish staking cycle")
}
