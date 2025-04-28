// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

// Run this using the command:
// ./bin/ginkgo -v ./tests/load -- --avalanchego-path=$PWD/build/avalanchego
func TestLoad(t *testing.T) {
	ginkgo.RunSpecs(t, "load tests")
}

var flagVars *e2e.FlagVars

func init() {
	flagVars = e2e.RegisterFlagsWithDefaultOwner("avalanchego-load")
}

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	// Run only once in the first ginkgo process

	tc := e2e.NewTestContext()
	nodes := tmpnet.NewNodesOrPanic(tmpnet.DefaultNodeCount)
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
	var privateNetwork *tmpnet.Network

	ginkgo.BeforeAll(func() {
		tc := e2e.NewTestContext()
		env := e2e.GetEnv(tc)
		privateNetwork = tmpnet.NewDefaultNetwork("avalanchego-load-test")
		privateNetwork.DefaultFlags = tmpnet.FlagsMap{}
		publicNetwork := env.GetNetwork()
		privateNetwork.DefaultFlags.SetDefaults(publicNetwork.DefaultFlags)
		privateNetwork.Nodes = make([]*tmpnet.Node, tmpnet.DefaultNodeCount)
		for i := range privateNetwork.Nodes {
			node := tmpnet.NewNode()
			err := node.EnsureKeys()
			require.NoError(tc, err, "ensuring keys for node %s", node.NodeID)
			privateNetwork.Nodes[i] = node
		}
		env.StartPrivateNetwork(privateNetwork)
	})

	ginkgo.It("C-Chain", func(ctx context.Context) {
		nodes := privateNetwork.Nodes
		preFundedKey := privateNetwork.PreFundedKeys[0].ToECDSA()
		const blockchainID = "C"
		endpoints, err := tmpnet.GetNodeWebsocketURIs(nodes, blockchainID)
		require.NoError(ginkgo.GinkgoT(), err, "getting node websocket URIs")
		config := config{
			endpoints:   endpoints,
			maxFeeCap:   50,
			maxTipCap:   1,
			agents:      1,
			txsPerAgent: 100,
		}
		err = execute(ctx, preFundedKey, config)
		if err != nil {
			ginkgo.GinkgoT().Error(err)
		}
	})
})
