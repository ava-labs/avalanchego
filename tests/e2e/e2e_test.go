// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e_test

import (
	"flag"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/ava-labs/avalanchego/tests/e2e"

	// ensure test packages are scanned by ginkgo
	_ "github.com/ava-labs/avalanchego/tests/e2e/banff"
	_ "github.com/ava-labs/avalanchego/tests/e2e/p"
	_ "github.com/ava-labs/avalanchego/tests/e2e/ping"
	_ "github.com/ava-labs/avalanchego/tests/e2e/static-handlers"
	_ "github.com/ava-labs/avalanchego/tests/e2e/x/transfer"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "e2e test suites")
}

var (
	networkRunnerGRPCEp   string
	networkRunnerLogLevel string

	networkRunnerAvalancheGoExecPath string
	networkRunnerAvalancheGoLogLevel string

	testKeysFile string

	usePersistentNetwork bool
)

func init() {
	flag.StringVar(
		&networkRunnerLogLevel,
		"log-level",
		"info",
		"log level",
	)

	flag.StringVar(
		&networkRunnerGRPCEp,
		"network-runner-grpc-endpoint",
		// TODO(marun) Default this.
		"",
		"gRPC server endpoint for network-runner",
	)
	flag.StringVar(
		&networkRunnerAvalancheGoExecPath,
		"network-runner-avalanchego-path",
		// TODO(marun) Default this.
		"",
		"[optional] avalanchego executable path (required if not using a persistent network)",
	)
	flag.StringVar(
		&networkRunnerAvalancheGoLogLevel,
		"network-runner-avalanchego-log-level",
		"INFO",
		"[optional] avalanchego log-level (used if not using a persistent network)",
	)

	// file that contains a list of new-line separated secp256k1 private keys
	flag.StringVar(
		&testKeysFile,
		"test-keys-file",
		// TODO(marun) Default this to the in-tree file
		"",
		"file that contains a list of new-line separated hex-encoded secp256k1 private keys (assume test keys are pre-funded, for test networks)",
	)

	flag.BoolVar(
		&usePersistentNetwork,
		"use-persistent-network",
		false,
		"whether to target a network that is already running. Useful for speeding up test development.",
	)
}

var _ = ginkgo.BeforeSuite(func() {
	err := e2e.Env.Setup(
		networkRunnerLogLevel,
		networkRunnerGRPCEp,
		networkRunnerAvalancheGoExecPath,
		networkRunnerAvalancheGoLogLevel,
		testKeysFile,
		usePersistentNetwork,
	)
	gomega.Expect(err).Should(gomega.BeNil())
})

var _ = ginkgo.AfterSuite(func() {
	err := e2e.Env.Teardown()
	gomega.Expect(err).Should(gomega.BeNil())
})
