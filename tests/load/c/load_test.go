// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"os"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// Run this using the command from the root of the repository in a Nix develop shell:
// task test-load
func TestLoad(t *testing.T) {
	ginkgo.RunSpecs(t, "load tests")
}

var flagVars *e2e.FlagVars

func init() {
	flagVars = e2e.RegisterFlagsWithDefaultOwner("avalanchego-load")
}

const (
	blockchainID  = "C"
	nodesCount    = 5
	agentsPerNode = 50
	agentsCount   = nodesCount * agentsPerNode
)

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	// Run only once in the first ginkgo process

	require.GreaterOrEqual(ginkgo.GinkgoT(), nodesCount, 5, "number of nodes must be at least 5")
	tc := e2e.NewTestContext()
	nodes := tmpnet.NewNodesOrPanic(nodesCount)
	network := &tmpnet.Network{
		Owner: "avalanchego-load-test",
		Nodes: nodes,
	}
	setPrefundedKeys(tc, network, agentsCount)

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
		network *tmpnet.Network
		metrics *load.Metrics

		logger logging.Logger
	)

	ginkgo.BeforeAll(func() {
		tc := e2e.NewTestContext()
		logger = tc.Log()
		env := e2e.GetEnv(tc)
		registry := prometheus.NewRegistry()

		network = env.GetNetwork()

		loadMetrics, err := load.NewMetrics(registry)
		require.NoError(tc, err, "failed to register load metrics")
		metrics = loadMetrics

		metricsServer := load.NewPrometheusServer("127.0.0.1:0", registry)
		metricsErrCh, err := metricsServer.Start()
		require.NoError(tc, err, "failed to start load metrics server")

		monitoringConfigFilePath, err := metricsServer.GenerateMonitoringConfig(network.UUID, network.Owner)
		require.NoError(tc, err, "failed to generate monitoring config file")

		ginkgo.DeferCleanup(func() {
			select {
			case err := <-metricsErrCh:
				require.NoError(tc, err, "metrics server exited with error")
			default:
				require.NoError(tc, metricsServer.Stop(), "failed to stop metrics server")
			}
		})
		ginkgo.DeferCleanup(func() {
			require.NoError(tc, os.Remove(monitoringConfigFilePath), "failed to remove monitoring config file")
		})
	})

	ginkgo.It("C-Chain simple", func(ctx context.Context) {
		endpoints, err := tmpnet.GetNodeWebsocketURIs(network.Nodes, blockchainID)
		require.NoError(ginkgo.GinkgoT(), err, "getting node websocket URIs")
		config := loadConfig{
			endpoints: endpoints,
			issuer:    issuerSimple,
			maxFeeCap: 4761904, // max fee cap equivalent to 100 ether
			agents:    agentsPerNode,
			minTPS:    2400,
			maxTPS:    3000,
			step:      100,
		}
		err = execute(ctx, network.PreFundedKeys, config, metrics, logger)
		require.NoError(ginkgo.GinkgoT(), err, "executing load test")
	})

	ginkgo.It("C-Chain opcoder", func(ctx context.Context) {
		endpoints, err := tmpnet.GetNodeWebsocketURIs(network.Nodes, blockchainID)
		require.NoError(ginkgo.GinkgoT(), err, "getting node websocket URIs")
		config := loadConfig{
			endpoints: endpoints,
			issuer:    issuerOpcoder,
			maxFeeCap: 300000000000,
			agents:    agentsCount,
			minTPS:    1000,
			maxTPS:    1600,
			step:      50,
		}
		err = execute(ctx, network.PreFundedKeys, config, metrics, logger)
		require.NoError(ginkgo.GinkgoT(), err, "executing load test")
	})
})

// setPrefundedKeys sets the pre-funded keys for the network, and keeps
// keys already set if any. If there are more keys than required, it
// keeps the already set keys as they are.
func setPrefundedKeys(t require.TestingT, network *tmpnet.Network, minKeys int) {
	if len(network.PreFundedKeys) >= minKeys {
		return
	}
	missingPreFundedKeys, err := tmpnet.NewPrivateKeys(minKeys - len(network.PreFundedKeys))
	require.NoError(t, err, "creating pre-funded keys")
	network.PreFundedKeys = append(network.PreFundedKeys, missingPreFundedKeys...)
}
