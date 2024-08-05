// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"math/big"
	"time"

	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/spf13/cast"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = e2e.DescribePChain("[Interchain Workflow]", ginkgo.Label(e2e.UsesCChainLabel), func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	const (
		transferAmount = 10 * units.Avax
		weight         = 2_000 * units.Avax // Used for both validation and delegation
	)

	ginkgo.It("should ensure that funds can be transferred from the P-Chain to the X-Chain and the C-Chain", func() {
		env := e2e.GetEnv(tc)

		network := env.GetNetwork()

		tc.By("checking that the network has a compatible minimum stake duration", func() {
			minStakeDuration := cast.ToDuration(network.DefaultFlags[config.MinStakeDurationKey])
			require.Equal(tmpnet.DefaultMinStakeDuration, minStakeDuration)
		})

		tc.By("creating wallet with a funded key to send from and recipient key to deliver to")
		recipientKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		keychain := env.NewKeychain(1)
		keychain.Add(recipientKey)
		nodeURI := env.GetRandomNodeURI()
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)
		xWallet := baseWallet.X()
		cWallet := baseWallet.C()
		pWallet := baseWallet.P()

		xBuilder := xWallet.Builder()
		xContext := xBuilder.Context()
		pBuilder := pWallet.Builder()
		pContext := pBuilder.Context()
		cBuilder := cWallet.Builder()
		cContext := cBuilder.Context()

		tc.By("defining common configuration")
		recipientEthAddress := evm.GetEthAddress(recipientKey)
		avaxAssetID := xContext.AVAXAssetID
		// Use the same owner for sending to X-Chain and importing funds to P-Chain
		recipientOwner := secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				recipientKey.Address(),
			},
		}
		// Use the same outputs for both X-Chain and C-Chain exports
		exportOutputs := []*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: avaxAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: transferAmount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs: []ids.ShortID{
							keychain.Keys[0].Address(),
						},
					},
				},
			},
		}

		tc.By("adding new node and waiting for it to report healthy")
		node := e2e.AddEphemeralNode(tc, network, tmpnet.FlagsMap{})
		e2e.WaitForHealthy(tc, node)

		tc.By("retrieving new node's id and pop")
		infoClient := info.NewClient(node.URI)
		nodeID, nodePOP, err := infoClient.GetNodeID(tc.DefaultContext())
		require.NoError(err)

		// Adding a validator should not break interchain transfer.
		endTime := time.Now().Add(30 * time.Second)
		tc.By("adding the new node as a validator", func() {
			rewardKey, err := secp256k1.NewPrivateKey()
			require.NoError(err)

			const (
				delegationPercent = 0.10 // 10%
				delegationShare   = reward.PercentDenominator * delegationPercent
			)

			_, err = pWallet.IssueAddPermissionlessValidatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID,
						End:    uint64(endTime.Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				nodePOP,
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

		// Adding a delegator should not break interchain transfer.
		tc.By("adding a delegator to the new node", func() {
			rewardKey, err := secp256k1.NewPrivateKey()
			require.NoError(err)

			_, err = pWallet.IssueAddPermissionlessDelegatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID,
						End:    uint64(endTime.Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				pContext.AVAXAssetID,
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardKey.Address()},
				},
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("exporting AVAX from the P-Chain to the X-Chain", func() {
			_, err := pWallet.IssueExportTx(
				xContext.BlockchainID,
				exportOutputs,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("importing AVAX from the P-Chain to the X-Chain", func() {
			_, err := xWallet.IssueImportTx(
				constants.PlatformChainID,
				&recipientOwner,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("checking that the recipient address has received imported funds on the X-Chain", func() {
			balances, err := xWallet.Builder().GetFTBalance(common.WithCustomAddresses(set.Of(
				recipientKey.Address(),
			)))
			require.NoError(err)
			require.Positive(balances[avaxAssetID])
		})

		tc.By("exporting AVAX from the P-Chain to the C-Chain", func() {
			_, err := pWallet.IssueExportTx(
				cContext.BlockchainID,
				exportOutputs,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("initializing a new eth client")
		ethClient := e2e.NewEthClient(tc, nodeURI)

		tc.By("importing AVAX from the P-Chain to the C-Chain", func() {
			_, err := cWallet.IssueImportTx(
				constants.PlatformChainID,
				recipientEthAddress,
				tc.WithDefaultContext(),
				e2e.WithSuggestedGasPrice(tc, ethClient),
			)
			require.NoError(err)
		})

		tc.By("checking that the recipient address has received imported funds on the C-Chain")
		balance, err := ethClient.BalanceAt(tc.DefaultContext(), recipientEthAddress, nil)
		require.NoError(err)
		require.Positive(balance.Cmp(big.NewInt(0)))

		tc.By("stopping validator node to free up resources for a bootstrap check")
		require.NoError(node.Stop(tc.DefaultContext()))

		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})
