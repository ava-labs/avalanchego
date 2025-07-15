// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package connect_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/ava-labs/avalanchego/proto/pb/info/v1/infov1connect"

	infov1 "github.com/ava-labs/avalanchego/proto/pb/info/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type stubInfoService struct{}

func (*stubInfoService) GetNodeVersion(
	_ context.Context,
	_ *connect.Request[infov1.GetNodeVersionRequest],
) (*connect.Response[infov1.GetNodeVersionResponse], error) {
	return connect.NewResponse(&infov1.GetNodeVersionResponse{
		Version: "stub-v1.2.3",
	}), nil
}

func (*stubInfoService) GetNodeID(
	_ context.Context,
	_ *connect.Request[infov1.GetNodeIDRequest],
) (*connect.Response[infov1.GetNodeIDResponse], error) {
	return connect.NewResponse(&infov1.GetNodeIDResponse{
		NodeId: "NodeID-stub-node",
	}), nil
}

func (*stubInfoService) GetNodeIP(
	_ context.Context,
	_ *connect.Request[infov1.GetNodeIPRequest],
) (*connect.Response[infov1.GetNodeIPResponse], error) {
	return connect.NewResponse(&infov1.GetNodeIPResponse{
		Ip: "127.0.0.1",
	}), nil
}

func (*stubInfoService) GetNetworkID(
	_ context.Context,
	_ *connect.Request[infov1.GetNetworkIDRequest],
) (*connect.Response[infov1.GetNetworkIDResponse], error) {
	return connect.NewResponse(&infov1.GetNetworkIDResponse{
		NetworkId: 1,
	}), nil
}

func (*stubInfoService) GetNetworkName(
	_ context.Context,
	_ *connect.Request[infov1.GetNetworkNameRequest],
) (*connect.Response[infov1.GetNetworkNameResponse], error) {
	return connect.NewResponse(&infov1.GetNetworkNameResponse{
		NetworkName: "network",
	}), nil
}

func (*stubInfoService) GetBlockchainID(
	_ context.Context,
	_ *connect.Request[infov1.GetBlockchainIDRequest],
) (*connect.Response[infov1.GetBlockchainIDResponse], error) {
	return connect.NewResponse(&infov1.GetBlockchainIDResponse{
		BlockchainId: "blockchain",
	}), nil
}

func (*stubInfoService) Peers(
	_ context.Context,
	_ *connect.Request[infov1.PeersRequest],
) (*connect.Response[infov1.PeersResponse], error) {
	return connect.NewResponse(&infov1.PeersResponse{
		NumPeers: 0,
		Peers:    []*infov1.PeerInfo{},
	}), nil
}

func (*stubInfoService) IsBootstrapped(
	_ context.Context,
	_ *connect.Request[infov1.IsBootstrappedRequest],
) (*connect.Response[infov1.IsBootstrappedResponse], error) {
	return connect.NewResponse(&infov1.IsBootstrappedResponse{
		IsBootstrapped: true,
	}), nil
}

func (*stubInfoService) Upgrades(
	_ context.Context,
	_ *connect.Request[infov1.UpgradesRequest],
) (*connect.Response[infov1.UpgradesResponse], error) {
	return connect.NewResponse(&infov1.UpgradesResponse{}), nil
}

func (*stubInfoService) Uptime(
	_ context.Context,
	_ *connect.Request[infov1.UptimeRequest],
) (*connect.Response[infov1.UptimeResponse], error) {
	return connect.NewResponse(&infov1.UptimeResponse{}), nil
}

func (*stubInfoService) GetVMs(
	_ context.Context,
	_ *connect.Request[infov1.GetVMsRequest],
) (*connect.Response[infov1.GetVMsResponse], error) {
	return connect.NewResponse(&infov1.GetVMsResponse{
		Vms: map[string]*infov1.VMAliases{
			"avm": {Aliases: []string{"avm"}},
		},
		Fxs: map[string]string{},
	}), nil
}

var _ = Describe("InfoService ConnectRPC E2E (integration, no stubs)", func() {
	var (
		client     infov1connect.InfoServiceClient
		ctx        context.Context
		httpClient *http.Client
		srv        *httptest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		stub := &stubInfoService{}
		pattern, handler := infov1connect.NewInfoServiceHandler(stub)

		mux := http.NewServeMux()
		mux.Handle(pattern, handler)

		// Start h2c server for testing ConnectRPC with HTTP/2
		srv = httptest.NewUnstartedServer(h2c.NewHandler(mux, &http2.Server{}))
		srv.EnableHTTP2 = true
		srv.Start()

		httpClient = &http.Client{Timeout: 5 * time.Second}
		client = infov1connect.NewInfoServiceClient(httpClient, srv.URL)
	})

	AfterEach(func() {
		if srv != nil {
			srv.Close()
		}
	})

	It("GetNodeVersion returns version info", func() {
		req := connect.NewRequest(&infov1.GetNodeVersionRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetNodeVersion(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.Version).ToNot(BeEmpty())
	})

	It("GetNodeID returns a node ID", func() {
		req := connect.NewRequest(&infov1.GetNodeIDRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetNodeID(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.NodeId).To(HavePrefix("NodeID-"))
	})

	It("GetNetworkID returns a network ID", func() {
		req := connect.NewRequest(&infov1.GetNetworkIDRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetNetworkID(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.NetworkId).To(BeNumerically(">", 0))
	})

	It("GetNetworkName returns a network name", func() {
		req := connect.NewRequest(&infov1.GetNetworkNameRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetNetworkName(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.NetworkName).ToNot(BeEmpty())
	})

	It("GetNodeIP returns an IP", func() {
		req := connect.NewRequest(&infov1.GetNodeIPRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetNodeIP(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.Ip).ToNot(BeEmpty())
	})

	It("GetBlockchainID returns a blockchain ID for X", func() {
		req := connect.NewRequest(&infov1.GetBlockchainIDRequest{Alias: "X"})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetBlockchainID(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.BlockchainId).ToNot(BeEmpty())
	})

	It("Peers returns a list (may be empty)", func() {
		req := connect.NewRequest(&infov1.PeersRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.Peers(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.NumPeers).To(BeNumerically(">=", 0))
	})

	It("IsBootstrapped returns true for X", func() {
		Eventually(func() bool {
			req := connect.NewRequest(&infov1.IsBootstrappedRequest{Chain: "X"})
			req.Header().Set("Avalanche-API-Route", "info")
			resp, err := client.IsBootstrapped(ctx, req)
			return err == nil && resp.Msg.IsBootstrapped
		}, 60*time.Second, 2*time.Second).Should(BeTrue())
	})

	It("Upgrades returns a response", func() {
		req := connect.NewRequest(&infov1.UpgradesRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.Upgrades(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg).ToNot(BeNil())
	})

	It("Uptime returns a response", func() {
		req := connect.NewRequest(&infov1.UptimeRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.Uptime(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg).ToNot(BeNil())
	})

	It("GetVMs returns at least avm", func() {
		req := connect.NewRequest(&infov1.GetVMsRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.GetVMs(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.Vms).To(HaveKey("avm"))
	})
})
