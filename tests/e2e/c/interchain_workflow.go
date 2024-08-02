// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = e2e.DescribeCChain("[Interchain Workflow]", func() {
	require := require.New(ginkgo.GinkgoT())

	const txAmount = 10 * units.Avax // Arbitrary amount to send and transfer

	ginkgo.It("should ensure that funds can be transferred from the C-Chain to the X-Chain and the P-Chain", func() {
		ginkgo.By("initializing a new eth client")
		// Select a random node URI to use for both the eth client and
		// the wallet to avoid having to verify that all nodes are at
		// the same height before initializing the wallet.
		nodeURI := e2e.Env.GetRandomNodeURI()
		ethClient := e2e.NewEthClient(nodeURI)

		ginkgo.By("allocating a pre-funded key to send from and a recipient key to deliver to")
		senderKey := e2e.Env.AllocatePreFundedKey()
		senderEthAddress := evm.GetEthAddress(senderKey)
		recipientKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		recipientEthAddress := evm.GetEthAddress(recipientKey)

		ginkgo.By("sending funds from one address to another on the C-Chain", func() {
			// Create transaction
			acceptedNonce, err := ethClient.AcceptedNonceAt(e2e.DefaultContext(), senderEthAddress)
			require.NoError(err)
			gasPrice := e2e.SuggestGasPrice(ethClient)
			tx := types.NewTransaction(
				acceptedNonce,
				recipientEthAddress,
				big.NewInt(int64(txAmount)),
				e2e.DefaultGasLimit,
				gasPrice,
				nil,
			)

			// Sign transaction
			cChainID, err := ethClient.ChainID(e2e.DefaultContext())
			require.NoError(err)
			signer := types.NewEIP155Signer(cChainID)
			signedTx, err := types.SignTx(tx, signer, senderKey.ToECDSA())
			require.NoError(err)

			_ = e2e.SendEthTransaction(ethClient, signedTx)

			ginkgo.By("waiting for the C-Chain recipient address to have received the sent funds")
			e2e.Eventually(func() bool {
				balance, err := ethClient.BalanceAt(e2e.DefaultContext(), recipientEthAddress, nil)
				require.NoError(err)
				return balance.Cmp(big.NewInt(0)) > 0
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see funds delivered before timeout")
		})

		// Wallet must be initialized after sending funds on the
		// C-Chain with the same node URI to ensure wallet state
		// matches on-chain state.
		ginkgo.By("initializing a keychain and associated wallet")
		keychain := secp256k1fx.NewKeychain(senderKey, recipientKey)
		baseWallet := e2e.NewWallet(keychain, nodeURI)
		xWallet := baseWallet.X()
		cWallet := baseWallet.C()
		pWallet := baseWallet.P()

		ginkgo.By("defining common configuration")
		xBuilder := xWallet.Builder()
		xContext := xBuilder.Context()
		cBuilder := cWallet.Builder()
		cContext := cBuilder.Context()
		avaxAssetID := xContext.AVAXAssetID
		// Use the same owner for import funds to X-Chain and P-Chain
		recipientOwner := secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				recipientKey.Address(),
			},
		}
		// Use the same outputs for both X-Chain and P-Chain exports
		exportOutputs := []*secp256k1fx.TransferOutput{
			{
				Amt: txAmount,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs: []ids.ShortID{
						keychain.Keys[0].Address(),
					},
				},
			},
		}

		ginkgo.By("exporting AVAX from the C-Chain to the X-Chain", func() {
			_, err := cWallet.IssueExportTx(
				xContext.BlockchainID,
				exportOutputs,
				e2e.WithDefaultContext(),
				e2e.WithSuggestedGasPrice(ethClient),
			)
			require.NoError(err)
		})

		ginkgo.By("importing AVAX from the C-Chain to the X-Chain", func() {
			_, err := xWallet.IssueImportTx(
				cContext.BlockchainID,
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
			require.Positive(balances[avaxAssetID])
		})

		ginkgo.By("exporting AVAX from the C-Chain to the P-Chain", func() {
			_, err := cWallet.IssueExportTx(
				constants.PlatformChainID,
				exportOutputs,
				e2e.WithDefaultContext(),
				e2e.WithSuggestedGasPrice(ethClient),
			)
			require.NoError(err)
		})

		ginkgo.By("importing AVAX from the C-Chain to the P-Chain", func() {
			_, err = pWallet.IssueImportTx(
				cContext.BlockchainID,
				&recipientOwner,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("checking that the recipient address has received imported funds on the P-Chain", func() {
			balances, err := pWallet.Builder().GetBalance(common.WithCustomAddresses(set.Of(
				recipientKey.Address(),
			)))
			require.NoError(err)
			require.Positive(balances[avaxAssetID])
		})

		_ = e2e.CheckBootstrapIsPossible(e2e.Env.GetNetwork())
	})
})
