// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"math/big"

	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = e2e.DescribeXChain("[Interchain Workflow]", ginkgo.Label(e2e.UsesCChainLabel), func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	const transferAmount = 10 * units.Avax

	ginkgo.It("should ensure that funds can be transferred from the X-Chain to the C-Chain and the P-Chain", func() {
		env := e2e.GetEnv(tc)

		nodeURI := env.GetRandomNodeURI()

		tc.By("creating wallet with a funded key to send from and recipient key to deliver to")
		recipientKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		keychain := env.NewKeychain(1)
		keychain.Add(recipientKey)
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)
		xWallet := baseWallet.X()
		cWallet := baseWallet.C()
		pWallet := baseWallet.P()

		tc.By("defining common configuration")
		recipientEthAddress := evm.GetEthAddress(recipientKey)
		xBuilder := xWallet.Builder()
		xContext := xBuilder.Context()
		cBuilder := cWallet.Builder()
		cContext := cBuilder.Context()
		avaxAssetID := xContext.AVAXAssetID
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

		tc.By("sending funds from one address to another on the X-Chain", func() {
			_, err = xWallet.IssueBaseTx(
				[]*avax.TransferableOutput{{
					Asset: avax.Asset{
						ID: avaxAssetID,
					},
					Out: &secp256k1fx.TransferOutput{
						Amt:          transferAmount,
						OutputOwners: recipientOwner,
					},
				}},
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("checking that the X-Chain recipient address has received the sent funds", func() {
			balances, err := xWallet.Builder().GetFTBalance(common.WithCustomAddresses(set.Of(
				recipientKey.Address(),
			)))
			require.NoError(err)
			require.Positive(balances[avaxAssetID])
		})

		tc.By("exporting AVAX from the X-Chain to the C-Chain", func() {
			_, err := xWallet.IssueExportTx(
				cContext.BlockchainID,
				exportOutputs,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("initializing a new eth client")
		ethClient := e2e.NewEthClient(tc, nodeURI)

		tc.By("importing AVAX from the X-Chain to the C-Chain", func() {
			_, err := cWallet.IssueImportTx(
				xContext.BlockchainID,
				recipientEthAddress,
				tc.WithDefaultContext(),
				e2e.WithSuggestedGasPrice(tc, ethClient),
			)
			require.NoError(err)
		})

		tc.By("checking that the recipient address has received imported funds on the C-Chain")
		tc.Eventually(func() bool {
			balance, err := ethClient.BalanceAt(tc.DefaultContext(), recipientEthAddress, nil)
			require.NoError(err)
			return balance.Cmp(big.NewInt(0)) > 0
		}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see recipient address funded before timeout")

		tc.By("exporting AVAX from the X-Chain to the P-Chain", func() {
			_, err := xWallet.IssueExportTx(
				constants.PlatformChainID,
				exportOutputs,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("importing AVAX from the X-Chain to the P-Chain", func() {
			_, err := pWallet.IssueImportTx(
				xContext.BlockchainID,
				&recipientOwner,
				tc.WithDefaultContext(),
			)
			require.NoError(err)
		})

		tc.By("checking that the recipient address has received imported funds on the P-Chain", func() {
			balances, err := pWallet.Builder().GetBalance(common.WithCustomAddresses(set.Of(
				recipientKey.Address(),
			)))
			require.NoError(err)
			require.Positive(balances[avaxAssetID])
		})

		_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
	})
})
