// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/common"
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
	nodesCount = 3
	// relative to the user home directory
	metricsFilePath = ".tmpnet/prometheus/file_sd_configs/load-test.json"
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
		network    *tmpnet.Network
		tracker    *load.Tracker[common.Hash]
		logger     = logging.NewLogger("c-chain-load-testing", logging.NewWrappedCore(logging.Info, os.Stdout, logging.Auto.ConsoleEncoder()))
		tc         = e2e.NewTestContext()
		r          = require.New(tc)
		registry   = prometheus.NewRegistry()
		metricsURI string
		cleanup    func()
	)

	tracker, err := load.NewTracker[common.Hash](registry)
	r.NoError(err)

	ginkgo.BeforeAll(func() {
		tc := e2e.NewTestContext()
		r := require.New(tc)
		env := e2e.GetEnv(tc)

		network = env.GetNetwork()
		network.Nodes = network.Nodes[:nodesCount]
		for _, node := range network.Nodes {
			err := node.EnsureKeys()
			r.NoError(err, "ensuring keys for node %s", node.NodeID)
		}

		metricsURI, cleanup = setupMetricsServer(r, registry, logger)
		writeCollectorConfiguration(r, metricsURI, e2e.GetEnv(tc).GetNetwork().UUID, logger)

		ginkgo.DeferCleanup(cleanup)
	})

	ginkgo.It("C-Chain simple", func(ctx context.Context) {
		const blockchainID = "C"
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
		const blockchainID = "C"
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
func setupMetricsServer(r *require.Assertions, registry *prometheus.Registry, logger logging.Logger) (string, func()) {
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

	// Return a cleanup function
	return metricsURI, func() {
		logger.Info("Stopping metrics server")
		r.NoError(metricsServer.Stop(), "stopping metrics server")
	}
}

// writeCollectorConfiguration generates and writes the Prometheus collector configuration
// so tmpnet can dynamically discover new scrape target via file-based service discovery
func writeCollectorConfiguration(r *require.Assertions, metricsURI, networkUUID string, logger logging.Logger) {
	homedir, err := os.UserHomeDir()
	r.NoError(err, "getting user home directory")

	collectorFilePath := filepath.Join(homedir, metricsFilePath)

	collectorConfig, err := generateCollectorConfig([]string{metricsURI}, networkUUID)
	r.NoError(err, "generating collector configuration")

	err = os.MkdirAll(filepath.Dir(collectorFilePath), 0755)
	r.NoError(err, "creating collector directory")

	err = os.WriteFile(collectorFilePath, collectorConfig, 0644)
	r.NoError(err, "writing collector configuration")

	logger.Info("Collector configuration written",
		zap.String("path", collectorFilePath),
		zap.String("target", metricsURI))

	// Register cleanup for the file
	ginkgo.DeferCleanup(func() {
		r.NoError(os.Remove(collectorFilePath))
	})
}

// generateCollectorConfig creates the Prometheus service discovery configuration
func generateCollectorConfig(targets []string, uuid string) ([]byte, error) {
	return json.MarshalIndent([]map[string]any{
		{
			"targets": targets,
			"labels": map[string]string{
				"network_owner": "load-test",
				"network_uuid":  uuid,
			},
		},
	}, "", "  ")
}
