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
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"

	infopb "github.com/ava-labs/avalanchego/connectproto/pb/info"
	connectinfopb "github.com/ava-labs/avalanchego/connectproto/pb/info/infoconnect"
)

var _ = ginkgo.Describe("[Connect Info API]", func() {
	var (
		tc      = e2e.NewTestContext()
		env     = e2e.GetEnv(tc)
		require = require.New(tc)
		client  connectinfopb.InfoServiceClient
		ctx     = tc.DefaultContext()
	)

	ginkgo.BeforeAll(func() {
		rootDir := env.RootNetworkDir
		network := tmpnet.NewDefaultNetwork("connect-info-e2e")
		require.NoError(tmpnet.BootstrapNewNetwork(ctx, tc.Log(), network, rootDir))

		node, err := network.GetNode(ids.BuildTestNodeID([]byte("node-0")))
		require.NoError(err)
		e2e.WaitForHealthy(tc, node)
		uri := node.GetAccessibleURI()
		client = connectinfopb.NewInfoServiceClient(http.DefaultClient, uri)
	})

	ginkgo.It("NodeVersion returns version info", func() {
		req := connect.NewRequest(&infopb.GetNodeVersionRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetNodeVersion(ctx, req)
		require.NoError(err)
		require.NotEmpty(resp.Msg.Version)
	})

	ginkgo.It("NodeID returns a node ID", func() {
		req := connect.NewRequest(&infopb.GetNodeIDRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetNodeID(ctx, req)
		require.NoError(err)
		require.True(strings.HasPrefix(resp.Msg.NodeId, "NodeID-"))
	})

	ginkgo.It("NetworkID returns a network ID", func() {
		req := connect.NewRequest(&infopb.GetNetworkIDRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetNetworkID(ctx, req)
		require.NoError(err)
		require.Positive(resp.Msg.NetworkId)
	})

	ginkgo.It("NetworkName returns a network name", func() {
		req := connect.NewRequest(&infopb.GetNetworkNameRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetNetworkName(ctx, req)
		require.NoError(err)
		require.NotEmpty(resp.Msg.NetworkName)
	})

	ginkgo.It("NodeIP returns an IP", func() {
		req := connect.NewRequest(&infopb.GetNodeIPRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetNodeIP(ctx, req)
		require.NoError(err)
		require.NotEmpty(resp.Msg.Ip)
	})

	ginkgo.It("BlockchainID returns a blockchain ID for X", func() {
		req := connect.NewRequest(&infopb.GetChainIDRequest{Alias: "X"})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetChainID(ctx, req)
		require.NoError(err)
		require.NotEmpty(resp.Msg.ChainId)
	})

	ginkgo.It("Peers returns a list", func() {
		req := connect.NewRequest(&infopb.GetPeersRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetPeers(ctx, req)
		require.NoError(err)
		require.NotEmpty(resp.Msg.Peers)
	})

	ginkgo.It("IsBootstrapped returns true for X", func() {
		tc.Eventually(func() bool {
			req := connect.NewRequest(&infopb.GetBootstrappedRequest{Chain: "X"})
			req.Header().Set("Avalanche-API-Route", "info")
			resp, err := client.GetBootstrapped(ctx, req)
			return err == nil && resp.Msg.Bootstrapped
		}, 60*time.Second, 2*time.Second, "node should eventually bootstrap")
	})

	ginkgo.It("Upgrades returns a response", func() {
		req := connect.NewRequest(&infopb.GetUpgradesRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetUpgrades(ctx, req)
		require.NoError(err)
		require.NotNil(resp.Msg)
	})

	ginkgo.It("Uptime returns a response", func() {
		req := connect.NewRequest(&infopb.GetUptimeRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetUptime(ctx, req)
		require.NoError(err)
		require.NotNil(resp.Msg)
	})

	// TODO acps test

	ginkgo.It("VMs returns at least avm", func() {
		req := connect.NewRequest(&infopb.GetVMsRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetVMs(ctx, req)
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
