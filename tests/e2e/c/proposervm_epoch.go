// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math/big"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/proposervm"
)

var _ = e2e.DescribeCChain("[ProposerVM Epoch]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("should advance the proposervm epoch according to the upgrade config epoch duration", func() {
		var (
			env        = e2e.GetEnv(tc)
			nodeURI    = env.GetRandomNodeURI()
			senderKey  = env.PreFundedKey
			ethClient  = e2e.NewEthClient(tc, nodeURI)
			infoClient = info.NewClient(nodeURI.URI)
		)

		ctx := tc.DefaultContext()
		upgrades, err := infoClient.Upgrades(ctx)
		require.NoError(err)

		if !upgrades.IsGraniteActivated(time.Now()) {
			ginkgo.Skip("skipping test because granite isn't active")
		}

		// Issue a transaction to the C-Chain to advance past genesis block
		issueTransaction(tc, ethClient, senderKey)

		proposerClient := proposervm.NewJSONRPCClient(nodeURI.URI, "C")

		initialEpoch, err := proposerClient.GetCurrentEpoch(ctx)
		require.NoError(err)
		tc.Log().Info("initial epoch",
			zap.Reflect("epoch", initialEpoch),
		)

		issueTransaction(tc, ethClient, senderKey)
		time.Sleep(upgrades.GraniteEpochDuration)
		issueTransaction(tc, ethClient, senderKey)

		advancedEpoch, err := proposerClient.GetCurrentEpoch(ctx)
		require.NoError(err)

		tc.Log().Info("advanced epoch",
			zap.Reflect("epoch", advancedEpoch),
		)

		require.Greater(advancedEpoch.Number, initialEpoch.Number)
		require.GreaterOrEqual(
			advancedEpoch.StartTime,
			initialEpoch.StartTime+int64(upgrades.GraniteEpochDuration.Seconds()),
		)
		// P-chain height may not increase if no new blocks were created on the
		// P-chain.
		require.GreaterOrEqual(advancedEpoch.PChainHeight, initialEpoch.PChainHeight)
	})
})

func issueTransaction(
	tc tests.TestContext,
	ethClient *ethclient.Client,
	senderKey *secp256k1.PrivateKey,
) {
	ctx := tc.DefaultContext()
	addr := senderKey.EthAddress()
	acceptedNonce, err := ethClient.AcceptedNonceAt(ctx, addr)
	require.NoError(tc, err)

	gasPrice := e2e.SuggestGasPrice(tc, ethClient)
	const amount = 10 * units.Avax // Arbitrary amount to transfer
	tx := types.NewTransaction(
		acceptedNonce,
		addr,
		new(big.Int).SetUint64(amount),
		e2e.DefaultGasLimit,
		gasPrice,
		nil,
	)

	cChainID, err := ethClient.ChainID(ctx)
	require.NoError(tc, err)
	signer := types.LatestSignerForChainID(cChainID)
	signedTx, err := types.SignTx(tx, signer, senderKey.ToECDSA())
	require.NoError(tc, err)

	receipt := e2e.SendEthTransaction(tc, ethClient, signedTx)
	require.Equal(tc, types.ReceiptStatusSuccessful, receipt.Status)
}
