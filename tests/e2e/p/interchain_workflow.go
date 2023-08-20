// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"math/big"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/plugin/evm"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ = e2e.DescribeXChain("[Interchain Workflow]", func() {
	require := require.New(ginkgo.GinkgoT())

	const (
		transferAmount = 10 * units.Avax
		weight         = 2_000 * units.Avax // Used for both validation and delegation
	)

	ginkgo.It("should ensure that funds can be transferred from the P-Chain to the X-Chain and the C-Chain", func() {
		ginkgo.By("creating wallet with a funded key to send from and recipient key to deliver to")
		factory := secp256k1.Factory{}
		recipientKey, err := factory.NewPrivateKey()
		require.NoError(err)
		keychain := e2e.Env.NewKeychain(1)
		keychain.Add(recipientKey)
		nodeURI := e2e.Env.GetRandomNodeURI()
		baseWallet := e2e.Env.NewWallet(keychain, nodeURI)
		xWallet := baseWallet.X()
		cWallet := baseWallet.C()
		pWallet := baseWallet.P()

		ginkgo.By("defining common configuration")
		recipientEthAddress := evm.GetEthAddress(recipientKey)
		avaxAssetID := xWallet.AVAXAssetID()
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

		ginkgo.By("adding new node and waiting for it to report healthy")
		network := e2e.Env.GetNetwork()
		node := e2e.AddEphemeralNode(network, testnet.FlagsMap{})
		e2e.WaitForHealthy(node)

		ginkgo.By("retrieving new node's id and pop")
		infoClient := info.NewClient(node.GetProcessContext().URI)
		nodeID, nodePOP, err := infoClient.GetNodeID(e2e.DefaultContext())
		require.NoError(err)

		ginkgo.By("adding the new node as a validator", func() {
			startTime := time.Now().Add(e2e.DefaultValidatorStartTimeDiff)
			// Validation duration doesn't actually matter to this
			// test - it is only ensuring that adding a validator
			// doesn't break interchain transfer.
			endTime := startTime.Add(30 * time.Second)

			rewardKey, err := factory.NewPrivateKey()
			require.NoError(err)

			delegationPercent := 0.10 // 10%
			delegationFee := uint32(reward.PercentDenominator * delegationPercent)

			_, err = pWallet.IssueAddPermissionlessValidatorTx(
				&txs.SubnetValidator{Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(startTime.Unix()),
					End:    uint64(endTime.Unix()),
					Wght:   weight,
				}},
				nodePOP,
				pWallet.AVAXAssetID(),
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardKey.Address()},
				},
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardKey.Address()},
				},
				delegationFee,
			)
			require.NoError(err)
		})

		ginkgo.By("adding a delegator to the new node", func() {
			startTime := time.Now().Add(e2e.DefaultValidatorStartTimeDiff)
			// Delegation duration doesn't actually matter to this
			// test - it is only ensuring that adding a delegator
			// doesn't break interchain transfer.
			endTime := startTime.Add(15 * time.Second)

			rewardKey, err := factory.NewPrivateKey()
			require.NoError(err)

			_, err = pWallet.IssueAddPermissionlessDelegatorTx(
				&txs.SubnetValidator{Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(startTime.Unix()),
					End:    uint64(endTime.Unix()),
					Wght:   weight,
				}},
				pWallet.AVAXAssetID(),
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardKey.Address()},
				},
			)
			require.NoError(err)
		})

		ginkgo.By("exporting AVAX from the P-Chain to the X-Chain", func() {
			_, err := pWallet.IssueExportTx(
				xWallet.BlockchainID(),
				exportOutputs,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("importing AVAX from the P-Chain to the X-Chain", func() {
			_, err := xWallet.IssueImportTx(
				constants.PlatformChainID,
				&recipientOwner,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("checking that the recipient address has received imported funds on the X-Chain", func() {
			balances, err := xWallet.Builder().GetFTBalance(common.WithCustomAddresses(set.Of(
				recipientKey.Address(),
			)))
			require.NoError(err)
			require.Greater(balances[avaxAssetID], uint64(0))
		})

		ginkgo.By("exporting AVAX from the P-Chain to the C-Chain", func() {
			_, err := pWallet.IssueExportTx(
				cWallet.BlockchainID(),
				exportOutputs,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("importing AVAX from the P-Chain to the C-Chain", func() {
			_, err := cWallet.IssueImportTx(
				constants.PlatformChainID,
				recipientEthAddress,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("checking that the recipient address has received imported funds on the C-Chain")
		ethClient := e2e.Env.NewEthClient(nodeURI)
		e2e.Eventually(func() bool {
			balance, err := ethClient.BalanceAt(e2e.DefaultContext(), recipientEthAddress, nil)
			require.NoError(err)
			return balance.Cmp(big.NewInt(0)) > 0
		}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see recipient address funded before timeout")
	})
})
