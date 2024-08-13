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
	tc := e2e.NewTestContext()
	require := require.New(tc)

	const txAmount = 10 * units.Avax // Arbitrary amount to send and transfer

	ginkgo.It("should ensure that funds can be transferred from the C-Chain to the X-Chain and the P-Chain", func() {
		env := e2e.GetEnv(tc)

		tc.By("initializing a new eth client")
		// Select a random node URI to use for both the eth client and
		// the wallet to avoid having to verify that all nodes are at
		// the same height before initializing the wallet.
		nodeURI := env.GetRandomNodeURI()
		ethClient := e2e.NewEthClient(tc, nodeURI)

		tc.By("allocating a pre-funded key to send from and a recipient key to deliver to")
		senderKey := env.AllocatePreFundedKey()
		senderEthAddress := evm.GetEthAddress(senderKey)
		recipientKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		recipientEthAddress := evm.GetEthAddress(recipientKey)

		tc.By("sending funds from one address to another on the C-Chain", func() {
			// Create transaction
			acceptedNonce, err := ethClient.AcceptedNonceAt(tc.DefaultContext(), senderEthAddress)
			require.NoError(err)
			gasPrice := e2e.SuggestGasPrice(tc, ethClient)
			tx := types.NewTransaction(
				acceptedNonce,
				recipientEthAddress,
				big.NewInt(int64(txAmount)),
				e2e.DefaultGasLimit,
				gasPrice,
				nil,
			)

			// Sign transaction
			cChainID, err := ethClient.ChainID(tc.DefaultContext())
			require.NoError(err)
			signer := types.NewEIP155Signer(cChainID)
			signedTx, err := types.SignTx(tx, signer, senderKey.ToECDSA())
			require.NoError(err)

			_ = e2e.SendEthTransaction(tc, ethClient, signedTx)

			tc.By("waiting for the C-Chain recipient address to have received the sent funds")
			tc.Eventually(func() bool {
				balance, err := ethClient.BalanceAt(tc.DefaultContext(), recipientEthAddress, nil)
				require.NoError(err)
				return balance.Cmp(big.NewInt(0)) > 0
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see funds delivered before timeout")
		})

		// Wallet must be initialized after sending funds on the
		// C-Chain with the same node URI to ensure wallet state
		// matches on-chain state.
		tc.By("initializing a keychain and associated wallet")
		keychain := secp256k1fx.NewKeychain(senderKey, recipientKey)
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)
		xWallet := baseWallet.X()
		cWallet := baseWallet.C()
		pWallet := baseWallet.P()

		tc.By("defining common configuration")
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

		tc.By("exporting AVAX from the C-Chain to the X-Chain", func() {
			_, err := cWallet.IssueExportTx(
				xContext.BlockchainID,
				exportOutputs,
				tc.WithDefaultContext(),
				e2e.WithSuggestedGasPrice(tc, ethClient),
			)
			require.NoError(err)
		})

		tc.By("importing AVAX from the C-Chain to the X-Chain", func() {
			_, err := xWallet.IssueImportTx(
				cContext.BlockchainID,
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

		tc.By("exporting AVAX from the C-Chain to the P-Chain", func() {
			_, err := cWallet.IssueExportTx(
				constants.PlatformChainID,
				exportOutputs,
				tc.WithDefaultContext(),
				e2e.WithSuggestedGasPrice(tc, ethClient),
			)
			require.NoError(err)
		})

		tc.By("importing AVAX from the C-Chain to the P-Chain", func() {
			_, err = pWallet.IssueImportTx(
				cContext.BlockchainID,
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
