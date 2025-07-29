// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"net/http"
	"connectrpc.com/connect"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/connectclient"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"

	infopb "github.com/ava-labs/avalanchego/connectproto/pb/info"
	connectinfopb "github.com/ava-labs/avalanchego/connectproto/pb/info/infoconnect"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

var _ = ginkgo.Describe("[Info Service]", ginkgo.Ordered, func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	var (
		network *tmpnet.Network
		node    *tmpnet.Node
		nodeID ids.NodeID
		client  connectinfopb.InfoServiceClient
	)

	ginkgo.BeforeAll(func() {
		network = e2e.GetEnv(tc).GetNetwork()
		node = network.Nodes[0]
		nodeID = node.NodeID
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
		tc.Log().Info("calling GetNodeVersion")
		req := connect.NewRequest(&infopb.GetNodeVersionRequest{})
		resp, err := client.GetNodeVersion(tc.DefaultContext(), req)
		require.NoError(err)
		require.NotEmpty(resp.Msg.Version)
	})

	ginkgo.It("serves GetNodeID", func() {
		tc.Log().Info("calling GetNodeID")
		req := connect.NewRequest(&infopb.GetNodeIDRequest{})
		resp, err := client.GetNodeID(tc.DefaultContext(), req)
		require.NoError(err)

		gotNodeID, err := ids.NodeIDFromString(resp.Msg.NodeId)
		require.NoError(err)
		require.Equal(nodeID, gotNodeID)
	})

	ginkgo.It("serves GetNetworkID", func() {
		tc.Log().Info("calling GetNetworkID")
		req := connect.NewRequest(&infopb.GetNetworkIDRequest{})
		resp, err := client.GetNetworkID(tc.DefaultContext(), req)
		require.NoError(err)
		require.Equal(constants.LocalID, resp.Msg.NetworkId)
	})

	ginkgo.It("serves GetNetworkName", func() {
		tc.Log().Info("calling GetNetworkName")
		req := connect.NewRequest(&infopb.GetNetworkNameRequest{})
		resp, err := client.GetNetworkName(tc.DefaultContext(), req)
		require.NoError(err)
		require.Equal("local", resp.Msg.NetworkName)
	})

	ginkgo.It("serves GetNodeIP", func() {
		tc.Log().Info("calling GetNodeIP")
		req := connect.NewRequest(&infopb.GetNodeIPRequest{})
		resp, err := client.GetNodeIP(tc.DefaultContext(), req)
		require.NoError(err)
		require.Equal("127.0.0.1", resp.Msg.Ip)
	})

	ginkgo.It("serves GetChainID", func() {
		tc.Log().Info("calling GetChainID")
		req := connect.NewRequest(&infopb.GetChainIDRequest{Alias: "X"})
		resp, err := client.GetChainID(tc.DefaultContext(), req)
		require.NoError(err)
		// TODO
		require.NotEmpty(resp.Msg.ChainId)
	})

	ginkgo.It("serves GetPeers", func() {
		tc.Log().Info("calling GetPeers")
		req := connect.NewRequest(&infopb.GetPeersRequest{})
		resp, err := client.GetPeers(tc.DefaultContext(), req)
		require.NoError(err)
		// TODO
		require.NotEmpty(resp.Msg.Peers)
	})

	ginkgo.It("serves GetBootstrapped", func() {
		tc.Log().Info("calling GetBootstrapped")
		req := connect.NewRequest(&infopb.GetBootstrappedRequest{Chain: "X"})
		//TODO
		_, err := client.GetBootstrapped(tc.DefaultContext(), req)
		require.NoError(err)
	})

	ginkgo.It("serves GetUpgrades", func() {
		tc.Log().Info("calling GetUpgrades")
		req := connect.NewRequest(&infopb.GetUpgradesRequest{})
		resp, err := client.GetUpgrades(tc.DefaultContext(), req)
		require.NoError(err)
		require.NotNil(resp.Msg)
	})

	ginkgo.It("serves GetUptime", func() {
		tc.Log().Info("calling GetUptime")
		req := connect.NewRequest(&infopb.GetUptimeRequest{})
		resp, err := client.GetUptime(tc.DefaultContext(), req)
		require.NoError(err)
		require.NotNil(resp.Msg)
	})

	// TODO acps test

	ginkgo.It("serves GetVMs", func() {
		tc.Log().Info("calling GetVMs")
		req := connect.NewRequest(&infopb.GetVMsRequest{})
		resp, err := client.GetVMs(tc.DefaultContext(), req)
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
