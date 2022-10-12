// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// e2e implements the e2e tests.
package e2e

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ava-labs/avalanche-network-runner/client"
	"github.com/ava-labs/avalanche-network-runner/rpcpb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/subnet-evm/tests/e2e/runner"
	"github.com/ava-labs/subnet-evm/tests/e2e/utils"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/formatter"
	"github.com/onsi/gomega"
	"gopkg.in/yaml.v2"

	_ "github.com/ava-labs/subnet-evm/tests/e2e/ping"
	_ "github.com/ava-labs/subnet-evm/tests/e2e/solidity"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "subnet-evm e2e test suites")
}

var (
	networkRunnerLogLevel string
	gRPCEp                string
	gRPCGatewayEp         string

	outputFile string
	pluginDir  string

	// sets the "avalanchego" exec path
	execPath string

	vmGenesisPath string

	skipNetworkRunnerStart    bool
	skipNetworkRunnerShutdown bool
)

func init() {
	flag.StringVar(
		&networkRunnerLogLevel,
		"network-runner-log-level",
		"info",
		"gRPC server endpoint",
	)
	flag.StringVar(
		&gRPCEp,
		"network-runner-grpc-endpoint",
		"0.0.0.0:8080",
		"gRPC server endpoint",
	)
	flag.StringVar(
		&gRPCGatewayEp,
		"network-runner-grpc-gateway-endpoint",
		"0.0.0.0:8081",
		"gRPC gateway endpoint",
	)

	flag.StringVar(
		&execPath,
		"avalanchego-path",
		"",
		"avalanchego executable path",
	)
	flag.StringVar(
		&pluginDir,
		"avalanchego-plugin-dir",
		"",
		"avalanchego plugin directory",
	)
	flag.StringVar(
		&vmGenesisPath,
		"vm-genesis-path",
		"",
		"VM genesis file path",
	)
	flag.StringVar(
		&outputFile,
		"output-path",
		"",
		"output YAML path to write local cluster information",
	)
	flag.BoolVar(
		&skipNetworkRunnerStart,
		"skip-network-runner-start",
		false,
		"'true' to skip network runner start",
	)
	flag.BoolVar(
		&skipNetworkRunnerShutdown,
		"skip-network-runner-shutdown",
		false,
		"'true' to skip network runner shutdown",
	)
}

const vmName = "subnetevm"

var vmID ids.ID

func init() {
	// TODO: add "getVMID" util function in avalanchego and import from "avalanchego"
	b := make([]byte, 32)
	copy(b, []byte(vmName))
	var err error
	vmID, err = ids.ToID(b)
	if err != nil {
		panic(err)
	}
}

var subnetEVMRPCEps []string

