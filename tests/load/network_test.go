// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

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
	ctx := context.Background()

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
			b, err := key.MarshalJSON()
			require.NoError(err)

			pks[i] = string(b)
		}

		devnetConfig = DevnetConfig{
			NodeWsURIs:  wsURIs,
			PrivateKeys: pks,
		}
	})

	ginkgo.It("can connect to an existing network", func() {
		workers := ConnectNetwork(tc, devnetConfig)

		client := workers[0].Client
		chainID, err := client.ChainID(ctx)
		require.NoError(err)

		test := TransferTest{
			Value: big.NewInt(1),
		}

		registry := prometheus.NewRegistry()
		generator, err := NewLoadGenerator(workers, chainID, "devnet-connection-test", registry, test)
		require.NoError(err)

		timeout := 30 * time.Second
		generator.Run(ctx, tc.Log(), timeout, timeout)
	})
})
