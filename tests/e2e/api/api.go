// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/connectclient"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"

	infopb "github.com/ava-labs/avalanchego/connectproto/pb/info"
	connectinfopb "github.com/ava-labs/avalanchego/connectproto/pb/info/infoconnect"
)

var _ = ginkgo.Describe("[Info]", ginkgo.Ordered, func() {
	var (
		tc      = e2e.NewTestContext()
		require = require.New(tc)
		network *tmpnet.Network
		node    *tmpnet.Node
		nodeID  ids.NodeID
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
		request := connect.NewRequest(&infopb.GetNodeVersionRequest{})
		response, err := client.GetNodeVersion(tc.DefaultContext(), request)
		require.NoError(err)

		require.NotEmpty(response.Msg.Version)
	})

	ginkgo.It("serves GetNodeID", func() {
		request := connect.NewRequest(&infopb.GetNodeIDRequest{})
		response, err := client.GetNodeID(tc.DefaultContext(), request)
		require.NoError(err)

		require.Equal(nodeID.String(), response.Msg.NodeId)
	})

	ginkgo.It("serves GetNetworkID", func() {
		request := connect.NewRequest(&infopb.GetNetworkIDRequest{})
		response, err := client.GetNetworkID(tc.DefaultContext(), request)
		require.NoError(err)

		require.Equal(network.GetNetworkID(), response.Msg.NetworkId)
	})

	ginkgo.It("serves GetNetworkName", func() {
		request := connect.NewRequest(&infopb.GetNetworkNameRequest{})
		response, err := client.GetNetworkName(tc.DefaultContext(), request)
		require.NoError(err)

		wantNetworkName := fmt.Sprintf("network-%d", network.GetNetworkID())
		require.Equal(wantNetworkName, response.Msg.NetworkName)
	})

	ginkgo.It("serves GetNodeIP", func() {
		request := connect.NewRequest(&infopb.GetNodeIPRequest{})
		response, err := client.GetNodeIP(tc.DefaultContext(), request)
		require.NoError(err)

		require.Equal(node.StakingAddress.String(), response.Msg.Ip)
	})

	ginkgo.It("serves GetChainID", func() {
		request := connect.NewRequest(&infopb.GetChainIDRequest{Alias: "P"})
		response, err := client.GetChainID(tc.DefaultContext(), request)
		require.NoError(err)

		require.Equal(ids.Empty.String(), response.Msg.ChainId)
	})

	ginkgo.It("serves GetPeers", func() {
		request := connect.NewRequest(&infopb.GetPeersRequest{})
		response, err := client.GetPeers(tc.DefaultContext(), request)
		require.NoError(err)

		require.Len(response.Msg.Peers, len(network.Nodes)-1)
	})

	ginkgo.It("serves GetBootstrapped", func() {
		request := connect.NewRequest(&infopb.GetBootstrappedRequest{Chain: "P"})
		response, err := client.GetBootstrapped(tc.DefaultContext(), request)
		require.NoError(err)

		require.True(response.Msg.Bootstrapped)
	})

	ginkgo.It("serves GetUpgrades", func() {
		request := connect.NewRequest(&infopb.GetUpgradesRequest{})
		response, err := client.GetUpgrades(tc.DefaultContext(), request)
		require.NoError(err)

		require.NotZero(response.Msg)
	})

	ginkgo.It("serves GetUptime", func() {
		request := connect.NewRequest(&infopb.GetUptimeRequest{})
		response, err := client.GetUptime(tc.DefaultContext(), request)
		require.NoError(err)

		require.NotZero(response.Msg.RewardingStakePercentage)
		require.NotZero(response.Msg.WeightedAveragePercentage)
	})

	ginkgo.It("serves GetACPs", func() {
		request := connect.NewRequest(&infopb.GetACPsRequest{})
		response, err := client.GetACPs(tc.DefaultContext(), request)
		require.NoError(err)

		require.NotZero(response.Msg.Acps)
	})

	ginkgo.It("serves GetVMs", func() {
		request := connect.NewRequest(&infopb.GetVMsRequest{})
		response, err := client.GetVMs(tc.DefaultContext(), request)
		require.NoError(err)

		gotVMAliases := make([]string, 0)
		for _, aliases := range response.Msg.Vms {
			gotVMAliases = append(gotVMAliases, aliases.Aliases...)
		}

		require.ElementsMatch([]string{"avm", "evm", "platform"}, gotVMAliases)

		gotFXAliases := make([]string, 0)
		for _, alias := range response.Msg.Fxs {
			gotFXAliases = append(gotFXAliases, alias)
		}

		require.ElementsMatch(
			[]string{"nftfx", "propertyfx", "secp256k1fx"},
			gotFXAliases,
		)
	})
})
