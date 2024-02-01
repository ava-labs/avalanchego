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

// Runs upgrade tests.
package upgrade_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	runner_sdk "github.com/ava-labs/avalanche-network-runner-sdk"
	"github.com/chain4travel/camino-node/tests"
)

func TestUpgrade(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "upgrade test suites")
}

var (
	logLevel                                 string
	networkRunnerGRPCEp                      string
	networkRunnerCaminoNodeExecPath          string
	networkRunnerCaminoNodeExecPathToUpgrade string
	networkRunnerCaminoLogLevel              string
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
		"gRPC server endpoint for network-runner",
	)
	flag.StringVar(
		&networkRunnerCaminoNodeExecPath,
		"network-runner-camino-node-path",
		"",
		"camino-node executable path",
	)
	flag.StringVar(
		&networkRunnerCaminoNodeExecPathToUpgrade,
		"network-runner-camino-node-path-to-upgrade",
		"",
		"camino-node executable path (to upgrade to, only required for upgrade tests with local network-runner)",
	)
	flag.StringVar(
		&networkRunnerCaminoLogLevel,
		"network-runner-camino-log-level",
		"INFO",
		"camino log-level",
	)
}

var runnerCli runner_sdk.Client

var _ = ginkgo.BeforeSuite(func() {
	_, err := os.Stat(networkRunnerCaminoNodeExecPath)
	gomega.Expect(err).Should(gomega.BeNil())

	_, err = os.Stat(networkRunnerCaminoNodeExecPathToUpgrade)
	gomega.Expect(err).Should(gomega.BeNil())

	runnerCli, err = runner_sdk.New(runner_sdk.Config{
		LogLevel:    logLevel,
		Endpoint:    networkRunnerGRPCEp,
		DialTimeout: 10 * time.Second,
	})
	gomega.Expect(err).Should(gomega.BeNil())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	presp, err := runnerCli.Ping(ctx)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())
	tests.Outf("{{green}}network-runner running in PID %d{{/}}\n", presp.Pid)

	tests.Outf("{{magenta}}starting network-runner with %q{{/}}\n", networkRunnerCaminoNodeExecPath)
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	resp, err := runnerCli.Start(ctx, networkRunnerCaminoNodeExecPath,
		runner_sdk.WithNumNodes(5),
		runner_sdk.WithGlobalNodeConfig(fmt.Sprintf(`{"log-level":"%s"}`, networkRunnerCaminoLogLevel)),
	)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())
	tests.Outf("{{green}}successfully started network-runner: {{/}} %+v\n", resp.ClusterInfo.NodeNames)

	// start is async, so wait some time for cluster health
	time.Sleep(time.Minute)

	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
	_, err = runnerCli.Health(ctx)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())
})

var _ = ginkgo.AfterSuite(func() {
	tests.Outf("{{red}}shutting down network-runner cluster{{/}}\n")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	_, err := runnerCli.Stop(ctx)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())

	tests.Outf("{{red}}shutting down network-runner client{{/}}\n")
	err = runnerCli.Close()
	gomega.Expect(err).Should(gomega.BeNil())
})

var _ = ginkgo.Describe("[Upgrade]", func() {
	ginkgo.It("can upgrade versions", func() {
		tests.Outf("{{magenta}}starting upgrade tests %q{{/}}\n", networkRunnerCaminoNodeExecPathToUpgrade)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		sresp, err := runnerCli.Status(ctx)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())

		for _, name := range sresp.ClusterInfo.NodeNames {
			tests.Outf("{{magenta}}restarting the node %q{{/}} with %q\n", name, networkRunnerCaminoNodeExecPathToUpgrade)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := runnerCli.RestartNode(ctx, name, runner_sdk.WithExecPath(networkRunnerCaminoNodeExecPathToUpgrade))
			cancel()
			gomega.Expect(err).Should(gomega.BeNil())

			time.Sleep(20 * time.Second)

			ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
			_, err = runnerCli.Health(ctx)
			cancel()
			gomega.Expect(err).Should(gomega.BeNil())
			tests.Outf("{{green}}successfully upgraded %q to %q{{/}} (current info: %+v)\n", name, networkRunnerCaminoNodeExecPathToUpgrade, resp.ClusterInfo.NodeInfos)
		}
	})
})
