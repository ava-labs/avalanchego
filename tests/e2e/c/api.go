// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"connectrpc.com/connect"
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

		proposerClient := pb.NewProposerVMClient(
			connectclient.New(),
			nodeURI.URI,
			connect.WithInterceptors(
				connectclient.SetRouteHeaderInterceptor{Route: avalancheCChainID.String()},
			),
		)
		resp, err := proposerClient.GetProposedHeight(tc.DefaultContext(), &connect.Request[proposervm.GetProposedHeightRequest]{})
		require.NoError(err)
		require.Positive(resp.Msg.Height, "proposervm height should be greater than 0")
	})
})
