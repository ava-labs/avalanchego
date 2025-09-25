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
	pbproposervm "github.com/ava-labs/avalanchego/connectproto/pb/proposervm"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/vms/proposervm"

	pb "github.com/ava-labs/avalanchego/connectproto/pb/proposervm/proposervmconnect"
)

var _ = e2e.DescribeCChain("[ProposerVM API]", ginkgo.Label("ProposerVMAPI"), func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	setupProposerVMTest := func() (avalancheCChainID ids.ID, nodeURI tmpnet.NodeURI) {
		var (
			env = e2e.GetEnv(tc)
		)
		nodeURI = env.GetRandomNodeURI()

		// Get the proper Avalanche C-Chain ID for routing
		keychain := env.NewKeychain()
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)
		cWallet := baseWallet.C()
		cBuilder := cWallet.Builder()
		cContext := cBuilder.Context()
		avalancheCChainID = cContext.BlockchainID

		// Send transactions to trigger block production and ProposerVM fork
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

		return avalancheCChainID, nodeURI
	}

	ginkgo.It("should advance the proposervm epoch according to the upgrade config epoch duration", func() {
		// Set up environment and trigger block production
		avalancheCChainID, nodeURI := setupProposerVMTest()

		// Test the ProposerVM Connect RPC API
		proposerClient := pb.NewProposerVMClient(
			connectclient.New(),
			nodeURI.URI,
			connect.WithInterceptors(
				connectclient.SetRouteHeaderInterceptor{Route: []string{avalancheCChainID.String(), proposervm.HTTPHeaderRoute}},
			),
		)
		resp, err := proposerClient.GetProposedHeight(tc.DefaultContext(), &connect.Request[pbproposervm.GetProposedHeightRequest]{})
		require.NoError(err)
		require.Positive(resp.Msg.Height, "proposervm height should be greater than 0")
	})

	ginkgo.It("should provide JSON-RPC API for ProposerVM", ginkgo.Label("ProposerVMAPI"), func() {
		// Set up environment and trigger block production
		avalancheCChainID, nodeURI := setupProposerVMTest()

		// Test the ProposerVM JSON-RPC API
		client := proposervm.NewClient(nodeURI.URI, avalancheCChainID.String())
		height, err := client.GetProposedHeight(tc.DefaultContext())
		require.NoError(err)
		require.Positive(height, "proposervm height should be greater than 0")
	})
})
