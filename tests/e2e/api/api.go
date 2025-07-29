// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/connectclient"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"

	infopb "github.com/ava-labs/avalanchego/connectproto/pb/info"
	connectinfopb "github.com/ava-labs/avalanchego/connectproto/pb/info/infoconnect"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var _ = ginkgo.Describe("[Info Service]", func() {
	var (
		tc      = e2e.NewTestContext()
		env     = e2e.GetEnv(tc)
		require = require.New(tc)
		client  connectinfopb.InfoServiceClient
		ctx     = tc.DefaultContext()
		nodeID ids.NodeID
	)

	ginkgo.BeforeAll(func() {
		rootDir := env.RootNetworkDir
		network := tmpnet.NewDefaultNetwork("info-service-test")
		require.NoError(tmpnet.BootstrapNewNetwork(ctx, tc.Log(), network, rootDir))

		var err error
		nodeID, err = ids.NodeIDFromString(network.GetAvailableNodeIDs()[0])
		require.NoError(err)
		node, err := network.GetNode(nodeID)
		require.NoError(err)

		e2e.WaitForHealthy(tc, node)

		uri := node.GetAccessibleURI()
		client = connectinfopb.NewInfoServiceClient(
			http.DefaultClient,
			uri,
			connect.WithInterceptors(
				connectclient.SetRouteHeaderInterceptor{Route: "info"},
			),
		)
	})

	ginkgo.It("serves GetNodeVersion", func() {
		req := connect.NewRequest(&infopb.GetNodeVersionRequest{})
		resp, err := client.GetNodeVersion(ctx, req)
		require.NoError(err)
		require.NotEmpty(resp.Msg.Version)
	})

	ginkgo.It("serves GetNodeID", func() {
		req := connect.NewRequest(&infopb.GetNodeIDRequest{})
		resp, err := client.GetNodeID(ctx, req)
		require.NoError(err)

		gotNodeID, err := ids.NodeIDFromString(resp.Msg.NodeId)
		require.NoError(err)
		require.Equal(nodeID, gotNodeID)
	})

	ginkgo.It("serves GetNetworkID", func() {
		req := connect.NewRequest(&infopb.GetNetworkIDRequest{})
		resp, err := client.GetNetworkID(ctx, req)
		require.NoError(err)
		require.Equal(constants.LocalID, resp.Msg.NetworkId)
	})

	ginkgo.It("serves GetNetworkName", func() {
		req := connect.NewRequest(&infopb.GetNetworkNameRequest{})
		resp, err := client.GetNetworkName(ctx, req)
		require.NoError(err)
		require.Equal("local", resp.Msg.NetworkName)
	})

	ginkgo.It("serves GetNodeIP", func() {
		req := connect.NewRequest(&infopb.GetNodeIPRequest{})
		resp, err := client.GetNodeIP(ctx, req)
		require.NoError(err)
		require.Equal("127.0.0.1", resp.Msg.Ip)
	})

	ginkgo.It("serves GetBlockchainID", func() {
		req := connect.NewRequest(&infopb.GetChainIDRequest{Alias: "X"})
		resp, err := client.GetChainID(ctx, req)
		require.NoError(err)
		// TODO
		require.NotEmpty(resp.Msg.ChainId)
	})

	ginkgo.It("serves GetPeers", func() {
		req := connect.NewRequest(&infopb.GetPeersRequest{})
		resp, err := client.GetPeers(ctx, req)
		require.NoError(err)
		// TODO
		require.NotEmpty(resp.Msg.Peers)
	})

	ginkgo.It("serves GetBootstrapped", func() {
		tc.Eventually(func() bool {
			req := connect.NewRequest(&infopb.GetBootstrappedRequest{Chain: "X"})
			resp, err := client.GetBootstrapped(ctx, req)
			return err == nil && resp.Msg.Bootstrapped
		}, 60*time.Second, 2*time.Second, "node should eventually bootstrap")
	})

	ginkgo.It("serves GetUpgrades", func() {
		req := connect.NewRequest(&infopb.GetUpgradesRequest{})
		resp, err := client.GetUpgrades(ctx, req)
		require.NoError(err)
		require.NotNil(resp.Msg)
	})

	ginkgo.It("serves GetUptime", func() {
		req := connect.NewRequest(&infopb.GetUptimeRequest{})
		resp, err := client.GetUptime(ctx, req)
		require.NoError(err)
		require.NotNil(resp.Msg)
	})

	// TODO acps test

	ginkgo.It("serves GetVMs", func() {
		req := connect.NewRequest(&infopb.GetVMsRequest{})
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
