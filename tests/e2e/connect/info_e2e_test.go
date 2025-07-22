// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package connect_test

import (
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/info/v1/infov1connect"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"

	infov1 "github.com/ava-labs/avalanchego/proto/pb/info/v1"
)

var _ = ginkgo.Describe("[Connect Info API]", func() {
	var (
		tc     = e2e.NewTestContext()
		env    = e2e.GetEnv(tc)
		client infov1connect.InfoServiceClient
		ctx    = tc.DefaultContext()
	)

	ginkgo.BeforeAll(func() {
		rootDir := env.RootNetworkDir
		network := tmpnet.NewDefaultNetwork("connect-info-e2e")

		err := tmpnet.BootstrapNewNetwork(ctx, tc.Log(), network, rootDir)
		require.NoError(tc, err)

		node, err := network.GetNode(ids.BuildTestNodeID([]byte("node-0")))
		e2e.WaitForHealthy(tc, node)
		url := node.GetAccessibleURI()
		client = infov1connect.NewInfoServiceClient(http.DefaultClient, url)
	})

	tc.By("NodeVersion returns version info", func() {
		req := connect.NewRequest(&infov1.NodeVersionRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NodeVersion(ctx, req)
		require.NoError(tc, err)
		require.NotEmpty(tc, resp.Msg.Version)
	})

	tc.By("NodeID returns a node ID", func() {
		req := connect.NewRequest(&infov1.NodeIDRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NodeID(ctx, req)
		require.NoError(tc, err)
		require.True(tc, strings.HasPrefix(resp.Msg.NodeId, "NodeID-"))
	})

	tc.By("NetworkID returns a network ID", func() {
		req := connect.NewRequest(&infov1.NetworkIDRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NetworkID(ctx, req)
		require.NoError(tc, err)
		require.Positive(tc, resp.Msg.NetworkId)
	})

	tc.By("NetworkName returns a network name", func() {
		req := connect.NewRequest(&infov1.NetworkNameRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NetworkName(ctx, req)
		require.NoError(tc, err)
		require.NotEmpty(tc, resp.Msg.NetworkName)
	})

	tc.By("NodeIP returns an IP", func() {
		req := connect.NewRequest(&infov1.NodeIPRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NodeIP(ctx, req)
		require.NoError(tc, err)
		require.NotEmpty(tc, resp.Msg.Ip)
	})

	tc.By("BlockchainID returns a blockchain ID for X", func() {
		req := connect.NewRequest(&infov1.BlockchainIDRequest{Alias: "X"})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.BlockchainID(ctx, req)
		require.NoError(tc, err)
		require.NotEmpty(tc, resp.Msg.BlockchainId)
	})

	tc.By("Peers returns a list", func() {
		req := connect.NewRequest(&infov1.PeersRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.Peers(ctx, req)
		require.NoError(tc, err)
		require.GreaterOrEqual(tc, resp.Msg.NumPeers, 0)
	})

	tc.By("IsBootstrapped returns true for X", func() {
		tc.Eventually(func() bool {
			req := connect.NewRequest(&infov1.IsBootstrappedRequest{Chain: "X"})
			req.Header().Set("Avalanche-API-Route", "info")
			resp, err := client.IsBootstrapped(ctx, req)
			return err == nil && resp.Msg.IsBootstrapped
		}, 60*time.Second, 2*time.Second, "node should eventually bootstrap")
	})

	tc.By("Upgrades returns a response", func() {
		req := connect.NewRequest(&infov1.UpgradesRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.Upgrades(ctx, req)
		require.NoError(tc, err)
		require.NotNil(tc, resp.Msg)
	})

	tc.By("Uptime returns a response", func() {
		req := connect.NewRequest(&infov1.UptimeRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.Uptime(ctx, req)
		require.NoError(tc, err)
		require.NotNil(tc, resp.Msg)
	})

	tc.By("VMs returns at least avm", func() {
		req := connect.NewRequest(&infov1.VMsRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.VMs(ctx, req)
		require.NoError(tc, err)
		found := false
		for _, v := range resp.Msg.Vms {
			for _, alias := range v.Aliases {
				if alias == "avm" {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		require.True(tc, found)
	})
})
