// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math/big"

	"connectrpc.com/connect"
	"github.com/ava-labs/libevm/core/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/connectclient"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/vms/proposervm"

	pbproposervm "github.com/ava-labs/avalanchego/connectproto/pb/proposervm"
	pb "github.com/ava-labs/avalanchego/connectproto/pb/proposervm/proposervmconnect"
)

var _ = e2e.DescribeCChain("[ProposerVM API]", ginkgo.Label("proposervm"), func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("exposes and handles API calls", func() {
		env := e2e.GetEnv(tc)
		nodeURI := env.GetRandomNodeURI()

		tc.By("advancing the C-chain height", func() {
			ethClient := e2e.NewEthClient(tc, nodeURI)
			senderKey := env.PreFundedKey
			senderEthAddress := senderKey.EthAddress()
			recipientKey := e2e.NewPrivateKey(tc)
			recipientEthAddress := recipientKey.EthAddress()

			for i := 0; i < 3; i++ {
				// Create and send a simple transaction to trigger block production
				nonce, err := ethClient.AcceptedNonceAt(tc.DefaultContext(), senderEthAddress)
				require.NoError(err)
				gasPrice := e2e.SuggestGasPrice(tc, ethClient)
				tx := types.NewTransaction(
					nonce,
					recipientEthAddress,
					big.NewInt(1000000000000000),
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

				// Send the transaction and wait for receipt
				receipt := e2e.SendEthTransaction(tc, ethClient, signedTx)
				require.Equal(types.ReceiptStatusSuccessful, receipt.Status)
			}
		})

		// Get the proper C-chain ID for routing
		keychain := env.NewKeychain()
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)
		cWallet := baseWallet.C()
		cBuilder := cWallet.Builder()
		cContext := cBuilder.Context()
		cChainID := cContext.BlockchainID

		tc.By("verifying the grpc service handles API calls", func() {
			proposerClient := pb.NewProposerVMClient(
				connectclient.New(),
				nodeURI.URI,
				connect.WithInterceptors(
					connectclient.SetRouteHeaderInterceptor{Route: []string{cChainID.String(), proposervm.HTTPHeaderRoute}},
				),
			)
			resp, err := proposerClient.GetProposedHeight(tc.DefaultContext(), &connect.Request[pbproposervm.GetProposedHeightRequest]{})
			require.NoError(err)
			require.Positive(resp.Msg.Height)
		})

		tc.By("verifying the jsonrpc service handles API calls", func() {
			client := proposervm.NewJSONRPCClient(nodeURI.URI, cChainID.String())
			height, err := client.GetProposedHeight(tc.DefaultContext())
			require.NoError(err)
			require.Positive(height)
		})
	})
})
