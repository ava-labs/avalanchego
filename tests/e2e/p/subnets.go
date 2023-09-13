// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/example/timestampvm"
	tsvm_client "github.com/ava-labs/avalanchego/vms/example/timestampvm/client"
)

var _ = e2e.DescribePChain("[Subnets]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("should support adding a subnet and custom chain to an existing network", func() {
		// TODO(marun) make the plugin path configurable
		pluginDir := filepath.Join(os.Getenv("GOPATH"), "src/github.com/ava-labs/avalanchego/build/plugins")
		if fileInfo, err := os.Stat(pluginDir); errors.Is(err, os.ErrNotExist) || !fileInfo.IsDir() {
			ginkgo.Skip(fmt.Sprintf("invalid plugin dir %s", pluginDir))
		}

		ginkgo.By("allocating a funded key")
		nodeURI := e2e.Env.GetRandomNodeURI()
		keychain := e2e.Env.NewKeychain(1)
		keyAddress := keychain.Keys[0].Address()
		baseWallet := e2e.Env.NewWallet(keychain, nodeURI)
		pWallet := baseWallet.P()

		network := e2e.Env.GetNetwork()
		genesisBytes := []byte("e2e")
		// TODO(marun) Simplify this call for e2e
		createdSubnets, err := testnet.CreateSubnets(
			ginkgo.GinkgoWriter,
			e2e.DefaultTimeout,
			pWallet,
			keyAddress,
			network,
			func(node testnet.Node) {
				// Ensure node is stopped on teardown
				ginkgo.DeferCleanup(func() {
					ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
					defer cancel()
					require.NoError(node.Stop(ctx))
				})
			},
			testnet.SubnetSpec{
				Blockchains: []testnet.BlockchainSpec{
					{
						VMName:  "timestamp",
						Genesis: genesisBytes,
					},
				},
				Nodes: []testnet.NodeSpec{
					{
						Flags: testnet.FlagsMap{
							config.PluginDirKey: pluginDir,
						},
						Count: testnet.DefaultNodeCount,
					},
				},
			},
		)
		require.NoError(err)

		ginkgo.By("enabling timestampvm clients")
		newNodes := createdSubnets[0].Nodes
		nodeURIs := make([]testnet.NodeURI, len(newNodes))
		for i, node := range newNodes {
			nodeURIs[i] = testnet.NodeURI{
				NodeID: node.GetID(),
				URI:    node.GetProcessContext().URI,
			}
		}
		blockchainID := createdSubnets[0].BlockchainIDs[0]
		tsvmClient := func(uri string) tsvm_client.Client {
			return tsvm_client.New(fmt.Sprintf("%s/ext/bc/%s", uri, blockchainID))
		}

		firstNodeURI := nodeURIs[0]
		var genesisBlockID ids.ID
		ginkgo.By(fmt.Sprintf("getting the timestampvm genesis block from %s", firstNodeURI.NodeID), func() {
			timestamp, data, height, id, _, err := tsvmClient(firstNodeURI.URI).GetBlock(e2e.DefaultContext(), nil)
			require.NoError(err)
			require.Zero(timestamp)
			require.Equal(timestampvm.BytesToData(genesisBytes), data)
			require.Zero(height)
			genesisBlockID = id
		})

		ginkgo.By(fmt.Sprintf("creating a new timestampvm block on %s", firstNodeURI.NodeID))
		data := timestampvm.BytesToData(hashing.ComputeHash256([]byte("test")))
		now := time.Now().Unix()
		success, err := tsvmClient(firstNodeURI.URI).ProposeBlock(e2e.DefaultContext(), data)
		require.NoError(err)
		require.True(success)

		for _, nodeURI := range nodeURIs {
			ginkgo.By(fmt.Sprintf("confirming that the new block was processed on %s", nodeURI.NodeID))
			tests.Outf(" ")
			client := tsvmClient(nodeURI.URI)
			e2e.DefaultEventually(func() bool {
				tests.Outf("{{blue}}.{{/}}")
				timestamp, blockData, height, _, parentID, err := client.GetBlock(e2e.DefaultContext(), nil)
				require.NoError(err)
				if height == 0 {
					return false
				}
				require.Less(uint64(now)-5, timestamp)
				require.Equal(blockData, data)
				require.Equal(height, 1)
				require.Equal(parentID, genesisBlockID)
				return true
			}, fmt.Sprintf("failed to see new block processed on %s", nodeURI.NodeID))
			tests.Outf("\n{{blue}} saw new block processed on %s{{/}}\n", nodeURI.NodeID)
		}
	})
})
