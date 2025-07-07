// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math/big"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/proposervm"
)

var _ = e2e.DescribeCChain("[ProposerVM Epoch]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	const txAmount = 10 * units.Avax // Arbitrary amount to send and transfer

	ginkgo.It("should advance the proposervm epoch according to the upgrade config epoch duration", func() {
		// TODO: Skip this test if Granite is not activated

		env := e2e.GetEnv(tc)
		var (
			senderKey           = env.PreFundedKey
			senderEthAddress    = senderKey.EthAddress()
			recipientKey        = e2e.NewPrivateKey(tc)
			recipientEthAddress = recipientKey.EthAddress()
		)

		tc.By("initializing a new eth client")
		// Select a random node URI to use for both the eth client and
		// the wallet to avoid having to verify that all nodes are at
		// the same height before initializing the wallet.
		nodeURI := env.GetRandomNodeURI()
		ethClient := e2e.NewEthClient(tc, nodeURI)

		proposerClient := proposervm.NewClient(nodeURI.URI, "C")

		tc.By("issuing C-Chain transactions to advance the epoch", func() {
			// Issue enough C-Chain transactions to observe the epoch advancing
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()

			// Issue enough txs to activate the proposervm form and ensure we advance the epoch (duration is 4s)
			const numTxs = 7
			txCount := 0

			initialEpochNumber, _, _, err := proposerClient.GetEpoch(tc.DefaultContext())
			require.NoError(err)

			for range ticker.C {
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

				receipt := e2e.SendEthTransaction(tc, ethClient, signedTx)
				require.Equal(types.ReceiptStatusSuccessful, receipt.Status)

				epochNumber, epochStartTime, pChainHeight, err := proposerClient.GetEpoch(tc.DefaultContext())
				tc.Log().Debug(
					"epoch",
					zap.Uint64("Epoch Number:", epochNumber),
					zap.Int64("Epoch Start Time:", epochStartTime),
					zap.Uint64("P-Chain Height:", pChainHeight),
				)

				txCount++
				if txCount >= numTxs {
					require.Greater(epochNumber, initialEpochNumber,
						"expected epoch number to advance after issuing %d transactions, but it did not",
						numTxs,
					)
					break
				}
			}
		})
	})
})
