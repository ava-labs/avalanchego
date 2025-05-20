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
	var network *tmpnet.Network

	ginkgo.BeforeAll(func() {
		tc := e2e.NewTestContext()
		env := e2e.GetEnv(tc)
		network = env.GetNetwork()
		network.Nodes = network.Nodes[:nodesCount]
	})

	ginkgo.It("C-Chain simple", func(ctx context.Context) {
		const blockchainID = "C"
		endpoints, err := tmpnet.GetNodeWebsocketURIs(network.Nodes, blockchainID)
		require.NoError(ginkgo.GinkgoT(), err, "getting node websocket URIs")
		config := loadConfig{
			endpoints: endpoints,
			issuer:    issuerSimple,
			maxFeeCap: 4761904, // max fee cap equivalent to 100 ether
			agents:    agentsPerNode,
			minTPS:    50,
			maxTPS:    90,
			step:      10,
		}
		err = execute(ctx, network.PreFundedKeys, config)
		if err != nil {
			ginkgo.GinkgoT().Error(err)
		}
	})

	ginkgo.It("C-Chain opcoder", func(ctx context.Context) {
		const blockchainID = "C"
		endpoints, err := tmpnet.GetNodeWebsocketURIs(network.Nodes, blockchainID)
		require.NoError(ginkgo.GinkgoT(), err, "getting node websocket URIs")
		config := loadConfig{
			endpoints: endpoints,
			issuer:    issuerOpcoder,
			maxFeeCap: 300000000000,
			agents:    agentsCount,
			minTPS:    30,
			maxTPS:    60,
			step:      5,
		}
		err = execute(ctx, network.PreFundedKeys, config)
		if err != nil {
			ginkgo.GinkgoT().Error(err)
		}
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
