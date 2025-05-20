// Co// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
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
	const preFundedKeysCount = 50
	setNetworkGas(tc, network, preFundedKeysCount)

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

		logger = logging.NewLogger("c-chain-load-testing", logging.NewWrappedCore(logging.Info, os.Stdout, logging.Auto.ConsoleEncoder()))
	)

	ginkgo.BeforeAll(func() {
		tc := e2e.NewTestContext()
		env := e2e.GetEnv(tc)
		registry := prometheus.NewRegistry()

		network = env.GetNetwork()
		network.Nodes = network.Nodes[:nodesCount]
		for _, node := range network.Nodes {
			err := node.EnsureKeys()
			require.NoError(tc, err, "ensuring keys for node %s", node.NodeID)
		}

		loadMetrics, err := load.NewMetrics(registry)
		require.NoError(tc, err, "failed to register load metrics")
		metrics = loadMetrics

		metricsServer := load.NewPrometheusServer("127.0.0.1:0", registry, logger)
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
			agents:    30,
			minTPS:    95,
			maxTPS:    150,
			step:      10,
		}
		err = execute(ctx, network.PreFundedKeys, config, metrics, logger)
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
		err = execute(ctx, network.PreFundedKeys, config, metrics, logger)
		if err != nil {
			ginkgo.GinkgoT().Error(err)
		}
	})
})
	// ginkgo.It("C-Chain opcoder", func(ctx context.Context) {
	// 	const blockchainID = "C"
	// 	endpoints, err := tmpnet.GetNodeWebsocketURIs(network.Nodes, blockchainID)
	// 	require.NoError(ginkgo.GinkgoT(), err, "getting node websocket URIs")
	// 	config := loadConfig{
	// 		endpoints: endpoints,
	// 		issuer:    issuerOpcoder,
	// 		maxFeeCap: 300000000000,
	// 		agents:    10,
	// 		minTPS:    30,
	// 		maxTPS:    100,
	// 		step:      10,
	// 	}
	// 	err = execute(ctx, network.PreFundedKeys, config)
	// 	if err != nil {
	// 		ginkgo.GinkgoT().Error(err)
	// 	}
	// })
})

func setNetworkGas(t require.TestingT, network *tmpnet.Network, preFundedKeysCount int) {
	if network.DefaultFlags == nil {
		network.DefaultFlags = make(tmpnet.FlagsMap)
	}
	network.DefaultFlags[config.DynamicFeesMaxGasCapacityKey] = "1000000000000000000"
	network.DefaultFlags[config.DynamicFeesMaxGasPerSecondKey] = "10000000000000000000"
	network.DefaultFlags[config.DynamicFeesTargetGasPerSecondKey] = "10000000000000000000"

	// We must set the pre-funded keys to generate a default genesis
	// with those keys.
	preFundedKeys, err := tmpnet.NewPrivateKeys(preFundedKeysCount)
	require.NoError(t, err, "creating pre-funded keys")
	network.PreFundedKeys = preFundedKeys

	network.Genesis, err = network.DefaultGenesis()
	require.NoError(t, err, "creating genesis")
	var cChainGenesis core.Genesis
	err = json.Unmarshal([]byte(network.Genesis.CChainGenesis), &cChainGenesis)
	require.NoError(t, err, "unmarshalling genesis")
	cChainGenesis.GasLimit = 10000000000000000000
	encodedChainGenesis, err := json.Marshal(cChainGenesis)
	require.NoError(t, err, "marshalling C chain genesis")
	network.Genesis.CChainGenesis = string(encodedChainGenesis)
}