var _ = ginkgo.BeforeSuite(func() {
	utils.SetOutputFile(outputFile)
	utils.SetPluginDir(pluginDir)
	utils.SetExecPath(execPath)
	utils.SetPluginDir(pluginDir)
	utils.SetVmGenesisPath(vmGenesisPath)
	utils.SetSkipNetworkRunnerStart(skipNetworkRunnerStart)
	utils.SetSkipNetworkRunnerShutdown(skipNetworkRunnerShutdown)

	err := runner.InitializeRunner(execPath, gRPCEp, networkRunnerLogLevel)
	gomega.Expect(err).Should(gomega.BeNil())

	if utils.GetSkipNetworkRunnerStart() {
		return
	}

	runnerCli := runner.GetClient()
	gomega.Expect(runnerCli).ShouldNot(gomega.BeNil())

	ginkgo.By("calling start API via network runner", func() {
		outf("{{green}}sending 'start' with binary path:{{/}} %q\n", utils.GetExecPath())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		resp, err := runnerCli.Start(
			ctx,
			utils.GetExecPath(),
			client.WithPluginDir(utils.GetPluginDir()),
			client.WithBlockchainSpecs(
				[]*rpcpb.BlockchainSpec{
					{
						VmName:  vmName,
						Genesis: utils.GetVmGenesisPath(),
					},
				},
			))
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())
		outf("{{green}}successfully started:{{/}} %+v\n", resp.ClusterInfo.NodeNames)
	})

	// TODO: network runner health should imply custom VM healthiness
	// or provide a separate API for custom VM healthiness
	// "start" is async, so wait some time for cluster health
	outf("\n{{magenta}}sleeping before checking custom VM status...{{/}}\n")
	time.Sleep(2 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	_, err = runnerCli.Health(ctx)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())

	subnetEVMRPCEps = make([]string, 0)
	blockchainID, logsDir := "", ""
	pid := 0

	// wait up to 5-minute for custom VM installation
	outf("\n{{magenta}}waiting for all custom VMs to report healthy...{{/}}\n")
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
done:
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			break done
		case <-time.After(5 * time.Second):
		}

		outf("{{magenta}}checking custom VM status{{/}}\n")
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
		resp, err := runnerCli.Status(cctx)
		ccancel()
		gomega.Expect(err).Should(gomega.BeNil())

		// all logs are stored under root data dir
		logsDir = resp.GetClusterInfo().GetRootDataDir()

		// ANR server pid
		pid = int(resp.GetClusterInfo().GetPid())

		for blkChainID, vmInfo := range resp.ClusterInfo.CustomChains {
			if vmInfo.VmId == vmID.String() {
				blockchainID = blkChainID
				outf("{{blue}}subnet-evm is ready:{{/}} %+v\n", vmInfo)
				break done
			}
		}
	}
	gomega.Expect(ctx.Err()).Should(gomega.BeNil())
	cancel()

	gomega.Expect(blockchainID).Should(gomega.Not(gomega.BeEmpty()))
	gomega.Expect(logsDir).Should(gomega.Not(gomega.BeEmpty()))

	cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
	uris, err := runnerCli.URIs(cctx)
	ccancel()
	gomega.Expect(err).Should(gomega.BeNil())
	outf("{{blue}}avalanche HTTP RPCs URIs:{{/}} %q\n", uris)

	for _, u := range uris {
		rpcEP := fmt.Sprintf("%s/ext/bc/%s/rpc", u, blockchainID)
		subnetEVMRPCEps = append(subnetEVMRPCEps, rpcEP)
		outf("{{blue}}avalanche subnet-evm RPC:{{/}} %q\n", rpcEP)
	}

	outf("{{blue}}{{bold}}writing output %q with PID %d{{/}}\n", utils.GetOutputPath(), pid)
	ci := clusterInfo{
		URIs:     uris,
		Endpoint: fmt.Sprintf("/ext/bc/%s", blockchainID),
		PID:      pid,
		LogsDir:  logsDir,
	}
	gomega.Expect(ci.Save(utils.GetOutputPath())).Should(gomega.BeNil())

	b, err := os.ReadFile(utils.GetOutputPath())
	gomega.Expect(err).Should(gomega.BeNil())
	outf("\n{{blue}}$ cat %s:{{/}}\n%s\n", utils.GetOutputPath(), string(b))
})

var _ = ginkgo.AfterSuite(func() {
	if utils.GetSkipNetworkRunnerShutdown() {
		return
	}

	// if cluster is running, shut it down
	running := runner.IsRunnerUp()
	if running {
		err := runner.StopNetwork()
		gomega.Expect(err).Should(gomega.BeNil())
	}
	err := runner.ShutdownClient()
	gomega.Expect(err).Should(gomega.BeNil())
})

// Outputs to stdout.
//
// e.g.,
//   Out("{{green}}{{bold}}hi there %q{{/}}", "aa")
//   Out("{{magenta}}{{bold}}hi therea{{/}} {{cyan}}{{underline}}b{{/}}")
//
// ref.
// https://github.com/onsi/ginkgo/blob/v2.0.0/formatter/formatter.go#L52-L73
//
func outf(format string, args ...interface{}) {
	s := formatter.F(format, args...)
	fmt.Fprint(formatter.ColorableStdOut, s)
}

// clusterInfo represents the local cluster information.
type clusterInfo struct {
	URIs     []string `json:"uris"`
	Endpoint string   `json:"endpoint"`
	PID      int      `json:"pid"`
	LogsDir  string   `json:"logsDir"`
}

const fsModeWrite = 0o600

func (ci clusterInfo) Save(p string) error {
	ob, err := yaml.Marshal(ci)
	if err != nil {
		return err
	}
	return os.WriteFile(p, ob, fsModeWrite)
}
