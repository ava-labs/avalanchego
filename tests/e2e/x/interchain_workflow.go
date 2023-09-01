// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"math/big"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/plugin/evm"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ = e2e.DescribeXChain("[Interchain Workflow]", func() {
	require := require.New(ginkgo.GinkgoT())

	const transferAmount = 10 * units.Avax

	ginkgo.It("should ensure that funds can be transferred from the X-Chain to the C-Chain and the P-Chain", func() {
		nodeURI := e2e.Env.GetRandomNodeURI()

		ginkgo.By("creating wallet with a funded key to send from and recipient key to deliver to")
		factory := secp256k1.Factory{}
		recipientKey, err := factory.NewPrivateKey()
		require.NoError(err)
		keychain := e2e.Env.NewKeychain(1)
		keychain.Add(recipientKey)
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
		// Use the same outputs for both C-Chain and P-Chain exports
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

		ginkgo.By("sending funds from one address to another on the X-Chain", func() {
			e2e.LogTxAndCheck(
				xWallet.IssueBaseTx(
					[]*avax.TransferableOutput{{
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt:          transferAmount,
							OutputOwners: recipientOwner,
						},
					}},
					e2e.WithDefaultContext(),
				),
			)
		})

		ginkgo.By("checking that the X-Chain recipient address has received the sent funds", func() {
			balances, err := xWallet.Builder().GetFTBalance(common.WithCustomAddresses(set.Of(
				recipientKey.Address(),
			)))
			require.NoError(err)
			require.Greater(balances[avaxAssetID], uint64(0))
		})

		ginkgo.By("exporting AVAX from the X-Chain to the C-Chain", func() {
			e2e.LogTxAndCheck(
				xWallet.IssueExportTx(
					cWallet.BlockchainID(),
					exportOutputs,
					e2e.WithDefaultContext(),
				),
			)
		})

		ginkgo.By("initializing a new eth client")
		ethClient := e2e.Env.NewEthClient(nodeURI)

		ginkgo.By("importing AVAX from the X-Chain to the C-Chain", func() {
			e2e.LogTxAndCheck(
				cWallet.IssueImportTx(
					xWallet.BlockchainID(),
					recipientEthAddress,
					e2e.WithDefaultContext(),
					e2e.WithSuggestedGasPrice(ethClient),
				),
			)
		})

		ginkgo.By("checking that the recipient address has received imported funds on the C-Chain")
		e2e.Eventually(func() bool {
			balance, err := ethClient.BalanceAt(e2e.DefaultContext(), recipientEthAddress, nil)
			require.NoError(err)
			return balance.Cmp(big.NewInt(0)) > 0
		}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see recipient address funded before timeout")

		ginkgo.By("exporting AVAX from the X-Chain to the P-Chain", func() {
			e2e.LogTxAndCheck(
				xWallet.IssueExportTx(
					constants.PlatformChainID,
					exportOutputs,
					e2e.WithDefaultContext(),
				),
			)
		})

		ginkgo.By("importing AVAX from the X-Chain to the P-Chain", func() {
			e2e.LogTxAndCheck(
				pWallet.IssueImportTx(
					xWallet.BlockchainID(),
					&recipientOwner,
					e2e.WithDefaultContext(),
				),
			)
		})

		ginkgo.By("checking that the recipient address has received imported funds on the P-Chain", func() {
			balances, err := pWallet.Builder().GetBalance(common.WithCustomAddresses(set.Of(
				recipientKey.Address(),
			)))
			require.NoError(err)
			require.Greater(balances[avaxAssetID], uint64(0))
		})
	})
})
