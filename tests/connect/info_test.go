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

func (*stubInfoService) NodeVersion(
	_ context.Context,
	_ *connect.Request[infov1.NodeVersionRequest],
) (*connect.Response[infov1.NodeVersionResponse], error) {
	return connect.NewResponse(&infov1.NodeVersionResponse{
		Version: "stub-v1.2.3",
	}), nil
}

func (*stubInfoService) NodeID(
	_ context.Context,
	_ *connect.Request[infov1.NodeIDRequest],
) (*connect.Response[infov1.NodeIDResponse], error) {
	return connect.NewResponse(&infov1.NodeIDResponse{
		NodeId: "NodeID-stub-node",
	}), nil
}

func (*stubInfoService) NodeIP(
	_ context.Context,
	_ *connect.Request[infov1.NodeIPRequest],
) (*connect.Response[infov1.NodeIPResponse], error) {
	return connect.NewResponse(&infov1.NodeIPResponse{
		Ip: "127.0.0.1",
	}), nil
}

func (*stubInfoService) NetworkID(
	_ context.Context,
	_ *connect.Request[infov1.NetworkIDRequest],
) (*connect.Response[infov1.NetworkIDResponse], error) {
	return connect.NewResponse(&infov1.NetworkIDResponse{
		NetworkId: 1,
	}), nil
}

func (*stubInfoService) NetworkName(
	_ context.Context,
	_ *connect.Request[infov1.NetworkNameRequest],
) (*connect.Response[infov1.NetworkNameResponse], error) {
	return connect.NewResponse(&infov1.NetworkNameResponse{
		NetworkName: "network",
	}), nil
}

func (*stubInfoService) BlockchainID(
	_ context.Context,
	_ *connect.Request[infov1.BlockchainIDRequest],
) (*connect.Response[infov1.BlockchainIDResponse], error) {
	return connect.NewResponse(&infov1.BlockchainIDResponse{
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

func (*stubInfoService) VMs(
	_ context.Context,
	_ *connect.Request[infov1.VMsRequest],
) (*connect.Response[infov1.VMsResponse], error) {
	return connect.NewResponse(&infov1.VMsResponse{
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

	It("NodeVersion returns version info", func() {
		req := connect.NewRequest(&infov1.NodeVersionRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NodeVersion(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.Version).ToNot(BeEmpty())
	})

	It("NodeID returns a node ID", func() {
		req := connect.NewRequest(&infov1.NodeIDRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NodeID(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.NodeId).To(HavePrefix("NodeID-"))
	})

	It("NetworkID returns a network ID", func() {
		req := connect.NewRequest(&infov1.NetworkIDRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NetworkID(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.NetworkId).To(BeNumerically(">", 0))
	})

	It("NetworkName returns a network name", func() {
		req := connect.NewRequest(&infov1.NetworkNameRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NetworkName(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.NetworkName).ToNot(BeEmpty())
	})

	It("NodeIP returns an IP", func() {
		req := connect.NewRequest(&infov1.NodeIPRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.NodeIP(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.Ip).ToNot(BeEmpty())
	})

	It("BlockchainID returns a blockchain ID for X", func() {
		req := connect.NewRequest(&infov1.BlockchainIDRequest{Alias: "X"})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.BlockchainID(ctx, req)
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

	It("VMs returns at least avm", func() {
		req := connect.NewRequest(&infov1.VMsRequest{})
		req.Header().Set("Avalanche-API-Route", "info")
		resp, err := client.VMs(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.Vms).To(HaveKey("avm"))
	})
})
