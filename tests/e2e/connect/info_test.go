package connect_test

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/ava-labs/avalanchego/proto/pb/info/v1"
	"github.com/ava-labs/avalanchego/proto/pb/info/v1/infov1connect"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("InfoService ConnectRPC Smoke Tests", func() {
	var (
		client infov1connect.InfoServiceClient
		ctx    context.Context
	)

	// Before the suite runs, wait for the node's health endpoint
	BeforeSuite(func() {
		baseHealthURL := "http://127.0.0.1:9650/ext/health/health"
		deadline := time.Now().Add(15 * time.Second)
		for {
			if time.Now().After(deadline) {
				Fail(fmt.Sprintf("timed out waiting for health endpoint at %s", baseHealthURL))
			}
			resp, err := http.Get(baseHealthURL)
			if err == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	})

	BeforeEach(func() {
		ctx = context.Background()
		baseURL := "http://127.0.0.1:9650/ext/info"
		httpClient := &http.Client{Timeout: 5 * time.Second}
		client = infov1connect.NewInfoServiceClient(httpClient, baseURL)
	})

	It("GetNodeVersion should return a non-empty version", func() {
		resp, err := client.GetNodeVersion(ctx, connect.NewRequest(&infov1.GetNodeVersionRequest{}))
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.Version).ToNot(BeEmpty())
	})

	It("GetNodeID should return a NodeID- prefix", func() {
		resp, err := client.GetNodeID(ctx, connect.NewRequest(&infov1.GetNodeIDRequest{}))
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.NodeId).To(ContainSubstring("NodeID-"))
	})

	It("GetNetworkID should return an ID > 0", func() {
		resp, err := client.GetNetworkID(ctx, connect.NewRequest(&infov1.GetNetworkIDRequest{}))
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.NetworkId).To(BeNumerically(">", 0))
	})

	It("GetBlockchainID should return a valid ID for alias X", func() {
		resp, err := client.GetBlockchainID(ctx, connect.NewRequest(&infov1.GetBlockchainIDRequest{Alias: "X"}))
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.BlockchainId).ToNot(BeEmpty())
	})

	It("Peers should return NumPeers >= 0", func() {
		resp, err := client.Peers(ctx, connect.NewRequest(&infov1.PeersRequest{}))
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Msg.NumPeers).To(BeNumerically(">=", 0))
	})
})
