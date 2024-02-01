// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e_test

import (
	"flag"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	// ensure test packages are scanned by ginkgo
	"github.com/chain4travel/camino-node/tests/e2e"
	_ "github.com/chain4travel/camino-node/tests/e2e/banff"
	_ "github.com/chain4travel/camino-node/tests/e2e/p"
	_ "github.com/chain4travel/camino-node/tests/e2e/ping"
	_ "github.com/chain4travel/camino-node/tests/e2e/static-handlers"
	_ "github.com/chain4travel/camino-node/tests/e2e/x/transfer"
	_ "github.com/chain4travel/camino-node/tests/e2e/x/whitelist-vtx"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "e2e test suites")
}

var (
	// helpers to parse test flags
	logLevel string

	networkRunnerGRPCEp             string
	networkRunnerCaminoNodeExecPath string
	networkRunnerCaminoLogLevel     string

	uris string

	testKeysFile string
)

func init() {
	flag.StringVar(
		&logLevel,
		"log-level",
		"info",
		"log level",
	)

	flag.StringVar(
		&networkRunnerGRPCEp,
		"network-runner-grpc-endpoint",
		"",
		"[optional] gRPC server endpoint for network-runner (only required for local network-runner tests)",
	)
	flag.StringVar(
		&networkRunnerCaminoNodeExecPath,
		"network-runner-camino-node-path",
		"",
		"[optional] camino-node executable path (only required for local network-runner tests)",
	)
	flag.StringVar(
		&networkRunnerCaminoLogLevel,
		"network-runner-camino-log-level",
		"INFO",
		"[optional] camino log-level (only required for local network-runner tests)",
	)

	// e.g., custom network HTTP RPC endpoints
	flag.StringVar(
		&uris,
		"uris",
		"",
		"HTTP RPC endpoint URIs for camino node (comma-separated, required to run against existing cluster)",
	)

	// file that contains a list of new-line separated secp256k1 private keys
	flag.StringVar(
		&testKeysFile,
		"test-keys-file",
		"",
		"file that contains a list of new-line separated hex-encoded secp256k1 private keys (assume test keys are pre-funded, for test networks)",
	)
}

var _ = ginkgo.BeforeSuite(func() {
	err := e2e.Env.ConfigCluster(
		logLevel,
		networkRunnerGRPCEp,
		networkRunnerCaminoNodeExecPath,
		networkRunnerCaminoLogLevel,
		uris,
		testKeysFile,
	)
	gomega.Expect(err).Should(gomega.BeNil())

	// check cluster can be started
	err = e2e.Env.StartCluster()
	gomega.Expect(err).Should(gomega.BeNil())

	// load keys
	err = e2e.Env.LoadKeys()
	gomega.Expect(err).Should(gomega.BeNil())

	// take initial snapshot. cluster will be switched off
	err = e2e.Env.SnapInitialState()
	gomega.Expect(err).Should(gomega.BeNil())

	// restart cluster
	err = e2e.Env.RestoreInitialState(false /*switchOffNetworkFirst*/)
	gomega.Expect(err).Should(gomega.BeNil())
})

var _ = ginkgo.AfterSuite(func() {
	err := e2e.Env.ShutdownCluster()
	gomega.Expect(err).Should(gomega.BeNil())
})
