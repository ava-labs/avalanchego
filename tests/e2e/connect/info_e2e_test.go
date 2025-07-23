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
		tc      = e2e.NewTestContext()
		env     = e2e.GetEnv(tc)
		require = require.New(tc)
		client  infov1connect.InfoServiceClient
		ctx     = tc.DefaultContext()
	)

	ginkgo.BeforeAll(func() {
		rootDir := env.RootNetworkDir
		network := tmpnet.NewDefaultNetwork("connect-info-e2e")

		err := tmpnet.BootstrapNewNetwork(ctx, tc.Log(), network, rootDir)
		require.NoError(err)

		node, err := network.GetNode(ids.BuildTestNodeID([]byte("node-0")))
		require.NoError(err)
		e2e.WaitForHealthy(tc, node)
		url := node.GetAccessibleURI()
		client = infov1connect.NewInfoServiceClient(http.DefaultClient, url)
	})

	ginkgo.It("NodeVersion returns version info", func() {
		req := connect.NewRequest(&infov1.NodeVersionRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NodeVersion(ctx, req)
		require.NoError(err)
		require.NotEmpty(resp.Msg.Version)
	})

	ginkgo.It("NodeID returns a node ID", func() {
		req := connect.NewRequest(&infov1.NodeIDRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NodeID(ctx, req)
		require.NoError(err)
		require.True(strings.HasPrefix(resp.Msg.NodeId, "NodeID-"))
	})

	ginkgo.It("NetworkID returns a network ID", func() {
		req := connect.NewRequest(&infov1.NetworkIDRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NetworkID(ctx, req)
		require.NoError(err)
		require.Positive(resp.Msg.NetworkId)
	})

	ginkgo.It("NetworkName returns a network name", func() {
		req := connect.NewRequest(&infov1.NetworkNameRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NetworkName(ctx, req)
		require.NoError(err)
		require.NotEmpty(resp.Msg.NetworkName)
	})

	ginkgo.It("NodeIP returns an IP", func() {
		req := connect.NewRequest(&infov1.NodeIPRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NodeIP(ctx, req)
		require.NoError(err)
		require.NotEmpty(resp.Msg.Ip)
	})

	ginkgo.It("BlockchainID returns a blockchain ID for X", func() {
		req := connect.NewRequest(&infov1.BlockchainIDRequest{Alias: "X"})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.BlockchainID(ctx, req)
		require.NoError(err)
		require.NotEmpty(resp.Msg.BlockchainId)
	})

	ginkgo.It("Peers returns a list", func() {
		req := connect.NewRequest(&infov1.PeersRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.Peers(ctx, req)
		require.NoError(err)
		require.NotEmpty(resp.Msg.Peers)
	})

	ginkgo.It("IsBootstrapped returns true for X", func() {
		tc.Eventually(func() bool {
			req := connect.NewRequest(&infov1.IsBootstrappedRequest{Chain: "X"})
			req.Header().Set("Avalanche-API-Route", "info")
			resp, err := client.IsBootstrapped(ctx, req)
			return err == nil && resp.Msg.IsBootstrapped
		}, 60*time.Second, 2*time.Second, "node should eventually bootstrap")
	})

	ginkgo.It("Upgrades returns a response", func() {
		req := connect.NewRequest(&infov1.UpgradesRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.Upgrades(ctx, req)
		require.NoError(err)
		require.NotNil(resp.Msg)
	})

	ginkgo.It("Uptime returns a response", func() {
		req := connect.NewRequest(&infov1.UptimeRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.Uptime(ctx, req)
		require.NoError(err)
		require.NotNil(resp.Msg)
	})

	ginkgo.It("VMs returns at least avm", func() {
		req := connect.NewRequest(&infov1.VMsRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.VMs(ctx, req)
		require.NoError(err)
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
		require.True(found)
	})
})
