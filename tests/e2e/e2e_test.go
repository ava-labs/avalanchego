// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e_test

import (
	"context"
	"flag"
	"os"
	"strings"
	"testing"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	runner_client "github.com/ava-labs/avalanche-network-runner/client"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/e2e"

	// ensure test packages are scanned by ginkgo
	_ "github.com/ava-labs/avalanchego/tests/e2e/ping"
	_ "github.com/ava-labs/avalanchego/tests/e2e/static-handlers"
	_ "github.com/ava-labs/avalanchego/tests/e2e/whitelist-vtx"
	_ "github.com/ava-labs/avalanchego/tests/e2e/x/transfer"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "e2e test suites")
}

var (
	logLevel            string
	avalanchegoLogLevel string

	networkRunnerGRPCEp string
	execPath            string

	enableWhitelistTxTests bool
	uris                   string
)

// TODO: support existing keys

func init() {
	flag.StringVar(
		&logLevel,
		"log-level",
		"info",
		"log level",
	)
	flag.StringVar(
		&avalanchegoLogLevel,
		"avalanchego-log-level",
		"INFO",
		"avalanchegoLogLevel log level (optional, only required for local network-runner)",
	)

	flag.StringVar(
		&networkRunnerGRPCEp,
		"network-runner-grpc-endpoint",
		"",
		"[optional] gRPC server endpoint for network-runner (only required for local network-runner tests)",
	)
	flag.StringVar(
		&execPath,
		"avalanchego-path",
		"",
		"[optional] avalanchego executable path (only required for local network-runner tests)",
	)

	// TODO: set timestamp on the test network machines to be more realistic
	flag.BoolVar(
		&enableWhitelistTxTests,
		"enable-whitelist-vtx-tests",
		false,
		"true to enable whitelist vtx tests",
	)
	flag.StringVar(
		&uris,
		"uris",
		"",
		"URIs for avalanche node (comma-separated, required to run against existing cluster)",
	)
}

var _ = ginkgo.BeforeSuite(func() {
	e2e.SetEnableWhitelistTxTests(enableWhitelistTxTests)

	if execPath != "" {
		_, err := os.Stat(execPath)
		gomega.Expect(err).Should(gomega.BeNil())
		e2e.SetExecPath(execPath)
	}

	// run with local network-runner
	if networkRunnerGRPCEp != "" {
		gomega.Expect(uris).Should(gomega.BeEmpty())

		runnerCli, err := e2e.SetRunnerClient(logLevel, networkRunnerGRPCEp)
		gomega.Expect(err).Should(gomega.BeNil())

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		presp, err := runnerCli.Ping(ctx)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())
		tests.Outf("{{green}}network-runner running in PID %d{{/}}\n", presp.Pid)

		tests.Outf("{{magenta}}starting network-runner with %q{{/}}\n", execPath)
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		resp, err := runnerCli.Start(ctx, execPath, runner_client.WithLogLevel(avalanchegoLogLevel))
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())
		tests.Outf("{{green}}successfully started network-runner :{{/}} %+v\n", resp.ClusterInfo.NodeNames)

		// start is async, so wait some time for cluster health
		time.Sleep(time.Minute)

		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
		_, err = runnerCli.Health(ctx)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())

		var uriSlice []string
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
		uriSlice, err = runnerCli.URIs(ctx)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())
		e2e.SetURIs(uriSlice)
	}

	// connect directly to existing cluster
	if uris != "" {
		gomega.Expect(networkRunnerGRPCEp).Should(gomega.BeEmpty())

		uriSlice := strings.Split(uris, ",")
		e2e.SetURIs(uriSlice)
	}

	uriSlice := e2e.GetURIs()
	tests.Outf("{{green}}URIs:{{/}} %q\n", uriSlice)
})

var _ = ginkgo.AfterSuite(func() {
	if networkRunnerGRPCEp != "" {
		runnerCli := e2e.GetRunnerClient()
		gomega.Expect(runnerCli).ShouldNot(gomega.BeNil())

		tests.Outf("{{red}}shutting down network-runner cluster{{/}}\n")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		_, err := runnerCli.Stop(ctx)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())

		tests.Outf("{{red}}shutting down network-runner client{{/}}\n")
		err = e2e.CloseRunnerClient()
		gomega.Expect(err).Should(gomega.BeNil())
	}
})
