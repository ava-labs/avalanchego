// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"crypto/tls"
	"math/big"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"

	"github.com/ava-labs/avalanchego/api/connectclient"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/connectproto/pb/proposervm"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"

	pb "github.com/ava-labs/avalanchego/connectproto/pb/proposervm/proposervmconnect"
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

		httpClient := &http.Client{
			Transport: &http2.Transport{
				AllowHTTP: true,
				DialTLSContext: func(_ context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
					// Skip TLS to use h2c
					return net.Dial(network, addr)
				},
			},
		}
		cChainID, err := ethClient.ChainID(tc.DefaultContext())
		require.NoError(err)

		proposerClient := pb.NewProposerVMClient(
			httpClient,
			nodeURI.URI,
			connect.WithInterceptors(
				connectclient.SetRouteHeaderInterceptor{Route: cChainID.String()},
			),
		)
		resp, err := proposerClient.GetProposedHeight(tc.DefaultContext(), &connect.Request[proposervm.GetProposedHeightRequest]{})
		require.NoError(err)
		require.Positive(resp.Msg.Height, "proposervm height should be greater than 0")
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
