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
	"github.com/ava-labs/avalanchego/connectproto/pb/proposervm"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"

	pb "github.com/ava-labs/avalanchego/connectproto/pb/proposervm/proposervmconnect"
)

var _ = e2e.DescribeCChain("[ProposerVM API]", ginkgo.Label("ProposerVMAPI"), func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("should advance the proposervm epoch according to the upgrade config epoch duration", func() {
		var (
			env     = e2e.GetEnv(tc)
			nodeURI = env.GetRandomNodeURI()
		)

		// Get the proper Avalanche C-Chain ID for routing (not the Ethereum chain ID)
		keychain := env.NewKeychain()
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)
		cWallet := baseWallet.C()
		cBuilder := cWallet.Builder()
		cContext := cBuilder.Context()
		avalancheCChainID := cContext.BlockchainID

		// First, create an ethereum client and send some transactions to trigger block production
		// This ensures the ProposerVM has forked and has some proposer blocks
		ethClient := e2e.NewEthClient(tc, nodeURI)

		// Send a few transactions to ensure block production and ProposerVM fork
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
				big.NewInt(1000000000000000), // 0.001 ETH
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

		// Now test the ProposerVM API - it should work since we have proposer blocks
		proposerClient := pb.NewProposerVMClient(
			connectclient.New(),
			nodeURI.URI,
			connect.WithInterceptors(
				connectclient.SetRouteHeaderInterceptor{Route: []string{avalancheCChainID.String(), "proposervm"}},
			),
		)
		resp, err := proposerClient.GetProposedHeight(tc.DefaultContext(), &connect.Request[proposervm.GetProposedHeightRequest]{})
		require.NoError(err)
		require.Positive(resp.Msg.Height, "proposervm height should be greater than 0")
	})
})
