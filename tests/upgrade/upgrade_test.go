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
	"github.com/ava-labs/avalanchego/tests"
)

func TestUpgrade(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "upgrade test suites")
}

var (
	logLevel                                  string
	networkRunnerGRPCEp                       string
	networkRunnerAvalancheGoExecPath          string
	networkRunnerAvalancheGoExecPathToUpgrade string
	networkRunnerAvalancheGoLogLevel          string
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
		&networkRunnerAvalancheGoExecPath,
		"network-runner-avalanchego-path",
		"",
		"avalanchego executable path",
	)
	flag.StringVar(
		&networkRunnerAvalancheGoExecPathToUpgrade,
		"network-runner-avalanchego-path-to-upgrade",
		"",
		"avalanchego executable path (to upgrade to, only required for upgrade tests with local network-runner)",
	)
	flag.StringVar(
		&networkRunnerAvalancheGoLogLevel,
		"network-runner-avalanchego-log-level",
		"INFO",
		"avalanchego log-level",
	)
}

var runnerCli runner_sdk.Client

var _ = ginkgo.BeforeSuite(func() {
	_, err := os.Stat(networkRunnerAvalancheGoExecPath)
	gomega.Expect(err).Should(gomega.BeNil())

	_, err = os.Stat(networkRunnerAvalancheGoExecPathToUpgrade)
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

	tests.Outf("{{magenta}}starting network-runner with %q{{/}}\n", networkRunnerAvalancheGoExecPath)
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	resp, err := runnerCli.Start(ctx, networkRunnerAvalancheGoExecPath,
		runner_sdk.WithNumNodes(5),
		runner_sdk.WithGlobalNodeConfig(fmt.Sprintf(`{"log-level":"%s"}`, networkRunnerAvalancheGoLogLevel)),
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
		tests.Outf("{{magenta}}starting upgrade tests %q{{/}}\n", networkRunnerAvalancheGoExecPathToUpgrade)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		sresp, err := runnerCli.Status(ctx)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())

		for _, name := range sresp.ClusterInfo.NodeNames {
			tests.Outf("{{magenta}}restarting the node %q{{/}} with %q\n", name, networkRunnerAvalancheGoExecPathToUpgrade)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := runnerCli.RestartNode(ctx, name, runner_sdk.WithExecPath(networkRunnerAvalancheGoExecPathToUpgrade))
			cancel()
			gomega.Expect(err).Should(gomega.BeNil())

			time.Sleep(20 * time.Second)

			ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
			_, err = runnerCli.Health(ctx)
			cancel()
			gomega.Expect(err).Should(gomega.BeNil())
			tests.Outf("{{green}}successfully upgraded %q to %q{{/}} (current info: %+v)\n", name, networkRunnerAvalancheGoExecPathToUpgrade, resp.ClusterInfo.NodeInfos)
		}
	})
})
