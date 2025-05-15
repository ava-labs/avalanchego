// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/onsi/ginkgo/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// Run this using the command:
// ./bin/ginkgo -v ./tests/load/c -- --avalanchego-path=$PWD/build/avalanchego
func TestLoad(t *testing.T) {
	ginkgo.RunSpecs(t, "load tests")
}

var flagVars *e2e.FlagVars

func init() {
	flagVars = e2e.RegisterFlagsWithDefaultOwner("avalanchego-load")
}

const (
	blockchainID = "C"
	nodesCount   = 3
)

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	// Run only once in the first ginkgo process

	tc := e2e.NewTestContext()
	nodes := tmpnet.NewNodesOrPanic(nodesCount)
	network := &tmpnet.Network{
		Owner: "avalanchego-load-test",
		Nodes: nodes,
	}

	env := e2e.NewTestEnvironment(
		tc,
		flagVars,
		network,
	)

	return env.Marshal()
}, func(envBytes []byte) {
	// Run in every ginkgo process

	// Initialize the local test environment from the global state
	e2e.InitSharedTestEnvironment(e2e.NewTestContext(), envBytes)
})

var _ = ginkgo.Describe("[Load Simulator]", ginkgo.Ordered, func() {
	var (
		network  *tmpnet.Network
		tc       *e2e.GinkgoTestContext
		tracker  *load.Tracker[common.Hash]
		registry *prometheus.Registry

		logger = logging.NewLogger("c-chain-load-testing", logging.NewWrappedCore(logging.Info, os.Stdout, logging.Auto.ConsoleEncoder()))
	)

	ginkgo.BeforeAll(func() {
		var (
			tc  = e2e.NewTestContext()
			env = e2e.GetEnv(tc)
		)

		network = env.GetNetwork()
		network.Nodes = network.Nodes[:nodesCount]
		for _, node := range network.Nodes {
			err := node.EnsureKeys()
			require.NoError(tc, err, "ensuring keys for node %s", node.NodeID)
		}

		cleanup := setupMetricsServer(tc, registry, network.UUID, network.Owner, logger)
		ginkgo.DeferCleanup(cleanup)
	})

	// Setup metrics server and load tracker before each test run
	// ensuring isolation
	ginkgo.BeforeEach(func() {
		promRegistry := prometheus.NewRegistry()
		registry = promRegistry

		loadTracker, err := load.NewTracker[common.Hash](registry)
		require.NoError(tc, err)
		tracker = loadTracker
	})

	ginkgo.It("C-Chain simple", func(ctx context.Context) {
		endpoints, err := tmpnet.GetNodeWebsocketURIs(network.Nodes, blockchainID)
		require.NoError(ginkgo.GinkgoT(), err, "getting node websocket URIs")
		config := config{
			endpoints: endpoints,
			issuer:    issuerSimple,
			maxFeeCap: 3000,
			agents:    1,
			minTPS:    50,
			maxTPS:    90,
			step:      10,
		}
		err = execute(ctx, tracker, network.PreFundedKeys, config, logger)
		if err != nil {
			ginkgo.GinkgoT().Error(err)
		}
	})

	ginkgo.It("C-Chain opcoder", func(ctx context.Context) {
		endpoints, err := tmpnet.GetNodeWebsocketURIs(network.Nodes, blockchainID)
		require.NoError(ginkgo.GinkgoT(), err, "getting node websocket URIs")
		config := config{
			endpoints: endpoints,
			issuer:    issuerOpcoder,
			maxFeeCap: 300000000000,
			agents:    1,
			minTPS:    30,
			maxTPS:    60,
			step:      5,
		}
		err = execute(ctx, tracker, network.PreFundedKeys, config, logger)
		if err != nil {
			ginkgo.GinkgoT().Error(err)
		}
	})
})

// setupMetricsServer creates Prometheus server with a dynamically allocated port
func setupMetricsServer(tc *e2e.GinkgoTestContext, registry *prometheus.Registry, networkUUID, networkOwner string, logger logging.Logger) func() {
	r := require.New(tc)

	// Allocate a port dynamically
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	r.NoError(err, "allocating dynamic port")

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	r.True(ok, "allocating dynamic port")
	port := tcpAddr.Port
	r.NoError(listener.Close(), "closing listener on port %d", port)

	metricsURI := fmt.Sprintf("127.0.0.1:%d", port)

	metricsServer := load.NewPrometheusServer(metricsURI, registry, logger)
	_, err = metricsServer.Start()
	r.NoError(err, "failed to start metrics server")

	monitoringConfigFilePath, err := metricsServer.GenerateMonitoringConfig(networkUUID, networkOwner)
	r.NoError(err, "failed to generate monitoring config file")

	// Return a cleanup function
	return func() {
		r.NoError(metricsServer.Stop(), "stopping metrics server")
		r.NoError(os.Remove(monitoringConfigFilePath))
	}
}
