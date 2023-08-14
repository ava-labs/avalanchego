// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet/local"

	// ensure test packages are scanned by ginkgo
	_ "github.com/ava-labs/avalanchego/tests/e2e/banff"
	_ "github.com/ava-labs/avalanchego/tests/e2e/p"
	_ "github.com/ava-labs/avalanchego/tests/e2e/static-handlers"
	_ "github.com/ava-labs/avalanchego/tests/e2e/x/transfer"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "e2e test suites")
}

var (
	avalancheGoExecPath  string
	persistentNetworkDir string
	usePersistentNetwork bool
)

func init() {
	flag.StringVar(
		&avalancheGoExecPath,
		"avalanchego-path",
		os.Getenv(local.AvalancheGoPathEnvName),
		fmt.Sprintf("avalanchego executable path (required if not using a persistent network). Also possible to configure via the %s env variable.", local.AvalancheGoPathEnvName),
	)
	flag.StringVar(
		&persistentNetworkDir,
		"network-dir",
		"",
		fmt.Sprintf("[optional] the dir containing the configuration of a persistent network to target for testing. Useful for speeding up test development. Also possible to configure via the %s env variable.", local.NetworkDirEnvName),
	)
	flag.BoolVar(
		&usePersistentNetwork,
		"use-persistent-network",
		false,
		"[optional] whether to target the persistent network identified by --network-dir.",
	)
}

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	// Run only once in the first ginkgo process

	require := require.New(ginkgo.GinkgoT())

	if usePersistentNetwork && len(persistentNetworkDir) == 0 {
		persistentNetworkDir = os.Getenv(local.NetworkDirEnvName)
	}

	// Load or create a test network
	var network *local.LocalNetwork
	if len(persistentNetworkDir) > 0 {
		tests.Outf("{{yellow}}Using a pre-existing network configured at %s{{/}}\n", persistentNetworkDir)

		var err error
		network, err = local.ReadNetwork(persistentNetworkDir)
		require.NoError(err)
	} else {
		tests.Outf("{{magenta}}Starting network with %q{{/}}\n", avalancheGoExecPath)

		ctx, cancel := context.WithTimeout(context.Background(), local.DefaultNetworkStartTimeout)
		defer cancel()
		var err error
		network, err = local.StartNetwork(
			ctx,
			ginkgo.GinkgoWriter,
			ginkgo.GinkgoT().TempDir(),
			&local.LocalNetwork{
				LocalConfig: local.LocalConfig{
					ExecPath: avalancheGoExecPath,
				},
			},
			testnet.DefaultNodeCount,
			testnet.DefaultFundedKeyCount,
		)
		require.NoError(err)
		ginkgo.DeferCleanup(func() {
			tests.Outf("Shutting down network\n")
			require.NoError(network.Stop())
		})

		tests.Outf("{{green}}Successfully started network{{/}}\n")
	}

	uris := network.GetURIs()
	require.NotEmpty(uris, "network contains no nodes")
	tests.Outf("{{green}}network URIs: {{/}} %+v\n", uris)

	testDataServerURI, err := fixture.ServeTestData(fixture.TestData{
		FundedKeys: network.FundedKeys,
	})
	tests.Outf("{{green}}test data server URI: {{/}} %+v\n", testDataServerURI)
	require.NoError(err)

	env := &e2e.TestEnvironment{
		NetworkDir:        network.Dir,
		URIs:              uris,
		TestDataServerURI: testDataServerURI,
	}
	bytes, err := json.Marshal(env)
	require.NoError(err)
	return bytes
}, func(envBytes []byte) {
	// Run in every ginkgo process

	// Initialize the local test environment from the global state
	e2e.Env = e2e.TestEnvironment{}
	require.NoError(ginkgo.GinkgoT(), json.Unmarshal(envBytes, &e2e.Env))
})
