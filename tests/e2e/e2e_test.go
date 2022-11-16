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

	runner_sdk "github.com/ava-labs/avalanche-network-runner-sdk"
	runner_sdk_rpcpb "github.com/ava-labs/avalanche-network-runner-sdk/rpcpb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/subnet-evm/tests/e2e/utils"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	_ "github.com/ava-labs/subnet-evm/tests/e2e/ping"
	_ "github.com/ava-labs/subnet-evm/tests/e2e/solidity"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "subnet-evm e2e test suites")
}

type networkClient struct {
	client runner_sdk.Client
}

var (
	networkRunnerLogLevel string
	gRPCEp                string
	gRPCGatewayEp         string

	// sets the "avalanchego" exec path
	avalanchegoExecPath  string
	avalanchegoPluginDir string
	avalanchegoLogLevel  string
	vmGenesisPath        string

	outputFile string

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
		&avalanchegoExecPath,
		"avalanchego-path",
		"",
		"avalanchego executable path",
	)
	flag.StringVar(
		&avalanchegoPluginDir,
		"avalanchego-plugin-dir",
		"",
		"avalanchego plugin directory",
	)
	flag.StringVar(
		&avalanchegoLogLevel,
		"avalanchego-log-level",
		"info",
		"avalanchego log level",
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
	networkClient := createNetworkClient()

	if skipNetworkRunnerStart {
		utils.Outf("{{green}}skipped 'start'{{/}}\n")
		return
	}

	networkClient.startNetwork()
	networkClient.checkHealth()
})

func createNetworkClient() *networkClient {
	runnerCli, err := runner_sdk.New(runner_sdk.Config{
		LogLevel:    networkRunnerLogLevel,
		Endpoint:    gRPCEp,
		DialTimeout: 10 * time.Second,
	})
	gomega.Expect(err).Should(gomega.BeNil())

	utils.SetOutputFile(outputFile)
	utils.SetExecPath(avalanchegoExecPath)
	utils.SetPluginDir(avalanchegoPluginDir)
	utils.SetVmGenesisPath(vmGenesisPath)
	utils.SetSkipNetworkRunnerShutdown(skipNetworkRunnerShutdown)
	utils.SetClient(runnerCli)

	// Set AVALANCHEGO_PATH for the solidity suite
	os.Setenv("AVALANCHEGO_PATH", avalanchegoExecPath)

	return &networkClient{client: runnerCli}
}

func (n *networkClient) startNetwork() {
	utils.Outf("{{green}}sending 'start' with binary path:{{/}} %q\n", utils.GetExecPath())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	resp, err := n.client.Start(
		ctx,
		utils.GetExecPath(),
		runner_sdk.WithPluginDir(utils.GetPluginDir()),
		runner_sdk.WithGlobalNodeConfig(fmt.Sprintf(`{"log-level":"%s"}`, avalanchegoLogLevel)),
		runner_sdk.WithNumNodes(5),
		runner_sdk.WithBlockchainSpecs(
			[]*runner_sdk_rpcpb.BlockchainSpec{
				{
					VmName:  vmName,
					Genesis: utils.GetVmGenesisPath(),
				},
			},
		))
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())
	utils.Outf("{{green}}successfully started:{{/}} %+v\n", resp.ClusterInfo.NodeNames)
}

func (n *networkClient) checkHealth() {
	// TODO: network runner health should imply custom VM healthiness
	// or provide a separate API for custom VM healthiness
	// "start" is async, so wait some time for cluster health
	utils.Outf("\n{{magenta}}sleeping before checking custom VM status...{{/}}\n")
	time.Sleep(2 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	_, err := n.client.Health(ctx)
	cancel()
	gomega.Expect(err).Should(gomega.BeNil())

	subnetEVMRPCEps = make([]string, 0)
	blockchainID, logsDir := "", ""
	pid := 0

	// wait up to 5-minute for custom VM installation
	utils.Outf("\n{{magenta}}waiting for all custom VMs to report healthy...{{/}}\n")
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
done:
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			break done
		case <-time.After(5 * time.Second):
		}

		utils.Outf("{{magenta}}checking custom VM status{{/}}\n")
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
		resp, err := n.client.Status(cctx)
		ccancel()
		gomega.Expect(err).Should(gomega.BeNil())

		// all logs are stored under root data dir
		logsDir = resp.GetClusterInfo().GetRootDataDir()

		// ANR server pid
		pid = int(resp.GetClusterInfo().GetPid())

		for blkChainID, vmInfo := range resp.ClusterInfo.CustomChains {
			if vmInfo.VmId == vmID.String() {
				blockchainID = blkChainID
				utils.Outf("{{blue}}subnet-evm is ready:{{/}} %+v\n", vmInfo)
				break done
			}
		}
	}
	gomega.Expect(ctx.Err()).Should(gomega.BeNil())
	cancel()

	gomega.Expect(blockchainID).Should(gomega.Not(gomega.BeEmpty()))
	gomega.Expect(logsDir).Should(gomega.Not(gomega.BeEmpty()))

	cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
	uris, err := n.client.URIs(cctx)
	ccancel()
	gomega.Expect(err).Should(gomega.BeNil())
	utils.Outf("{{blue}}avalanche HTTP RPCs URIs:{{/}} %q\n", uris)

	for _, u := range uris {
		rpcEP := fmt.Sprintf("%s/ext/bc/%s/rpc", u, blockchainID)
		subnetEVMRPCEps = append(subnetEVMRPCEps, rpcEP)
		utils.Outf("{{blue}}avalanche subnet-evm RPC:{{/}} %q\n", rpcEP)
	}

	utils.Outf("{{blue}}{{bold}}writing output %q with PID %d{{/}}\n", utils.GetOutputPath(), pid)
	ci := utils.ClusterInfo{
		URIs:                  uris,
		Endpoint:              fmt.Sprintf("/ext/bc/%s", blockchainID),
		PID:                   pid,
		LogsDir:               logsDir,
		SubnetEVMRPCEndpoints: subnetEVMRPCEps,
	}
	utils.SetClusterInfo(ci)
	gomega.Expect(ci.Save(utils.GetOutputPath())).Should(gomega.BeNil())

	b, err := os.ReadFile(utils.GetOutputPath())
	gomega.Expect(err).Should(gomega.BeNil())
	utils.Outf("\n{{blue}}$ cat %s:{{/}}\n%s\n", utils.GetOutputPath(), string(b))
}

var _ = ginkgo.AfterSuite(func() {
	if utils.GetSkipNetworkRunnerShutdown() {
		return
	}

	// if cluster is running, shut it down
	if isRunnerUp() {
		gomega.Expect(stopNetwork()).Should(gomega.BeNil())
	}
	gomega.Expect(closeClient()).Should(gomega.BeNil())
})

func isRunnerUp() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, err := utils.GetClient().Health(ctx)
	cancel()
	return err == nil
}

func stopNetwork() error {
	utils.Outf("{{red}}shutting down network{{/}}\n")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	_, err := utils.GetClient().Stop(ctx)
	cancel()
	return err
}

func closeClient() error {
	utils.Outf("{{red}}shutting down client{{/}}\n")
	return utils.GetClient().Close()
}
