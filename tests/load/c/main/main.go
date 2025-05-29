// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/tests/load/c"
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
	flagVars = e2e.RegisterFlags()

	// Disable default metrics link generation to prevent duplicate links.
	// We generate load specific links.
	e2e.EmitMetricsLink = false
}

func main() {
	log := tests.NewDefaultLogger(logPrefix)
	tc := tests.NewTestContext(log)
	defer tc.Cleanup()

	require := require.New(tc)
	ctx := context.Background()

	nodes := tmpnet.NewNodesOrPanic(nodesCount)

	keys, err := tmpnet.NewPrivateKeys(agentsCount)
	require.NoError(err)
	network := &tmpnet.Network{
		Owner:         "avalanchego-load-test",
		Nodes:         nodes,
		PreFundedKeys: keys,
	}

	e2e.NewTestEnvironment(tc, flagVars, network)
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

	endpoints, err := tmpnet.GetNodeWebsocketURIs(ctx, network.Nodes, blockchainID, func(func()) {})
	require.NoError(err, "failed †o get node websocket URIs")
	config := c.LoadConfig{
		Endpoints: endpoints,
		Agents:    agentsPerNode,
		MinTPS:    100,
		MaxTPS:    500,
		Step:      100,
	}

	require.NoError(
		c.Execute(ctx, network.PreFundedKeys, config, metrics, log),
		"failed to execute load test",
	)
}
