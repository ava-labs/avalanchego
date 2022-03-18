// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Runs upgrade tests.
package upgrade_test

import (
	"context"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	runner_client "github.com/ava-labs/avalanche-network-runner/client"

	"github.com/ava-labs/avalanchego/tests"
)

func TestUpgrade(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "upgrade test suites")
}

var (
	logLevel            string
	networkRunnerGRPCEp string

	execPath          string
	execPathToUpgrade string
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
		&execPath,
		"avalanchego-path",
		"",
		"avalanchego executable path",
	)
	flag.StringVar(
		&execPathToUpgrade,
		"avalanchego-path-to-upgrade",
		"",
		"avalanchego executable path (to upgrade to, only required for upgrade tests with local network-runner)",
	)
}

var runnerCli runner_client.Client

var _ = ginkgo.BeforeSuite(func() {
	_, err := os.Stat(execPath)
	gomega.Expect(err).Should(gomega.BeNil())

	_, err = os.Stat(execPathToUpgrade)
	gomega.Expect(err).Should(gomega.BeNil())

	runnerCli, err = runner_client.New(runner_client.Config{
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

	tests.Outf("{{magenta}}starting network-runner with %q{{/}}\n", execPath)
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	resp, err := runnerCli.Start(ctx, execPath)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())
	tests.Outf("{{green}}successfully started network-runner :{{/}} %+v\n", resp.ClusterInfo.NodeNames)

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
		tests.Outf("{{magenta}}starting upgrade tests %q{{/}}\n", execPathToUpgrade)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		sresp, err := runnerCli.Status(ctx)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())

		for _, name := range sresp.ClusterInfo.NodeNames {
			tests.Outf("{{magenta}}restarting the node %q{{/}} with %q\n", name, execPathToUpgrade)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := runnerCli.RestartNode(ctx, name, execPathToUpgrade)
			cancel()
			gomega.Expect(err).Should(gomega.BeNil())

			time.Sleep(20 * time.Second)

			ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
			_, err = runnerCli.Health(ctx)
			cancel()
			gomega.Expect(err).Should(gomega.BeNil())
			tests.Outf("{{green}}successfully upgraded %q to %q{{/}} (current info: %+v)\n", name, execPathToUpgrade, resp.ClusterInfo.NodeInfos)
		}
	})
})
