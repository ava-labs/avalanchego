// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load"
)

const (
	blockchainID = "C"
	// invariant: nodesCount >= 5
	nodesCount    = 5
	agentsPerNode = 50
	agentsCount   = nodesCount * agentsPerNode
	logPrefix     = "avalanchego-load-test"
)

var flagVars *e2e.FlagVars

func init() {
	flagVars = e2e.RegisterFlagsWithDefaultOwner("avalanchego-load")

	// Disable default metrics link generation to prevent duplicate links.
	// We generate load specific links.
	e2e.EmitMetricsLink = false
}

func TestLoad(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	log := tests.NewDefaultLogger(logPrefix)
	tc := tests.NewTestContext(log)

	startTime := time.Now()
	nodes := tmpnet.NewNodesOrPanic(nodesCount)
	network := &tmpnet.Network{
		Owner: "avalanchego-load-test",
		Nodes: nodes,
	}

	setPrefundedKeys(t, network, agentsCount)

	testEnv := e2e.NewTestEnvironment(tc, flagVars, network)
	defer func() {
		require.NoError(network.Stop(ctx), "failed to stop network")
	}()

	registry := prometheus.NewRegistry()
	metrics, err := load.NewMetrics(registry)
	require.NoError(err, "failed to register load metrics")

	metricsServer := load.NewPrometheusServer("127.0.0.1:0", registry)
	merticsErrCh, err := metricsServer.Start()
	require.NoError(err, "failed to start load metrics server")

	monitoringConfigFilePath, err := metricsServer.GenerateMonitoringConfig(network.UUID)
	require.NoError(err, "failed to generate monitoring config file")

	defer func() {
		select {
		case err := <-merticsErrCh:
			require.NoError(err, "metrics server exited with error")
		default:
			require.NoError(metricsServer.Stop(), "failed to stop metrics server")
		}
	}()

	defer func() {
		require.NoError(
			os.Remove(monitoringConfigFilePath),
			"failed †o remove monitoring config file",
		)
	}()

	endpoints, err := tmpnet.GetNodeWebsocketURIs(ctx, network.Nodes, blockchainID)
	require.NoError(err, "failed †o get node websocket URIs")
	config := loadConfig{
		endpoints: endpoints,
		agents:    agentsPerNode,
		minTPS:    100,
		maxTPS:    500,
		step:      100,
	}

	require.NoError(
		execute(ctx, network.PreFundedKeys, config, metrics, log),
		"failed to execute load test",
	)

	load.GenerateMetricsLink(testEnv.GetNetwork().UUID, log, startTime)
}

// setPrefundedKeys sets the pre-funded keys for the network, and keeps
// keys already set if any. If there are more keys than required, it
// keeps the already set keys as they are.
func setPrefundedKeys(t require.TestingT, network *tmpnet.Network, minKeys int) {
	if len(network.PreFundedKeys) >= minKeys {
		return
	}

	require := require.New(t)
	missingPreFundedKeys, err := tmpnet.NewPrivateKeys(minKeys - len(network.PreFundedKeys))
	require.NoError(err, "creating pre-funded keys")
	network.PreFundedKeys = append(network.PreFundedKeys, missingPreFundedKeys...)
}
