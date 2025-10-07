// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const blockchainID = "C"

var flagVars *e2e.FlagVars

func TestDevnetConnection(t *testing.T) {
	ginkgo.RunSpecs(t, "devnet connection test suites")
}

func init() {
	flagVars = e2e.RegisterFlags()
}

var _ = ginkgo.Describe("[Devnet Connection]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	var devnetConfig DevnetConfig

	ginkgo.BeforeEach(func() {
		numNodes, err := flagVars.NodeCount()
		require.NoError(err)

		nodes := tmpnet.NewNodesOrPanic(numNodes)

		keys, err := tmpnet.NewPrivateKeys(numNodes)
		require.NoError(err)

		network := &tmpnet.Network{
			Nodes:         nodes,
			PreFundedKeys: keys,
		}

		e2e.NewTestEnvironment(tc, flagVars, network)

		wsURIs, err := tmpnet.GetNodeWebsocketURIs(network.Nodes, blockchainID)
		require.NoError(err)

		pks := make([]string, len(keys))
		for i, key := range keys {
			pks[i] = string(key.Bytes())
		}

		devnetConfig = DevnetConfig{
			NodeWsURIs:  wsURIs,
			PrivateKeys: pks,
		}
	})

	ginkgo.It("can connect to an existing network", func() {
		registry := prometheus.NewRegistry()
		metricsServer, err := tests.NewPrometheusServer(registry)
		require.NoError(err)

		_ = ConnectNetwork(tc, metricsServer, devnetConfig)
	})
})
