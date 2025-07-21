// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package connect_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/ava-labs/avalanchego/proto/pb/info/v1/infov1connect"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"

	infov1 "github.com/ava-labs/avalanchego/proto/pb/info/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	client     infov1connect.InfoServiceClient
	ctx        context.Context
	httpClient *http.Client
	nodeURI    string
)

func TestInfoE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Info E2E Suite")
}

var _ = BeforeSuite(func() {
	ctx = context.Background()
	tc := e2e.NewTestContext()
	env := e2e.GetEnv(tc)
	uris := env.GetNodeURIs()
	nodeURI = uris[0].URI

	httpClient = &http.Client{Timeout: 10 * time.Second}
	client = infov1connect.NewInfoServiceClient(httpClient, nodeURI)
})

var _ = Describe("InfoService ConnectRPC E2E", func() {
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

	It("Peers returns a list", func() {
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
		Expect(found).To(BeTrue())
	})
})
