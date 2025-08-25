// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math/big"
	"time"

	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
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
			senderKey    = env.PreFundedKey
			recipientKey = e2e.NewPrivateKey(tc)
		)

		// Select a random node URI to use for both the eth client and
		// the wallet to avoid having to verify that all nodes are at
		// the same height before initializing the wallet.
		nodeURI := env.GetRandomNodeURI()
		ethClient := e2e.NewEthClient(tc, nodeURI)

		proposerClient := proposervm.NewClient(nodeURI.URI, "C")

		tc.By("issuing C-Chain transactions to advance the epoch", func() {
			initialEpoch, err := proposerClient.GetEpoch(tc.DefaultContext())
			require.NoError(err)

			time.Sleep(5 * time.Second)

			issueTransaction(tc, ethClient, senderKey, recipientKey.EthAddress(), int64(txAmount))

			epoch, err := proposerClient.GetEpoch(tc.DefaultContext())
			require.NoError(err)
			tc.Log().Debug(
				"epoch",
				zap.Uint64("Epoch Number:", epoch.Number),
				zap.Int64("Epoch Start Time:", epoch.StartTime.Unix()),
				zap.Uint64("P-Chain Height:", epoch.Height),
			)

			require.Greater(
				epoch.Number,
				initialEpoch.Number,
				"expected epoch number to advance, but it did not",
			)
		})
	})
})

func issueTransaction(
	tc *e2e.GinkgoTestContext,
	ethClient *ethclient.Client,
	senderKey *secp256k1.PrivateKey,
	recipientEthAddress common.Address,
	txAmount int64,
) {
	acceptedNonce, err := ethClient.AcceptedNonceAt(tc.DefaultContext(), senderKey.EthAddress())
	require.NoError(tc, err)
	gasPrice := e2e.SuggestGasPrice(tc, ethClient)
	tx := types.NewTransaction(
		acceptedNonce,
		recipientEthAddress,
		big.NewInt(txAmount),
		e2e.DefaultGasLimit,
		gasPrice,
		nil,
	)

	cChainID, err := ethClient.ChainID(tc.DefaultContext())
	require.NoError(tc, err)
	signer := types.NewEIP155Signer(cChainID)
	signedTx, err := types.SignTx(tx, signer, senderKey.ToECDSA())
	require.NoError(tc, err)

	receipt := e2e.SendEthTransaction(tc, ethClient, signedTx)
	require.Equal(tc, types.ReceiptStatusSuccessful, receipt.Status)
}
