// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	nodesCount = 3
	metricsURI = "127.0.0.1:8082"
	// relative to the user home directory
	metricsFilePath = ".tmpnet/prometheus/file_sd_configs/c-chain-load-test.json"
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
	var network *tmpnet.Network

	ginkgo.BeforeAll(func() {
		tc := e2e.NewTestContext()
		env := e2e.GetEnv(tc)
		r := require.New(tc)

		network = env.GetNetwork()
		network.Nodes = network.Nodes[:nodesCount]
		for _, node := range network.Nodes {
			err := node.EnsureKeys()
			r.NoError(err, "ensuring keys for node %s", node.NodeID)
		}

		collectorConfigBytes, err := generateCollectorConfig(
			[]string{metricsURI},
			e2e.GetEnv(tc).GetNetwork().UUID,
		)
		r.NoError(err, "failed to generate collector config for network %s", e2e.GetEnv(tc).GetNetwork().UUID)

		homedir, err := os.UserHomeDir()
		r.NoError(err, "failed to get user home dir")

		collectorFilePath := filepath.Join(homedir, metricsFilePath)
		r.NoError(writeCollectorConfig(collectorFilePath, collectorConfigBytes), "failed to write collector config at path %s", collectorFilePath)

		ginkgo.DeferCleanup(func() {
			r.NoError(os.Remove(collectorFilePath))
		})
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
		err = execute(ctx, network.PreFundedKeys, config)
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
		err = execute(ctx, network.PreFundedKeys, config)
		if err != nil {
			ginkgo.GinkgoT().Error(err)
		}
	})
})

func generateCollectorConfig(targets []string, uuid string) ([]byte, error) {
	nodeLabels := map[string]string{
		"network_owner": "c-chain-load-test",
		"network_uuid":  uuid,
	}
	cfg := []map[string]any{
		{
			"labels":  nodeLabels,
			"targets": targets,
		},
	}

	return json.MarshalIndent(cfg, "", " ")
}

func writeCollectorConfig(metricsFilePath string, config []byte) error {
	file, err := os.OpenFile(metricsFilePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	defer func() {
		_ = file.Close()
	}()

	if _, err := file.Write(config); err != nil {
		return err
	}

	return nil
}
