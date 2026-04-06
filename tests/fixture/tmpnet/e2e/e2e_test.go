// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

func TestE2E(t *testing.T) {
	ginkgo.RunSpecs(t, "tmpnet e2e test suite")
}

var flagVars *e2e.FlagVars

func init() {
	flagVars = e2e.RegisterFlags(
		e2e.WithDefaultOwner("tmpnet-e2e"),
		e2e.WithDefaultNodeCount(1),
	)
}

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	tc := e2e.NewEventHandlerTestContext()
	return e2e.NewTestEnvironment(
		tc,
		flagVars,
		&tmpnet.Network{
			Owner: flagVars.NetworkOwner(),
			Nodes: tmpnet.NewNodesOrPanic(1),
		},
	).Marshal()
}, func(envBytes []byte) {
	e2e.InitSharedTestEnvironment(e2e.NewTestContext(), envBytes)
})

var _ = ginkgo.Describe("[tmpnet archive]", func() {
	ginkgo.It("exports non-ephemeral nodes and imports a restartable network with a fresh identity", func() {
		tc := e2e.NewTestContext()
		require := require.New(tc)
		env := e2e.GetEnv(tc)

		network := &tmpnet.Network{
			Owner: "tmpnet-archive-private",
			Nodes: tmpnet.NewNodesOrPanic(1),
		}
		env.StartPrivateNetwork(network)
		require.Len(network.Nodes, 1)
		persistentNode := network.Nodes[0]
		originalUUID := network.UUID
		originalDir := network.Dir

		nodeURI := tmpnet.NodeURI{
			NodeID: persistentNode.NodeID,
			URI:    persistentNode.GetAccessibleURI(),
		}
		ethClient := e2e.NewEthClient(tc, nodeURI)
		senderKey := network.PreFundedKeys[0]
		senderEthAddress := senderKey.EthAddress()
		recipientKey := e2e.NewPrivateKey(tc)
		recipientEthAddress := recipientKey.EthAddress()

		transferAmount := big.NewInt(1)
		for range 3 {
			nonce, err := ethClient.AcceptedNonceAt(tc.DefaultContext(), senderEthAddress)
			require.NoError(err)
			gasPrice := e2e.SuggestGasPrice(tc, ethClient)
			tx := types.NewTransaction(
				nonce,
				recipientEthAddress,
				transferAmount,
				e2e.DefaultGasLimit,
				gasPrice,
				nil,
			)
			cChainID, err := ethClient.ChainID(tc.DefaultContext())
			require.NoError(err)
			signer := types.NewEIP155Signer(cChainID)
			signedTx, err := types.SignTx(tx, signer, senderKey.ToECDSA())
			require.NoError(err)
			receipt := e2e.SendEthTransaction(tc, ethClient, signedTx)
			require.Equal(types.ReceiptStatusSuccessful, receipt.Status)
		}

		expectedRecipientBalance := big.NewInt(0).Mul(transferAmount, big.NewInt(3))
		recipientBalance, err := ethClient.BalanceAt(tc.DefaultContext(), recipientEthAddress, nil)
		require.NoError(err)
		require.Zero(recipientBalance.Cmp(expectedRecipientBalance))

		originalHeight, err := ethClient.BlockNumber(tc.DefaultContext())
		require.NoError(err)
		require.Positive(originalHeight)

		ephemeralNode := tmpnet.NewEphemeralNode(tmpnet.FlagsMap{})
		require.NoError(network.StartNode(tc.ContextWithTimeout(e2e.DefaultTimeout), ephemeralNode))
		e2e.WaitForHealthy(tc, ephemeralNode)

		require.NoError(network.Stop(tc.ContextWithTimeout(e2e.DefaultTimeout)))

		archiveDir, err := os.MkdirTemp("", "tmpnet-archive-e2e-*")
		require.NoError(err)
		tc.DeferCleanup(func() {
			require.NoError(os.RemoveAll(archiveDir))
		})
		archivePath := filepath.Join(archiveDir, "network.tar.gz")
		require.NoError(tmpnet.ExportNetworkArchive(tc.ContextWithTimeout(e2e.DefaultTimeout), tc.Log(), originalDir, archivePath))

		importedNetwork, err := tmpnet.ImportNetworkArchive(tc.ContextWithTimeout(e2e.DefaultTimeout), tc.Log(), archivePath, env.RootNetworkDir, &network.DefaultRuntimeConfig)
		require.NoError(err)
		tc.DeferCleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
			defer cancel()
			require.NoError(importedNetwork.Stop(ctx))
		})

		require.NotEqual(originalUUID, importedNetwork.UUID)
		require.NotEqual(originalDir, importedNetwork.Dir)
		require.Len(importedNetwork.Nodes, 1)
		require.Equal(persistentNode.NodeID, importedNetwork.Nodes[0].NodeID)

		require.NoError(importedNetwork.Bootstrap(tc.ContextWithTimeout(e2e.DefaultTimeout), tc.Log()))
		importedNodeURI := tmpnet.NodeURI{
			NodeID: importedNetwork.Nodes[0].NodeID,
			URI:    importedNetwork.Nodes[0].GetAccessibleURI(),
		}
		importedClient := e2e.NewEthClient(tc, importedNodeURI)
		importedHeight, err := importedClient.BlockNumber(tc.DefaultContext())
		require.NoError(err)
		require.GreaterOrEqual(importedHeight, originalHeight)

		importedRecipientBalance, err := importedClient.BalanceAt(tc.DefaultContext(), recipientEthAddress, nil)
		require.NoError(err)
		require.Zero(importedRecipientBalance.Cmp(expectedRecipientBalance))
	})
})
