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

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/tests"
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
		var (
			env          = e2e.GetEnv(tc)
			nodeURI      = env.GetRandomNodeURI()
			senderKey    = env.PreFundedKey
			recipientKey = e2e.NewPrivateKey(tc)
			ethClient    = e2e.NewEthClient(tc, nodeURI)
			infoClient   = info.NewClient(nodeURI.URI)
		)

		upgrades, err := infoClient.Upgrades(tc.DefaultContext())
		require.NoError(err)

		if !upgrades.IsGraniteActivated(time.Now()) {
			ginkgo.Skip("skipping test because granite isn't active")
		}

		// Issue a transaction to the C-Chain to advance past genesis block
		issueTransaction(tc, ethClient, senderKey, recipientKey.EthAddress(), txAmount)

		tc.Eventually(func() bool {
			bn, err := ethClient.BlockNumber(tc.DefaultContext())
			require.NoError(err)
			return bn != 0
		},
			e2e.DefaultTimeout,
			e2e.DefaultPollingInterval,
			"failed to see a non-zero block before timeout",
		)

		tc.By("issuing C-Chain transactions to advance the epoch", func() {
			proposerClient := proposervm.NewClient(nodeURI.URI, "C")

			initialEpoch, err := proposerClient.GetCurrentEpoch(tc.DefaultContext())
			require.NoError(err)
			tc.Log().Info("initial epoch", zap.Any("epoch", initialEpoch))

			time.Sleep(upgrades.GraniteEpochDuration)
			issueTransaction(tc, ethClient, senderKey, recipientKey.EthAddress(), txAmount)

			advancedEpoch, err := proposerClient.GetCurrentEpoch(tc.DefaultContext())
			require.NoError(err)

			tc.Log().Info("advanced epoch", zap.Any("epoch", advancedEpoch))

			require.Equal(
				advancedEpoch.Number,
				initialEpoch.Number+1,
				"expected epoch number to advance, but it did not",
			)
		})
	})
})

func issueTransaction(
	tc tests.TestContext,
	ethClient *ethclient.Client,
	senderKey *secp256k1.PrivateKey,
	recipientEthAddress common.Address,
	txAmount uint64,
) {
	acceptedNonce, err := ethClient.AcceptedNonceAt(tc.DefaultContext(), senderKey.EthAddress())
	require.NoError(tc, err)
	gasPrice := e2e.SuggestGasPrice(tc, ethClient)
	tx := types.NewTransaction(
		acceptedNonce,
		recipientEthAddress,
		new(big.Int).SetUint64(txAmount),
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
